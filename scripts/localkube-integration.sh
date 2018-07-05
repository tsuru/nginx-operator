#!/bin/bash -xe

make build
kubectl create namespace nginx-operator-integration
kubectl apply -f deploy/ --namespace nginx-operator-integration
sleep 30s

NGINX_OPERATOR_INTEGRATION=1 go test ./...