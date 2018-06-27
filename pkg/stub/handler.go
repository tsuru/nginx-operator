package stub

import (
	"context"

	"github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	"github.com/tsuru/nginx-operator/pkg/stub/k8s"

	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
)

func NewHandler() sdk.Handler {
	return &Handler{}
}

type Handler struct {
	// Fill me
}

// Handle handles events for the operator
func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *v1alpha1.Nginx:
		logger := logrus.WithFields(map[string]interface{}{
			"name":      o.GetName(),
			"namespace": o.GetNamespace(),
			"kind":      o.GetObjectKind(),
		})

		if event.Deleted {
			// Do nothing because garbage collector will remove created resources using the OwnerReference.
			// All secondary resources must have the CR set as their OwnerReference for this to be the case
			logger.Debug("object deleted")
			return nil
		}

		deployment := k8s.NewDeployment(o)

		err := sdk.Create(deployment)
		if err != nil && !errors.IsAlreadyExists(err) {
			logger.Errorf("Failed to create deployment: %v", err)
			return err
		}

		if err := sdk.Get(deployment); err != nil {
			logger.Errorf("Failed to retrieve deployment: %v", err)
			return err
		}

		// TODO: reconcile deployment fields with nginx fields
		// call sdk.Update if there were any changes

		if errors.IsAlreadyExists(err) {
			if err := sdk.Update(deployment); err != nil {
				logger.Errorf("Failed to update deployment: %v", err)
				return err
			}
		}
	}
	return nil
}
