package k8s

import (
	"testing"

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

func baseDeployment() appv1.Deployment {
	return appv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-nginx-deployment",
			Namespace: "default",
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
									Name:          "http",
									ContainerPort: int32(80),
									Protocol:      corev1.ProtocolTCP,
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
						MountPath: "/etc/nginx",
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
						MountPath: "/etc/nginx",
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
			assertDeployment(t, &want, NewDeployment(&nginx))
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
