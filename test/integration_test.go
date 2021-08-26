// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/tsuru/nginx-operator/api/v1alpha1"
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
	cleanup, outErr := createNamespace(testingNamespace)
	if outErr != nil {
		require.NoError(t, outErr)
	}
	defer cleanup()

	t.Run("simple.yaml", func(t *testing.T) {
		if err := apply("testdata/simple.yaml", testingNamespace); err != nil {
			t.Error(err)
		}

		nginx, err := getReadyNginx("simple", 2, 1)
		require.NoError(t, err)
		require.NotNil(t, nginx)
		assert.Equal(t, 2, len(nginx.Status.Pods))
		assert.Equal(t, 1, len(nginx.Status.Services))
	})

	t.Run("with-certificates.yaml", func(t *testing.T) {
		err := apply("testdata/with-certificates.yaml", testingNamespace)
		require.NoError(t, err)

		defer func() {
			err = delete("testdata/with-certificates.yaml", testingNamespace)
			require.NoError(t, err)
		}()

		nginx, err := getReadyNginx("my-secured-nginx", 1, 1)
		require.NoError(t, err)
		require.NotNil(t, nginx)
		assert.Equal(t, "nginx:stable-alpine", nginx.Spec.Image)
		assert.Equal(t, "/healthz", nginx.Spec.HealthcheckPath)

		nginxService := corev1.Service{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"}}
		err = get(&nginxService, "my-secured-nginx-service", nginx.Namespace)
		require.NoError(t, err)
		assert.NotNil(t, nginxService)
		assert.Equal(t, int32(80), nginxService.Spec.Ports[0].Port)
		assert.Equal(t, int32(443), nginxService.Spec.Ports[1].Port)

		podName := nginx.Status.Pods[0].Name
		err = waitPodBeAvailable(podName, testingNamespace)
		require.NoError(t, err)

		tests := []struct {
			filename       string
			expectedSha256 string
		}{
			{
				filename:       "/etc/nginx/certs/my-rsa-cert/tls.crt",
				expectedSha256: "6a95f3b95972f4beae4b54918498817d95f3d8cfb766c053cae656839e392430",
			},
			{
				filename:       "/etc/nginx/certs/my-rsa-cert/tls.key",
				expectedSha256: "f8a6da5db392519513597345931237dbb1a0a28fb89b7ce3b41562f988f9ef67",
			},
			{
				filename:       "/etc/nginx/certs/my-ecdsa-cert/tls.crt",
				expectedSha256: "358687f51dc536333a5c6746fca484838e465aa9d23c872d5a6cffc2767ef089",
			},
			{
				filename:       "/etc/nginx/certs/my-ecdsa-cert/tls.key",
				expectedSha256: "251024281967b22081c2e62cdf9b858cc46a7e8320ede8b19a288499612f0688",
			},
		}

		for _, tt := range tests {
			output, err := kubectl("exec", podName, "-n", testingNamespace, "-c", "nginx", "--", "sha256sum", tt.filename)
			require.NoError(t, err)
			assert.Contains(t, string(output), tt.expectedSha256)
		}
	})

	t.Run("testdata/with-autoscaling.yaml", func(t *testing.T) {
		err := apply("testdata/with-autoscaling.yaml", testingNamespace)
		require.NoError(t, err)

		defer func() {
			err = delete("testdata/with-autoscaling.yaml", testingNamespace)
			require.NoError(t, err)
		}()

		nginx, err := getReadyNginx("my-autoscaled-nginx", 1, 1)
		require.NoError(t, err)
		require.NotNil(t, nginx)

		_, err = kubectl("scale", "nginx", "my-autoscaled-nginx", "-n", testingNamespace, "--replicas", "5")
		assert.NoError(t, err)

		_, err = getReadyNginx("my-autoscaled-nginx", 5, 1)
		require.NoError(t, err)
	})
}

func getReadyNginx(name string, expectedPods int, expectedSvcs int) (*v1alpha1.Nginx, error) {
	nginx := &v1alpha1.Nginx{TypeMeta: metav1.TypeMeta{Kind: "Nginx"}}
	timeout := time.After(60 * time.Second)
	for {
		err := get(nginx, name, testingNamespace)
		if err != nil {
			fmt.Printf("Err getting nginx %q: %v. Retrying...\n", name, err)
		}
		if len(nginx.Status.Pods) == expectedPods && len(nginx.Status.Services) == expectedSvcs {
			return nginx, nil
		}
		select {
		case <-timeout:
			return nil, fmt.Errorf("Timeout waiting for nginx status. Last nginx object: %#v. Last error: %v", nginx, err)
		case <-time.After(time.Millisecond * 100):
		}
	}
}

func waitPodBeAvailable(name, namespace string) error {
	timeout := time.After(5 * time.Minute)
	for {
		output, err := kubectl("wait", "-n", namespace, "pod", name, "--for", "condition=Ready=true")
		if err == nil {
			return nil
		}
		select {
		case <-timeout:
			return fmt.Errorf("Timeout waiting pod %q becomes available. Last output: %s. Last error: %v", name, string(output), err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
