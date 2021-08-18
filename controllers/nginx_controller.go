// Copyright 2020 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	nginxv1alpha1 "github.com/tsuru/nginx-operator/api/v1alpha1"
	"github.com/tsuru/nginx-operator/pkg/k8s"
)

// NginxReconciler reconciles a Nginx object
type NginxReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=nginx.tsuru.io,resources=nginxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nginx.tsuru.io,resources=nginxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=networking,resources=ingresses,verbs=get;ĺist;watch;create;update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

func (r *NginxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nginxv1alpha1.Nginx{}).
		Watches(&source.Kind{Type: new(corev1.Pod)}, handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
			// HACK(nettoclaudio): We're watching the pods in order to update
			// the Nginx status subresource with as fresh as possible
			// information.
			name := k8s.GetNginxNameFromObject(o)
			if name == "" {
				return nil
			}

			return []ctrl.Request{{NamespacedName: types.NamespacedName{Name: name, Namespace: o.GetNamespace()}}}
		})).
		Complete(r)
}

func (r *NginxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("nginx", req.NamespacedName)

	var instance nginxv1alpha1.Nginx
	err := r.Client.Get(ctx, req.NamespacedName, &instance)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Nginx resource not found, skipping reconcile")
			return ctrl.Result{}, nil
		}

		log.Error(err, "Unable to get Nginx resource")
		return ctrl.Result{}, err
	}

	if err := r.reconcileNginx(ctx, &instance); err != nil {
		log.Error(err, "Fail to reconcile")
		return ctrl.Result{}, err
	}

	if err := r.refreshStatus(ctx, &instance); err != nil {
		log.Error(err, "Fail to refresh status subresource")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *NginxReconciler) reconcileNginx(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	if err := r.reconcileDeployment(ctx, nginx); err != nil {
		return err
	}

	if err := r.reconcileService(ctx, nginx); err != nil {
		return err
	}

	if err := r.reconcileIngress(ctx, nginx); err != nil {
		return err
	}

	return nil
}

func (r *NginxReconciler) reconcileDeployment(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	newDeploy, err := k8s.NewDeployment(nginx)
	if err != nil {
		return fmt.Errorf("failed to assemble deployment from nginx: %v", err)
	}

	err = r.Client.Create(ctx, newDeploy)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create deployment: %v", err)
	}

	if err == nil {
		return nil
	}

	currDeploy := &appsv1.Deployment{}

	err = r.Client.Get(ctx, types.NamespacedName{Name: newDeploy.Name, Namespace: newDeploy.Namespace}, currDeploy)
	if err != nil {
		return fmt.Errorf("failed to retrieve deployment: %v", err)
	}

	currSpec, err := k8s.ExtractNginxSpec(currDeploy.ObjectMeta)
	if err != nil {
		return fmt.Errorf("failed to extract nginx from deployment: %v", err)
	}

	if reflect.DeepEqual(nginx.Spec, currSpec) {
		return nil
	}

	currDeploy.Spec = newDeploy.Spec
	if err := k8s.SetNginxSpec(&currDeploy.ObjectMeta, nginx.Spec); err != nil {
		return fmt.Errorf("failed to set nginx spec into object meta: %v", err)
	}

	if err := r.Client.Update(ctx, currDeploy); err != nil {
		return fmt.Errorf("failed to update deployment: %v", err)
	}

	return nil
}

func (r *NginxReconciler) reconcileService(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	svcName := types.NamespacedName{
		Name:      fmt.Sprintf("%s-service", nginx.Name),
		Namespace: nginx.Namespace,
	}

	logger := r.Log.WithName("reconcileService").WithValues("Service", svcName)
	logger.V(4).Info("Getting Service resource")

	newService := k8s.NewService(nginx)

	var currentService corev1.Service
	err := r.Client.Get(ctx, svcName, &currentService)
	if err != nil && errors.IsNotFound(err) {
		logger.
			WithValues("ServiceResource", newService).V(4).Info("Creating a Service resource")

		return r.Client.Create(ctx, newService)
	}

	if err != nil {
		return fmt.Errorf("failed to retrieve Service resource: %v", err)
	}

	newService.ResourceVersion = currentService.ResourceVersion
	newService.Spec.ClusterIP = currentService.Spec.ClusterIP
	newService.Spec.HealthCheckNodePort = currentService.Spec.HealthCheckNodePort

	if newService.Spec.Type == corev1.ServiceTypeNodePort || newService.Spec.Type == corev1.ServiceTypeLoadBalancer {
		// avoid nodeport reallocation preserving the current ones
		for _, currentPort := range currentService.Spec.Ports {
			for index, newPort := range newService.Spec.Ports {
				if currentPort.Port == newPort.Port {
					newService.Spec.Ports[index].NodePort = currentPort.NodePort
				}
			}
		}
	}

	logger.WithValues("ServiceResource", newService).V(4).Info("Updating Service resource")

	return r.Client.Update(ctx, newService)
}

func (r *NginxReconciler) reconcileIngress(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	if nginx == nil {
		return fmt.Errorf("nginx cannot be nil")
	}

	new := k8s.NewIngress(nginx)

	var current networkingv1.Ingress
	err := r.Client.Get(ctx, types.NamespacedName{Name: new.Name, Namespace: new.Namespace}, &current)
	if errors.IsNotFound(err) {
		if nginx.Spec.Ingress == nil {
			return nil
		}

		return r.Client.Create(ctx, new)
	}

	if err != nil {
		return err
	}

	if nginx.Spec.Ingress == nil {
		return r.Client.Delete(ctx, &current)
	}

	if !shouldUpdateIngress(&current, new) {
		return nil
	}

	new.ResourceVersion = current.ResourceVersion

	return r.Client.Update(ctx, new)
}

func shouldUpdateIngress(current, new *networkingv1.Ingress) bool {
	if current == nil || new == nil {
		return false
	}

	return !reflect.DeepEqual(current.Annotations, new.Annotations) ||
		!reflect.DeepEqual(current.Labels, new.Labels) ||
		!reflect.DeepEqual(current.Spec, new.Spec)
}

func (r *NginxReconciler) refreshStatus(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	pods, err := listPods(ctx, r.Client, nginx)
	if err != nil {
		return fmt.Errorf("failed to list pods for nginx: %v", err)
	}

	services, err := listServices(ctx, r.Client, nginx)
	if err != nil {
		return fmt.Errorf("failed to list services for nginx: %v", err)

	}

	sort.Slice(nginx.Status.Pods, func(i, j int) bool {
		return nginx.Status.Pods[i].Name < nginx.Status.Pods[j].Name
	})

	sort.Slice(nginx.Status.Services, func(i, j int) bool {
		return nginx.Status.Services[i].Name < nginx.Status.Services[j].Name
	})

	if !reflect.DeepEqual(pods, nginx.Status.Pods) || !reflect.DeepEqual(services, nginx.Status.Services) {
		nginx.Status.Pods = pods
		nginx.Status.Services = services
		nginx.Status.CurrentReplicas = int32(len(pods))
		nginx.Status.PodSelector = k8s.LabelsForNginxString(nginx.Name)
		err := r.Client.Status().Update(ctx, nginx)
		if err != nil {
			return fmt.Errorf("failed to update nginx status: %v", err)
		}
	}

	return nil
}

// listPods return all the pods for the given nginx sorted by name
func listPods(ctx context.Context, c client.Client, nginx *nginxv1alpha1.Nginx) ([]nginxv1alpha1.PodStatus, error) {
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(k8s.LabelsForNginx(nginx.Name))
	listOps := &client.ListOptions{Namespace: nginx.Namespace, LabelSelector: labelSelector}
	err := c.List(ctx, podList, listOps)
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
	labelSelector := labels.SelectorFromSet(k8s.LabelsForNginx(nginx.Name))
	listOps := &client.ListOptions{Namespace: nginx.Namespace, LabelSelector: labelSelector}
	err := c.List(ctx, serviceList, listOps)
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
