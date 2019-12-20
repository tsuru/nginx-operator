TAG=latest
IMAGE=tsuru/nginx-operator

git_tag    := $(shell git describe --tags --abbrev=0 2>/dev/null || echo 'untagged')
git_commit := $(shell git rev-parse HEAD 2>/dev/null | cut -c1-7)

NGINX_OPERATOR_VERSION ?= $(git_tag)/$(git_commit)
GO_LDFLAGS ?= -X=github.com/tsuru/nginx-operator/version.Version=$(NGINX_OPERATOR_VERSION)

.PHONY: test deploy local build push generate lint deploy/crds

test:
	go test ./...

lint:
	go install ./...
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin
	golangci-lint run -c .golangci.yml ./...

deploy:
	kubectl apply -R -f deploy/

deploy/crds:
	kubectl apply -f deploy/crds/

local: deploy/crds
	operator-sdk up local --go-ldflags $(GO_LDFLAGS)

generate:
	operator-sdk generate k8s

build:
	operator-sdk build $(IMAGE):$(TAG) --go-build-args "-ldflags $(GO_LDFLAGS)"

push: build
	docker push $(IMAGE):$(TAG)
