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
	"github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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
	cleanup, outErr := createNamespace(testingNamespace)
	if outErr != nil {
		t.Fatal(outErr)
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

		nginx, err := getReadyNginx("my-secured-nginx", 1, 1)
		require.NoError(t, err)
		require.NotNil(t, nginx)
		assert.Equal(t, "nginx:alpine", nginx.Spec.Image)

		defer func() {
			err = delete("testdata/with-certificates.yaml", testingNamespace)
			require.NoError(t, err)
		}()

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
				filename:       "/etc/nginx/certs/rsa.crt",
				expectedSha256: "f50457089e715bbc9d5a31a16cf53cc2f13a68333df71559bb5d06be2d2b8a63",
			},
			{
				filename:       "/etc/nginx/certs/rsa.key",
				expectedSha256: "18580c25b2807b4c95502dd7051d414299e40d8d14024ad5d69c9915ec41e66e",
			},
			{
				filename:       "/etc/nginx/certs/custom_dir/custom_name.crt",
				expectedSha256: "159af275ab3b22d9737617e51daca64efafb48287ecb3650661d2116cb4ef0c9",
			},
			{
				filename:       "/etc/nginx/certs/custom_dir/custom_name.key",
				expectedSha256: "253b9795dcd80c493dcfade6b3dc5506fac1a38850abaa4e639fada5ea3dad5e",
			},
		}

		for _, tt := range tests {
			output, err := kubectl("exec", podName, "-n", testingNamespace, "-c", "nginx", "--", "sha256sum", tt.filename)
			require.NoError(t, err)
			assert.Contains(t, string(output), tt.expectedSha256)
		}
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
	pingMessage := "PING"
	for {
		output, err := kubectl("exec", name, "-n", namespace, "-c", "nginx", "--", "echo", "-n", pingMessage)
		if err == nil && string(output) == pingMessage {
			return nil
		}
		select {
		case <-timeout:
			return fmt.Errorf("Timeout waiting pod %q becomes available. Last output: %s. Last error: %v", name, string(output), err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
