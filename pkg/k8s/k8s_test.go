// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package k8s

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tsuruConfig "github.com/tsuru/config"
	"github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	one int32 = 1
)

func baseNginx() v1alpha1.Nginx {
	return v1alpha1.Nginx{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-nginx",
			Namespace: "default",
		},
		Spec:   v1alpha1.NginxSpec{},
		Status: v1alpha1.NginxStatus{},
	}
}

func nginxWithService() v1alpha1.Nginx {
	n := baseNginx()
	n.Spec.Service = &v1alpha1.NginxService{
		Type: "LoadBalancer",
	}
	return n
}

func nginxWithCertificate() v1alpha1.Nginx {
	n := baseNginx()
	n.Spec.Certificates = &v1alpha1.TLSSecret{
		SecretName: "my-secret",
		Items: []v1alpha1.TLSSecretItem{
			{
				KeyField:         "key-field",
				KeyPath:          "key-path",
				CertificateField: "cert-field",
				CertificatePath:  "cert-path",
			},
		},
	}
	return n
}

func baseDeployment() appv1.Deployment {
	return appv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-nginx",
			Namespace:   "default",
			Annotations: make(map[string]string),
		},
		Spec: appv1.DeploymentSpec{
			Strategy: appv1.DeploymentStrategy{
				Type:          appv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appv1.RollingUpdateDeployment{},
			},
			Replicas: &one,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"nginx.tsuru.io/resource-name": "my-nginx",
					"nginx.tsuru.io/app":           "nginx",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "",
					Namespace: "default",
					Labels: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "nginx",
							Image:   "nginx:latest",
							Command: nginxEntrypoint,
							Ports: []corev1.ContainerPort{
								{
									Name:          defaultHTTPPortName,
									ContainerPort: defaultHTTPPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          defaultHTTPSPortName,
									ContainerPort: defaultHTTPSPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							ReadinessProbe: &corev1.Probe{
								TimeoutSeconds: int32(1),
								Handler: corev1.Handler{
									Exec: &corev1.ExecAction{
										Command: []string{"sh", "-c", "curl -m1 -kfsS -o /dev/null http://localhost:8080"},
									},
								},
							},
							Lifecycle: &corev1.Lifecycle{
								PostStart: &corev1.Handler{
									Exec: &corev1.ExecAction{
										Command: defaultPostStartCommand,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func Test_NewDeployment(t *testing.T) {
	tests := []struct {
		name       string
		nginxFn    func(n v1alpha1.Nginx) v1alpha1.Nginx
		deployFn   func(d appv1.Deployment) appv1.Deployment
		teardownFn func()
	}{
		{
			name: "empty-spec",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				return d
			},
		},
		{
			name: "multiple-replicas",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				v := int32(3)
				n.Spec.Replicas = &v
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				v := int32(3)
				d.Spec.Replicas = &v
				return d
			},
		},
		{
			name: "custom-image",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.Image = "tsuru/nginx:latest"
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].Image = "tsuru/nginx:latest"
				return d
			},
		},
		{
			name: "with-config-configmap",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.Config = &v1alpha1.ConfigRef{
					Kind: v1alpha1.ConfigKindConfigMap,
					Name: "config-map-xpto",
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "nginx-config",
						MountPath: "/etc/nginx/nginx.conf",
						SubPath:   "nginx.conf",
					},
				}
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "nginx-config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "config-map-xpto",
								},
							},
						},
					},
				}
				return d
			},
		},
		{
			name: "with-config-inline",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.Config = &v1alpha1.ConfigRef{
					Kind:  v1alpha1.ConfigKindInline,
					Name:  "config-inline",
					Value: "server {}",
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "nginx-config",
						MountPath: "/etc/nginx/nginx.conf",
						SubPath:   "nginx.conf",
					},
				}
				d.Spec.Template.Annotations = map[string]string{
					"config-inline": "server {}",
				}
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "nginx-config",
						VolumeSource: corev1.VolumeSource{
							DownwardAPI: &corev1.DownwardAPIVolumeSource{
								Items: []corev1.DownwardAPIVolumeFile{
									{
										Path: "nginx.conf",
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.annotations['config-inline']",
										},
									},
								},
							},
						},
					},
				}
				return d
			},
		},
		{
			name: "with-tls",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.Certificates = &v1alpha1.TLSSecret{
					SecretName: "my-secret",
					Items: []v1alpha1.TLSSecretItem{
						{
							CertificateField: "cert-field",
							CertificatePath:  "cert-path",
							KeyField:         "key-field",
							KeyPath:          "key-path",
						},
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          defaultHTTPPortName,
						ContainerPort: defaultHTTPPort,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          defaultHTTPSPortName,
						ContainerPort: defaultHTTPSPort,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{Name: "nginx-certs", MountPath: "/etc/nginx/certs"},
				}
				d.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					TimeoutSeconds: int32(2),
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"sh", "-c", "curl -m1 -kfsS -o /dev/null http://localhost:8080 && curl -m1 -kfsS -o /dev/null https://localhost:8443"},
						},
					},
				}
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "nginx-certs",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "my-secret",
								Items: []corev1.KeyToPath{
									{Key: "cert-field", Path: "cert-path"},
									{Key: "key-field", Path: "key-path"},
								},
							},
						},
					},
				}
				return d
			},
		},
		{
			name: "with-tls-default-values",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.Certificates = &v1alpha1.TLSSecret{
					SecretName: "my-secret",
					Items: []v1alpha1.TLSSecretItem{
						{
							CertificateField: "cert.crt",
							KeyField:         "cert.key",
						},
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          defaultHTTPPortName,
						ContainerPort: defaultHTTPPort,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          defaultHTTPSPortName,
						ContainerPort: defaultHTTPSPort,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{Name: "nginx-certs", MountPath: "/etc/nginx/certs"},
				}
				d.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					TimeoutSeconds: int32(2),
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"sh", "-c", "curl -m1 -kfsS -o /dev/null http://localhost:8080 && curl -m1 -kfsS -o /dev/null https://localhost:8443"},
						},
					},
				}
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "nginx-certs",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "my-secret",
								Items: []corev1.KeyToPath{
									{Key: "cert.crt", Path: "cert.crt"},
									{Key: "cert.key", Path: "cert.key"},
								},
							},
						},
					},
				}
				return d
			},
		},
		{
			name: "with-two-certificates",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.Certificates = &v1alpha1.TLSSecret{
					SecretName: "my-secret",
					Items: []v1alpha1.TLSSecretItem{
						{
							CertificateField: "rsa.crt.pem",
							KeyField:         "rsa.key.pem",
						},
						{
							CertificateField: "ecdsa.crt.pem",
							KeyField:         "ecdsa.key.pem",
						},
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          defaultHTTPPortName,
						ContainerPort: defaultHTTPPort,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          defaultHTTPSPortName,
						ContainerPort: defaultHTTPSPort,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{Name: "nginx-certs", MountPath: "/etc/nginx/certs"},
				}
				d.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					TimeoutSeconds: int32(2),
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"sh", "-c", "curl -m1 -kfsS -o /dev/null http://localhost:8080 && curl -m1 -kfsS -o /dev/null https://localhost:8443"},
						},
					},
				}
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "nginx-certs",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "my-secret",
								Items: []corev1.KeyToPath{
									{Key: "rsa.crt.pem", Path: "rsa.crt.pem"},
									{Key: "rsa.key.pem", Path: "rsa.key.pem"},
									{Key: "ecdsa.crt.pem", Path: "ecdsa.crt.pem"},
									{Key: "ecdsa.key.pem", Path: "ecdsa.key.pem"},
								},
							},
						},
					},
				}
				return d
			},
		},
		{
			name: "with-affinity",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.Affinity = &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "tsuru.io/pool", Values: []string{"my-pool"}},
								}},
							},
						},
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Affinity = &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "tsuru.io/pool", Values: []string{"my-pool"}},
								}},
							},
						},
					},
				}
				return d
			},
		},
		{
			name: "with-host-network",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.HostNetwork = true
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.HostNetwork = true
				d.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{
							"NET_BIND_SERVICE",
						},
					},
				}
				d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          defaultHTTPPortName,
						ContainerPort: defaultHTTPHostNetworkPort,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          defaultHTTPSPortName,
						ContainerPort: defaultHTTPSHostNetworkPort,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				one := intstr.FromInt(1)
				d.Spec.Strategy.RollingUpdate.MaxUnavailable = &one
				d.Spec.Strategy.RollingUpdate.MaxSurge = &one
				d.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					TimeoutSeconds: int32(1),
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"sh", "-c", "curl -m1 -kfsS -o /dev/null http://localhost:80"},
						},
					},
				}
				return d
			},
		},
		{
			name: "with-tls-and-host-network",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.HostNetwork = true
				n.Spec.Certificates = &v1alpha1.TLSSecret{
					SecretName: "my-secret",
					Items: []v1alpha1.TLSSecretItem{
						{
							CertificateField: "cert-field",
							CertificatePath:  "cert-path",
							KeyField:         "key-field",
							KeyPath:          "key-path",
						},
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.HostNetwork = true
				d.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{
							"NET_BIND_SERVICE",
						},
					},
				}
				d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          defaultHTTPPortName,
						ContainerPort: defaultHTTPHostNetworkPort,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          defaultHTTPSPortName,
						ContainerPort: defaultHTTPSHostNetworkPort,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				one := intstr.FromInt(1)
				d.Spec.Strategy.RollingUpdate.MaxUnavailable = &one
				d.Spec.Strategy.RollingUpdate.MaxSurge = &one
				d.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					TimeoutSeconds: int32(2),
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"sh", "-c", "curl -m1 -kfsS -o /dev/null http://localhost:80 && curl -m1 -kfsS -o /dev/null https://localhost:443"},
						},
					},
				}
				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{Name: "nginx-certs", MountPath: "/etc/nginx/certs"},
				}
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "nginx-certs",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "my-secret",
								Items: []corev1.KeyToPath{
									{Key: "cert-field", Path: "cert-path"},
									{Key: "key-field", Path: "key-path"},
								},
							},
						},
					},
				}
				return d
			},
		},
		{
			name: "with-security-context",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.SecurityContext = &corev1.SecurityContext{
					RunAsUser:  new(int64),
					RunAsGroup: new(int64),
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
					RunAsUser:  new(int64),
					RunAsGroup: new(int64),
				}
				return d
			},
		},
		{
			name: "with-security-context-and-host-network",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate = v1alpha1.NginxPodTemplateSpec{
					HostNetwork: true,
				}
				n.Spec.PodTemplate.SecurityContext = &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{corev1.Capability("all")},
						Add:  []corev1.Capability{corev1.Capability("NET_ADMIN")},
					},
					RunAsUser:  func(n int64) *int64 { return &n }(int64(100)),
					RunAsGroup: func(n int64) *int64 { return &n }(int64(100)),
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.HostNetwork = true
				d.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{
							"all",
						},
						Add: []corev1.Capability{
							"NET_ADMIN",
							"NET_BIND_SERVICE",
						},
					},
					RunAsUser:  func(n int64) *int64 { return &n }(int64(100)),
					RunAsGroup: func(n int64) *int64 { return &n }(int64(100)),
				}
				one := intstr.FromInt(1)
				d.Spec.Strategy.RollingUpdate.MaxUnavailable = &one
				d.Spec.Strategy.RollingUpdate.MaxSurge = &one
				d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          defaultHTTPPortName,
						ContainerPort: defaultHTTPHostNetworkPort,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          defaultHTTPSPortName,
						ContainerPort: defaultHTTPSHostNetworkPort,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				d.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					TimeoutSeconds: int32(1),
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"sh", "-c", "curl -m1 -kfsS -o /dev/null http://localhost:80"},
						},
					},
				}

				return d
			},
		},
		{
			name: "with-resources",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.Resources = corev1.ResourceRequirements{
					Limits: corev1.ResourceList(map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceMemory: *resource.NewQuantity(int64(100), resource.DecimalSI),
					}),
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Limits: corev1.ResourceList(map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceMemory: *resource.NewQuantity(int64(100), resource.DecimalSI),
					}),
				}
				return d
			},
		},
		{
			name: "with-extra-files",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.ExtraFiles = &v1alpha1.FilesRef{
					Name: "my-extra-files-in-configmap",
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
					Name:      "nginx-extra-files",
					MountPath: "/etc/nginx/extra_files",
				})
				d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
					Name: "nginx-extra-files",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "my-extra-files-in-configmap",
							},
						},
					},
				})
				return d
			},
		},
		{
			name: "with-extra-files-and-files-mounted-on-custom-subdirs",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.ExtraFiles = &v1alpha1.FilesRef{
					Name: "my-extra-files-in-configmap",
					Files: map[string]string{
						"www_index.html":     "www/index.html",
						"another-nginx.cnf":  "another-nginx.cnf",
						"waf_sqli-rules.cnf": "waf/sqli-rules.cnf",
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
					Name:      "nginx-extra-files",
					MountPath: "/etc/nginx/extra_files",
				})
				d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
					Name: "nginx-extra-files",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "my-extra-files-in-configmap",
							},
							Items: []corev1.KeyToPath{
								{
									Key:  "another-nginx.cnf",
									Path: "another-nginx.cnf",
								},
								{
									Key:  "waf_sqli-rules.cnf",
									Path: "waf/sqli-rules.cnf",
								},
								{
									Key:  "www_index.html",
									Path: "www/index.html",
								},
							},
						},
					},
				})
				return d
			},
		},
		{
			name: "when adding extra labels to PodTemplate",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.Labels = map[string]string{
					"some-custom-label": "label-value",
					"project":           "z",
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Labels = mergeMap(map[string]string{
					"some-custom-label": "label-value",
					"project":           "z",
				}, d.Spec.Template.Labels)
				return d
			},
		},
		{
			name: "when adding extra annotations to PodTemplate",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.Annotations = map[string]string{
					"tsuru.io/pool":        "some-pool",
					"tsuru.io/another-key": "another-value",
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Annotations = map[string]string{
					"tsuru.io/pool":        "some-pool",
					"tsuru.io/another-key": "another-value",
				}
				return d
			},
		},
		{
			name: "when nginx controller has custom annotations",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				tsuruConfig.Set("nginx-controller:pod-template:annotations", map[interface{}]interface{}{
					"nginx.tsuru.io/some-key": "some value",
				})
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Annotations = map[string]string{
					"nginx.tsuru.io/some-key": "some value",
				}
				return d
			},
			teardownFn: func() {
				tsuruConfig.Unset("nginx-controller:pod-template:annotations")
			},
		},
		{
			name: "when nginx resource has both controller and user annotations",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				tsuruConfig.Set("nginx-controller:pod-template:annotations", map[interface{}]interface{}{
					"nginx.tsuru.io/some-key":       "some value",
					"nginx.tsuru.io/conflicted-key": "controller value",
				})
				n.Spec.PodTemplate.Annotations = map[string]string{
					"some-user-annotation":          "value",
					"nginx.tsuru.io/conflicted-key": "user wins",
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Annotations = map[string]string{
					"nginx.tsuru.io/some-key":       "some value",
					"some-user-annotation":          "value",
					"nginx.tsuru.io/conflicted-key": "user wins",
				}
				return d
			},
			teardownFn: func() {
				tsuruConfig.Unset("nginx-controller:pod-template:annotations")
			},
		},
		{
			name: "when nginx controller has custom labels",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				tsuruConfig.Set("nginx-controller:pod-template:labels", map[interface{}]interface{}{
					"nginx_custom_label": "custom label",
				})
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				expectedLabels := mergeMap(d.Spec.Template.Labels, map[string]string{"nginx_custom_label": "custom label"})
				d.Spec.Template.Labels = expectedLabels
				return d
			},
			teardownFn: func() {
				tsuruConfig.Unset("nginx-controller:pod-template:labels")
			},
		},
		{
			name: "when nginx resource has both controller and user labels",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				tsuruConfig.Set("nginx-controller:pod-template:labels", map[interface{}]interface{}{
					"nginx_custom_label": "custom label",
					"conflicted_label":   "controller value",
				})
				n.Spec.PodTemplate.Labels = map[string]string{
					"user_custom_label": "custom value",
					"conflicted_label":  "user wins",
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				expectedLabels := mergeMap(d.Spec.Template.Labels, map[string]string{
					"nginx_custom_label": "custom label",
					"user_custom_label":  "custom value",
					"conflicted_label":   "user wins",
				})
				d.Spec.Template.Labels = expectedLabels
				return d
			},
			teardownFn: func() {
				tsuruConfig.Unset("nginx-controller:pod-template:labels")
			},
		},
		{
			name: "with-cache",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.Cache = v1alpha1.NginxCacheSpec{
					Path: "/var/cache",
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
					Name:      "cache-vol",
					MountPath: "/var/cache",
				})
				d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
					Name: "cache-vol",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				})
				return d
			},
		},
		{
			name: "with-cache-memory-size",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				q, err := resource.ParseQuantity("10Mi")
				require.NoError(t, err)
				n.Spec.Cache = v1alpha1.NginxCacheSpec{
					InMemory: true,
					Path:     "/var/cache",
					Size:     &q,
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
					Name:      "cache-vol",
					MountPath: "/var/cache",
				})
				q, err := resource.ParseQuantity("10Mi")
				require.NoError(t, err)
				d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
					Name: "cache-vol",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							Medium:    "Memory",
							SizeLimit: &q,
						},
					},
				})
				return d
			},
		},
		{
			name: "with-lifecycle-pre-stop",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.Lifecycle = &v1alpha1.NginxLifecycle{
					PreStop: &v1alpha1.NginxLifecycleHandler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"echo",
								"hello world",
							},
						},
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{
					PreStop: &corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"echo",
								"hello world",
							},
						},
					},
					PostStart: &corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"/bin/sh",
								"-c",
								"nginx -t && touch /tmp/done",
							},
						},
					},
				}
				return d
			},
		},
		{
			name: "with-lifecycle-poststart-exec",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.Lifecycle = &v1alpha1.NginxLifecycle{
					PreStop: &v1alpha1.NginxLifecycleHandler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"echo",
								"hello world",
							},
						},
					},
					PostStart: &v1alpha1.NginxLifecycleHandler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"echo",
								"hello world",
							},
						},
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{
					PreStop: &corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"echo",
								"hello world",
							},
						},
					},
					PostStart: &corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"/bin/sh",
								"-c",
								"nginx -t && touch /tmp/done && echo hello world",
							},
						},
					},
				}
				return d
			},
		},
		{
			name: "with-lifecycle-poststart-exec-empty-command",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.Lifecycle = &v1alpha1.NginxLifecycle{
					PostStart: &v1alpha1.NginxLifecycleHandler{
						Exec: &corev1.ExecAction{
							Command: []string{},
						},
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{
					PostStart: &corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"/bin/sh",
								"-c",
								"nginx -t && touch /tmp/done",
							},
						},
					},
				}
				return d
			},
		},
		{
			name: "with custom termination graceful period",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.TerminationGracePeriodSeconds = func(n int64) *int64 { return &n }(int64(60 * 2))
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.TerminationGracePeriodSeconds = func(n int64) *int64 { return &n }(int64(60 * 2))
				return d
			},
		},
		{
			name: "with custom ports",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.Ports = []corev1.ContainerPort{
					{
						Name:          "http",
						ContainerPort: 20001,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          "https",
						ContainerPort: 20002,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          defaultHTTPPortName,
						ContainerPort: 20001,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          defaultHTTPSPortName,
						ContainerPort: 20002,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				d.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					TimeoutSeconds: int32(1),
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"sh", "-c", "curl -m1 -kfsS -o /dev/null http://localhost:20001"},
						},
					},
				}
				return d
			},
		},
		{
			name: "with low port",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.HostNetwork = true
				n.Spec.PodTemplate.Ports = []corev1.ContainerPort{
					{
						Name:          "http",
						ContainerPort: 80,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          "https",
						ContainerPort: 443,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.HostNetwork = true
				d.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{
							"NET_BIND_SERVICE",
						},
					},
				}
				d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          defaultHTTPPortName,
						ContainerPort: 80,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          defaultHTTPSPortName,
						ContainerPort: 443,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				one := intstr.FromInt(1)
				d.Spec.Strategy.RollingUpdate.MaxUnavailable = &one
				d.Spec.Strategy.RollingUpdate.MaxSurge = &one
				d.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					TimeoutSeconds: int32(1),
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"sh", "-c", "curl -m1 -kfsS -o /dev/null http://localhost:80"},
						},
					},
				}
				return d
			},
		},
		{
			name: "with host network and custom ports",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.HostNetwork = true
				n.Spec.PodTemplate.Ports = []corev1.ContainerPort{
					{
						Name:          "http",
						ContainerPort: 20001,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          "https",
						ContainerPort: 20002,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.HostNetwork = true
				d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          defaultHTTPPortName,
						ContainerPort: 20001,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          defaultHTTPSPortName,
						ContainerPort: 20002,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				one := intstr.FromInt(1)
				d.Spec.Strategy.RollingUpdate.MaxUnavailable = &one
				d.Spec.Strategy.RollingUpdate.MaxSurge = &one
				d.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					TimeoutSeconds: int32(1),
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"sh", "-c", "curl -m1 -kfsS -o /dev/null http://localhost:20001"},
						},
					},
				}
				return d
			},
		},
		{
			name: "with tls and custom ports",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.Ports = []corev1.ContainerPort{
					{
						Name:          "http",
						ContainerPort: 20001,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          "https",
						ContainerPort: 20002,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				n.Spec.Certificates = &v1alpha1.TLSSecret{
					SecretName: "my-secret",
					Items: []v1alpha1.TLSSecretItem{
						{
							CertificateField: "cert.crt",
							KeyField:         "cert.key",
						},
					},
				}
				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          defaultHTTPPortName,
						ContainerPort: 20001,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          defaultHTTPSPortName,
						ContainerPort: 20002,
						Protocol:      corev1.ProtocolTCP,
					},
				}
				d.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					TimeoutSeconds: int32(2),
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"sh", "-c", "curl -m1 -kfsS -o /dev/null http://localhost:20001 && curl -m1 -kfsS -o /dev/null https://localhost:20002"},
						},
					},
				}
				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{Name: "nginx-certs", MountPath: "/etc/nginx/certs"},
				}
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "nginx-certs",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "my-secret",
								Items: []corev1.KeyToPath{
									{Key: "cert.crt", Path: "cert.crt"},
									{Key: "cert.key", Path: "cert.key"},
								},
							},
						},
					},
				}
				return d
			},
		},
		{
			name: "with volumes",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.Volumes = []corev1.Volume{
					{
						Name: "test",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "my-test-claim",
							},
						},
					},
				}
				n.Spec.PodTemplate.VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "test",
						MountPath: "/tmp/test",
					},
				}

				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
					Name:      "test",
					MountPath: "/tmp/test",
				})
				d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
					Name: "test",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "my-test-claim",
						},
					},
				})
				return d
			},
		},
		{
			name: "with initContainers",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.InitContainers = []corev1.Container{
					{
						Name:  "preheat-cache",
						Image: "tsuru/preheat-cache:3321",
						Args:  []string{"-c", "rsync /tmp /blah"},
					},
				}

				return n
			},
			deployFn: func(d appv1.Deployment) appv1.Deployment {
				d.Spec.Template.Spec.InitContainers = append(d.Spec.Template.Spec.InitContainers, corev1.Container{
					Name:  "preheat-cache",
					Image: "tsuru/preheat-cache:3321",
					Args:  []string{"-c", "rsync /tmp /blah"},
				})
				return d
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nginx := tt.nginxFn(baseNginx())
			want := tt.deployFn(baseDeployment())
			want.OwnerReferences = []metav1.OwnerReference{
				*metav1.NewControllerRef(&nginx, schema.GroupVersionKind{
					Group:   v1alpha1.SchemeGroupVersion.Group,
					Version: v1alpha1.SchemeGroupVersion.Version,
					Kind:    "Nginx",
				}),
			}
			dep, err := NewDeployment(&nginx)
			assert.NoError(t, err)
			spec, err := json.Marshal(nginx.Spec)
			assert.NoError(t, err)
			want.Annotations[generatedFromAnnotation] = string(spec)
			assertDeployment(t, &want, dep)
			if tt.teardownFn != nil {
				tt.teardownFn()
			}
		})
	}
}

func assertDeployment(t *testing.T, want, got *appv1.Deployment) {
	assert.Equal(t, want.TypeMeta, got.TypeMeta)
	assert.Equal(t, want.ObjectMeta, got.ObjectMeta)
	assert.Equal(t, want.Spec, got.Spec)
	assert.Equal(t, want, got)
}

func TestNewService(t *testing.T) {
	tests := []struct {
		name  string
		nginx v1alpha1.Nginx
		want  *corev1.Service
	}{
		{
			name:  "base",
			nginx: baseNginx(),
			want: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx-service",
					Namespace: "default",
					Labels: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("http"),
							Port:       int32(80),
						},
						{
							Name:       "https",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("https"),
							Port:       int32(443),
						},
					},
					Selector: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
					},
					Type: corev1.ServiceTypeClusterIP,
				},
			},
		},
		{
			name:  "with-service",
			nginx: nginxWithService(),
			want: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx-service",
					Namespace: "default",
					Labels: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("http"),
							Port:       int32(80),
						},
						{
							Name:       "https",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("https"),
							Port:       int32(443),
						},
					},
					Selector: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
					},
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
		},
		{
			name:  "with-tls",
			nginx: nginxWithCertificate(),
			want: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx-service",
					Namespace: "default",
					Labels: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("http"),
							Port:       int32(80),
						},
						{
							Name:       "https",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("https"),
							Port:       int32(443),
						},
					},
					Type: corev1.ServiceTypeClusterIP,
					Selector: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
					},
				},
			},
		},
		{
			name: "with-service-extra",
			nginx: func() v1alpha1.Nginx {
				n := nginxWithService()
				n.Spec.Service.LoadBalancerIP = "10.0.0.1"
				n.Spec.Service.Labels = map[string]string{
					"x":                  "y",
					"nginx.tsuru.io/app": "ignored",
				}
				n.Spec.Service.Annotations = map[string]string{
					"a": "b",
				}
				return n
			}(),
			want: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx-service",
					Namespace: "default",
					Labels: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
						"x":                            "y",
					},
					Annotations: map[string]string{
						"a": "b",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("http"),
							Port:       int32(80),
						},
						{
							Name:       "https",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("https"),
							Port:       int32(443),
						},
					},
					Selector: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
					},
					Type:           corev1.ServiceTypeLoadBalancer,
					LoadBalancerIP: "10.0.0.1",
				},
			},
		},
		{
			name: "using Local externalTrafficPolicy",
			nginx: func() v1alpha1.Nginx {
				n := nginxWithService()
				n.Spec.Service.Type = corev1.ServiceTypeLoadBalancer
				n.Spec.Service.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
				return n
			}(),
			want: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx-service",
					Namespace: "default",
					Labels: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("http"),
							Port:       int32(80),
						},
						{
							Name:       "https",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("https"),
							Port:       int32(443),
						},
					},
					Selector: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
					},
					Type:                  corev1.ServiceTypeLoadBalancer,
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
				},
			},
		},
		{
			name: "without pod selector",
			nginx: func() v1alpha1.Nginx {
				n := nginxWithService()
				n.Spec.Service.UsePodSelector = func(b bool) *bool { return &b }(false)
				return n
			}(),
			want: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx-service",
					Namespace: "default",
					Labels: map[string]string{
						"nginx.tsuru.io/resource-name": "my-nginx",
						"nginx.tsuru.io/app":           "nginx",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("http"),
							Port:       int32(80),
						},
						{
							Name:       "https",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("https"),
							Port:       int32(443),
						},
					},
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.want.OwnerReferences = []metav1.OwnerReference{
				*metav1.NewControllerRef(&tt.nginx, schema.GroupVersionKind{
					Group:   v1alpha1.SchemeGroupVersion.Group,
					Version: v1alpha1.SchemeGroupVersion.Version,
					Kind:    "Nginx",
				}),
			}
			assert.Equal(t, tt.want, NewService(&tt.nginx))
		})
	}
}

func TestExtractNginxSpec(t *testing.T) {
	mustMarshal := func(t *testing.T, n v1alpha1.NginxSpec) string {
		data, err := json.Marshal(n)
		assert.Nil(t, err)
		return string(data)
	}
	tests := []struct {
		name        string
		annotations map[string]string
		want        v1alpha1.NginxSpec
		wantedErr   string
	}{
		{
			name:      "missing-annotation",
			want:      v1alpha1.NginxSpec{},
			wantedErr: `missing "nginx.tsuru.io/generated-from" annotation in deployment`,
		},
		{
			name: "default",
			annotations: map[string]string{
				generatedFromAnnotation: mustMarshal(t, v1alpha1.NginxSpec{
					Image: "custom-image",
				}),
			},
			want:      v1alpha1.NginxSpec{Image: "custom-image"},
			wantedErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := metav1.ObjectMeta{
				Annotations: tt.annotations,
			}
			got, err := ExtractNginxSpec(o)
			if tt.wantedErr != "" {
				assert.EqualError(t, err, tt.wantedErr)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
