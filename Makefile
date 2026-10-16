APP_NAME ?= northscope
IMAGE ?= ghcr.io/emircanagac/northscope:dev
KUBECONFIG ?= $(HOME)/.kube/config

.PHONY: ui-build build run docker

ui-build:
	npm --prefix ui run build

build: ui-build
	mkdir -p bin
	go build -o bin/$(APP_NAME) ./cmd/northscope

run: ui-build
	go run ./cmd/northscope -kubeconfig "$(KUBECONFIG)"

docker:
	docker build -t $(IMAGE) .
