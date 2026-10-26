# Placement Resource Snapshot Manager: Integration and Unit Test Plan

**Date**: October 16, 2026 02:32 AEDT
**Revised**: October 24, 2026 07:20 AEDT, for leftover snapshot detection and UID-based snapshot names (see
[Revision: leftover snapshots and UID-based names](#revision-leftover-snapshots-and-uid-based-names)).
**Revised**: October 26, 2026 16:20 AEDT, for latest-index checks and secondary snapshot GC, and to add a unit test
plan (see [Revision: latest-index checks, secondary GC, and unit tests](#revision-latest-index-checks-secondary-gc-and-unit-tests)).
**Revised**: October 27, 2026 02:13 AEDT, for Service and Job sanitization (PP-13 and PP-14; see
[E. Manifest sanitization](#e-manifest-sanitization-snapshotresource)).
**Revised**: October 27, 2026 03:35 AEDT, for the API server fallback on cache misses in name-based selection (see the
October 27, 03:19 [Status](#status) entry).
**Task**: Plan additional envtest-based integration tests for `pkg/v1/managers/placementresourcesnapshot/`.
Revised October 26: unit tests are planned as well.

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
| `ops.go` `retrieveLatestSnapshot` / `isSnapshotUpToDate` / `cleanUpDanglingSecondarySnapshots` | 74–79% |

The existing suite (`manager_integration_test.go`, ~3.5k lines) covers the snapshot lifecycle for both scopes:
create, up-to-date, stale, split by count/size, oversized resource, dangling secondaries, and default-limit GC.
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
| A12 | Named resource does not exist; then create it | User error ("selected resource does not exist"), then success on retry (an APIServer error before October 27) |
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
- Services and Jobs (added October 27). `snapshotResource` strips the values that the hub API server fills in, as
  they would conflict with the ones each member cluster's API server generates. `TestSnapshotResource` (unit) covers
  every branch with hand-built objects, including the paths that change nothing (a headless Service, a Job with
  `manualSelector: true`). The two contexts below check the stripping against what envtest's API server actually
  fills in, and that re-creating the object with the same spec does not produce a new snapshot. They are appended
  to the end of the first `Describe`, so the existing positional IDs do not shift.
  - **PP-13, `manifest sanitization (services)`**: create a `LoadBalancer` Service with two ports and
    `externalTrafficPolicy: Local`, so that the API server fills in `clusterIP`, `clusterIPs`, a `nodePort` per
    port, and `healthCheckNodePort`. Assert that the live object carries all of them, so the spec can't pass
    trivially.
    1. Snapshot. These keys are absent from the raw manifest; the rest of the spec equals the live spec with them
       cleared, and the metadata keeps the user's labels.
    2. Delete the Service and re-create it with the same spec. The API server fills in new values.
    3. `SnapshotResourcesIfStale` returns index 0, and there is no index 1.
  - **PP-14, `manifest sanitization (jobs)`**: create a Job without `manualSelector`. Assert that the API server
    added `batch.kubernetes.io/controller-uid` to the selector and to the pod template labels, and the legacy
    `controller-uid` to the pod template labels.
    1. Snapshot. Neither `controller-uid` nor `batch.kubernetes.io/controller-uid` is left in the selector or the
       pod template labels, and the pod template has no `creationTimestamp`. The user's labels and the `job-name`
       labels are kept, and the rest of the spec equals the live spec.
    2. Delete the Job and re-create it with the same spec. It gets a new UID, and so new controller-uid labels.
    3. `SnapshotResourcesIfStale` returns index 0, and there is no index 1.
    - Envtest constraint: `batch/v1` Jobs default to orphan propagation, which adds the `orphan` finalizer, and
      envtest runs no garbage collector to remove it. Jobs are therefore deleted with background propagation:
      `deleteObjectsAndWait` takes optional `client.DeleteOption`s.

### F. Informer (cache) path, with a dedicated `Manager`

**Not implemented as planned.** At 07:30 the shared suite started reading Deployments through the cache instead (see
Status). That covers name-based and label-based reads from the cache, and mixed policies that read ConfigMaps from
the API server. Since October 27, a name-based cache miss is confirmed with a read from the API server (see the
October 27, 03:19 Status entry): PP-2b covers a name in neither the cache nor the API server, and the unit test
`TestRetrieveResourceByName` covers the fallback itself and an unsynced informer. These planned checks are still
uncovered: identical hashes from the cache and API server paths for the same policy, and ineligible objects read
through the cache.

The original plan:

- Build a separate `informer.Manager` and `AddStaticResource` for `v1/ConfigMap` and `v1/Namespace`.
  Then call `Start` and `WaitForCacheSync`, and construct a second `Manager` with `New(hubMgr, ...)`.
  It does not need to be added to `hubMgr`.
- Name-based and label-based selection through the cache must yield resources and a hash identical to the
  API-server-path manager for the same policy. This is the key invariant, and it also guards the informer transform.
- A name not in the cache returns an APIServer error. (Superseded on October 27: a cache miss falls back to the API
  server, and a name missing there too is a User error.)
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
5. A dangling secondary with a finalizer returns Transient repeatedly while it is terminating.
   After the finalizer is removed, creation proceeds.
6. An extra labeled secondary beyond the count label at the latest index: the call returns exactly `count`
   snapshots, sorted by sub-index, with no error. (Revised October 26: the extra must be owned by the policy, since
   an ownerless one is now a leftover, and it is also enqueued for GC and deleted; see SC-6 in the October 26
   revision.)
7. Deleting one secondary after creation returns Unexpected ("fewer sub-indexed ... than the count label").
8. Hand-crafted labeled primaries with a count of `abc`, a count of `0`, or an index of `abc` return Unexpected.
   The index case needs at least 2 primaries so that the sort comparator runs. (Revised October 26: the count-label
   messages were reworded, and a non-contiguous sub-index case is added; see SC-8 in the October 26 revision.)
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
  Existing GC tests cover only single-group snapshots. (Revised October 26: GC-2 adds its extra secondary only once
  that index is no longer the latest, since an extra at the latest index is now collected on its own.)
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
    "orphaned resource snapshots are detected; waiting for garbage collection to complete"; the error names the
    orphaned snapshots.
  - **Other owner references** (a differently named owner, an extra owner, the wrong kind): User, "found a placement
    resource snapshot (NAME) that has unexpected owner references". The name is in the message, so it reaches the
    policy status.
  - While blocked, no history GC requests are enqueued and no snapshot is created.
- `garbageCollect` no longer requires the snapshots at the primary's index to be owned by the policy; orphaned groups
  are deleted too. The comment at the top of `garbageCollect` explains why this is safe in normal operations: the block
  above, plus `cleanUpDanglingSecondarySnapshots`. As a sanity check against heavy cache lag, it skips (and logs) any
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
  primary is not detected. It lingers until the policy creates snapshots at that index. (Revised October 26: the gap
  is narrower; ownerless secondaries at the latest index are now detected. See LO-6.)

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

(Revised October 26: this table lists the unit tests as of October 24, plus the two added October 27 on `main`. The
full unit test plan, including new tests, is in the [October 26 revision](#unit-test-plan).)

| Unit test | Covers |
|---|---|
| `uniquename/uniquename_test.go` | Name format, a different UID giving a different name, dots, truncation boundaries, uniqueness |
| `TestCheckForInconsistentResourceSnapshots` | Every owner variant (stale UID, orphan, other name, extra owner, wrong kind, namespaced), scan order, returned leftovers, enqueued requests, orphans already marked for deletion (still leftovers, not re-enqueued) |
| `TestIsSnapshotInUseByAnyPlacementBinding` | Orphan bypass, other-name and wrong-kind owners, cross-namespace bindings, list failure |
| `TestGarbageCollect` | Owned and orphaned groups, in use, re-created primary, primary not found, Get failure, skipped snapshots at the same index (another primary, a different or missing policy owner) |
| `TestStart` | The GC worker loop: retry with backoff, drop on Unexpected errors, stop on context cancellation (added with this revision; see below) |
| `TestSnapshotResource` | Service and Job sanitization in `snapshotResource`: assigned cluster IPs and node ports, a headless Service, a Job with and without `manualSelector`, and that the input is not modified (added October 27) |
| `TestRetrieveResourceByName` | Name-based retrieval: a cache hit; the API server fallback on a cache miss (both scopes); a name missing from both (a User error wrapping NotFound); non-NotFound errors from either side; an unsynced informer (added October 27) |

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

- **Placement:** LO-1 to LO-5 go in a new `Describe("corner cases (snapshots of previous placement
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
4. With a paused manager, the call fails as Transient ("orphaned resource snapshots are detected"). The drained queue
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
   Transient ("cleaned up dangling secondary ..."); the next call creates index 0. The index-1 leftover is still there.
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

## Revision: latest-index checks, secondary GC, and unit tests

Added October 26. It revises the plan for the working-tree changes to `ops.go` and `gc.go` (not yet committed), and
adds a unit test plan for the package; earlier revisions left most of the package to integration tests.

### What changed in the code

- `retrieveLatestSnapshot` (`ops.go`) now ends in a new `allSnapshotsFor`, which always lists every snapshot at the
  latest primary's index. The fast path that returned the primary alone when its count label was `1` is gone. On
  that listing, in order:
  1. Sort by sub-index; a malformed sub-index is Unexpected (unchanged).
  2. **`checkForInconsistentResourceSnapshots` now also runs on the listing**, so the owner checks cover secondaries
     at the latest index: an ownerless one is orphaned (Transient "orphaned resource snapshots are detected", and
     enqueued for GC unless it is pending deletion); one owned by a stale policy UID is Transient; one with
     unexpected owners is a User error.
  3. **New:** the given primary must be the first listed snapshot (same name and UID); otherwise Transient
     ("…is not found as the first snapshot listed at its index; the client cache might have changed between reads").
  4. The count label is parsed; the messages now read "count label **value** to integer" and "count label **has a
     value** less than 1" (Unexpected).
  5. Fewer snapshots than the count: Unexpected (unchanged).
  6. **Extras beyond the count are now enqueued for GC** (`enqueueLeftoverResourceSnapshotsForGC`; extras pending
     deletion are skipped), instead of only being ignored. The call still succeeds and logs a warning.
  7. **New:** the returned snapshots must have contiguous sub-indices `0` to `count-1`; otherwise Unexpected
     ("…the sub-indices are not contiguous"). The contents-hash check follows (unchanged).
  - Ordering: history GC (`enqueueOldResourceSnapshotsForGC`) runs **before** `allSnapshotsFor`. The two never
    overlap: history GC only takes primaries at older indices (the latest is excluded), while `allSnapshotsFor` only
    enqueues snapshots at the latest index, in practice always secondaries. So a block at the latest index leaves
    history GC alone, whereas leftover primaries block it, as they would skew the history itself (Q10, resolved).
- `garbageCollect` (`gc.go`) accepts requests for **secondary** snapshots. After the shared Get, UID, and
  pending-deletion checks, a target whose sub-index is not `0` is deleted right away, with a UID precondition and no
  in-use check. Requests for primaries behave as before. Log messages on the shared path no longer say "primary".
- `cleanUpDanglingSecondarySnapshots` deletes with a UID precondition, as GC already did, and (since 16:31) ignores
  NotFound, like GC (Q11, resolved).
- Refactor, no behavior change: `enqueueResourceSnapshotForGC` is the single place that builds GC requests;
  `checkForInconsistentResourceSnapshots`, `enqueueOldResourceSnapshotsForGC`, and the new
  `enqueueLeftoverResourceSnapshotsForGC` call it.
- Docs: the rationale at the top of `garbageCollect` was restored and extended to secondaries, and
  `checkForInconsistentResourceSnapshots` documents the narrower known gap (ownerless secondaries at indices other
  than the latest).

### Existing tests: adapted October 26 (done)

| Spec | Change |
|---|---|
| SC-6 | The extras are owned decoys (`createOwnedDecoySnapshot`); both "extra" specs also assert that the extra is deleted. The count-1 spec was renamed (it was "should ignore extra secondary snapshots when the count label is 1") |
| SC-8 | The two count-label messages match the new wording |
| GC-2 | The extra secondary at index 1 is added after index 2 exists, so it survives until index 1 is collected; the index-0 expectation no longer lists it |
| SC-7, LO-5 | Comments corrected: the manager can delete a single secondary, but never one within the count; leftover secondaries are detected at the latest index only |

Validation: `ginkgo -p` (without `--race`) passed 347 of 347 specs, none skipped, in about 38s. The `gc_test.go` unit
tests pass.

### Cross-reference: plan items vs. the change

| Plan item (spec) | Affected? | Action |
|---|---|---|
| P1-1 to P1-3, P1-9 (SC-1 to SC-3, SC-9) | No | None |
| P1-4, P1-5 (SC-4, SC-5): dangling-secondary cleanup | Only the UID precondition, which envtest can't observe deterministically | U3, U8 |
| P1-6 (SC-6): extras | Yes | Adapted; extend (SC-6 below) |
| P1-7 (SC-7): fewer than the count | No; the primary-first check passes before the count check | None |
| P1-8 (SC-8): malformed labels | Messages reworded; new contiguity check | Adapted; extend (SC-8 below) |
| GC-1, GC-3 to GC-6 | No; GC-3's queue assertions only involve primaries | None; the SC-6 extension covers requests for secondaries |
| GC-2: multi-group GC | Yes | Adapted |
| LO-1 to LO-4 | No; they are blocked at the primary level, before `allSnapshotsFor` | None; the owner variants on secondaries go to U1 |
| LO-5, and the known gap | The gap is narrower; LO-5's leftovers are never at the latest index (none exists yet, or they sit at the next index), so it still holds | Comment adapted; add LO-6 |
| `TestCheckForInconsistentResourceSnapshots` | No; the function never reads sub-indices, and its fixtures carry none | None; secondaries are covered through U1 |
| `TestGarbageCollect` | Every case targets a primary | U2, U3 |
| `TestIsSnapshotInUseByAnyPlacementBinding`, `TestStart` | No | None |
| The primary missing from the latest-index listing | Not reproducible in envtest (the cache must change between two reads) | U1 |

### Integration cases to add

(Implemented at 16:40; see Status.)

Wherever a call should fail, check both entry points (as before).

**SC-6 (extend). Extras beyond the count label** (PP; in the existing context, before the suite manager's call
collects the extra)

1. With a paused manager, the call succeeds with the `count` snapshots, and the drained queue holds exactly one
   request: for the extra, with its namespaced name and UID and the policy's namespaced name. No request targets the
   primary or the sub-index-1 secondary.
2. A second extra with a finalizer, marked for deletion: the call still succeeds, since an owned extra does not block
   (unlike an ownerless leftover), and no request is enqueued for it. Remove the finalizer; it is gone.
   `createOwnedDecoySnapshot` takes no finalizers, so this needs a variant or a follow-up update.

**SC-8 (extend). Non-contiguous sub-indices**

- A primary with a count of `2`, plus a secondary at sub-index `2` and none at `1`: Unexpected, "the sub-indices are
  not contiguous". Append it to the end of the table, so that `staleMalformedIndexTestCaseIdx` stays valid.

**LO-6. An orphaned secondary at the latest index** (PP; a new context in `Describe("corner cases (snapshots of
previous placement policies)")`, after LO-5)

The suite manager's GC worker would collect the leftover right away and race the assertions, so steps 3 to 5 use a
paused manager.

1. Create the policy and a two-group snapshot at index 0.
2. Create an ownerless indexed secondary at index 0, sub-index 1, named with an old UID. It duplicates a sub-index,
   mimicking a previous policy's secondary that slipped past the dangling-secondary cleanup under cache lag.
3. Both entry points fail as Transient ("orphaned resource snapshots are detected"), and the error names the
   leftover. The drained queue holds exactly one request, for the leftover.
4. Change the selected resources: the calls still fail the same way, and nothing is created at index 1
   (`verifyNoPrimarySnapshotAtIdx(1)`).
5. Process the request with the paused manager's `garbageCollect`: only the leftover is deleted, and the index-0
   primary and its owned secondary keep their UIDs.
6. `SnapshotResourcesIfNoSnapshotExists` returns the original index-0 set (stale), and `SnapshotResourcesIfStale`
   creates index 1.

That history GC still runs while the latest index is blocked (Q10) is covered in U5, not here.

### Unit test plan

(Implemented at 16:59, U1 to U15; see Status.)

Unit tests cover branch-level behavior and what envtest can't observe: delete options, call order, divergence between
the cache and the API server, and a primary missing from a listing. Integration tests keep the end-to-end flows.
Conventions as in `CLAUDE.md`: table-driven, `cmp.Diff` and no assert libraries, `want` and `got`, and messages of
the form `"Func(...) = %v, want %v"`.

**Techniques and helpers**

- **Field indexes.** `newFakeHubClientWithIndexes` (`gc_test.go`) registers only the "owned by and indexed" snapshot
  index. Tests that go through `retrieveLatestSnapshot` also need the "owned by and sub-indexed" index; extend the
  helper or add a variant.
- **Cache lag.** `hubClient` and `hubUncachedReader` are separate fields, so a test can back them with two fake
  clients holding different objects; for example, a primary that only the "API server" store has, for the quorum-read
  branches.
- **Delete options.** The fake client enforces `ResourceVersion` preconditions but not `UID` ones. An interceptor
  records each Delete's `client.DeleteOptions`, and the test compares their `Preconditions.UID`s.
- **Create order.** An interceptor records the names of Create calls, in order.
- **GC requests.** Use `newTestGCQueue`; drain the queue and compare with `cmp.Diff`, as
  `TestCheckForInconsistentResourceSnapshots` does.
- **Fixtures.** A snapshot builder with the index, sub-index, count label (primaries only), contents hash, owner
  references, and an optional deletion timestamp, for both scopes. `TestGarbageCollect`'s local `snapshot` builder can
  be promoted. The fake client rejects an object with a deletion timestamp but no finalizers, so pending-deletion
  fixtures need a finalizer.
- **Layout.** New tests go in `ops_test.go`, `snapshots_test.go`, `resources_test.go`, and `manager_test.go`, matching
  the source files; the GC additions stay in `gc_test.go`. New shared unit helpers go in a new `utils_test.go`; the
  existing ones stay in `gc_test.go`, as decided at 06:52.

**UT-P0: the October 26 changes**

U1. `TestAllSnapshotsFor` (`ops_test.go`, new). Cluster-scoped unless noted:

| Case | Want |
|---|---|
| Primary only, count 1 | `[primary]`; no requests |
| Primary and 2 secondaries, listed out of order, count 3 | Sorted by sub-index, primary first |
| Same, for a namespaced policy | Same; covers the namespaced list branch |
| Extras at sub-indices 2 and 3, count 2 | The first 2; one request per extra, carrying the policy's namespaced name; none for the primary or sub-index 1 |
| An extra pending deletion | Success; no request for it |
| An ownerless secondary (old-UID name, duplicate sub-index 1) | Transient, naming the leftover; one request, for it |
| An ownerless secondary pending deletion | Transient; no request |
| A secondary owned by a stale policy UID | Transient (stale owner); no requests |
| A secondary with an extra owner | User |
| The given primary is not in the store | Transient ("not found as the first snapshot listed"); no requests |
| The stored primary has a different UID from the given one | Transient |
| A count label of `abc` or `0`, or none | Unexpected |
| Fewer snapshots than the count | Unexpected |
| Sub-indices `0` and `2`, count 2 | Unexpected ("not contiguous") |
| A secondary with a different contents hash | Unexpected |
| A malformed sub-index label | Unexpected (sort) |

U2. `TestGarbageCollect` (`gc_test.go`, extend). Give each case an optional GC request; the default stays the
primary's. New cases, each targeting a secondary:

| Case | Want |
|---|---|
| An owned extra (sub-index 2), next to an owned primary and sub-index 1 | Only the extra is deleted |
| An ownerless secondary next to an owned primary | Only it is deleted |
| An owned extra while a binding uses the primary | The extra is deleted; secondaries skip the in-use check |
| The secondary was re-created (UID mismatch) | Kept; no error |
| The secondary is pending deletion | Kept; no error and no Delete call |
| Delete fails (interceptor) | APIServer error; kept |
| Delete returns NotFound (interceptor) | No error |
| A namespaced secondary | Deleted; covers the namespaced Get branch |

A secondary that is not found takes the same path as the existing "primary snapshot not found" case; no new case.

U3. **Delete preconditions** (`gc_test.go`, a subtest of U2; and a U8 case). With the recording interceptor, every
Delete carries `Preconditions.UID` equal to the object's UID, on all three paths: a secondary target; the primary
path's secondaries and primary; and `cleanUpDanglingSecondarySnapshots`.

U4. `TestEnqueueResourceSnapshotForGC` (`gc_test.go`, new; one table that also covers
`enqueueLeftoverResourceSnapshotsForGC`):

- A cluster-scoped and a namespaced snapshot: the request carries the snapshot's namespaced name and UID, and the
  policy's namespaced name.
- A snapshot pending deletion: no request.
- `enqueueLeftoverResourceSnapshotsForGC` with three snapshots, one of them pending deletion: two requests.
  Enqueueing the same snapshot twice yields one request (the workqueue deduplicates).

**UT-P1: functions near the change that have no unit tests**

U5. `TestRetrieveLatestSnapshot` (`ops_test.go`, new). Integration covers most branches; this covers the ordering.

- No primaries: `nil` and no error.
- Primaries out of order: the highest index wins.
- A leftover primary: Transient, and **no** history requests, only the leftover's own (blocked before history GC).
- The latest primary pending deletion: Transient, and history requests **are** enqueued (expected; Q10).
- An ownerless secondary at the latest index: Transient; history requests are enqueued, plus one for the leftover
  (expected; Q10).
- A history limit of 1 with three indices: requests for the two older primaries only.

U6. `TestEnqueueOldResourceSnapshotsForGC` (`gc_test.go`, new; pure):

- No limit set means the default of 3; a limit of `0` or less means 1.
- History within the limit: no requests. A limit of 1 with four primaries: the three oldest.
- Primaries pending deletion are skipped.

U7. `TestIsSnapshotUpToDate` (`ops_test.go`, new; two stores):

- Hash mismatch: false, with no quorum read (an interceptor on the uncached reader sees no Get).
- Hash match, nothing at the next index: true.
- Hash match, and the next primary exists only in the uncached store: Transient ("a newer snapshot already exists").
- A malformed index label: Unexpected. An uncached Get failure: APIServer.

U8. `TestCleanUpDanglingSecondarySnapshots` (`ops_test.go`, new; two stores):

- Nothing at the index: false.
- A primary at the index in the cache: Transient; nothing is deleted.
- A primary only in the uncached store: Transient; nothing is deleted.
- Owned, ownerless, and stale-owner secondaries alike (there is no owner check): all deleted, with UID preconditions;
  true.
- Only secondaries pending deletion: nothing is deleted, and it still returns true, so the caller requeues.
- Delete fails: APIServer. Delete returns NotFound: no error, and it still returns true, so the caller requeues
  (Q11).

U9. `TestCreateResourceSnapshotAnyway` (`ops_test.go`, new; a Create-recording interceptor and a small
`maxPerSnapshotResourceCnt`):

- No latest primary: index 0. A latest primary at index 4: index 5. A malformed latest index: Unexpected.
- Three groups: Creates run in the order sub-index 2, sub-index 1, then the primary. The primary has a count of 3,
  names come from `uniquename`, and every snapshot has a controller owner reference to the policy.
- Dangling secondaries at the next index are deleted, the call is Transient ("cleaned up dangling secondary …"), and
  nothing is created.
- A secondary Create fails (AlreadyExists): APIServer, and the primary is not created. The primary Create fails:
  APIServer.

U10. `TestIsSnapshotSynchronized` (`ops_test.go`, new; the checks that run before any resource is read):

- A nil snapshot or policy: Unexpected. A non-primary snapshot: Unexpected.
- Not owned by the policy, or in another namespace: Unexpected.
- Owned by a stale policy UID: Transient.
- No contents hash: Unexpected.
- The hash comparison itself reads the selected resources (REST mapper, dynamic client), so it stays with integration
  (optional, as before).

U11. `TestPlacementPolicyOwnerRef` and `TestIsSamePlacementPolicyOwner` (`gc_test.go`, new; pure):

- Owner selection: a malformed API version is skipped; the kind must match the scope (`ClusterPlacementPolicy` for
  cluster-scoped objects); the name must match; the matching reference is returned from among several; no match
  returns nil.
- Same owner: both nil is true, one nil is false, the same UID is true, and different UIDs are false.

**UT-P2: earlier gaps elsewhere in the package (optional)**

U12. `TestSnapshotResource` (`resources_test.go`): strips the last-applied and deployment-revision annotations
(dropping `metadata.annotations` when nothing else is left), owner references, managed fields, read-only metadata,
finalizers, and status; keeps other annotations, labels, and the spec; and sets the identifier from the GVK. Assert on
raw JSON, as P0-E does.

U13. `TestSplitResourcesIntoSizeControlledGroups` (`resources_test.go`): empty input gives one empty group; splits by
count and by size (`AdditionalInfo` counts toward the size); a resource exactly at the size limit fits; one over the
limit is a User error.

U14. `TestPrimaryPlacementResourceSnapshot`, `TestSecondaryPlacementResourceSnapshot`, and
`TestPlacementPolicyOwnerLabelVal` (`snapshots_test.go`): labels (owned-by value, index, sub-index, and the count on
primaries only), annotations (contents hash, full owned-by name), the controller owner reference, and the type per
scope; the owned-by label is truncated and hashed at its length limit.

U15. `TestNew` (`manager_test.go`): each argument check fails with the right category (mutex slot count, size and
count bounds, a nil eligibility checker). The checks run before the controller manager is used, so a nil manager
works for the failure cases.

### Open questions (October 26)

10. **History GC while blocked at the latest index.** `retrieveLatestSnapshot` enqueues history GC before
    `allSnapshotsFor`, so leftovers at the latest index (and, as before, a latest primary pending deletion) do not
    stop history GC, whereas leftover primaries do. Should `enqueueOldResourceSnapshotsForGC` move after
    `allSnapshotsFor`? **Resolved at 16:30**: no; the order does not matter, as the two never overlap.
    - History GC only takes primaries (from the primary scan) at older indices; the latest is excluded, and
      collecting a primary touches only its own index.
    - `allSnapshotsFor` only enqueues snapshots at the latest index, and in practice only secondaries: extras always
      are (the count is at least 1, and the primary is verified to come first), and an ownerless primary there would
      already have been caught by the primary scan, barring a cache change between the two reads.
    - Leftover primaries block history GC for a different reason: they would skew the history itself.
    - U5 covers the behavior as expected, not as a pin.
11. **NotFound in `cleanUpDanglingSecondarySnapshots`.** Its Delete fails with an APIServer error on NotFound (the
    caller requeues), while every GC delete ignores NotFound. Pin it (U8), or align it with GC? **Resolved at
    16:31**: aligned with GC; the Delete now ignores NotFound (`!apierrors.IsNotFound(err)`). The method still
    returns true, so the caller still requeues. A re-created snapshot (new UID) still fails the UID precondition with
    a conflict, not NotFound. U8 expects no error.

## Out of Scope

These are unit-test territory or unreachable:

- `New()` argument validation. (Revised October 26: now planned as unit test U15.)
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

Q5 to Q9 came up during the P1 and P2 planning; their questions and answers are in the Status log. Q10 and Q11 came
up during the October 26 revision and are resolved; see [Open questions (October 26)](#open-questions-october-26).

## Validation

- `KUBEBUILDER_ASSETS=$(hack/tools/bin/setup-envtest-release-0.22 use 1.33.0 -p path) go test ./pkg/v1/managers/placementresourcesnapshot/... -coverprofile=...`
- Targets: `resources.go` at 90% or more for every function except unreachable branches; package at 85% or more.
  Dropped at 06:41: per the user, coverage is no longer measured.
- Suite runtime increase of about 60s or less (baseline is about 58s).
- `make reviewable`.
- Final runs use `ginkgo -p --race` instead of `go test`.
- Unit tests (added October 26) need no envtest when filtered by name, for example
  `go test -race -count=1 -run '^Test(AllSnapshotsFor|GarbageCollect|EnqueueResourceSnapshotForGC)$' ./pkg/v1/managers/placementresourcesnapshot/`.
  Without `-run`, `go test` also starts the envtest suite (`TestAPIs`), which needs `KUBEBUILDER_ASSETS`. Run the
  queue-based tests with `-count=30` as well, as was done for `TestStart`.
- From October 27, per the user: integration runs are always focused (`ginkgo -p --race --focus=...`) on the specs
  being changed. The whole suite is not run.

## Status

The detailed P0, P1, and P2 plans mentioned below were working documents and were not committed. Their spec IDs map
to test contexts by position, in file order: PP-n and CPP-n are the n-th `Context` of the two `Describe`s in
`resources_integration_test.go`; SC-n and NM-n are those of the two `Describe`s in `corner_cases_integration_test.go`
(SC-1 runs once per scope, as the first two contexts); and GC-n are those of `Describe("GC variants")` in
`ops_gc_integration_test.go`. Contexts added after the IDs were assigned are skipped when counting: in the PP
`Describe`, "resources with non-controller owner references" (not in the plan) and "named resource not found
(informer-backed)" (October 27, referred to as PP-2b). PP-13 and PP-14 ("manifest sanitization (services)" and
"(jobs)", October 27) are the exception: they were numbered by their positions in the file at the time, which counted
the non-controller owner references context; they are the last two contexts of the PP `Describe`.

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
        - A resource selected by name that does not exist is a User error (added October 27; a cache miss is
          confirmed with a read from the API server first). Auto-rollout then waits for its next rollout window
          instead of retrying, and a staged update run fails its initialization.
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
- October 26, 16:20: The plan was revised for latest-index checks and secondary snapshot GC (working-tree changes to
  `ops.go` and `gc.go`), and a unit test plan was added; see
  [Revision: latest-index checks, secondary GC, and unit tests](#revision-latest-index-checks-secondary-gc-and-unit-tests).
  - Before the revision, the three specs that the change broke (SC-6, SC-8, GC-2) were adapted, and two outdated test
    comments (SC-7, LO-5) were corrected. `ginkgo -p` passed 347 of 347 specs.
  - To add:
    - Integration: extend SC-6 and SC-8, and add LO-6.
    - Unit, by priority: U1 to U4 for the change itself; U5 to U11 for nearby functions without unit tests; U12 to
      U15 (optional) for earlier gaps elsewhere in the package.
  - Open: Q10 (recommendation: pin) and Q11 (recommendation: align with GC).
  - More entries for the PR description:
    - Ownerless or stale-owner secondaries at the latest index now block snapshot creation (Transient), and
      ownerless ones are garbage-collected; unexpected owners on them are a User error.
    - Extra secondaries beyond the count label are garbage-collected.
    - New errors: the primary missing from the latest-index listing (Transient), and non-contiguous sub-indices
      (Unexpected).
    - The count-label error messages were reworded.
    - `garbageCollect` deletes secondary targets directly, and orphan cleanup deletes with UID preconditions.
  - Implementation is waiting for approval.
- October 26, 16:30: The user resolved Q10: the order of history GC and the latest-index checks does not matter.
  History GC only takes primaries at older indices, while `allSnapshotsFor` only enqueues snapshots at the latest
  index (in practice, secondaries), so the two never overlap. U5 now covers the behavior as expected rather than
  pinning it. Q11 is still open.
- October 26, 16:31: The user resolved Q11 by aligning orphan cleanup with GC.
  - Code: the Delete in `cleanUpOrphanedSecondarySnapshots` (`ops.go`) now ignores NotFound
    (`!apierrors.IsNotFound(err)`; `ops.go` gained the `apierrors` import that `gc.go` already uses). The method
    still returns true, so the caller still requeues.
  - Plan: U8 now expects no error on NotFound.
  - More for the PR description: orphan cleanup ignores NotFound on delete.
  - Validation: `gofmt` and `go vet` are clean, the unit tests pass, and `ginkgo -p` passed 347 of 347 specs.
  - No open questions remain; implementation is waiting for approval.
- October 26, 16:40: The integration cases of the October 26 revision were implemented (the 11 new specs are all in
  `corner_cases_integration_test.go`). The unit tests (U1 to U15) are not implemented yet.
  - **SC-6** (extended; 4 specs in the existing context): with a paused manager, the queue holds only the extra's
    request; a second extra (sub-index 3) pending deletion neither blocks the calls nor is enqueued; once it is gone,
    the calls return the same set. The paused manager is never called at the same time as the manager under test,
    which still collects the first extra.
  - **SC-8** (extended; 1 table entry, appended): non-contiguous sub-indices (a primary with a count of 2 and a
    secondary at sub-index 2) return Unexpected. New constant: `nonContiguousSubIdxsErrMsg`.
  - **LO-6** ("orphaned secondary snapshot at the latest index"; 6 specs, at the end of the leftover `Describe`): uses
    a paused manager throughout. Both entry points fail as Transient; the error's `leftoverPlacementResourceSnapshots`
    attribute names the leftover (read with `errAttrValue`); each call enqueues exactly one request, for the leftover.
    The block holds after a resource change. The paused manager's `garbageCollect` deletes only the leftover, and the
    owned snapshots keep their UIDs. The calls then return the original set (stale) and create index 1.
  - Helper change: `gcRequestFor` (`utils_integration_test.go`) is documented for any snapshot, primary or secondary,
    and its parameter is renamed to `snapshot`. The test file now imports `k8s.io/klog/v2`.
  - Validation: `go vet` is clean; the focused run passed 24 of 24 specs; `ginkgo -p --race` passed 358 of 358 specs
    (about 43s), with no data races; `golangci-lint --fast-only` reported 0 issues.
- October 26, 16:59: The unit test plan (U1 to U15, including the optional U12 to U15) was implemented. There are 17
  new test functions and 8 new `TestGarbageCollect` cases; with the 4 existing functions, the package has 21 unit test
  functions and 149 subtests.
  - Files:
    - `ops_test.go` (new): U1 `TestAllSnapshotsFor` (18 cases), U5 `TestRetrieveLatestSnapshot`, U7
      `TestIsSnapshotUpToDate`, U8 `TestCleanUpOrphanedSecondarySnapshots`, U9 `TestCreateResourceSnapshotAnyway`, and
      U10 `TestIsSnapshotSynchronized`.
    - `gc_test.go`: U2 (8 `TestGarbageCollect` cases; each case can now name its GC request, and the remaining
      snapshots are listed across both scopes), U3 `TestGarbageCollectDeletePreconditions` (the cleanup path is a U8
      case), U4 `TestEnqueueResourceSnapshotForGC`, U6 `TestEnqueueOldResourceSnapshotsForGC`, and U11
      `TestPlacementPolicyOwnerRef` and `TestIsSamePlacementPolicyOwner`.
    - `resources_test.go` (new): U12 `TestSnapshotResource` (it also checks that the given resource is not
      modified) and U13 `TestSplitResourcesIntoSizeControlledGroups`.
    - `snapshots_test.go` (new): U14 `TestPrimaryPlacementResourceSnapshot` and `TestSecondaryPlacementResourceSnapshot`
      (each also covers a `SetControllerReference` failure: a namespace-scoped owner of a cluster-scoped snapshot), and
      `TestPlacementPolicyOwnerLabelVal`.
    - `manager_test.go` (new): U15 `TestNew`.
    - `utils_test.go` (new): shared unit helpers: the `snapshotFixture` builder (`newSnapshotFixture` and `with*`
      modifiers), `newTestPlacementPolicy`, `testPolicyOwnerRef`, the Delete recorder (`recordDeletes`, `deleteCall`),
      `listSnapshotNames`, and `verifyErrCategoryAndMsgSubstr`.
  - Helper changes: `newFakeHubClientWithIndexes` (`gc_test.go`) also registers the "owned by and sub-indexed"
    snapshot index. The unit tests reuse the Ginkgo-free integration helpers `drainGCRequests`, `gcRequestFor`, and
    `snapshotNamesOf`, and the `testFinalizer` constant.
  - Small additions beyond the plan: namespace-scoped cases in U5 and U9; a malformed index label case and a
    multi-group case in U5; a "latest snapshot only" case in U6.
  - Mutation checks (reverted afterwards; the sources were restored byte-for-byte), each caught by the intended cases:
    orphan cleanup failing on NotFound (U8); no primary-first check (U1, two cases); no contiguity check (U1); the
    secondary GC path disabled (U2, two cases; U3).
  - Validation: `go vet` and `gofmt` are clean; all unit tests pass with `-race`; the queue-based tests pass with
    `-race -count=30`; `golangci-lint --fast-only` reported 0 issues; `ginkgo -p --race` passed 358 of 358 specs.
  - Not built: the optional integration backlog (F.1 to F.4, and `IsSnapshotSynchronized`'s hash comparison).
- October 26, 17:08: Lint fixes in the new unit tests: `fmt.Appendf` in `ops_test.go`, and preallocated slices in
  `ops_test.go` (`summarize`) and `utils_test.go` (`listSnapshotNames`).
- October 26, 17:40: Renamed `cleanUpOrphanedSecondarySnapshots` to `cleanUpDanglingSecondarySnapshots`, at the user's
  suggestion. "Orphaned" now means only "no owner reference" (as with Kubernetes' orphan propagation), and "dangling"
  means "a secondary snapshot at an index with no primary snapshot".
  - Code (`ops.go`, `gc.go`): the function, its doc comment (which now defines "dangling" and the return value), the
    related comments, and the messages, e.g., "cleaned up dangling secondary placement resource snapshots; requeue
    before creating new snapshots".
  - Tests: U8 is now `TestCleanUpDanglingSecondarySnapshots`. The U9 case, the SC-3 to SC-5 contexts and specs, the
    LO-5 and LO-6 comments, and the `verifyDanglingSecondariesCleanedUp` helpers in `ops_gc_integration_test.go` use
    the new term. The error-message constant is now `danglingSecondariesCleanedUpErrMsg`. Uses of "orphaned" that mean
    "no owner" (LO-2, LO-3, LO-6, and the GC unit tests) are unchanged.
  - This plan: the sections above the status log use the new names; earlier status entries keep the old ones.
  - The user changed `checkForInconsistentResourceSnapshots` (`gc.go`) to name its result `orphaned` and to return nil
    with an error. The `TestCheckForInconsistentResourceSnapshots` case "orphaned snapshot followed by a snapshot owned
    by a deleted placement policy with the same name" now wants no snapshots returned; it still wants the GC request.
  - Validation: `go build`, `go vet`, and `gofmt` are clean; `golangci-lint --fast-only` reported 0 issues; all unit
    tests pass with `-race`; `ginkgo -p --race` passed 358 of 358 specs.
- October 26, 17:47: Consistency fixes from the latest review round (items 4 to 7).
  - Error/log keys: "observedIdx" (`garbageCollect`) is now "observedIndex", "snapshotIdx"
    (`cleanUpDanglingSecondarySnapshots`) is now "snapshotIndex", and "actualCount" (`allSnapshotsFor`) is now
    "observedCount". The workgenerator's `retrieval.go` still uses "actualCount" for its own count check.
  - Messages: "found via quorum reads" (`isSnapshotUpToDate`) is now "found via quorum read", and
    `IsSnapshotSynchronized` now says "of the same name", like the GC check. The tests follow:
    `quorumReadNewerSnapshotErrMsg` and the U10 case.
  - The `klog.Warningf` in `allSnapshotsFor` is now a structured `klog.InfoS` (same visibility), with the extra
    snapshots listed (`extraPlacementResourceSnapshots`).
  - The index-parse errors in `isSnapshotUpToDate` and `createResourceSnapshotAnyway` now carry the primary snapshot
    and the observed index.
  - Validation: `go build`, `go vet`, and `gofmt` are clean; `golangci-lint --fast-only` reported 0 issues;
    `ginkgo -p --race` passed 358 of 358 specs.
- October 26, 17:58: Settled the snapshot terminology (item 2 of the latest review round): "orphaned" means "no owner
  reference", "leftover" means "an extra secondary snapshot beyond the count label", and "dangling" means "a secondary
  snapshot at an index with no primary snapshot".
  - Code: the user renamed the result of `checkForInconsistentResourceSnapshots` to `orphaned` (17:31). Its callers
    in `ops.go` follow: the variable is `orphaned`, the message is "orphaned resource snapshots are detected; waiting
    for garbage collection to complete", and the error key is `orphanedPlacementResourceSnapshots`. The docs of
    `checkForInconsistentResourceSnapshots` (which also notes that no snapshots are returned with an error),
    `allSnapshotsFor`, and `garbageCollect` use the new terms. `enqueueLeftoverResourceSnapshotsForGC` keeps its name.
  - Tests: `leftoverSnapshotsErrMsg` is now `orphanedSnapshotsErrMsg`. The `Describe` for LO-1 to LO-6 is now
    "corner cases (snapshots of previous placement policies)" (it covers snapshots still owned by a deleted policy
    too). LO-5 ("orphaned secondary snapshots without a primary snapshot") and LO-6 use "orphaned" in their names,
    variables (`orphanedSnapshotName*`, `verifyBlockedByOrphan`), and comments. U1/U5 messages and the U5 case
    "orphaned primary snapshot", and the `TestCheckForInconsistentResourceSnapshots` fields (`wantOrphaned`,
    `orphanedRef`) follow. "Leftover" in `TestEnqueueResourceSnapshotForGC` still means extras.
  - This plan: the sections above the status log use the new message and `Describe` names; the LO-* labels stay.
  - Validation: `go build`, `go vet`, and `gofmt` are clean; `golangci-lint --fast-only` reported 0 issues;
    `ginkgo -p --race` passed 358 of 358 specs.
- October 26, 18:05: Typo and wording fixes from the latest review round, plus one trivia item.
  - `ops.go`: the `snapshotResources` doc list now reads "a) …; b) …; or c) …"; "yet … yet" in `isSnapshotUpToDate`
    is gone; "a few secondary … snapshots" and "(their contents might be outdated, and their specs are immutable
    anyway)" in `createResourceSnapshotAnyway`; "match the current state" in `IsSnapshotSynchronized`.
  - `manager.go`: "to shut down the GC workqueue"; the `New` error message says "mu slot count" (no test matched it).
  - Tests: "clean-up(s)" is now "cleanup(s)" in the integration tests (14 places, including the SC-4 context name),
    matching the production code.
  - `snapshots.go`: the local imports now sit in their own group, as in the other files.
  - Left as they are, per the user: item 1 (history GC runs before the pending-deletion check; only reachable by
    tampering), the `TestGarbageCollect` case names, the `retrieveLatestSnapshot` result names, and `actualCount` in
    the workgenerator's `retrieval.go`.
  - Validation: `go build`, `go vet`, and `gofmt` are clean; `golangci-lint --fast-only` reported 0 issues;
    `ginkgo -p --race` passed 358 of 358 specs.
- October 27, 02:13: `snapshotResource` now strips Service and Job fields filled in by the hub API server, matching
  `generateRawContent` in `pkg/utils/controller/resource_selector_resolver.go`:
  - Services: `clusterIP` and `clusterIPs` (unless headless), `healthCheckNodePort`, and each port's `nodePort`.
  - Jobs without `manualSelector`: the `controller-uid` and `batch.kubernetes.io/controller-uid` labels in the
    selector and the pod template, and the pod template's `creationTimestamp`.
  - The new unit test `TestSnapshotResource` (in the new file `resources_test.go`) passes with `-race`.
  - The plan adds PP-13 (Services) and PP-14 (Jobs) to section E; implementation follows.
  - Another entry for the PR description: Service and Job manifests in snapshots no longer carry these fields.
- October 27, 02:20: PP-13 and PP-14 were implemented at the end of the first `Describe` in
  `resources_integration_test.go`. There are two ordered contexts with 4 specs each.
  - New constants: `serviceNameTemplate`, `jobNameTemplate`, and `legacyJobControllerUIDLabel`.
  - New helpers in `utils_integration_test.go`, next to `newDeployment`: `newLoadBalancerService`,
    `verifyServiceFieldsFilledIn`, `serviceSpecWithoutFilledInFields`, `newJob`, `verifyJobControllerUIDLabelsAdded`,
    and `jobSpecWithoutControllerUIDLabels`.
  - `deleteObjectsAndWait` takes optional `client.DeleteOption`s; existing callers are unchanged. The Job context
    deletes with background propagation.
  - Envtest facts confirmed by the preconditions: the API server (1.33) fills in all of the Service fields, and it
    adds both the current and the legacy controller UID labels to the Job's pod template.
  - Validation:
    - `ginkgo -p --race --focus='manifest sanitization \((services|jobs)\)'` passed 8 of 355 specs (347 skipped).
      Per the user, the whole suite was not run.
    - `gofmt` and `go vet` are clean. `golangci-lint --fast-only` reported 0 issues. In the new code, the full run
      flags only `goconst` findings, the same style as the rest of the files.
- October 27, 03:19: A cache miss in name-based selection is now confirmed with a read from the API server
  (`resources.go`).
  - `retrieveResourceByName`: when the informer cache returns NotFound, the resource is read again through the
    dynamic client by `retrieveResourceByNameWithNoCache` (renamed from `getSelectedResourceFromAPIServer` at 03:28),
    which the no-informer path now shares. A NotFound from that read is a User error ("selected resource does not
    exist") that still wraps the NotFound; any other error stays an APIServer error. Other cache errors are unchanged.
  - The cache and the direct read produce the same snapshot: the informer transform only strips `managedFields`,
    which `snapshotResource` strips anyway.
  - Test updates:
    - A12 (PP-2, and the not-found case in CPP-1's table) now expects a User error with the new message.
    - New context PP-2b "named resource not found (informer-backed)": a nonexistent Deployment (read through the
      cache) is a User error; once the Deployment is created, a snapshot is taken right away, without waiting for the
      cache.
    - New unit test `TestRetrieveResourceByName` (9 cases, in `resources_test.go` next to `TestSnapshotResource`),
      with `test/utils/informer.FakeManager` and a fake dynamic client; see the unit test table.
    - `placementpolicy/controller_test.go`: the "selected resource gone" fixture uses the new error shape.
  - Callers: the placement policy controller already treated NotFound as a selection error (no change); auto-rollout
    now reports `FailedToSnapshotResourcesDuringAutoRollout` and waits for the next rollout window instead of
    retrying; a staged update run fails its initialization (B11 in the staged update run plan).
  - Validation: `ginkgo -p --race ./pkg/v1/managers/placementresourcesnapshot`: 350 of 350 specs pass; the focused
    unit tests pass with `-race`; `go vet` and `golangci-lint --fast-only` report 0 issues.
  - 03:35: breadcrumb fixes: A12, section F, the unit test table, the PR description list, and the note on contexts
    skipped by the positional IDs (which also corrects PP-5 and PP-9, shifted by the non-controller owner references
    context).
  - 03:45: merged `main` (#1018, the Service and Job sanitization in the 02:13 and 02:20 entries above).
    `resources_test.go` was added on both sides; it now holds both `TestRetrieveResourceByName` and
    `TestSnapshotResource`. Validation: both unit tests pass with `-race`; `ginkgo -p --race
    ./pkg/v1/managers/placementresourcesnapshot` passes 358 of 358 specs (347, plus 3 for PP-2b and 8 for PP-13 and
    PP-14).
- October 27, 04:10: Merged `main` (#1018 and #1019) into `feat/handle-incomplete-snapshots-in-work-generator`. Only
  this plan conflicted; both sides' additions were kept, in time order.
  - The October 26 entries (16:20 to 18:05) and the October 27 entries (02:13 to 03:45) were made on separate
    branches. The October 26 code is on `feat/clean-up-orphaned-secondary-snapshots`, which `main` does not have yet.
    Spec counts in each set are relative to its own branch.
  - Expected conflict: that branch's `resources_test.go` (U12 and U13) and the one on `main` both define
    `TestSnapshotResource`. Combine the two when that branch merges `main`.
