# Build the refreshtoken binary
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.24.6 AS builder

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

ARG TARGETARCH

# Build
RUN echo "Building images with GOOS=${GOOS} GOARCH=${GOARCH}"
RUN CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH GOEXPERIMENT=systemcrypto GO111MODULE=on go build -o refreshtoken main.go

# Use distroless as minimal base image to package the refreshtoken binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/base:nonroot
WORKDIR /
COPY --from=builder /workspace/refreshtoken .
USER 65532:65532

ENTRYPOINT ["/refreshtoken"]
