module github.com/tsuru/nginx-operator

go 1.16

require (
	github.com/go-logr/logr v0.4.0
	github.com/stretchr/testify v1.6.1
	go.uber.org/zap v1.16.0
	k8s.io/api v0.21.0
	k8s.io/apimachinery v0.21.0
	k8s.io/client-go v0.21.0
	sigs.k8s.io/controller-runtime v0.9.0-beta.2
)
