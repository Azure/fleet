REGISTRY ?= ghcr.io
KIND_IMAGE ?= kindest/node:v1.33.4
ifndef TAG
	TAG ?= $(shell git rev-parse --short=7 HEAD)
endif
HUB_AGENT_IMAGE_VERSION ?= $(TAG)
MEMBER_AGENT_IMAGE_VERSION ?= $(TAG)
REFRESH_TOKEN_IMAGE_VERSION ?= $(TAG)
CRD_INSTALLER_IMAGE_VERSION ?= $(TAG)

# Optional additional tag applied to every image within the same `docker buildx
# build` invocation. A stable release sets this to the short, v-less version
# (e.g. "0.4.0") so both "v0.4.0" and "0.4.0" are published from one build,
# which replaces a separate `docker buildx imagetools create` retag step.
IMAGE_EXTRA_TAG ?=

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
JOIN_MEMBERS ?= false

# Directories
ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(abspath $(TOOLS_DIR)/bin)

# Binaries
# Note: Need to use abspath so we can invoke these from subdirectories

CONTROLLER_GEN_VER := v0.20.0
CONTROLLER_GEN_BIN := controller-gen
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/$(CONTROLLER_GEN_BIN)-$(CONTROLLER_GEN_VER))

STATICCHECK_VER := v0.6.1
STATICCHECK_BIN := staticcheck
STATICCHECK := $(abspath $(TOOLS_BIN_DIR)/$(STATICCHECK_BIN)-$(STATICCHECK_VER))

GOIMPORTS_VER := v0.42.0
GOIMPORTS_BIN := goimports
GOIMPORTS := $(abspath $(TOOLS_BIN_DIR)/$(GOIMPORTS_BIN)-$(GOIMPORTS_VER))

GOLANGCI_LINT_VER := v2.13.2
GOLANGCI_LINT_BIN := golangci-lint
GOLANGCI_LINT := $(abspath $(TOOLS_BIN_DIR)/$(GOLANGCI_LINT_BIN)-$(GOLANGCI_LINT_VER))

# ENVTEST_K8S_VERSION refers to the version of k8s binary assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.33.0
# ENVTEST_VER is the version of the ENVTEST binary
ENVTEST_VER = release-0.22
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
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) github.com/golangci/golangci-lint/v2/cmd/golangci-lint $(GOLANGCI_LINT_BIN) $(GOLANGCI_LINT_VER)

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
	$(GOLANGCI_LINT) run -v --fast-only

.PHONY: lint-full
lint-full: $(GOLANGCI_LINT) ## Run slower linters to detect possible issues
	$(GOLANGCI_LINT) run -v

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
# Note: this recipe runs both unit tests and integration tests under the pkg/ directory.
.PHONY: local-unit-test
local-unit-test: $(ENVTEST) ## Run unit tests
	export CGO_ENABLED=1 && \
	export KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" && \
	go test `go list ./pkg/... ./cmd/...` -race -coverpkg=./...  -coverprofile=ut-coverage.xml -covermode=atomic -v -timeout=30m

# Note: this recipe runs the integration tests under the /test/scheduler and /test/apis/ directories with the Ginkgo CLI.
.PHONY: integration-test
integration-test: $(ENVTEST) ## Run integration tests
	export CGO_ENABLED=1 && \
	export KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" && \
	ginkgo -v -p --race --cover --coverpkg=./pkg/scheduler/... -coverprofile=scheduler-it.out ./test/scheduler && \
	ginkgo -v -p --race --cover --coverpkg=./apis/ -coverprofile=api-validation-it.out ./test/apis/...

.PHONY: kubebuilder-assets-path
kubebuilder-assets-path: $(ENVTEST) ## Get the path to kubebuilder assets
	@export CGO_ENABLED=1 && \
	export KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" && \
	echo $$KUBEBUILDER_ASSETS

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
ifeq ($(JOIN_MEMBERS),true)
	$(MAKE) join-members
else
	@echo ""
	@echo "Clusters are ready but member clusters have not been joined to the hub."
	@echo "To join them, run: make join-members"
	@echo "Or re-run with: JOIN_MEMBERS=true make setup-clusters"
endif

.PHONY: join-members
join-members: ## Join member clusters to the hub cluster (run after setup-clusters)
	cd ./test/e2e && chmod +x ./join.sh && ./join.sh $(MEMBER_CLUSTER_COUNT)

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
	go build -o bin/kubectl-fleet ./tools/fleet/

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
BUILDKIT_VERSION ?= v0.18.1

# QEMU binfmt registration images (see setup-qemu). Both run --privileged, so
# they come from the Microsoft-vetted MCR mirrors (no direct third-party pulls)
# and are pinned by digest as well as tag: a floating reference on a privileged
# release-path container is a supply-chain risk. The binfmt mirror digest is
# byte-identical to the upstream docker.io/tonistiigi/binfmt tag it mirrors.
#
# qemu-user-static has no manifest list - neither upstream nor on the mirror. It
# is a single linux/amd64 schema-2 manifest, which is all setup-qemu needs: the
# QEMU_IMAGE branch runs only when TARGET_ARCH is amd64. Obtain its digest with a
# request that names a schema-2 or OCI media type (substitute QEMU_VERSION for the
# tag):
#
#   curl -sI -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
#     https://mcr.microsoft.com/v2/mirror/docker/multiarch/qemu-user-static/manifests/7.2.0-1 \
#     | grep -i docker-content-digest
#
# `docker manifest inspect -v` and `docker buildx imagetools inspect` also report
# the correct digest. What does NOT is a bare `curl -sI` sending no Accept header,
# or one naming only the manifest-list type: MCR then answers with a deprecated
# schema-1 `v1+prettyjws` view synthesised on demand from the schema-2 manifest.
# The digest it advertises is stable, but it is never stored as a manifest
# revision, so fetching by it returns "manifest unknown" and every pull of a pin
# taken that way fails - most likely how the previous pin here was produced.
#
# Both mirrors carry exactly one tag today, so bumping either version below needs
# MCR to onboard the new tag first.
QEMU_VERSION ?= 7.2.0-1
QEMU_IMAGE ?= mcr.microsoft.com/mirror/docker/multiarch/qemu-user-static:$(QEMU_VERSION)@sha256:fe60359c92e86a43cc87b3d906006245f77bfc0565676b80004cc666e4feb9f0
BINFMT_VERSION ?= qemu-v9.2.2-52
BINFMT_IMAGE ?= mcr.microsoft.com/mirror/docker/tonistiigi/binfmt:$(BINFMT_VERSION)@sha256:1b804311fe87047a4c96d38b4b3ef6f62fca8cd125265917a9e3dc3c996c39e6

# Platforms to build container images for. Defaults to the host/target platform
# so local single-arch builds (e.g. loading into kind, which cannot load a
# multi-platform image) keep working unchanged; `push` overrides this to build a
# multi-arch manifest for the release.
PLATFORMS ?= $(TARGET_OS)/$(TARGET_ARCH)
RELEASE_PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: push
push: ## Build and push all Docker images as multi-arch manifests
	$(MAKE) OUTPUT_TYPE="type=registry" PLATFORMS="$(RELEASE_PLATFORMS)" docker-build-hub-agent docker-build-member-agent docker-build-refresh-token

.PHONY: helm-push
helm-push: ## Package and push Helm charts to OCI registry
	helm package charts/hub-agent --version $(CHART_VERSION) --app-version $(TAG) --destination .helm-packages
	helm package charts/member-agent --version $(CHART_VERSION) --app-version $(TAG) --destination .helm-packages
	helm push .helm-packages/hub-agent-$(CHART_VERSION).tgz oci://$(REGISTRY)
	helm push .helm-packages/member-agent-$(CHART_VERSION).tgz oci://$(REGISTRY)
	rm -rf .helm-packages


# Directory and artifact names for the standalone CRD release tarball.
CRD_PACKAGE_DIR ?= _crd-package
CRD_PACKAGE_NAME ?= kubefleet-crds-$(TAG)
# Use sha256sum when available (Linux/CI), fall back to shasum (macOS).
SHA256SUM ?= $(shell command -v sha256sum >/dev/null 2>&1 && echo "sha256sum" || echo "shasum -a 256")

.PHONY: crd-package
crd-package: ## Package the raw CRDs into a release tarball with a SHA-256 checksum
	rm -rf $(CRD_PACKAGE_DIR)
	mkdir -p $(CRD_PACKAGE_DIR)/crds/hub $(CRD_PACKAGE_DIR)/crds/member
	# Source from the charts' CRD sets (symlinks into config/crd/bases) so the
	# bundle mirrors exactly what each agent installs: hub-cluster CRDs under
	# crds/hub, member-cluster CRDs under crds/member. cp -L dereferences the
	# symlinks so the archive holds the raw CRD YAML, not dangling links.
	cp -L charts/hub-agent/templates/crds/*.yaml $(CRD_PACKAGE_DIR)/crds/hub/
	cp -L charts/member-agent/templates/crds/*.yaml $(CRD_PACKAGE_DIR)/crds/member/
	tar -czf $(CRD_PACKAGE_DIR)/$(CRD_PACKAGE_NAME).tgz -C $(CRD_PACKAGE_DIR) crds
	cd $(CRD_PACKAGE_DIR) && $(SHA256SUM) $(CRD_PACKAGE_NAME).tgz > $(CRD_PACKAGE_NAME).tgz.sha256
	@echo "Packaged CRDs into $(CRD_PACKAGE_DIR)/$(CRD_PACKAGE_NAME).tgz"

.PHONY: crd-verify
crd-verify: ## Verify the chart CRD directories cover every CRD in config/crd/bases; note (chenyu1): kubefleet.dev CRDs are ignored for now until the implementation is completed.
	@bases="$$(mktemp)"; charts="$$(mktemp)"; \
	ls config/crd/bases/ | grep -v '^placement\.kubefleet\.dev' | sort > "$$bases"; \
	{ ls charts/hub-agent/templates/crds/; ls charts/member-agent/templates/crds/; } | sort > "$$charts"; \
	missing="$$(comm -3 "$$bases" "$$charts")"; \
	rm -f "$$bases" "$$charts"; \
	if [ -n "$$missing" ]; then \
		echo "ERROR: chart CRD directories are out of sync with config/crd/bases."; \
		echo "Left column = only in config/crd/bases; right column = only in the charts:"; \
		echo "$$missing"; \
		echo "If you added a CRD, symlink it into charts/hub-agent/templates/crds/ or charts/member-agent/templates/crds/."; \
		exit 1; \
	fi; \
	echo "crd-verify: chart CRD directories cover all CRDs in config/crd/bases"

# Register QEMU binfmt handlers so the host can build/run non-native binaries.
# This must run for any build that targets a non-native platform - whether a
# multi-platform build or a single-platform cross build (e.g. PLATFORMS=linux/arm64
# on an amd64 host) - regardless of whether the buildx builder already exists (a
# pre-existing builder would otherwise skip emulation setup and the foreign-arch
# build would fail with "exec format error"). The registration is idempotent, so
# it is safe to re-run. It is skipped only when PLATFORMS is exactly the native
# platform, which never needs emulation.
#
# On some systems the emulation setup might not work at all (e.g., macOS on Apple
# Silicon -> Rosetta 2 will be used by Docker Desktop as the default emulation
# option for AMD64 on ARM64 container compatibility).
.PHONY: setup-qemu
setup-qemu: ## Register QEMU emulation for multi-architecture image builds
	$(info Auto-detected system architecture: $(TARGET_ARCH))
	@case "$(PLATFORMS)" in \
		"$(TARGET_OS)/$(TARGET_ARCH)") \
			echo "Native single-platform build ($(PLATFORMS)); skipping QEMU setup" ;; \
		*) \
			echo "Build targets non-native platforms ($(PLATFORMS)); registering QEMU emulation"; \
			if [ "$(TARGET_ARCH)" = "amd64" ] ; then \
				docker run --rm --privileged $(QEMU_IMAGE) --reset -p yes; \
			else \
				docker run --rm --privileged $(BINFMT_IMAGE) --install all; \
			fi ;; \
	esac

# By default, docker buildx create will pull image moby/buildkit:buildx-stable-1 and hit the too many requests error
# Always select and bootstrap the named builder, not only on creation: if the
# builder already exists but another builder is current, the subsequent
# `docker buildx build` would silently run on the wrong builder (potentially a
# docker-driver one with no multi-platform support).
.PHONY: docker-buildx-builder
docker-buildx-builder: setup-qemu
	@if ! docker buildx ls | grep -q "$(BUILDX_BUILDER_NAME)"; then \
		docker buildx create --driver-opt image=mcr.microsoft.com/oss/v2/moby/buildkit:$(BUILDKIT_VERSION) --name $(BUILDX_BUILDER_NAME); \
	fi
	docker buildx use $(BUILDX_BUILDER_NAME)
	docker buildx inspect $(BUILDX_BUILDER_NAME) --bootstrap

.PHONY: docker-build-hub-agent
docker-build-hub-agent: docker-buildx-builder ## Build hub-agent image
	docker buildx build \
		--file docker/$(HUB_AGENT_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform=$(PLATFORMS) \
		--pull \
		--tag $(REGISTRY)/$(HUB_AGENT_IMAGE_NAME):$(HUB_AGENT_IMAGE_VERSION) \
		$(if $(IMAGE_EXTRA_TAG),--tag $(REGISTRY)/$(HUB_AGENT_IMAGE_NAME):$(IMAGE_EXTRA_TAG)) \
		--progress=$(BUILDKIT_PROGRESS_TYPE) .

.PHONY: docker-build-member-agent
docker-build-member-agent: docker-buildx-builder ## Build member-agent image
	docker buildx build \
		--file docker/$(MEMBER_AGENT_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform=$(PLATFORMS) \
		--pull \
		--tag $(REGISTRY)/$(MEMBER_AGENT_IMAGE_NAME):$(MEMBER_AGENT_IMAGE_VERSION) \
		$(if $(IMAGE_EXTRA_TAG),--tag $(REGISTRY)/$(MEMBER_AGENT_IMAGE_NAME):$(IMAGE_EXTRA_TAG)) \
		--progress=$(BUILDKIT_PROGRESS_TYPE) .

.PHONY: docker-build-refresh-token
docker-build-refresh-token: docker-buildx-builder ## Build refresh-token image
	docker buildx build \
		--file docker/$(REFRESH_TOKEN_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform=$(PLATFORMS) \
		--pull \
		--tag $(REGISTRY)/$(REFRESH_TOKEN_IMAGE_NAME):$(REFRESH_TOKEN_IMAGE_VERSION) \
		$(if $(IMAGE_EXTRA_TAG),--tag $(REGISTRY)/$(REFRESH_TOKEN_IMAGE_NAME):$(IMAGE_EXTRA_TAG)) \
		--progress=$(BUILDKIT_PROGRESS_TYPE) .

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
