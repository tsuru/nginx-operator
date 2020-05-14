// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package k8s

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	tsuruConfig "github.com/tsuru/config"
	"github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// Default docker image used for nginx
	defaultNginxImage = "nginx:latest"

	// Default port names used by the nginx container and the ClusterIP service
	defaultHTTPPort            = int32(8080)
	defaultHTTPHostNetworkPort = int32(80)
	defaultHTTPPortName        = "http"

	defaultHTTPSPort            = int32(8443)
	defaultHTTPSHostNetworkPort = int32(443)
	defaultHTTPSPortName        = "https"

	defaultCacheVolumeExtraSize = float64(1.05)

	curlProbeCommand = "curl -m%d -kfsS -o /dev/null %s"

	// Mount path where nginx.conf will be placed
	configMountPath = "/etc/nginx"

	// Default configuration filename of nginx
	configFileName = "nginx.conf"

	// Mount path where certificate and key pair will be placed
	certMountPath = configMountPath + "/certs"

	// Mount path where the additional files will be mounted on
	extraFilesMountPath = configMountPath + "/extra_files"

	// Annotation key used to stored the nginx that created the deployment
	generatedFromAnnotation = "nginx.tsuru.io/generated-from"
)

var nginxEntrypoint = []string{
	"/bin/sh",
	"-c",
	"while ! [ -f /tmp/done ]; do sleep 0.5; done && exec nginx -g 'daemon off;'",
}

var defaultPostStartCommand = []string{
	"/bin/sh",
	"-c",
	"nginx -t && touch /tmp/done",
}

// NewDeployment creates a deployment for a given Nginx resource.
func NewDeployment(n *v1alpha1.Nginx) (*appv1.Deployment, error) {
	n.Spec.Image = valueOrDefault(n.Spec.Image, defaultNginxImage)
	setDefaultPorts(&n.Spec.PodTemplate)

	if n.Spec.Replicas == nil {
		var one int32 = 1
		n.Spec.Replicas = &one
	}

	securityContext := n.Spec.PodTemplate.SecurityContext

	if hasLowPort(n.Spec.PodTemplate.Ports) {
		if securityContext == nil {
			securityContext = &corev1.SecurityContext{}
		}
		if securityContext.Capabilities == nil {
			securityContext.Capabilities = &corev1.Capabilities{}
		}
		securityContext.Capabilities.Add = append(securityContext.Capabilities.Add, "NET_BIND_SERVICE")
	}

	var maxSurge, maxUnavailable *intstr.IntOrString
	if n.Spec.PodTemplate.HostNetwork {
		// Round up instead of down as is the default behavior for maxUnvailable,
		// this is useful because we must allow at least one pod down for
		// hostNetwork deployments.
		adjustedValue := intstr.FromInt(int(math.Ceil(float64(*n.Spec.Replicas) * 0.25)))
		maxUnavailable = &adjustedValue
		maxSurge = &adjustedValue
	}

	deployment := appv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.Name,
			Namespace: n.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(n, schema.GroupVersionKind{
					Group:   v1alpha1.SchemeGroupVersion.Group,
					Version: v1alpha1.SchemeGroupVersion.Version,
					Kind:    "Nginx",
				}),
			},
		},
		Spec: appv1.DeploymentSpec{
			Strategy: appv1.DeploymentStrategy{
				Type: appv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appv1.RollingUpdateDeployment{
					MaxUnavailable: maxUnavailable,
					MaxSurge:       maxSurge,
				},
			},
			Replicas: n.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: LabelsForNginx(n.Name),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   n.Namespace,
					Annotations: assembleAnnotations(*n),
					Labels:      assembleLabels(*n),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "nginx",
							Image:           n.Spec.Image,
							Command:         nginxEntrypoint,
							Resources:       n.Spec.Resources,
							SecurityContext: securityContext,
							Ports:           n.Spec.PodTemplate.Ports,
							VolumeMounts:    n.Spec.PodTemplate.VolumeMounts,
						},
					},
					InitContainers:                n.Spec.PodTemplate.InitContainers,
					Affinity:                      n.Spec.PodTemplate.Affinity,
					HostNetwork:                   n.Spec.PodTemplate.HostNetwork,
					TerminationGracePeriodSeconds: n.Spec.PodTemplate.TerminationGracePeriodSeconds,
					Volumes:                       n.Spec.PodTemplate.Volumes,
				},
			},
		},
	}
	setupProbes(n.Spec, &deployment)
	setupConfig(n.Spec.Config, &deployment)
	setupTLS(n.Spec.Certificates, &deployment)
	setupExtraFiles(n.Spec.ExtraFiles, &deployment)
	setupCacheVolume(n.Spec.Cache, &deployment)
	setupLifecycle(n.Spec.Lifecycle, &deployment)

	// This is done on the last step because n.Spec may have mutated during these methods
	if err := SetNginxSpec(&deployment.ObjectMeta, n.Spec); err != nil {
		return nil, err
	}

	return &deployment, nil
}

func mergeMap(a, b map[string]string) map[string]string {
	if a == nil {
		return b
	}
	for k, v := range b {
		a[k] = v
	}
	return a
}

// NewService assembles the ClusterIP service for the Nginx
func NewService(n *v1alpha1.Nginx) *corev1.Service {
	var labels, annotations map[string]string
	var lbIP string
	var externalTrafficPolicy corev1.ServiceExternalTrafficPolicyType
	labelSelector := LabelsForNginx(n.Name)
	if n.Spec.Service != nil {
		labels = n.Spec.Service.Labels
		annotations = n.Spec.Service.Annotations
		lbIP = n.Spec.Service.LoadBalancerIP
		externalTrafficPolicy = n.Spec.Service.ExternalTrafficPolicy
		if n.Spec.Service.UsePodSelector != nil && !*n.Spec.Service.UsePodSelector {
			labelSelector = nil
		}
	}
	service := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.Name + "-service",
			Namespace: n.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(n, schema.GroupVersionKind{
					Group:   v1alpha1.SchemeGroupVersion.Group,
					Version: v1alpha1.SchemeGroupVersion.Version,
					Kind:    "Nginx",
				}),
			},
			Labels:      mergeMap(labels, LabelsForNginx(n.Name)),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       defaultHTTPPortName,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString(defaultHTTPPortName),
					Port:       int32(80),
				},
				{
					Name:       defaultHTTPSPortName,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString(defaultHTTPSPortName),
					Port:       int32(443),
				},
			},
			Selector:              labelSelector,
			LoadBalancerIP:        lbIP,
			Type:                  nginxService(n),
			ExternalTrafficPolicy: externalTrafficPolicy,
		},
	}
	return &service
}

func nginxService(n *v1alpha1.Nginx) corev1.ServiceType {
	if n == nil || n.Spec.Service == nil {
		return corev1.ServiceTypeClusterIP
	}
	return corev1.ServiceType(n.Spec.Service.Type)
}

// LabelsForNginx returns the labels for a Nginx CR with the given name
func LabelsForNginx(name string) map[string]string {
	return map[string]string{
		"nginx.tsuru.io/resource-name": name,
		"nginx.tsuru.io/app":           "nginx",
	}
}

// LabelsForNginxString returns the labels in string format.
func LabelsForNginxString(name string) string {
	return k8slabels.FormatLabels(LabelsForNginx(name))
}

func GetNginxNameFromObject(o metav1.Object) string {
	return o.GetLabels()["nginx.tsuru.io/resource-name"]
}

// ExtractNginxSpec extracts the nginx used to create the object
func ExtractNginxSpec(o metav1.ObjectMeta) (v1alpha1.NginxSpec, error) {
	ann, ok := o.Annotations[generatedFromAnnotation]
	if !ok {
		return v1alpha1.NginxSpec{}, fmt.Errorf("missing %q annotation in deployment", generatedFromAnnotation)
	}
	var spec v1alpha1.NginxSpec
	if err := json.Unmarshal([]byte(ann), &spec); err != nil {
		return v1alpha1.NginxSpec{}, fmt.Errorf("failed to unmarshal nginx from annotation: %v", err)
	}
	return spec, nil
}

// SetNginxSpec sets the nginx spec into the object annotation to be later extracted
func SetNginxSpec(o *metav1.ObjectMeta, spec v1alpha1.NginxSpec) error {
	if o.Annotations == nil {
		o.Annotations = make(map[string]string)
	}
	origSpec, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	o.Annotations[generatedFromAnnotation] = string(origSpec)
	return nil
}

func setupConfig(conf *v1alpha1.ConfigRef, dep *appv1.Deployment) {
	if conf == nil {
		return
	}
	dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      "nginx-config",
		MountPath: fmt.Sprintf("%s/%s", configMountPath, configFileName),
		SubPath:   configFileName,
	})
	switch conf.Kind {
	case v1alpha1.ConfigKindConfigMap:
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "nginx-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: conf.Name,
					},
				},
			},
		})
	case v1alpha1.ConfigKindInline:
		// FIXME: inline content is being written out of order
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = make(map[string]string)
		}
		dep.Spec.Template.Annotations[conf.Name] = conf.Value
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "nginx-config",
			VolumeSource: corev1.VolumeSource{
				DownwardAPI: &corev1.DownwardAPIVolumeSource{
					Items: []corev1.DownwardAPIVolumeFile{
						{
							Path: "nginx.conf",
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: fmt.Sprintf("metadata.annotations['%s']", conf.Name),
							},
						},
					},
				},
			},
		})
	}
}

// setupTLS appends an https port if TLS secrets are specified
func setupTLS(secret *v1alpha1.TLSSecret, dep *appv1.Deployment) {
	if secret == nil {
		return
	}

	dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      "nginx-certs",
		MountPath: certMountPath,
	})

	var items []corev1.KeyToPath
	for _, item := range secret.Items {
		items = append(items, corev1.KeyToPath{
			Key:  item.CertificateField,
			Path: valueOrDefault(item.CertificatePath, item.CertificateField),
		}, corev1.KeyToPath{
			Key:  item.KeyField,
			Path: valueOrDefault(item.KeyPath, item.KeyField),
		})
	}

	dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: "nginx-certs",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secret.SecretName,
				Items:      items,
			},
		},
	})
}

// setupExtraFiles configures the volume source and mount into Deployment resource.
func setupExtraFiles(fRef *v1alpha1.FilesRef, dep *appv1.Deployment) {
	if fRef == nil {
		return
	}
	volumeMountName := "nginx-extra-files"
	dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      volumeMountName,
		MountPath: extraFilesMountPath,
	})
	var items []corev1.KeyToPath
	for key, path := range fRef.Files {
		items = append(items, corev1.KeyToPath{
			Key:  key,
			Path: path,
		})
	}
	// putting the items in a deterministic order to allow tests
	if items != nil {
		sort.Slice(items, func(i, j int) bool {
			return items[i].Key < items[j].Key
		})
	}
	dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: volumeMountName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: fRef.Name,
				},
				Items: items,
			},
		},
	})
}

func valueOrDefault(value, def string) string {
	if value != "" {
		return value
	}
	return def
}

func assembleLabels(n v1alpha1.Nginx) map[string]string {
	labels := LabelsForNginx(n.Name)
	if value, err := tsuruConfig.Get("nginx-controller:pod-template:labels"); err == nil {
		if controllerLabels, ok := value.(map[interface{}]interface{}); ok {
			labels = mergeMap(labels, convertToStringMap(controllerLabels))
		}
	}
	return mergeMap(labels, n.Spec.PodTemplate.Labels)
}

func assembleAnnotations(n v1alpha1.Nginx) map[string]string {
	var annotations map[string]string
	if value, err := tsuruConfig.Get("nginx-controller:pod-template:annotations"); err == nil {
		if controllerAnnotations, ok := value.(map[interface{}]interface{}); ok {
			annotations = convertToStringMap(controllerAnnotations)
		}
	}
	return mergeMap(annotations, n.Spec.PodTemplate.Annotations)
}

func convertToStringMap(m map[interface{}]interface{}) map[string]string {
	var result map[string]string
	for k, v := range m {
		if result == nil {
			result = make(map[string]string)
		}
		key, ok := k.(string)
		if !ok {
			continue
		}
		value, ok := v.(string)
		if !ok {
			continue
		}
		result[key] = value
	}
	return result
}

func setupCacheVolume(cache v1alpha1.NginxCacheSpec, dep *appv1.Deployment) {
	if cache.Path == "" {
		return
	}
	const cacheVolName = "cache-vol"
	medium := corev1.StorageMediumDefault
	if cache.InMemory {
		medium = corev1.StorageMediumMemory
	}
	cacheVolume := corev1.Volume{
		Name: cacheVolName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: medium,
			},
		},
	}
	if cache.Size != nil {
		// Nginx cache manager allows the cache size to temporarily exceeds
		// the limit configured with the max_size directive. Here we are adding
		// extra 5% of space to the cache volume to avoid pod evictions.
		// https://docs.nginx.com/nginx/admin-guide/content-cache/content-caching/#nginx-processes-involved-in-caching
		cacheLimit := math.Ceil(float64(cache.Size.Value()) * defaultCacheVolumeExtraSize)
		cacheVolume.VolumeSource.EmptyDir.SizeLimit = resource.NewQuantity(int64(cacheLimit), resource.BinarySI)
	}
	dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, cacheVolume)
	dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      cacheVolName,
		MountPath: cache.Path,
	})
}

func setupLifecycle(lifecycle *v1alpha1.NginxLifecycle, dep *appv1.Deployment) {
	defaultLifecycle := corev1.Lifecycle{
		PostStart: &corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: defaultPostStartCommand,
			},
		},
	}
	dep.Spec.Template.Spec.Containers[0].Lifecycle = &defaultLifecycle
	if lifecycle == nil {
		return
	}
	if lifecycle.PreStop != nil && lifecycle.PreStop.Exec != nil {
		dep.Spec.Template.Spec.Containers[0].Lifecycle.PreStop = &corev1.Handler{Exec: lifecycle.PreStop.Exec}
	}
	if lifecycle.PostStart != nil && lifecycle.PostStart.Exec != nil {
		var postStartCommand []string
		if len(lifecycle.PostStart.Exec.Command) > 0 {
			lastElemIndex := len(defaultPostStartCommand) - 1
			for i, item := range defaultPostStartCommand {
				if i < lastElemIndex {
					postStartCommand = append(postStartCommand, item)
				}
			}
			postStartCommandString := defaultPostStartCommand[lastElemIndex]
			lifecyclePoststartCommandString := strings.Join(lifecycle.PostStart.Exec.Command, " ")
			postStartCommand = append(postStartCommand, fmt.Sprintf("%s && %s", postStartCommandString, lifecyclePoststartCommandString))
		} else {
			postStartCommand = defaultPostStartCommand
		}
		dep.Spec.Template.Spec.Containers[0].Lifecycle.PostStart.Exec.Command = postStartCommand
	}
}

func portByName(ports []corev1.ContainerPort, name string) *corev1.ContainerPort {
	for i, port := range ports {
		if port.Name == name {
			return &ports[i]
		}
	}
	return nil
}

func setDefaultPorts(podSpec *v1alpha1.NginxPodTemplateSpec) {
	if portByName(podSpec.Ports, defaultHTTPPortName) == nil {
		httpPort := defaultHTTPPort
		if podSpec.HostNetwork {
			httpPort = defaultHTTPHostNetworkPort
		}
		podSpec.Ports = append(podSpec.Ports, corev1.ContainerPort{
			Name:          defaultHTTPPortName,
			ContainerPort: httpPort,
			Protocol:      corev1.ProtocolTCP,
		})
	}

	if portByName(podSpec.Ports, defaultHTTPSPortName) == nil {
		httpsPort := defaultHTTPSPort
		if podSpec.HostNetwork {
			httpsPort = defaultHTTPSHostNetworkPort
		}
		podSpec.Ports = append(podSpec.Ports, corev1.ContainerPort{
			Name:          defaultHTTPSPortName,
			ContainerPort: httpsPort,
			Protocol:      corev1.ProtocolTCP,
		})
	}
}

func setupProbes(nginxSpec v1alpha1.NginxSpec, dep *appv1.Deployment) {
	httpPort := portByName(nginxSpec.PodTemplate.Ports, defaultHTTPPortName)
	cmdTimeoutSec := int32(1)

	var commands []string
	if httpPort != nil {
		httpURL := fmt.Sprintf("http://localhost:%d%s", httpPort.ContainerPort, nginxSpec.HealthcheckPath)
		commands = append(commands, fmt.Sprintf(curlProbeCommand, cmdTimeoutSec, httpURL))
	}

	if nginxSpec.Certificates != nil {
		httpsPort := portByName(nginxSpec.PodTemplate.Ports, defaultHTTPSPortName)
		if httpsPort != nil {
			httpsURL := fmt.Sprintf("https://localhost:%d%s", httpsPort.ContainerPort, nginxSpec.HealthcheckPath)
			commands = append(commands, fmt.Sprintf(curlProbeCommand, cmdTimeoutSec, httpsURL))
		}
	}

	if len(commands) == 0 {
		return
	}

	dep.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
		TimeoutSeconds: cmdTimeoutSec * int32(len(commands)),
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"sh", "-c",
					strings.Join(commands, " && "),
				},
			},
		},
	}
}

func hasLowPort(ports []corev1.ContainerPort) bool {
	for _, port := range ports {
		if port.ContainerPort < 1024 {
			return true
		}
	}
	return false
}
