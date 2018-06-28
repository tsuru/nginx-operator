#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

vendor/k8s.io/code-generator/generate-groups.sh \
all \
github.com/tsuru/nginx-operator/pkg/generated \
github.com/tsuru/nginx-operator/pkg/apis \
nginx:v1alpha1 \
--go-header-file "./tmp/codegen/boilerplate.go.txt"
