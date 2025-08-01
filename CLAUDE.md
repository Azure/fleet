# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

KubeFleet is a CNCF sandbox project that provides multi-cluster application management for Kubernetes. It uses a hub-and-spoke architecture with two main components:

- **Hub Agent**: Runs on the central hub cluster, manages placement decisions, scheduling, and cluster inventory
- **Member Agent**: Runs on each member cluster, applies workloads and reports cluster status

## Common Development Commands

### Building and Testing
```bash
# Build all binaries
make build

# Run specific agent binaries directly
make run-hubagent
make run-memberagent

# Run all tests (unit + integration)
make test

# Run unit tests only
make local-unit-test

# Run integration tests only  
make integration-test

# Run E2E tests
make e2e-tests

# Run custom E2E tests with labels
make e2e-tests-custom
```

### Code Quality
```bash
# Run linting (required before commits)
make lint

# Run full linting (slower but more thorough)
make lint-full

# Run static analysis
make staticcheck

# Format code
make fmt

# Run go vet
make vet

# Run all quality checks
make reviewable
```

### Code Generation
```bash
# Generate CRDs and manifests
make manifests

# Generate deep copy methods
make generate
```

### E2E Testing
```bash
# Set up test clusters (creates 3 member clusters by default)
make setup-clusters

# Run E2E tests with custom cluster count
make setup-clusters MEMBER_CLUSTER_COUNT=5

# Run parallel E2E tests (default - excludes custom tests)
make e2e-tests

# Collect logs after E2E tests
make collect-e2e-logs

# Clean up test clusters
make clean-e2e-tests
```

### Docker and Images
```bash
# Build and push all images
make push

# Build individual images
make docker-build-hub-agent
make docker-build-member-agent
make docker-build-refresh-token
```

## Architecture Overview

### Core API Types
- **ClusterResourcePlacement (CRP)**: Main API for placing cluster scoped resources across clusters with scheduling policies. If one namespace is selected, we will place everything in that namespace across clusters.
- **ResourcePlacement (RP)**: Main API for placing namespaced resources across clusters with scheduling policies.
- **MemberCluster**: Represents a member cluster with identity and heartbeat settings
- **ClusterResourceBinding**: Represents scheduling decisions binding cluster scoped resources to clusters
- **ResourceBinding**: Represents scheduling decisions binding resources to clusters
- **ClusterResourceSnapshot**: Immutable snapshot of cluster resource state for rollback and history
- **ResourceSnapshot**: Immutable snapshot of namespaced resource state for rollback and history
- **Work**: Contains manifests to be applied on member clusters
- **ClusterResourceSnapshot**: Immutable snapshots of resources to be placed
- **ClusterSchedulingPolicySnapshot**: Immutable snapshots of scheduling policies

### Key Controllers
- **ClusterResourcePlacement Controller** (`pkg/controllers/clusterresourceplacement/`): Manages CRP lifecycle
- **Scheduler** (`pkg/scheduler/`): Makes placement decisions using pluggable framework
- **Rollout Controller** (`pkg/controllers/rollout/`): Manages rollout the changes to all the clusters that a placement decision has been made for
- **WorkGenerator** (`pkg/controllers/workgenerator/`): Generates Work objects from bindings
- **WorkApplier** (`pkg/controllers/workapplier/`): Applies Work manifests on member clusters
- **Resource Placement Watchers**: Monitor and react to changes in placement decisions
- **ClusterResourceBinding Watcher** (`pkg/controllers/clusterresourcebindingwatcher/`): Watches binding changes
- **ClusterResourcePlacement Watcher** (`pkg/controllers/clusterresourceplacementwatcher/`): Watches placement changes

### Scheduler Framework
The scheduler uses a pluggable architecture similar to Kubernetes scheduler:
- **Filter plugins**: `clusteraffinity`, `tainttoleration`, `clustereligibility`
- **Score plugins**: `clusteraffinity`, `sameplacementaffinity`
- **Property-based scheduling**: Uses cluster properties (CPU, memory, cost) for decisions

### Placement Strategies
- **PickAll**: Place on all matching clusters
- **PickN**: Place on N highest-scoring clusters  
- **PickFixed**: Place on specific named clusters

## Directory Structure

```
apis/                     # API definitions and CRDs
├── cluster/v1beta1/     # MemberCluster APIs
├── placement/v1beta1/   # Placement and work APIs
pkg/controllers/         # All controllers organized by resource type
pkg/scheduler/           # Scheduler framework and plugins  
pkg/propertyprovider/    # Cloud-specific property providers (Azure)
pkg/utils/              # Shared utilities and helpers
cmd/hubagent/           # Hub agent main and setup
cmd/memberagent/        # Member agent main and setup
```

## Development Notes

- Always run `make reviewable` before submitting PRs
- Follow [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md) when possible
- Favor standard library over third-party libraries
- Controllers should be thoroughly tested with integration tests
- New scheduler plugins should implement both Filter and Score interfaces
- Use existing patterns from similar controllers when adding new functionality
- Property providers should implement the `PropertyProvider` interface
- PR titles must use prefixes: `feat:`, `fix:`, `docs:`, `test:`, `chore:`, `ci:`, `perf:`, `refactor:`, `revert:`

## Testing Patterns

### Unit Tests
- Use `testify` for assertions
- Controllers use `envtest` for integration testing with real etcd
- Mock external dependencies with `gomock`
- Unit test files: `<go_file>_test.go` in same directory
- Table-driven test style preferred

### Integration Tests  
- Located in `test/integration/` and `test/scheduler/`
- Use Ginkgo/Gomega framework
- Tests run against real Kind clusters
- Files named: `<go_file>_integration_test.go`
- Separate test suites for different placement strategies

### E2E Tests
- Located in `test/e2e/`
- Use Ginkgo/Gomega framework  
- Test cross-controller interactions
- Use shared test manifests in `test/integration/manifests/`
- Run with `make e2e-tests` against 3 Kind clusters

## Key Patterns

### Controller Pattern
All controllers follow standard Kubernetes controller patterns:
- Reconcile functions with retry logic
- Status subresource updates
- Event recording for debugging

### Snapshot-based Versioning
- All policy changes create immutable snapshots
- Enables rollback and change tracking
- Snapshots stored as separate CRDs

### Property-based Scheduling
- Property providers collect cluster metrics
- Azure provider tracks VM costs and node properties
- Custom properties supported for scheduling decisions

### Multi-API Version Support
- v1alpha1 APIs maintained for backward compatibility  
- v1beta1 APIs are current stable version
- Feature flags control API version enablement

### Watcher Pattern
- Resource placement watchers monitor CRP and binding changes
- Event-driven architecture for responsive placement decisions
- Separate watchers for different resource types to enable focused reconciliation