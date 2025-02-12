# Build the memberagent binary
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.22.12 AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/memberagent/main.go main.go
COPY apis/ apis/
COPY pkg/ pkg/

ARG TARGETARCH

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GO111MODULE=on go build -o memberagent main.go

# Use distroless as minimal base image to package the memberagent binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/memberagent .
USER 65532:65532

ENTRYPOINT ["/memberagent"]
