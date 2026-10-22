# Work Applier (v1) Integration Test Plan

**Date**: October 21, 2026 20:45 AEDT
**Task**: Plan (not yet implement) the integration tests for `pkg/v1/controllers/workapplier`, building on the
envtest suite in [suite_test.go](../../../pkg/v1/controllers/workapplier/suite_test.go) and the two existing IT files
([core_integration_test.go](../../../pkg/v1/controllers/workapplier/core_integration_test.go),
[failures_integration_test.go](../../../pkg/v1/controllers/workapplier/failures_integration_test.go)). The old work
applier ITs (`pkg/controllers/workapplier`) are used as a reference only; many do not apply as-is because of API and
behavior changes (see [Old tests → new plan](#old-tests--new-plan)).

**Status**: In progress. The [Decisions](#decisions) are adopted. All P0 contexts are implemented, plus W1–W3, R2, R3,
L2, L3 (P1) and L5 (P2); the remaining P1/P2 contexts (including the V section and W4) are pending. See
[Status](#status) for details.

## Requirements

1. Cover the behaviors of the new work applier that the existing ITs do not cover yet, against real API servers
   (envtest hub + member).
2. Do not port the old ITs as-is; adapt the scenarios that still apply to the `v1alpha1` API (`SyncStrategy`, linked
   work objects, `DiffDetails`), and drop the ones whose features are gone.
3. Follow repo conventions: Ginkgo/Gomega, `<area>_integration_test.go`, one `Context` per scenario (`Ordered`,
   `BeforeAll`/`AfterAll`), random names and a fresh namespace per context (envtest cannot delete namespaces),
   whole-struct `cmp.Diff`, `want` naming, existing `*Actual` helper style.
4. Always run with `ginkgo -p` (see [Running the tests](#running-the-tests)).
5. Put **all** IT helpers (fixtures, builders, `*Actual` checks, GC simulation, recorder accessors, shared expected
   messages/constants) in [utils_integration_test.go](../../../pkg/v1/controllers/workapplier/utils_integration_test.go)
   (renamed from `utils_test.go` on October 21, 2026). The `*_integration_test.go` scenario files hold only
   `Describe`/`Context` bodies.

## Baseline (October 21, 2026)

```bash
KUBEBUILDER_ASSETS="$(make -s kubebuilder-assets-path | tail -1)" \
  go run github.com/onsi/ginkgo/v2/ginkgo -p --race ./pkg/v1/controllers/workapplier
```

155 of 155 specs pass; 61.7s of spec time, 1m13s wall.

## Controller behavior under test

Reconcile flow ([controller.go](../../../pkg/v1/controllers/workapplier/controller.go)):

1. Not joined (`ready=false`) → requeue after 5s, nothing else. `Join()` sets ready; `Leave(ctx)` sets not-ready,
   deletes every appliedWork of a finalized work with **Orphan** propagation, `Forget`s both rate limiters, and strips
   the cleanup finalizer (`placement.kubefleet.dev/work-cleanup`) from all works in the reserved namespace.
2. Get work; not found → done. Only **primary** works (with `placement.kubefleet.dev/linked-work-count`) are
   reconciled; secondary-work events are mapped to the primary via its controller owner reference
   ([eventhandler.go](../../../pkg/v1/controllers/workapplier/eventhandler.go)). Watch uses
   `GenerationChangedPredicate` (label/annotation-only changes are not events).
3. Deleting → `cleanupWhenBindingDeleted` ([cleanup.go](../../../pkg/v1/controllers/workapplier/cleanup.go)):
   delete the primary appliedWork with **Foreground** (`CleanUpResources`, default) or **Orphan**
   (`OrphanResources`) propagation, using the **primary's** sync strategy. Wait; after `cleanupWaitTime` set
   `BlockOwnerDeletion=false` on the appliedWork owner ref of every manifest that has an `Applied` condition, and
   annotate the appliedWork with `placement.kubefleet.dev/applied-work-forced-deleted`; after `2×cleanupWaitTime`
   proceed anyway. Then delete secondary appliedWorks, drop secondary finalizers, `Forget`, drop the primary finalizer.
4. Retrieval ([retrieval.go](../../../pkg/v1/controllers/workapplier/retrieval.go)): list works by the
   `ownerNS/bindingName` field index; linked = same primary-snapshot annotation, leftover = different. Transient error
   (no processing, no finalizer) if the linked count mismatches the annotation or any leftover work is not deleting.
   Unexpected error if the binding label / snapshot annotation is missing or the count is not an integer. Secondaries
   are sorted by name.
5. Add the cleanup finalizer to every linked work; ensure one appliedWork per linked work (transient error if one is
   being deleted). **All** manifests across linked works are owned by the **primary** appliedWork
   (`BlockOwnerDeletion=true`).
6. Pre-process ([preprocess.go](../../../pkg/v1/controllers/workapplier/preprocess.go)): decode (unknown kind / scope
   mismatch → `DecodingErred`; transient REST mapping error → abort), reject `generateName` (`FoundGenerateName`) and
   nameless (`NoNameSpecified`) manifests, reject duplicates across **all** linked works by `GK+ns+name`
   (`Duplicated`; first occurrence in primary-then-name order wins). Write-ahead (skipped when the work `Applied`
   condition already observes the current generation): carry over existing manifest statuses, add
   `PreparingToProcess` for new ones, delete leftover manifests (in the old status, not seen in any linked work, with
   an `Applied` condition, still owned by the primary appliedWork UID; co-owned → disown only).
7. Leftover works: remove their applied manifests not seen in linked works, delete their appliedWork, drop finalizer.
8. Default the sync strategy if nil (CSA, `WhenOwnedByOthers=ReportError`, `WhenDrifted=ApplyAnyway`,
   `WhenAlreadyExists=ReportError`, `WhenPlacementDeleted=CleanUpResources`,
   `WhenNamespaceDoesNotExist=CreateNamespace`, `PartialComparison`). The **primary's** strategy governs all linked
   works' manifests.
9. Process in **waves** across all linked works ([waves.go](../../../pkg/v1/controllers/workapplier/waves.go);
   unknown groups/resources go last), per manifest ([process.go](../../../pkg/v1/controllers/workapplier/process.go)):
   - owned by an old-API (`placement.kubernetes-fleet.io/v1beta1`) AppliedWork → `OwnedByOtherKubeFleetAPIObjects`
     (always);
   - exists, not owned by us, `WhenAlreadyExists≠ReportError` → takeover
     ([takeover_drift_detection.go](../../../pkg/v1/controllers/workapplier/takeover_drift_detection.go)): strip
     owner refs to `v1alpha1` AppliedWorks whose work no longer exists; other owners + not `ShareOwnership` →
     `FailedToTakeOver`; another `v1alpha1` AppliedWork owner → `FailedToTakeOver`; `TakeOverIfNoDiff` with diffs →
     `FailedToTakeOver` + `DiffDetails` (partial = dry-run SSA; Invalid → degraded full comparison); else add owner ref;
   - exists, not owned by us → `NotTakenOver` (covers `WhenAlreadyExists=ReportError`, the default);
   - `WhenDrifted=ReportError` and live manifest-hash annotation equals the current manifest hash → pre-apply drift
     detection → `FoundDrifts` / `FoundDriftsInDegradedMode` + `DiffDetails`;
   - apply ([apply.go](../../../pkg/v1/controllers/workapplier/apply.go)): sanitize, set manifest-hash annotation,
     validate owner refs (co-owned while `ShareOwnership` is off → `FailedToApply`); create (auto-creating the namespace
     if `CreateNamespace`; the namespace is unmanaged) or CSA (strategic merge for built-ins, JSON merge for CRs;
     falls back to non-forced SSA when the last-applied annotation exceeds the size limit) or SSA (`ForceConflicts`;
     forced anyway when all field managers are KubeFleet/`before-first-apply`); optimistic lock iff
     `WhenDrifted=ReportError`. API error → `FailedToApply`;
   - `FullComparison` → post-apply drift detection; diffs are reported with `Applied=True`.
10. Availability ([availability_tracker.go](../../../pkg/v1/controllers/workapplier/availability_tracker.go)) for
    applied manifests only: Deployment/StatefulSet/DaemonSet/Service/CRD/PDB rules; data objects available; others
    `NotTrackable` (counted as available). Not applied → no `Available` condition (a stale one is removed).
11. Status ([status.go](../../../pkg/v1/controllers/workapplier/status.go)): per-manifest conditions carry the
    member object's generation; `DiffDetails.FirstDiffedObservedTimestamp` is preserved while diffs persist and reset
    once they clear; work `Applied`/`Available` (+ `SomeManifestsAreNotTrackable`); status writes are skipped when
    nothing changed; appliedWork status lists applied resources (with UIDs) per work.
12. Requeue after `periodicRequeueRateLimiter.When(...)`
    ([backoff.go](../../../pkg/v1/controllers/workapplier/backoff.go)); the counter resets when any linked work's
    name/generation set or any manifest's processing result changes. Default: 5s once, then slow backoff from 2s
    (×1.2, cap 15s), then fast (×1.5, cap 15m); skip to fast when everything is available.

## What the existing ITs already cover

| File / Context | Behaviors |
| --- | --- |
| core: single work, `core ops` | Finalizer, appliedWork, NS + Deployment apply, owner refs, status, appliedWork status |
| core: single work, `garbage collection` | Foreground appliedWork deletion, forced unblock (`BlockOwnerDeletion=false`), finalizer removal after `2×cleanupWaitTime` |
| core: single work, `leftover cleanup` | Manifest removed from work → object deleted |
| core: multiple works, `core ops` / `garbage collection` | Linked set apply, primary appliedWork owns all, per-work statuses, secondary finalizer/appliedWork cleanup |
| core: multiple works, `leftover cleanup (remove and shuffle…)` | Rollout in progress (inconsistent set → no processing), manifest moved between works kept, removed one deleted |
| core: multiple works, `scaling up` / `scaling down` | Linked count changes, leftover works cleaned up (manifests, appliedWork, finalizer) |
| failures: decoding / generateName / no name / scope mismatch | Pre-processing rejections + fix-later recovery |
| failures: duplicates (×5) | Within one work, same GK different versions, remove-all, primary↔secondary, two secondaries (name-sort winner), handover without re-creation |
| failures: `namespace absent with namespace auto-creation disabled` | `WhenNamespaceDoesNotExist=ReportError` → `FailedToApply`; namespace created outside → applied, unmanaged |

**Not covered at all**: takeover (`WhenAlreadyExists`, `WhenOwnedByOthers`), drift detection (`WhenDrifted`,
`ComparisonOption`), SSA / CSA semantics and fallback, namespace auto-creation (default), `OrphanResources`, leftover
safety (not-owned / co-owned objects), availability beyond Deployment/data objects, waves ordering, requeue/backoff
wiring, status-write skipping, `Join`/`Leave`, namespaced-owner indexing, malformed primary works.

## Test environment constraints and design implications

1. **No GC controller.** Foreground/Orphan deletion of an appliedWork leaves it stuck with `foregroundDeletion` /
   `orphan`. Dependents are never deleted and owner refs are never stripped. Tests simulate GC where needed
   (`appliedWorkRemovedActual` already strips `foregroundDeletion`; add an `orphan` variant). Consequences:
   - `Leave` leaves every appliedWork stuck with `orphan`; on `Join`, `ensureAppliedWorks` returns a transient
     error until the test simulates GC (strip the AppliedWork owner refs from dependents, then the finalizer).
   - Cleanup policy differences (`CleanUpResources` vs `OrphanResources`) are observable only through the propagation
     finalizer on the appliedWork and the forced-unblock path, not through object deletion.
2. **No built-in controllers.** Availability is simulated by writing status (Deployment helper exists; add
   StatefulSet/DaemonSet/PDB/LoadBalancer Service). The API server itself does allocate Service ClusterIPs and marks
   CRDs `Established`/`NamesAccepted`.
3. **No namespace deletion.** Each context uses a fresh namespace; cluster-scoped fixtures (ClusterRole, PriorityClass,
   CRDs) use random names.
4. **Periodic requeue is the only re-trigger for member-side changes.** The work applier does not watch member
   objects; drift/takeover/fix-on-member scenarios rely on the default limiter (first requeue 5s, then 2–15s), so they
   fit in `eventuallyDuration` (10s) for one requeue; use a 20s budget (`eventuallyDurationForRequeue`) where two or
   more requeues are needed (e.g., asserting `FirstDiffedObservedTimestamp` stays stable).
5. **No per-reconcile timestamp in status.** The old `ObservationTime` fields are gone and status writes are skipped
   on no-op reconciles, so reconcile timing and write counts are only observable through instrumented clients (see
   [Suite changes](#suite-changes)).
6. **Default strategy is conservative.** With `nil` sync strategy, pre-existing objects are **not** taken over
   (`NotTakenOver`). Takeover/drift tests must set the strategy explicitly. Fields the test leaves empty are filled
   in by the API server's CRD defaults, except `ServerSideApplyOptions` (defaulted in code).
7. **Full comparison and server-populated fields.** Full comparison of objects with server defaults (Namespace
   `/spec/finalizers`, Deployment defaults, Job selectors/labels) always yields diffs. Use ConfigMaps/Secrets/CRs for
   deterministic full-comparison cases, and assert the full diff list (`cmp.Diff`) where defaults are involved.
8. **`Join`/`Leave` affect the whole reconciler.** `Leave` marks the reconciler not ready, and it strips the cleanup
   finalizer from (and orphans the appliedWork objects of) **every** work object in the reconciler's reserved
   namespace. L1 therefore uses a dedicated reconciler (Decision 2), so that a `Leave`, or a failed test stuck in the
   left state, never affects the other test cases on the same process. `Serial` is not needed. Ginkgo runs specs one
   at a time within a process (an `Ordered` container runs as a unit), and every process has its own envtest
   environments and reconcilers. `Serial` only prevents overlap with specs on other processes, which share nothing
   here. (The earlier note claiming that specs on the same process run in parallel was incorrect.)
9. **Shared member cluster across reconcilers** (Decision 1) is safe only if no test lets one reconciler touch objects
   owned by another reconciler's appliedWorks (`removeLeftBehindAppliedWorkOwnerRefs` resolves works in its own
   reserved namespace only).
10. **The first reconciliation of a work object can take seconds.** It adds the finalizer, creates the appliedWork
    and applies the manifests, and it often runs while the suite is still starting up. Two causes were measured with
    `ginkgo -p --race` (October 22, 2026):
    - The first AppliedWork create on a fresh member API server (Kubernetes 1.33.0) sometimes takes ~2.0s. It is one
      HTTP round trip that returns `201`, with no `Retry-After` and no client-go retries, so the wait is server-side.
      Later creates take a few milliseconds.
    - The CPU is starved while every Ginkgo process boots its envtest control planes; first reconciliations then take
      1–7s in wall time, even though their API calls are fast.

    Timing assertions must therefore not span the first reconciliation (R2/R3 count gaps from the second one).

## Suite changes

0. **Lifecycle reconciler** (`workApplierLifecycle`, hub reserved namespace `fleet-member-experimental-lifecycle`, own
   hub manager `hubMgrLifecycle`). **Implemented (October 21, 2026).** It shares member env 1 (Decision 1), has its
   own cleanup rate limiter, and shares `defaultRequeueRateLimiter`, which keys by namespace/name and so never
   collides. The manager sets `Controller: config.Controller{SkipNameValidation: ptr.To(true)}`, as the work applier
   always registers its controller under the same name (test-only setting). Used by L1; any future test case that
   calls `Join`/`Leave` should use it too.
1. **Reconciler 2: waves** (`workApplierWaves`, hub reserved namespace `fleet-member-experimental-waves`, own hub
   manager `hubMgrWaves`). **Implemented (October 21, 2026).** Same member env as reconciler 1 (Decision 1). Uses
   `parallelizerWithFixedDelay` (port from the old `pkg/controllers/workapplier/suite_test.go`) that sleeps
   `parallelizerFixedDelay` (3s, Decision 4) after every `ParallelizeUntil` whose name starts with
   `processingManifestsInWave`. Its hub client is a `recordingClient` (`hubClientRecorderWaves`), so W5 can count
   work status writes.
2. **Reconciler 3: instrumented** (`workApplierInstrumented`, `fleet-member-experimental-instrumented`, own hub manager
   `hubMgrInstrumented`, same member env). **Implemented (October 21, 2026).** Wraps:
   - the hub (cached) client (`hubClientRecorderInstrumented`): records the timestamps of `Get` calls on `Work`
     objects (= reconcile starts per primary work) and counts `Status().Update` per work;
   - the member client (`memberClientRecorderInstrumented`): counts `Status().Update` per appliedWork.

   It uses a fast custom periodic limiter (`instrumentedRequeueRateLimiter`: 1 fixed attempt of 2s; slow ×1.5 from
   2s, cap 4s; fast ×2, cap 8s; skip-to-fast on), so backoff stages are visible within ~30s. Ported and extended the
   old `clientWrapperWithStatusUpdateCounter` as `recordingClient`.
3. Register the extra field indexes per manager (`newHubManager`), create the extra hub namespaces in
   `setupResources`, start/stop the extra managers in `BeforeSuite`/`AfterSuite`. **Implemented.**
4. Add constants: `eventuallyDurationForRequeue = 20s`, `parallelizerFixedDelay = 3s`. Only `parallelizerFixedDelay`
   was added; specs use multiples of `eventuallyDuration` instead.
5. **CI wiring** (Decision 5): add a `TestMain` that skips the package when `KUBEFLEET_CI_TEST_RUNNER_NAME=default`,
   and a dedicated CI step: `ginkgo -v -p --procs=4 --race --cover -coverprofile=work-applier-v1-it-coverage.out
   ./pkg/v1/controllers/workapplier/`, following the `stagedupdaterun` / old work applier pattern. **Implemented
   (October 22, 2026)**: `TestMain` in `suite_test.go`, and the step `Run work applier (v1) unit and integration
   tests with Ginkgo CLI & generate coverage` in `.github/workflows/ci.yml`, after the staged update run step.

## Shared test utilities ([utils_integration_test.go](../../../pkg/v1/controllers/workapplier/utils_integration_test.go))

All helpers go in this file (Requirement 5), including the ones listed below; do not define helpers in the scenario
files.

- Fixtures: `secret`, `job`, `clusterRole`, `service`, `statefulSet`, `daemonSet`, `pdb`, `testResource`
  (`test.kubernetes-fleet.io/v1alpha1`, preinstalled on the member), and a CRD builder with random group/plural.
- `createPreExistingObject(obj, fieldOwner)`: creates an object on the member directly (own field manager, optional
  extra owner refs, optional old-API AppliedWork owner ref).
- `ownerRefsActual(obj, wantRefs)`, `objectUntouchedActual(obj, wantResourceVersion)` (stays as it was, for
  `Consistently`), generic `memberObjectActual` comparisons for the new kinds.
- `manifestStatusActual(workNS, workName, ordinal, want)` for single-manifest assertions (incl. `DiffDetails`), and
  `retrieveFirstDiffedTimestamp(...)`.
- `appliedWorkBeingDeletedWithOrphanActual`, `appliedWorkRemovedWithOrphanActual` (simulate orphan GC: strip the
  AppliedWork owner refs from listed dependents, then the `orphan` finalizer).
- `appliedWorkForcedDeletedAnnotationActual`.
- `buildWorkObjectWithOwnerNS(...)` (non-empty `placement.kubefleet.dev/owner-namespace` label).
- `markStatefulSetAsAvailable`, `markDaemonSetAsAvailable`, `markPDBAsAvailable`, `markLBServiceAsAvailable`.
- Instrumented-reconciler accessors: `reconcileTimestamps(workKey)`, `workStatusUpdateCount(workKey)`,
  `appliedWorkStatusUpdateCount(name)`.

## Test plan

Priorities: **P0** = core new-API behavior with no coverage; **P1** = important edge/safety behavior; **P2** = nice to
have. Every context is `Ordered` and cleans up in `AfterAll` (delete work → simulate GC → work gone).

### T. `takeover_integration_test.go`: pre-existing objects (`WhenAlreadyExists`, `WhenOwnedByOthers`)

| ID | P | Context | Setup → steps | Assert |
| --- | --- | --- | --- | --- |
| T1 | P0 | default strategy (report error) on pre-existing objects | Pre-create NS-scoped ConfigMap (different data) + matching Deployment; work with `nil` strategy | Both `Applied=False/NotTakenOver`, no `DiffDetails`, no `Available` cond; objects untouched (`Consistently`), no owner ref; work `Applied=False`, `Available=False`; appliedWork lists nothing; on delete, objects stay |
| T2 | P0 | always take over | Pre-create ConfigMap (different data + extra member-only label) with no owners; `AlwaysTakeOver` | Owner ref added; managed fields overwritten; member-only label kept (CSA); `Applied=True`; deletion → foreground on appliedWork |
| T3 | P0 | take over if no diff (no diff) | Pre-create identical NS + Deployment; `TakeOverIfNoDiff` | Taken over (owner refs), `Applied=True`, `Available` after marking |
| T4 | P0 | take over if no diff (diff, partial comparison) → fix on member | Pre-create ConfigMap with `data.foo` diff + Deployment with replicas diff; `TakeOverIfNoDiff`, partial | `FailedToTakeOver` + `DiffDetails` (paths, values, `ObservedInMemberClusterGeneration`), untouched; then revert ConfigMap on member → taken over at next requeue, `DiffDetails` cleared; Deployment still failing |
| T5 | P0 | take over if no diff (unmanaged-only diff), partial vs full | Two contexts: pre-create ConfigMap identical except a member-only label; partial / full | Partial: taken over, no diffs; Full: `FailedToTakeOver` with `/metadata/labels/<k>` (`ValueInMember` only) |
| T6 | P1 | take over if no diff (degraded) | Pre-create Job whose template label differs; `TakeOverIfNoDiff`, partial | `FailedToTakeOver` with the degraded full-comparison diff list (assert with `cmp.Diff`); Job untouched |
| T7 | P0 | co-ownership | Pre-create ConfigMap owned by an arbitrary non-KubeFleet owner; `AlwaysTakeOver` + `WhenOwnedByOthers=ReportError` → switch to `ShareOwnership` → switch back to `ReportError` | `FailedToTakeOver` ("owned by some other source(s)"), untouched → co-owned (both refs), applied → `FailedToApply` ("co-owned … disallowed"), refs unchanged |
| T8 | P1 | owned by old KubeFleet APIs | Pre-create ConfigMap owned by a `placement.kubernetes-fleet.io/v1beta1` AppliedWork; `AlwaysTakeOver` + `ShareOwnership` | `OwnedByOtherKubeFleetAPIObjects`; untouched |
| T9 | P0 | duplicate placement across independent works | Work A (own binding) places ConfigMap X; then work B (different binding) places X with: `nil` → `AlwaysTakeOver` → `AlwaysTakeOver`+`ShareOwnership`; delete work A and strip only the propagation finalizer from A's appliedWork (X keeps a dangling owner ref, as with a GC that has not caught up) | B: `NotTakenOver` → `FailedToTakeOver` (other owners) → `FailedToTakeOver` (another KubeFleet AppliedWork); after work A is gone, B strips the left-behind ref and takes over X (owner refs = B only) |
| T10 | P1 | switch report error → take over if no diff | Pre-existing identical NS + Deployment; `nil` → update strategy to `TakeOverIfNoDiff` | `NotTakenOver` → taken over and applied without spec change on member |
| T11 | P1 | linked works use the primary's strategy | Primary `AlwaysTakeOver` (ConfigMap A); secondary strategy `nil` (pre-existing ConfigMap B) | B taken over and owned by the **primary** appliedWork; secondary status `Applied=True` |
| T12 | P1 | sensitive data in takeover diffs | Pre-create Secret with different `data`; `TakeOverIfNoDiff` | `FailedToTakeOver`; `/data/<k>` values are `(redacted for security reasons)` |

### D. `drift_integration_test.go`: drift detection (`WhenDrifted`, `ComparisonOption`)

| ID | P | Context | Setup → steps | Assert |
| --- | --- | --- | --- | --- |
| D1 | P0 | apply anyway (default), partial | NS + Deployment; change replicas on member; add member-only label | Replicas reverted at the next periodic requeue (no work change); member-only label kept; no `DiffDetails` |
| D2 | P0 | report error, partial, drift appears, changes, then is reverted | NS + Deployment (`WhenDrifted=ReportError`); mark available; change replicas on member (2), then change them again (3) to force a status rewrite while still drifted; revert on member, then re-mark available (the replica edits bump the generation) | `FoundDrifts`, `/spec/replicas` diff, `ObservedInMemberClusterGeneration`; Deployment untouched; manifest `Available` cond **removed**; work `Applied=False`/`Available=False`; `FirstDiffedObservedTimestamp` stable across the rewrite; after revert: `Applied=True`, `DiffDetails=nil`, `Available` back |
| D3 | P1 | report error, partial, unmanaged change only | Add member-only label | No drift; `Applied=True` |
| D4 | P0 | report error, manifest bumped after drift | After drift (D2 setup), update the manifest (e.g., image) | Hash mismatch → apply proceeds and overwrites the drift; `Applied=True`, no diffs |
| D5 | P0 | report error, full comparison | ConfigMap; add member-only label | `FoundDrifts` with `/metadata/labels/<k>` |
| D6 | P0 | apply anyway, full comparison (post-apply diffs) | ConfigMap + NS; add member-only label to ConfigMap | `Applied=True` (reason `Applied`) **with** `DiffDetails` for the label (persists, CSA keeps it); NS reports a diff on its server-populated finalizers (exact path depends on the fixture's `spec`) |
| D7 | P1 | report error, partial, degraded | NS + Job; change the template label on member (immutable field) | `FoundDriftsInDegradedMode`; Job untouched; diff list asserted |
| D8 | P1 | sensitive data in drift details | Secret + ConfigMap, `ReportError`; change both on member | ConfigMap values shown; Secret `/data/*` redacted |
| D9 | P1 | switch report error → apply anyway with drift present | D2 setup, then update strategy | Drift overwritten; `DiffDetails` cleared |
| D10 | P2 | drift on a secondary work's manifest | Primary (`ReportError`) + secondary with ConfigMap; drift on that ConfigMap | Reported on the **secondary** work's status; strategy from primary |

### A. `apply_integration_test.go`: apply methods and semantics

| ID | P | Context | Setup → steps | Assert |
| --- | --- | --- | --- | --- |
| A1 | P0 | CSA manifest updates (built-in, strategic merge) | ConfigMap with labels/annotations/data; third party adds data key + label, changes a managed key; update manifest (change, remove label, add annotation) | Managed values follow manifest; removed label gone; third-party additions kept; `Applied` generation bumps |
| A2 | P1 | CSA on custom resources (JSON merge patch) | `TestResource` (preinstalled CRD); third party adds `items`, changes `foo`; update manifest | Scalars follow manifest, selector replaced; third-party `items` kept; `Available=True/NotTrackable`, work reason `SomeManifestsAreNotTrackable` |
| A3 | P1 | missing last-applied annotation | Strip `placement.kubefleet.dev/last-applied-configuration` on member; update manifest | Still converges; annotation restored |
| A4 | P0 | CSA → SSA fallback for oversized objects | ConfigMap with ~300KB data; then update it | Created with empty last-applied annotation; update applied via SSA; `Applied=True` |
| A5 | P0 | SSA and force conflicts | Pre-create ConfigMap via test field manager (`data.foo=bar`); `ServerSideApply`, `AlwaysTakeOver`, manifest `foo=baz`; then `ForceConflicts=true` | `FailedToApply` (conflict) → applied, `managedFields` show KubeFleet owning `data.foo` |
| A6 | P1 | switch CSA ↔ SSA | NS + Deployment; CSA → SSA → CSA, changing a field each time | Each switch applies (self-conflict avoided via forced SSA); no spurious failures |
| A7 | P0 | namespace auto-creation (default) | ConfigMap in a non-existent namespace, `nil` strategy | Namespace created with no owner refs; ConfigMap applied; on work deletion the namespace stays and is not in appliedWork status |
| A8 | P1 | API server rejects a manifest; replacement fails | NS + ConfigMap + invalid Deployment (e.g., negative replicas) → fix it; then replace all manifests with an invalid one | `FailedToApply` with correct identifier; others applied; fixed → applied; replaced → old objects removed even though the new manifest fails, appliedWork empty |
| A9 | P1 | CRD and its CR in the same work | Random-group CRD + one CR | CRD applied first (wave 1); CR applied at a later reconcile once the REST mapper sees the CRD (first pass `DecodingErred` is allowed); both eventually `Applied`; CRD `Available` |
| A10 | P2 | manifest reorder within a work | Swap the order of two ConfigMaps | No re-creation (UIDs stable); status ordinals follow new order; condition LTTs stable |

### C. `cleanup_integration_test.go`: deletion policies and leftover safety

| ID | P | Context | Setup → steps | Assert |
| --- | --- | --- | --- | --- |
| C1 | P0 | orphan resources on placement deletion (linked works) | Primary (`OrphanResources`) + secondary; delete primary | Primary appliedWork deleting with `orphan` finalizer; secondary appliedWork deleted + finalizer dropped; after `cleanupWaitTime` refs unblocked + forced-deleted annotation; work gone after `2×cleanupWaitTime`; objects remain |
| C2 | P1 | the primary's deletion policy wins | Primary `nil` (cleanup) + secondary `OrphanResources` | Foreground propagation (`foregroundDeletion`) |
| C3 | P0 | leftover object no longer owned by KubeFleet | Apply ConfigMap; strip the AppliedWork owner ref on member (same code path as a user delete + re-create, without racing the periodic requeue that would re-create it); wait for `NotTakenOver`; remove manifest | Object kept |
| C4 | P0 | leftover object never taken over | T1-style pre-existing ConfigMap (`NotTakenOver`); remove manifest | Object kept untouched |
| C5 | P0 | leftover co-owned object | `ShareOwnership` co-owned ConfigMap (T7-style); remove manifest | Only the AppliedWork owner ref removed; object and other owner kept |
| C6 | P1 | co-owned object dropped by a rollout (leftover work path) | Primary + secondary; secondary holds the co-owned ConfigMap; roll out to a new snapshot without it (delete old secondary) | Disowned, not deleted |
| C7 | P1 | remove all manifests | Work with NS + ConfigMap → empty manifests | ConfigMap deleted; work `Applied=True`/`Available=True` (0 manifests); appliedWork status empty |
| C8 | P2 | forced-deletion annotation (extend core `garbage collection`) | — | Assert `placement.kubefleet.dev/applied-work-forced-deleted` on the stuck appliedWork |

### L. `lifecycle_integration_test.go`: join/leave, work selection, indexing

| ID | P | Context | Setup → steps | Assert |
| --- | --- | --- | --- | --- |
| L1 | P0 | leave and re-join (dedicated lifecycle reconciler) | 3 single works (ConfigMaps) applied; `Leave` (twice, idempotent); update all works; `Consistently` not processed; simulate orphan GC; `Join`; `DeferCleanup(Join)` | After `Leave`: no finalizers, appliedWorks deleting with `orphan`; objects intact; updates not applied while left. After GC + `Join`: finalizers back, new appliedWorks; objects (now unowned) → `NotTakenOver` with default strategy (see Behaviors 1); with `AlwaysTakeOver` works, objects re-adopted and updated |
| L2 | P1 | non-primary work objects | A standalone work without the linked-count annotation, plus a secondary-style work owned by a primary work that does not exist | Never processed: no finalizer, no appliedWork, no status, no object or namespace created (`Consistently`); deletable at once |
| L3 | P1 | malformed primary works | Three primaries: no binding label / no snapshot annotation / non-integer count | Never processed (no finalizer, no appliedWork); deletable at once |
| L4 | P0 | owner-namespace isolation in the field index | Two single primaries with the **same** binding name: cluster-scoped (`owner-namespace=""`) and namespaced (`owner-namespace=<ns>`), plus a secondary for the namespaced one | Each processed as its own linked set (linked count matches); deleting one does not touch the other |
| L5 | P2 | appliedWork deleted externally | Delete the appliedWork (background); simulate GC by deleting the dependents | appliedWork re-created (new UID); objects re-created and owned by it; appliedWork status lists the new UIDs |

### V. `availability_integration_test.go`: availability tracking

| ID | P | Context | Setup → steps | Assert |
| --- | --- | --- | --- | --- |
| V1 | P1 | immediately available and untrackable kinds | NS, ClusterIP Service, CRD, Secret, ExternalName Service, `TestResource` | Service/CRD/data `Available`; ExternalName + CR `NotTrackable`; work `Available=True/SomeManifestsAreNotTrackable` |
| V2 | P1 | workloads become available | StatefulSet, DaemonSet, PDB, LoadBalancer Service | `Unavailable` → mark each available → `Available`; work reason transitions to `AllManifestsAvailable` |

Exhaustive per-kind rules belong in unit tests (see below).

### W. `waves_integration_test.go`: wave ordering (reconciler 2)

| ID | P | Context | Assert |
| --- | --- | --- | --- |
| W1 | P1 | single wave (NS + PriorityClass) | Created within ~1s of each other |
| W2 | P1 | two consecutive waves (NS → ConfigMap) | Gap ≥ delay, < 2×delay |
| W3 | P1 | two non-consecutive waves (NS → Role) | Gap ≥ delay, < 2×delay (empty wave adds nothing) |
| W4 | P2 | all waves (shuffled manifests incl. EndpointSlice as unknown) | Waves 0–5 + last all present; intra-wave ≤ 2s; adjacent waves ≥ delay |
| W5 | P0 | waves span linked works (runs on reconciler 2, whose hub client also counts status writes) | NS in the **secondary**, ConfigMap in the **primary**, `WhenNamespaceDoesNotExist=ReportError` | ConfigMap applied in the first pass: the primary's work status is written exactly twice (write-ahead + final `Applied=True`). A failed first pass would add a third write. The creation-timestamp gap (NS → ConfigMap: ≥ delay, < 2×delay) also catches a failed first pass: the ConfigMap would then be applied in a later reconciliation, at least one requeue delay (5s) plus wave delays later, beyond the upper bound |

### R. `requeue_integration_test.go`: requeue and status writes (reconciler 3)

| ID | P | Context | Assert |
| --- | --- | --- | --- |
| R1 | P0 | status writes are skipped at steady state | NS + ConfigMap single work: exactly 2 work status writes (write-ahead + final) and 1 appliedWork status write; none over ≥2 more reconciles; same per work for a primary + secondary set |
| R2 | P1 | backoff progression (unavailable) + reset on result change | Deployment never available: delays 2s, 2s, 3s, 6s, 8s, 8s; gaps checked from the 2nd reconcile: 2s, 3s, 6s, 8s, 8s. Marking it available → next gap is the fixed delay (2s) |
| R3 | P1 | skip to fast backoff (available) + reset on change | ConfigMap: delays 2s, 2s, 4s, 8s, 8s; gaps checked from the 2nd reconcile: 2s, 4s, 8s, 8s. `nudgeWork` → after skipping the first reconcile at or after the nudge, the next gap is 2s (the fixed delay or the initial slow backoff delay; the skip guards against a periodic reconcile that reads the pre-nudge generation from the cache) |

Gaps are derived from `When()` with the proposed limiter (fixed 2s ×1; slow ×1.5 from 2s, cap 4s; fast ×2, cap 8s).
They are measured between reconcile starts, so they include the reconcile duration (tolerance −250ms/+1.5s). The
backoff-progression gaps skip the first reconciliation (see
[constraint 10](#test-environment-constraints-and-design-implications)); the fixed delay is checked by the reset
specs instead. R scenarios avoid takeover, so that `Work` `Get` calls map 1:1 to reconciles
(`removeLeftBehindAppliedWorkOwnerRefs` also `Get`s works). Count only successful status writes in R1/W5 to stay
robust to conflicts. R2/R3 are labeled `slow` (Decision 3).

### Better as unit tests (out of scope here; gaps in the new package)

The new package has UTs only for `backoff`, `cleanup.shouldCleanUpManifest`, and `eventhandler`. Port/adapt from the
old package:

- `availability_tracker_test.go` (every kind and status combination).
- `waves_test.go` (`organizeManifestsIntoProcessingWaves`).
- `preprocess_test.go` (decode, identifiers, duplicates, `findLeftOverManifests`, write-ahead status building).
- `apply_test.go` (`sanitizeManifestObject`, `setLastAppliedConfigAnnotation` size limit,
  `shouldUseForcedServerSideApply`, `validateOwnerRefs`).
- `drift_detection_takeover_test.go` (`preparePatchDetails`, `discardFieldsIrrelevantInComparisonFrom`, redaction,
  `isPlacedByKubeFleetInDuplicate`, `shouldInitiateTakeOverAttempt`, `isOwnedByOlderKubeFleetAPIObjects`).
- `status_test.go` (condition setters, port-back, `FirstDiffedObservedTimestamp`, work-level aggregation).
- Branches envtest cannot reach reliably: `FailedToFindObjInMemberCluster`, `FailedToRunDriftDetection`,
  `AppliedWithFailedDriftDetection`, transient decoding errors, context cancellation mid-wave, `Forget` bookkeeping.

## Old tests → new plan

| Old area (`pkg/controllers/workapplier`) | New plan |
| --- | --- |
| `apply new manifests (regular)`, migrated single ConfigMap | Covered (core) |
| `garbage collect removed manifests`, migrated "delete manifest removed from work" | Covered (core leftover) |
| Generate name, decoding errors, duplicates, `mixed (pre-processing)` | Covered (failures) |
| GC `blockOwnerDeletion=false` (3 co-ownership variants) | Basic unblock covered (core); co-owned variants → C5/C6. Other-owner valid/invalid variants dropped (no GC in envtest → nothing observable) |
| Takeover `IfNoDiff` (no diff / partial diff / full diff) | → T3 / T4, T5 |
| `never take over` | Behavior changed → T1 (`ReportError` is now the **default**; no diffs are computed) |
| Drift: partial / degraded / full / overwrite (always apply) | → D2 / D7 / D5 / D1 |
| Drift before manifest version bump | → D4 |
| First drifted time preservation | → D2 (`FirstDiffedObservedTimestamp`) |
| Obscure sensitive data (drift / report diff) | → D8 / T12 |
| Report diff (6 contexts), switch report diff ↔ CSA/SSA | Dropped (no `ReportDiff` in `v1alpha1`); diff reporting lives in T4/T5 and D2/D5; strategy switches → T10, D9, A6 |
| Switch never → take over if no diff | → T10 |
| Falling back from CSA to SSA | → A4 |
| Status back-reporting | Dropped (no report-back strategy in `v1alpha1`) |
| Migrated: built-in change / third-party merge | → A1 |
| Migrated: CRD change | → A2 |
| Migrated: missing last-applied annotation | → A3 |
| Migrated: failed manifest identification | Behavior changed (missing namespaces are auto-created by default) → A8 (API validation failure) |
| Migrated: delete even if apply fails | → A8 |
| Migrated: reorder manifests | → A10 |
| Migrated: join and leave | → L1 (now `Leave` orphans appliedWorks; envtest needs simulated GC) |
| `backoff_integration_test.go` | → R2/R3 (via instrumented reconcile timestamps; `ObservationTime` is gone) |
| `status_integration_test.go` (skipped status updates) | → R1 |
| `waves_integration_test.go` | → W1–W4, plus W5 (cross-work waves, new) |
| Priority queue runs (`KUBEFLEET_CI_WORK_APPLIER_RUN_WITH_PRIORITY_QUEUE`), metrics | Dropped (no priority queue or metrics in the new package) |

## Decisions

All the decisions below are adopted; the implemented test cases follow them.

1. **Extra reconcilers share member env 1** (separate hub managers and reserved namespaces) rather than starting extra
   member API servers per Ginkgo process, as the old suite did (4 member envs). This keeps `-p` runs light. The
   constraint in [Test environment constraints](#test-environment-constraints-and-design-implications) item 9 applies.
   Applied to the lifecycle, waves, and instrumented reconcilers (October 21, 2026).
2. **`Join`/`Leave` (L1) runs on a dedicated lifecycle reconciler**, with `DeferCleanup(workApplierLifecycle.Join)`.
   No `Serial` decorator is needed (see constraint 8). **Adopted (October 21, 2026)**, replacing the earlier choice of
   `Serial` on reconciler 1. L4 does not call `Join`/`Leave`, so it stays on reconciler 1.
3. **Include R2/R3 (backoff timing), labeled `slow`**, on the instrumented reconciler with a fast custom limiter. The
   limiter math is already covered by `backoff_test.go`; these specs only check the wiring (counter reset on spec and
   result changes, skip-to-fast). **Adopted (October 21, 2026)**; the alternative (drop R2/R3 and keep R1 only) is not
   taken.
4. **Wave delay 3s** (old: 5s), with assertions `gap ≥ 3s` and `gap < 6s` (creation timestamps have 1s granularity).
5. **Gate the package in `make local-unit-test` and add a dedicated Ginkgo CI step** (`TestMain` +
   `KUBEFLEET_CI_TEST_RUNNER_NAME`), as for `stagedupdaterun`. Previously the package ran under `go test` in
   `make local-unit-test` with no `-p`. **Implemented (October 22, 2026)**; see [Suite changes](#suite-changes) item 5.
6. **Unit-test gaps** (previous section) are a follow-up, not part of this IT work.

## Behaviors to note (documented only; tests assert current behavior)

1. **Re-join does not re-adopt resources by default.** `Leave` orphans appliedWorks, and the GC strips their owner
   refs. On re-join, new appliedWorks get new UIDs and the existing objects are unowned. With the default
   `WhenAlreadyExists=ReportError` they become `NotTakenOver`. Product review (October 22, 2026): this is
   **expected**. Resources previously applied by a cluster that left and rejoined may need to be taken over again.
2. **Stale-UID owner refs are not cleaned up.** `removeLeftBehindAppliedWorkOwnerRefs` drops AppliedWork owner refs
   only when the **work** is gone (name check). A ref to a re-created appliedWork of the same name (old UID) is kept, so
   takeover fails as a duplicate placement.
3. `ReportError` + `FullComparison` on objects with server-populated fields (Namespace, Deployment, Job) never
   converges: the first apply succeeds with post-apply diffs, and every later reconcile reports `FoundDrifts`.
   `TakeOverIfNoDiff` + `FullComparison` never takes such objects over.
4. Post-apply diffs (full comparison) are reported with `Applied=True` (reason `Applied`); there is no dedicated
   condition.
5. Not-applied manifests get no `Available` condition (removed, not `False`), and the work reads
   `Available=False/SomeManifestsAreNotYetAvailable` even when the cause is an apply failure.
6. Label/annotation-only changes to works (e.g., linked-count or snapshot annotation without a spec change) are
   filtered by `GenerationChangedPredicate` and only picked up at the next periodic requeue.
7. Events on secondary works without a valid controller owner ref are dropped (logged); their changes are picked up
   only by the primary's periodic requeue.
8. Pre-apply drift detection requires the live manifest-hash annotation to match. Objects just taken over, or whose
   manifest just changed, are applied (drifts overwritten) even with `WhenDrifted=ReportError`.
9. A work with zero manifests reports `Applied=True` and `Available=True`.
10. Taking over a pre-existing Deployment bumps its generation (1 → 2) even when its spec is unchanged: the API
    server bumps Deployment generations on annotation changes too (annotations are copied to ReplicaSets), and the
    apply op adds the KubeFleet annotations. Manifest conditions therefore report generation 2 for taken-over
    Deployments (T3), but 1 for Deployments the work applier created or never wrote to.

## Running the tests

Always use the Ginkgo CLI in parallel mode, via `go run` so the CLI matches the Ginkgo version pinned in `go.mod`
(v2.33.0):

```bash
KUBEBUILDER_ASSETS="$(make -s kubebuilder-assets-path | tail -1)" \
  go run github.com/onsi/ginkgo/v2/ginkgo -p --race ./pkg/v1/controllers/workapplier
```

Each parallel process runs its own `BeforeSuite` (own envtest hub/member, managers, reconcilers) and runs one spec at
a time; `Ordered` contexts stay within one process and run as a unit. Use
`--focus="<Describe/Context text>"` for a subset (the regex is not anchored at the `Describe` text, so `^takeover`
matches nothing), `--focus-file=<file>_integration_test.go` for one file, and `--label-filter='!slow'` to skip R2/R3.

## Estimated runtime

Rough, per context (serial time): most T/D/A/C/L/V contexts 10–25s (one or two periodic requeues plus fast cleanup
via simulated GC); C1/C2 ~15s (`2×cleanupWaitTime`); W1–W3 ~10s each, W4 ~25s, W5 ~10s; R1 ~15s, R2/R3 ~30s each; L1
~30s. ~55 new contexts ≈ 15–18 min of serial time. With `-p` on ~8 processes this is ~3–4 min wall (vs. 1m13s
today).

## Status

- [x] Surveyed the new package source, the existing ITs, and the old ITs (October 21, 2026).
- [x] Baseline run: 155/155 pass with `ginkgo -p --race` (61.7s spec time).
- [x] Decisions adopted (see [Decisions](#decisions)).
- [ ] Suite changes + shared utilities (Suite changes items 0–3 and 5 done; the P1/P2 fixtures pending).
- [x] **T1** (October 21, 2026): `takeover_integration_test.go`, context `pre-existing objects with the default sync
  strategy (report error, no takeover)`, 10 specs. Beyond the plan row, the work deletion does **not** simulate GC, so
  the work applier takes the forced cleanup path (which visits every manifest with an `Applied` condition) and the test
  verifies that path does not write to objects it never owned. Added helper `objectUntouchedActual` (resource version
  unchanged) and constant `notTakenOverErrMsg` to `utils_integration_test.go`. Verified: focused run 10/10; mutation
  check (`AlwaysTakeOver`) fails as expected; full package run 165/165 with `ginkgo -p --race` (69.2s spec time);
  `golangci-lint --fast-only` clean.
- [x] Renamed `utils_test.go` → `utils_integration_test.go` (`git mv`; helpers only, no `Test` functions) (October 21,
  2026).
- [x] **T-section P0s** (October 21, 2026): T2–T5, T7, T9 in `takeover_integration_test.go`. There are 7 new contexts
  (T5 is split into partial and full comparison contexts), and the file now has 64 specs:
  - T2 `always take over`; T3 `take over if no diff (no diff)`, which asserts the not-yet-available status before
    marking the Deployment available, to avoid racing the takeover and apply writes (Behaviors 10 covers its
    generation of 2);
  - T4 `take over if no diff (diffs in managed fields, partial comparison)`, which also checks that the Deployment's
    `FirstDiffedObservedTimestamp` survives a status rewrite;
  - T5 `… (diffs in unmanaged fields only, partial comparison)` / `… (full comparison)`; T7 `co-ownership`; T9
    `objects placed by another work object (duplicate placement)`.
  - New in `utils_integration_test.go`: helpers `configMapObjectActual` (labels + any owners),
    `objectNotRecreatedActual`, `retrieveManifestFirstDiffedTimestamp`; the `externalOwnerRef` fixture; constants
    `ownedByOthersErrMsg`, `placedInDuplicateErrMsg`, `coOwnershipDisallowedErrMsg`, and
    `memberOnlyLabelKey`/`memberOnlyLabelValue`.
  - Verified: the takeover file runs 64/64. Two temporary product mutations were caught (keeping left-behind
    AppliedWork owner refs → T9 fails; skipping the co-ownership check in `validateOwnerRefs` → T7 fails). The full
    package runs 219/219 with `ginkgo -p --race` (67.0s spec time, 1m15s wall), and `golangci-lint --fast-only`,
    `gofmt` and `goimports` are clean.
- [x] **W and R sections** (October 21–22, 2026): W1, W2, W3 (P1) and W5 (P0) in the new `waves_integration_test.go`
  (4 contexts, 9 specs, on reconciler 2); R1 (P0, as two contexts: a single work and a linked primary + secondary
  set), R2 and R3 (P1, labeled `slow`) in the new `requeue_integration_test.go` (4 contexts, 18 specs, on reconciler
  3). W4 (P2) is not written yet. Suite changes items 1–3 are implemented for these (see
  [Suite changes](#suite-changes)).
  - W1–W3 compare the creation timestamps of the applied objects (same wave: within 1s; next wave: ≥ delay and
    < 2×delay). W5 checks that each linked work status is written exactly twice, i.e., the config map in the primary
    work is applied in the same pass as the namespace in the secondary work; it also checks that the config map is
    created in the wave right after the namespace.
  - R1 checks 2 work status writes and 1 appliedWork status write per work, then no more writes over ≥2 further
    reconciles. R2/R3 check the requeue gaps between reconcile starts, then the reset to 2s delays after the
    deployment is marked available (R2) or the work is updated (R3; see the review fixes below).
  - New in `utils_integration_test.go`: `parallelizerWithFixedDelay`, `recordingClient` (with
    `recordingStatusWriter`), `requeueGapsActual`, `retrieveCreationTimestamp`, and the constants
    `requeueGapLowerTolerance`/`requeueGapUpperTolerance` (−250ms/+1.5s).
  - Mutation checks (October 21). Batch 1 made every manifest wave 0 and disabled the reset on linked-work
    generation changes. It failed W2, W3, W5 and R3's reset spec, plus R1's single-work context, which also depends
    on wave ordering (its namespace and config map are in different waves). Batch 2 put every manifest in its own
    wave, never skipped status writes, and disabled both the reset on processing-result changes and skip-to-fast.
    It failed W1, W2, W5, both R1 contexts, R3's backoff spec and R2.
  - **R2/R3 first-gap flake (verified and fixed October 22, 2026).** In batch 2, R2 failed at its backoff spec
    instead of its reset spec. Re-running showed the mutation check is sound: disabling the processing-result reset
    alone fails R2 exactly at the reset spec (gap 8.03s, want 2s), and forced status writes alone leave R2 passing.
    The backoff-spec failure was a pre-existing flake. With no mutation, the waves and requeue files failed in 6 of
    14 `ginkgo -p --race` runs (R2 ×4, R3 ×2), and each time only the first gap was off, by ~2–2.5s. Instrumenting
    `Reconcile` traced this to the first reconciliation itself (see constraint 10). The fix: `requeueGapsActual`
    takes a `skip` count, and the backoff specs count gaps from the second reconciliation (R2: 2s, 3s, 6s, 8s, 8s;
    R3: 2s, 4s, 8s, 8s); the specs are renamed `should back off slowly, then back off fast` and
    `should skip to fast backoff`. Replaying all 26 recorded R2/R3 runs, every one passes with the first gap
    skipped; the worst later overshoot is +84ms.
  - Verified after the fix: the waves and requeue files pass in 6 of 6 runs. Three mutations of `backoff.go` were
    each caught at the expected spec only:
    - no reset on processing-result or work changes fails the R2 and R3 reset specs;
    - never skipping to fast backoff fails R3's backoff spec;
    - always skipping to fast backoff fails R2's backoff spec.

    The full package runs 368/368 with `ginkgo -p --race` (123.8s spec time, 2m13s wall); lint, gofmt and
    goimports are clean.
- [x] **C-section and L-section P0s** (October 21, 2026): C1, C3, C4, C5 in the new `cleanup_integration_test.go`
  (4 contexts) and L1, L4 in the new `lifecycle_integration_test.go` (2 contexts), 43 specs in all.
  - C1 `orphan resources upon placement deletion (linked work objects)`. Only the primary sets `OrphanResources`; the
    secondary has no sync strategy. The test checks that:
    - the primary appliedWork is deleted with the `orphan` finalizer;
    - after `cleanupWaitTime`, both applied objects' AppliedWork refs are unblocked and the
      `applied-work-forced-deleted` annotation is set;
    - after `2×cleanupWaitTime`, the secondary finalizer and appliedWork are removed and the primary work is gone,
      while the primary appliedWork stays pending deletion for the GC and the applied objects stay in place.
  - C3 `leftover objects no longer owned by KubeFleet`: the AppliedWork owner ref is stripped on the member, which
    yields `NotTakenOver`. Removing the manifest then leaves the object untouched.
  - C4 `leftover objects never taken over`: the pre-existing object is `NotTakenOver`; removing the manifest leaves it
    untouched.
  - C5 `leftover objects co-owned with other sources`: removing the manifest drops only the AppliedWork owner ref;
    the other owner and the data are kept, and the object is not re-created.
  - L1 `leaving and re-joining the fleet` (originally `Serial` on reconciler 1; rewired to the dedicated lifecycle
    reconciler, see the next entry). Two works are used, one
    with the default strategy and one with `AlwaysTakeOver`, instead of three. The test checks that:
    - `Leave` (retried on conflicts, then called again to check it is idempotent) removes the finalizers and deletes
      the appliedWorks with the `orphan` finalizer;
    - work updates are not processed while left;
    - after the GC is simulated (the AppliedWork owner refs are stripped from the dependents, then the `orphan`
      finalizer) and `Join` is called, the finalizers come back and the appliedWorks are re-created with new UIDs;
    - the default-strategy work reports `NotTakenOver` and leaves the orphaned object as is, which confirms
      Behaviors 1, while the `AlwaysTakeOver` work re-adopts the object (same UID) and applies the update.

    `DeferCleanup` and the `AfterAll` re-join the fleet if the test fails.
  - L4 `owner namespaces of placement bindings (same placement binding name)`: a cluster-scoped binding (work A) and
    a namespaced binding of the same name (works B and B2), all linked to a snapshot of the same name. Each set is
    processed on its own, and deleting work A leaves B2's finalizer and appliedWork and the B-owned objects alone.
  - Changes in `utils_integration_test.go`: `appliedWorkRemovedActual` now also strips the `orphan` finalizer
    (simulating orphan GC). New: `appliedWorkBeingDeletedWithOrphanActual`.
  - Verified: the C and L files run 43/43. One batch of five temporary product mutations failed exactly the expected
    six contexts:
    - foreground deletion despite `OrphanResources` fails C1;
    - skipping the ownership check on leftovers fails C3 and C4;
    - deleting co-owned leftovers fails C5;
    - dropping the owner namespace from the field index fails L4;
    - reconciling while not joined fails L1.

    The full package runs 328/328 with `ginkgo -p --race` (121.8s spec time, 2m10s wall; the `Serial` L1 tail runs
    alone at the end). Lint, gofmt and goimports are clean.
- [x] **Dedicated lifecycle reconciler + L1 rewire** (October 21, 2026). Added `workApplierLifecycle` / `hubMgrLifecycle`
  and the `fleet-member-experimental-lifecycle` hub namespace to `suite_test.go` (Suite changes item 0). L1 now uses
  them and drops `Serial` (Decision 2 revised; constraint 8 corrected); L4 stays on reconciler 1. Verified: the
  lifecycle file runs 18/18; the "reconcile while not joined" mutation still fails L1 at "should not process the work
  objects while away from the fleet". The full package runs 328/328 with `ginkgo -p --race` (104.1s spec time,
  1m51s wall, down from 2m10s now that L1 runs in parallel with the other contexts); lint, gofmt and goimports are
  clean.
- [x] **Rest of the L section** (October 21, 2026): L2 (P1), L3 (P1), L5 (P2) in `lifecycle_integration_test.go`, all
  on reconciler 1 (none of them calls `Join`/`Leave`). The L section is now complete; the file has 5 contexts and 31
  specs.
  - L2 `work objects that are not primary work objects`: a standalone work (no linked-count annotation, no owner)
    and a secondary-style work whose controller owner (a primary work) does not exist.
  - L3 `malformed primary work objects`: three primaries, missing the binding label, missing the snapshot annotation,
    and with a non-integer linked count.
  - For L2 and L3, the manifests target a namespace that is never created, so an unprocessed work leaves no object
    and no auto-created namespace behind. The test checks this with `Consistently`, then checks that each work is
    removed at once on deletion (no finalizer).
  - The malformed works are safe for the rest of the suite: the field index logs extraction errors and skips the
    object.
  - L5 `appliedWork objects deleted by an external actor`: the appliedWork is deleted with background propagation,
    then GC is simulated by deleting the dependent. At the next requeue, both the appliedWork and the object are
    re-created with new UIDs, and the appliedWork status lists the new object UID. If the work applier reconciles
    between the two deletions, it briefly reports the object as `NotTakenOver` (it is still owned by the deleted
    appliedWork) until the GC catches up.
  - New in `utils_integration_test.go`: `workNotProcessedActual`.
  - Verified: the lifecycle file runs 31/31. One batch of mutations failed exactly L2, L3 and L5 at the expected
    specs, while L1 and L4 passed:
    - five product changes: the event handler enqueues ownerless non-primary works; `Reconcile` stops skipping
      non-primary works; `retrieval.go` ignores the missing binding label, ignores the missing snapshot annotation,
      and treats an unparsable linked count as 1;
    - a test-side change: L5 skips the simulated GC of the dependent.

    The full package runs 341/341 with `ginkgo -p --race` (109.8s spec time, 1m59s wall); lint, gofmt and goimports
    are clean.
- [x] **A-section P0s** (October 21, 2026): A1, A4, A5, A7 in the new `apply_integration_test.go` (4 contexts, 29
  specs).
  - A1 `client-side apply (manifest updates and changes made by third parties)`. A third-party change to a field the
    manifest specifies is reverted at the next periodic requeue, while added labels and data keys are kept. The
    updated manifest then removes a label, adds an annotation and changes a data value; the third-party additions
    survive the three-way merge.
  - A4 `client-side apply falling back to server-side apply (oversized objects)`: ~300 KiB of data leaves the
    last-applied annotation empty. The update is applied via SSA, which is asserted through `managedFields`: the
    changed field is owned only by `kubefleet-member-agent/Apply`.
  - A5 `server-side apply (field ownership conflicts)`: the takeover succeeds, but the non-forced SSA reports
    `FailedToApply` with `conflict with "integration-test-manager"` and leaves the data alone. With
    `ForceConflicts=true` the manifest is applied and KubeFleet owns the field.
  - A7 `namespace auto-creation (default)`: the namespace is created without owner refs or KubeFleet annotations, is
    not reported in the work or appliedWork status, and is left as is after the work is deleted.
  - Changes in `utils_integration_test.go`: `configMapObjectActual` now also compares non-KubeFleet annotations;
    `namespaceNotManagedActual` also fails on a deletion timestamp. New: `nonKubeFleetAnnotations`,
    `configMapObjectWithDataDigestActual` (digests keep failure output readable for large data),
    `objectAnnotationActual`, `fieldManagersActual` (`[MANAGER]/[OPERATION]` owners of a `managedFields` path), and
    the constant `thirdPartyFieldManagerName`.
  - Verified: the apply file runs 29/29. Four temporary mutations of `apply.go` were each caught at the expected spec:
    ignoring the last-applied config in the three-way merge fails A1; disabling the SSA fallback fails A4; always
    forcing conflicts fails A5; never creating namespaces fails A7. The full package runs 285/285 with
    `ginkgo -p --race` (83.6s spec time, 1m33s wall); lint, gofmt and goimports are clean.
- [x] **D-section P0s** (October 21, 2026): D1, D2, D4, D5, D6 in the new `drift_integration_test.go` (5 contexts,
  37 specs).
  - D1 `apply anyway (default), partial comparison`: the drift is overwritten at the next periodic requeue, with no
    work change; the label that only the member has is kept; no diff is reported.
  - D2 `report error, partial comparison (drift appears, changes, and is reverted)`. Deviation from the plan row:
    instead of waiting through ≥2 requeues, the test **changes** the drift (replicas 2 → 3). Unchanged statuses are
    never rewritten, so waiting would prove nothing; the change forces a real status rewrite while the object stays
    drifted, and the test checks that `FirstDiffedObservedTimestamp` survives it. It also asserts that the
    `Available` condition is removed while drifted, and that the manifest is applied again after the revert.
  - D4 `report error, partial comparison (manifest updated after a drift)`: a new image in the manifest changes its
    hash, so the apply goes ahead and overwrites the drifted replica count.
  - D5 `report error, full comparison` (ConfigMap; the namespace is created directly, see Behaviors 3).
  - D6 `apply anyway, full comparison (diffs reported after apply ops)`: `Applied=True` with `DiffDetails` for the
    label that only the member has, which persists; the Namespace manifest reports `/spec/finalizers` =
    `[kubernetes]` (server-populated).
  - New in `utils_integration_test.go`: `deploymentObjectActual` (`regularDeploymentObjectAppliedActual` is now a
    thin wrapper around it, comparing the same fields), `updateDeploymentInMemberCluster`,
    `updateConfigMapInMemberCluster` (retry on conflicts, return the updated object), and `objectLabelsActual`.
  - Verified: the drift file runs 37/37. Two batches of temporary product mutations were each caught at the expected
    spec:
    - (A) ignoring the manifest-hash gate fails D4; disabling post-apply detection fails D6; always resetting the
      first-diff timestamp fails D2;
    - (B) disabling pre-apply detection fails D2, D4 and D5.

    The full package runs 256/256 with `ginkgo -p --race` (78.8s spec time, 1m27s wall); lint, gofmt and goimports
    are clean.
- [x] **Review fixes** (October 22, 2026). Not yet re-run; pending verification.
  - T1 `pre-existing objects with the default sync strategy (report error, no takeover)`: the `AfterAll` now deletes
    the work itself (ignoring NotFound) and waits for it to be removed. Previously a failed spec skipped the delete
    spec, so the `AfterAll` timed out on the appliedWork and left the work behind on reconciler 1.
  - T9 `objects placed by another work object (duplicate placement)`: the `AfterAll` deletes both works A and B
    ignoring NotFound (work B might not exist yet if the test case fails early).
  - R3: the reset spec skips the first reconcile at or after the nudge (`skip: 1`). A periodic reconcile that starts
    right after `nudgedAt` might still read the pre-nudge generation from the cache, and the reconcile that the update
    triggers would then follow it immediately (a gap of ~0s). The checked gap is 2s either way (the fixed delay or the
    initial slow backoff delay), and 8s without the reset. Renamed to
    `should requeue after 2 seconds again, as the work object changes`.
  - L4: the `Consistently` check compares the UID of work B2's appliedWork (`objectNotRecreatedActual`) instead of
    only checking that it exists. Otherwise a cleanup that wrongly touched B2 could be masked: work B's next periodic
    reconcile adds the finalizer back and re-creates the appliedWork.
  - W1: renamed the spec to `should create the objects in the same wave`; the timestamp check shows that the objects
    share a wave, not that they are processed in parallel.
- [ ] P1 contexts.
- [ ] P2 contexts.
