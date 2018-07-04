package test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testingNamespace = "nginx-operator-integration"

	testingEnvironment = "NGINX_OPERATOR_INTEGRATION"
)

func TestMain(m *testing.M) {
	if os.Getenv(testingEnvironment) == "" {
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func Test_Operator(t *testing.T) {
	if err := createNamespace(testingNamespace); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := deleteNamespace(testingNamespace); err != nil {
			t.Fatal(err)
		}
	}()

	t.Run("simple.yaml", func(t *testing.T) {
		if err := apply("testdata/simple.yaml", testingNamespace); err != nil {
			t.Error(err)
		}

		nginx, err := getReadyNginx("simple", 2, 1)
		assert.Nil(t, err)
		assert.Equal(t, 2, len(nginx.Status.Pods))
		assert.Equal(t, 1, len(nginx.Status.Services))
	})
}

func getReadyNginx(name string, expectedPods int, expectedSvcs int) (*v1alpha1.Nginx, error) {
	nginx := &v1alpha1.Nginx{TypeMeta: metav1.TypeMeta{Kind: "Nginx"}}
	timeout := time.After(10 * time.Second)
	for {
		if len(nginx.Status.Pods) == expectedPods && len(nginx.Status.Services) == expectedSvcs {
			return nginx, nil
		}
		if err := get(nginx, name); err != nil {
			return nil, err
		}
		select {
		case <-timeout:
			return nil, fmt.Errorf("Timeout waiting for nginx status. Last status: %v", nginx.Status)
		default:
		}
	}
}
