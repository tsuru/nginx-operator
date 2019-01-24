#!/bin/bash -xe

# Download kubectl, which is a requirement for using minikube.
curl -Lo kubectl https://storage.googleapis.com/kubernetes-release/release/v1.9.0/bin/linux/amd64/kubectl && chmod +x kubectl && sudo mv kubectl /usr/local/bin/
# Download minikube.
curl -Lo minikube https://storage.googleapis.com/minikube/releases/v0.25.2/minikube-linux-amd64 && chmod +x minikube && sudo mv minikube /usr/local/bin/
sudo minikube start --vm-driver=none --kubernetes-version=$KUBERNETES_VERSION
# Fix the kubectl context, as it's often stale.
minikube update-context
# Wait for Kubernetes to be up and ready.
JSONPATH='{range .items[*]}{@.metadata.name}:{range @.status.conditions[*]}{@.type}={@.status};{end}{end}'; until kubectl get nodes -o jsonpath="$JSONPATH" 2>&1 | grep -q "Ready=True"; do sleep 1; done

make build
kubectl create namespace nginx-operator-integration
sed -ie 's/imagePullPolicy: Always/imagePullPolicy: Never/g' deploy/operator.yaml
kubectl apply -f deploy/crds/*_crd.yaml
kubectl apply -f deploy/ --namespace nginx-operator-integration
sleep 30s
kubectl get deployment --all-namespaces
kubectl get pods --all-namespaces
NGINX_OPERATOR_INTEGRATION=1 go test ./...