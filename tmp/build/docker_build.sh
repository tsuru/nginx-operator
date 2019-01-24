#!/usr/bin/env bash

if ! which docker > /dev/null; then
	echo "docker needs to be installed"
	exit 1
fi

: ${IMAGE:?"Need to set IMAGE, e.g. gcr.io/<repo>/<your>-operator"}
TAG=${TAG:-latest}

echo "building container ${IMAGE}:${TAG}..."
docker build -t "${IMAGE}:${TAG}" -f tmp/build/Dockerfile .
