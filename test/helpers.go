package test

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
)

func createNamespace(ns string) error {
	if out, err := kubectl("create", "namespace", ns); err != nil {
		if strings.Contains(string(out), "AlreadyExists") {
			return nil
		}
		return fmt.Errorf("failed to create namespace %q: %v - out: %v", ns, err, string(out))
	}
	return nil
}

func deleteNamespace(ns string) error {
	if _, err := kubectl("delete", "namespace", ns); err != nil {
		return fmt.Errorf("failed to delete namespace %q: %v", ns, err)
	}
	return nil
}

func apply(file string, ns string) error {
	if _, err := kubectl("apply", "-f", file, "--namespace", ns); err != nil {
		return fmt.Errorf("failed to apply %q: %v", file, err)
	}
	return nil
}

func delete(file string, ns string) error {
	if _, err := kubectl("delete", "-f", file, "--namespace", ns); err != nil {
		return fmt.Errorf("failed to apply %q: %v", file, err)
	}
	return nil
}

func get(obj runtime.Object, name string) error {
	out, err := kubectl("get", obj.GetObjectKind().GroupVersionKind().Kind, "-o", "json", name)
	if err != nil {
		return err
	}
	return json.Unmarshal(out, obj)
}

func kubectl(arg ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.TODO(), "kubectl", arg...)
	fmt.Printf("Running %v\n", cmd)
	return cmd.CombinedOutput()
}
