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
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_nginx")

// Add creates a new Nginx Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileNginx{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("nginx-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Nginx
	err = c.Watch(&source.Kind{Type: &nginxv1alpha1.Nginx{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// HACK(nettoclaudio): Since the Nginx needs store all its pods' info into
	// the status field, we need watching every pod changes and enqueue a new
	// reconcile request to its Nginx owner, if any.
	return c.Watch(&source.Kind{Type: &corev1.Pod{}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(func(o handler.MapObject) []reconcile.Request {
				nginxResourceName := k8s.GetNginxNameFromObject(o.Meta)
				if nginxResourceName == "" {
					return nil
				}

				return []reconcile.Request{
					{NamespacedName: types.NamespacedName{
						Name:      nginxResourceName,
						Namespace: o.Meta.GetNamespace(),
					}},
				}
			}),
		},
	)
}

var _ reconcile.Reconciler = &ReconcileNginx{}

// ReconcileNginx reconciles a Nginx object
type ReconcileNginx struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Nginx object and makes changes based on the state read
// and what is in the Nginx.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileNginx) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Nginx", request)
	reqLogger.Info("Starting Nginx reconciling")
	defer reqLogger.Info("Finishing Nginx reconciling")

	ctx := context.Background()

	instance := &nginxv1alpha1.Nginx{}
	err := r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Nginx resource not found, skipping reconcile")
			return reconcile.Result{}, nil
		}

		reqLogger.Error(err, "Unable to get Nginx resource")
		return reconcile.Result{}, err
	}

	if err := r.reconcileNginx(ctx, instance); err != nil {
		reqLogger.Error(err, "Fail to reconcile")
		return reconcile.Result{}, err
	}

	if err := r.reconcilePDB(ctx, instance); err != nil {
		reqLogger.Error(err, "Failed to reconcile PodDisruptionBudget")
		return reconcile.Result{}, err
	}

	if err := r.refreshStatus(ctx, instance); err != nil {
		reqLogger.Error(err, "Fail to refresh status subresource")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileNginx) reconcileNginx(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	if err := r.reconcileDeployment(ctx, nginx); err != nil {
		return err
	}

	if err := r.reconcileService(ctx, nginx); err != nil {
		return err
	}

	return nil
}

func (r *ReconcileNginx) reconcileDeployment(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	newDeploy, err := k8s.NewDeployment(nginx)
	if err != nil {
		return fmt.Errorf("failed to assemble deployment from nginx: %v", err)
	}

	err = r.client.Create(ctx, newDeploy)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create deployment: %v", err)
	}

	if err == nil {
		return nil
	}

	currDeploy := &appv1.Deployment{}

	err = r.client.Get(ctx, types.NamespacedName{Name: newDeploy.Name, Namespace: newDeploy.Namespace}, currDeploy)
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

	if err := r.client.Update(ctx, currDeploy); err != nil {
		return fmt.Errorf("failed to update deployment: %v", err)
	}

	return nil
}

func (r *ReconcileNginx) reconcileService(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	svcName := types.NamespacedName{
		Name:      fmt.Sprintf("%s-service", nginx.Name),
		Namespace: nginx.Namespace,
	}

	logger := log.WithName("reconcileService").WithValues("Service", svcName)
	logger.V(4).Info("Getting Service resource")

	newService := k8s.NewService(nginx)

	var currentService corev1.Service
	err := r.client.Get(ctx, svcName, &currentService)
	if err != nil && errors.IsNotFound(err) {
		logger.
			WithValues("ServiceResource", newService).V(4).Info("Creating a Service resource")

		return r.client.Create(ctx, newService)
	}

	if err != nil {
		return fmt.Errorf("failed to retrieve Service resource: %v", err)
	}

	newService.ResourceVersion = currentService.ResourceVersion
	newService.Spec.ClusterIP = currentService.Spec.ClusterIP
	newService.Spec.HealthCheckNodePort = currentService.Spec.HealthCheckNodePort

	// avoid nodeport reallocation preserving the current ones
	for _, currentPort := range currentService.Spec.Ports {
		for index, newPort := range newService.Spec.Ports {
			if currentPort.Port == newPort.Port {
				newService.Spec.Ports[index].NodePort = currentPort.NodePort
			}
		}
	}

	logger.WithValues("ServiceResource", newService).V(4).Info("Updating Service resource")

	return r.client.Update(ctx, newService)
}

func (r *ReconcileNginx) reconcilePDB(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	if nginx == nil {
		return fmt.Errorf("nginx cannot be nil")
	}

	newPDB := k8s.NewPDB(nginx)

	var current policyv1beta1.PodDisruptionBudget
	err := r.client.Get(ctx, types.NamespacedName{Name: newPDB.Name, Namespace: newPDB.Namespace}, &current)
	if errors.IsNotFound(err) {
		if nginx.Spec.DisruptionBudget == nil {
			return nil
		}

		return r.client.Create(ctx, newPDB)
	}

	if err != nil {
		return err
	}

	if nginx.Spec.DisruptionBudget == nil {
		return r.client.Delete(ctx, &current)
	}

	newPDB.ResourceVersion = current.ResourceVersion
	return r.client.Update(ctx, newPDB)
}

func (r *ReconcileNginx) refreshStatus(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	pods, err := listPods(ctx, r.client, nginx)
	if err != nil {
		return fmt.Errorf("failed to list pods for nginx: %v", err)
	}
	services, err := listServices(ctx, r.client, nginx)
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
		err := r.client.Status().Update(ctx, nginx)
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
