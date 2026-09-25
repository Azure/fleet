REGISTRY ?= ghcr.io
ifndef TAG
$(error TAG is required; set it explicitly)
endif
MULTI_AGENT_IMAGE_VERSION ?= $(TAG)

MULTI_AGENT_IMAGE_NAME ?= multi-agent

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

## --------------------------------------
## Images
## --------------------------------------

OUTPUT_TYPE ?= type=registry
BUILDX_BUILDER_NAME ?= img-builder
QEMU_VERSION ?= 7.2.0-1
BUILDKIT_VERSION ?= v0.18.1

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
docker-buildx-builder:
	$(info Auto-detected system architecture: $(TARGET_ARCH))
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

.PHONY: docker-build-multi-agent
docker-build-multi-agent: docker-buildx-builder ## Build multi-agent image
	docker buildx build \
		--file fleet/docker/multi-agent.Dockerfile \
		--tag $(REGISTRY)/$(MULTI_AGENT_IMAGE_NAME):$(MULTI_AGENT_IMAGE_VERSION) \
		--output=$(OUTPUT_TYPE) \
		--platform=$(TARGET_OS)/$(TARGET_ARCH) \
		--pull \
		--progress=$(BUILDKIT_PROGRESS_TYPE) \
		--build-arg GOARCH=$(TARGET_ARCH) \
		--build-arg GOOS=$(TARGET_OS) .
