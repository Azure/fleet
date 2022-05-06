include makefiles/dependency.mk

REGISTRY ?= ghcr.io
IMAGE_NAME := Azure/fleet
IMAGE_VERSION ?= v0.1.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

## --------------------------------------
## Linting
## --------------------------------------

.PHONY: lint
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run -v

.PHONY: lint-full
lint-full: $(GOLANGCI_LINT) ## Run slower linters to detect possible issues
	$(GOLANGCI_LINT) run -v --fast=false

## --------------------------------------
## Development
## --------------------------------------

staticcheck: staticchecktool
	$(STATICCHECK) ./...

.PHONY: fmt
fmt:  goimports ## Run go fmt against code.
	go fmt ./...
	$(GOIMPORTS) -local go.goms.io/fleet -w $$(go list -f {{.Dir}} ./...)

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: envtest## Run tests.
	CGO_ENABLED=1 go test ./... -race -coverprofile=coverage.txt -covermode=atomic -v

reviewable: fmt vet lint staticcheck
	go mod tidy

## TODO: make run/install/manifest

## --------------------------------------
## Images
## --------------------------------------

OUTPUT_TYPE ?= type=registry
BUILDX_BUILDER_NAME ?= img-builder
QEMU_VERSION ?= 5.2.0-2

.PHONY: docker-buildx-builder
docker-buildx-builder:
	@if ! docker buildx ls | grep $(BUILDX_BUILDER_NAME); then \
		docker run --rm --privileged multiarch/qemu-user-static:$(QEMU_VERSION) --reset -p yes; \
		docker buildx create --name $(BUILDX_BUILDER_NAME) --use; \
		docker buildx inspect $(BUILDX_BUILDER_NAME) --bootstrap; \
	fi

.PHONY: docker-build
docker-build: docker-buildx-builder
	docker buildx build \
		--file Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/amd64" \
		--pull \
		--tag $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_VERSION) .
