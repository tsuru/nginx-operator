package k8s

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/stretchr/testify/assert"
	"github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
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

func nginxWithTLSSecret() v1alpha1.Nginx {
	n := baseNginx()
	n.Spec.TLSSecret = &v1alpha1.TLSSecret{
		SecretName:       "my-secret",
		KeyField:         "key-field",
		KeyPath:          "key-path",
		CertificateField: "cert-field",
		CertificatePath:  "cert-path",
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
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"nginx_cr": "my-nginx",
					"app":      "nginx",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "",
					Namespace: "default",
					Labels: map[string]string{
						"nginx_cr": "my-nginx",
						"app":      "nginx",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
							Ports: []corev1.ContainerPort{
								{
									Name:          defaultHTTPPortName,
									ContainerPort: defaultHTTPPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							ReadinessProbe: &corev1.Probe{
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/",
										Port:   intstr.FromString(defaultHTTPPortName),
										Scheme: corev1.URISchemeHTTP,
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
		name     string
		nginxFn  func(n v1alpha1.Nginx) v1alpha1.Nginx
		deployFn func(d appv1.Deployment) appv1.Deployment
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
				n.Spec.TLSSecret = &v1alpha1.TLSSecret{
					SecretName:       "my-secret",
					KeyField:         "key-field",
					KeyPath:          "key-path",
					CertificateField: "cert-field",
					CertificatePath:  "cert-path",
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
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/",
							Port:   intstr.FromString(defaultHTTPSPortName),
							Scheme: corev1.URISchemeHTTPS,
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
									{Key: "key-field", Path: "key-path"},
									{Key: "cert-field", Path: "cert-path"},
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
				n.Spec.TLSSecret = &v1alpha1.TLSSecret{
					SecretName: "my-secret",
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
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/",
							Port:   intstr.FromString(defaultHTTPSPortName),
							Scheme: corev1.URISchemeHTTPS,
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
									{Key: "tls.key", Path: "tls.key"},
									{Key: "tls.crt", Path: "tls.crt"},
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
			name: "with-resources",
			nginxFn: func(n v1alpha1.Nginx) v1alpha1.Nginx {
				n.Spec.PodTemplate.Resources = corev1.ResourceRequirements{
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
						"nginx_cr": "my-nginx",
						"app":      "nginx",
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
					},
					Selector: map[string]string{
						"nginx_cr": "my-nginx",
						"app":      "nginx",
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
						"nginx_cr": "my-nginx",
						"app":      "nginx",
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
					},
					Selector: map[string]string{
						"nginx_cr": "my-nginx",
						"app":      "nginx",
					},
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
		},
		{
			name:  "with-tls",
			nginx: nginxWithTLSSecret(),
			want: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx-service",
					Namespace: "default",
					Labels: map[string]string{
						"nginx_cr": "my-nginx",
						"app":      "nginx",
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
						"nginx_cr": "my-nginx",
						"app":      "nginx",
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
					"x":   "y",
					"app": "ignored",
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
						"nginx_cr": "my-nginx",
						"app":      "nginx",
						"x":        "y",
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
					},
					Selector: map[string]string{
						"nginx_cr": "my-nginx",
						"app":      "nginx",
					},
					Type:           corev1.ServiceTypeLoadBalancer,
					LoadBalancerIP: "10.0.0.1",
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
