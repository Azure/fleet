# KubeFleet Copilot Instructions

## Build, Test, and Lint Commands

```bash
make build                # Build all binaries
make reviewable           # Run all quality checks (fmt, vet, lint, staticcheck, tidy) — required before PRs
make lint                 # Fast linting
make lint-full            # Thorough linting (--fast=false)
make test                 # Unit + integration tests
make local-unit-test      # Unit tests only
make integration-test     # Integration tests only (Ginkgo, uses envtest)
make manifests            # Regenerate CRDs from API types
make generate             # Regenerate deep copy methods
```

### Running a single test

```bash
# Single package
go test -v -race -timeout=30m ./pkg/controllers/rollout/...

# Single test by name
go test -v -race -run TestReconcile ./pkg/controllers/rollout/...

# Single Ginkgo integration test by description
cd test/scheduler && ginkgo -v --focus="should schedule"
```

### E2E tests

```bash
make setup-clusters                 # Create 3 Kind clusters
make e2e-tests                      # Run E2E suite (ginkgo, ~70min timeout)
make clean-e2e-tests                # Tear down clusters
```

## Architecture

KubeFleet is a multi-cluster Kubernetes management system (CNCF sandbox) using a hub-and-spoke model. The **hub agent** runs on a central cluster; **member agents** run on each managed cluster.

### Reconciliation Pipeline

User-created placement flows through a chain of controllers:

```
ClusterResourcePlacement / ResourcePlacement  (user intent)
        ↓
  Placement Controller → creates ResourceSnapshot + SchedulingPolicySnapshot (immutable)
        ↓
  Scheduler → creates ClusterResourceBinding / ResourceBinding (placement decisions)
        ↓
  Rollout Controller → manages staged rollout of bindings
        ↓
  Work Generator → creates Work objects (per-cluster manifests)
        ↓
  Work Applier (member agent) → applies manifests, creates AppliedWork
        ↓
  Status flows back: AppliedWork → Work status → Binding status → Placement status
```

### API Naming Convention

CRDs starting with `Cluster` are cluster-scoped; the name without the `Cluster` prefix is the namespace-scoped counterpart. For example: `ClusterResourcePlacement` (cluster-scoped) vs `ResourcePlacement` (namespace-scoped). This affects CRUD operations — namespace-scoped resources require a `Namespace` field in `types.NamespacedName`.

### Scheduler Framework

Pluggable architecture modeled after the Kubernetes scheduler:
- Plugin interfaces: `PreFilterPlugin`, `FilterPlugin`, `PreScorePlugin`, `ScorePlugin`, `PostBatchPlugin`
- Built-in plugins: `clusteraffinity`, `tainttoleration`, `clustereligibility`, `sameplacementaffinity`
- Placement strategies: **PickAll** (all matching), **PickN** (top N scored), **PickFixed** (named clusters)
- Plugins share state via `CycleStatePluginReadWriter`

### Snapshot-Based Versioning

All policy and resource changes create immutable snapshot CRDs (`ResourceSnapshot`, `SchedulingPolicySnapshot`, `OverrideSnapshot`). This enables rollback, change tracking, and consistent scheduling decisions.

## Terminology

- **Fleet**: A collection of clusters managed together
- **Hub Cluster**: Central control plane cluster
- **Member Cluster**: A managed cluster in the fleet
- **Hub Agent**: Controllers on the hub for scheduling and placement
- **Member Agent**: Controllers on member clusters for applying workloads and reporting status

## Code Conventions

- Follow the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- Favor standard library over third-party libraries
- PR titles must use a prefix: `feat:`, `fix:`, `docs:`, `test:`, `chore:`, `ci:`, `perf:`, `refactor:`, `revert:`, `style:`, `interface:`, `util:`, or `[WIP] `
- Always add an empty line at the end of new files
- Run `make reviewable` before submitting PRs

### Controller Pattern

All controllers embed `client.Client`, use a standard `Reconcile` loop (fetch → check deletion → apply defaults → business logic → requeue), update status via the status subresource, and record events. Error handling uses categorized errors (API Server, User, Expected, Unexpected) for retry semantics. See existing controllers in `pkg/controllers/` for reference.

### API Interface Pattern

Resources implement `Conditioned` (for status conditions) and `ConditionedObj` (combining `client.Object` + `Conditioned`). See `apis/interface.go`.

## Testing Conventions

- **Unit tests**: `<file>_test.go` in the same directory; table-driven style
- **Integration tests**: `<file>_integration_test.go`; use Ginkgo/Gomega with `envtest`
- **E2E tests**: `test/e2e/`; Ginkgo/Gomega against Kind clusters
- Do **not** use assert libraries; use `cmp.Diff` / `cmp.Equal` from `google/go-cmp` for comparisons
- Use `want` / `wanted` (not `expect` / `expected`) for desired state variables
- Test output format: `"FuncName(%v) = %v, want %v"`
- Compare structs in one shot with `cmp.Diff`, not field-by-field
- Mock external dependencies with `gomock`
- When adding Ginkgo tests, add to a new `Context`; reuse existing setup

## Collaboration Protocol

### Domain Knowledge

Refer to `.github/.copilot/domain_knowledge/` for entity relationships, workflows, and ubiquitous language. Update these files as understanding grows.

### Specifications

Use `.github/.copilot/specifications/` for feature specs. Ask which specifications apply if unclear.

### Breadcrumb Protocol

For non-trivial tasks, create a breadcrumb file at `.github/.copilot/breadcrumbs/yyyy-mm-dd-HHMM-{title}.md` to track decisions and progress. Update it before and after code changes, and get plan approval before implementation. See existing breadcrumbs for format examples.
