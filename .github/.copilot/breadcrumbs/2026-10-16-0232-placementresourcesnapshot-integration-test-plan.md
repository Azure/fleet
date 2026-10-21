# Placement Resource Snapshot Manager: Integration Test Plan

**Date**: October 16, 2026 02:32 AEDT
**Task**: Plan additional envtest-based integration tests for `pkg/v1/managers/placementresourcesnapshot/`.

## Baseline

Measured with `go test ./pkg/v1/managers/placementresourcesnapshot/... -coverprofile` (envtest 1.33.0): **72.9%** of statements.

| Function | Coverage |
|---|---|
| `resources.go` `retrieveResourceByName` | 47.1% |
| `resources.go` `retrieveResourcesByLabelSelector` | 53.1% |
| `resources.go` `snapshotResource` | 72.7% |
| `resources.go` `lookUpGVKGVRAndNamespace` | 75.0% |
| `resources.go` `retrieveAndHashSelectedResources` | 77.3% |
| `uniquename.go` (both functions) | 20.0% |
| `snapshots.go` `placementPolicyOwnerLabelVal` | 50.0% |
| `ops.go` `retrieveLatestSnapshot` / `isSnapshotUpToDate` / `cleanUpOrphanedSecondarySnapshots` | 74–79% |

The existing suite (`manager_integration_test.go`, ~3.5k lines) covers the snapshot lifecycle for both scopes:
create, up-to-date, stale, split by count/size, oversized resource, orphaned secondaries, and default-limit GC.
Every test selects only ConfigMaps/Deployments in the `app` namespace, so these paths are never run:

- The resource eligibility checks (new, uncommitted): namespace, GVK, and object-level checks.
- Selector validation errors (scope/namespace mismatches, missing name/label selector, bad label selector, unknown kind).
- The informer (cache) read path: the suite's informer manager never registers or starts any informer.
- Manifest sanitization of annotations (last-applied, deployment revision), plus status stripping when status is set.
- Deduplication across selectors, cluster-scoped kinds (Namespace, ClusterRole), and label selections that match nothing.
- Long or dotted policy names (hashed snapshot names, truncated owned-by label).
- Custom `ResourceRevisionHistoryLimit`, GC of multi-group snapshots, and stale-cache branches in `ops.go`.

## Layout

All files share the existing suite in `suite_test.go`. As implemented:

- `ops_gc_integration_test.go`: the former `manager_integration_test.go` (renamed at 02:48), plus a
  `Describe("GC variants")` for P2. The P2 specs were planned for a separate `gc_integration_test.go`.
- `resources_integration_test.go`: selection, eligibility, dedup, and sanitization (P0-A to P0-E).
- `corner_cases_integration_test.go`: stale-cache and inconsistent-state branches, and long and dotted names (P1).
  It replaces the planned `ops_integration_test.go` and `snapshots_integration_test.go`.
- `utils_integration_test.go`: all shared helpers (consolidated at 06:52). It replaces the planned
  `helpers_integration_test.go`; the helper names differ from the original sketch.
- The planned `resources_cache_integration_test.go` (P0-F) was not created; see section F.

## P0: `resources.go`

### A. Selector validation (DescribeTable, both scopes where relevant)

Each entry creates the policy, calls `SnapshotResourcesIfStale`, asserts the error category and message,
and asserts no snapshot was created.

| # | Setup | Want |
|---|---|---|
| A1 | PP selector `namespace` differs from the PP namespace | User |
| A2 | PP selector `namespace` equals the PP namespace | Success (positive control) |
| A3 | PP lives in `default` (and in a `kube-*` namespace) | User (policy namespace ineligible) |
| A4 | CPP selects a ConfigMap in `kube-system` | User (selector namespace ineligible) |
| A5 | CPP selects a namespaced kind without `namespace` | User |
| A6 | CPP selects ClusterRole with `namespace` set | User |
| A7 | PP selects ClusterRole | User (cluster-scoped via PP) |
| A8 | Denied GVK: `v1/Pod`, `coordination.k8s.io/v1/Lease`, `placement.kubefleet.dev/v1alpha1/PlacementPolicy` | User |
| A9 | Neither `name` nor `labelSelector` (allowed by CEL) | User |
| A10 | Invalid label selector (`In` with no values; unknown operator) | User |
| A11 | Unknown kind (typo) | Currently Unexpected (see open question 1) |
| A12 | Named resource does not exist; then create it | APIServer error, then success on retry |
| A13 | In-memory accessor with empty selectors (CRD enforces MinItems=1); nil accessor | Unexpected |

### B. Object eligibility, name-based (explicit selection fails)

Use a fresh namespace, because envtest runs no controller-manager and these objects must be created by hand.

- ConfigMap `kube-root-ca.crt`, ServiceAccount `default`, a Secret of type `kubernetes.io/service-account-token`:
  each returns a User error whose message carries the reason. Control: an Opaque Secret succeeds.
- A ConfigMap with `ownerReferences` returns a User error.
- A terminating ConfigMap (finalizer + delete) returns a User error.
- A CPP selecting Namespace `kube-system` by name returns a User error ("selected namespace is not eligible").
- A CPP selecting a fresh Namespace by name succeeds. Verify the manifest: status stripped, spec matches the live object.

### C. Object eligibility, label-based (ineligible objects are skipped)

- A fresh namespace holds eligible ConfigMaps plus `kube-root-ca.crt`, an owned ConfigMap, and a terminating ConfigMap,
  all carrying the same label. The snapshot contains only the eligible ones.
- Dynamic case: add an ownerRef to an eligible ConfigMap. The snapshot goes stale and the new one excludes it.
  Remove the ownerRef and the ConfigMap is included again.
- A CPP selects Namespaces with `kubernetes.io/metadata.name In [default, kube-system, kube-public, <fresh>]`.
  Only `<fresh>` is snapshotted.
- A label selector that matches nothing creates a primary snapshot with zero resources (pin; open question 4).
  Adding a matching object makes it stale and produces a new snapshot.

### D. Deduplication and ordering

- The same ConfigMap selected by name and by label appears once.
- An HPA selected via `autoscaling/v1` and then `autoscaling/v2` appears once with `Identifier.APIVersion == v1`
  (the first selector wins). Swapping the selector order changes the hash, and the new snapshot carries v2.
- Reordering selectors for distinct resources keeps the same hash, so `SnapshotResourcesIfStale` returns the
  existing snapshot (no new index).
- A CPP selecting cluster-scoped kinds (ClusterRole, Namespace) plus namespaced resources across two namespaces:
  resources are sorted by `group/kind/namespace/name`.

### E. Manifest sanitization (`snapshotResource`)

Assert on raw JSON (`map[string]any`) so that the absence of keys is checked, not just typed zero values.

- ConfigMap with `kubectl.kubernetes.io/last-applied-configuration` plus a custom annotation: only the custom one remains.
- ConfigMap with only last-applied: `metadata.annotations` is absent.
- Deployment with a `deployment.kubernetes.io/revision` annotation and a status set via the status subresource:
  both are stripped, and `uid`, `resourceVersion`, `generation`, `creationTimestamp`, `managedFields` are absent.
- Finalizers on a selected object: pin the current behavior, which is that they are retained (open question 2).
- Changes that must not trigger a new snapshot:
  - Updating only the last-applied annotation keeps the hash unchanged.
  - A status-only update keeps the hash unchanged.

### F. Informer (cache) path, with a dedicated `Manager`

**Not implemented as planned.** At 07:30 the shared suite started reading Deployments through the cache instead (see
Status). That covers name-based and label-based reads from the cache, and mixed policies that read ConfigMaps from
the API server. These planned checks are still uncovered: identical hashes from the cache and API server paths for
the same policy, a cache miss returning an APIServer error, the fallback for a registered but unsynced informer, and
ineligible objects read through the cache.

The original plan:

- Build a separate `informer.Manager` and `AddStaticResource` for `v1/ConfigMap` and `v1/Namespace`.
  Then call `Start` and `WaitForCacheSync`, and construct a second `Manager` with `New(hubMgr, ...)`.
  It does not need to be added to `hubMgr`.
- Name-based and label-based selection through the cache must yield resources and a hash identical to the
  API-server-path manager for the same policy. This is the key invariant, and it also guards the informer transform.
- A name not in the cache returns an APIServer error.
- One representative ineligible object each for name-based (error) and label-based (skip).
- A mixed policy reads the ConfigMap from the cache and the Deployment via the API server fallback.
- An informer that is registered but not started falls back to the API server and succeeds.
- After a ConfigMap update, `Eventually` the hash changes (cache convergence).

## P1: `ops.go` stale-cache and inconsistency branches

**Technique**: snapshot objects without the owned-by label are not indexed (see `fieldindexers/hub.go`), so the cached
field-index lists never see them, while the uncached quorum reads by name do. This drives the
"cache is stale" branches deterministically.

1. A decoy named `uniqueNameForPrimary(P, latest+1)` makes `SnapshotResourcesIfStale` return Transient.
   Also pin that `SnapshotResourcesIfNoSnapshotExists` fails the same way (open question 3).
2. A decoy at the primary name for index 0 on a fresh policy returns an APIServer error (AlreadyExists) and creates no
   secondaries. After the decoy is removed, the call succeeds.
3. A decoy at the secondary name `(0, 1)` on a multi-group policy returns an APIServer error and the primary is not created.
   After the decoy is removed, the call succeeds.
4. A labeled secondary at the next index plus an unlabeled primary decoy at the same index returns Transient,
   and the secondary is **not** deleted.
5. An orphaned secondary with a finalizer returns Transient repeatedly while it is terminating.
   After the finalizer is removed, creation proceeds.
6. An extra labeled secondary beyond the count label at the latest index: the call returns exactly `count`
   snapshots, sorted by sub-index, with no error.
7. Deleting one secondary after creation returns Unexpected ("fewer sub-indexed ... than the count label").
8. Hand-crafted labeled primaries with a count of `abc`, a count of `0`, or an index of `abc` return Unexpected.
   The index case needs at least 2 primaries so that the sort comparator runs.
9. Concurrency: N goroutines call `SnapshotResourcesIfStale` on one policy.
   - Exactly one snapshot exists per index.
   - Any errors are retryable (Transient or APIServer).
   - The policy `Eventually` converges to up-to-date.

## P1: Long and dotted names

- A PP name of about 240 characters (owned-by label truncated and hashed; snapshot names hashed), with
  multi-group snapshots:
  - Names match `uniqueNameFor*()`.
  - The label matches `placementPolicyOwnerLabelVal`.
  - The annotation keeps the full name.
  - The stale path produces index 1, and GC plus the binding in-use check work, since they rely on the annotation.
- A dotted name (`app.policy.v1`) produces dot-free hashed snapshot names, and the flow works end to end.
- Two long names sharing their first 48 characters do not interfere: each policy sees only its own snapshots.
- One CPP variant.

## P2: GC variants

- `ResourceRevisionHistoryLimit=1`: the previous snapshot is GC'd after the next retrieval.
  With a limit of 5, five snapshots are kept. Lowering the limit from 5 to 1 triggers bulk GC.
- GC of multi-group snapshots removes the primary and all of its secondaries.
  Existing GC tests cover only single-group snapshots.
- A terminating snapshot (with a finalizer) is not re-enqueued, and GC completes once the finalizer is removed.

## Out of Scope

These are unit-test territory or unreachable:

- `New()` argument validation.
- Hash, marshal, and `SetControllerReference` failures.
- API server transport errors.
- `AdditionalInfo` sizing.
- The GC owner-UID mismatch case, which `gc_test.go` already covers.

## Open Questions (all resolved; see Status)

1. A typo in the selector Kind fails REST mapping and returns an **Unexpected** error. Should it be a User error
   (`meta.IsNoMatchError`)? **Resolved at 03:07**: it is now a User error.
2. `snapshotResource` keeps `metadata.finalizers`, so they would be propagated to member clusters. Is that intended?
   **Resolved at 03:07**: finalizers are now stripped.
3. `SnapshotResourcesIfNoSnapshotExists` still runs `isSnapshotUpToDate` and can fail with Transient even though
   existing snapshots are present. Is that intended? **Resolved at 05:33**: yes; a retry succeeds once the cache
   catches up.
4. A label selection that matches nothing produces an empty primary snapshot. Is that intended?
   **Resolved at 03:07**: an empty selection is now a User error.

Q5 to Q9 came up during the P1 and P2 planning; their questions and answers are in the Status log.

## Validation

- `KUBEBUILDER_ASSETS=$(hack/tools/bin/setup-envtest-release-0.22 use 1.33.0 -p path) go test ./pkg/v1/managers/placementresourcesnapshot/... -coverprofile=...`
- Targets: `resources.go` at 90% or more for every function except unreachable branches; package at 85% or more.
  Dropped at 06:41: per the user, coverage is no longer measured.
- Suite runtime increase of about 60s or less (baseline is about 58s).
- `make reviewable`.
- Final runs use `ginkgo -p --race` instead of `go test`.

## Status

The detailed P0, P1, and P2 plans mentioned below were working documents and were not committed. Their spec IDs map
to test contexts by position, in file order: PP-n and CPP-n are the n-th `Context` of the two `Describe`s in
`resources_integration_test.go`; SC-n and NM-n are those of the two `Describe`s in `corner_cases_integration_test.go`
(SC-1 runs once per scope, as the first two contexts); and GC-n are those of `Describe("GC variants")` in
`ops_gc_integration_test.go`.

- 02:32: High-level plan drafted.
- 02:48: The existing IT file has been renamed to `ops_gc_integration_test.go`.
  A detailed plan for P0-A to P0-E was written, with resource selection and eligibility tests going in
  `resources_integration_test.go`.
  Its environment facts, error messages, and manifest shapes were verified with a throwaway envtest probe spec,
  which has since been removed.
  - Implementation then waited for approval and for answers to Q1, Q2, and Q4.
- 03:07: The user resolved Q1, Q2, and Q4 in code (`resources.go`, `ops.go`), and the detailed plan was updated:
  - Q1: unknown kinds, versions, and groups are now user errors. Added cases A11c (unknown group) and
    A11d (unknown kind through a label selector, which covers the label-path lookup error).
  - Q2: finalizers are now stripped. PP-9 asserts their absence and that a finalizer-only change creates no snapshot;
    CPP-2 asserts that the Namespace's `spec.finalizers` is kept.
  - Q4: an empty selection is now a user error. PP-5 covers it both with and without an existing snapshot, and
    CPP-3 adds an "every match is ineligible" case. The empty branch of the size-controlled split is now
    unreachable through the public entry points.
  - The existing `ops_gc_integration_test.go` suite still passes (62s).
  - Q3 (`createOnlyWhenMissing` failing on a quorum-read transient error) was left open, as it is outside P0-A to
    P0-E. It was resolved at 05:33.
- 03:12: P0-A to P0-E were implemented in `pkg/v1/managers/placementresourcesnapshot/resources_integration_test.go`.
  - The file contains 14 ordered contexts and 78 specs.
  - It adds shared helpers for error and category checks, snapshot reads from the cache, absence checks, and cleanup.
  - Validation:
    - The focused run passed (78/78, about 22s).
    - The full package run with `-race` passed (90s), and coverage rose from 72.9% to 78.7%.
    - `make lint` (`--fast-only`) reported 0 issues. Under `lint-full`, the new file has only `goconst` findings
      on string literals, the same style as `ops_gc_integration_test.go`.
    - A mutation check confirmed that the intended specs fail when finalizer stripping, deduplication, or the
      label-path eligibility check is disabled.
- 04:49: A detailed P1 plan was written.
  - It covers the stale-cache and inconsistent-state branches (SC-1 to SC-9) and long and dotted names (NM-1 to NM-5).
  - It targets `corner_cases_integration_test.go`. The requested `..._tests.go` name would not be compiled as a test file.
  - Every behavior was verified with a throwaway envtest probe, since removed.
  - **Finding (Q5)**: concurrent calls can hit the `unexpected` "found a primary placement resource snapshot at the
    given index while cleaning up orphaned secondary snapshots" error under ordinary cache lag (1 in 8 calls in the
    probe). Recommendation: make it transient.
  - New questions: Q6 (a missing secondary is unrecoverable) and Q7 (a known label-collision limitation).
- 05:26: The user resolved Q5 and Q7:
  - **Q5**: the sanity-check failure in `cleanUpOrphanedSecondarySnapshots` is now `transient`. Verified with a probe:
    12 races in 192 concurrent calls, all transient, with no `unexpected` errors and exactly one set of snapshots per
    index.
  - **Q7**: ownership lookups now key on the owned-by annotation (the full name).
    - Changed code: the `hub.go` extractors, the `ops.go` field matchers, `gc.go`, and `workgenerator/retrieval.go`.
    - The final code is consistent; checked statically.
    - A mid-edit state (index still keyed on the label, GC on the full name) leaked secondaries in GC for names over
      61 characters, as the probe showed.
    - Plan updates: indexed decoys need the annotation, `verifyOwnedSnapshots` filters by annotation, and the new
      NM-6 guards the former collision.
  - Follow-up: one `workgenerator` suite run during the Q7 edits failed its 10 "multiple placement resource snapshots"
    specs. A full re-run on the final code was interrupted at the time; the suite passed in the 07:50 review.
- 05:33: The user resolved Q3 and Q6 as expected behavior:
  - **Q3**: the transient error is fine; a retry finds the newer snapshot once the cache catches up. Verified with a
    probe that simulates the catch-up by turning the decoy into an indexed snapshot: both entry points then return
    index 1 as up to date, and no index 2 is created. SC-1 now ends with this catch-up instead of removing the decoy.
  - **Q6**: a missing secondary needs manual recovery. Verified with a probe:
    - A resource change alone does **not** recover; the call still returns `unexpected`, because
      `retrieveLatestSnapshot` fails before the selected resources are read.
    - Deleting the broken primary does recover; the next call re-creates the set.
    - SC-7 now checks all three stages.
  - No open questions remain for the P1 plan.
- 06:24: A detailed P2 plan was written.
  - It targets a new `Describe("GC variants")` in `ops_gc_integration_test.go`, with seven contexts (GC-1 to GC-7):
    limit changes, multi-group GC (CPP, plus an extra secondary), the GC request queue, snapshots pending deletion,
    retryable and non-retryable GC failures, and same-named policies across namespaces and scopes.
  - New technique: a *paused manager*, a second `Manager` built with `New(hubMgr, ...)` that is never started. Its
    workqueue can be drained to assert the exact GC requests, deduplication, and in-memory limit overrides.
  - Overlap is avoided: NM-2 already covers limit 1 with two groups (PP), and `gc_test.go` covers the UID and owner
    mismatch cases.
  - Every behavior was verified with a tag-guarded throwaway envtest probe (`//go:build gcprobe`), since removed.
    With the probe, coverage moved as follows:
    - `enqueueOldResourceSnapshotsForGC`: 84.6% to 100%
    - `garbageCollect`: 82.1% to 87.5%
    - `Start`: 65.5% to 93.1%
    - Package: 86.7% to 89.1%
  - New questions:
    - **Q8**: an out-of-band deletion of an old primary leaks its secondaries; the plan pins the current behavior.
    - **Q9** (ops.go, outside P2): a terminating latest primary is returned as up to date.
    - Minor: the owned-by annotation check in `garbageCollect` is duplicated, so the second check is dead code.
  - Implementation then waited for approval and for an answer to Q8.
- 06:36: The user resolved the P2 questions, and the plan was updated:
  - **Q8** is expected behavior: it does not happen in normal operation, and a guard would be too expensive. GC-4
    step 5 pins it.
  - **Q9**: a sanity check in `retrieveLatestSnapshot` (ops.go:184–188) now returns a transient error when the latest
    primary is marked for deletion. It was verified with a tag-guarded probe, since removed:
    - Both entry points fail as transient, including after a resource change, and no new index is created.
    - GC-4 steps 1 to 8 are unchanged.
    - Added GC-4 step 9 and the constant `latestSnapshotPendingDeletionErrMsg`.
  - The duplicated owned-by check in `garbageCollect` was removed. The coverage figures were recomputed:
    `garbageCollect` 83.6% to 89.1%, and the package about 86.7% to 89.2%.
  - No open questions remained for P2, and implementation waited for approval.
- 06:41: P2 was implemented as `Describe("GC variants")` at the end of `ops_gc_integration_test.go`.
  - It has seven ordered contexts (GC-1 to GC-7) and 57 specs.
  - New constants were added to the existing `const` block. New helpers, including the paused manager, `checkOwnedSnapshots`, and
    `createPlacementBinding`, sit just before the new `Describe`.
  - Validation:
    - `ginkgo -p --race .` passed 316 of 316 specs, with about 49s of spec time.
    - `make lint` reported 0 issues.
  - Per the user, coverage is no longer measured, and the planned mutation checks were skipped (no repeated runs).
- 06:52: All helper functions were moved out of the three `*_integration_test.go` files into a new `utils_integration_test.go`.
  - 61 functions moved, plus the helper-only types `snapshotErrTestCase` and `snapshotCallResult`.
  - `suite_test.go` and `gc_test.go` were left untouched, per the user.
  - Constants and spec-only types (`decoySnapshotSpec`) stay in their files.
  - The move was done mechanically with an AST-based tool, so the bodies and doc comments are byte-for-byte identical. The
    source files only lost lines, and the set of functions is unchanged.
  - Validation:
    - `ginkgo -p --race .` passed 316 of 316 specs.
    - golangci-lint (`--fast-only`) on the package reported 0 issues.
- 07:50: A code review of the branch found several issues, which have been fixed:
  - Review fixes:
    - `resourceeligibility.New` now takes a `CheckerOptions` struct instead of three positional bools.
      `Baseline(restMapper)` always runs the object checks; the v0 default uses an unexported variant that skips them.
    - `AllowsNamespace` allows `""` (cluster-scoped resources) before honoring the skip flag, so a skipped check in
      allow-list mode no longer denies cluster-scoped resources.
    - The manager's `New` rejects a nil eligibility checker.
    - `retrieveAndHashSelectedResources` checks the placement policy's namespace once, before the selector loop.
    - `gc.go`: a stale "owned-by label" comment was corrected, and a redundant alias was removed.
  - Deployments are now read through the cache:
    - `suite_test.go` registers a Deployment informer with the dynamic informer manager (`CreateInformerForResource`,
      `Start`, `WaitForCacheSync`). It replaces a controller-runtime watch on Deployments that nothing read from.
      ConfigMaps and all other kinds are still read from the API server.
    - This exposed cache-lag races: 1 of 316 specs failed.
    - Fix: two new helpers in `utils_integration_test.go`, `waitForDeploymentsInCache` and
      `waitForDeploymentsRemovedFromCache`. They are called after every Deployment write in `ops_gc_integration_test.go`
      and `resources_integration_test.go`.
    - The informer applies events in order, so waiting for a context's own writes also covers earlier contexts'
      deletions. The `AfterAll` blocks need no waits.
  - Validation:
    - `ginkgo -p --race` passed for `placementresourcesnapshot`, `workgenerator`, `resourceeligibility`,
      `resourcewatcher`, and `utils/controller`.
    - `golangci-lint --fast-only` reported 0 issues.
    - The user confirmed the final build and test runs.
  - Still open (not code):
    - The PR description must list this branch's behavior changes:
      - Ownership lookups key on the owned-by annotation.
      - GC deletes use UID preconditions.
      - Finalizers are stripped from snapshotted manifests.
      - Error categories changed:
        - An unknown GVK is a User error.
        - An empty selection is a User error.
        - A terminating latest primary is Transient.
        - The orphan-cleanup sanity check is Transient.
        - A contents-hash mismatch is Unexpected.
      - The `Checker` API changed: `AllowsObject` also returns a reason, and `New` takes `CheckerOptions`.
    - PR #974 also edits `guard_test.go`, so a merge conflict is expected.
