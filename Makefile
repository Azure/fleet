REGISTRY ?= ghcr.io
KIND_IMAGE ?= kindest/node:v1.31.0
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

KUBECONFIG ?= $(HOME)/.kube/config
HUB_SERVER_URL ?= https://172.19.0.2:6443

HUB_KIND_CLUSTER_NAME = hub-testing
MEMBER_KIND_CLUSTER_NAME = member-testing
MEMBER_CLUSTER_COUNT ?= 3

# Directories
ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(abspath $(TOOLS_DIR)/bin)
CLUSTER_CONFIG := $(abspath test/e2e/v1alpha1/kind-config.yaml)

# Binaries
# Note: Need to use abspath so we can invoke these from subdirectories

CONTROLLER_GEN_VER := v0.16.0
CONTROLLER_GEN_BIN := controller-gen
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/$(CONTROLLER_GEN_BIN)-$(CONTROLLER_GEN_VER))

STATICCHECK_VER := 2025.1
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

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


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

staticcheck: $(STATICCHECK)
	$(STATICCHECK) ./...

.PHONY: fmt
fmt: $(GOIMPORTS) ## Run go fmt against code.
	go fmt ./...
	$(GOIMPORTS) -local go.goms.io/fleet -w $$(go list -f {{.Dir}} ./...)

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

## --------------------------------------
## Kind
## --------------------------------------

# Note that these targets are only used for E2E tests of the v1alpha1 API.

create-hub-kind-cluster:
	kind create cluster --name $(HUB_KIND_CLUSTER_NAME) --image=$(KIND_IMAGE) --config=$(CLUSTER_CONFIG) --kubeconfig=$(KUBECONFIG)

create-member-kind-cluster:
	kind create cluster --name $(MEMBER_KIND_CLUSTER_NAME) --image=$(KIND_IMAGE) --config=$(CLUSTER_CONFIG) --kubeconfig=$(KUBECONFIG)

load-hub-docker-image:
	kind load docker-image --name $(HUB_KIND_CLUSTER_NAME) $(REGISTRY)/$(HUB_AGENT_IMAGE_NAME):$(HUB_AGENT_IMAGE_VERSION)

load-member-docker-image:
	kind load docker-image --name $(MEMBER_KIND_CLUSTER_NAME) $(REGISTRY)/$(REFRESH_TOKEN_IMAGE_NAME):$(REFRESH_TOKEN_IMAGE_VERSION)
	kind load docker-image --name $(MEMBER_KIND_CLUSTER_NAME) $(REGISTRY)/$(MEMBER_AGENT_IMAGE_NAME):$(MEMBER_AGENT_IMAGE_VERSION)

## --------------------------------------
## test
## --------------------------------------

.PHONY: test
test: manifests generate fmt vet local-unit-test integration-test ## Run tests.

##
## workaround to bypass the pkg/controllers/workv1alpha1 tests failure
##
.PHONY: local-unit-test
local-unit-test: $(ENVTEST) ## Run tests.
	export CGO_ENABLED=1 && \
	export KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" && \
	go test ./pkg/controllers/workv1alpha1 -race -coverprofile=ut-coverage.xml -covermode=atomic -v && \
	go test `go list ./pkg/... ./cmd/... | grep -v pkg/controllers/workv1alpha1` -race -coverpkg=./...  -coverprofile=ut-coverage.xml -covermode=atomic -v

.PHONY: integration-test
integration-test: $(ENVTEST) ## Run tests.
	export CGO_ENABLED=1 && \
	export KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" && \
	ginkgo -v -p --race --cover --coverpkg=./pkg/scheduler/... ./test/scheduler && \
	ginkgo -v -p --race --cover --coverpkg=./... ./test/apis/... ./test/crdinstaller && \
	go test ./test/integration/... -coverpkg=./...  -race -coverprofile=it-coverage.xml -v

## local tests & e2e tests

install-hub-agent-helm:
	kind export kubeconfig --name $(HUB_KIND_CLUSTER_NAME)
	helm install hub-agent ./charts/hub-agent/ \
	--set image.pullPolicy=Never \
	--set image.repository=$(REGISTRY)/$(HUB_AGENT_IMAGE_NAME) \
	--set image.tag=$(HUB_AGENT_IMAGE_VERSION) \
	--set logVerbosity=5 \
	--set namespace=fleet-system \
	--set enableWebhook=true \
	--set webhookServiceName=fleetwebhook \
	--set webhookClientConnectionType=service \
	--set enableV1Alpha1APIs=true \
	--set enableV1Beta1APIs=false \
	--set enableClusterInventoryAPI=true \
	--set logFileMaxSize=1000000

.PHONY: e2e-v1alpha1-hub-kubeconfig-secret
e2e-v1alpha1-hub-kubeconfig-secret:
	kind export kubeconfig --name $(HUB_KIND_CLUSTER_NAME)
	kubectl apply -f test/e2e/v1alpha1/hub-agent-sa-secret.yaml
	TOKEN=$$(kubectl get secret hub-kubeconfig-secret -n fleet-system -o jsonpath='{.data.token}' | base64 -d) ;\
	kind export kubeconfig --name $(MEMBER_KIND_CLUSTER_NAME) ;\
	kubectl delete secret hub-kubeconfig-secret --ignore-not-found ;\
	kubectl create secret generic hub-kubeconfig-secret --from-literal=token=$$TOKEN

install-member-agent-helm: install-hub-agent-helm e2e-v1alpha1-hub-kubeconfig-secret
	kind export kubeconfig --name $(HUB_KIND_CLUSTER_NAME)
	## Get kind cluster IP that docker uses internally so we can talk to the other cluster. the port is the default one.
	HUB_SERVER_URL="https://$$(docker inspect $(HUB_KIND_CLUSTER_NAME)-control-plane --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'):6443" ;\
	kind export kubeconfig --name $(MEMBER_KIND_CLUSTER_NAME) ;\
	helm install member-agent ./charts/member-agent/ \
	--set config.hubURL=$$HUB_SERVER_URL \
	--set image.repository=$(REGISTRY)/$(MEMBER_AGENT_IMAGE_NAME) \
	--set image.tag=$(MEMBER_AGENT_IMAGE_VERSION) \
	--set refreshtoken.repository=$(REGISTRY)/$(REFRESH_TOKEN_IMAGE_NAME) \
	--set refreshtoken.tag=$(REFRESH_TOKEN_IMAGE_VERSION) \
	--set image.pullPolicy=Never \
	--set refreshtoken.pullPolicy=Never \
	--set config.memberClusterName="kind-$(MEMBER_KIND_CLUSTER_NAME)" \
	--set logVerbosity=5 \
	--set namespace=fleet-system
	# to make sure member-agent reads the token file.
	kubectl delete pod --all -n fleet-system

build-e2e-v1alpha1:
	go test -c ./test/e2e/v1alpha1

run-e2e-v1alpha1: build-e2e-v1alpha1
	KUBECONFIG=$(KUBECONFIG) HUB_SERVER_URL="https://$$(docker inspect $(HUB_KIND_CLUSTER_NAME)-control-plane --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'):6443" ./v1alpha1.test -test.v -ginkgo.v

.PHONY: create-kind-cluster
create-kind-cluster: create-hub-kind-cluster create-member-kind-cluster install-helm

.PHONY: install-helm
install-helm: load-hub-docker-image load-member-docker-image install-member-agent-helm

.PHONY: e2e-tests-v1alpha1
e2e-tests-v1alpha1: create-kind-cluster run-e2e-v1alpha1

.PHONY: e2e-tests
e2e-tests: setup-clusters
	cd ./test/e2e && ginkgo -v -p .

.PHONY: setup-clusters
setup-clusters:
	cd ./test/e2e && chmod +x ./setup.sh && ./setup.sh $(MEMBER_CLUSTER_COUNT)

## reviewable
.PHONY: reviewable
reviewable: fmt vet lint staticcheck
	go mod tidy

## --------------------------------------
## Code Generation
## --------------------------------------

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd"

# Generate manifests e.g. CRD, RBAC etc.
.PHONY: manifests
manifests: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		$(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./apis/..." output:crd:artifacts:config=config/crd/bases

# Generate code
generate: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		object:headerFile="hack/boilerplate.go.txt" paths="./..."

## --------------------------------------
## Build
## --------------------------------------

.PHONY: build
build: generate fmt vet ## Build agent binaries.
	go build -o bin/hubagent cmd/hubagent/main.go
	go build -o bin/memberagent cmd/memberagent/main.go
	go build -o bin/crdinstaller cmd/crdinstaller/main.go

.PHONY: run-hubagent
run-hubagent: manifests generate fmt vet ## Run a controllers from your host.
	go run ./cmd/hubagent/main.go

.PHONY: run-memberagent
run-memberagent: manifests generate fmt vet ## Run a controllers from your host.
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
push:
	$(MAKE) OUTPUT_TYPE="type=registry" docker-build-hub-agent docker-build-member-agent docker-build-refresh-token docker-build-crd-installer

# By default, docker buildx create will pull image moby/buildkit:buildx-stable-1 and hit the too many requests error
.PHONY: docker-buildx-builder
docker-buildx-builder:
	@if ! docker buildx ls | grep $(BUILDX_BUILDER_NAME); then \
		docker run --rm --privileged mcr.microsoft.com/mirror/docker/multiarch/qemu-user-static:$(QEMU_VERSION) --reset -p yes; \
		docker buildx create --driver-opt image=mcr.microsoft.com/oss/v2/moby/buildkit:$(BUILDKIT_VERSION) --name $(BUILDX_BUILDER_NAME) --use; \
		docker buildx inspect $(BUILDX_BUILDER_NAME) --bootstrap; \
	fi

.PHONY: docker-build-hub-agent
docker-build-hub-agent: docker-buildx-builder
	docker buildx build \
		--file docker/$(HUB_AGENT_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/amd64" \
		--pull \
		--tag $(REGISTRY)/$(HUB_AGENT_IMAGE_NAME):$(HUB_AGENT_IMAGE_VERSION) .

.PHONY: docker-build-member-agent
docker-build-member-agent: docker-buildx-builder
	docker buildx build \
		--file docker/$(MEMBER_AGENT_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/amd64" \
		--pull \
		--tag $(REGISTRY)/$(MEMBER_AGENT_IMAGE_NAME):$(MEMBER_AGENT_IMAGE_VERSION) .

.PHONY: docker-build-refresh-token
docker-build-refresh-token: docker-buildx-builder
	docker buildx build \
		--file docker/$(REFRESH_TOKEN_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/amd64" \
		--pull \
		--tag $(REGISTRY)/$(REFRESH_TOKEN_IMAGE_NAME):$(REFRESH_TOKEN_IMAGE_VERSION) .

.PHONY: docker-build-crd-installer
docker-build-crd-installer: docker-buildx-builder
	docker buildx build \
		--file docker/crd-installer.Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/amd64" \
		--pull \
		--tag $(REGISTRY)/$(CRD_INSTALLER_IMAGE_NAME):$(CRD_INSTALLER_IMAGE_VERSION) .

## -----------------------------------
## Cleanup
## -----------------------------------

.PHONY: clean-bin
clean-bin: ## Remove all generated binaries
	rm -rf $(TOOLS_BIN_DIR)
	rm -rf ./bin

# Note that these targets are only used for E2E tests of the v1alpha1 API.

.PHONY: uninstall-helm
uninstall-helm: clean-testing-resources
	kind export kubeconfig --name $(HUB_KIND_CLUSTER_NAME)
	helm uninstall hub-agent

	kind export kubeconfig --name $(MEMBER_KIND_CLUSTER_NAME)
	helm uninstall member-agent

.PHONY: clean-testing-resources
clean-testing-resources:
	kind export kubeconfig --name $(HUB_KIND_CLUSTER_NAME)
	kubectl delete ns fleet-member-kind-member-testing --ignore-not-found
	kubectl delete memberclusters.fleet.azure.com kind-$(MEMBER_KIND_CLUSTER_NAME) --ignore-not-found

	kind export kubeconfig --name $(MEMBER_KIND_CLUSTER_NAME)
	kubectl delete ns fleet-member-kind-member-testing --ignore-not-found

.PHONY: clean-e2e-tests-v1alpha1
clean-e2e-tests-v1alpha1:
	kind delete cluster --name $(HUB_KIND_CLUSTER_NAME)
	kind delete cluster --name $(MEMBER_KIND_CLUSTER_NAME)

.PHONY: clean-e2e-tests
clean-e2e-tests:
	cd ./test/e2e && chmod +x ./stop.sh && ./stop.sh $(MEMBER_CLUSTER_COUNT)
