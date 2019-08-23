TAG=latest
IMAGE=tsuru/nginx-operator
SIDECAR_IMAGE=tsuru/nginx-operator-sidecar

.PHONY: test deploy local build push generate lint

test:
	go test ./...

lint:
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin
	golangci-lint run ./...

deploy:
	kubectl apply -f deploy/

local: deploy
	operator-sdk up local

generate:
	operator-sdk generate k8s

build:
	operator-sdk build $(IMAGE):$(TAG)

build-sidecar:
	docker build -t $(SIDECAR_IMAGE):$(TAG) ./nginx-sidecar/

push: build build-sidecar
	docker push $(IMAGE):$(TAG)
	docker push $(SIDECAR_IMAGE):$(TAG)
