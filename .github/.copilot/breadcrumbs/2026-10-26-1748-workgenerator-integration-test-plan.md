# Work Generator (v1) Integration Test Plan

**Date**: October 26, 2026 17:48 AEDT (revised 19:33 AEDT for the latest code changes, 21:41 AEDT for the
`Available` condition fix, and October 27, 02:58 AEDT for the `IncompleteSetOfResources` message)
**Task**: Plan (not yet implement) the tests for `pkg/v1/controllers/workgenerator`, based on the current code base,
including the uncommitted changes in [controller.go](../../../pkg/v1/controllers/workgenerator/controller.go),
[retrieval.go](../../../pkg/v1/controllers/workgenerator/retrieval.go),
[status.go](../../../pkg/v1/controllers/workgenerator/status.go),
[works.go](../../../pkg/v1/controllers/workgenerator/works.go), and
[placementbinding_types.go](../../../apis/kubefleet.dev/placement/v1alpha1/placementbinding_types.go):

- invalid placement resource snapshots are surfaced on the binding (`Synchronized=False/InvalidSnapshots`, plus
  `Available=False/IncompleteSetOfResources` when the binding still points to the last processed snapshot or nothing
  has been processed; after a rollout to a different snapshot the previous `Available` condition, necessarily from an
  older generation, is kept), with the resource counters recomputed from the works already placed;
- sync-strategy-only changes are patched onto the works without reading the snapshots (the new strategy-only path);
- the retrieval checks are reworked (duplicates, gaps, and missing snapshots are all `User` errors);
- the binding spec name fields require a minimum length of 1.

Cross-reference the existing tests, and identify the scenarios to add, update, and remove. The old work generator ITs
(`pkg/controllers/workgenerator`) are used as a reference only (see [Old tests → new plan](#old-tests--new-plan)).

**Status**: Implemented (October 26, 2026), apart from the deferred unit tests U1 and U6. All [Decisions](#decisions)
are resolved. Done: X5 and X6 (fixing the six existing specs that the code changes broke; see [Baseline](#baseline)),
the S, L, and R sections, unit tests U2–U5 and U7, the rate limiter option (Decision 8), and the CI wiring
(Decision 5).

## Requirements

1. Cover the new invalid-snapshot handling (the `InvalidSnapshots` and `IncompleteSetOfResources` reasons, duplicate,
   gap, and missing-snapshot detection, the recount of resources, the no-op write skip) for both scopes.
2. Cover the strategy-only path, and update the existing contexts that the code changes broke.
3. Fill the remaining behavior gaps found in the survey; remove or fold redundant contexts only with approval.
4. Follow repo conventions: Ginkgo/Gomega, `<area>_integration_test.go`, one `Ordered` `Context` per scenario
   (`BeforeAll`/`AfterAll`, teardown in `AfterAll`), random names, whole-struct `cmp.Diff`, `want` naming, the existing
   `*Actual` helper style, and both scopes through the `scopes` loop and `bindingFixture`
   ([lifecycle_integration_test.go](../../../pkg/v1/controllers/workgenerator/lifecycle_integration_test.go)).
5. Always run with `ginkgo -p` (see [Running the tests](#running-the-tests)).
6. Put new IT helpers in [utils_integration_test.go](../../../pkg/v1/controllers/workgenerator/utils_integration_test.go);
   scenario files hold only `Describe`/`Context` bodies.

## Baseline

```bash
KUBEBUILDER_ASSETS="$(make -s kubebuilder-assets-path | tail -1)" \
  go run github.com/onsi/ginkgo/v2/ginkgo -p --race ./pkg/v1/controllers/workgenerator
```

- Before the 19:33 changes (October 26, 2026): 452 of 452 specs pass; 74.6s of spec time, 1m25s wall.
- On the current code (October 26, 2026, after the 19:33 changes): 404 of 452 run; 398 pass, **6 fail**, 48 are
  skipped (the rest of the failing `Ordered` contexts); 75.3s of spec time, 1m27s wall. All six failures are expected
  consequences of the code changes:
  - `handle inconsistent resource snapshots`, both scopes, the first `should not update the … binding status or the
    work objects`: a missing secondary is now a `User` error, so the binding is marked `InvalidSnapshots` (X5).
  - `changing sync strategies`, 1 and 2 snapshots × both scopes, `should refresh the placement binding status`: the spec
    expects `WaitingForSync`, but the strategy-only path leaves the binding status as it was (X6).

## Controller behavior under test

Watches ([controller.go](../../../pkg/v1/controllers/workgenerator/controller.go)): `PlacementBinding` and
`ClusterPlacementBinding` with `GenerationChangedPredicate` (status-only updates are not events); `Work` create, delete,
and **status-changing** updates, mapped to the owner binding through the `placement.kubefleet.dev/owner-namespace` label
and the owned-by-binding annotation (events missing either are dropped and logged). Placement resource snapshots are
**not** watched.

Reconcile flow:

1. Get the binding (`ClusterPlacementBinding` if the request has no namespace); not found → done.
2. Deleting or suspended → `cleanupWorks` ([cleanup.go](../../../pkg/v1/controllers/workgenerator/cleanup.go)): no
   cleanup finalizer → done. Otherwise delete the **primary** work only (secondaries are left to K8s GC through their
   controller owner reference to the primary); if suspended and not deleting, unset
   `LastProcessedResourceSnapshotName`; remove the finalizer.
3. Add the cleanup finalizer (`placement.kubefleet.dev/work-generator-cleanup`).
4. Sanity check: an empty `clusterName` or `resourceSnapshotName` → `Unexpected` error. Unreachable through the API, as
   the CRD requires a minimum length of 1 for `placementPolicyName`, `clusterName`, and `resourceSnapshotName`.
5. List the works in `fleet-member-<cluster>` through the owner-binding field index
   (`<ownerNS>/<workOwnerLabelValue(bindingName)>`).
6. `areWorksInSync` ([works.go](../../../pkg/v1/controllers/workgenerator/works.go)) returns one of:
   - `ResourcesOutOfSync`: the last processed snapshot differs from the spec snapshot, there are no works, a work is not
     linked to the spec snapshot, or the number of works differs from the linked work count on the primary;
   - `StrategyOutOfSync`: all of the above hold, but some work's sync strategy differs from the binding's;
   - `InSync`: everything matches;
   - an `Unexpected` error: multiple works carry the linked work count, or it is invalid.

   Then:
   - `InSync` → `refreshPlacementBindingStatus` (step 12) → done.
   - `StrategyOutOfSync` → `refreshSyncStrategyOnlyForWorks`: for every work whose strategy differs, a merge patch of
     `spec.syncStrategy` only (optimistic lock on the cached copy), in parallel. The snapshots are not read, and the
     binding status is not touched: the conditions keep the previous generation and the counters their previous
     values. The patch bumps the works' generations, so the member agent re-applies them, and the resulting work status
     events lead to the in-sync refresh, which writes the conditions for the new generation. → done.
   - `ResourcesOutOfSync` → step 7.
7. Retrieve the snapshots ([retrieval.go](../../../pkg/v1/controllers/workgenerator/retrieval.go)), cached reads:
   - Get the primary (not found → an API server error that `IsNotFound`); parse the count label (non-integer or < 1 →
     `Unexpected`); count 1 → return the primary alone (no listing).
   - Otherwise list by the owned-by-annotation + index field index (missing annotation/label on the primary →
     `Unexpected`), and sort by sub-index (malformed label → `Unexpected`).
   - Check the sub-indices of the first `count + 1` snapshots: one equal to the previous one → `User` ("found placement
     resource snapshots with duplicate sub-indices…"), which also catches a stray right after the counted ones that
     duplicates the last of them; a gap among the first `count` → `User` ("found placement resource snapshots with
     inconsistent sub-indices…"). Snapshots further down are not checked.
   - Fewer snapshots than the count → `User` ("there are fewer placement resource snapshots than the count label
     indicates; some snapshots might have been deleted…").
   - A contents-hash mismatch among the first `count` → `Unexpected` (not surfaced on the binding).
   - Return the first `count`.
8. On a not-found or `User` error from step 7, `markPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots`
   ([status.go](../../../pkg/v1/controllers/workgenerator/status.go)):
   - `Synchronized=False/InvalidSnapshots` for the current generation, with the message `Failed to synchronize resources
     as the given placement resource snapshots are invalid: <err>`;
   - if the last processed snapshot is the spec snapshot (or nothing has been processed):
     `Available=False/IncompleteSetOfResources` for the current generation, with the message `Cannot check the
     availability of resources as the given placement resource snapshots are invalid; some resources might be missing
     from the target cluster`; otherwise (a rollout to a different snapshot) `Available` is left as is, which is safe as
     it can only carry an older generation (or be absent);
   - the counters and failed resources are recomputed from the works linked to the last processed snapshot (none →
     0/0/0 and no failed resources; failed resources capped at 50);
   - `LastProcessedResourceSnapshotName` is untouched; the write is skipped if the whole status is unchanged.

   Either way the error is returned, so the controller retries with backoff (the only re-trigger, as snapshots are not
   watched). A cache-lag "not found" right after a rollout is accepted; it self-heals on retry.
9. `refreshWorks`: build the primary work and one work per secondary snapshot (names from the policy's namespaced name,
   secondaries carry the sub-index; owner labels hashed when over 61 characters or dotted; annotations for the linked
   primary snapshot, `placement-resource-snapshot/<subIdx>`, owned-by policy/binding; manifests; sync strategy), set the
   linked work count on the primary, delete dangling works first, create/update the primary, set it as the controller
   owner of the secondaries, then create/update the secondaries in parallel. Reports whether any work was created or had
   its spec changed (metadata-only changes do not count).
10. `reportPlacementBindingProcessingProgress`: `LastProcessedResourceSnapshotName` = primary,
    `Synchronized=False/WaitingForSynchronization`, `Available=Unknown/WaitingForAvailabilityCheck`, `SelectedResources`
    = manifest total, the other counters and `FailedResources` cleared.
11. No spec change → requeue after 1s (so a same-content rollout refreshes the status without member re-processing);
    otherwise wait for work events.
12. `refreshPlacementBindingStatus`: skip (no write) if any work's `Applied`, then `Available`, condition is missing or
    stale; `Synchronized` = `True/AllResourcesSynchronized` or `False/FailedToSynchronizeSomeResources`; `Available` =
    `True/AllResourcesAvailable` or `False/SomeResourcesUnavailable`; counters from the manifest conditions; failed
    resources (`Applied≠True`, or applied with `Available≠True`) carry `DiffDetails` and the false condition, are sorted
    by group/version/kind/namespace/name, and are capped at `maxFailedResourcesToReport` (50); the write is skipped
    when nothing changed.

## What the existing tests already cover

20 contexts in [core_ops_integration_test.go](../../../pkg/v1/controllers/workgenerator/core_ops_integration_test.go)
(four `Describe`s: namespace-/cluster-scoped × single/multiple snapshots, written out per scope) and 22 in
[lifecycle_integration_test.go](../../../pkg/v1/controllers/workgenerator/lifecycle_integration_test.go) (11 per scope,
through `scopes`).

| File / Context | Behaviors |
| --- | --- |
| core: single, `generate works` | Finalizer, exact primary work, `WaitingForSync` → mark → synced; cleanup on delete |
| core: single, `update works` | Rollout to a new snapshot → work updated; statuses |
| core: single, `refresh binding status upon work status changes` | In-sync path: a synced binding turns `Available=False/SomeResourcesUnavailable` with failed resources |
| core: single & multiple, `update works (no spec changes)` | Same-content rollout: metadata-only re-link in place; status refreshed through the 1s requeue without member re-processing |
| core: single & multiple, `backport status for failed manifests` | Failed-to-apply (with `DiffDetails`) and unavailable manifests → `FailedResources`, counters, `FailedToSynchronizeSomeResources`; multiple works merged |
| core: multiple, `generate works` | Primary + secondaries, owner refs, linked count; status not refreshed until every work is processed |
| core: multiple, `update works` | Scale down (dangling work deleted), in-place updates (same UIDs), scale up (created) |
| core: multiple, `handle inconsistent resource snapshots` | Rewritten (X5, done): missing secondaries → `InvalidSnapshots` ("fewer…"), the pre-rollout `Available` condition kept, counters from the last processed works, works unchanged; a hash mismatch afterwards leaves the status (and message) as is; recovery through retries once the secondaries appear |
| lifecycle: `deleting placement bindings` | Single (another binding on member-2 unaffected); multiple (primary deleted, secondaries left to GC) |
| lifecycle: `suspending placement bindings` | Suspend/resume (works removed, last processed snapshot unset, finalizer removed, no works while suspended, a rollout made while suspended applied on resume); unprocessed works; delete while suspended |
| lifecycle: `changing sync strategies` | Updated (X6, done): set/unset with 1 and 2 snapshots → works patched in place; the binding status left untouched (previous generation) until the works are marked, then synced |
| lifecycle: `handling work object events` | Foreign works (3 kinds) ignored; out-of-band deletion of the primary, a secondary, or the only work → re-created, others untouched |
| unit: [works_test.go](../../../pkg/v1/controllers/workgenerator/works_test.go) | `workOwnerLabelValue` (5 + distinctness); `areWorksInSync` (15 cases, every branch; updated for `ResourcesOutOfSync` / `StrategyOutOfSync`) |
| unit: [uniquename_test.go](../../../pkg/v1/controllers/workgenerator/uniquename_test.go) | Work names (12 cases), uniqueness, the derived-from formatter |

**Not covered at all**: the invalid-snapshot marking (not found, duplicates, gaps, missing snapshots), the
`IncompleteSetOfResources` reason, the recount of resources in the invalid state, the no-op write skip, duplicates
further down the listing being ignored, invalid snapshots on new bindings / with out-of-band work deletion / with
suspension, the strategy-only path with missing snapshots and combined with a rollout, long and dotted names end to
end, one policy on multiple clusters, secondary works lingering across binding incarnations, a missing reserved
namespace, `Applied`-without-`Available` gating, status-write skipping in the in-sync refresh, work metadata tampering,
the 50-entry cap, and the failed-resource **order** (the status helpers sort before comparing).

## Test environment constraints and design implications

1. **No GC controller.** Deleting a binding (or its primary work) leaves the secondary works behind; `teardown` deletes
   them explicitly. L3 relies on this to model GC lag.
2. **Snapshots are not watched.** Recovery from an invalid snapshot set (other than through a binding spec change)
   relies on error retries. In production these follow controller-runtime's default limiter (per item, 5ms doubling
   up to 1000s, reset only on success), so the next retry comes after roughly as long as the binding has been failing.
   The suite instead runs the controller with a capped limiter (Decision 8; [Suite changes](#suite-changes) item 2), so
   a retry is at most ~1s away and recovery fits in `eventuallyDuration`. Retries also run about once a second during
   every `Consistently` window, which makes the "status unchanged while retrying" checks meaningful.
3. **Shared cache.** The tests and the controller share the manager's cached client, so a binding created right after
   its snapshot can briefly observe "not found" and get `InvalidSnapshots` before self-healing (B1). `Eventually`
   tolerates it; do not start a `Consistently` on a binding's status right after creating it.
4. **Condition messages are ignored** by `placementBindingStatusUpdatedActual` / `placementBindingStatusMatchesActual`
   (`ignoreFieldConditionLTTMsg`); the invalid-snapshot message needs its own check.
5. **Mixed generations.** Two cases leave conditions at different generations: the strategy-only path (every condition
   keeps the previous generation until the works are re-processed), and an invalid mark after a rollout to a different
   snapshot (`Available` kept at the pre-rollout generation). `placementBindingStatusUpdatedActual` stamps every
   condition with the current generation, so these need `placementBindingStatusMatchesActual` with explicit
   generations.
6. **Strategy patches bump work generations.** `markWorkAsAppliedAndAvailable` stamps the work's current generation, so
   marking the works after a strategy patch is what lets the in-sync refresh run (X6, S7).
7. **Snapshot specs are immutable** (CEL); "re-create" means delete, wait until gone, create again.
8. **Label selectors in helpers use raw binding names** (`worksGeneratedActual`, `listWorkUIDs`); they are wrong for
   names that get hashed (over 61 characters or dotted). L2 needs them to use `workOwnerLabelValue`.
9. **Namespaces cannot be deleted in envtest.** L4 uses a fresh random cluster name and creates its reserved namespace
   mid-test.
10. **The CRD rejects empty strings** for `placementPolicyName`, `clusterName`, and `resourceSnapshotName`
    (`minLength: 1`), so step 4's sanity check cannot be reached in envtest.

## Suite changes

1. **CI wiring** (Decision 5, implemented October 26, 2026). Previously the package ran under `go test` in
   `make local-unit-test` (no `-p`), and the ~30 new contexts make a serial run noticeably slower. It follows the
   `stagedupdaterun` / v1 work applier pattern:
   - a `TestMain` in [suite_test.go](../../../pkg/v1/controllers/workgenerator/suite_test.go) that exits early when
     `KUBEFLEET_CI_TEST_RUNNER_NAME=default` (the `make test` step), and runs the suite otherwise;
   - a dedicated step in `.github/workflows/ci.yml`, after the v1 work applier step:
     `ginkgo -v -p --procs=4 --race --cover -coverprofile=work-generator-v1-it-coverage.out
     ./pkg/v1/controllers/workgenerator/`, with `KUBEFLEET_CI_TEST_RUNNER_NAME: 'ginkgo'` (the Codecov step picks up
     the profile, as it searches for all coverage files).

   The unit tests in the package then run in the Ginkgo step as well, as `TestMain` gates the whole test binary.
2. **Rate limiter option** (Decision 8, implemented October 26, 2026; a small production change).
   - Product: `New` in [controller.go](../../../pkg/v1/controllers/workgenerator/controller.go) takes an extra
     `workqueue.TypedRateLimiter[reconcile.Request]` argument, stored on the `Reconciler` and passed to
     `controller.Options.RateLimiter` in `SetupWithManager`. Nil keeps controller-runtime's default (with the default
     priority queue, an exponential 5ms → 1000s limiter), following the v1 work applier's constructor, which also takes
     optional limiters with nil meaning the default. The only production caller,
     [setup.go](../../../cmd/hubagent/workload/setup.go), passes nil, so production behavior does not change; the unit
     tests pass nil too.
   - Suite: [suite_test.go](../../../pkg/v1/controllers/workgenerator/suite_test.go) passes
     `workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](requeueBaseDelay, requeueMaxDelay)` (5ms,
     1s), with a comment explaining why (no snapshot or namespace watch; recovery depends on retries).
   - Existing tests: every `eventuallyDurationForBackoffRetries` (the two `handle inconsistent resource snapshots`
     contexts, the S section, and L4) is now `eventuallyDuration`, and the constant is deleted.

## Shared test utilities

The helpers live in [utils_integration_test.go](../../../pkg/v1/controllers/workgenerator/utils_integration_test.go).

- **Done (October 26, 2026):** moved `scope`, `scopes`, `snapshotSet`, and `bindingFixture` here from
  `lifecycle_integration_test.go` (Requirement 6; Decision 2).
- **Done (October 26, 2026), used by X5/X6 and the S section:**
  - `snapshotSet` now records its index and resources; `newSnapshotSet(index, count)` prepares a set without creating
    it, and `createSnapshotsInSet(set, subIdxs...)` creates (or re-creates) some or all of its snapshots in descending
    sub-index order; `createSnapshots(index, count)` does both. Together they cover gaps, late snapshots, and missing
    snapshots (the plan's former `createSnapshotsWithSubIdxs`).
  - `deleteSnapshot(setIdx, subIdx)`; `createStraySnapshot(index, subIdx, count, contentsHash)` /
    `deleteStraySnapshot(name)`: extra snapshots under `%s-snapshot-%d-%d-stray-%s`, cleaned up by `teardown`.
  - `recordStatus()` (status and resource version), `statusUnchangedActual()`, `resourceVersionUnchangedActual()`,
    `recordWorkUIDs()` + `expectWorksNotRecreated()`. `registerBaselineSpecs` records the status once the baseline
    is synced.
  - `invalidSnapshotsStatusActual(wantStatus, wantMsgSubstr)`: the whole status (conditions without an explicit
    observed generation are stamped with the current one; explicit ones are kept) plus the message prefix and details;
    `wantPlacementBindingStatusInvalidSnapshots(placed, lastProcessed, previousStatus)` builds the common expected
    status: with a previous status (a rollout to a different snapshot), its `Available` condition (generation
    included) is kept; with nil, `Available=False/IncompleteSetOfResources` is expected. It is adjusted in place for
    the other cases (no last processed snapshot, failed resources).
  - `worksMatchBindingStrategyActual(setIdx)`: works carry the binding's sync strategy as written to the API server.
  - `updateSpec(mutate)` / `updatePlacementBindingSpec(ns, name, mutate)`: several spec fields in one update (S7b).
  - `placementBindingSyncCondMessageActual(ns, name, wantSubstr)`.
  - Constants: `invalidSnapshotsMsgPrefix`, `fewerSnapshotsErrMsg`, `duplicateSubIdxErrMsg`, `inconsistentSubIdxErrMsg`,
    `snapshotNotFoundErrMsgFmt` (`%q not found`), `straySnapshotNameTemplate`.
- **Done (October 26, 2026), used by the L and R sections:**
  - `newBindingFixtureWithNames(sc, clusterName, bindingName, policyName)`; `f.newFixtureOfSamePolicy(clusterName)`:
    another binding of the same policy that shares the fixture's snapshot sets (L1); `longDottedName(prefix, length)`
    (L2).
  - `f.createSnapshotWithResources(index, resources, identifiers)` (R4); `snapshotSet.resourceCount()` now sums the
    identifiers instead of assuming three resources per snapshot.
  - `f.registerWorkGenerationSpecs(setIdx)`: the first half of the baseline (up to `WaitingForSync`), which
    `registerBaselineSpecs` now builds on (R1, R4).
  - `markWorkAsApplied` / `f.markWorksAsApplied` (R1); `touchWorkStatus` (R2, R3); `setWorkAnnotation`,
    `workAnnotationActual`, `f.workGeneration`, `f.syncCondTransitionedSinceRecordActual` (R3);
    `createReservedNamespace` (L4); `placementBindingStatusInOrderActual` (R4: failed resources compared in order).
  - `worksGeneratedActual`, `listWorkUIDs`, and `wantWorkForPlacementResourceSnapshot` use
    `workOwnerLabelValue(...)` for the owner labels; the snapshot helper's owner label uses `snapshotOwnerLabelValue`,
    which mirrors the snapshot manager's truncation (names over 61 characters), so that long policy names are valid
    label values. No change for the short names used elsewhere.
  - Constants: `paddedConfigMapNameTemplate`, `markedAsAppliedManifestMsg`, `markedAsAppliedWorkMsg`.

## Test plan

Priorities: **P0** = the new behavior, and the fixes for the broken existing contexts; **P1** = important edge/safety
behavior; **P2** = nice to have. Every row runs for both scopes unless it says otherwise; every context tears down in
`AfterAll`.

### S. `invalid_snapshots_integration_test.go`: invalid placement resource snapshots (new)

| ID | P | Context | Setup → steps | Assert |
| --- | --- | --- | --- | --- |
| S1 | P0 | roll out to a missing snapshot | Baseline on set 0 (1 snapshot) synced; record status; point the binding to set 1's primary name before set 1 exists; mark the Deployment in set 0's work unavailable; then create set 1 (1 snapshot) | `Synchronized=False/InvalidSnapshots` at the new generation, and the baseline `Available` condition kept (previous generation), message = prefix + the not-found error naming the snapshot; counters 3/3/3 recomputed from set 0's works; last processed still set 0; works unchanged and binding resource version stable over `consistentlyDuration` while the controller retries (no-op skip). After the Deployment turns unavailable: counters 3/3/2 with the Deployment in `FailedResources`, conditions unchanged (the recount follows the works). After set 1 appears: primary work updated in place (same UID), `WaitingForSync(set 1)` → mark → synced |
| S2 | P0 | duplicate of the last counted sub-index; the message follows the cause | Baseline on set 0 (2 snapshots); point to set 1's primary (missing); create set 1 (2 snapshots) plus a stray at sub-index 1 with the **same** contents hash (the case the hash check cannot catch); delete the stray | Not-found message first; then the message changes to the duplicate error (one more status write, then stable); works still from set 0 (`Consistently`); after deleting the stray: works updated in place to set 1, `WaitingForSync` → mark → synced. Variant (P2): the stray has a different hash |
| S3 | P1 | duplicates further down the listing are ignored | Baseline on set 0 (1); create set 1 (count 2) plus two extras at sub-index 2 (sorted after the snapshot right after the counted ones); roll out | No `InvalidSnapshots`; exactly 2 works (no work for sub-index 2); `WaitingForSync` → mark → synced. Pins the `count + 1` limit (B11) |
| S4 | P1 | duplicate primary at the same index | Baseline on set 0 (1); create set 1 (count 2) plus a stray sub-index 0 at index 1; roll out; delete the stray | Duplicate message; recovers to set 1 |
| S5 | P1 | gap in the counted sub-indices | Baseline on set 0 (1); create set 1 with count 3 and sub-indices 0, 2, 3; roll out; then create sub-index 1 | `inconsistent sub-indices` message; after the fix: 3 works (sub-indices 0–2), sub-index 3 ignored as an extra. Gaps do not occur in normal ops; this pins the sanity check |
| S6 | P1 | new binding with a missing snapshot | Create the binding pointing to a nonexistent primary; then create the snapshot | Finalizer added; `Synchronized=False/InvalidSnapshots` and `Available=False/IncompleteSetOfResources` at generation 1; counters 0/0/0, no failed resources, no last processed; no works (`Consistently`); after the snapshot appears: works generated, `WaitingForSync` |
| S7 | P1 | sync strategy change while the processed snapshot is missing (strategy-only path) | Baseline on set 0 (2); delete set 0's primary (premature GC); set a sync strategy; mark the works | Works patched in place with the new strategy (same UIDs, manifests and links unchanged) without reading the snapshots; no `InvalidSnapshots`; the binding status unchanged (conditions at the previous generation, counters 6/6/6; `Consistently`); after marking: synced at the new generation. Pins the resolution of B5 (Decision 7) |
| S7b | P2 | sync strategy change together with a rollout to a missing snapshot | Baseline on set 0 (1); in one update, set a sync strategy and point to set 1 (missing); then create set 1 | Resource path: `InvalidSnapshots` at the new generation, the baseline `Available` condition kept (previous generation); works keep the old (nil) strategy (`Consistently`); after set 1 appears: works updated with set 1 and the new strategy, `WaitingForSync` → mark → synced |
| S8 | P2 | works deleted out of band while the processed snapshot is invalid | Baseline on set 0 (2); delete set 0's primary; delete the secondary work; re-create the primary | `InvalidSnapshots` and `Available=False/IncompleteSetOfResources` at the **same** generation (B4); counters recomputed from the remaining primary work (3/3/3); the secondary work not re-created (`Consistently`); after the re-creation: re-created, `WaitingForSync` → mark → synced |
| S9 | P2 | suspend, resume, and delete while invalid | Binding in S1's invalid state; suspend; resume; delete | Suspend: works removed, last processed unset, finalizer removed, conditions left from before (older generation). Resume: finalizer back, `InvalidSnapshots` + `IncompleteSetOfResources` at the new generation, counters 0/0/0, no works. Delete: binding removed (cleanup does not read snapshots) |

Missing secondaries ("fewer than count") and a hash mismatch after an invalid mark are covered by the rewritten
`handle inconsistent resource snapshots` contexts (X5).

### L. `lifecycle_integration_test.go`: topology, naming, and lifecycle gaps

| ID | P | Context | Setup → steps | Assert |
| --- | --- | --- | --- | --- |
| L1 | P1 | one policy on two clusters | Two bindings of the same policy (member-1, member-2) on the same set (2 snapshots); mark works on member-1 only; roll out set 1 to binding 1 only; delete binding 1 | Same work names in both reserved namespaces; only binding 1 synced after marking; binding 2's works and status untouched by binding 1's rollout and deletion |
| L2 | P1 | long and dotted names | Policy and binding names over 61 characters with dots; 2 snapshots; baseline; rollout; delete | Work names truncated/dot-free as `uniqueNameForWorkDerivedFromPlacementResourceSnapshot` computes; owner labels hashed; the in-sync path works (listing through the field index, event mapping through the annotation); rollout in place; primary work removed on delete |
| L3 | P2 | binding re-created while old secondary works linger | Baseline on set 0 (3 snapshots); delete the binding (secondaries linger, no GC); re-create it (same name) on set 1 (2 snapshots) | Sub-index 1 work updated in place (same UID) with the new primary as controller owner; sub-index 2 work deleted as dangling; linked count 2; synced after marking |
| L4 | P2 | reserved namespace of the target cluster missing | Binding to `member-<rand>` with no reserved namespace; then create `fleet-member-<rand>` (one scope is enough) | Finalizer added; no works and no status (`Consistently`); after the namespace appears: works generated (within `eventuallyDuration`, thanks to the suite's capped rate limiter, as the work generator does not watch namespaces), `WaitingForSync` → mark → synced |
| L5 | — | ~~empty `resourceSnapshotName` / `clusterName`~~ | Dropped (October 26, 2026): the CRD now rejects empty values (Decision 7) | — |

### R. `status_integration_test.go`: status refresh gaps

| ID | P | Context | Setup → steps | Assert |
| --- | --- | --- | --- | --- |
| R1 | P2 | applied, availability not yet reported | Baseline up to `WaitingForSync`; mark works `Applied` only; then mark them available | Status stays `WaitingForSync` (`Consistently`); synced after the availability update |
| R2 | P2 | no-op refresh skips the write | Synced binding; update a work's status without changing the outcome (e.g., a manifest condition message) | Binding resource version unchanged (`Consistently`). Note: this pins the observable behavior only; the API server also treats an identical status update as a no-op (no resource version bump), so the test cannot tell the client-side skip apart (verified by a mutation run, October 26, 2026). U4 covers the skip itself (its "status unchanged" case fails when the skip is removed) |
| R3 | P2 | work metadata tampered | Synced binding; change the primary work's linked-snapshot annotation; later touch the work's status | Not reconciled while only metadata changes (`Consistently`; B7); after the status touch: re-linked in place, status goes `WaitingForSync` then synced (1s requeue), no spec change |
| R4 | P2 | more than 50 failed resources | One snapshot with 51 ConfigMaps (one scope); mark all failed to apply | `FailedResources` has 50 entries in the documented order (compare **without** `SortSlices`); counters 51/0/0. Could be U4 instead |

### Existing tests: update, keep, remove, or fold

| ID | Existing | Proposal | Reason |
| --- | --- | --- | --- |
| X5 | core `handle inconsistent resource snapshots`, both scopes | **Rewrite (P0; done October 26, 2026)** | Index 1 without its secondary: now `InvalidSnapshots` with the "fewer … deleted" message, the pre-rollout `Available` condition kept (its generation included), counters 3/3/3 from index 0's works, works unchanged. Index 1's secondary with a mismatched hash: `Unexpected`, not surfaced, so the status stays exactly as in the previous step (the "fewer" message remains though the cause changed; B2). Index 2 without its secondaries: `InvalidSnapshots` again, at the newer generation. Recovery once the secondaries appear: unchanged; now within `eventuallyDuration` (Decision 8; it took ~15s per scope on the 1-minute budget before) |
| X6 | lifecycle `changing sync strategies`, 1 and 2 snapshots × both scopes | **Update (P0; done October 26, 2026)** | After setting (and after unsetting) the strategy: works patched in place (already asserted); the binding status stays as recorded before the change, with conditions at the previous generation (`Consistently`), instead of `WaitingForSync`; after marking the works: synced at the new generation |
| X1 | core `generate works` (single snapshot), both scopes | Keep (Decision 3) | Overlaps `registerBaselineSpecs` in the single-snapshot lifecycle contexts, but kept |
| X2 | core cluster-scoped `Describe`s (10 contexts, ~2,100 lines) | Keep as is (Decision 4) | They mirror the namespace-scoped ones; folding them into the `scopes` loop is not planned |
| X3 | core `refresh binding status upon work status changes` | Keep | Only test of a synced → unavailable transition through the in-sync path (backport starts from `WaitingForSync`) |
| X4 | core single `update works (no spec changes)` | Keep | Exercises the count-1 fast path; the multiple-snapshot one exercises the listing |

Apart from X5 and X6, no existing test is obsolete.

### Unit tests (Decision 6)

U2–U5 and U7 are implemented (October 26, 2026); U1 and U6 are deferred. The unit tests use the controller-runtime
fake client, with interceptors that count status updates and patches (and inject errors), and share builders in
`status_test.go` (`newCondition`, `newManifestStatus`, `newWorkWithStatus`, `workConds`, `newFailedResource`,
`newBindingWithStatus`, `newUnitTestHubClient`, `countStatusUpdates`, `failedToApplyWorkWithManyManifests`). They also
reuse a few pure helpers from `utils_integration_test.go` (the manifest identifier builders, `objectRefFromManifestIdentifier`,
`paddedConfigMapNameTemplate`), as both are compiled into the same test binary.

- **U1 `retrievePrimaryAndSecondaryPlacementResourceSnapshots`** (deferred): with a fake client and the field index:
  count label invalid / < 1, missing owned-by annotation or index label, malformed sub-index label (sort errors), fewer
  than count, duplicates (last counted, right after the counted ones, further down (ignored), primary), gap, hash
  mismatch, extras trimmed, ordering. Needs the `fieldindexers` extractors exported (or a test-only index setup).
- **U2 `TestMarkPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots`** (`status_test.go`, 8 cases, done): a
  rollout to an invalid snapshot (the previous `Available` condition kept at its generation, counters only from the
  works linked to the last processed snapshot); the same with no previous `Available` condition (none added); nothing
  processed yet (`Available=False/IncompleteSetOfResources`, 0/0/0, works ignored); the processed snapshot itself
  invalid (`Available=False/IncompleteSetOfResources` at the same generation); the 50-entry cap; already marked with the same details (no write) or different details (write); a
  failed update (API server error).
- **U3** (`status_test.go`, done): `TestCountResourcesInWorksByProcessingResults` (5 cases: no works, unprocessed
  manifests, applied with and without availability results, failures incl. `DiffDetails` and an ignored `Available`
  condition on a not-applied manifest, the sort order across works by group/version/kind/namespace/name),
  `TestFailedResourceFromManifestStatus` (2), `TestRefreshPlacementBindingSyncCond` (4), and
  `TestRefreshPlacementBindingAvailableCond` (4) (missing or stale work conditions → not ready, conditions untouched).
- **U4 `TestRefreshPlacementBindingStatus`** (`status_test.go`, 8 cases, done): not ready (no `Applied`, stale
  `Applied`, no `Available`) → no write; all synchronized; some failed (sorted, with counters); **status unchanged → no
  write** (the client-side skip that R2 cannot tell apart); the 50-entry cap; a failed update.
- **U5** (`works_test.go`, done): `TestBuildWorkObjectFor` (3 cases: a primary snapshot with a namespace-scoped binding
  and a sync strategy (copied, not shared); a secondary snapshot with a cluster-scoped binding with long dotted names
  (hashed labels, full names in the annotations); a missing sub-index label → `Unexpected` error) and
  `TestUpdateWorkObjectMetadataAndSpec` (2 cases: a bare work object; an existing one, whose owned labels/annotations,
  manifests, and sync strategy are overwritten while other labels/annotations are kept).
- **U6 work event handler** (deferred): nil objects, failed casts, missing label/annotation, status-only update
  filter. Needs the handler funcs pulled out of `SetupWithManager`.
- **U7 `TestRefreshSyncStrategyOnlyForWorks`** (`works_test.go`, 4 cases, done): only the works whose strategy differs
  are patched (set and unset), and only `spec.syncStrategy` changes; nothing patched when all are in sync; a stale
  cached copy → conflict error (API server category), thanks to the optimistic lock.
- Unreachable today: the empty-name sanity check in `Reconcile` (the CRD rejects empty names), the
  duplicate-work-name check in `refreshWorks` (duplicate sub-indices are rejected first), the `Atoi` error inside the
  sub-index loop (the sort has already parsed every label), and non-not-found binding `Get` errors.

## Old tests → new plan

| Old area (`pkg/controllers/workgenerator`) | New plan |
| --- | --- |
| Bound binding with a single snapshot; manifests not applied → available | Covered (core single) |
| Multiple resource snapshots | Covered (core multiple) |
| Namespaced / cluster-scoped envelope objects; envelope override snapshots | Dropped (no envelopes in the v1 work generator) |
| Valid / invalid / invalid resource / not found overrides | Dropped (no overrides in the v1 work generator) |
| Should not touch/reset `RolloutStarted` | Dropped (no `RolloutStarted` on v1 bindings) |
| Bound binding with a not found cluster | No direct analog (the v1 work generator does not read `MemberCluster`); nearest is L4 |
| Sync apply strategy changes, valid snapshots | Covered (lifecycle `changing sync strategies`, after X6) |
| Sync apply strategy changes, **missing** snapshots | → S7 (the strategy-only path patches the strategy without the snapshots, as the old work generator did) |
| Apply ↔ `ReportDiff`; status under different apply strategies | Dropped (no `ReportDiff` in `v1alpha1`); diff backport is covered by `backport status for failed manifests` |
| `ResourcePlacement` lifecycle | Covered (namespace-scoped contexts) |

## Behaviors to note (documented only; tests assert current behavior)

1. **Cache lag can briefly mark a binding `InvalidSnapshots`** ("not found") right after its snapshot was created; it
   self-heals on retry (accepted; noted in the code).
2. **Only contents-hash mismatches are `Unexpected`** and stay off the binding; not found, duplicates, gaps, and missing
   snapshots ("fewer than count") are all marked `InvalidSnapshots`. An `Unexpected` error that follows an invalid mark
   leaves the earlier message in place (X5).
3. **In the invalid state**, `Synchronized=False/InvalidSnapshots`; `Available=False/IncompleteSetOfResources` when the
   binding still points to the last processed snapshot (or none was processed); after a rollout to a different
   snapshot, `Available` is left as is, which is safe as it then necessarily carries an older generation (or is
   absent); the counters and failed resources are recomputed from the works linked to the last processed snapshot
   on every reconcile, so they follow work status changes (S1); `LastProcessedResourceSnapshotName` is untouched.
4. **Same-generation re-read failures** (works deleted out of band after the processed snapshot became invalid) set
   `Available=False/IncompleteSetOfResources` at the same generation (S8). Fixed October 26, 2026: `Available` used to
   be left as is here, so it could read `True` for the same generation next to `Synchronized=False/InvalidSnapshots`.
5. **Strategy-only changes do not need the snapshots and do not reset the binding status** (resolved October 26,
   2026). Until the works are re-processed, the conditions keep the previous generation and the counters their previous
   values (X6, S7); consumers that check the condition generation (staged update runs, the policy status) see the
   binding as not synchronized for the new generation meanwhile. A strategy change made together with a resource
   change goes through the resource path and needs valid snapshots (S7b).
6. **Recovery without a binding event depends on backoff**: after many failures the next retry can be minutes away (up
   to 1000s with the default rate limiter); a binding spec change (e.g., a rollout) is picked up immediately.
7. **Work metadata changes are not events**: tampered labels/annotations are corrected only at the next work status
   change or binding event (R3).
8. **Empty `placementPolicyName` / `clusterName` / `resourceSnapshotName` are rejected by the CRD** (`minLength: 1`,
   October 26, 2026). No KubeFleet code writes empty values: bindings are created only by the placement policy
   controller (a member cluster name and the latest snapshot name; creations are held back while there is no
   snapshot), and the snapshot name is changed only by auto-rollout and staged update runs (always the name of a
   retrieved snapshot).
9. **Deleting a binding deletes only the primary work**; secondaries rely on K8s GC through their owner reference. A
   binding re-created before GC runs adopts or deletes them (L3).
10. **A primary whose count label is 1 is never listed**, so duplicates or extras at its index are never checked.
11. **Only the first `count + 1` snapshots are checked**: duplicates further down the listing are ignored (S3).

## Decisions

1. **Resolved (October 26, 2026).** New invalid-snapshot tests go in a new `invalid_snapshots_integration_test.go`,
   run for both scopes with `bindingFixture`. Status-refresh gaps go in a new `status_integration_test.go`; L rows join
   `lifecycle_integration_test.go`.
2. **Accepted (October 26, 2026).** Move `scope`/`scopes`/`snapshotSet`/`bindingFixture` into
   `utils_integration_test.go` (Requirement 6).
3. **Rejected (October 26, 2026).** X1 stays.
4. **Skipped (October 26, 2026).** X2 is not folded into the `scopes` loop.
5. **Accepted (October 26, 2026).** Add a `TestMain` gate and a dedicated `ginkgo -p` CI step for the package (see
   [Suite changes](#suite-changes)).
6. **Partially implemented (October 26, 2026).** Unit tests U2–U5 and U7 are added; U1 and U6 (which need small
   production refactors first) are left alone for now.
7. **Resolved (October 26, 2026)** by code changes:
   - B8, empty spec fields: `+kubebuilder:validation:MinLength=1` on `placementPolicyName`, `clusterName`, and
     `resourceSnapshotName` in
     [placementbinding_types.go](../../../apis/kubefleet.dev/placement/v1alpha1/placementbinding_types.go); CRDs
     regenerated (`make manifests`). No API validation tests for now (by request). L5 is dropped.
   - B5, strategy changes blocked by invalid snapshots: the strategy-only path (19:33 changes) patches the strategy
     without reading the snapshots. S7 pins it, S7b covers the combined case, and X6 updates the existing contexts.
8. **Resolved (October 26, 2026): option (b).** Background: the 1-minute timeout was the test-side constant
   `eventuallyDurationForBackoffRetries`, an `Eventually` budget used only by the two `handle inconsistent resource
   snapshots` contexts. The controller does not watch snapshots and only notices a fix at its next error retry, which,
   with the default limiter, comes after roughly as long as the binding has been failing. Options considered:
   - (a) keep the 1-minute budget with short invalid windows (slow: 30–60s per S context; flake risk under load);
   - **(b) add an optional rate limiter to `New` (nil = the default) and use a capped one in the suite** (chosen; see
     [Suite changes](#suite-changes) item 2);
   - (c) nudge the controller with work status updates (does not apply when a binding has no works; no longer tests
     unassisted recovery).

## Running the tests

Always use the Ginkgo CLI in parallel mode, via `go run` so the CLI matches the Ginkgo version pinned in `go.mod`
(v2.33.0):

```bash
KUBEBUILDER_ASSETS="$(make -s kubebuilder-assets-path | tail -1)" \
  go run github.com/onsi/ginkgo/v2/ginkgo -p --race ./pkg/v1/controllers/workgenerator
```

Use `--focus="<Describe/Context text>"` for a subset and `--focus-file=<file>_integration_test.go` for one file.
`KUBEBUILDER_ASSETS` must be an absolute path; the make target prints one, which can contain spaces on macOS
(`~/Library/Application Support/...`), so keep the quotes.

## Estimated runtime

Rough, per context (serial time), with the suite's capped limiter: S contexts 15–25s each (dominated by baselines and
`Consistently` windows; recovery takes about a second), 20 contexts ≈ 7 min; L1/L2 ~20s each × 2 scopes; L3 ~20s × 2;
L4 ~15s; R1–R3 ~15s × 2; R4 ~15s. About 34 new contexts (S: 10 × 2; L: 3 × 2 + 1; R: 3 × 2 + 1) ≈ 11–13 min of
serial time, mostly waiting; with `-p` this is
~2–3 min wall (vs. 1m25s today). X5 and X6 add a few `Consistently` windows; the X5 contexts also get faster with the
capped limiter.

## Status

- [x] Surveyed the package source, the existing ITs and unit tests, and the old ITs (October 26, 2026).
- [x] Baseline run before the 19:33 changes: 452/452 pass with `ginkgo -p --race` (74.6s spec time, 1m25s wall).
- [x] Re-baselined on the 19:33 changes: 398 pass, 6 fail (X5 × 2, X6 × 4), 48 skipped; plan revised accordingly
  (October 26, 2026).
- [x] Decisions 1–8 resolved (October 26, 2026).
- [x] `MinLength=1` on the three binding spec name fields; CRDs regenerated (Decision 7).
- [x] Fixed the broken existing contexts (X5, X6) (October 26, 2026). New helpers in `utils_integration_test.go`:
  `wantPlacementBindingStatusInvalidSnapshots`, `placementBindingSyncCondMessageActual`, and the constant
  `fewerSnapshotsErrMsg`; new `bindingFixture` methods `recordStatus` / `statusUnchangedActual` (the fixture still
  lives in `lifecycle_integration_test.go` until Decision 2 moves it). Verified: `ginkgo -p --race` passes 464/464
  (105.4s spec time, 1m56s wall; +12 specs); the X5 recovery takes ~15s per scope, within the 1-minute budget.
- [x] **CI wiring** (Decision 5) (October 26, 2026): `TestMain` gate in `suite_test.go` (verified: with
  `KUBEFLEET_CI_TEST_RUNNER_NAME=default`, `go test` exits right away without running the suite) and the step `Run
  work generator (v1) unit and integration tests with Ginkgo CLI & generate coverage` in `.github/workflows/ci.yml`,
  after the v1 work applier step.
- [x] **Rate limiter option** (Decision 8) (October 26, 2026): `New(hubClient, workerCnt, rateLimiter)`; the hub agent
  and the unit tests pass nil; the suite passes a 5ms → 1s exponential limiter; all `eventuallyDurationForBackoffRetries`
  uses are now `eventuallyDuration`, and the constant is gone. Verified: `go vet` on the package and on
  `cmd/hubagent/workload`; `ginkgo -p --race` passes 930/930 (163.8s spec time, 2m58s wall); `golangci-lint` reports
  no new issues.
- [x] Shared utilities for X5/X6 and the S section, including the fixture move (Decision 2) (October 26, 2026); the L/R
  helpers are pending (see [Shared test utilities](#shared-test-utilities)).
- [x] **S section, all rows** (October 26, 2026): `invalid_snapshots_integration_test.go`, `Describe("handling invalid
  placement resource snapshots")`, 11 contexts per scope (S1; S2 with the same and a different stray hash; S3–S9,
  S7b), 304 specs. Steps that depend on a retry after a snapshot change (not a binding or work event) used
  `eventuallyDurationForBackoffRetries` at first; they are on `eventuallyDuration` since Decision 8 landed.
  - Verified: the S contexts pass on their own and in the full run, 768/768 with `ginkgo -p --race` (133.5s spec time,
    2m26s wall).
  - Two temporary product mutations were caught: limiting the sub-index check to the counted snapshots fails S2 (both
    scopes), and disabling the strategy-only path fails S7 (both scopes).
  - A `--repeat=2` run of the S and X5 contexts passed twice. On the third attempt a Ginkgo worker process died of
    SIGPIPE mid-run, with no spec failure reported (a runner issue, not seen otherwise).
  - `golangci-lint` reports no new issues.
  - Found and fixed a race in X5 while doing so: the second "marked as failed" check could pass against the stale
    pre-rollout generation in the cache, as the status before and after that rollout differs only in its generations;
    the check now first waits until the binding points to the rolled-out snapshot.
- [x] **L section, all rows** (October 26, 2026), in `lifecycle_integration_test.go`, four new `Describe`s (7 contexts,
  L5 dropped):
  - L1 `binding a placement policy to multiple clusters`; L2 `placement bindings with long and dotted names` (a
    100-character binding name and a 240-character policy name, both dotted; the primary work name is truncated to
    251 characters); L3 `re-creating placement bindings`; L4 `missing reserved namespaces` (namespace-scoped only).
- [x] **R section, all rows** (October 26, 2026), in the new `status_integration_test.go`, `Describe("refreshing
  placement binding status")` (7 contexts): R1–R3 for both scopes, R4 namespace-scoped only (51 ConfigMaps placed in
  descending name order).
  - R3 checks the reset-then-refresh by the Synchronized condition's last transition time (later than before the
    tampering), as the transient `WaitingForSync` state lasts only ~1s (the requeue).
- Verification (L and R): the new contexts pass on their own (162 specs) and in the full run, 930/930 with
  `ginkgo -p --race` (146.8s spec time, 2m35s wall). Temporary product mutations: dropping the failed-resource sort
  fails R4; listing works by the raw binding name (instead of the hashed label value) fails L2 (both scopes); removing
  the client-side no-op skip does **not** fail R2 (see the R2 row). `go vet` and `gofmt` are clean; `golangci-lint`
  reports no new issues (two `goconst` findings from the new helpers were fixed with constants).
- [x] **Unit tests U2–U5 and U7** (October 26, 2026; see [Unit tests](#unit-tests-decision-6)): 9 test functions, 39
  table cases, in the new `status_test.go` and in `works_test.go`. Verified: all pass with `go test -race`, and the
  package runs 930/930 with `ginkgo -p --race` (167.3s spec time, 2m57s wall). Temporary product mutations, each caught
  by the intended case: removing the client-side no-op skip in `refreshPlacementBindingStatus` (U4 "status
  unchanged"); always setting `IncompleteSetOfResources` (U2 "the processed snapshot has become invalid"); patching
  every work regardless of its strategy (U7, 3 cases); dropping the optimistic lock from the patch (U7 conflict case).
  `golangci-lint` reports no new issues (four `goconst` findings on repeated literals in the new tests were fixed with
  local constants).
- [ ] **`Available` condition fix** (October 26, 2026, 21:41 AEDT; tests updated, not yet run). In
  `markPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots`, the `IncompleteSetOfResources` check is inverted: it
  now applies when the last processed snapshot is the spec snapshot (or none was processed), and the previous
  `Available` condition is kept after a rollout to a different snapshot (B4; Behaviors 3 and 4). Test updates: U2 (the
  rollout cases keep the previous `Available`; the processed-snapshot case expects `IncompleteSetOfResources`; a new
  case for a rollout with no previous `Available`, so 8 cases and 40 table cases in total); the helpers
  (`wantPlacementBindingStatusInvalidSnapshots` takes the previous status, `invalidSnapshotsStatusActual` keeps
  explicit generations, `registerBaselineSpecs` records the status); S1, S2, S4, S5, S7b, and S9 (rollout) keep the
  baseline `Available`; S8 expects `IncompleteSetOfResources` at the same generation; X5 keeps the pre-rollout
  `Available`. The verification figures and mutation results above predate this change.
- [x] **`IncompleteSetOfResources` message reworded** (October 27, 2026, 02:58 AEDT). The `Available=False` message set
  by `markPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots` now gives the cause and fits the nothing-processed
  case too (the old "might have become incomplete" implied a previously complete set): `Cannot check the availability
  of resources as the given placement resource snapshots are invalid; some resources might be missing from the target
  cluster`. Test updates: U2's `incompleteSetOfResourcesCond` builder only; the ITs ignore condition messages
  (`ignoreFieldConditionLTTMsg`), so no IT changes. Verified: U2 passes (8/8) with `go test -race -run
  TestMarkPlacementBindingAsFailedToSynchronizeDueToInvalidSnapshots`.
