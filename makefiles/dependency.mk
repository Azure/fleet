# Directories
ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR := $(abspath $(ROOT_DIR)/bin)
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(abspath $(TOOLS_DIR)/bin)

# Binaries
GOLANGCI_LINT_VER := v1.41.1
GOLANGCI_LINT_BIN := golangci-lint
GOLANGCI_LINT := $(TOOLS_BIN_DIR)/$(GOLANGCI_LINT_BIN)-$(GOLANGCI_LINT_VER)

# Scripts
GO_INSTALL := ./hack/go-install.sh

## --------------------------------------
## Style checks
## --------------------------------------

.PHONY: staticchecktool
staticchecktool:
ifeq (, $(shell which staticcheck))
	@{ \
	set -e ;\
	echo 'installing honnef.co/go/tools/cmd/staticcheck ' ;\
	GO111MODULE=auto go get honnef.co/go/tools/cmd/staticcheck@2022.1 ;\
	}
STATICCHECK=$(GOBIN)/staticcheck
else
STATICCHECK=$(shell which staticcheck)
endif


## --------------------------------------
## Tooling Binaries
## --------------------------------------

$(GOLANGCI_LINT):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) github.com/golangci/golangci-lint/cmd/golangci-lint $(GOLANGCI_LINT_BIN) $(GOLANGCI_LINT_VER)

## --------------------------------------
## GOIMPORTS
## --------------------------------------

.PHONY: goimports
goimports:
ifeq (, $(shell which goimports))
	@{ \
	set -e ;\
	go install golang.org/x/tools/cmd/goimports@latest ;\
	}
GOIMPORTS=$(GOBIN)/goimports
else
GOIMPORTS=$(shell which goimports)
endif

## --------------------------------------
## ENVTEST
## --------------------------------------

.PHONY: envtest
envtest:
ifeq (, $(shell which setup-envtest))
	@{ \
	set -e ;\
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest ;\
	}
ENVTEST=$(GOBIN)/setup-envtest
else
ENVTEST=$(shell which setup-envtest)
endif
