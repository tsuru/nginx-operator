// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nginx

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	nginxv1alpha1 "github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	"github.com/tsuru/nginx-operator/pkg/k8s"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
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
	return &ReconcileNginx{mgr: mgr, client: mgr.GetClient(), scheme: mgr.GetScheme()}
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
	mgr    manager.Manager
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
	if nginx.Spec.Service.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal {
		return r.reconcileDeploymentBlueGreen(ctx, nginx)
	}

	newDeploy, err := k8s.NewDeployment(nginx, "")
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

func (r *ReconcileNginx) reconcileDeploymentBlueGreen(ctx context.Context, nginx *nginxv1alpha1.Nginx) error {
	colors := []string{"blue", "green"}
	depList := &appv1.DeploymentList{}
	err := r.client.List(ctx, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			k8s.LabelInstanceName: nginx.Name,
		}),
	}, depList)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	var oldDeploy *appv1.Deployment
	colorIdx := 0

	if depList.Size() > 1 {
		return fmt.Errorf("too many deployments found for app: %#v", depList.Items)
	}

	if depList.Size() > 0 {
		oldDeploy = &depList.Items[0]
		if depList.Items[0].Labels[k8s.LabelInstanceColor] == colors[colorIdx] {
			colorIdx = (colorIdx + 1) % len(colors)
		}
	}

	currSpec, err := k8s.ExtractNginxSpec(oldDeploy.ObjectMeta)
	if err != nil {
		return fmt.Errorf("failed to extract nginx from deployment: %v", err)
	}

	if reflect.DeepEqual(nginx.Spec, currSpec) {
		return nil
	}

	var color string
	if oldDeploy != nil {
		color = colors[colorIdx]
	}

	newDeploy, err := k8s.NewDeployment(nginx, color)
	if err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}
	err = r.client.Create(ctx, newDeploy)
	if err != nil {
		return err
	}
	err = r.waitRollout(ctx, newDeploy)
	if err != nil {
		err = fmt.Errorf("rollout failed for %v: %w", newDeploy.Name, err)
		deleteErr := r.client.Delete(ctx, newDeploy)
		if deleteErr != nil {
			return fmt.Errorf("unable to delete failed deployment after error: %v - original error: %w", deleteErr, err)
		}
		return err
	}
	return r.client.Delete(ctx, oldDeploy)
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
	labelSelector := labels.SelectorFromSet(k8s.LabelsForNginx(nginx.Name))
	listOps := &client.ListOptions{Namespace: nginx.Namespace, LabelSelector: labelSelector}
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

func (r *ReconcileNginx) waitRollout(ctx context.Context, deploy *appv1.Deployment) error {
	fieldSelector := fields.OneTermEqualSelector("metadata.name", deploy.Name).String()

	rawClient, err := kubernetes.NewForConfig(r.mgr.GetConfig())
	if err != nil {
		return err
	}

	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return rawClient.Apps().Deployments(deploy.Namespace).List(metav1.ListOptions{
				FieldSelector: fieldSelector,
			})
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return rawClient.Apps().Deployments(deploy.Namespace).Watch(metav1.ListOptions{
				FieldSelector: fieldSelector,
			})
		},
	}

	preconditionFunc := func(store cache.Store) (bool, error) {
		_, exists, err := store.Get(&metav1.ObjectMeta{Namespace: deploy.Namespace, Name: deploy.Name})
		if err != nil {
			return true, err
		}
		if !exists {
			return true, errors.NewNotFound(appv1.Resource("deployment"), deploy.Name)
		}
		return false, nil
	}

	timeout := 5 * time.Minute
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err = watchtools.UntilWithSync(ctx, lw, deploy, preconditionFunc, func(e watch.Event) (bool, error) {
		switch t := e.Type; t {
		case watch.Added, watch.Modified:
			evtDeploy := e.Object.(*appv1.Deployment)
			if evtDeploy.Generation <= evtDeploy.Status.ObservedGeneration {
				// rollout is done
				return true, nil
			}
			return false, nil
		case watch.Deleted:
			return true, fmt.Errorf("object has been deleted")
		default:
			return true, fmt.Errorf("internal error: unexpected event %#v", e)
		}
	})
	return err
}
