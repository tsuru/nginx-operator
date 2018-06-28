package k8s

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const defaultNginxImage = "nginx:latest"

// NewDeployment creates a deployment for a given Nginx resource.
func NewDeployment(n *v1alpha1.Nginx) *appv1.Deployment {
	image := n.Spec.Image
	if image == "" {
		image = defaultNginxImage
	}
	deployment := appv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.Name + "-deployment",
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
			Replicas: n.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: LabelsForNginx(n.Name),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: n.Namespace,
					Labels:    LabelsForNginx(n.Name),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: image,
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
	setupConfig(n.Spec.Config, &deployment)
	return &deployment
}

// NewService assembles the ClusterIP service for the Nginx
func NewService(n *v1alpha1.Nginx) *corev1.Service {
	return &corev1.Service{
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
			Selector: LabelsForNginx(n.Name),
			Type:     corev1.ServiceTypeClusterIP,
		},
	}
}

// LabelsForNginx returns the labels for a Nginx CR with the given name
func LabelsForNginx(name string) map[string]string {
	return map[string]string{
		"nginx_cr": name,
		"app":      "nginx",
	}
}

func setupConfig(conf *v1alpha1.ConfigRef, dep *appv1.Deployment) {
	if conf == nil {
		return
	}
	dep.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{
			Name:      "nginx-config",
			MountPath: "/etc/nginx",
		},
	}
	switch conf.Kind {
	case v1alpha1.ConfigKindConfigMap:
		dep.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "nginx-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: conf.Name,
						},
					},
				},
			},
		}
	case v1alpha1.ConfigKindInline:
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = make(map[string]string)
		}
		dep.Spec.Template.Annotations[conf.Name] = conf.Value
		dep.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
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
			},
		}
	}
}
