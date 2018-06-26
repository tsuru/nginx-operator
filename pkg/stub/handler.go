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

func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *v1alpha1.Nginx:
		err := sdk.Create(k8s.NewDeployment(o))
		if err != nil && !errors.IsAlreadyExists(err) {
			logrus.Errorf("Failed to create deployment: %v", err)
			return err
		}
	}
	return nil
}
