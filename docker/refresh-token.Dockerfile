# Build the refreshtoken binary
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.24.12 AS builder

ARG GOOS="linux"
ARG GOARCH="amd64"

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/authtoken/main.go main.go
COPY pkg/authtoken pkg/authtoken

# Build with CGO enabled and GOEXPERIMENT=systemcrypto for internal usage
RUN echo "Building for GOOS=${GOOS} GOARCH=${GOARCH}"
RUN CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH GOEXPERIMENT=systemcrypto GO111MODULE=on go build -o refreshtoken main.go

# Use Azure Linux distroless base image to package the refreshtoken binary
# Refer to https://mcr.microsoft.com/en-us/artifact/mar/azurelinux/distroless/base/about for more details
FROM mcr.microsoft.com/azurelinux/distroless/base:3.0
WORKDIR /
COPY --from=builder /workspace/refreshtoken .
USER 65532:65532

ENTRYPOINT ["/refreshtoken"]
