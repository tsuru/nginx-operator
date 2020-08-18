// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nginx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsuru/nginx-operator/pkg/apis"
	"github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
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
					{
						Name:       "http",
						TargetPort: intstr.FromString("http"),
						Protocol:   corev1.ProtocolTCP,
						NodePort:   int32(0),
						Port:       int32(80),
					},
					{
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
					Certificates: &v1alpha1.TLSSecret{},
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
							Name:       "https",
							TargetPort: intstr.FromString("https"),
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(443),
							NodePort:   int32(30667),
						},
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
					{
						Name:       "http",
						TargetPort: intstr.FromString("http"),
						Protocol:   corev1.ProtocolTCP,
						NodePort:   int32(30666),
						Port:       int32(80),
					},
					{
						Name:       "https",
						TargetPort: intstr.FromString("https"),
						Protocol:   corev1.ProtocolTCP,
						NodePort:   int32(30667),
						Port:       int32(443),
					},
				}
				assert.Equal(t, expectedPorts, got.Spec.Ports)
			},
		},
		{
			name: "when updating the nginx service field, should update the service resource as well",
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
					Service: &v1alpha1.NginxService{
						Type: corev1.ServiceTypeLoadBalancer,
						Annotations: map[string]string{
							"nginx.tsuru.io/new-annotation": "v1",
						},
						Labels: map[string]string{
							"nginx.tsuru.io/new-label": "v1",
						},
					},
					Certificates: &v1alpha1.TLSSecret{},
				},
			},
			service: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx-service",
					Namespace: "default",
					Annotations: map[string]string{
						"old-service-annotation": "v1",
					},
					Labels: map[string]string{
						"old-service-label": "v1",
					},
				},
				Spec: corev1.ServiceSpec{
					ClusterIP:           "10.1.1.10",
					HealthCheckNodePort: int32(43123),
					Ports: []corev1.ServicePort{
						{
							Name:       "https",
							TargetPort: intstr.FromString("https"),
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(443),
							NodePort:   int32(30667),
						},
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
				assert.Equal(t, got.Spec.ClusterIP, "10.1.1.10")
				assert.Equal(t, got.Spec.Type, corev1.ServiceTypeLoadBalancer)
				assert.Equal(t, got.Spec.HealthCheckNodePort, int32(43123))
				expectedPorts := []corev1.ServicePort{
					{
						Name:       "http",
						TargetPort: intstr.FromString("http"),
						Protocol:   corev1.ProtocolTCP,
						NodePort:   int32(30666),
						Port:       int32(80),
					},
					{
						Name:       "https",
						TargetPort: intstr.FromString("https"),
						Protocol:   corev1.ProtocolTCP,
						NodePort:   int32(30667),
						Port:       int32(443),
					},
				}
				assert.Equal(t, expectedPorts, got.Spec.Ports)
				assert.Equal(t, got.Labels, map[string]string{
					"nginx.tsuru.io/app":           "nginx",
					"nginx.tsuru.io/resource-name": "my-nginx",
					"nginx.tsuru.io/new-label":     "v1",
				})
				assert.Equal(t, got.Annotations, map[string]string{"nginx.tsuru.io/new-annotation": "v1"})
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
			err := reconciler.reconcileService(context.TODO(), tt.nginx)
			gotService := &corev1.Service{}
			serviceName := types.NamespacedName{Name: tt.nginx.Name + "-service", Namespace: tt.nginx.Namespace}
			require.NoError(t, reconciler.client.Get(context.Background(), serviceName, gotService))
			tt.assertion(t, err, gotService)
		})
	}
}

func TestReconcileNginx_reconcilePDB(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	apis.AddToScheme(scheme)
	policyv1beta1.AddToScheme(scheme)

	ten := intstr.FromString("10%")
	hundred := intstr.FromString("100%")

	tests := []struct {
		name      string
		nginx     *v1alpha1.Nginx
		resources []runtime.Object
		assert    func(t *testing.T, err error, got *policyv1beta1.PodDisruptionBudget)
	}{
		{
			name: "when nginx is nil",
			assert: func(t *testing.T, err error, got *policyv1beta1.PodDisruptionBudget) {
				assert.EqualError(t, err, "nginx cannot be nil")
			},
		},
		{
			name: "when max unavailable is set and an old pdb resource does not exist",
			nginx: &v1alpha1.Nginx{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-instance",
					Namespace: "default",
				},
				Spec: v1alpha1.NginxSpec{
					DisruptionBudget: &v1alpha1.NginxDisruptionBudget{
						MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "10%"},
					},
				},
			},
			assert: func(t *testing.T, err error, got *policyv1beta1.PodDisruptionBudget) {
				require.NoError(t, err)
				assert.NotNil(t, got)

				expected := policyv1beta1.PodDisruptionBudgetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nginx.tsuru.io/app":           "nginx",
							"nginx.tsuru.io/resource-name": "my-instance",
						},
					},
					MaxUnavailable: &ten,
				}
				assert.Equal(t, got.Spec, expected)
			},
		},
		{
			name: "when the pdb resource exists but spec has changed",
			nginx: &v1alpha1.Nginx{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-instance",
					Namespace: "default",
				},
				Spec: v1alpha1.NginxSpec{
					DisruptionBudget: &v1alpha1.NginxDisruptionBudget{
						MinAvailable: &hundred,
					},
				},
			},
			resources: []runtime.Object{
				&policyv1beta1.PodDisruptionBudget{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1beta1",
						Kind:       "PodDisruptionBudget",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-instance",
						Namespace: "default",
					},
					Spec: policyv1beta1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nginx.tsuru.io/app":           "nginx",
								"nginx.tsuru.io/resource-name": "my-instance",
							},
						},
						MaxUnavailable: &ten,
					},
				},
			},
			assert: func(t *testing.T, err error, got *policyv1beta1.PodDisruptionBudget) {
				require.NoError(t, err)
				assert.NotNil(t, got)

				expected := policyv1beta1.PodDisruptionBudgetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nginx.tsuru.io/app":           "nginx",
							"nginx.tsuru.io/resource-name": "my-instance",
						},
					},
					MinAvailable: &hundred,
				}
				assert.Equal(t, got.Spec, expected)
			},
		},
		{
			name: "when pdb exists but disruptionbudget on nginx is nil",
			nginx: &v1alpha1.Nginx{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-instance",
					Namespace: "default",
				},
			},
			resources: []runtime.Object{
				&policyv1beta1.PodDisruptionBudget{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1beta1",
						Kind:       "PodDisruptionBudget",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-instance",
						Namespace: "default",
					},
					Spec: policyv1beta1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nginx.tsuru.io/app":           "nginx",
								"nginx.tsuru.io/resource-name": "my-instance",
							},
						},
						MaxUnavailable: &ten,
					},
				},
			},
			assert: func(t *testing.T, err error, got *policyv1beta1.PodDisruptionBudget) {
				require.NoError(t, err)
				assert.Nil(t, got)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.assert)
			r := ReconcileNginx{client: fake.NewFakeClientWithScheme(scheme, tt.resources...)}
			err := r.reconcilePDB(context.TODO(), tt.nginx)
			var got *policyv1beta1.PodDisruptionBudget
			if tt.nginx != nil {
				got = new(policyv1beta1.PodDisruptionBudget)
				key := types.NamespacedName{Name: got.Name, Namespace: got.Namespace}
				nerr := r.client.Get(context.TODO(), key, got)
				if nerr != nil {
					got = nil
				}
			}
			tt.assert(t, err, got)
		})
	}
}
