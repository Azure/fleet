# Staged Update Run Controller (v1) Integration Test Plan

**Date**: October 20, 2026 17:23 AEDT
**Task**: Plan and implement the integration tests for `pkg/v1/controllers/stagedupdaterun`, built on the envtest
suite in [suite_test.go](../../../pkg/v1/controllers/stagedupdaterun/suite_test.go). All planned scenarios are
implemented; see [Status](#status) for the progress log and the follow-ups from the review.

## Requirements

1. Cover the new controller end to end against a real API server (envtest), for both `ClusterStagedUpdateRun` and
   `StagedUpdateRun`.
2. Do not port the old `pkg/controllers/updaterun` tests as-is; the new controller behaves differently (see
   [Old tests → new plan](#old-tests--new-plan)).
3. Follow repo conventions: Ginkgo/Gomega, `<file>_integration_test.go`, a new `Context` per scenario group,
   whole-struct `cmp.Diff` comparisons, `want` naming.
4. Keep the package fast enough for `make local-unit-test` (estimate below).

## Controller behavior under test

Reconcile flow (see `controller.go`):

1. Deleting → `cleanup`: relinquish the binding manager role on the linked policy (skipped if the policy is gone),
   drop finalizer `rollout.kubefleet.dev/staged-update-run-cleanup`.
2. `shouldSkipProcessing` → `cleanup` and stop when `Initialized=False`, `Completed` exists, or
   `Initialized=True && spec.suspended`. The suspended path sets `Suspended=True/Suspended`; otherwise a previously
   suspended run gets `Suspended=False/Resumed`. Skips resync `ObservedGeneration` of `Initialized`/`Completed`.
3. Add the cleanup finalizer.
4. Get the linked policy (same namespace as the run). Not found → terminal: `Initialized=False/Failed`
   ("No linked placement policy") before init, `Completed=True/Failed` after init.
5. Claim the binding manager role (`policy.status.bindingManager`, controller name `staged-update-run`). Several runs
   may co-hold it; if another controller holds it, back off with the claim rate limiter and write no status.
6. Initialize **once** (never re-validated afterwards). User errors → `Initialized=False/Failed` (terminal):
   - strategy lookup (missing → user error);
   - bindings listed by the `spec.placementPolicyName` field index, deleting ones excluded;
   - member clusters fetched by name; missing/deleting → binding silently dropped; none left → user error;
   - grouping: first matching stage wins; `nil` selector = empty stage; empty selector = all remaining clusters;
     clusters matched by no stage are left out of the run; sort by integer `sortingLabelKey` (name tie-break) or by
     name; missing / non-integer sort label → user error;
   - `maxConcurrency`: int or percent (rounded down), min 1, default 1;
   - snapshot: explicit `resourceSnapshotName` must exist, not be deleting, be primary (sub-index `"0"`), and be
     controlled by the policy; otherwise `SnapshotResourcesIfStale` (create index 0 / reuse latest / create next),
     whose user errors also fail the initialization, e.g., a resource selected by name that does not exist (the
     snapshot manager confirms a cache miss with a read from the API server before reporting it).
7. Re-check skip (a run created suspended is initialized, then suspended and cleaned up).
8. `process`, bounded by `refreshPeriod` (30s minimum in `New`; the suite overrides it to 5s, see decision 1):
   - run `Started`; per stage: `StartedTimestamp`, `Started`, then before-stage tasks (approval only);
   - clusters via a worker pool of `min(maxConcurrency, 10)`; per cluster: set
     `binding.spec.resourceSnapshotName`, set `Started`, poll (cached client, availability rate limiter) until
     `Synchronized` and `Available` are True at the post-update binding generation;
   - shortcuts / failures: `binding.spec.suspended` → success; snapshot changed by someone else → `FailedToUpdate`
     (conflict); binding missing / deleting / cluster-name mismatch → `FailedToUpdate`; elapsed >
     `maxWaitTimePerClusterMinutes` → `FailedToUpdate` (timeout);
   - failures are counted across stages; more than `maxAllowedClusterFailures` (default 1) → cancel in-flight
     clusters, stage `Failed`, run `Completed=True/Failed`. Every cluster in the failing stage with `Started` but no
     `Completed` is marked `Completed=True/UpdateCanceled` (`markAllOngoingUpdatesInStageAsCanceled`); clusters that
     never started get no conditions;
   - then after-stage tasks (approval and/or timed wait, both must clear); stage and run complete as
     `Succeeded` or `PartiallySucceeded`.
9. Status is written **once at the end of Reconcile** (also on errors). Requeue: 200ms by default, the remaining
   time for timed waits, and **no requeue** while waiting on approvals (the approval request watch re-triggers).

Watches: runs (`GenerationChangedPredicate`, i.e. only spec changes such as suspend toggles) and
`ApprovalRequest`/`ClusterApprovalRequest` (create with `Approved` present, `Approved` added/changed, delete).
Bindings, policies, strategies, member clusters and snapshots are **not** watched.

## Test environment constraints and design implications

1. **Only this controller runs.** Tests create bindings by hand (no placement policy controller), simulate binding
   status (no work generator/applier), and clean up owner-referenced objects themselves (no GC in envtest). The lack
   of GC also makes the "stale approval request from a same-named run" path testable naturally.
2. **Run status visibility.** A reconcile blocks in the cluster wait loop for up to `refreshPeriod` and persists
   status only at the end. With the suite's 5s override, in-progress run status (e.g., a cluster `Started` but not
   `Completed`) shows up within ~5s of a cluster stalling, so:
   - assert ordering and progress primarily on side effects written immediately: binding spec, approval requests,
     the policy's binding manager claim, the finalizer;
   - assert run status at rest points (waiting on approval or timed wait, suspended, completed, failed, init failed),
     and for in-progress states after a budget expiry (≤5s + polling), as in C1 and C13.
3. **Writing to a run mid-reconcile** (e.g., toggling `suspended`) makes the in-flight reconcile's status update
   conflict and be dropped; the next reconcile recomputes. Assertions must not depend on that lost write.
4. **Suspension is not immediate.** An in-flight reconcile keeps processing (and may start new clusters or stages)
   until it blocks or the budget runs out. Suspension tests keep the in-flight cluster unavailable until
   `Suspended=True` is observed.
5. **Simulated availability.** The helper waits until the controller has updated
   `binding.spec.resourceSnapshotName` (generation bumped), then sets `Synchronized` and `Available` to True with
   `ObservedGeneration = generation`. Writing binding status earlier can conflict with the controller's spec update
   (shared `resourceVersion`) and produce an erred cluster and a requeue.
6. **Timeouts are expensive.** The CRD minimum for `maxWaitTimePerClusterMinutes` is 1, so the timeout path costs at
   least 60s. Failure-policy tests use cheap failure injection instead (conflict, missing binding, deleting binding,
   cluster-name mismatch); one context (C12) covers real timeouts with four runs (two per scope) in parallel.
7. **Fast rate limiters** (250ms–2s in the suite) make claim retries and availability polling converge within ~2s.
8. **Eventually budgets:** 10s (`eventuallyDuration`) for rest-point assertions, 20s
   (`eventuallyDurationForBudgetExpiry`) for anything that waits on a budget expiry, and ~120s for real timeouts.
9. **Cache lag on fixtures.** The controller reads policies, strategies, bindings and member clusters from the cache it
   shares with the tests; a strategy or binding the cache has yet to see fails initialization terminally. Wait for all
   fixtures to be cached (`waitForObjectsToBeCached`) before creating a run; for objects marked for deletion, wait
   until the cache sees the deletion timestamp (`markObjectForDeletion` does this).
10. **Unblocking in-flight rollouts at cleanup.** A run waiting on an unavailable binding holds its reconcile for up
    to `refreshPeriod`, which would delay its deletion. Deleting the bindings unblocks it: the wait loop fails to
    find the binding on its next poll (≤2s with the suite's rate limiters), the reconcile returns, and the deletion
    is processed. `removeObjects` deletes runs and bindings together (in its first phase), so contexts that leave a
    run mid-rollout (e.g., B9) need no extra steps.
12. **Approval requests go last at cleanup.** Deleting an approval request makes the controller process the run it
    references; if the cache still has the run, the controller may re-create the request, which then never goes
    away (no GC). `removeObjects` therefore removes (and confirms the removal of) everything else, runs included,
    before it deletes any approval request; the request's delete event then finds no run to process.
11. **Injecting snapshot conflicts.** A reconcile that reads a run whose cached copy has yet to record a cluster's
    `Started` treats that cluster's binding as not yet updated, and points it back at the snapshot to roll out. Before
    changing a binding's snapshot behind the controller's back (C7, C10), wait for the persisted status to show the
    cluster `Started`; the cache is shared and monotonic, so the controller will not read an older copy afterwards.

## Suite changes

Added so far:

- A second app namespace, `otherAppNamespaceName` (`other-app`), created in `BeforeSuite` alongside `app`, for the
  namespace isolation checks (B5, B6). envtest cannot delete namespaces, so both are created once per process.
- `refreshPeriodForTesting` (5s, decision 1): the suite builds the reconciler with `New(..., minRefreshPeriod)` (which
  rejects anything shorter) and then sets `reconciler.refreshPeriod` directly before `SetupWithManager`, with a
  comment on the deviation from production. Added with C1; all of group B still passes with it.

Still to add as needed: nothing so far. The timing constants (`eventuallyDuration`, `eventuallyInterval`,
`consistentlyDuration`, `consistentlyInterval`, `eventuallyDurationForBudgetExpiry`,
`consistentlyDurationAcrossBudgetExpiries`, `eventuallyDurationForClusterTimeout`) live in `utils_integration_test.go`,
as in the workgenerator tests.

Dropped: exposing the placement resource snapshot manager. B8 creates its snapshots directly
(`createPlacementResourceSnapshot`) instead; the controller only checks a snapshot's existence, deletion timestamp,
sub-index label and controller owner, and the manager's garbage collection only runs after the manager itself
creates a snapshot, so hand-made snapshots are left alone.

## Shared test utilities (`utils_integration_test.go`)

Scope-agnostic helpers built on the accessor interfaces, parameterized by a namespace (`clusterScopedNS`, i.e. `""`,
for cluster-scoped objects, `appNamespaceName` for namespaced ones). Following the existing v1 integration tests
(e.g., workgenerator), every scenario group runs for both scopes (decision 2) as two `Describe` blocks, one per scope
(namespaced first, then cluster-scoped), each with its own `Ordered` contexts. Scope-specific checks live only in
the namespaced block (see the scope notes below).

Implemented (all of A1–A10, B1–B10, C1–C13, D1–D8, E1–E3):

- **Object stubs**: `newPlacementPolicyObj`, `newPlacementBindingObj`, `newPlacementResourceSnapshotObj`,
  `newStagedUpdateStrategyObj`, `newStagedUpdateRunObj` (namespace + name, scope picked by the namespace),
  `newConfigMapObj` (app namespace).
- **Creators**: `createConfigMap`, `createPlacementPolicy` (selects a ConfigMap by name), `createMemberCluster`,
  `createPlacementBinding`, `createStagedUpdateStrategy(stages)`, `createStagedUpdateRun(spec)`.
- **Member cluster fixtures**: `memberClusterAndBindingNames(randStr, count)` (names `cluster-<rand>-NN`, zero-based and
  zero-padded so that lexical order matches index order), `createMemberClustersAndBindings(ns, policy, names,
  bindingNames, labels)` (created in reverse name order; bindings point at a placeholder previous snapshot), and
  `memberClusterAndBindingObjs` (stubs for cleanup).
- **Cache sync**: `waitForObjectsToBeCached(objs...)` (see constraint 9).
- **Checkers**: `stagedUpdateRunStatusUpdatedActual(ns, name, want)` (sets every condition's `ObservedGeneration`
  to the run's generation), `stagedUpdateRunFailedToInitializeActual(ns, name, msgSubstr)` (only
  `Initialized=False/Failed`, plus a message substring), `primaryPlacementResourceSnapshotCreatedActual(policy,
  index)` (labels and owner ref), `objectRemovedActual(obj)`.
- **Want builders**: `wantStagedUpdateRunStatusInitializedAndSuspended(snapshot, stages)`,
  `wantPerClusterStatuses(names, bindingNames, idxs...)` (expected cluster order by index).
- **Snapshots**: `createPlacementResourceSnapshot(name, ownerPolicy, index, subIndex, count)` (labels + controller
  owner ref, no resources), `primaryPlacementResourceSnapshotName`, `secondaryPlacementResourceSnapshotName`.
- **Updaters**: `updateConfigMapData`, `updateMemberClusterLabels`, `updateStagedUpdateStrategyStages`,
  `updateStagedUpdateRunSuspended` (all retry on conflicts).
- **Deletion fixtures**: `markObjectForDeletion(obj)` (adds `testFinalizer`, deletes, waits for the cache to see the
  deletion timestamp) and `removeTestFinalizer(obj)` (call in `AfterAll` before `removeObjects`).
- **More checkers**: `stagedUpdateRunConditionActual(ns, name, cond)` (one run-level condition at the current
  generation), `stagedUpdateRunStageLayoutActual(ns, name, stages)` (stages/clusters/tasks/concurrency, progress
  ignored), `stagedUpdateRunCleanupFinalizerRemovedActual`, `bindingManagerRoleReleasedActual(policyNS, policyName)`,
  `placementBindingResourceSnapshotActual` / `placementBindingsResourceSnapshotActual` (also used with the previous
  snapshot name to assert bindings are untouched).
- **Message substrings**: the initialization failure substrings are constants in `initialize_integration_test.go`
  (e.g., `strategyNotFoundMsgSubstr`), so the cluster-scoped copy of the tests cannot alter them.
- **Cleanup**: `removeObjects(objs...)` works in two phases (everything but approval requests, then approval
  requests; see constraint 12); within each phase, it issues all deletions first, then waits for each to be gone, so
  cleanup does not cost one polling interval per object. `AfterAll` blocks build the list with `slices.Concat`
  (keeps the `prealloc` linter quiet).
- **Unique names** per context (random suffix via `utils.RandStr()`), so leftovers never collide; member clusters
  are cluster-scoped, so their names carry the suffix too.
- **Rollout (C1)**:
  - simulator `markPlacementBindingAsAvailable(ns, name, wantSnapshot)` (waits for the binding to point at the
    snapshot, then sets `Synchronized`/`Available` True at the binding's generation; see constraint 5);
  - condition builders `stagedUpdateRunInitializedCond`, `stagedUpdateRunStartedCond`,
    `stagedUpdateRunCompletedCond(reason)`, `perStageStartedCond`, `perStageCompletedCond(reason)`,
    `perClusterStartedCond`, `perClusterCompletedCond(reason)`, plus `perClusterUpdateSucceededConds()` /
    `perStageSucceededConds()` and `wantPerClusterStatus(names, bindingNames, idx, conds...)`;
  - checkers `stagedUpdateRunRolloutStatusActual(ns, name, want)` (full status, stage timestamps ignored; shares its
    comparison core with `stagedUpdateRunStatusUpdatedActual`) and `stagedUpdateRunStagesCompletedInOrderActual`
    (every stage started and completed; each stage starts no earlier than the previous one completed, at the
    seconds precision of `metav1.Time`).
- **Rollout (C2–C5)**:
  - `markPlacementBindingAsAvailable` is now a thin wrapper around `setPlacementBindingConditions(ns, name,
    wantSnapshot, synchronizedStatus, availableStatus, observedGenerationOffset)` (C3 uses offset -1 for a stale
    observation and `Available=False` for a synchronized-only binding);
  - `updatePlacementBindingSuspended(ns, name, suspended)` waits until the cache observes the flag, so that the
    controller deterministically takes the "suspended before the update" shortcut (C5);
  - checkers `placementBindingGenerationActual(ns, name, gen)` (C4: spec never updated) and
    `perClusterConditionMessageActual(ns, run, cluster, condType, substr)` (C5: both suspended shortcuts report
    "binding is suspended"; the substring is a constant in `process_integration_test.go`).
- **Rollout (C6–C9)**:
  - `updatePlacementBindingSpec(ns, name, mutate)` updates a binding spec and waits until the cache observes the new
    generation; `updatePlacementBindingSuspended` is now a wrapper around it. C7 uses it for conflict injection
    (snapshot name) and C9 for retargeting (cluster name);
  - condition builders `stagedUpdateRunSuspendedCond` (now also used by
    `wantStagedUpdateRunStatusInitializedAndSuspended`), `stagedUpdateRunResumedCond`,
    `perStageTaskTimedWaitStartedCond`, `perStageTaskWaitTimeElapsedCond`, and `perClusterUpdateFailedConds()`
    (`Started` + `Completed/FailedToUpdate`; failures detected before the binding is updated have no `Started`);
  - checker `stagedUpdateRunStageLastedAtLeastActual(ns, name, stage, minDuration)` (stage timestamps; holds at the
    seconds precision of persisted timestamps, as the controller measures waits against persisted timestamps too).
- **Rollout (C10–C13)**:
  - `getPerClusterCondition(ns, run, cluster, condType)` (C13 reads the `Started` transition time;
    `perClusterConditionMessageActual` now builds on it) and `perStageConditionMessageActual(ns, run, stage,
    condType, substr)` (C10/C11 stage failure messages, via `stageFailedMsgSubstrFmt`);
  - timing constants `consistentlyDurationAcrossBudgetExpiries` (`2*refreshPeriodForTesting + 2s`, C13) and
    `eventuallyDurationForClusterTimeout` (2 min, C12);
  - `slowTestLabel` (`"slow"`) on C12; skip it with `--label-filter='!slow'`.
- **Stage tasks (D1)**:
  - `newApprovalRequestObj(ns, name)` (scope picked by the namespace) and `stagedUpdateRunKind(ns)`;
  - simulator `approveApprovalRequest(ns, name)` (sets `Approved=True` at the request's generation, reason
    `markedAsApprovedReason`);
  - checkers `approvalRequestCreatedActual(runNS, runName, requestName, stage)` (spec `{stagedUpdateRunName,
    stageName}` and a single controller owner ref to the run, with its UID) and
    `approvalRequestApprovalAcceptedActual(ns, name)` (`Approved` + `ApprovalAccepted=True/ApprovalAccepted` at the
    request's generation);
  - condition builders `perStageTaskApprovalRequestCreatedCond`, `perStageTaskApprovalRequestApprovedCond`;
  - the expected request name is built from a test-side template (`beforeStageApprovalRequestNameTemplate` in
    `tasks_integration_test.go`), not from the controller's own formatter.
- **Stage tasks (D2–D8)**:
  - `approveApprovalRequest` / `rejectApprovalRequest` now share `setApprovalRequestApproved(ns, name, status)`
    (reasons `markedAsApprovedReason` / `markedAsRejectedReason`; waits until the cache observes the new status, so
    that checks right afterwards, e.g., D3's and E1's Consistently checks, do not read a stale copy); `createManualApprovalRequest(ns, name, run,
    stage)` (no owner refs) and `getApprovalRequest(ns, name)`;
  - checkers `approvalRequestConditionsActual(ns, name, conds)` (exact conditions at the request's generation;
    `approvalRequestApprovalAcceptedActual` now wraps it), `approvalRequestAbsentActual`,
    `approvalRequestReplacedActual(ns, name, oldUID)` (exists with a different UID), and
    `perStageTaskTimedWaitLastedAtLeastActual(ns, run, stage, min)` (after-stage TimedWait: `WaitTimeElapsed` at
    least `min` after `TimedWaitStarted`, and the stage completed no earlier);
  - builders `approvalRequestApprovedCond(status)`, `approvalRequestApprovalAcceptedCond`,
    `perStageTaskApprovalRequestOwnershipMismatchCond`, `wantApprovalTaskStatus(name, conds...)`,
    `wantTimedWaitTaskStatus(waitTime, conds...)`, and (in `tasks_integration_test.go`)
    `wantSingleClusterCompletedStatus`;
  - deleting an approval request that the controller re-creates (D6, D8) uses a plain `hubClient.Delete`:
    `removeObjects` would wait for the name to disappear, which it never does.
- **Lifecycle (A1)**:
  - checker `stagedUpdateRunCleanupFinalizerAddedActual(ns, name)` (counterpart of the "removed" one);
  - checker `bindingManagerRoleHeldActual(policyNS, policyName, wantRefs)` (`status.bindingManager` equals
    `{controllerName, wantRefs}`, refs in any order via `lessFuncObjectRef`; the controller name comes from the
    package's `controllerName` constant), with `wantStagedUpdateRunObjectRef(ns, name)` building each ref
    test-side (`rollout.kubefleet.dev` / `v1alpha1` / `StagedUpdateRun` or `ClusterStagedUpdateRun` / namespace /
    name).
- **Lifecycle (A2–A10)**:
  - `bindingManagerRoleHeldActual` now wraps a generic `bindingManagerActual(policyNS, policyName, want)` (also used
    for another controller's claim in A6); `updatePlacementPolicyBindingManager(ns, name, bm)` writes
    `status.bindingManager` directly and deliberately does not wait for the cache (the controller may claim a
    released role right away; A6 waits for the other controller's claim explicitly before creating the run);
  - `wantStagedUpdateRunOwnerRefs(ns, name, uid)` (now shared by `approvalRequestCreatedActual`) and
    `approvalRequestOwnerRefsActual(ns, name, wantRefs)` (A8 checks the owner ref survives the run's deletion);
  - `stagedUpdateRunConditionMessageActual(ns, name, condType, substr)` (A5's `Completed` message);
  - in `lifecycle_integration_test.go`: `beforeStageApprovalStages()`, `wantWaitingForBeforeStageApprovalStatus(...)`
    (A1 refactored onto both), `triggerApprovalRequestNameTemplate`, `noLinkedPlacementPolicyMsgSubstr`, and the
    `otherBindingManager*` constants.
  - Triggering a reconcile without changing a run: `triggerStagedUpdateRunReconciliation(ns, run, stage,
    requestName)` creates an approval request whose spec names the run, waits for the cache to see it, deletes it
    (the delete handler enqueues the named run), and waits for the cache to see the deletion. Used by A1, A3, A4,
    and E2, before Consistently checks that would otherwise pass without the controller ever looking at the run.
- **Suspension (E1–E3)**:
  - `stagedUpdateRunRolloutStatusAsIsActual(ns, name, want)`: like `stagedUpdateRunRolloutStatusActual`, but
    compares condition `ObservedGeneration`s as given in the want (the shared core now takes a
    `setObservedGenerations` flag); needed once a run's generation changes, as the controller refreshes some
    conditions' generations but not others (see behavior to note 7);
  - `observedAt(gen, cond)` sets the generation on a condition builder's output.

Still to add: nothing; all planned scenarios are implemented. (Message substring checks, listed here earlier, exist:
`stagedUpdateRunFailedToInitializeActual`, `perClusterConditionMessageActual`, `perStageConditionMessageActual`,
`stagedUpdateRunConditionMessageActual`.)

## Test plan

File layout mostly mirrors the source files; the lifecycle and suspension tests (A and E) share
`lifecycle_integration_test.go`. Ordered contexts with step-wise `It`s. Every scenario group runs for both
scopes unless a scope note says otherwise.

### A. `lifecycle_integration_test.go`: lifecycle and bookkeeping

- **A1** While waiting on a before-stage approval: the finalizer is present and
  `policy.status.bindingManager == {controllerName: staged-update-run, objectRefs: [run]}`.
- **A2** After a successful run: `Completed=True/Succeeded`, finalizer removed, binding manager cleared; the run
  object remains.
- **A3** A completed run is not reprocessed: change a binding's snapshot or availability; the run's status and the
  binding stay as they are (Consistently).
- **A4** Policy missing at creation → `Initialized=False/Failed` "No linked placement policy", finalizer removed;
  creating the policy later does not revive the run.
- **A5** Policy deleted mid-run (while waiting on an approval) → `Completed=True/Failed` "No linked placement policy",
  finalizer removed.
- **A6** Claim contention: `bindingManager` pre-set to another controller → finalizer added, the run's status stays
  empty (binding manager conflicts are not reported in the status), the other claim stays, bindings untouched
  (Consistently ~5s); clear it → the run initializes, claims, and gets `Started=True/UpdateStarted`.
- **A7** Co-holding: two runs on one policy (both waiting on approvals) → both refs listed; deleting one removes only
  its ref, and the other continues.
- **A8** Delete while a cluster update is in flight → the run is gone, the claim is released, and bindings keep their
  current spec (no rollback). Approval requests carry a controller owner ref to the run (asserted, then deleted
  manually since there is no GC).
- **A9** Delete after the policy was deleted → the finalizer is still removed (NotFound path).
- **A10** Delete a run that failed to initialize → removed immediately.

### B. `initialize_integration_test.go`: one-shot initialization

Most cases create the run with `suspended: true`, which gives a stable, fully initialized rest point (and releases
the claim), then compare the full status.

- **B1** Happy path:
  - two label-selected stages, sorted by name; `status.stages` matches the want, including resolved
    `maxConcurrency` and task statuses (`WaitTime` copied for timed waits, no request name yet);
  - `Initialized=True/PreppedResourceSnapshotAndAllStages`;
  - `resourceSnapshotNameToRollout == <policy>-resource-snapshot-0`, and that snapshot exists and is owned by the
    policy.
- **B2** Grouping: overlapping selectors → the first stage wins; `nil` selector → empty stage; empty selector → all
  remaining clusters; a cluster matched by no stage is left out of every stage.
- **B3** Sorting: integer ascending with name tie-break; missing label → init failed; non-integer → init failed.
- **B4** `maxConcurrency`: unset → 1; `3` with 2 clusters → 3; `"50%"` of 5 → 2; `"10%"` of 3 → 1; percent on an
  empty stage → 1.
- **B5** Binding and cluster filtering:
  - excluded: bindings of other policies (field index); deleting bindings (finalizer, then delete); bindings to
    non-existent clusters; deleting clusters;
  - namespaced scope only: bindings in another namespace with the same policy name are excluded;
  - all excluded → init failed "no placement bindings are found".
- **B6** Strategy missing → init failed and terminal (creating the strategy later does not help). Namespaced scope
  only: a strategy in another namespace is not found.
- **B7** Snapshot selection without an explicit name: none exists → index 0 created; up-to-date latest → reused
  with no new snapshot; selected ConfigMap changed → index 1 created.
- **B8** Explicit snapshot: valid primary owned by the policy → used; not found / sub-index not `"0"` / owned by
  another policy / marked for deletion → init failed.
- **B9** One-shot init: after init (suspended), change the strategy (add a stage, change a selector), cluster labels,
  and add a binding; resume → stages unchanged.
- **B10** Created suspended → `Initialized=True`, `Suspended=True/Suspended`, no `Started`, bindings untouched,
  claim released, finalizer removed.
- **B11** Selected resource missing: the policy selects (by name) a ConfigMap that does not exist →
  `SnapshotResourcesIfStale` returns a user error ("selected resource does not exist") → init failed, claim released,
  no snapshot created; creating the ConfigMap later does not help (terminal, like B6).

### C. `process_integration_test.go`: rollout execution

- **C1** Multi-stage happy path: canary (1 cluster) then prod (3 clusters, `maxConcurrency` 1, sorted).
  - Stage 2 bindings stay untouched during stage 1, and cluster N+1's binding stays untouched until N is marked
    available (Consistently).
  - While cluster N is held unavailable, the persisted status shows it `Started` without `Completed` within one
    budget (~5s).
  - Final status:
    - every cluster `Started` and `Completed/UpdateSucceeded`;
    - stages `Completed/Succeeded` with ordered timestamps;
    - the run `Started` and `Completed/Succeeded`;
    - all bindings on the target snapshot.
- **C2** Concurrency: `maxConcurrency: 2` of 3 → two bindings updated together, the third waits for one to complete;
  `"100%"` → all at once.
- **C3** Availability detection: stale True conditions (`observedGeneration` < generation) and
  `Synchronized` without `Available` do not count; both True at the current generation do.
- **C4** A binding already on the target snapshot and available → completes without a spec update (generation
  unchanged).
- **C5** Binding suspended: before processing → immediate success; suspended during the wait → success. Both report
  "(binding is suspended)".
- **C6** Empty stage (`nil` selector) between two stages → `Started` and `Completed/Succeeded` with no clusters; its
  after-stage tasks still run.
- **C7** Conflict: while waiting on cluster X, change its binding's snapshot → X `FailedToUpdate` (conflict);
  default failure policy → stage and run `PartiallySucceeded`.
- **C8** Binding deleted after init (suspend, delete, resume) → `FailedToUpdate` "binding cannot be found".
- **C9** Binding deleting, or its `clusterName` changed after init → `FailedToUpdate`.
- **C10** Threshold within a stage (`maxAllowedClusterFailures: 0`, `maxConcurrency: 2`, three clusters A, B, C):
  wait until A's and B's bindings are on the target snapshot (C still on the old one), keep B unavailable, then fail
  A (conflict injection) →
  - stage `Failed` (its message counts only `FailedToUpdate` clusters: "1 out of 3"), run `Completed/Failed`;
  - A `Completed=True/FailedToUpdate`; B (in flight) `Started` plus `Completed=True/UpdateCanceled`;
  - C (never started) has no conditions, and its binding stays on the old snapshot (Consistently);
  - the next stage's bindings are untouched;
  - claim released, finalizer removed.
  - Keep B's binding unavailable throughout: if it became available right as the cancel happens, B could
    legitimately finish as `UpdateSucceeded` instead.
- **C11** Threshold across stages (max 1): one failure in stage 1 (`PartiallySucceeded`), with a reconcile boundary
  (a budget expiry while the next stage's first cluster is in flight) between the two stages, then one failure in
  stage 2 → stage 2 `Failed`, run `Failed`. This exercises
  `countClusterUpdateFailures` on persisted status.
- **C12** Real timeouts (`Label("slow")`), each run with one cluster that never becomes available:
  - T1 (`maxWaitTimePerClusterMinutes: 1`, max 1) → `FailedToUpdate` (timeout), run `PartiallySucceeded`;
  - T2 (max 0) → run `Failed`.
  - Exception to the one-`Describe`-per-scope layout: one context creates T1 and T2 for both scopes at once (4 runs in parallel), so
    the 1-minute minimum is paid once (~70–90s total).
- **C13** Budget exhaustion: one cluster never becomes available. After the 5s budget, the persisted status shows
  run, stage and cluster `Started`, with no `Completed`. Mark it available → completes with no second spec update
  (re-entry path).

### D. `tasks_integration_test.go`: stage tasks and approval requests

- **D1** Before-stage approval:
  - request `<run>-before-stage-<stage>` is created with a controller owner ref to the run and spec
    `{stagedUpdateRunName, stageName}`;
  - task `ApprovalRequestName` is set, with `ApprovalRequestCreated=True/RequestCreated`;
  - the stage is already `Started`; bindings stay untouched (Consistently);
  - approve → the request gets `ApprovalAccepted=True`, the task gets `ApprovalRequestApproved=True`, and the
    rollout starts promptly (driven by the watch, not polling).
- **D2** After-stage approval: `<run>-after-stage-<stage>` is created only after every cluster in
  the stage is done; the stage does not complete and the next stage stays untouched until approved.
- **D3** `Approved=False` → no progress; flipping it to True → progress.
- **D4** After-stage timed wait (5s): `TimedWaitStarted`, then `WaitTimeElapsed` at least 5s later; the next stage
  starts only after that.
- **D5** Approval and timed wait after one stage: both must clear (approve early → still waits for the timer; timer
  done → still waits for the approval).
- **D6** Deleted approval request → re-created with the same name; the run proceeds once approved.
- **D7** Stale request from a deleted same-named run (no GC): create run R, wait for its request, delete R,
  re-create R → the old request is deleted and re-created with the new run's UID as owner.
- **D8** Manually created request (no owner) with the expected name → `ApprovalRequestCreated=False/
  RequestOwnershipMismatch` and no progress; delete it → the controller creates its own and the condition returns to
  `True/RequestCreated`. The manual request's `spec.stagedUpdateRunName` must name the run, so its delete event
  re-enqueues the run.

### E. Suspension (Context group in `lifecycle_integration_test.go`)

- **E1** Suspend while waiting on an approval → `Suspended=True/Suspended`, claim released, finalizer removed;
  approving while suspended makes no progress; resume → `Suspended=False/Resumed`, claim and finalizer re-acquired,
  rollout proceeds.
- **E2** Suspend during a cluster update:
  - keep the binding unavailable until `Suspended=True` is observed (within one ~5s budget);
  - mark it available while suspended → no status change;
  - resume → the cluster completes, and its `Started` `LastTransitionTime` is at or after the `Resumed`
    transition (wait timer reset).
- **E3** `ObservedGeneration` resync: toggling `suspended` on a completed run bumps `Completed`'s
  `observedGeneration` without adding `Suspended`; on an init-failed run it bumps `Initialized`'s.

### Better as unit tests (out of scope here)

- Approval request name truncation and hashing (`uniquename.go`). **Done (October 20, 2026)**: `uniquename_test.go`
  covers both formatters with golden names (short, exactly 200 characters, 201 characters, a cut right after a dot,
  and an over-long stage name), checks that truncated names stay apart (hash suffix) and that before/after names
  differ, and runs every run-name length (1–253) with varying dot positions and three stage-name lengths, checking
  the length limit, the hash suffix, and DNS-subdomain validity. `uniquename.go` is at 100% statement coverage;
  removing the dot trimming makes all three relevant tests fail.
- The concurrency cap of 10 workers.
- The pre-loop quorum-read conflict path (racy to hit via envtest).
- Approval event handler filtering (`eventhandler.go`).
- Rejecting a before-stage `TimedWait` (the CRD already forbids it).
- The `UpdateCanceled` branches in `refreshOneCluster` (treated as failed) and `countClusterUpdateFailures` (counted
  as a failure). The controller never reaches them: `UpdateCanceled` is only written into a stage that is marked
  `Failed`, and failed stages are skipped before their clusters are looked at.

## Old tests → new plan

| Old area (`pkg/controllers/updaterun`) | New plan |
| --- | --- |
| Finalizer added | Adapted → A1 |
| Delete normal / failed / processing run | Adapted → A8–A10 |
| Delete removes approval requests | Changed: controller relies on owner-ref GC; assert owner ref (A8) |
| `validateCRP` (not found / strategy type / ApplyStrategy copy) | Not-found → A4; the rest dropped (no such fields) |
| `determinePolicySnapshot` (PickN / PickFixed / PickAll) | Dropped: no policy snapshots; bindings are listed by policy name |
| `collectScheduledClusters` (binding states, to-be-deleted, counts) | Dropped; replaced by B5 |
| Strategy not found | Adapted → B6 |
| After-stage task validation (duplicates, wait time) | Dropped: enforced by CRD CEL (belongs in `test/apis`) |
| Invalid sorting label | Adapted → B3 (plus missing label) |
| Cluster in multiple stages / in no stage → fail | Behavior changed → B2 (first match wins; unmatched left out) |
| Empty vs. nil selector | Adapted → B2 |
| Stage status generation | Adapted → B1 |
| `resourceSnapshotIndex` checks | Replaced by explicit `resourceSnapshotName` checks → B8 |
| Create / reuse / next-index snapshot | Adapted → B7 |
| Override snapshots | Dropped |
| `validation_integration_test.go` (re-validate every reconcile) | Dropped; replaced by B9 (one-shot init) |
| One-by-one rollout, approvals, timed waits | Adapted → C1, D1–D5 |
| Empty stage | Adapted → C6 |
| Stuck condition / metrics | Dropped (no metrics); replaced by timeout + failure policy (C10–C12) |
| Unexpected binding state abort | Replaced by C8 / C9 |
| Delete stage | Dropped (no delete stage) |
| Report-diff mode | Dropped (no report-diff in the new binding API) |
| Recreate deleted approval request | Adapted → D6 |
| State `Initialize` → `Run` | Replaced by created-suspended → resume (B10, E1) |
| `stop_integration_test.go` (`Stop` / `Run`) | Replaced by E1–E3 (suspend releases the claim; not immediate; wait timer reset on resume) |

## Decisions (approved October 20, 2026)

1. Override `refreshPeriod` to 5s in the suite (direct field override after `New`; see Suite changes). This makes
   in-progress run status visible quickly, at the cost of deviating from the 30s production minimum.
2. Run every scenario group for both scopes, as one `Describe` block per scope (the existing v1 integration test
   pattern, adopted instead of the originally proposed `DescribeTableSubtree`); scope-specific checks (B5 namespace
   isolation, B6 cross-namespace strategy) run in the namespaced block only.
3. Include real timeouts in C12 only, with all four runs (T1 and T2 for both scopes) in parallel, labeled `slow`.
4. Simulate binding availability by hand in each test (no auto-responder), so ordering can be asserted.

## Behaviors to note (documented only; no action for now)

The tests should assert the current behavior below as-is. None of these items is to be changed as part of this work.

1. An approval ownership mismatch returns a user error from Reconcile, so recovery uses controller-runtime's
   exponential backoff; after the manual request is deleted, a fast re-trigger depends on that request naming this
   run.
2. Clusters matched by no stage are silently left out of the run, with no condition or message.
3. Suspension relinquishes the binding manager role, so other managers may move bindings while a run is suspended;
   on resume this surfaces as `FailedToUpdate` (conflict).
4. Suspension only takes effect after the in-flight reconcile ends; until then it may keep starting clusters and
   stages.
5. A missing policy at creation is terminal, even if the policy appears later. The same holds for a resource selected
   by name that is missing at initialization (B11).
6. **Resolved (October 20, 2026).** Previously, in a run that failed on the threshold, in-flight clusters kept
   `Started` without `Completed`. They are now marked `Completed=True/UpdateCanceled` (C10). Verified against envtest
   with a throwaway test (A conflict → `FailedToUpdate`, in-flight B → `UpdateCanceled`, unstarted C untouched).
   Remaining nuances, documented only:
   - An in-flight cluster whose binding becomes available right as the cancel happens may finish as
     `UpdateSucceeded` instead of `UpdateCanceled`.
   - Narrow, pre-existing race: if a binding spec update is on the wire when the context is canceled, the API server
     may apply it while the controller sees an error. That cluster then has no conditions even though its binding
     moved. Not reliably testable via envtest.
7. Stage-task conditions (`ApprovalRequestCreated`, `ApprovalRequestApproved`, `TimedWaitStarted`, `WaitTimeElapsed`)
   keep the `ObservedGeneration` they were set at; unlike run-, stage- and cluster-level conditions, they are never
   refreshed when the run's generation changes (e.g., after a suspend and resume). E1 asserts a completed run whose
   approval task has `ApprovalRequestCreated` at generation 1 and `ApprovalRequestApproved` at generation 3. Related:
   while a run is suspended, only `Initialized` (and `Suspended`) carry the new generation; on a completed run, a
   generation change refreshes only `Completed` (E3).
8. Suspending a run mid-reconcile makes that reconcile's final status write fail with a conflict (the run changed),
   so whatever it learned is dropped and the next reconcile works from the last persisted status. E2 therefore
   suspends only after the persisted status shows the in-flight cluster `Started`.

## Running the tests

Use the Ginkgo CLI in parallel mode rather than `go test`, via `go run` so that the CLI matches the Ginkgo version
pinned in `go.mod` (v2.33.0; a v2.23.4 binary in `GOPATH/bin` is older):

```bash
KUBEBUILDER_ASSETS="$(make -s kubebuilder-assets-path | tail -1)" \
  go run github.com/onsi/ginkgo/v2/ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun
```

Each parallel process runs its own `BeforeSuite` (its own envtest API server, manager, and controller), as the suite
already does; `Ordered` contexts stay within one process, and the random name suffixes keep contexts apart within a
process. Add `--focus="<Describe/Context text>"` to run a subset, and `--label-filter='!slow'` to skip the ~1-minute
per-cluster timeout context (C12). Run each change once; repeated runs (flake hunting) are not needed for now.

In CI (`.github/workflows/ci.yml`), the package is skipped in the `make test` step (`TestMain` in `suite_test.go`
exits early when `KUBEFLEET_CI_TEST_RUNNER_NAME` is `default`, as the work applier tests do) and runs in its own step
instead: `ginkgo -v -p --procs=4 --race --cover -coverprofile=staged-update-run-it-coverage.out
./pkg/v1/controllers/stagedupdaterun/` with `KUBEFLEET_CI_TEST_RUNNER_NAME=ginkgo`. Run locally the same way (with
the v2.23.4 CLI that CI installs): 610 of 610 specs pass, ~207s of spec time, ~3m44s wall, 85.9% coverage (measured
before B11, which brought the total to 620 specs).

## Estimated runtime

With the 5s refresh period, about 6 min per scope (A ~1 min, B ~1.5 min, C ~1.5–2 min without C12, D ~1 min,
E ~1 min), so ~12 min for both scopes, plus ~1.5 min for C12: roughly 13–14 min of serial spec time. With
`ginkgo -p`, wall time drops to roughly the longest context (C12, ~1.5 min) plus the share of the rest per process;
measured after group C: ~80s of spec time, ~1m33s wall (including compilation), dominated by C12; after group D:
~115s of spec time, ~2m07s wall; after group A: ~102s of spec time, ~1m55s wall; with everything (606 specs): ~99s of
spec time, ~1m51s wall; after B11 (620 specs, October 27): ~150s of spec time, ~2m39s wall.
Outside CI, `make local-unit-test` still runs this package serially under `go test` within the 30m timeout.

## Status

Plan approved with the decisions above. **All planned scenarios (A1–A10, B1–B11, C1–C13, D1–D8, E1–E3) are
implemented**; see the last entry below for the final validation.

- October 20, 2026: updated for the per-cluster `Canceled` reason (behavior to note 6 resolved; C10 and the
  unit-test list adjusted).
- October 20, 2026: **B1 implemented** in `initialize_integration_test.go` (context "label-selected stages", both
  scopes), with the shared utilities in `utils_integration_test.go`. Five member clusters (two canary, three prod,
  created in reverse name order); canary stage: before-stage approval, after-stage 10m timed wait, default
  concurrency; prod stage: `maxConcurrency: 2`, after-stage approval + 5m timed wait. Asserts the full run status
  (stages, cluster order, task statuses, resolved concurrency, `Initialized` and `Suspended` conditions, snapshot to
  roll out) and the created primary snapshot (labels, owner ref). Validation:
  - `go test -race ./pkg/v1/controllers/stagedupdaterun/...`: 4 of 4 specs pass (~11s) (later runs use
    `ginkgo -p`; see Running the tests);
  - negative check (expected prod concurrency changed to 3): fails with a clear status diff;
  - golangci-lint: no findings in the new test files.
- October 20, 2026: **B2–B4 implemented** in `initialize_integration_test.go` (both scopes; B1 refactored onto the
  new member cluster helpers). Contexts per scope:
  - B2 "overlapping, nil, and empty label selectors": stages east (region=east), prod (env=prod), empty (nil
    selector), rest (`{}` selector); member cluster 1 matches east and prod and lands in east; cluster 3 is
    picked up by rest; the empty stage has no clusters.
  - B2 "clusters selected by no stage": a cluster without an env label is left out of both stages.
  - B3 "clusters sorted by an integer label": `order` values 10, 2, 1, 2 → order 2, 1, 3, 0 (numeric, not lexical;
    tie broken by name).
  - B3 "clusters missing the sorting label" / "clusters with a non-integer sorting label": the run (not
    suspended) ends with only `Initialized=False/Failed`, message containing "yet the label is missing on a
    cluster" / "failed to parse sorting label value as integer".
  - B4 "max concurrency resolution": default → 1; 3 with 2 clusters → 3; "50%" of 5 → 2; "10%" of 3 → 1; "100%"
    of an empty stage → 1.
  - The cluster-scoped `Describe` is a mechanical copy of the namespaced one (namespace and wording only).
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 16 of 16 specs pass (~14s spec time);
    golangci-lint: no findings in the test files.
- October 20, 2026: **B5–B10 implemented** in `initialize_integration_test.go` (both scopes unless noted). The
  file now holds 16 namespaced and 14 cluster-scoped contexts; the cluster-scoped `Describe` is still a mechanical
  copy, minus the namespaced-only contexts. Contexts added per scope:
  - B5 "placement bindings and member clusters to leave out": 6 clusters; only 0 and 1 make it into the stage
    (2: binding of another policy; 3: binding marked for deletion; 4: member cluster missing; 5: member cluster
    marked for deletion).
  - B5 "no placement bindings that count": all bindings filtered out → `noPlacementBindingsMsgSubstr`.
  - B5 (namespaced only) "placement bindings in another namespace": a same-policy-name binding in `other-app` is
    left out.
  - B6 "missing staged update strategy": init fails; creating the strategy and then bumping the run generation
    (suspend) yields a re-processed run that is still failed (condition observed at generation 2).
  - B6 (namespaced only) "staged update strategy in another namespace": not found → init fails.
  - B7 "resource snapshot selection (no resource snapshot specified)": run 1 creates index 0; run 2 reuses index 0;
    after a ConfigMap change, run 3 creates and uses index 1.
  - B8 "resource snapshot selection (resource snapshot specified)": hand-made snapshots; valid primary → used;
    missing / secondary (sub-index 1) / other policy's / marked for deletion → init fails with the matching message.
  - B9 "one-shot initialization": after init (suspended), the strategy (selector changed, stage added), a cluster's
    labels and a new cluster + binding change so that a re-initialization would reorder clusters; on resume,
    `Suspended=False/Resumed`, the stage layout is unchanged, cluster 0's binding moves to the snapshot, and the
    others (including the new cluster's) stay on the previous snapshot (Consistently).
  - B10 "staged update runs created as suspended": full status (no `Started`), finalizer removed, binding manager
    role released, bindings untouched (Consistently).
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 68 of 68 specs pass (~25s spec time,
    ~36s wall); go vet and golangci-lint: no findings in the test files.
- October 20, 2026: **C1 implemented** in `process_integration_test.go` (both scopes; context "multiple stages, one
  cluster at a time"), together with the 5s refresh period override in the suite. Four member clusters: 0 (canary),
  1-3 (prod, `order` labels 30/10/20, so the prod rollout order is 2, 3, 1); no stage tasks; run not suspended.
  For each cluster in turn: its binding moves to the snapshot (Eventually); the bindings of the clusters that come
  later stay on the previous snapshot (Consistently, 5s); the persisted status shows the cluster `Started` without
  `Completed` (full status, within a budget expiry); then the binding is marked available. Finally: the run, both
  stages, and every cluster are `Completed` as succeeded, the stages completed in order (timestamps), and all
  bindings point at the snapshot.
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 102 of 102 specs pass (~35s spec time,
    ~45s wall); go vet and golangci-lint: no findings in the test files.
- October 20, 2026: **C2–C5 implemented** in `process_integration_test.go` (both scopes). Contexts per scope:
  - C2 "concurrent updates within a stage": canary stage (clusters 0-2, `maxConcurrency: 2`) then prod stage
    (clusters 3-5, `"100%"` → resolved to 3). Bindings 0 and 1 move together while 2-5 stay put (Consistently);
    the persisted status shows clusters 0 and 1 `Started`; marking cluster 1 available frees a slot for cluster 2
    while the prod clusters stay put; once the canary stage completes, bindings 3-5 move together and all three
    show `Started`; the run completes with stages in order.
  - C3 "placement binding availability detection": with the cluster in flight, `Synchronized`/`Available` True at
    the previous generation, and then `Synchronized` True with `Available` False at the current generation, each
    leave the persisted in-progress status unchanged (Consistently, 5s); both True at the current generation
    completes the run.
  - C4 "placement bindings already up to date": cluster 0's binding is created pointing at the snapshot the run
    will create (`<policy>-resource-snapshot-0`) and already available; it completes immediately (status shows it
    succeeded while cluster 1 is in flight), and stays at generation 1 (never updated).
  - C5 "suspended placement bindings": cluster 0's binding is suspended before the run (its binding still moves to
    the snapshot, and the rollout moves on to cluster 1 without availability); cluster 1's binding is suspended
    mid-wait; the run completes, and the `Completed` messages read "binding has been suspended" and "binding is
    suspended" respectively.
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 158 of 158 specs pass (~38s spec time,
    ~49s wall); go vet and golangci-lint: no findings in the test files.
- October 20, 2026: **C6–C9 implemented** in `process_integration_test.go` (both scopes). Contexts per scope:
  - C6 "empty stage with after-stage tasks": canary (cluster 0) → empty (nil selector, after-stage 5s timed wait) →
    prod (cluster 1). Once the canary cluster is available, the persisted status shows the empty stage `Started`
    with its task `TimedWaitStarted` (persisted right as the wait starts); the prod binding moves only after the
    wait; final status has the task with `TimedWaitStarted` + `WaitTimeElapsed`, all stages succeeded and in order,
    and the empty stage lasting at least 5s by its timestamps.
  - C7 "resource snapshot conflicts": with cluster 0 in flight, the test waits for the persisted `Started` before
    pointing its binding at another snapshot (see the race note below); cluster 0 ends `FailedToUpdate` ("resource
    snapshot name conflict"), cluster 1 succeeds, stage and run end `PartiallySucceeded` (default failure policy),
    and the binding keeps the conflicting snapshot.
  - C8 "placement bindings deleted after initialization": suspended run initializes; binding 0 is deleted (cache
    observes it); on resume, cluster 0 ends `Completed/FailedToUpdate` with no `Started` ("the binding cannot be
    found"), cluster 1 succeeds; run conditions include `Suspended=False/Resumed`; `PartiallySucceeded`.
  - C9 "placement bindings marked for deletion or retargeted after initialization": `maxAllowedClusterFailures: 2`;
    after init, binding 0 is marked for deletion and binding 1's `clusterName` is changed; on resume both fail with
    no `Started` ("marked for deletion or its cluster name does not match"), keep the previous snapshot, cluster 2
    succeeds; `PartiallySucceeded`.
  - Race noted while writing C7 (not a controller change, a test-ordering rule): a reconcile that reads a run from
    a cache that has yet to record a cluster's `Started` treats the binding as not yet updated and rewrites its
    snapshot name. Tests that change a binding's snapshot behind the controller's back must first observe the
    persisted `Started` (the shared cache is monotonic, so the controller sees it too).
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 218 of 218 specs pass (~42s spec time,
    ~55s wall); go vet and golangci-lint: no findings in the test files.
- October 20, 2026: **C10–C13 implemented** in `process_integration_test.go` — group C is complete. Contexts:
  - C10 "cluster failures exceeding the threshold within a stage" (both scopes): canary stage (clusters 0-2,
    `maxConcurrency: 2`) then prod (cluster 3); `maxAllowedClusterFailures: 0`. After the persisted status shows
    clusters 0 and 1 `Started`, binding 0 gets a conflicting snapshot (binding 1 is never marked available). Final:
    cluster 0 `FailedToUpdate`, cluster 1 `Started` + `Completed/Canceled`, cluster 2 no conditions; canary stage
    `Failed` ("1 out of 3 clusters failed to update; total failures across stages have exceeded the threshold");
    run `Completed/Failed`; bindings 2 and 3 stay on the previous snapshot (Consistently); finalizer removed and
    binding manager role released.
  - C11 "cluster failures exceeding the threshold across stages" (both scopes): default policy (max 1). Cluster 0
    fails (conflict) and cluster 1 succeeds → canary `PartiallySucceeded`; the test then waits for the persisted
    status to show the canary stage completed and cluster 2 `Started` (a reconcile boundary, so the next
    reconcile counts the canary failure from persisted status), and injects a conflict on cluster 2 → prod `Failed`
    ("1 out of 2 ..."), run `Failed`; cluster 3 never starts and its binding stays put. The plan's "after-stage
    approval" boundary was replaced with this budget-expiry boundary (no approval helpers needed).
  - C12 "member clusters that time out" (one context for both scopes, `Label("slow")`): four runs created at once,
    `maxWaitTimePerClusterMinutes: 1`, stage concurrency `"100%"`; per scope, T1 (max 1, clusters 0 and 1; cluster 1
    marked available) → cluster 0 `FailedToUpdate` ("has failed due to timeout"), cluster 1 succeeded, stage and
    run `PartiallySucceeded`; T2 (max 0, cluster 0 only) → stage and run `Failed`. Each stage lasted at least one
    minute by its timestamps.
  - C13 "processing time budget expiries" (both scopes): one cluster held unavailable; once the in-progress status
    is persisted, it stays the same across two more budget expiries (Consistently, 12s), the cluster's `Started`
    transition time is unchanged (no reset of the wait time limit), and the binding stays at generation 2 (no
    second spec update); after marking it available, the run completes and the binding is still at generation 2.
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 274 of 274 specs pass (~80s spec time,
    ~1m33s wall; C12 dominates); `ginkgo --dry-run --label-filter='!slow'` selects 270 of 274 (only C12 skipped);
    go vet and golangci-lint: no findings in the test files.
- October 20, 2026: **D1 implemented** in the new `tasks_integration_test.go` (both scopes; context "before-stage
  approval"). One stage with a before-stage approval task and both clusters at `"100%"` concurrency. Checks: the
  request `<run>-before-stage-canary` exists with spec `{run, canary}` and a controller owner ref to the run (UID
  included); the persisted status shows the stage `Started` with the task's `ApprovalRequestName` and
  `ApprovalRequestCreated=True/RequestCreated`; bindings stay on the previous snapshot (Consistently, 5s); after the
  test sets `Approved=True`, the request gets `ApprovalAccepted=True`, both bindings move within 10s (the run is not
  requeued while waiting for approval, so this relies on the approval request watch), and once they are available,
  the run completes with the task showing `ApprovalRequestCreated` + `ApprovalRequestApproved`. The cluster-scoped
  copy is generated by the same script, which now also rewrites "approval request" to "cluster approval request".
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 290 of 290 specs pass (~76s spec time,
    ~1m29s wall); go vet and golangci-lint: no findings in the test files.
- October 20, 2026: **D2–D8 implemented** in `tasks_integration_test.go` (both scopes) — group D is complete.
  Contexts per scope:
  - D2 "after-stage approval": canary (clusters 0, 1; after-stage approval) then prod (cluster 2). The request
    `<run>-after-stage-canary` stays absent (Consistently, 5s) while cluster 0 and then cluster 1 are in flight, and
    is created once both are done; the persisted status shows the canary stage `Started` (not completed) with the
    task `ApprovalRequestCreated`; binding 2 stays put (Consistently) and the status is unchanged; after approval,
    the request is accepted, prod rolls out, and the run completes with stages in order.
  - D3 "rejected approval": after `Approved=False`, the request carries only that condition (no
    `ApprovalAccepted`) and the binding stays put (Consistently); the waiting status is unchanged; flipping to True
    → accepted → rollout → completed.
  - D4 "after-stage timed wait": canary (after-stage 5s wait) then prod; status shows `TimedWaitStarted` right
    after the canary cluster is done; prod rolls out only after the wait; the final task has `TimedWaitStarted` +
    `WaitTimeElapsed` at least 5s apart, the canary stage completed no earlier, and stages are in order.
  - D5 "after-stage approval and timed wait": canary `[Approval, TimedWait 10s]`, prod `[Approval, TimedWait 5s]`.
    Canary approved early → status shows the approval task approved but the wait still running, prod untouched;
    prod rolls out only after the 10s wait (checked via the task timestamps). Prod's wait elapses first → status
    shows `WaitTimeElapsed` with the approval still pending, unchanged for 5s (no requeue); approving completes
    the run.
  - D6 "deleted approval request": deleting the request → re-created with the same name and a new UID, still owned
    by the run; binding stays put until the new request is approved; then completes.
  - D7 "stale approval request from a deleted staged update run of the same name": the run is deleted (its request
    stays, no GC), the stale request is then approved, and the run is re-created. The controller replaces the stale
    request (new UID, owner = the re-created run's UID, no conditions), so the old approval does not carry over;
    the binding stays put until the new request is approved.
  - D8 "approval request not owned by the staged update run": a manually created (and approved) request with the
    expected name and no owner → task `ApprovalRequestCreated=False/RequestOwnershipMismatch`, no rollout
    (Consistently); deleting it (the request names the run, so its delete event re-enqueues the run) → the
    controller creates its own (owner = run) and the condition returns to `True/RequestCreated`; approve → completes.
  - First run: 6 failures, all test bugs, fixed: (1) D6/D8 deleted the request through `removeObjects`, which waits
    for the name to disappear while the controller re-creates it right away; (2) D5's in-progress want omitted the
    prod stage's after-stage task statuses, which exist (with no progress) from initialization on.
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 430 of 430 specs pass (~115s spec time,
    ~2m07s wall); go vet and golangci-lint: no findings in the test files.
- October 20, 2026: **A1 implemented** in the new `lifecycle_integration_test.go` (both scopes; context "staged
  update runs waiting for approval"). One stage with a before-stage approval that is never granted. Once the
  persisted status shows the run waiting for approval, the run has the cleanup finalizer and the policy's
  `status.bindingManager` is exactly `{controllerName: staged-update-run, objectRefs: [<this run>]}` (Eventually),
  and both stay that way while the run waits (Consistently, 5s). `AfterAll` deletes the run while it is still in
  progress, which also exercises the deletion path (role released, finalizer removed) as part of cleanup.
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 438 of 438 specs pass (~93s spec time,
    ~1m45s wall); go vet and golangci-lint: no findings in the test files.
- October 20, 2026: **A2–A10 implemented** in `lifecycle_integration_test.go` (both scopes) — group A is complete.
  Contexts per scope:
  - A2 + A3 "completed staged update runs" (one context): the run completes (`Completed=True/Succeeded`), the
    finalizer is removed, the binding manager role is released, and the run object stays (Consistently). Then
    the binding is pointed at another snapshot and marked unavailable, and a reconcile is triggered (create +
    delete an approval request naming the run); the run's status and the binding stay as they are (Consistently).
  - A4 "missing placement policy at creation": `Initialized=False/Failed` ("No linked placement policy"),
    finalizer removed; after the policy is created and the run's `suspended` flag is toggled (to force a
    reconcile), the run is still failed at the new generation, with no claim on the new policy and the binding
    untouched (Consistently).
  - A5 "placement policy deleted while waiting for approval": the policy is deleted, and approving the request
    (policies are not watched, so the approval is what triggers the run) yields `Completed=True/Failed` ("No linked
    placement policy") with the approval task still only `ApprovalRequestCreated`; finalizer removed; binding
    untouched.
  - A6 "binding manager role held by another controller": `status.bindingManager` is pre-set to another
    controller (and observed in the cache) before the run is created; the run gets the finalizer but its status
    stays empty, the other claim stays, and the binding stays put (Consistently); after the claim is cleared, the
    run initializes (waiting for approval) and claims the role for itself. (Superseded by the A6 update below:
    the status is no longer empty.)
  - A7 "multiple staged update runs of the same placement policy": both runs wait for approval and are both in the
    claim; deleting run 1 leaves only run 2 in the claim; approving run 2 rolls out and completes it, releasing the
    role.
  - A8 "staged update runs deleted while a member cluster is being updated": after approval, cluster 0's binding
    moves; deleting the run removes it (within a budget expiry), releases the role, leaves binding 0 on the new
    snapshot and binding 1 on the previous one (Consistently), and leaves the approval request with its controller
    owner ref to the deleted run (no GC).
  - A9 "staged update runs deleted after their placement policy": with the run waiting (finalizer present), the
    policy is deleted and then the run; the run is removed (NotFound path in cleanup).
  - A10 "staged update runs that failed to initialize": missing strategy → init failed, finalizer removed; deleting
    the run removes it right away.
  - First run: 2 failures (A6, both scopes), a test bug, fixed: the binding-manager helper waited for the cache to
    show the cleared claim, but the controller re-claimed the role within its retry backoff, so the cleared state
    was never observed. The helper no longer waits; A6 waits for the other controller's claim explicitly instead.
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 538 of 538 specs pass (~102s spec time,
    ~1m55s wall); go vet and golangci-lint: no findings in the test files.
- October 20, 2026: **A6 updated** for the controller change that reports failed binding manager claims: when the
  claim fails (another controller holds the role), the run gets `Started=False/FailedDueToBindingManagerConflicts`
  ("... currently managed by another controller; will retry later"), and `process` now re-sets `Started` to
  `True/UpdateStarted` whenever it is not True. A6 now first waits for the run's status to be exactly that one
  condition (plus the message check), then keeps the previous Consistently check (status unchanged, other claim
  intact, binding untouched) against that status instead of an empty one; the existing "initialize" check after the
  claim is cleared verifies that `Started` flips to True. No other test hits a failed claim.
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 540 of 540 specs pass (~105s spec time,
    ~1m57s wall); go vet and golangci-lint: no findings in the test files.
- October 20, 2026: **E1–E3 implemented** in `lifecycle_integration_test.go` (both scopes) — all groups complete.
  Generations: each run is created at 1, suspended at 2, resumed at 3. Contexts per scope:
  - E1 "staged update runs suspended while waiting for approval": once suspended, the status is `Initialized@2`,
    `Started@1`, `Suspended=True@2` (stage and task conditions still @1), the finalizer is removed, and the role is
    released; approving while suspended changes nothing (status as is, request without `ApprovalAccepted`, binding
    untouched; Consistently). On resume: `Suspended=False/Resumed`, the approval is accepted, the binding moves, and the
    finalizer and the claim come back; the completed status has every run/stage/cluster condition @3 but the
    approval task's `ApprovalRequestCreated` @1 (behavior to note 7).
  - E2 "staged update runs suspended while a member cluster is being updated": the test waits for the persisted
    status to show cluster 0 `Started` (budget expiry), then suspends; the suspension lands once the in-flight
    reconcile ends (cluster 0 `Started@1`, cluster 1 untouched), finalizer removed and role released. Cluster 0 is
    marked available while suspended — no change (Consistently). On resume, cluster 0 completes without a new
    binding update, cluster 1 rolls out, and the run completes (all @3); cluster 0's `Started` transition time is
    not before the `Resumed` one (its wait time limit was reset).
  - E3 "suspending and resuming finished staged update runs": a completed run and an init-failed run (missing
    strategy) on the same policy; suspending and then resuming each moves only `Completed` (completed run: @2, @3;
    everything else stays @1) or `Initialized` (init-failed run) to the new generation, with no `Suspended`
    condition either way; the binding stays on the rolled-out snapshot.
  - The assembly script now checks that regenerating the cluster-scoped block from the namespaced one reproduces
    the existing cluster-scoped block; the only difference found (and accepted) was one comment in A6 now reading
    "cluster staged update run".
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 606 of 606 specs pass (~99s spec time,
    ~1m51s wall); go vet and golangci-lint: no findings in the test files.
- October 20, 2026: **tests updated for the review follow-up controller changes**:
  - binding manager conflicts are no longer reported as a condition (the `Started=False/
    FailedDueToBindingManagerConflicts` condition, its builder, and `bindingManagerConflictsMsgSubstr` are gone). A6
    is back to asserting that the run's status stays empty while it retries the claim (one `It` fewer per scope);
  - the per-cluster cancellation reason is now `UpdateCanceled` (C10 picks it up through the constant);
  - both suspended-binding shortcuts now report "(binding is suspended)"; C5 checks one substring
    (`bindingSuspendedMsgSubstr`) for both clusters, which also removes its dependence on which shortcut is taken;
  - the strategy-not-found message is now "staged update strategy cannot be found" (`strategyNotFoundMsgSubstr`).
  - Validation: `ginkgo -p --race --focus=...` on A6 and C5 (18 specs) and on C10 and C11 (38 specs): all pass; go
    vet and golangci-lint: no findings in the test files (604 specs in total).
  - The cluster worker loop in `processOneStage` then moved into `doClusterProcessingWork` (with a shared
    `perStageClusterProcessingTracker`), as the added cancellation check had pushed `processOneStage` over the
    `gocyclo` limit. `ginkgo -p --race --label-filter='!slow'`: 600 of 600 specs pass (~104s spec time, ~2m wall).
- October 20, 2026: **review follow-up: Consistently checks that could not fail, and a stale-cache race**:
  - A1 and E2: nothing reconciles a run that waits for approval or is suspended, so their Consistently checks held
    regardless of what the controller would do. Both now trigger a reconciliation first (A1 also checks the run
    status). Mutation check: with the controller changed to ignore a suspension after marking it, E2 fails at
    "should not process ... while it is suspended" in both scopes, and passes without the trigger.
  - A4: the reconciliation is now triggered without suspending the run (a suspension would also stop a run that the
    controller wrongly re-initialized), and the Consistently check covers the run status as well; the Eventually
    check that could pass on a stale (generation 1) cached copy is gone.
  - A3 uses the new `triggerStagedUpdateRunReconciliation` helper.
  - D3 and E1: `setApprovalRequestApproved` now waits for the cache, so their Consistently checks no longer race
    with the informer.
  - B6 had the same problem as A4 and got the same fix (trigger without suspending, Consistently check on the run
    status, the binding manager role, and the binding), plus an explicit check that the role claimed for the failed
    initialization is released. `triggerApprovalRequestNameTemplate` moved to `utils_integration_test.go`, as two
    files use it now. `ginkgo -p --race --focus=...` on B6 (10 specs): all pass (610 specs in total).
  - `removeObjects` now removes approval requests last, after everything else (runs included) is gone, so that a
    run that the cache still has cannot re-create a request that cleanup has just deleted (constraint 12).
    `ginkgo -p --race --label-filter='!slow'`: 606 of 606 specs pass (~96s spec time, ~1m49s wall).
  - B9: the stage layout is now checked after the rollout has started (the Resumed condition is set before the
    initialization step, so checking right after it could pass before the controller re-checks the stages).
  - A2: dropped the check that the run object stays around (nothing reconciled the run during that check, and the
    controller never deletes runs); A3's checks read the run anyway.
  - D5: both approval requests (canary and prod) are now checked for `ApprovalAccepted`, like the other approvals.
  - `createPlacementResourceSnapshot`'s comment no longer claims to match the snapshot manager's output (the
    snapshots lack the manager's annotations, so they suit only runs that name their snapshot explicitly).
  - Validation: `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun` (including the `slow` context): 610 of 610
    specs pass (~106s spec time, ~2m01s wall); go vet and golangci-lint: no findings in the test files.
  - Validation: `ginkgo -p --race --focus=...` on A1, A3, A4, E1, E2, and D3 (108 specs): all pass; go vet and
    golangci-lint: no findings in the test files (608 specs in total).
- October 27, 2026: **B11 implemented** in `initialize_integration_test.go` (context "selected resource missing",
  both scopes, after B8), following the placement resource snapshot manager change that reports a resource selected
  by name that does not exist as a user error (after confirming a cache miss with a read from the API server; the
  suite registers no informers, so the manager reads ConfigMaps from the API server directly). The policy selects a
  ConfigMap that is never created up front; the run (not suspended) ends with only `Initialized=False/Failed`, message
  containing "selected resource does not exist" (`selectedResourceNotFoundMsgSubstr`), and the binding manager role is
  released. Then the ConfigMap is created and a reconciliation triggered (without suspending the run, as in B6); a
  Consistently check covers the run status, the role, the binding (still on the previous snapshot), and the absence
  of the index 0 snapshot (which a re-initialization would create).
  - No existing test covered this: B7 always creates the selected ConfigMap, and B8's "not found" is about an
    explicitly specified snapshot.
  - Mutation check: with the snapshot manager reporting the missing resource as an API server error again (the
    previous behavior), the run is never marked as failed and both scopes fail at "should fail to initialize".
  - Validation: `ginkgo -p --race --focus="selected resource missing"`: 10 of 10 specs pass;
    `ginkgo -p --race ./pkg/v1/controllers/stagedupdaterun`: 620 of 620 specs pass (~150s spec time, ~2m39s wall);
    go vet and golangci-lint: no findings.
