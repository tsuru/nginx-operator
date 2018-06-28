// Code generated by informer-gen. DO NOT EDIT.

// This file was automatically generated by informer-gen

package v1alpha1

import (
	internalinterfaces "github.com/tsuru/nginx-operator/pkg/generated/informers/externalversions/internalinterfaces"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// Nginxes returns a NginxInformer.
	Nginxes() NginxInformer
}

type version struct {
	factory          internalinterfaces.SharedInformerFactory
	namespace        string
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory, namespace string, tweakListOptions internalinterfaces.TweakListOptionsFunc) Interface {
	return &version{factory: f, namespace: namespace, tweakListOptions: tweakListOptions}
}

// Nginxes returns a NginxInformer.
func (v *version) Nginxes() NginxInformer {
	return &nginxInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}
