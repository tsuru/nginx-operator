// Copyright 2020 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/tsuru/nginx-operator/api/v1alpha1"
)

func TestNginxReconciler_reconcileService(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	v1alpha1.AddToScheme(scheme)

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
					Service: &v1alpha1.NginxService{
						Type: corev1.ServiceTypeNodePort,
					},
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
					Type: corev1.ServiceTypeNodePort,
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
		{
			name: "when updating the nginx service type, should discard nodeports when new service is clusterIP",
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
						Type: corev1.ServiceTypeClusterIP,
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
					Name:        "my-nginx-service",
					Namespace:   "default",
					Annotations: map[string]string{},
					Labels:      map[string]string{},
				},
				Spec: corev1.ServiceSpec{
					Type:                corev1.ServiceTypeLoadBalancer,
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
				assert.Equal(t, got.Spec.Type, corev1.ServiceTypeClusterIP)
				assert.Equal(t, got.Spec.HealthCheckNodePort, int32(43123))
				expectedPorts := []corev1.ServicePort{
					{
						Name:       "http",
						TargetPort: intstr.FromString("http"),
						Protocol:   corev1.ProtocolTCP,
						Port:       int32(80),
					},
					{
						Name:       "https",
						TargetPort: intstr.FromString("https"),
						Protocol:   corev1.ProtocolTCP,
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

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(resources...).
				Build()

			r := &NginxReconciler{
				Client: client,
				Scheme: scheme,
				Log:    ctrl.Log.WithName("test"),
			}
			err := r.reconcileService(context.TODO(), tt.nginx)
			gotService := &corev1.Service{}
			serviceName := types.NamespacedName{Name: tt.nginx.Name + "-service", Namespace: tt.nginx.Namespace}
			require.NoError(t, r.Client.Get(context.Background(), serviceName, gotService))
			tt.assertion(t, err, gotService)
		})
	}
}
