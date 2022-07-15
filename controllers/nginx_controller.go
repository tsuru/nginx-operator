// Copyright 2020 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/tsuru/nginx-operator/api/v1alpha1"
	nginxv1alpha1 "github.com/tsuru/nginx-operator/api/v1alpha1"
	"github.com/tsuru/nginx-operator/pkg/k8s"
)

// NginxReconciler reconciles a Nginx object
type NginxReconciler struct {
	client.Client
	EventRecorder    record.EventRecorder
	Log              logr.Logger
	Scheme           *runtime.Scheme
	AnnotationFilter labels.Selector
}

// +kubebuilder:rbac:groups=nginx.tsuru.io,resources=nginxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nginx.tsuru.io,resources=nginxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch

func (r *NginxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nginxv1alpha1.Nginx{}).
		Owns(&appsv1.Deployment{}).
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

	if !r.shouldManageNginx(&instance) {
		log.V(1).Info("Nginx resource doesn't match annotations filters, skipping it")
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Minute}, nil
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
		return fmt.Errorf("failed to build Deployment from Nginx: %w", err)
	}

	var currentDeploy appsv1.Deployment
	err = r.Client.Get(ctx, types.NamespacedName{Name: newDeploy.Name, Namespace: newDeploy.Namespace}, &currentDeploy)
	if errors.IsNotFound(err) {
		return r.Client.Create(ctx, newDeploy)
	}

	if err != nil {
		return fmt.Errorf("failed to retrieve Deployment: %w", err)
	}

	existingNginxSpec, err := k8s.ExtractNginxSpec(currentDeploy.ObjectMeta)
	if err != nil {
		return fmt.Errorf("failed to extract Nginx spec from Deployment annotations: %w", err)
	}

	if reflect.DeepEqual(nginx.Spec, existingNginxSpec) {
		return nil
	}

	replicas := currentDeploy.Spec.Replicas

	patch := client.StrategicMergeFrom(currentDeploy.DeepCopy())
	currentDeploy.Spec = newDeploy.Spec

	if newDeploy.Spec.Replicas == nil {
		// NOTE: replicas field is set to nil whenever it's managed by some
		// autoscaler controller e.g HPA.
		currentDeploy.Spec.Replicas = replicas
	}

	err = k8s.SetNginxSpec(&currentDeploy.ObjectMeta, nginx.Spec)
	if err != nil {
		return fmt.Errorf("failed to set Nginx spec in Deployment annotations: %w", err)
	}

	err = r.Client.Patch(ctx, &currentDeploy, patch)
	if err != nil {
		return fmt.Errorf("failed to patch Deployment: %w", err)
	}

	return nil
}

func (r *NginxReconciler) reconcileService(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	newService := k8s.NewService(nginx)

	var currentService corev1.Service
	err := r.Client.Get(ctx, types.NamespacedName{Name: newService.Name, Namespace: newService.Namespace}, &currentService)

	if errors.IsNotFound(err) {
		err = r.Client.Create(ctx, newService)
		if errors.IsForbidden(err) && strings.Contains(err.Error(), "exceeded quota") {
			r.EventRecorder.Eventf(nginx, corev1.EventTypeWarning, "ServiceQuotaExceeded", "failed to create Service: %s", err)
			return err
		}

		if err != nil {
			r.EventRecorder.Eventf(nginx, corev1.EventTypeWarning, "ServiceCreationFailed", "failed to create Service: %s", err)
			return err
		}

		r.EventRecorder.Eventf(nginx, corev1.EventTypeNormal, "ServiceCreated", "service created successfully")
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to retrieve Service resource: %v", err)
	}

	newService.ResourceVersion = currentService.ResourceVersion
	newService.Spec.ClusterIP = currentService.Spec.ClusterIP
	newService.Spec.HealthCheckNodePort = currentService.Spec.HealthCheckNodePort
	newService.Finalizers = currentService.Finalizers

	for annotation, value := range currentService.Annotations {
		if newService.Annotations[annotation] == "" {
			newService.Annotations[annotation] = value
		}
	}

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

	err = r.Client.Update(ctx, newService)
	if err != nil {
		r.EventRecorder.Eventf(nginx, corev1.EventTypeWarning, "ServiceUpdateFailed", "failed to update Service: %s", err)
		return err
	}

	r.EventRecorder.Eventf(nginx, corev1.EventTypeNormal, "ServiceUpdated", "service updated successfully")
	return nil
}

func (r *NginxReconciler) reconcileIngress(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	if nginx == nil {
		return fmt.Errorf("nginx cannot be nil")
	}

	newIngress := k8s.NewIngress(nginx)

	var currentIngress networkingv1.Ingress
	err := r.Client.Get(ctx, types.NamespacedName{Name: newIngress.Name, Namespace: newIngress.Namespace}, &currentIngress)
	if errors.IsNotFound(err) {
		if nginx.Spec.Ingress == nil {
			return nil
		}

		return r.Client.Create(ctx, newIngress)
	}

	if err != nil {
		return err
	}

	if nginx.Spec.Ingress == nil {
		return r.Client.Delete(ctx, &currentIngress)
	}

	if !shouldUpdateIngress(&currentIngress, newIngress) {
		return nil
	}

	newIngress.ResourceVersion = currentIngress.ResourceVersion
	newIngress.Finalizers = currentIngress.Finalizers

	return r.Client.Update(ctx, newIngress)
}

func shouldUpdateIngress(currentIngress, newIngress *networkingv1.Ingress) bool {
	if currentIngress == nil || newIngress == nil {
		return false
	}

	return !reflect.DeepEqual(currentIngress.Annotations, newIngress.Annotations) ||
		!reflect.DeepEqual(currentIngress.Labels, newIngress.Labels) ||
		!reflect.DeepEqual(currentIngress.Spec, newIngress.Spec)
}

func (r *NginxReconciler) refreshStatus(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	deploys, err := listDeployments(ctx, r.Client, nginx)
	if err != nil {
		return err
	}

	var deployStatuses []v1alpha1.DeploymentStatus
	var replicas int32
	for _, d := range deploys {
		replicas += d.Status.Replicas
		deployStatuses = append(deployStatuses, v1alpha1.DeploymentStatus{Name: d.Name})
	}

	services, err := listServices(ctx, r.Client, nginx)
	if err != nil {
		return fmt.Errorf("failed to list services for nginx: %v", err)
	}

	ingresses, err := listIngresses(ctx, r.Client, nginx)
	if err != nil {
		return fmt.Errorf("failed to list ingresses for nginx: %w", err)
	}

	sort.Slice(nginx.Status.Services, func(i, j int) bool {
		return nginx.Status.Services[i].Name < nginx.Status.Services[j].Name
	})

	sort.Slice(nginx.Status.Ingresses, func(i, j int) bool {
		return nginx.Status.Ingresses[i].Name < nginx.Status.Ingresses[j].Name
	})

	status := v1alpha1.NginxStatus{
		CurrentReplicas: replicas,
		PodSelector:     k8s.LabelsForNginxString(nginx.Name),
		Deployments:     deployStatuses,
		Services:        services,
		Ingresses:       ingresses,
	}

	if reflect.DeepEqual(nginx.Status, status) {
		return nil
	}

	nginx.Status = status

	err = r.Client.Status().Update(ctx, nginx)
	if err != nil {
		return fmt.Errorf("failed to update nginx status: %v", err)
	}

	return nil
}

func listDeployments(ctx context.Context, c client.Client, nginx *nginxv1alpha1.Nginx) ([]appsv1.Deployment, error) {
	var deployList appsv1.DeploymentList

	err := c.List(ctx, &deployList, &client.ListOptions{
		Namespace:     nginx.Namespace,
		LabelSelector: labels.SelectorFromSet(k8s.LabelsForNginx(nginx.Name)),
	})
	if err != nil {
		return nil, err
	}

	deploys := deployList.Items

	// NOTE: specific implementation for backward compatibility w/ Deployments
	// that does not have Nginx labels yet.
	if len(deploys) == 0 {
		err = c.List(ctx, &deployList, &client.ListOptions{Namespace: nginx.Namespace})
		if err != nil {
			return nil, err
		}

		desired := *metav1.NewControllerRef(nginx, schema.GroupVersionKind{
			Group:   v1alpha1.GroupVersion.Group,
			Version: v1alpha1.GroupVersion.Version,
			Kind:    "Nginx",
		})

		for _, deploy := range deployList.Items {
			for _, owner := range deploy.OwnerReferences {
				if reflect.DeepEqual(owner, desired) {
					deploys = append(deploys, deploy)
				}
			}
		}
	}

	sort.Slice(deploys, func(i, j int) bool { return deploys[i].Name < deploys[j].Name })

	return deploys, nil
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

func listIngresses(ctx context.Context, c client.Client, nginx *nginxv1alpha1.Nginx) ([]nginxv1alpha1.IngressStatus, error) {
	var ingressList networkingv1.IngressList

	options := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(k8s.LabelsForNginx(nginx.Name)),
		Namespace:     nginx.Namespace,
	}
	if err := c.List(ctx, &ingressList, options); err != nil {
		return nil, err
	}

	var ingresses []nginxv1alpha1.IngressStatus
	for _, i := range ingressList.Items {
		ingresses = append(ingresses, nginxv1alpha1.IngressStatus{Name: i.Name})
	}

	sort.Slice(ingresses, func(i, j int) bool {
		return ingresses[i].Name < ingresses[j].Name
	})

	return ingresses, nil
}

func (r *NginxReconciler) shouldManageNginx(nginx *v1alpha1.Nginx) bool {
	// empty filter matches all resources
	if r.AnnotationFilter == nil || r.AnnotationFilter.Empty() {
		return true
	}

	return r.AnnotationFilter.Matches(labels.Set(nginx.Annotations))
}
