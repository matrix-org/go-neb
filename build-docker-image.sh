#!/bin/bash

set -e

docker run \
    --rm \
    -v "$PWD":/usr/src/myapp \
    -w /usr/src/myapp \
    golang:1.8-alpine \
    sh -c 'apk add --no-cache git gcc musl-dev && go get -u github.com/constabulary/gb/... && gb build -f github.com/matrix-org/go-neb'

docker build -t ${IMAGE_PREFIX}go-neb:latest .
