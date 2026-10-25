# Placement Resource Snapshot Manager: Integration Test Plan

**Date**: October 16, 2026 02:32 AEDT
**Revised**: October 24, 2026 07:20 AEDT, for leftover snapshot detection and UID-based snapshot names (see
[Revision: leftover snapshots and UID-based names](#revision-leftover-snapshots-and-uid-based-names)).
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

- A PP name of about 240 characters (owned-by label truncated and hashed; snapshot names truncated), with
  multi-group snapshots:
  - Names match `uniquename.PrimaryPlacementResourceSnapshotName` and `SecondaryPlacementResourceSnapshotName`,
    computed with the policy UID (revised October 24; names were `uniqueNameFor*()` before).
  - The label matches `placementPolicyOwnerLabelVal`.
  - The annotation keeps the full name.
  - The stale path produces index 1, and GC plus the binding in-use check work, since they rely on the annotation.
- A dotted name (`app.policy.v1`) produces dot-free snapshot names, and the flow works end to end.
- Two long names sharing their first 48 characters do not interfere: each policy sees only its own snapshots.
- One CPP variant.

## P2: GC variants

- `ResourceRevisionHistoryLimit=1`: the previous snapshot is GC'd after the next retrieval.
  With a limit of 5, five snapshots are kept. Lowering the limit from 5 to 1 triggers bulk GC.
- GC of multi-group snapshots removes the primary and all of its secondaries.
  Existing GC tests cover only single-group snapshots.
- A terminating snapshot (with a finalizer) is not re-enqueued, and GC completes once the finalizer is removed.

## Revision: leftover snapshots and UID-based names

Added October 24. It revises the plan for the snapshot manager changes that handle placement policies deleted and
re-created with the same name.

### What changed in the code

- `retrieveLatestSnapshot` (`ops.go`) now runs `checkForInconsistentResourceSnapshots` (`gc.go`) on the primary
  snapshots, right after sorting them. Both entry points are affected. For each primary:
  - **Owned by a same-named policy with another UID** (the previous policy is still being garbage-collected):
    Transient, "found a placement resource snapshot owned by a different placement policy of the same name (e.g., one
    that has been deleted); waiting for garbage collection to complete". The scan stops.
  - **No owner references** (left over from a policy deleted with orphan propagation): a GC request is enqueued,
    unless the snapshot is already marked for deletion. Either way, after the scan, the call fails as Transient,
    "leftover resource snapshots are detected; waiting for garbage collection to complete"; the error names the
    leftover snapshots.
  - **Other owner references** (a differently named owner, an extra owner, the wrong kind): User, "found a placement
    resource snapshot (NAME) that has unexpected owner references". The name is in the message, so it reaches the
    policy status.
  - While blocked, no history GC requests are enqueued and no snapshot is created.
- `garbageCollect` no longer requires the snapshots at the primary's index to be owned by the policy; orphaned groups
  are deleted too. The comment at the top of `garbageCollect` explains why this is safe in normal operations: the block
  above, plus `cleanUpOrphanedSecondarySnapshots`. As a sanity check against heavy cache lag, it skips (and logs) any
  listed snapshot that is another primary, or whose policy owner (or lack of one) differs from the primary's. Since
  snapshot names include the policy UID, the API server no longer stops two policy generations from sharing an index.
- `isSnapshotInUseByAnyPlacementBinding` takes the policy's namespaced name from the GC request.
  - A snapshot without an owner reference to the policy is never in use, so orphans are collected even when a
    binding still names them.
  - Binding owners are no longer checked; any binding that names the policy and the snapshot keeps it.
  - So "in use" never produces an error any more. GC returns nil and the request is dropped, not retried, until the
    next retrieval enqueues it again.
- Snapshot names moved to the exported `uniquename` package. Primary and secondary names always end in a
  12-character hash of the policy name, index, sub-index (secondaries only), and **policy UID**. Long or dotted names
  are truncated and sanitized; there is no longer a separate "hashed" form.
- Known gap, accepted and documented on `checkForInconsistentResourceSnapshots`: an ownerless secondary without a
  primary is not detected. It lingers until the policy creates snapshots at that index.

### Existing tests: already adapted, no action needed

The current tests pass: `go test -race ./pkg/v1/managers/placementresourcesnapshot/...` took 195s, and the
`uniquename` package about 3s.

| Area | Adaptation |
|---|---|
| Every name expectation | Uses `uniquename.*` with the policy UID, computed after the policy is created |
| Indexed decoys (SC-1, SC-8) | Owned by the policy (`createOwnedDecoySnapshot`, `decoyOwnerRefs`); without an owner they would now count as leftovers and be garbage-collected |
| NM-1 name length boundaries | 216, 217, 218, 219, and 253 characters (previously 229 to 232, and 253); "hashed" is now "truncated" |
| `autorollout` and `stagedupdaterun` suites | Their name helpers take the policy instead of its name |

Unindexed decoys (no owned-by annotation) are never scanned and need no owner references.

### Removed

- **GC-5, "retryable GC failures" and "non-retryable GC failures"**, with the constants `gcStaleOwnerInUseErrMsg`,
  `gcMissingOwnerInUseErrMsg`, and `staleOwnerUID`. The errors they checked no longer exist. They are already gone
  from `ops_gc_integration_test.go`.
  - They were the only integration coverage of two things, both re-added below:
    - a binding blocking GC until it goes away;
    - the GC worker's retry (backoff) and drop paths in `Start`.
- The out-of-scope entry for "the GC owner-UID mismatch case" (rewritten in Out of Scope).

### Unit test coverage (do not duplicate in integration)

| Unit test | Covers |
|---|---|
| `uniquename/uniquename_test.go` | Name format, a different UID giving a different name, dots, truncation boundaries, uniqueness |
| `TestCheckForInconsistentResourceSnapshots` | Every owner variant (stale UID, orphan, other name, extra owner, wrong kind, namespaced), scan order, returned leftovers, enqueued requests, orphans already marked for deletion (still leftovers, not re-enqueued) |
| `TestIsSnapshotInUseByAnyPlacementBinding` | Orphan bypass, other-name and wrong-kind owners, cross-namespace bindings, list failure |
| `TestGarbageCollect` | Owned and orphaned groups, in use, re-created primary, primary not found, Get failure, skipped snapshots at the same index (another primary, a different or missing policy owner) |
| `TestStart` | The GC worker loop: retry with backoff, drop on Unexpected errors, stop on context cancellation (added with this revision; see below) |

**Not covered anywhere:** `IsSnapshotSynchronized` (a gap that predates this change, including its owner-UID check).
At planning time, the `Start` worker loop was not covered either; `TestStart` now covers it.

The integration cases below therefore take one representative per owner variant. They focus on what unit tests
can't show: real field indexes and the cache, the running GC worker, recovery once the block clears, and both entry
points.

### Envtest constraints

- **There is no kube-controller-manager, so no Kubernetes GC.**
  - Deleting a policy leaves its snapshots in place with the old owner UID. That is exactly the "previous policy
    still being collected" state. Deleting the old snapshots by hand stands in for that collection finishing.
  - Orphan propagation can't complete: the policy would hang on the `orphan` finalizer. Simulate it by removing the
    snapshots' owner references, then deleting the policy normally.
- **Some helpers don't work while a previous policy's snapshots exist.** `snapshotResourcesAndExpectErr` and
  `verifyNoPlacementResourceSnapshots` match by the owned-by label, so they would find those snapshots. Use
  `verifyNoPrimarySnapshotAtIdx`, which builds the name with the policy's current UID, or a UID-aware variant.

### Cases to add

- **Placement:** LO-1 to LO-5 go in a new `Describe("corner cases (leftover snapshots from previous placement
  policies)")` in `corner_cases_integration_test.go`. GC-5 goes back into `Describe("GC variants")`, reusing the freed
  slot.
- Wherever a call should fail, check **both entry points**.

**LO-1. Re-created policy; the previous policy's snapshots are not collected yet** (PP)

1. Create the policy and a snapshot at index 0.
2. Delete the policy and re-create it with the same name.
3. Both entry points fail as Transient ("owned by a different placement policy of the same name"):
   - nothing is created under the new UID's names (`verifyNoPrimarySnapshotAtIdx(0)`);
   - the old snapshot is untouched;
   - a paused manager's queue stays empty, so no history GC happens while blocked.
4. Delete the old snapshot by hand. The call then creates index 0, owned by the new UID, with a name different from
   the old index-0 name.

**LO-2. Re-created policy; the previous policy's snapshots are orphaned** (PP and CPP, looped like SC-1)

1. Create the policy and snapshots at index 0 and 1, with two groups at index 1 so secondaries are covered.
2. Create a binding with no owner that names the old index-1 primary (an orphaned binding).
3. Remove the owner references from all snapshots, delete the policy, and re-create it.
4. With a paused manager, the call fails as Transient ("leftover resource snapshots are detected"). The drained queue
   holds exactly one request per old primary, each carrying the policy's namespaced name.
5. With the suite manager, every old snapshot, primary and secondary, is `Eventually` gone, even though the binding
   still names one.
6. The call then `Eventually` succeeds with index 0 under the new UID. No old name is reused, and the binding is left
   untouched.

**LO-3. An orphaned primary pending deletion** (PP)

1. Create the policy and a snapshot at index 0. Give the snapshot a finalizer, orphan it, and re-create the policy.
   (Implemented with a single snapshot; the LO-2 setup, as first planned, adds nothing here.)
2. Once the GC worker has marked the snapshot for deletion, calls stay Transient ("leftover resource snapshots are
   detected", `Consistently`), and nothing is created.
3. Remove the finalizer; the call then succeeds.

**LO-4. Unexpected owner references** (PP)

1. Create the policy and a snapshot at index 0.
2. Add a second owner reference, to the selected ConfigMap, on the primary.
3. Both entry points fail as User errors whose message contains the snapshot name:
   - a change to the selected resources still creates nothing;
   - no GC request is queued (paused manager).
4. Remove the extra owner reference. The call returns index 0 again, or creates index 1 if the resources changed.

The other owner variants are unit-tested; one representative is enough here.

**LO-5. Leftover secondaries without a primary** (PP; pins the documented limitation)

1. Before creating the policy, create two ownerless indexed secondaries for its name, named with an old UID: index 0
   sub-index 1, and index 1 sub-index 1.
2. Create the policy. The leftovers don't block the first call. That call removes the index-0 leftover and fails as
   Transient ("cleaned up orphaned secondary ..."); the next call creates index 0. The index-1 leftover is still there.
3. Change the selected resources. The index-1 leftover is cleaned up the same way, then index 1 is created.

**GC-5. Snapshots in use by placement bindings** (PP; replaces the removed GC-5)

1. Create a policy with `ResourceRevisionHistoryLimit` 1 and a snapshot at index 0, plus a binding owned by the
   policy that names index 0.
2. Create index 1, then call again; that retrieval sees both indices and enqueues index 0.
3. Once the GC worker has picked up the request (its workqueue is empty), index 0 is kept (`Consistently`), and
   `NumRequeues` for its request stays 0: the request is dropped, not retried.
4. With a paused manager, `garbageCollect` returns nil and index 0 is kept.
5. Point the binding to index 1. The next retrieval enqueues index 0 again, and it is collected.
6. A binding with no owner that names an outdated snapshot also keeps it, since binding owners are not checked.

**GC worker loop (`Start`) — a unit test, not integration.** Add `TestStart` to `gc_test.go` (or a new
`manager_test.go`), using the fake client with an interceptor:

- A retryable failure (a Delete error) is retried with backoff (`NumRequeues` of at least 1). Once Delete succeeds,
  the snapshots are gone and `NumRequeues` drops back to 0.
- An unexpected failure (a malformed index label) is dropped: `NumRequeues` stays 0, and later requests are still
  processed.
- Canceling the context stops the loop.

Integration can no longer produce a retryable GC error deterministically without an admission policy, which is why
this moves to a unit test.

**Optional (a gap that predates this change):** cover `IsSnapshotSynchronized`, as a unit or integration test:

- current and stale hashes;
- a non-primary snapshot;
- a snapshot owned by another policy;
- a stale owner UID (Transient).

## Out of Scope

These are unit-test territory or unreachable:

- `New()` argument validation.
- Hash, marshal, and `SetControllerReference` failures.
- API server transport errors.
- `AdditionalInfo` sizing.
- The owner-reference variants of the leftover check, the in-use check, and GC, which `gc_test.go` covers, and
  snapshot name formatting, which `uniquename/uniquename_test.go` covers. (Revised October 24: this entry used to
  name the GC owner-UID mismatch case, which no longer exists.)

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
- October 24, 07:20: The plan was revised for leftover snapshot detection and UID-based snapshot names; see
  [Revision: leftover snapshots and UID-based names](#revision-leftover-snapshots-and-uid-based-names).
  - Existing tests were already adapted: names use `uniquename.*` with the policy UID, indexed decoys are owned by
    the policy, and the NM-1 boundaries moved. `go test -race` passes for the package (195s) and for `uniquename`.
  - Removed: GC-5 (retryable and non-retryable GC failures), whose errors no longer exist.
  - To add: LO-1 to LO-5 (leftover snapshots from previous policies), a new GC-5 (snapshots in use by bindings), and
    a unit test for the GC worker loop. Optional: `IsSnapshotSynchronized`.
  - More entries for the PR description:
    - Snapshot creation is blocked while leftover primaries exist. A stale owner UID or an orphan is Transient, and
      unexpected owners are a User error.
    - GC no longer checks ownership, and the in-use check is by name only. (Revised at 07:56: GC now skips snapshots
      whose policy owner differs from the primary's.)
    - Snapshot names always carry a hash that includes the policy UID; the exported `uniquename` package builds them.
  - Implementation is waiting for approval.
- October 24, 07:35: The revision was implemented (commit `f2e74e10`).
  - **LO-1 to LO-5** are in a new `Describe("corner cases (leftover snapshots from previous placement policies)")` at
    the end of `corner_cases_integration_test.go`. There are six contexts (LO-2 runs once per scope) and 33 specs.
    Three error message constants were added: `staleOwnerSnapshotErrMsg`, `leftoverSnapshotsErrMsg`, and
    `unexpectedOwnerRefsErrMsg`.
  - **GC-5** ("snapshots in use by placement bindings", 8 specs) is in `Describe("GC variants")`, at the position of
    the removed contexts. Because the IDs are positional, the "same-named placement policies" context is now GC-6
    (it was GC-7).
  - **`TestStart`** is a new unit test in `gc_test.go` with three subtests: a retryable failure is retried with backoff
    and then forgotten, an unexpected failure is dropped while later requests still run, and canceling the context
    stops the loop. It uses a test workqueue with a 1ms backoff, the fake client with an interceptor, and the helpers
    `newTestGCQueue`, `startGCWorker`, and `pollUntil`.
  - **New shared helpers** in `utils_integration_test.go`:
    - `recreatePlacementPolicy`
    - `setSnapshotOwnerRefs`, which also waits until the cache has observed the change
    - `updatePlacementBindingSnapshot`
  - The optional `IsSnapshotSynchronized` coverage was not done.
  - Validation:
    - `ginkgo -p --race ./...` passed: 347 of 347 specs (9 processes, 53s), plus the `uniquename` unit tests. The
      focused run of the new specs matched 41.
    - `go test -race -count=30 -run TestStart` passed.
    - `golangci-lint --fast-only` reported 0 issues. In the new code, the full run flags only a `goconst` finding on
      the `ConfigMap` literal, the same style as the rest of the file.
- October 24, 07:56: Following review, `garbageCollect` gained a sanity check in its delete loop. It skips (and logs at
  `V(2)`, as a cache-lag symptom rather than an error) any snapshot at the primary's index that is another primary, or
  whose policy owner (or lack of one) differs from the primary's. The check uses the new helper
  `isSamePlacementPolicyOwner`.
  - Why: names now include the policy UID, so only the cache-based block keeps two policy generations from sharing an
    index. If the cache lags, GC of an orphaned primary could otherwise delete a re-created policy's snapshots at that
    index. Losing only its secondaries would leave the policy stuck on the "fewer sub-indexed snapshots" Unexpected error.
  - `TestGarbageCollect` has four new cases: another primary at the index; an orphaned primary alongside a re-created
    policy's snapshots; a secondary owned by another UID; and an ownerless secondary under an owned primary.
  - `checkForInconsistentResourceSnapshots` no longer re-enqueues ownerless primaries that are already marked for
    deletion, but they still block snapshotting. `TestCheckForInconsistentResourceSnapshots` has two new cases for
    this.
  - Doc and wording fixes from the review:
    - `checkForInconsistentResourceSnapshots` now returns the leftover primaries (as `klog.ObjectRef`s) instead of a
      bool, so the "leftover resource snapshots are detected" error names them, along with the policy.
    - The stale-owner error now reads "owned by a different placement policy of the same name (e.g., one that has
      been deleted)"; `staleOwnerSnapshotErrMsg` was updated to match.
    - Comments now credit the Kubernetes garbage collector (kube-controller-manager), not the API server.
    - The doc comment of `isSnapshotInUseByAnyPlacementBinding` says that binding owners are not checked.
- October 24, 08:05: Minor test fixes from the review, and breadcrumb consistency fixes.
  - `gc_test.go` builds snapshot names with `uniquename` instead of the pre-UID format. The "primary snapshot has been
    re-created" case now re-creates the snapshot under the same policy, since only the same policy can reuse a name.
  - GC-5's `verifySnapshotsKeptWithoutRetries` first waits until the GC worker has picked up the request (the
    workqueue is empty). Before, `NumRequeues` of 0 also held for a request that had not been processed at all.
  - Breadcrumb fixes:
    - The implementation entry is now timed 07:35, matching the commit, instead of 07:50.
    - The unit test table lists `TestStart`, and "not covered anywhere" no longer lists the `Start` loop.
    - The LO-3 plan matches the single-snapshot implementation.
    - The leftover check and GC-5 descriptions match the current behavior.
  - Validation: `gofmt` and `go vet` are clean, and the `gc_test.go` unit tests pass with `-race`. The integration
    suite was not re-run.
