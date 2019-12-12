package endpoints

import (
	"context"
	"reflect"
	"time"

	"github.com/tsuru/nginx-operator/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubernetes/pkg/api/v1/endpoints"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_nginx")

type reconcileEndpoints struct {
	mgr      manager.Manager
	recorder record.EventRecorder
}

var _ reconcile.Reconciler = &reconcileEndpoints{}

func Add(mgr manager.Manager) error {
	r := &reconcileEndpoints{
		mgr:      mgr,
		recorder: mgr.GetRecorder("nginx-endpoints-controller"),
	}

	c, err := controller.New("nginx-endpoints-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	err = c.Watch(
		&source.Kind{Type: &corev1.Pod{}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: &podToServiceMap{client: mgr.GetClient()},
		},
		podChangedPredicate{},
	)
	if err != nil {
		return err
	}

	err = c.Watch(
		&source.Kind{Type: &corev1.Service{}},
		&handler.EnqueueRequestForObject{},
		svcLabelPredicate{},
	)
	return err
}

type svcLabelPredicate struct{}

func (p svcLabelPredicate) Create(e event.CreateEvent) bool {
	return isCustomEndpointsService(e.Meta)
}

func (p svcLabelPredicate) Delete(e event.DeleteEvent) bool {
	return isCustomEndpointsService(e.Meta)
}

func (p svcLabelPredicate) Update(e event.UpdateEvent) bool {
	return isCustomEndpointsService(e.MetaOld) || isCustomEndpointsService(e.MetaNew)
}

func (p svcLabelPredicate) Generic(e event.GenericEvent) bool {
	return isCustomEndpointsService(e.Meta)
}

func isCustomEndpointsService(meta metav1.Object) bool {
	if meta == nil {
		return false
	}
	labels := meta.GetLabels()
	if labels == nil {
		return false
	}
	_, ok1 := labels[k8s.LabelNginxCustomEndpoints]
	_, ok2 := labels[k8s.LabelNginxApp]
	_, ok3 := labels[k8s.LabelNginxResourceName]
	return ok1 && ok2 && ok3
}

type podChangedPredicate struct {
	predicate.Funcs
}

func (podChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.MetaOld == nil || e.MetaNew == nil {
		return false
	}
	if e.MetaOld.GetResourceVersion() == e.MetaNew.GetResourceVersion() {
		return false
	}
	oldPod, ok := e.ObjectOld.(*corev1.Pod)
	if !ok {
		return false
	}
	newPod, ok := e.ObjectNew.(*corev1.Pod)
	if !ok {
		return false
	}

	podChangedFlag := podChanged(oldPod, newPod)

	// Check if the pod labels have changed, indicating a possible
	// change in the service membership
	labelsChanged := false
	if !reflect.DeepEqual(newPod.Labels, oldPod.Labels) ||
		!hostNameAndDomainAreEqual(newPod, oldPod) {
		labelsChanged = true
	}

	// If both the pod and labels are unchanged, no update is needed
	if !podChangedFlag && !labelsChanged {
		return false
	}

	return true
}

func podChanged(oldPod, newPod *corev1.Pod) bool {
	// If the pod's deletion timestamp is set, remove endpoint from ready address.
	if newPod.DeletionTimestamp != oldPod.DeletionTimestamp {
		return true
	}
	// If the pod's readiness has changed, the associated endpoint address
	// will move from the unready endpoints set to the ready endpoints.
	// So for the purposes of an endpoint, a readiness change on a pod
	// means we have a changed pod.
	if podutil.IsPodReady(oldPod) != podutil.IsPodReady(newPod) {
		return true
	}
	// Convert the pod to an EndpointAddress, clear inert fields,
	// and see if they are the same.
	newEndpointAddress := podToEndpointAddress(newPod)
	oldEndpointAddress := podToEndpointAddress(oldPod)
	// Ignore the ResourceVersion because it changes
	// with every pod update. This allows the comparison to
	// show equality if all other relevant fields match.
	newEndpointAddress.TargetRef.ResourceVersion = ""
	oldEndpointAddress.TargetRef.ResourceVersion = ""

	return !reflect.DeepEqual(newEndpointAddress, oldEndpointAddress)
}

type podToServiceMap struct {
	client client.Client
}

func (m *podToServiceMap) Map(o handler.MapObject) []reconcile.Request {
	req, err := m.doMap(o)
	if err != nil {
		log.Error(err, "unable to map pod to service")
		return nil
	}
	return req
}

func (m *podToServiceMap) doMap(o handler.MapObject) ([]reconcile.Request, error) {
	podLabels := o.Meta.GetLabels()

	var svcList corev1.ServiceList
	err := m.client.List(context.Background(), &client.ListOptions{}, &svcList)
	if err != nil {
		return nil, err
	}

	var requests []reconcile.Request
	for _, service := range svcList.Items {
		if service.Spec.Selector == nil {
			continue
		}
		if !isCustomEndpointsService(service.GetObjectMeta()) {
			continue
		}

		nginxName := k8s.GetNginxNameFromObject(service.GetObjectMeta())
		selector := labels.Set(k8s.LabelsForNginx(nginxName)).AsSelectorPreValidated()
		if selector.Matches(labels.Set(podLabels)) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: service.Namespace,
					Name:      service.Name,
				},
			})
		}
	}
	return requests, nil
}

func (r *reconcileEndpoints) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	startTime := time.Now()
	defer func() {
		log.V(4).Info("Finished syncing endpoints.", "service", request.NamespacedName, "duration", time.Since(startTime))
	}()

	namespace, name := request.Namespace, request.Name
	cli := r.mgr.GetClient()

	ctx := context.Background()

	var service *corev1.Service
	err := cli.Get(ctx, request.NamespacedName, service)
	if err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}

		endpoint := corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		}
		err = cli.Delete(ctx, &endpoint)
		if err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	nginxName := k8s.GetNginxNameFromObject(service.GetObjectMeta())
	selector := labels.Set(k8s.LabelsForNginx(nginxName)).AsSelectorPreValidated()

	log.V(5).Info("About to update endpoints", "service", request.NamespacedName)
	var podList *corev1.PodList
	err = cli.List(ctx, &client.ListOptions{
		LabelSelector: selector,
	}, podList)
	if err != nil {
		return reconcile.Result{}, err
	}

	subsets := []corev1.EndpointSubset{}
	var totalReadyEps int
	var totalNotReadyEps int

	for _, pod := range podList.Items {
		if len(pod.Status.PodIP) == 0 {
			log.V(5).Info("Failed to find an IP", "pod namespace", pod.Namespace, "pod name", pod.Name)
			continue
		}
		// We allow even pods with DeletionTimestamp to proceed

		epa := *podToEndpointAddress(&pod)

		hostname := pod.Spec.Hostname
		if len(hostname) > 0 && pod.Spec.Subdomain == service.Name && service.Namespace == pod.Namespace {
			epa.Hostname = hostname
		}

		for i := range service.Spec.Ports {
			servicePort := &service.Spec.Ports[i]

			portName := servicePort.Name
			portProto := servicePort.Protocol
			var portNum int
			portNum, err = podutil.FindPort(&pod, servicePort)
			if err != nil {
				log.V(4).Info("Failed to find port", "service", request.NamespacedName, "err", err)
				continue
			}

			var readyEps, notReadyEps int
			epp := &v1.EndpointPort{Name: portName, Port: int32(portNum), Protocol: portProto}
			subsets, readyEps, notReadyEps = addEndpointSubset(subsets, &pod, epa, epp)
			totalReadyEps = totalReadyEps + readyEps
			totalNotReadyEps = totalNotReadyEps + notReadyEps
		}
	}
	subsets = endpoints.RepackSubsets(subsets)

	// See if there's actually an update here.
	var currentEndpoints *corev1.Endpoints
	err = cli.Get(ctx, types.NamespacedName{
		Namespace: service.Namespace,
		Name:      service.Name,
	}, currentEndpoints)
	if err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		currentEndpoints = &v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      service.Name,
				Namespace: service.Namespace,
				Labels:    service.Labels,
			},
		}
	}

	createEndpoints := len(currentEndpoints.ResourceVersion) == 0

	if !createEndpoints &&
		apiequality.Semantic.DeepEqual(currentEndpoints.Subsets, subsets) &&
		apiequality.Semantic.DeepEqual(currentEndpoints.Labels, service.Labels) {
		log.V(5).Info("endpoints are equal, skipping update", "service", request.NamespacedName)
		return reconcile.Result{}, nil
	}
	newEndpoints := currentEndpoints.DeepCopy()
	newEndpoints.Subsets = subsets
	newEndpoints.Labels = service.Labels
	if newEndpoints.Annotations == nil {
		newEndpoints.Annotations = make(map[string]string)
	}

	log.V(4).Info("Update endpoints", "service", request.NamespacedName, "ready", totalReadyEps, "not ready", totalNotReadyEps)
	if createEndpoints {
		// No previous endpoints, create them
		err = cli.Create(ctx, newEndpoints)
	} else {
		// Pre-existing
		err = cli.Update(ctx, newEndpoints)
	}
	if err != nil {
		if createEndpoints && errors.IsForbidden(err) {
			// A request is forbidden primarily for two reasons:
			// 1. namespace is terminating, endpoint creation is not allowed by default.
			// 2. policy is misconfigured, in which case no service would function anywhere.
			// Given the frequency of 1, we log at a lower level.
			log.V(5).Info("Forbidden from creating endpoints", "err", err)
		}

		if createEndpoints {
			r.recorder.Eventf(newEndpoints, v1.EventTypeWarning, "FailedToCreateEndpoint", "Failed to create endpoint for service %v/%v: %v", service.Namespace, service.Name, err)
		} else {
			r.recorder.Eventf(newEndpoints, v1.EventTypeWarning, "FailedToUpdateEndpoint", "Failed to update endpoint %v/%v: %v", service.Namespace, service.Name, err)
		}

		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func addEndpointSubset(subsets []corev1.EndpointSubset, pod *corev1.Pod, epa corev1.EndpointAddress,
	epp *corev1.EndpointPort) ([]corev1.EndpointSubset, int, int) {
	var readyEps int
	var notReadyEps int
	ports := []corev1.EndpointPort{}
	if epp != nil {
		ports = append(ports, *epp)
	}
	if podutil.IsPodReady(pod) {
		subsets = append(subsets, corev1.EndpointSubset{
			Addresses: []corev1.EndpointAddress{epa},
			Ports:     ports,
		})
		readyEps++
	} else if shouldPodBeInEndpoints(pod) {
		log.V(5).Info("Pod is out of service", "pod namespace", pod.Namespace, "pod name", pod.Name)
		subsets = append(subsets, corev1.EndpointSubset{
			NotReadyAddresses: []corev1.EndpointAddress{epa},
			Ports:             ports,
		})
		notReadyEps++
	}
	return subsets, readyEps, notReadyEps
}

func shouldPodBeInEndpoints(pod *corev1.Pod) bool {
	switch pod.Spec.RestartPolicy {
	case corev1.RestartPolicyNever:
		return pod.Status.Phase != corev1.PodFailed && pod.Status.Phase != corev1.PodSucceeded
	case corev1.RestartPolicyOnFailure:
		return pod.Status.Phase != corev1.PodSucceeded
	default:
		return true
	}
}

func podToEndpointAddress(pod *corev1.Pod) *corev1.EndpointAddress {
	return &corev1.EndpointAddress{
		IP:       pod.Status.PodIP,
		NodeName: &pod.Spec.NodeName,
		TargetRef: &corev1.ObjectReference{
			Kind:            "Pod",
			Namespace:       pod.ObjectMeta.Namespace,
			Name:            pod.ObjectMeta.Name,
			UID:             pod.ObjectMeta.UID,
			ResourceVersion: pod.ObjectMeta.ResourceVersion,
		}}
}

func hostNameAndDomainAreEqual(pod1, pod2 *corev1.Pod) bool {
	return pod1.Spec.Hostname == pod2.Spec.Hostname &&
		pod1.Spec.Subdomain == pod2.Spec.Subdomain
}
