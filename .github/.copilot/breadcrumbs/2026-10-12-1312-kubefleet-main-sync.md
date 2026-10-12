# Implementation: October 2026 KubeFleet Main Sync

## Overview

Pull the latest changes from `upstream` (`Azure/fleet`) and merge the latest `cncf` (`kubefleet-dev/kubefleet`) main branch into `Azure/fleet` while preserving Fleet-specific module paths, build settings, Azure Linux distroless bases, and linting standards.

## Key Changes & Resolutions

1. **Upstream fast-forward**: Fast-forwarded branch to `upstream/main` (`23641d78 fix: protect reconcile labels on managed jobs and deployments (#1362)`).
2. **CNCF merge**: Merged `cncf/main` (commit `b3199824`).
3. **Dockerfiles**: Preserved Azure Linux 3.0 distroless base (`mcr.microsoft.com/azurelinux/distroless/base:3.0`) for `docker/hub-agent.Dockerfile`, `docker/member-agent.Dockerfile`, and `docker/refresh-token.Dockerfile` for FIPS compatibility.
4. **Module paths**: Rewrote all incoming `github.com/kubefleet-dev/kubefleet` module references in 31 Go files to `go.goms.io/fleet`.
5. **Conflict resolutions**:
   - `apis/placement/v1alpha1/zz_generated.deepcopy.go`: Kept existing `go.goms.io/fleet/apis/placement/v1beta1` import.
   - `go.mod` / `go.sum`: Preserved Azure Fleet's `github.com/grpc-ecosystem/grpc-gateway/v2` dependency, accepted updated `ginkgo`/`gomega` and dependency bumps, ran `go mod tidy`.
   - `pkg/controllers/placement/controller_integration_test.go`: Adapted to CNCF's label-based sorting and removed `metricsutils`, referencing `go.goms.io/fleet`.
   - `pkg/controllers/placement/controller_test.go`: Added `hubmetrics` with `go.goms.io/fleet` module prefix.
   - `pkg/controllers/updaterun/controller_integration_test.go`: Adapted to condition transition timestamp assertions with `go.goms.io/fleet` imports.
   - `pkg/controllers/updaterun/execution_test.go`: Added controller utils import with `go.goms.io/fleet` prefix.
   - `pkg/utils/apiresources.go`: Disabled `kfplacementv1alpha1.GroupVersion.Group` placement with `go.goms.io/fleet` prefix.
   - `pkg/utils/labels/labels_test.go`: Retained both `TestExtractLabelFromMemberCluster` and CNCF's new error message and policy index label tests.
   - `pkg/v1/controllers/workgenerator/retrieval.go`: Added `fieldindexers` import with `go.goms.io/fleet` prefix.
   - `tools/fleet/cmd/draincluster/draincluster_test.go`: Added `condition` import with `go.goms.io/fleet` prefix.
   - `SECURITY.md`: Trimmed trailing whitespace on reporting section.

## Verification

- `go build ./...` passed.
- `go vet ./...` passed.
- Unit tests in `pkg/utils/labels`, `tools/fleet/cmd/draincluster`, `pkg/controllers/placement`, and `pkg/controllers/updaterun` passed.
