SHELL := /bin/bash

ROOT := $(shell git rev-parse --show-toplevel)

VERSION ?= $(shell git describe --dirty="-dev")

DOCKER_IMG ?= form3tech/kube-ecr-refresher
DOCKER_TAG ?= $(VERSION)

.PHONY: docker.build
docker.build:
	docker build -t $(DOCKER_IMG):$(DOCKER_TAG) $(ROOT)

.PHONY: docker.push
docker.push: docker.build
	docker push $(DOCKER_IMG):$(DOCKER_TAG)

.PHONY: install-golangci-lint
install-golangci-lint:
	curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s -- -b $$(go env GOPATH)/bin latest

.PHONY: install-deps
install-deps: install-golangci-lint

.PHONY: lint
lint:
	golangci-lint run ./... --enable-all --disable lll,wsl --deadline 5m

.PHONY: skaffold
skaffold:
	skaffold dev -f $(ROOT)/hack/skaffold/skaffold.yaml
