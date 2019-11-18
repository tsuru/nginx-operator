// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nginx

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	nginxv1alpha1 "github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	"github.com/tsuru/nginx-operator/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

func AddStatusController(mgr manager.Manager) error {
	statusReconciler := &NginxStatusReconcile{
		client: mgr.GetClient(),
	}

	c, err := controller.New("nginx-status-controller", mgr, controller.Options{Reconciler: statusReconciler})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &nginxv1alpha1.Nginx{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return c.Watch(
		&source.Kind{Type: &corev1.Pod{}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(mapPodToNginxObject),
		},
	)
}

func mapPodToNginxObject(o handler.MapObject) []reconcile.Request {
	nginxName := k8s.GetNginxNameFromObject(o.Meta)
	if nginxName == "" {
		return nil
	}

	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name:      nginxName,
			Namespace: o.Meta.GetNamespace(),
		}},
	}
}

type NginxStatusReconcile struct {
	client client.Client
}

func (r *NginxStatusReconcile) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	ctx := context.Background()

	var nginx nginxv1alpha1.Nginx
	err := r.client.Get(ctx, request.NamespacedName, &nginx)
	if err != nil && errors.IsNotFound(err) {
		return reconcile.Result{}, nil
	}

	if err != nil {
		return reconcile.Result{}, err
	}

	if err = refreshStatus(ctx, r.client, &nginx); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func refreshStatus(ctx context.Context, client client.Client, nginx *nginxv1alpha1.Nginx) error {
	pods, err := listPods(ctx, client, nginx)
	if err != nil {
		return fmt.Errorf("failed to list pods for nginx: %v", err)
	}

	services, err := listServices(ctx, client, nginx)
	if err != nil {
		return fmt.Errorf("failed to list services for nginx: %v", err)
	}

	sort.Slice(nginx.Status.Pods, func(i, j int) bool {
		return nginx.Status.Pods[i].Name < nginx.Status.Pods[j].Name
	})

	sort.Slice(nginx.Status.Services, func(i, j int) bool {
		return nginx.Status.Services[i].Name < nginx.Status.Services[j].Name
	})

	if reflect.DeepEqual(pods, nginx.Status.Pods) && reflect.DeepEqual(services, nginx.Status.Services) {
		return nil
	}

	nginx.Status.Pods = pods
	nginx.Status.Services = services
	nginx.Status.CurrentReplicas = int32(len(pods))
	nginx.Status.PodSelector = k8s.LabelsForNginxString(nginx.Name)

	return client.Status().Update(ctx, nginx)
}

// listPods return all the pods for the given nginx sorted by name
func listPods(ctx context.Context, c client.Client, nginx *nginxv1alpha1.Nginx) ([]nginxv1alpha1.PodStatus, error) {
	podList := &corev1.PodList{}
	listOps := &client.ListOptions{Namespace: nginx.Namespace, LabelSelector: labels.SelectorFromSet(k8s.LabelsForNginx(nginx.Name))}
	err := c.List(ctx, listOps, podList)
	if err != nil {
		return nil, err
	}

	var pods []nginxv1alpha1.PodStatus
	for _, p := range podList.Items {
		if p.Status.PodIP == "" {
			p.Status.PodIP = "<pending>"
		}

		if p.Status.HostIP == "" {
			p.Status.HostIP = "<pending>"
		}

		pods = append(pods, nginxv1alpha1.PodStatus{
			Name:   p.Name,
			PodIP:  p.Status.PodIP,
			HostIP: p.Status.HostIP,
		})
	}

	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})

	return pods, nil
}

// listServices return all the services for the given nginx sorted by name
func listServices(ctx context.Context, c client.Client, nginx *nginxv1alpha1.Nginx) ([]nginxv1alpha1.ServiceStatus, error) {
	serviceList := &corev1.ServiceList{}
	listOps := &client.ListOptions{Namespace: nginx.Namespace, LabelSelector: labels.SelectorFromSet(k8s.LabelsForNginx(nginx.Name))}
	err := c.List(ctx, listOps, serviceList)
	if err != nil {
		return nil, err
	}

	var services []nginxv1alpha1.ServiceStatus
	for _, s := range serviceList.Items {
		services = append(services, nginxv1alpha1.ServiceStatus{
			Name: s.Name,
		})
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return services, nil
}
