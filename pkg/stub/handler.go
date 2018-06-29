package stub

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	"github.com/tsuru/nginx-operator/pkg/stub/k8s"

	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/sirupsen/logrus"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func NewHandler(logger *logrus.Logger) sdk.Handler {
	return &Handler{
		logger: logger,
	}
}

type Handler struct {
	logger *logrus.Logger
}

// Handle handles events for the operator
func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *v1alpha1.Nginx:
		logger := h.logger.WithFields(map[string]interface{}{
			"name":      o.GetName(),
			"namespace": o.GetNamespace(),
			"kind":      o.GetObjectKind().GroupVersionKind().String(),
		})

		logger.Debugf("Handling event for object: %+v", o)

		if err := reconcile(ctx, event, o, logger); err != nil {
			logger.Errorf("fail to reconcile: %v", err)
			return err
		}

		if err := refreshStatus(ctx, event, o, logger); err != nil {
			logger.Errorf("fail to refresh status: %v", err)
			return err
		}

	}
	return nil
}

func reconcile(ctx context.Context, event sdk.Event, nginx *v1alpha1.Nginx, logger *logrus.Entry) error {
	if event.Deleted {
		// Do nothing because garbage collector will remove created resources using the OwnerReference.
		// All secondary resources must have the CR set as their OwnerReference for this to be the case
		logger.Info("object deleted")
		return nil
	}

	if err := reconcileDeployment(ctx, nginx, logger); err != nil {
		return err
	}

	if err := reconcileService(ctx, nginx); err != nil {
		return err
	}

	return nil
}

func reconcileDeployment(ctx context.Context, nginx *v1alpha1.Nginx, logger *logrus.Entry) error {
	newDeploy, err := k8s.NewDeployment(nginx)
	if err != nil {
		return fmt.Errorf("failed to assemble deployment from nginx: %v", err)
	}

	err = sdk.Create(newDeploy)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create deployment: %v", err)
	}

	if err == nil {
		return nil
	}

	currDeploy := &appv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      newDeploy.Name,
			Namespace: newDeploy.Namespace,
		},
	}
	if err := sdk.Get(currDeploy); err != nil {
		return fmt.Errorf("failed to retrieve deployment: %v", err)
	}

	currSpec, err := k8s.ExtractNginxSpec(currDeploy.ObjectMeta)
	if err != nil {
		return fmt.Errorf("failed to extract nginx from deployment: %v", err)
	}

	if reflect.DeepEqual(nginx.Spec, currSpec) {
		logger.Debug("nothing changed")
		return nil
	}

	currDeploy.Spec = newDeploy.Spec
	if err := k8s.SetNginxSpec(&currDeploy.ObjectMeta, nginx.Spec); err != nil {
		return fmt.Errorf("failed to set nginx spec into object meta: %v", err)
	}

	if err := sdk.Update(currDeploy); err != nil {
		return fmt.Errorf("failed to update deployment: %v", err)
	}

	return nil
}

func reconcileService(ctx context.Context, nginx *v1alpha1.Nginx) error {
	service := k8s.NewService(nginx)

	err := sdk.Create(service)
	if errors.IsAlreadyExists(err) {
		return nil
	}

	return err
}

func refreshStatus(ctx context.Context, event sdk.Event, nginx *v1alpha1.Nginx, logger *logrus.Entry) error {
	if event.Deleted {
		logger.Debug("nginx deleted, skipping status update")
		return nil
	}

	pods, err := listPods(nginx)
	if err != nil {
		return fmt.Errorf("failed to list pods for nginx: %v", err)
	}

	services, err := listServices(nginx)
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
		err := sdk.Update(nginx)
		if err != nil {
			return fmt.Errorf("failed to update nginx status: %v", err)
		}
	}

	return nil
}

// listPods return all the pods for the given nginx sorted by name
func listPods(nginx *v1alpha1.Nginx) ([]v1alpha1.NginxPod, error) {
	podList := &corev1.PodList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
	}

	labelSelector := labels.SelectorFromSet(k8s.LabelsForNginx(nginx.Name)).String()
	listOps := &metav1.ListOptions{LabelSelector: labelSelector}
	err := sdk.List(nginx.Namespace, podList, sdk.WithListOptions(listOps))
	if err != nil {
		return nil, err
	}

	var pods []v1alpha1.NginxPod
	for _, p := range podList.Items {
		if p.Status.PodIP == "" {
			p.Status.PodIP = "<pending>"
		}
		pods = append(pods, v1alpha1.NginxPod{
			Name:  p.Name,
			PodIP: p.Status.PodIP,
		})
	}
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})

	return pods, nil
}

// listServices return all the services for the given nginx sorted by name
func listServices(nginx *v1alpha1.Nginx) ([]v1alpha1.NginxService, error) {
	serviceList := &corev1.ServiceList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
	}

	labelSelector := labels.SelectorFromSet(k8s.LabelsForNginx(nginx.Name)).String()
	listOps := &metav1.ListOptions{LabelSelector: labelSelector}
	err := sdk.List(nginx.Namespace, serviceList, sdk.WithListOptions(listOps))
	if err != nil {
		return nil, err
	}

	var services []v1alpha1.NginxService
	for _, s := range serviceList.Items {
		if s.Spec.ClusterIP == "" {
			s.Spec.ClusterIP = "<pending>"
		}
		services = append(services, v1alpha1.NginxService{
			Name:      s.Name,
			Type:      string(s.Spec.Type),
			ServiceIP: s.Spec.ClusterIP,
		})
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return services, nil
}
