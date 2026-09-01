# syntax=docker/dockerfile:1
# Build the hubagent binary
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.26.6-1 AS builder

# TARGETOS and TARGETARCH are populated automatically by BuildKit for each
# platform being built, so a single multi-platform `docker buildx build`
# produces a correctly built binary per architecture.
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/hubagent/ cmd/hubagent/
COPY apis/ apis/
COPY pkg/ pkg/

# Build. CGO + systemcrypto compiles against the target architecture's OpenSSL,
# which is why the builder runs per-target under emulation (see the Makefile's
# docker-buildx-builder / setup-qemu targets) rather than cross-compiling.
RUN echo "Building hubagent with GOOS=${TARGETOS} GOARCH=${TARGETARCH}" && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOEXPERIMENT=systemcrypto go build -o hubagent ./cmd/hubagent/

# Use distroless as minimal base image to package the hubagent binary.
# The pinned digest must reference a multi-arch image index so BuildKit can
# resolve the matching base layer for each target architecture.
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/base:nonroot@sha256:2d7d29b504e7166f6d0c7655a18ebf5def5b37b029f8c4f8667e434ba774844f
WORKDIR /
COPY --link --from=builder /workspace/hubagent .
USER 65532:65532

ENTRYPOINT ["/hubagent"]
