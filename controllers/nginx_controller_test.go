// Copyright 2020 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/tsuru/nginx-operator/api/v1alpha1"
	nginxv1alpha1 "github.com/tsuru/nginx-operator/api/v1alpha1"
)

func TestNginxReconciler_reconcileService(t *testing.T) {
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
					TLS: []v1alpha1.NginxTLS{{SecretName: "my-secret"}},
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
					TLS: []v1alpha1.NginxTLS{{SecretName: "my-secret"}},
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
				assert.Equal(t, map[string]string{
					"nginx.tsuru.io/app":           "nginx",
					"nginx.tsuru.io/resource-name": "my-nginx",
					"nginx.tsuru.io/new-label":     "v1",
				}, got.Labels)
				assert.Equal(t, map[string]string{"nginx.tsuru.io/new-annotation": "v1", "old-service-annotation": "v1"}, got.Annotations)
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
					TLS: []v1alpha1.NginxTLS{{SecretName: "my-secret"}},
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
					Type:                  corev1.ServiceTypeLoadBalancer,
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
					ClusterIP:             "10.1.1.10",
					HealthCheckNodePort:   int32(43123),
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
				assert.Equal(t, got.Spec.ExternalTrafficPolicy, corev1.ServiceExternalTrafficPolicyType(""))
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
		{
			name: "when updating the nginx service, should maintain other controller's annotations",
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
						Annotations: map[string]string{
							"annotation-from-this-controller": "updated",
						},
					},
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
						"annotation-from-this-controller":    "please-update",
						"annotation-from-another-controller": "please-keep-it",
					},
					Labels: map[string]string{},
				},
				Spec: corev1.ServiceSpec{
					Type:                  corev1.ServiceTypeLoadBalancer,
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
					ClusterIP:             "10.1.1.10",
					HealthCheckNodePort:   int32(43123),
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
				assert.Equal(t, map[string]string{
					"annotation-from-this-controller":    "updated",
					"annotation-from-another-controller": "please-keep-it",
				}, got.Annotations)
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
				WithScheme(newScheme()).
				WithRuntimeObjects(resources...).
				Build()

			r := &NginxReconciler{
				Client: client,
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

func TestNginxReconciler_reconcileIngress(t *testing.T) {
	resources := []runtime.Object{
		&networkingv1.Ingress{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "networking.k8s.io/v1",
				Kind:       "Ingress",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-nginx-1",
				Namespace: "default",
			},
			Spec: networkingv1.IngressSpec{
				IngressClassName: func(s string) *string { return &s }("default-ingress"),
				Rules: []networkingv1.IngressRule{
					{
						Host: "my-nginx-1.test",
						IngressRuleValue: networkingv1.IngressRuleValue{
							HTTP: &networkingv1.HTTPIngressRuleValue{
								Paths: []networkingv1.HTTPIngressPath{
									{
										Path: "/",
										Backend: networkingv1.IngressBackend{
											Service: &networkingv1.IngressServiceBackend{
												Name: "my-nginx-1-service",
												Port: networkingv1.ServiceBackendPort{Name: "http"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	tests := map[string]struct {
		nginx         *nginxv1alpha1.Nginx
		expectedError string
		assert        func(t *testing.T, c client.Client, nginx *v1alpha1.Nginx)
	}{
		"when nginx is nil, should return expected error": {
			expectedError: "nginx cannot be nil",
		},

		"when ingress does not exist, should create one": {
			nginx: &nginxv1alpha1.Nginx{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-nginx-2",
					Namespace:       "default",
					ResourceVersion: "666",
				},
				Spec: nginxv1alpha1.NginxSpec{
					Ingress: &nginxv1alpha1.NginxIngress{
						IngressClassName: func(s string) *string { return &s }("custom-class"),
					},
				},
			},
			assert: func(t *testing.T, c client.Client, nginx *v1alpha1.Nginx) {
				var got networkingv1.Ingress
				err := c.Get(context.TODO(), types.NamespacedName{Name: "my-nginx-2", Namespace: "default"}, &got)
				require.NoError(t, err)

				assert.Equal(t, networkingv1.Ingress{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "networking.k8s.io/v1",
						Kind:       "Ingress",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:            "my-nginx-2",
						Namespace:       "default",
						ResourceVersion: "1",
						Labels: map[string]string{
							"nginx.tsuru.io/app":           "nginx",
							"nginx.tsuru.io/resource-name": "my-nginx-2",
						},
						OwnerReferences: []metav1.OwnerReference{
							*metav1.NewControllerRef(nginx, schema.GroupVersionKind{
								Group:   v1alpha1.GroupVersion.Group,
								Version: v1alpha1.GroupVersion.Version,
								Kind:    "Nginx",
							}),
						},
					},
					Spec: networkingv1.IngressSpec{
						IngressClassName: func(s string) *string { return &s }("custom-class"),
						DefaultBackend: &networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: "my-nginx-2-service",
								Port: networkingv1.ServiceBackendPort{Name: "http"},
							},
						},
					},
				}, got)
			},
		},

		"when nginx removes ingress field, should remove the ingress resource": {
			nginx: &v1alpha1.Nginx{
				ObjectMeta: metav1.ObjectMeta{Name: "my-nginx-1", Namespace: "default"},
			},
			assert: func(t *testing.T, c client.Client, nginx *v1alpha1.Nginx) {
				var got networkingv1.Ingress
				err := c.Get(context.TODO(), types.NamespacedName{Name: "my-nginx-1", Namespace: "default"}, &got)
				assert.Error(t, err)
				assert.True(t, errors.IsNotFound(err))
			},
		},

		"when ingress already exists, updating the ingress field in nginx should update the target ingress resource": {
			nginx: &v1alpha1.Nginx{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-nginx-1",
					Namespace: "default",
				},
				Spec: v1alpha1.NginxSpec{
					Ingress: &v1alpha1.NginxIngress{
						Annotations: map[string]string{"custom.nginx.tsuru.io/foo": "bar"},
						Labels:      map[string]string{"custom.nginx.tsuru.io": "key1"},
					},
				},
			},
			assert: func(t *testing.T, c client.Client, nginx *v1alpha1.Nginx) {
				var got networkingv1.Ingress
				err := c.Get(context.TODO(), types.NamespacedName{Name: "my-nginx-1", Namespace: "default"}, &got)
				require.NoError(t, err)

				assert.Equal(t, map[string]string{"custom.nginx.tsuru.io/foo": "bar"}, got.Annotations)
				assert.Equal(t, map[string]string{
					"nginx.tsuru.io/app":           "nginx",
					"nginx.tsuru.io/resource-name": "my-nginx-1",
					"custom.nginx.tsuru.io":        "key1",
				}, got.Labels)
				assert.Equal(t, networkingv1.IngressSpec{
					DefaultBackend: &networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: "my-nginx-1-service",
							Port: networkingv1.ServiceBackendPort{Name: "http"},
						},
					},
				}, got.Spec)
			},
		},

		"when TLS is set, should not set the default backend": {
			nginx: &nginxv1alpha1.Nginx{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-nginx-2",
					Namespace:       "default",
					ResourceVersion: "666",
				},
				Spec: nginxv1alpha1.NginxSpec{
					Ingress: &nginxv1alpha1.NginxIngress{},
					TLS: []nginxv1alpha1.NginxTLS{
						{SecretName: "example-com-certs", Hosts: []string{"www.example.com"}},
					},
				},
			},
			assert: func(t *testing.T, c client.Client, nginx *v1alpha1.Nginx) {
				var got networkingv1.Ingress
				err := c.Get(context.TODO(), types.NamespacedName{Name: "my-nginx-2", Namespace: "default"}, &got)
				require.NoError(t, err)

				assert.Equal(t, networkingv1.Ingress{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "networking.k8s.io/v1",
						Kind:       "Ingress",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:            "my-nginx-2",
						Namespace:       "default",
						ResourceVersion: "1",
						Labels: map[string]string{
							"nginx.tsuru.io/app":           "nginx",
							"nginx.tsuru.io/resource-name": "my-nginx-2",
						},
						OwnerReferences: []metav1.OwnerReference{
							*metav1.NewControllerRef(nginx, schema.GroupVersionKind{
								Group:   v1alpha1.GroupVersion.Group,
								Version: v1alpha1.GroupVersion.Version,
								Kind:    "Nginx",
							}),
						},
					},
					Spec: networkingv1.IngressSpec{
						TLS: []networkingv1.IngressTLS{
							{SecretName: "example-com-certs", Hosts: []string{"www.example.com"}},
						},
						Rules: []networkingv1.IngressRule{
							{
								Host: "www.example.com",
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: []networkingv1.HTTPIngressPath{
											{
												Path:     "/",
												PathType: func(t networkingv1.PathType) *networkingv1.PathType { return &t }(networkingv1.PathTypePrefix),
												Backend: networkingv1.IngressBackend{
													Service: &networkingv1.IngressServiceBackend{
														Name: "my-nginx-2-service",
														Port: networkingv1.ServiceBackendPort{
															Name: "http",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				}, got)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithScheme(newScheme()).
				WithRuntimeObjects(resources...).
				Build()

			r := &NginxReconciler{Client: client}
			err := r.reconcileIngress(context.TODO(), tt.nginx)
			if tt.expectedError != "" {
				assert.EqualError(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)
			if tt.assert != nil {
				tt.assert(t, client, tt.nginx)
			}
		})
	}
}

func TestNginxReconciler_reconcileStatus(t *testing.T) {
	nginx := v1alpha1.Nginx{ObjectMeta: metav1.ObjectMeta{Name: "my-nginx", Namespace: "default"}}

	resources := []runtime.Object{
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-nginx",
				Namespace: "default",
				Labels: map[string]string{
					"nginx.tsuru.io/app":           "nginx",
					"nginx.tsuru.io/resource-name": "my-nginx",
				},
			},
			Status: appsv1.DeploymentStatus{
				Replicas: int32(3),
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-nginx-service",
				Namespace: "default",
				Labels: map[string]string{
					"nginx.tsuru.io/app":           "nginx",
					"nginx.tsuru.io/resource-name": "my-nginx",
				},
			},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-nginx",
				Namespace: "default",
				Labels: map[string]string{
					"nginx.tsuru.io/app":           "nginx",
					"nginx.tsuru.io/resource-name": "my-nginx",
				},
			},
		},
		&nginx,
	}

	client := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithRuntimeObjects(resources...).
		Build()

	r := &NginxReconciler{Client: client}
	assert.NoError(t, r.refreshStatus(context.TODO(), &nginx))

	var got v1alpha1.Nginx
	err := client.Get(context.TODO(), types.NamespacedName{Name: "my-nginx", Namespace: "default"}, &got)
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.NginxStatus{
		CurrentReplicas: int32(3),
		PodSelector:     "nginx.tsuru.io/app=nginx,nginx.tsuru.io/resource-name=my-nginx",
		Deployments:     []v1alpha1.DeploymentStatus{{Name: "my-nginx"}},
		Services:        []v1alpha1.ServiceStatus{{Name: "my-nginx-service"}},
		Ingresses:       []v1alpha1.IngressStatus{{Name: "my-nginx"}},
	}, got.Status)
}

func TestNginxReconciler_shouldManageNginx(t *testing.T) {
	tests := []struct {
		nginx            *v1alpha1.Nginx
		annotationFilter func() labels.Selector
		expected         bool
	}{
		{
			nginx: &v1alpha1.Nginx{},
			annotationFilter: func() labels.Selector {
				return nil
			},
			expected: true,
		},
		{
			nginx: &v1alpha1.Nginx{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"custom.annotation":  "foo",
						"another.annotation": "bar",
					},
				},
			},
			annotationFilter: func() labels.Selector {
				ls, err := metav1.ParseToLabelSelector("custom.annotation==foo,another.annotation")
				require.NoError(t, err)
				sel, err := metav1.LabelSelectorAsSelector(ls)
				require.NoError(t, err)
				return sel
			},
			expected: true,
		},
		{
			nginx: &v1alpha1.Nginx{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"my.custom.annotation/skip": "true",
					},
				},
			},
			annotationFilter: func() labels.Selector {
				ls, err := metav1.ParseToLabelSelector("!my.custom.annotation/skip")
				require.NoError(t, err)
				sel, err := metav1.LabelSelectorAsSelector(ls)
				require.NoError(t, err)
				return sel
			},
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			r := &NginxReconciler{AnnotationFilter: tt.annotationFilter()}
			got := r.shouldManageNginx(tt.nginx)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func newScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	clientgoscheme.AddToScheme(scheme)
	v1alpha1.AddToScheme(scheme)
	return scheme
}
