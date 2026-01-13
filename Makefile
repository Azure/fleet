REGISTRY ?= ghcr.io
KIND_IMAGE ?= kindest/node:v1.33.4
ifndef TAG
	TAG ?= $(shell git rev-parse --short=7 HEAD)
endif
HUB_AGENT_IMAGE_VERSION ?= $(TAG)
MEMBER_AGENT_IMAGE_VERSION ?= $(TAG)
REFRESH_TOKEN_IMAGE_VERSION ?= $(TAG)
CRD_INSTALLER_IMAGE_VERSION ?= $(TAG)

HUB_AGENT_IMAGE_NAME ?= hub-agent
MEMBER_AGENT_IMAGE_NAME ?= member-agent
REFRESH_TOKEN_IMAGE_NAME ?= refresh-token
CRD_INSTALLER_IMAGE_NAME ?= crd-installer
ARC_MEMBER_AGENT_HELMCHART_NAME = arc-member-cluster-agents-helm-chart

TARGET_OS ?= linux
TARGET_ARCH ?= amd64
# Note (chenyu1): switch to the `plain` progress type to see the full outputs in the docker build
# progress.
BUILDKIT_PROGRESS_TYPE ?= auto

TARGET_OS ?= linux
TARGET_ARCH ?= amd64
AUTO_DETECT_ARCH ?= TRUE

# Auto-detect system architecture if it is allowed and the necessary commands are available on the system.
ifeq ($(AUTO_DETECT_ARCH), TRUE)
ARCH_CMD_INSTALLED := $(shell command -v arch 2>/dev/null)
ifdef ARCH_CMD_INSTALLED
TARGET_ARCH := $(shell arch)
# The arch command may return arch strings that are aliases of expected TARGET_ARCH values;
# do the mapping here.
ifeq ($(TARGET_ARCH),$(filter $(TARGET_ARCH),x86_64))
	TARGET_ARCH := amd64
else ifeq ($(TARGET_ARCH),$(filter $(TARGET_ARCH),aarch64 arm))
	TARGET_ARCH := arm64
endif
$(info Auto-detected system architecture: $(TARGET_ARCH))
endif
endif

# Note (chenyu1): switch to the `plain` progress type to see the full outputs in the docker build
# progress.
BUILDKIT_PROGRESS_TYPE ?= auto

KUBECONFIG ?= $(HOME)/.kube/config
HUB_SERVER_URL ?= https://172.19.0.2:6443

HUB_KIND_CLUSTER_NAME = hub-testing
MEMBER_KIND_CLUSTER_NAME = member-testing
MEMBER_CLUSTER_COUNT ?= 3

# Directories
ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(abspath $(TOOLS_DIR)/bin)

# Binaries
# Note: Need to use abspath so we can invoke these from subdirectories

CONTROLLER_GEN_VER := v0.16.0
CONTROLLER_GEN_BIN := controller-gen
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/$(CONTROLLER_GEN_BIN)-$(CONTROLLER_GEN_VER))

STATICCHECK_VER := master
STATICCHECK_BIN := staticcheck
STATICCHECK := $(abspath $(TOOLS_BIN_DIR)/$(STATICCHECK_BIN)-$(STATICCHECK_VER))

GOIMPORTS_VER := latest
GOIMPORTS_BIN := goimports
GOIMPORTS := $(abspath $(TOOLS_BIN_DIR)/$(GOIMPORTS_BIN)-$(GOIMPORTS_VER))

GOLANGCI_LINT_VER := v1.64.7
GOLANGCI_LINT_BIN := golangci-lint
GOLANGCI_LINT := $(abspath $(TOOLS_BIN_DIR)/$(GOLANGCI_LINT_BIN)-$(GOLANGCI_LINT_VER))

# ENVTEST_K8S_VERSION refers to the version of k8s binary assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.30.0
# ENVTEST_VER is the version of the ENVTEST binary
ENVTEST_VER = v0.0.0-20240317073005-bd9ea79e8d18
ENVTEST_BIN := setup-envtest
ENVTEST := $(abspath $(TOOLS_BIN_DIR)/$(ENVTEST_BIN)-$(ENVTEST_VER))

PROTOC_GEN_GO_VER := v1.36.10
PROTOC_GEN_GO_BIN := protoc-gen-go
PROTOC_GEN_GO := $(abspath $(TOOLS_BIN_DIR)/$(PROTOC_GEN_GO_BIN)-$(PROTOC_GEN_GO_VER))

PROTOC_GEN_GO_GRPC_VER := v1.5.1
PROTOC_GEN_GO_GRPC_BIN := protoc-gen-go-grpc
PROTOC_GEN_GO_GRPC := $(abspath $(TOOLS_BIN_DIR)/$(PROTOC_GEN_GO_GRPC_BIN)-$(PROTOC_GEN_GO_GRPC_VER))

PROTOC_GEN_GRPC_GATEWAY_VER := v2.27.3
PROTOC_GEN_GRPC_GATEWAY_BIN := protoc-gen-grpc-gateway
PROTOC_GEN_GRPC_GATEWAY := $(abspath $(TOOLS_BIN_DIR)/$(PROTOC_GEN_GRPC_GATEWAY_BIN)-$(PROTOC_GEN_GRPC_GATEWAY_VER))

PROTOC_VER := 28.0
PROTOC_BIN := protoc
PROTOC := $(abspath $(TOOLS_BIN_DIR)/$(PROTOC_BIN)-$(PROTOC_VER))

# Scripts
GO_INSTALL := ./hack/go-install.sh

## --------------------------------------
## Tooling Binaries
## --------------------------------------

$(GOLANGCI_LINT):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) github.com/golangci/golangci-lint/cmd/golangci-lint $(GOLANGCI_LINT_BIN) $(GOLANGCI_LINT_VER)

$(CONTROLLER_GEN):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) sigs.k8s.io/controller-tools/cmd/controller-gen $(CONTROLLER_GEN_BIN) $(CONTROLLER_GEN_VER)

# Style checks
$(STATICCHECK):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) honnef.co/go/tools/cmd/staticcheck $(STATICCHECK_BIN) $(STATICCHECK_VER)

# GOIMPORTS
$(GOIMPORTS):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) golang.org/x/tools/cmd/goimports $(GOIMPORTS_BIN) $(GOIMPORTS_VER)

# ENVTEST
$(ENVTEST):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) sigs.k8s.io/controller-runtime/tools/setup-envtest $(ENVTEST_BIN) $(ENVTEST_VER)

# PROTOC_GEN_GO
$(PROTOC_GEN_GO):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) google.golang.org/protobuf/cmd/protoc-gen-go $(PROTOC_GEN_GO_BIN) $(PROTOC_GEN_GO_VER)

# PROTOC_GEN_GO_GRPC
$(PROTOC_GEN_GO_GRPC):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) google.golang.org/grpc/cmd/protoc-gen-go-grpc $(PROTOC_GEN_GO_GRPC_BIN) $(PROTOC_GEN_GO_GRPC_VER)

# PROTOC_GEN_GRPC_GATEWAY
$(PROTOC_GEN_GRPC_GATEWAY):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway $(PROTOC_GEN_GRPC_GATEWAY_BIN) $(PROTOC_GEN_GRPC_GATEWAY_VER)

# PROTOC
$(PROTOC):
	curl -L -o $(TOOLS_BIN_DIR)/protoc.zip https://github.com/protocolbuffers/protobuf/releases/download/v$(PROTOC_VER)/protoc-$(PROTOC_VER)-linux-x86_64.zip && \
	unzip $(TOOLS_BIN_DIR)/protoc.zip -d $(TOOLS_BIN_DIR)/protoc_tmp && mv $(TOOLS_BIN_DIR)/protoc_tmp/bin/protoc $(PROTOC) && rm -rf $(TOOLS_BIN_DIR)/protoc.zip $(TOOLS_BIN_DIR)/protoc_tmp

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


## --------------------------------------
## Linting
## --------------------------------------

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run fast linting
	$(GOLANGCI_LINT) run -v

.PHONY: lint-full
lint-full: $(GOLANGCI_LINT) ## Run slower linters to detect possible issues
	$(GOLANGCI_LINT) run -v --fast=false

## --------------------------------------
## Development
## --------------------------------------

staticcheck: $(STATICCHECK) ## Run static analysis
	$(STATICCHECK) ./...

.PHONY: fmt
fmt:  $(GOIMPORTS) ## Run go fmt against code
	go fmt ./...
	$(GOIMPORTS) -local go.goms.io/fleet -w $$(go list -f {{.Dir}} ./...)

.PHONY: vet
vet: ## Run go vet against code
	go vet ./...

## --------------------------------------
## test
## --------------------------------------

.PHONY: test
test: manifests generate fmt vet local-unit-test integration-test ## Run unit tests and integration tests

##
# Set up the timeout parameters as some of the tests (rollout controller) lengths have exceeded the default 10 minute mark.
# TO-DO (chenyu1): enable parallelization for single package integration tests.
.PHONY: local-unit-test
local-unit-test: $(ENVTEST) ## Run unit tests
	export CGO_ENABLED=1 && \
	export KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" && \
	go test `go list ./pkg/... ./cmd/...` -race -coverpkg=./...  -coverprofile=ut-coverage.xml -covermode=atomic -v -timeout=30m

.PHONY: integration-test
integration-test: $(ENVTEST) ## Run integration tests
	export CGO_ENABLED=1 && \
	export KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" && \
	ginkgo -v -p --race --cover --coverpkg=./pkg/scheduler/... ./test/scheduler && \
	ginkgo -v -p --race --cover --coverpkg=./... ./test/apis/...

## local tests & e2e tests

# E2E test label filter (can be overridden)
LABEL_FILTER ?= !custom

.PHONY: e2e-tests
e2e-tests: setup-clusters ## Run E2E tests
	cd ./test/e2e && ginkgo --timeout=70m --label-filter="$(LABEL_FILTER)" -v -p .

e2e-tests-custom: setup-clusters ## Run custom E2E tests with labels
	cd ./test/e2e && ginkgo --label-filter="custom" -v -p . 

.PHONY: setup-clusters
setup-clusters: ## Set up Kind clusters for E2E testing
	cd ./test/e2e && chmod +x ./setup.sh && ./setup.sh $(MEMBER_CLUSTER_COUNT)

.PHONY: collect-e2e-logs
collect-e2e-logs: ## Collect logs from hub and member agent pods after e2e tests
	cd ./test/e2e && chmod +x ./collect-logs.sh && ./collect-logs.sh $(MEMBER_CLUSTER_COUNT)

## reviewable
.PHONY: reviewable
reviewable: fmt vet lint staticcheck ## Run all quality checks before PR
	go mod tidy

## --------------------------------------
## Code Generation
## --------------------------------------

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd"

# Generate manifests e.g. CRD, RBAC etc.
.PHONY: manifests
manifests: $(CONTROLLER_GEN) ## Generate CRDs and manifests
	$(CONTROLLER_GEN) \
		$(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./apis/..." output:crd:artifacts:config=config/crd/bases

# Generate protobuf code
.PHONY: protos
protos: $(PROTOC_GEN_GO) $(PROTOC_GEN_GO_GRPC) $(PROTOC_GEN_GRPC_GATEWAY) $(PROTOC)
	PATH=$$PATH:$(TOOLS_BIN_DIR) $(PROTOC) --go_out=. --go_opt=paths=source_relative \
	          --go-grpc_out=. --go-grpc_opt=paths=source_relative \
			  --grpc-gateway_out=grpc_api_configuration=apis/protos/azure/compute/v1/vmsizerecommender_http.yaml,logtostderr=true:. --grpc-gateway_opt=paths=source_relative,generate_unbound_methods=true \
			  apis/protos/azure/compute/v1/vmsizerecommender.proto

# Generate code
generate: $(CONTROLLER_GEN) protos ## Generate deep copy methods
	$(CONTROLLER_GEN) \
		object:headerFile="hack/boilerplate.go.txt" paths="./..."

## --------------------------------------
## Build
## --------------------------------------

.PHONY: build
build: generate fmt vet ## Build agent binaries
	go build -o bin/hubagent cmd/hubagent/main.go
	go build -o bin/memberagent cmd/memberagent/main.go
	go build -o bin/crdinstaller cmd/crdinstaller/main.go

.PHONY: run-hubagent
run-hubagent: manifests generate fmt vet ## Run hub-agent from your host
	go run ./cmd/hubagent/main.go

.PHONY: run-memberagent
run-memberagent: manifests generate fmt vet ## Run member-agent from your host
	go run ./cmd/memberagent/main.go

.PHONY: run-crdinstaller
run-crdinstaller: manifests generate fmt vet ## Run CRD installer from your host.
	go run ./cmd/crdinstaller/main.go --mode=$(MODE)

## --------------------------------------
## Images
## --------------------------------------

OUTPUT_TYPE ?= type=registry
BUILDX_BUILDER_NAME ?= img-builder
QEMU_VERSION ?= 7.2.0-1
BUILDKIT_VERSION ?= v0.18.1

.PHONY: push
push: ## Build and push all Docker images
	$(MAKE) OUTPUT_TYPE="type=registry" docker-build-hub-agent docker-build-member-agent docker-build-refresh-token docker-build-crd-installer

# By default, docker buildx create will pull image moby/buildkit:buildx-stable-1 and hit the too many requests error
#
# Note (chenyu1): the step below sets up emulation for building/running non-native binaries on the host. The original
# setup assumes that the Makefile is always run on an x86_64 platform, and adds support for non-x86_64 hosts. Here
# we keep the original setup if the build target is x86_64 platforms (default) for compatibility reasons, but will switch to
# a more general setup for non-x86_64 hosts.
#
# On some systems the emulation setup might not work at all (e.g., macOS on Apple Silicon -> Rosetta 2 will be used
# by Docker Desktop as the default emulation option for AMD64 on ARM64 container compatibility).
.PHONY: docker-buildx-builder
# Note (chenyu1): the step below sets up emulation for building/running non-native binaries on the host. The original
# setup assumes that the Makefile is always run on an x86_64 platform, and adds support for non-x86_64 hosts. Here
# we keep the original setup if the build target is x86_64 platforms (default) for compatibility reasons, but will switch to
# a more general setup for non-x86_64 hosts.
#
# On some systems the emulation setup might not work at all (e.g., macOS on Apple Silicon -> Rosetta 2 will be used 
# by Docker Desktop as the default emulation option for AMD64 on ARM64 container compatibility).
docker-buildx-builder:
	@if ! docker buildx ls | grep $(BUILDX_BUILDER_NAME); then \
		if [ "$(TARGET_ARCH)" = "amd64" ] ; then \
			echo "The target is an x86_64 platform; setting up emulation for other known architectures"; \
			docker run --rm --privileged mcr.microsoft.com/mirror/docker/multiarch/qemu-user-static:$(QEMU_VERSION) --reset -p yes; \
		else \
			echo "Setting up emulation for known architectures"; \
			docker run --rm --privileged tonistiigi/binfmt --install all; \
		fi ;\
		docker buildx create --driver-opt image=mcr.microsoft.com/oss/v2/moby/buildkit:$(BUILDKIT_VERSION) --name $(BUILDX_BUILDER_NAME) --use; \
		docker buildx inspect $(BUILDX_BUILDER_NAME) --bootstrap; \
	fi

.PHONY: docker-build-hub-agent
docker-build-hub-agent: docker-buildx-builder ## Build hub-agent image
	docker buildx build \
		--file docker/$(HUB_AGENT_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform=$(TARGET_OS)/$(TARGET_ARCH) \
		--pull \
		--tag $(REGISTRY)/$(HUB_AGENT_IMAGE_NAME):$(HUB_AGENT_IMAGE_VERSION) \
		--progress=$(BUILDKIT_PROGRESS_TYPE) \
		--build-arg GOARCH=$(TARGET_ARCH) \
		--build-arg GOOS=$(TARGET_OS) .

.PHONY: docker-build-member-agent
docker-build-member-agent: docker-buildx-builder ## Build member-agent image
	docker buildx build \
		--file docker/$(MEMBER_AGENT_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform=$(TARGET_OS)/$(TARGET_ARCH) \
		--pull \
		--tag $(REGISTRY)/$(MEMBER_AGENT_IMAGE_NAME):$(MEMBER_AGENT_IMAGE_VERSION) \
		--progress=$(BUILDKIT_PROGRESS_TYPE) \
		--build-arg GOARCH=$(TARGET_ARCH) \
		--build-arg GOOS=$(TARGET_OS) .

.PHONY: docker-build-refresh-token
docker-build-refresh-token: docker-buildx-builder ## Build refresh-token image
	docker buildx build \
		--file docker/$(REFRESH_TOKEN_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform=$(TARGET_OS)/$(TARGET_ARCH) \
		--pull \
		--tag $(REGISTRY)/$(REFRESH_TOKEN_IMAGE_NAME):$(REFRESH_TOKEN_IMAGE_VERSION) \
		--progress=$(BUILDKIT_PROGRESS_TYPE) \
		--build-arg GOARCH=$(TARGET_ARCH) \
		--build-arg GOOS=${TARGET_OS} .

.PHONY: docker-build-crd-installer
docker-build-crd-installer: docker-buildx-builder
	docker buildx build \
		--file docker/crd-installer.Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform=$(TARGET_OS)/$(TARGET_ARCH) \
		--pull \
		--tag $(REGISTRY)/$(CRD_INSTALLER_IMAGE_NAME):$(CRD_INSTALLER_IMAGE_VERSION) \
		--progress=$(BUILDKIT_PROGRESS_TYPE) \
		--build-arg GOARCH=$(TARGET_ARCH) \
		--build-arg GOOS=${TARGET_OS} .

# Fleet Agents and Networking Agents are packaged and pushed to MCR for Arc Extension.
.PHONY: helm-package-arc-member-cluster-agents
helm-package-arc-member-cluster-agents:
	envsubst < charts/member-agent-arc/values.yaml > charts/member-agent-arc/values.yaml.tmp && \
	mv charts/member-agent-arc/values.yaml.tmp charts/member-agent-arc/values.yaml && \
	helm package charts/member-agent-arc/ --version $(ARC_MEMBER_AGENT_HELMCHART_VERSION)

	helm push $(ARC_MEMBER_AGENT_HELMCHART_NAME)-$(ARC_MEMBER_AGENT_HELMCHART_VERSION).tgz oci://$(REGISTRY)

## -----------------------------------
## Cleanup
## -----------------------------------

.PHONY: clean-bin
clean-bin: ## Remove all generated binaries
	rm -rf $(TOOLS_BIN_DIR)
	rm -rf ./bin

.PHONY: clean-e2e-tests
clean-e2e-tests: ## Clean up E2E test clusters
	cd ./test/e2e && chmod +x ./stop.sh && ./stop.sh $(MEMBER_CLUSTER_COUNT)
