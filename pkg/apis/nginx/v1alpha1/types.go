package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type NginxList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Nginx `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Nginx struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              NginxSpec   `json:"spec"`
	Status            NginxStatus `json:"status,omitempty"`
}

type NginxSpec struct {
	// Number of desired pods. This is a pointer to distinguish between explicit
	// zero and not specified. Defaults to the default deployment replicas value.
	// +optional
	Replicas *int32
	// Docker image name. Defaults to "nginx:latest".
	// +optional
	Image string
	// Reference to the nginx config object.
	Config ConfigRef
}

type NginxStatus struct{}

// ConfigRef is a reference to a config object.
type ConfigRef struct {
	// Name of the config object.
	Name string
	// Kind of the config object. Defaults to ConfigKindConfigMap.
	Kind ConfigKind
}

type ConfigKind string

const (
	ConfigKindConfigMap = ConfigKind("ConfigMap")
)
