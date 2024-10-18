// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package k8s

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/tsuru/nginx-operator/api/v1alpha1"
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

	defaultProxyProtocolHTTPPortName  = "proxy-http"
	defaultProxyProtocolHTTPSPortName = "proxy-https"

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
	"while ! [ -f /tmp/done ]; do [ -f /tmp/error ] && cat /tmp/error >&2; sleep 0.5; done && exec nginx -g 'daemon off;'",
}

var defaultPostStartCommand = []string{
	"/bin/sh",
	"-c",
	"nginx -t | tee /tmp/error && touch /tmp/done",
}

// NewDeployment creates a deployment for a given Nginx resource.
func NewDeployment(n *v1alpha1.Nginx) (*appv1.Deployment, error) {
	n.Spec.Image = valueOrDefault(n.Spec.Image, defaultNginxImage)
	setDefaultPorts(&n.Spec.PodTemplate)

	containerSecurityContext := n.Spec.PodTemplate.ContainerSecurityContext

	if hasLowPort(n.Spec.PodTemplate.Ports) {
		if containerSecurityContext == nil {
			containerSecurityContext = &corev1.SecurityContext{}
		}
		if containerSecurityContext.Capabilities == nil {
			containerSecurityContext.Capabilities = &corev1.Capabilities{}
		}
		containerSecurityContext.Capabilities.Add = append(containerSecurityContext.Capabilities.Add, "NET_BIND_SERVICE")
	}

	var maxSurge, maxUnavailable *intstr.IntOrString
	if n.Spec.PodTemplate.HostNetwork {
		// Round up instead of down as is the default behavior for maxUnvailable,
		// this is useful because we must allow at least one pod down for
		// hostNetwork deployments.
		r := int32(1)
		if n.Spec.Replicas != nil && *n.Spec.Replicas > int32(0) {
			r = *n.Spec.Replicas
		}

		adjustedValue := intstr.FromInt(int(math.Ceil(float64(r) * 0.25)))
		maxUnavailable = &adjustedValue
		maxSurge = &adjustedValue
	}

	if ru := n.Spec.PodTemplate.RollingUpdate; ru != nil {
		maxSurge, maxUnavailable = ru.MaxSurge, ru.MaxUnavailable
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
					Group:   v1alpha1.GroupVersion.Group,
					Version: v1alpha1.GroupVersion.Version,
					Kind:    "Nginx",
				}),
			},
			Labels: LabelsForNginx(n.Name),
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
					Annotations: n.Spec.PodTemplate.Annotations,
					Labels:      mergeMap(LabelsForNginx(n.Name), n.Spec.PodTemplate.Labels),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: n.Spec.PodTemplate.ServiceAccountName,
					EnableServiceLinks: func(b bool) *bool { return &b }(false),
					Containers: append([]corev1.Container{
						{
							Name:            "nginx",
							Image:           n.Spec.Image,
							Command:         nginxEntrypoint,
							Resources:       n.Spec.Resources,
							SecurityContext: containerSecurityContext,
							Ports:           n.Spec.PodTemplate.Ports,
							VolumeMounts:    n.Spec.PodTemplate.VolumeMounts,
						},
					}, n.Spec.PodTemplate.Containers...),
					InitContainers:                n.Spec.PodTemplate.InitContainers,
					Affinity:                      n.Spec.PodTemplate.Affinity,
					NodeSelector:                  n.Spec.PodTemplate.NodeSelector,
					HostNetwork:                   n.Spec.PodTemplate.HostNetwork,
					TerminationGracePeriodSeconds: n.Spec.PodTemplate.TerminationGracePeriodSeconds,
					Volumes:                       n.Spec.PodTemplate.Volumes,
					Tolerations:                   n.Spec.PodTemplate.Toleration,
					TopologySpreadConstraints:     n.Spec.PodTemplate.TopologySpreadConstraints,
					SecurityContext:               n.Spec.PodTemplate.PodSecurityContext,
				},
			},
		},
	}
	setupProbes(n.Spec, &deployment)
	setupConfig(n.Spec.Config, &deployment)
	setupTLS(n.Spec.TLS, &deployment)
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
	annotations := map[string]string{}
	labels := map[string]string{}

	var lbIP string
	var externalTrafficPolicy corev1.ServiceExternalTrafficPolicyType
	labelSelector := LabelsForNginx(n.Name)

	if n.Spec.Service != nil {
		labels = n.Spec.Service.Labels

		if n.Spec.Service.Annotations != nil {
			annotations = n.Spec.Service.Annotations
		}
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
					Group:   v1alpha1.GroupVersion.Group,
					Version: v1alpha1.GroupVersion.Version,
					Kind:    "Nginx",
				}),
			},
			Labels:      mergeMap(labels, LabelsForNginx(n.Name)),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports:                 fillPorts(n, nginxService(n)),
			Selector:              labelSelector,
			LoadBalancerIP:        lbIP,
			Type:                  nginxService(n),
			ExternalTrafficPolicy: externalTrafficPolicy,
		},
	}

	if service.Spec.Type == corev1.ServiceTypeClusterIP {
		service.Spec.ExternalTrafficPolicy = ""
	}
	return &service
}

func fillPorts(n *v1alpha1.Nginx, t corev1.ServiceType) []corev1.ServicePort {
	if n.Spec.PodTemplate.Ports != nil && t == corev1.ServiceTypeLoadBalancer {
		ports := make([]corev1.ServicePort, 0)
		for _, port := range n.Spec.PodTemplate.Ports {
			if port.Name == defaultProxyProtocolHTTPPortName {
				ports = append(ports, corev1.ServicePort{
					Name:       defaultProxyProtocolHTTPPortName,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString(defaultProxyProtocolHTTPPortName),
					Port:       int32(80),
				})
			}
			if port.Name == defaultProxyProtocolHTTPSPortName {
				ports = append(ports, corev1.ServicePort{
					Name:       defaultProxyProtocolHTTPSPortName,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString(defaultProxyProtocolHTTPSPortName),
					Port:       int32(443),
				})
			}
		}
		if len(ports) > 0 {
			return ports
		}
	}
	return []corev1.ServicePort{
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
	}
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

func GetNginxNameFromObject(o client.Object) string {
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

func NewIngress(nginx *v1alpha1.Nginx) *networkingv1.Ingress {
	labels := LabelsForNginx(nginx.Name)
	if nginx.Spec.Ingress != nil {
		labels = mergeMap(nginx.Spec.Ingress.Labels, labels)
	}

	var annotations map[string]string
	if nginx.Spec.Ingress != nil {
		annotations = mergeMap(nginx.Spec.Ingress.Annotations, annotations)
	}

	var ingressClass *string
	if nginx.Spec.Ingress != nil {
		ingressClass = nginx.Spec.Ingress.IngressClassName
	}

	var rules []networkingv1.IngressRule
	var tls []networkingv1.IngressTLS
	serviceName := fmt.Sprintf("%s-service", nginx.Name)

	for _, t := range nginx.Spec.TLS {
		hosts := t.Hosts
		if len(hosts) == 0 {
			// NOTE: making sure a wildcard HTTP rule is going to be set whenever the
			// TLS certificates doesn't specify any hostname.
			hosts = []string{""}
		}

		for _, host := range hosts {
			rules = append(rules, networkingv1.IngressRule{
				Host: host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path:     "/",
								PathType: func(pt networkingv1.PathType) *networkingv1.PathType { return &pt }(networkingv1.PathTypePrefix),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: serviceName,
										Port: networkingv1.ServiceBackendPort{
											Name: defaultHTTPPortName,
										},
									},
								},
							},
						},
					},
				},
			})
		}

		tls = append(tls, networkingv1.IngressTLS{
			SecretName: t.SecretName,
			Hosts:      t.Hosts,
		})
	}

	return &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        nginx.Name,
			Namespace:   nginx.Namespace,
			Annotations: annotations,
			Labels:      labels,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(nginx, schema.GroupVersionKind{
					Group:   v1alpha1.GroupVersion.Group,
					Version: v1alpha1.GroupVersion.Version,
					Kind:    "Nginx",
				}),
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ingressClass,
			Rules:            rules,
			TLS:              tls,
			DefaultBackend: &networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: serviceName,
					Port: networkingv1.ServiceBackendPort{
						Name: defaultHTTPPortName,
					},
				},
			},
		},
	}
}

func setupConfig(conf *v1alpha1.ConfigRef, dep *appv1.Deployment) {
	if conf == nil {
		return
	}

	volumeName := "nginx-config"

	dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      volumeName,
		MountPath: fmt.Sprintf("%s/%s", configMountPath, configFileName),
		SubPath:   configFileName,
		ReadOnly:  true,
	})

	switch conf.Kind {
	case v1alpha1.ConfigKindConfigMap:
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: conf.Name,
					},
					Optional: func(b bool) *bool { return &b }(false),
				},
			},
		})

	case v1alpha1.ConfigKindInline:
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = make(map[string]string)
		}

		key := "nginx.tsuru.io/custom-nginx-config"
		dep.Spec.Template.Annotations[key] = conf.Value

		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				DownwardAPI: &corev1.DownwardAPIVolumeSource{
					Items: []corev1.DownwardAPIVolumeFile{
						{
							Path: "nginx.conf",
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: fmt.Sprintf("metadata.annotations['%s']", key),
							},
						},
					},
				},
			},
		})
	}
}

// setupTLS configures the Secret volumes and attaches them in the nginx container.
func setupTLS(tls []v1alpha1.NginxTLS, dep *appv1.Deployment) {
	for index, t := range tls {
		volumeName := fmt.Sprintf("nginx-certs-%d", index)

		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: t.SecretName,
					Optional:   func(b bool) *bool { return &b }(false),
				},
			},
		})

		dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: filepath.Join(certMountPath, t.SecretName),
			ReadOnly:  true,
		})
	}
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
		PostStart: &corev1.LifecycleHandler{
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
		dep.Spec.Template.Spec.Containers[0].Lifecycle.PreStop = &corev1.LifecycleHandler{Exec: lifecycle.PreStop.Exec}
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

	if len(nginxSpec.TLS) > 0 {
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
		ProbeHandler: corev1.ProbeHandler{
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
