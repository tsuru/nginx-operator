// Copyright 2020 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.currentReplicas,selectorpath=.status.podSelector
// +kubebuilder:printcolumn:name="Current",type=integer,JSONPath=`.status.currentReplicas`
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Nginx is the Schema for the nginxes API
type Nginx struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NginxSpec   `json:"spec,omitempty"`
	Status NginxStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NginxList contains a list of Nginx
type NginxList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Nginx `json:"items"`
}

// NginxSpec defines the desired state of Nginx
type NginxSpec struct {
	// Replicas is the number of desired pods. Defaults to the default deployment
	// replicas value.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// Image is the container image name. Defaults to "nginx:latest".
	// +optional
	Image string `json:"image,omitempty"`
	// Config is a reference to the NGINX config object which stores the NGINX
	// configuration file. When provided the file is mounted in NGINX container on
	// "/etc/nginx/nginx.conf".
	// +optional
	Config *ConfigRef `json:"config,omitempty"`
	// TLS configuration.
	// +optional
	TLS []NginxTLS `json:"tls,omitempty"`
	// Template used to configure the nginx pod.
	// +optional
	PodTemplate NginxPodTemplateSpec `json:"podTemplate,omitempty"`
	// Service to expose the nginx pod
	// +optional
	Service *NginxService `json:"service,omitempty"`
	// Ingress defines a convenient way to expose the Nginx service.
	// +optional
	Ingress *NginxIngress `json:"ingress,omitempty"`
	// ExtraFiles references to additional files into a object in the cluster.
	// These additional files will be mounted on `/etc/nginx/extra_files`.
	// +optional
	ExtraFiles *FilesRef `json:"extraFiles,omitempty"`
	// HealthcheckPath defines the endpoint used to check whether instance is
	// working or not.
	// +optional
	HealthcheckPath string `json:"healthcheckPath,omitempty"`
	// Resources requirements to be set on the NGINX container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// Cache allows configuring a cache volume for nginx to use.
	// +optional
	Cache NginxCacheSpec `json:"cache,omitempty"`
	// Lifecycle describes actions that should be executed when
	// some event happens to nginx container.
	// +optional
	Lifecycle *NginxLifecycle `json:"lifecycle,omitempty"`
}

type NginxTLS struct {
	// SecretName is the name of the Secret which contains the certificate-key
	// pair. It must reside in the same Namespace as the Nginx resource.
	//
	// NOTE: The Secret should follow the Kubernetes TLS secrets type.
	// More info: https://kubernetes.io/docs/concepts/configuration/secret/#tls-secrets.
	SecretName string `json:"secretName"`
	// Hosts are a list of hosts included in the TLS certificate. Defaults to the
	// wildcard of hosts: "*".
	// +optional
	Hosts []string `json:"hosts,omitempty"`
}

type NginxIngress struct {
	// Annotations are extra annotations for the Ingress resource.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// Labels are extra labels for the Ingress resource.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// IngressClassName is the class to be set on Ingress.
	// +optional
	IngressClassName *string `json:"ingressClassName,omitempty"`
}

type NginxService struct {
	// Type is the type of the service. Defaults to the default service type value.
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`
	// LoadBalancerIP is an optional load balancer IP for the service.
	// +optional
	LoadBalancerIP string `json:"loadBalancerIP,omitempty"`
	// Labels are extra labels for the service.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations are extra annotations for the service.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// ExternalTrafficPolicy defines whether external traffic will be routed to
	// node-local or cluster-wide endpoints. Defaults to the default Service
	// externalTrafficPolicy value.
	// +optional
	ExternalTrafficPolicy corev1.ServiceExternalTrafficPolicyType `json:"externalTrafficPolicy,omitempty"`
	// UsePodSelector defines whether Service should automatically map the
	// endpoints using the pod's label selector. Defaults to true.
	// +optional
	UsePodSelector *bool `json:"usePodSelector,omitempty"`
}

// ConfigRef is a reference to a config object.
type ConfigRef struct {
	// Kind of the config object. Defaults to "ConfigMap".
	Kind ConfigKind `json:"kind"`
	// Name of the ConfigMap object with "nginx.conf" key inside. It must reside
	// in the same Namespace as the Nginx resource. Required when Kind is "ConfigMap".
	//
	// It's mutually exclusive with Value field.
	// +optional
	Name string `json:"name,omitempty"`
	// Value is the raw Nginx configuration. Required when Kind is "Inline".
	//
	// It's mutually exclusive with Name field.
	// +optional
	Value string `json:"value,omitempty"`
}

type ConfigKind string

const (
	// ConfigKindConfigMap is a Kind of configuration that points to a configmap
	ConfigKindConfigMap = ConfigKind("ConfigMap")
	// ConfigKindInline is a kinda of configuration that is setup as a annotation on the Pod
	// and is inject as a file on the container using the Downward API.
	ConfigKindInline = ConfigKind("Inline")
)

// FilesRef is a reference to arbitrary files stored into a ConfigMap in the
// cluster.
type FilesRef struct {
	// Name points to a ConfigMap resource (in the same namespace) which holds
	// the files.
	Name string `json:"name"`
	// Files maps each key entry from the ConfigMap to its relative location on
	// the nginx filesystem.
	// +optional
	Files map[string]string `json:"files,omitempty"`
}

type NginxPodTemplateSpec struct {
	// Affinity to be set on the nginx pod.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// NodeSelector to be set on the nginx pod.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Annotations are custom annotations to be set into Pod.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// Labels are custom labels to be added into Pod.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// HostNetwork enabled causes the pod to use the host's network namespace.
	// +optional
	HostNetwork bool `json:"hostNetwork,omitempty"`
	// Ports is the list of ports used by nginx.
	// +optional
	Ports []corev1.ContainerPort `json:"ports,omitempty"`
	// TerminationGracePeriodSeconds defines the max duration seconds which the
	// pod needs to terminate gracefully. Defaults to pod's
	// terminationGracePeriodSeconds default value.
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// SecurityContext configures security attributes for the nginx pod.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// Volumes that will attach to nginx instances
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`
	// VolumeMounts will mount volume declared above in directories
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
	// InitContainers are executed in order prior to containers being started
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	InitContainers []corev1.Container `json:"initContainers,omitempty"`
}

type NginxCacheSpec struct {
	// InMemory if set to true creates a memory backed volume.
	InMemory bool `json:"inMemory,omitempty"`
	// Path is the mount path for the cache volume.
	Path string `json:"path"`
	// Size is the maximum size allowed for the cache volume.
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`
}

type NginxLifecycle struct {
	PostStart *NginxLifecycleHandler `json:"postStart,omitempty"`
	PreStop   *NginxLifecycleHandler `json:"preStop,omitempty"`
}

type NginxLifecycleHandler struct {
	Exec *corev1.ExecAction `json:"exec,omitempty"`
}

// NginxStatus defines the observed state of Nginx
type NginxStatus struct {
	// CurrentReplicas is the last observed number from the NGINX object.
	CurrentReplicas int32 `json:"currentReplicas,omitempty"`
	// PodSelector is the NGINX's pod label selector.
	PodSelector string `json:"podSelector,omitempty"`

	Deployments []DeploymentStatus `json:"deployments,omitempty"`
	Services    []ServiceStatus    `json:"services,omitempty"`
	Ingresses   []IngressStatus    `json:"ingresses,omitempty"`
}

type DeploymentStatus struct {
	// Name is the name of the Deployment created by nginx
	Name string `json:"name"`
}

type ServiceStatus struct {
	// Name is the name of the Service created by nginx
	Name string `json:"name"`
}

type IngressStatus struct {
	// Name is the name of the Ingress created by nginx
	Name string `json:"name"`
}

func init() {
	SchemeBuilder.Register(&Nginx{}, &NginxList{})
}
