package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
					"nginx": "my-nginx",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "",
					Namespace: "default",
					Labels: map[string]string{
						"nginx": "my-nginx",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
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
