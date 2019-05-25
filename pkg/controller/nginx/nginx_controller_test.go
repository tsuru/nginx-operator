package nginx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsuru/nginx-operator/pkg/apis"
	"github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileNginx_reconcileService(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	apis.AddToScheme(scheme)

	tests := []struct {
		name      string
		nginx     *v1alpha1.Nginx
		service   *corev1.Service
		assertion func(t *testing.T, err error, got *corev1.Service)
	}{
		{
			name: "when service doesn't exist yet, should create that and use an auto-allocated nodeport (tcp/0)",
			nginx: &v1alpha1.Nginx{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "extensions.tsuru.io/v1alpha1",
					Kind:       "Nginx",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx",
					Namespace: "default",
				},
				Spec:   v1alpha1.NginxSpec{},
				Status: v1alpha1.NginxStatus{},
			},
			assertion: func(t *testing.T, err error, got *corev1.Service) {
				assert.NoError(t, err)
				assert.NotNil(t, got)
				expectedPorts := []corev1.ServicePort{
					corev1.ServicePort{
						Name:       "http",
						TargetPort: intstr.FromString("http"),
						Protocol:   corev1.ProtocolTCP,
						NodePort:   int32(0),
						Port:       int32(80),
					},
				}
				assert.Equal(t, expectedPorts, got.Spec.Ports)
			},
		},
		{
			name: "when setup an https port to the service, should use an auto-allocated nodeport on https and preserve its http nodeport",
			nginx: &v1alpha1.Nginx{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "extensions.tsuru.io/v1alpha1",
					Kind:       "Nginx",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx",
					Namespace: "default",
				},
				Spec: v1alpha1.NginxSpec{
					TLSSecret: &v1alpha1.TLSSecret{},
				},
				Status: v1alpha1.NginxStatus{},
			},
			service: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.FromString("http"),
							Port:       int32(80),
							NodePort:   int32(30666),
						},
					},
				},
			},
			assertion: func(t *testing.T, err error, got *corev1.Service) {
				assert.NoError(t, err)
				assert.NotNil(t, got)
				expectedPorts := []corev1.ServicePort{
					corev1.ServicePort{
						Name:       "http",
						TargetPort: intstr.FromString("http"),
						Protocol:   corev1.ProtocolTCP,
						NodePort:   int32(30666),
						Port:       int32(80),
					},
					corev1.ServicePort{
						Name:       "https",
						TargetPort: intstr.FromString("https"),
						Protocol:   corev1.ProtocolTCP,
						NodePort:   int32(0),
						Port:       int32(443),
					},
				}
				assert.Equal(t, expectedPorts, got.Spec.Ports)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resources := []runtime.Object{}
			if tt.service != nil {
				resources = append(resources, tt.service)
			}
			reconciler := &ReconcileNginx{
				client: fake.NewFakeClientWithScheme(scheme, resources...),
				scheme: scheme,
			}
			err := reconciler.reconcileService(tt.nginx)
			gotService := &corev1.Service{}
			serviceName := types.NamespacedName{Name: tt.nginx.Name + "-service", Namespace: tt.nginx.Namespace}
			require.NoError(t, reconciler.client.Get(nil, serviceName, gotService))
			tt.assertion(t, err, gotService)
		})
	}
}
