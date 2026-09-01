# syntax=docker/dockerfile:1
# Build the hubagent binary.
#
# The builder stage always runs on the build host's own platform
# (--platform=$BUILDPLATFORM) and cross-compiles for each target platform, so
# the Go toolchain, cgo and gcc never execute under QEMU. Running the builder
# per target under emulation crashed gcc's cc1 ("internal compiler error:
# Segmentation fault" / "cgo: gcc produced no output" in runtime/cgo and net)
# and, when it did not crash, took over an hour per image. Only the final
# distroless stage is per-target, and it runs no commands.
FROM --platform=$BUILDPLATFORM mcr.microsoft.com/oss/go/microsoft/golang:1.26.6-1 AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# The module download above is declared before the target-specific ARGs so
# that BuildKit shares its layer across every target platform of one build.
# TARGETOS and TARGETARCH are populated automatically by BuildKit for each
# platform being built, so a single multi-platform `docker buildx build`
# produces a correctly built binary per architecture.
ARG TARGETOS
ARG TARGETARCH
ARG BUILDARCH

# GOEXPERIMENT=systemcrypto requires CGO_ENABLED=1. Its OpenSSL backend
# dlopen()s libcrypto at run time, so no OpenSSL headers are needed to build -
# only a C compiler and libc headers for the target architecture. When the
# target differs from the architecture this stage runs on, install Debian's
# cross toolchain for it. Either way /usr/local/bin/target-gcc ends up pointing
# at the right compiler. BUILDARCH is this stage's own architecture because the
# stage is pinned to --platform=$BUILDPLATFORM above.
RUN set -eu; \
    if [ "${TARGETARCH}" = "${BUILDARCH}" ]; then \
        ln -s /usr/bin/gcc /usr/local/bin/target-gcc; \
    else \
        case "${TARGETARCH}" in \
            arm64) pkg=gcc-aarch64-linux-gnu; triple=aarch64-linux-gnu ;; \
            amd64) pkg=gcc-x86-64-linux-gnu; triple=x86_64-linux-gnu ;; \
            *) echo "unsupported TARGETARCH ${TARGETARCH}" >&2; exit 1 ;; \
        esac; \
        apt-get update; \
        apt-get install -y --no-install-recommends "${pkg}" "libc6-dev-${TARGETARCH}-cross"; \
        rm -rf /var/lib/apt/lists/*; \
        ln -s "/usr/bin/${triple}-gcc" /usr/local/bin/target-gcc; \
    fi

# Copy the go source
COPY cmd/hubagent/ cmd/hubagent/
COPY apis/ apis/
COPY pkg/ pkg/

# Build for the target platform with the compiler selected above.
RUN echo "Building hubagent with GOOS=${TARGETOS} GOARCH=${TARGETARCH} CC=$(readlink -f /usr/local/bin/target-gcc)" && \
    CGO_ENABLED=1 CC=target-gcc GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOEXPERIMENT=systemcrypto go build -o hubagent ./cmd/hubagent/

# Use distroless as minimal base image to package the hubagent binary.
# The pinned digest must reference a multi-arch image index so BuildKit can
# resolve the matching base layer for each target architecture.
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/base:nonroot@sha256:2d7d29b504e7166f6d0c7655a18ebf5def5b37b029f8c4f8667e434ba774844f
WORKDIR /
COPY --link --from=builder /workspace/hubagent .
USER 65532:65532

ENTRYPOINT ["/hubagent"]
