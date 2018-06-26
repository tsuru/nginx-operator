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

func Test_NewDeployment(t *testing.T) {
	baseNginx := v1alpha1.Nginx{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-nginx",
			Namespace: "default",
		},
		Spec:   v1alpha1.NginxSpec{},
		Status: v1alpha1.NginxStatus{},
	}
	baseDeploy := appv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-nginx-deployment",
			Namespace: "default",
		},
		Spec: appv1.DeploymentSpec{
			Replicas: nil,
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
					OwnerReferences: nil,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "busybox",
							Image:   "busybox",
							Command: []string{"sleep", "3600"},
						},
					},
					RestartPolicy: "",
				},
			},
		},
	}
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nginx := tt.nginxFn(baseNginx)
			want := tt.deployFn(baseDeploy)
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
