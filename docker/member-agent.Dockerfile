# Build the memberagent binary
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.25.8 AS builder

ARG GOOS=linux
ARG GOARCH=amd64

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/memberagent cmd/memberagent/
COPY apis/ apis/
COPY pkg/ pkg/

# Build
RUN echo "Building images with GOOS=$GOOS GOARCH=$GOARCH"
RUN CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH GOEXPERIMENT=systemcrypto GO111MODULE=on go build -o memberagent cmd/memberagent/main.go

# Use distroless as minimal base image to package the memberagent binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/base:nonroot@sha256:a696c7c8545ba9b2b2807ee60b8538d049622f0addd85aee8cec3ec1910de1f9
WORKDIR /
COPY --from=builder /workspace/memberagent .
USER 65532:65532

ENTRYPOINT ["/memberagent"]
