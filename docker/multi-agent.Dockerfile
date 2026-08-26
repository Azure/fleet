# Build all Fleet required binaries.
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.26.5 AS builder

ARG GOOS=linux
ARG GOARCH=amd64

# Build the Fleet hub/member agent and refresh token binaries.
WORKDIR /core-workspace

# Copy the go.mod and go.sum files.
COPY fleet/go.mod go.mod
COPY fleet/go.sum go.sum
# Cache dependencies before building and copying the source code to avoid re-downloading upon retries.
# It also ensures that source code changes do not invalidate the downloaded Go dependencies layer.
RUN go mod download

# Copy the Go source code.
COPY fleet/cmd/ cmd/
COPY fleet/apis/ apis/
COPY fleet/pkg/ pkg/

# Build the hub agent.
RUN echo "Building the hub agent binary for GOOS=$GOOS GOARCH=$GOARCH"
RUN CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH GOEXPERIMENT=systemcrypto GO111MODULE=on go build -o hubagent cmd/hubagent/main.go

# Build the member agent.
RUN echo "Building the member agent binary for GOOS=$GOOS GOARCH=$GOARCH"
RUN CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH GOEXPERIMENT=systemcrypto GO111MODULE=on go build -o memberagent cmd/memberagent/main.go

# Build the refresh token binary.
RUN echo "Building the refresh token binary for GOOS=$GOOS GOARCH=$GOARCH"
RUN CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH GOEXPERIMENT=systemcrypto GO111MODULE=on go build -o refreshtoken cmd/authtoken/main.go

# Build the Fleet networking agent binaries.
WORKDIR /networking-workspace

# Copy the go.mod and go.sum files.
COPY fleet-networking/go.mod go.mod
COPY fleet-networking/go.sum go.sum
# Cache dependencies before building and copying the source code to avoid re-downloading upon retries.
# It also ensures that source code changes do not invalidate the downloaded Go dependencies layer.
RUN go mod download

# Copy the Go source code.
COPY fleet-networking/cmd/ cmd/
COPY fleet-networking/pkg/ pkg/
COPY fleet-networking/api/ api/

# Build the hub networking agent.
RUN echo "Building the hub networking agent binary for GOOS=$GOOS GOARCH=$GOARCH"
RUN CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH GO111MODULE=on go build -o hub-net-controller-manager cmd/hub-net-controller-manager/main.go

# Build the member networking agent.
RUN echo "Building the member networking agent binary for GOOS=$GOOS GOARCH=$GOARCH"
RUN CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH GO111MODULE=on go build -o member-net-controller-manager cmd/member-net-controller-manager/main.go

# Build the MCS networking agent.
RUN echo "Building the MCS networking agent binary for GOOS=$GOOS GOARCH=$GOARCH"
RUN CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH GO111MODULE=on go build -o mcs-controller-manager cmd/mcs-controller-manager/main.go

# Use Azure Linux distroless base image to package the binaries.
# For more details, refer to https://mcr.microsoft.com/en-us/artifact/mar/azurelinux/distroless/base/about.
FROM mcr.microsoft.com/azurelinux/distroless/base:3.0
WORKDIR /
COPY --from=builder /core-workspace/hubagent .
COPY --from=builder /core-workspace/memberagent .
COPY --from=builder /core-workspace/refreshtoken .
COPY --from=builder /networking-workspace/hub-net-controller-manager .
COPY --from=builder /networking-workspace/member-net-controller-manager .
COPY --from=builder /networking-workspace/mcs-controller-manager .

USER 65532:65532

# No default entrypoint is set for the unified image.
ENTRYPOINT []