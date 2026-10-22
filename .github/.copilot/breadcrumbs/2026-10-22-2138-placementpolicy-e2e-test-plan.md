# Placement Policy APIs (FEP-0001) E2E Test Plan

**Date**: October 22, 2026 21:38 AEDT
**Task**: Plan E2E coverage in [test/e2e](../../../test/e2e) for the `placement.kubefleet.dev/v1alpha1` and
`rollout.kubefleet.dev/v1alpha1` APIs (placement policies, bindings, snapshots, cluster claims, staged update runs,
approval requests, auto-rollout). So far only prerequisite P-2 (wiring auto-rollout into the hub agent) is
implemented. Questions 1–10 are resolved or deferred; Q10 is a known bug whose affected scenarios are skipped.
Q11 stays open while the user prepares a fix; Q12 is resolved (P-5). The E2Es themselves are waiting for approval.

## Requirements

1. Cover the user journeys that the controller ITs can't: real member clusters and member agents, the hub GC,
   status flowing back (member → Work → binding → policy), and handoffs between controllers (policy ↔ staged update
   run ↔ auto-rollout, through the binding manager role).
2. Do **not** port the CRP/RP E2Es 1:1. The new experience differs in ways that change what is worth testing (see
   [Experience differences](#experience-differences-vs-crprp)).
3. Follow repo conventions: Ginkgo/Gomega, `Ordered` contexts with step-wise `It`s, whole-struct `cmp.Diff`,
   `want` naming, per-process names (`GinkgoParallelProcess()`), and a new `Context` per scenario.
4. Stay inside the CI budget. The new lane also runs through `make e2e-tests`, which has a 70m timeout.

## Experience differences vs CRP/RP

These differences drive the scenario selection below.

| Area | CRP / RP | Placement policies | E2E implication |
| --- | --- | --- | --- |
| Initial placement | `RollingUpdate`: automatic; `External`: waits for an update run | The first snapshot is taken at creation, and new bindings carry the latest snapshot, so resources land with no rollout object | Assert placement without any run |
| Resource changes | Rolled out by the rollout strategy | **Nothing happens.** The policy reports `Synchronized=False/ResourcesChangedPendingRollout` until a staged update run or auto-rollout acts | Pending-rollout state is a first-class scenario |
| Scheduling changes | Bindings go through rollout or delete stages | Bind/unbind apply immediately (no delete stage). New bindings get the latest **existing** snapshot, which may be stale vs the hub | Scale up after a resource change gets the old content |
| Rollout vs scheduling | n/a | Mutually exclusive through `status.bindingManager`. Binding changes wait (5s→5m backoff) while a run or auto-rollout holds the role | Dedicated mutual-exclusion scenarios |
| Picking clusters | PickAll / PickN / PickFixed | A list of `ClusterSelector`s with `count` (int/`All`) and `minCount`. "Fixed" = match on the alias label | Overlapping selectors and count semantics |
| Stickiness | Scheduler semantics | A bound cluster keeps its binding when its labels/properties stop matching, or it is tainted/unhealthy. Only **selector term** changes re-evaluate. Exception, a **known bug (Q10)**: a cluster that starts leaving (deletion timestamp set) has its binding deleted at once. The expected behavior is to delete it only after the cluster has left. A fix will come separately | Sticky relabel and taint scenarios |
| Unfulfillable | Scheduling failure | A `ClusterClaim` is created (unless `KeepSearching`), and withdrawn once eligible clusters satisfy the selector. Nothing provisions clusters for claims in the E2E environment (decision 9) | Claim creation and withdrawal; fulfillment only by clusters the tests bring in |
| Namespace selection | Selecting a namespace takes its contents | A CPP selecting a `Namespace` places **only** the namespace object. Contents need selectors with `namespace` | Explicit scenario |
| Apply config | `ApplyStrategy` (incl. `ReportDiff`), overrides, envelopes | `SyncStrategy` only: no ReportDiff mode, overrides, or envelopes | Those areas are out of scope |
| Status | Per-cluster status on the placement | Per-binding status (counts, `failedResources`, diffs) plus aggregate counts on the policy | Assert both levels |
| Validation | Webhook + CEL | CEL + runtime (`ResourceCollected=False`). VAPs are being added separately and are out of scope here (Q8) | Runtime-error scenarios |
| Approvals | `kubectl fleet approve` | The plugin does not support the new `ApprovalRequest`, so tests patch the status `Approved=True` | Approve helper |

## Existing FEP-0001 E2E coverage (do not duplicate)

| File | Covers |
| --- | --- |
| [placement_policy_test.go](../../../test/e2e/placement_policy_test.go) | PP `region=east,count=2`: fulfilled, bindings name a snapshot, CM placed only on east. Unfulfillable PP: claim added, withdrawn on delete. CPP with fulfillable + unfulfillable selectors: claim refs CPP, cluster binding on west |
| [annotation_placement_test.go](../../../test/e2e/annotation_placement_test.go) | Alias label seeded. Generate / update / restore / recreate / invalid-annotation event / delete on annotation removal. API group carried. CPP generated for a Namespace |

Not covered at all: staged update runs and approvals, auto-rollout, sync strategies, availability, resource
selection errors, snapshots, stickiness, taints, deletion policies, interop with CRP/RP, and join/leave.

## Prerequisites and blockers

- **P-1 (done, 00:19)** Register `rollout.kubefleet.dev/v1alpha1` in the E2E scheme
  ([setup_test.go](../../../test/e2e/setup_test.go)). Only the placement group is registered today.
- **P-2 (done, 22:02)** Wired `pkg/v1/controllers/autorollout` into
  [setup.go](../../../cmd/hubagent/workload/setup.go) behind `enablePlacementPolicyAPIs`. It sits next to the staged
  update run controller, shares the placement resource snapshot manager, and uses the `auto-rollout-controller` event
  recorder. Its concurrency is a fixed 5 (P-5). The chart RBAC now grants `patch` on (cluster)placementpolicies for
  the timestamp merge patch. The existing `events.k8s.io` rule already covers its events.
- **P-3** Add shared v1alpha1 helpers (see [Helpers](#helpers)) in new files, separate from the legacy CRP helpers.
- **P-4 (done, 03:22; decided, Q5)** Give every new-API E2E its own label and lane. Tag all of them with
  `Label("placementpolicy")`, including the two existing FEP files and the join/leave scenarios (section L). Add a CI
  matrix entry `placementpolicy` running `make e2e-tests LABEL_FILTER="!custom && placementpolicy"` with the same
  settings as the other lanes. Add `&& !placementpolicy` to the default lane's filter. The joinleave lane
  (`!custom && joinleave`) needs no change, since new-API specs never carry the `joinleave` label. Implemented in
  [ci.yml](../../../.github/workflows/ci.yml), [placement_policy_test.go](../../../test/e2e/placement_policy_test.go),
  and [annotation_placement_test.go](../../../test/e2e/annotation_placement_test.go).
- Nit: [IMPLEMENTATION-NOTES.md](../../../pkg/v1/controllers/placementpolicy/IMPLEMENTATION-NOTES.md) still says
  "Not wired into the hub agent yet".
- **P-5 (done, 23:45; Q12)** The v1 setup no longer reads v1beta1 configuration, and no new flags were added (v1 is
  experimental).
  - **Hub ([setup.go](../../../cmd/hubagent/workload/setup.go)):** fixed concurrency for the work generator (10),
    staged update runs (5), and auto-rollout (5), set by the user at 23:54. These are lower than what
    `--max-fleet-size` and `--max-concurrent-cluster-placement` produced at their defaults (100/10/10), so the v1
    controllers now run with less concurrency than before.
  - **Member ([main.go](../../../cmd/memberagent/main.go)):** the v1 work applier uses a fixed concurrency of 5, a
    fixed cleanup wait of 5m (no longer `--deletion-wait-time`), and its own built-in rate limiters.
  - **Kept shared on purpose:** the placeability filters (skipped namespaces, GVK allow/deny lists,
    `enableWorkload`) and manager infrastructure.
  - **E2E knob:** the member agent reads `TEST_ONLY_PLACEMENT_POLICY_WORK_APPLIER_MAX_REQUEUE_DELAY_SECONDS` only
    when it is set. It caps every requeue delay of the v1 applier through the new
    `NewDefaultRequeueRateLimiterWithMaxDelay`, which reuses the default backoff shape.
    [setup.sh](../../../test/e2e/setup.sh) sets it to 5 with `kubectl set env` after each member-agent install, so the
    product chart gains no new value.
  - **Docs:** both chart READMEs note that the v1beta1 tuning values don't apply to the placement policy
    controllers.

## Environment facts that shape the design

- Members: `kind-cluster-1` (region=east, env=prod), `kind-cluster-2` (east, canary), `kind-cluster-3` (west, prod).
  Each carries the alias label (= cluster name), so a selector on the alias label is deterministic. Node counts are
  2 / 3 / 4 ([kindconfigs](../../../test/e2e/kindconfigs)), which makes `kubernetes-fleet.io/node-count` usable for
  property expressions. CI runs with the Azure property provider; gate on `isAzurePropertyProviderEnabled`.
- Scoring (fewer nodes; more available CPU/memory) depends on live capacity. **Never assert which cluster wins a
  ranked pick.** Use alias/label selectors, or assert relative facts (e.g., "the originally picked one is kept").
- Staged update run `refreshPeriod` is **30s** in the hub agent. Mid-run status can lag up to ~30s, so assert side
  effects (binding `spec.resourceSnapshotName`, member contents, approval requests, `bindingManager`) and rest
  points. Add `stagedUpdateRunEventuallyDuration` (~90s). `maxWaitTimePerClusterMinutes` is at least 1, so each
  timeout scenario costs ≥60s.
- **Staged update runs share only 5 workers (P-5)**, and a reconcile holds its worker for up to the 30s refresh
  period while clusters roll out. A cluster that never becomes available holds a worker for its whole
  `maxWaitTimePerClusterMinutes`, in 30s slices. The lane runs ~15 ginkgo processes in parallel (16-CPU runner), so
  several long-blocking runs at once would starve every other staged update run. Hence U7 runs its timeout runs one
  at a time, holding at most one worker. Waiting on an approval or a timed wait holds no worker, and ConfigMap
  rollouts finish in seconds, so the other U contexts stay parallel.
- Auto-rollout minimum wait is clamped to [1m, 1h] (default 3m). Any "second auto-rollout" costs ≥1m.
- The policy controller reconciles on **any** policy update (no predicate), so a released role is picked up
  promptly. Its periodic requeue is 150s (fulfilled) or 2m (unfulfilled). **Auto-rollout does not react to a role
  release**: it is not a relevant change to its event handler, so a blocked auto-rollout retries on its claim
  backoff (5s doubling, up to 180s by default).
- The v1 work applier auto-creates namespaces and leaves them behind. Use `cleanupWorkResourcesOnHubAndMembers()`.
- **The v1 work applier's requeue delays are capped at 5s in E2E (P-5).** The v1beta1 flags in setup.sh
  (`workApplierRequeueRateLimiterMax*BackoffDelaySeconds=5`) don't reach it; the test-only env var does. Changes
  made only on the member side (drift, a namespace created by hand) are therefore noticed within ~5s. Without the
  cap (production), a settled Work requeues after 15s growing 1.5× up to 15m. Likewise, setup.sh's
  `MaxConcurrentClusterPlacement=200` doesn't affect the v1 controllers, which use fixed concurrency.
- Suite setup creates two **virtual** `MemberCluster`s in every lane: `kind-unhealthy-cluster` (stale heartbeat) and
  `kind-left-cluster` (stuck mid-delete). Both are ineligible, so no-selector, `count: All`, NotIn and DoesNotExist
  policies never bind them, and `desired` for `count: All` counts only schedulable matches. Never assert on "all
  `MemberCluster`s".
- **No claim provisioner (decided, 9).** Nothing in the E2E environment fulfills cluster claims, and nothing will
  for now. Tests may assert that a claim is created, its contents and labels, `activeClusterClaims`, and its
  withdrawal. They must never wait for a provider: no `Completed` condition and no `provisionedClusterName`. A
  selector gets fulfilled only by a cluster the test itself brings in: an existing member relabeled to match (S13), or
  a cluster joining the fleet (L4). Specs that leave an unfulfilled selector behind must delete the policy, which
  withdraws its claims, so no claims leak between specs.
- Contexts that mutate `MemberCluster` labels or taints must be `Serial`, with `DeferCleanup` restoring them.
  Everything else should be parallel-safe: per-process namespaces and policy names, and claim names hash the
  policy's namespaced name. The existing FEP Describes are `Serial` without needing to be; consider relaxing that.

## Test plan

Priorities: **P0** = core journeys needed for alpha confidence; **P1** = important behaviors and likely regressions;
**P2** = edge cases and nice-to-haves. Scope: PP = `PlacementPolicy`, CPP = `ClusterPlacementPolicy`. Alternate
scopes across scenarios rather than doubling everything. Run both scopes only where code paths differ (staged
runs, claims, cross-namespace selection).

### S. Scheduling: `placement_policy_scheduling_test.go` (extends the existing file's coverage)

| # | Pri | Scope | Scenario | Key assertions |
| --- | --- | --- | --- | --- |
| S1 | P0 (implemented, passed on Kind) | PP | No `clusterSelectors` | All 3 clusters bound; `desired=scheduled=3`; CM on all |
| S2 | P0 (implemented, passed on Kind) | CPP | `count: All` + `env=prod` | Bindings on clusters 1 and 3 only; CM absent on 2 (Consistently) |
| S3 | P0 (implemented, passed on Kind) | PP | Overlapping selectors: A=`alias=cluster-1,count 1`, B=`env=prod,count 2` | Exactly 2 bindings (1, 3). Binding 1's `spec.clusterSelectors` lists A's and B's terms; binding 3 lists B's. `desired=scheduled=3` (per-selector sums), while `synchronized=available=2` (counted per bound cluster) |
| S4 | P0 (implemented, passed on Kind) | PP | Scale `region=east` count 1→2→1 | 2nd binding added; on scale-down the **newest** is trimmed and the originally picked cluster keeps its binding (same UID) and CM. The scale-up waits out the first binding's creation second (Q14) |
| S5 | P0 (implemented, passed on Kind) | PP, Serial | Sticky: `env=canary,count 1` binds 2, then relabel cluster-2 `env=prod` | Binding and CM kept (Consistently), still counted. Then change the selector **terms** → re-evaluated → unbound, CM removed, claim added |
| S6 | P1 | CPP | `region=east,count 3,minCount 2` | `scheduled=2`, `Scheduled=False/FailedToFindSomeRequiredClusters` (message shows floor met), claim added. **Pins the current behavior as intended (Q7):** reaching `minCount` but not `count` reports `Scheduled=False` |
| S7 | P1 | PP | `whenUnfulfilled: KeepSearching` on an unfulfillable selector | No claim (Consistently), `activeClusterClaims=0`, `Scheduled=False` |
| S8 | P1 | PP | `matchLabelExpressions` with In / NotIn / Exists / DoesNotExist, all with `count: All` (one Ordered context, swapping terms; each swap is a term change, so the set is re-evaluated) | Bound set follows each change. Without `count: All`, the default count of 1 would bind a single score-ranked cluster |
| S9 | P1 | CPP | `matchClusterPropertyExpressions` `node-count Ge 3`, then `Eq 2`, both with `count: All` (gated on the property provider) | {2, 3}, then {1}. Node counts include the control plane; the legacy pick-all test expects the same {2, 3} for `Ge 3` |
| S10 | P1 | PP | Runtime-invalid selector (`Gt` inside `matchLabelExpressions`) | `Scheduled=False/InvalidClusterSelectors`; existing bindings untouched; fixing the spec recovers |
| S11 | P1 | PP, Serial | Taints: taint cluster-2 NoSchedule; `region=east,count All` | Only 1 bound; adding a toleration binds 2 |
| S12 | P1 | PP, Serial | Taint after bind: `env=prod,count 1` binds X, then taint X | X keeps binding and CM, not counted; the other prod cluster Y is bound. Untaint → overpick trimmed: Y unbound, X kept |
| S13 | P1 | CPP, Serial | An **existing** cluster starts matching: `region=south,count 1` → claim (never fulfilled); relabel cluster-3 `region=south` | Binding on 3, CM placed, claim withdrawn, `activeClusterClaims=0`, `Scheduled=True`. The new-cluster variant is L4 |
| S14 | P2 | PP | Term change on an unfulfilled selector | Claim replaced under the **same name** (new UID) with the new terms |
| S15 | P2 | CPP | Delete a binding out of band | The work generator deletes that binding's Works, so the member's resources are removed (default `CleanUpResources`). The policy then recreates the binding, and the resources come back as new objects (new UIDs). Pin this churn |

### R. Resource selection and snapshots: `placement_policy_resources_test.go`

| # | Pri | Scope | Scenario | Key assertions |
| --- | --- | --- | --- | --- |
| R1 | P0 (implemented, passed on Kind) | PP | Label-selected CMs; then edit one and add another matching CM on the hub | Initial set placed. After the edits: `Synchronized=False/ResourcesChangedPendingRollout`; `latestResourceRevisionName` unchanged; members keep old data (Consistently) |
| R2 | P0 (implemented, passed on Kind) | PP | Pending change + scale up (count 1→2) | The new cluster gets the **latest existing** (old) snapshot's content, not the hub's current content |
| R3 | P0 (implemented, passed on Kind) | CPP | Select a `Namespace` and a `ClusterRole` by name; then delete the CPP and **re-create** it with a CM selector (with `namespace`) added | Only the namespace (owned by an AppliedWork) and the ClusterRole are placed, not the namespace's CMs (Consistently). Deleting the CPP removes both from the members. After waiting for the old bindings and snapshots to be garbage-collected, the re-created CPP places all three. Adding the selector to the live CPP instead would only report `ResourcesChangedPendingRollout` (a selector change is a resource change), hence the re-create (decision 13) |
| R4 | P0 (implemented, passed on Kind) | PP | Selection errors: `namespace` ≠ the PP's; a named CM that is missing (two contexts) | `ResourceCollected=False/FailedToCollectSomeResources`, `Synchronized=False/FailedToSynchronizeSomeClusters`, `Available=False`, no latest revision, no bindings (Consistently). Fixing the namespace (to the PP's own) or creating the CM → `True` and placed. Once the separate VAPs land, the namespace-mismatch case may be rejected at admission instead; adjust it then |
| R5 | P1 | PP/CPP | Hub eligibility: a PP in `default`; a CPP naming `kube-root-ca.crt`; a label selector that also matches an owned CM | Both by-name cases → `ResourceCollected=False`. The owned CM is silently skipped |
| R6 | P1 | CPP | 60 CMs (> 50 per snapshot) | Primary + secondary snapshots (sub-index/count labels). Per cluster: primary + secondary Works (`linked-work-count`). All 60 on the members; binding `selectedResources=60` |
| R7 | P1 | CPP | `resourceRevisionHistoryLimit: 1` + 3 staged update runs whose strategy leaves cluster-3 out (U9's setup) | Old snapshots GC'd, **except** the one cluster-3's binding still references. Auto-rollout can't produce this state, since it moves every binding |
| R8 | P2 | PP | Delete a label-selected CM on the hub, then roll out | Removed from the members (leftover-manifest cleanup) |

### A. Sync strategy, status, and availability: `placement_policy_sync_strategy_test.go`

The goal is real-member behavior plus the status contract (binding `failedResources`/counts → policy conditions).
The [v1 work applier ITs](./2026-10-21-2045-workapplier-v1-integration-test-plan.md) already cover the full matrix,
so this section picks a representative subset.

| # | Pri | Scenario | Key assertions |
| --- | --- | --- | --- |
| A1 | P0 (implemented as CPP, passed on Kind) | Pre-existing unmanaged CM on cluster-1, default `whenAlreadyExists=ReportError` | Binding `failedResources` has the CM with `Applied=False` (NotTakenOver); policy `Synchronized=False/FailedToSynchronizeSomeClusters`; `synchronizedClusters` excludes 1. Then set `AlwaysTakeOver` on the policy → binding `syncStrategy` updated **without a rollout** → taken over → `Synchronized=True` |
| A2 | P0 (implemented, passed on Kind) | `whenDrifted: ReportError`; edit the CM on the member | `FoundDrifts` with `diffDetails` on the binding. Revert → recovers. Contrast with the default `ApplyAnyway` (drift overwritten). Detection waits for the applier's periodic requeue, which the E2E cap keeps within ~5s (P-5) |
| A3 | P0 (implemented, passed on Kind) | `whenPlacementDeleted: OrphanResources`, then delete the policy | Resources stay on the members with no KubeFleet owner refs; hub bindings, Works, and snapshots are gone |
| A4 | P0 (implemented, passed on Kind) | Deployment placed (default availability) | Binding `Available=True/AllResourcesAvailable`, counts set; policy `resourcesAvailableClusters=N`, `Available=True` |
| A5 | P1 | Deployment with an unpullable image | Binding `Available=False/SomeResourcesUnavailable`; policy `Available=False/ResourcesUnavailableOnSomeClusters` |
| A6 | P1 | `TakeOverIfNoDiff` with a differing pre-existing CM, partial vs full comparison | `FailedToTakeOver` + `diffDetails`; a field present only on the member matters only under full comparison |
| A7 | P1 | `whenNamespaceDoesNotExist: ReportError`; select only a CM whose namespace is not on the member | First wait until the member namespace from earlier specs is fully gone: per-process namespace names are reused, and cleanup is async. Then: failed apply reported; creating the ns on the member → applied (ns unmanaged). D1 pins the default `CreateNamespace` leftover |
| A8 | P1 | `whenOwnedByOthers` with a member CM owned by another object (+ AlwaysTakeOver) | `ReportError` → takeover fails; `ShareOwnership` → co-owned |
| A9 | P2 | `ServerSideApply` (+ `forceConflicts`) on a field owned by another manager | Applied with forced ownership; without force → apply failure |

### X. Interop and conflicts: `placement_policy_interop_test.go`

| # | Pri | Scenario | Key assertions |
| --- | --- | --- | --- |
| X1 | P1 | A CRP (PickFixed cluster-1) places the app namespace; a PP selects the same CM to cluster-1, even with `AlwaysTakeOver` + `ShareOwnership` | PP binding reports `OwnedByOtherKubeFleetAPIObjects`; the CRP stays healthy and unchanged |
| X2 | Deferred | Reverse: the PP places first, then the CRP selects the same namespace (default, then `allowCoOwnership: true`) | **Expected (Q3): blocked in both cases.** Today the legacy applier only treats v1beta1 AppliedWork owners as KubeFleet duplicates ([apply.go](../../../pkg/controllers/workapplier/apply.go)), so `allowCoOwnership` may still permit dual management. The fix comes in a separate PR; add X2 there, not in this E2E work |
| X3 | P1 | Two PPs (same ns) select the same CM to the same cluster; create the second only after the first is synchronized | With the default `ReportError`, the second binding reports `NotTakenOver`; with `AlwaysTakeOver`, it reports `FailedToTakeOver` (another v1 AppliedWork owns the CM). The first stays synchronized. Deleting the first removes the CM, then the second creates it |

### U. Staged update runs: `placement_policy_staged_update_run_test.go` (U1–U3), plus `_stages` (U4, U5, U9), `_interruptions` (U6, U8), and `_failures` (U7, U10)

Each scenario runs in both scopes where noted (PP + `StagedUpdateRun` + `StagedUpdateStrategy` in the same namespace;
CPP + the `Cluster*` variants). Common strategy: stage `canary` (`env=canary`, after-stage Approval) → stage `prod`
(`env=prod`). The policy is placed first (initial content v0), then the hub CM is edited to v1.

| # | Pri | Scope | Scenario | Key assertions |
| --- | --- | --- | --- | --- |
| U1 | P0 (implemented, passed on Kind) | both | Happy path, run with no `resourceSnapshotName` | `Initialized=True`; a new snapshot is created and the policy's `latestResourceRevisionName` moves; status `resourceSnapshotNameToRollout` set. Canary gets v1 while prod stays v0 → policy `SomeClustersOnOlderResourceRevision`. Approval request created (name from `status.stages[].afterStageTasks[].approvalRequestName`, owned by the run). Prod untouched while unapproved (Consistently). Approve → `ApprovalAccepted` → prod gets v1 → `Completed=True/Succeeded`; stage/cluster statuses (`cmp.Diff`, ignore times); policy `Synchronized=True`; `bindingManager` nil |
| U2 | P0 (implemented, passed on Kind) | PP | Rollback: a run with `resourceSnapshotName` = the index-0 snapshot (after a forward run took index 1; single-stage strategy with an empty selector) | Members return to v0; policy reports `SomeClustersOnOlderResourceRevision` (latest is still index 1); pin this, along with `synchronized=available=0` and `Available=False/SomeClustersUnavailable` (availability is judged on the latest revision only) |
| U3 | P0 (implemented, passed on Kind) | CPP | Mutual exclusion. Policy selectors `env=canary,count 1` + `env=prod,count 1` (binds 2 and one prod cluster X). While the canary approval is pending, raise the prod selector to `count 2` (no relabel, no Serial) | No binding for the other prod cluster Y while the run holds the role (Consistently). After approval and completion → Y bound with the **new** snapshot → Y gets v1. Also pin the policy status while it is deferred: `desired=scheduled=3` (Y already counts), `synchronized=available=1`, `SomeClustersOnOlderResourceRevision`, `bindingManager` = the run |
| U4 | P1 (implemented, passed on Kind) | PP | After-stage TimedWait (~30s) | Prod does not start before the wait elapses (stage timestamps); then completes |
| U5 | P1 (implemented, passed on Kind) | CPP | Before-stage Approval on `prod` | Canary done, prod blocked until approved |
| U6 | P1 (implemented, passed on Kind) | both | Created `suspended: true` | Initialized and snapshot chosen; `Suspended=True`; nothing rolled out (Consistently). Unsuspend → `Suspended=False/Resumed` → proceeds. Suspend while waiting on approval → approving does not advance until resumed |
| U7 | P1 (implemented, passed on Kind) | both | Failure policy, `maxWaitTimePerClusterMinutes: 1`. (a) `maxAllowedClusterFailures: 0`, v1 = Deployment with a bad image. (b) `maxAllowedClusterFailures: 1` with exactly one bad cluster. The new API has no overrides, so every cluster gets the same manifest; make one cluster fail with a member-side precondition, e.g., pre-create the namespace on that member with a ResourceQuota that blocks pods | (a) First cluster `FailedToUpdate` → `Completed=True/Failed`; later stages untouched; role released. (b) `PartiallySucceeded`. Run the four runs **one at a time** in an Ordered context (not `Serial`), so U7 holds at most one of the 5 staged update run workers and the rest of the lane keeps going (see the environment facts). Allow U7 more time: ~1–1.5 min per run, ~5–6 min in total, running alongside the lane |
| U8 | P1 (implemented, passed on Kind) | PP | Run deleted mid-way (waiting on approval) | Approval requests GC'd; role released; partial state kept. A new run completes the rollout |
| U9 | P1 (implemented, passed on Kind) | CPP | Strategy whose stages miss cluster-3 | Run `Succeeded`; cluster-3 stays on the old revision; policy `SomeClustersOnOlderResourceRevision`. Ties to R7 (referenced snapshot not GC'd) |
| U10 | P1 (implemented, passed on Kind) | PP | Init failures: missing strategy; missing policy; `resourceSnapshotName` from another policy; missing `sortingLabelKey` label | `Initialized=False/Failed` (terminal); nothing rolled out |
| U11 | P2 | CPP | `maxConcurrency` `3` and `70%` on a 3-cluster stage | Resolved `status.stages[].maxConcurrency` (3 and 2); all clusters end updated |
| U12 | P2 | PP | Hand-made approval request with the run's deterministic name, created before the run reaches the stage (name computed with the format in [uniquename.go](../../../pkg/v1/controllers/stagedupdaterun/uniquename.go)) | `ApprovalRequestCreated=False/RequestOwnershipMismatch`. Deleting it → the run creates its own request, which then needs approval as usual |
| U13 | P2 | PP | Rolling out an annotation-generated policy (name found via provenance labels) | Shows annotation placement and runs work together |
| U14 | P2 | CPP | `binding.spec.suspended=true` set by hand mid-run (see section B) | That cluster counts as succeeded; its resources are removed (work generator) |

### AR. Auto-rollout: `placement_policy_auto_rollout_test.go`

| # | Pri | Scenario | Key assertions |
| --- | --- | --- | --- |
| AR1 | P0 (implemented as PP, passed on Kind) | `auto-rollout-enabled: "true"`, min wait `1m`; edit the selected CM | All bound clusters get v1 with no run; the `last-rollout-attempt-timestamp` annotation is written; `Synchronized=True`; `bindingManager` nil |
| AR2 | P0 (implemented, passed on Kind) | Annotation absent, then `"false"` | Pending rollout persists (Consistently ≥30s) |
| AR3 | P1 (implemented, passed on Kind) | Second edit right after AR1 | Not rolled out before timestamp + 1m; rolled out after (~1m cost) |
| AR4 | P1 (implemented, passed on Kind) | Coexistence, in this order: auto-rollout policy synchronized → create a staged run (it claims the role and waits on the canary approval) → edit the CM | While the run holds the role, auto-rollout does not act: prod keeps the old content (Consistently ~15s; keep this window short so the claim backoff stays small). Approve → the run completes and releases the role → auto-rollout rolls out the edit it was blocked on. Auto-rollout does not react to the role release; it retries on its claim backoff (5s doubling, up to 180s), so allow up to ~3 min |
| AR5 | P1 (implemented, passed on Kind) | Annotation-generated policy: the user sets up rollout separately by adding the auto-rollout annotation to it (the annotation controller leaves extra annotations alone) | Edits to the annotated resource roll out. Do **not** pin what a generated policy does without the annotation; that default is undecided (Q4) |
| AR6 | P2 | Invalid / out-of-range `minimum-wait-period` | Pin clamping and defaulting only if cheap. Otherwise leave to the ITs |

### L. Member cluster lifecycle: `placement_policy_join_leave_test.go` (`placementpolicy` lane)

These run in the new lane rather than the joinleave lane (Q5). Every L context is `Serial`, so it runs only after the
lane's parallel specs finish. Each one rejoins every cluster it removed and restarts any agent it stopped in
`DeferCleanup`, so the fleet is whole for whatever runs next.

| # | Pri | Scenario | Key assertions |
| --- | --- | --- | --- |
| L1 | **Skipped (Q10)** | `env=prod,count All` CPP; cluster-3 leaves | **Expected:** the binding stays while cluster-3 is leaving and is deleted only after it has left. Then Works are gone, counts drop, there is no claim, and the resources are **kept** on cluster-3 (orphaned, no owner refs). Today the binding is deleted as soon as cluster-3 starts leaving, a known bug to be fixed separately. Add L1 with the fix |
| L2 | **Skipped (Q10)** | Rejoin cluster-3 (default `ReportError`), then take the resources over again with `TakeOverIfNoDiff` (unchanged resources) and `AlwaysTakeOver` | **Expected (Q2):** resources left behind by a cluster that left are unowned on rejoin and must be taken over again. Default → `NotTakenOver` on the binding's `failedResources`; policy `Synchronized=False`. Switching the policy's `whenAlreadyExists` (no rollout needed) → re-adopted, owner refs restored, `Synchronized=True`. Needs L1's leftover resources, so it lands with the fix. Until then, B2 covers the same re-takeover mechanics through suspension |
| L3 | P2 | Member agent stopped on a bound cluster (heartbeat stale, ~3m) | Binding kept, not counted, gap filled. Agent back → overpick trimmed (newest goes). Expensive |
| L4 | P0 (skipped for now, per the user) | A **new** cluster fulfills a waiting selector. With cluster-3 out of the fleet, create a CPP selecting `fleet-e2e/new-cluster=true,count 1` → claim created. Then rejoin cluster-3 via `createMemberCluster` with its usual labels plus `fleet-e2e/new-cluster=true` | **Precondition:** before cluster-3 leaves, assert that no (cluster) placement binding targets it, so the Q10 bug can't affect this test. Before the join: `Scheduled=False`, one claim carrying the selector terms, and the claim stays unfulfilled (Consistently: no `Completed`, no `provisionedClusterName`). After cluster-3 joins and is healthy: binding on 3, CM placed, claim withdrawn, `activeClusterClaims=0`, `Scheduled=True`. Remove the extra label in `DeferCleanup` |
| L5 | P1 | A matching cluster that registers but never joins: L4's setup, but the new cluster is a `MemberCluster` with no agent (per-process name, matching label) | No binding; the claim stays active (Consistently), since withdrawal requires an eligible cluster. Follow the existing virtual-cluster pattern ([scheduler_watchers_test.go](../../../test/e2e/scheduler_watchers_test.go)): create with `createMemberCluster` and clean up with `ensureMemberClusterAndRelatedResourcesDeletion`, which strips finalizers, so there is no force-delete wait |
| L6 | **Deferred to the Q10 fix** | Hub-side timing of a leave, on a **virtual** cluster: create it, mark it healthy, let a `count: All` policy bind it, then delete its `MemberCluster` | With no agent to report the leave, the hub member-cluster controller keeps the `MemberCluster` in `Terminating` until force delete (`forceDeleteWaitTime` is 1m in the E2E hub). **Expected:** the binding is kept throughout that window (Consistently) and deleted once the cluster is gone. This fails today. It checks binding timing deterministically and needs no leave/rejoin of a real cluster. Only L1 can show that the resources are kept, since a virtual cluster has no member side |

L4 stands alone in Phase 1, with its own leave and rejoin; its precondition keeps it clear of the Q10 bug. Once the
Q10 fix lands, L1, L2 and L4 can share one Ordered `Serial` context (leave → L1 → create L4's policy → rejoin with
the extra label → L4 → L2), so cluster-3 leaves and rejoins only once. L4's policy must then select a different
ConfigMap than L1's; otherwise the two policies conflict over the same object on cluster-3 (as in X3).

### B. Binding suspension: `placement_policy_binding_suspension_test.go`

Users don't normally edit `binding.spec.suspended`; upcoming APIs will build on it (Q6). These tests edit it directly
as a stand-in for those APIs, to cover the mechanism end to end. On suspension the work generator deletes the
binding's Works and clears `status.lastProcessedResourceSnapshotName`. The member applier then removes the resources
according to `whenPlacementDeleted`. The placement policy controller never touches the field.

| # | Pri | Scope | Scenario | Key assertions |
| --- | --- | --- | --- | --- |
| B1 | P1 | PP | Suspend one binding of a 3-cluster policy (no rollout in flight), then unsuspend it | Suspended: that cluster's Works are gone, its resources are removed, and `lastProcessedResourceSnapshotName` is cleared. The other clusters are untouched, and the binding stays suspended with no replacement binding created (Consistently). Pin the policy's counts and conditions as reported. Unsuspended: Works recreated, resources back, `Synchronized=True` |
| B2 | P1 | CPP | B1 with `whenPlacementDeleted: OrphanResources` | Suspending leaves the resources on the member, orphaned with no KubeFleet owner refs. After unsuspending, the binding reports `NotTakenOver` until the policy switches `whenAlreadyExists` to a takeover option (cf. L2) |
| B3 | P2 | PP | Suspended binding + a resource change rolled out (auto-rollout) | The suspended binding's `resourceSnapshotName` moves but nothing is applied to that cluster. On unsuspension it gets the latest content |

### D. Deletion and ownership: `placement_policy_lifecycle_test.go`

| # | Pri | Scenario | Key assertions |
| --- | --- | --- | --- |
| D1 | P0 (implemented, passed on Kind) | Delete a policy (default `CleanUpResources`) | Explicitly: bindings, Works (`fleet-member-*` ns), and snapshots gone on the hub; AppliedWorks and resources gone on the members; auto-created member ns remains (pinned) |
| D2 | P1 | Delete with `propagationPolicy: Orphan`, then recreate the same name | Orphaned bindings **adopted** (same UIDs, new controller ref); member CM not churned (resourceVersion unchanged). **Pending the Q11 fix:** the snapshots are orphaned too and are currently reused without an owner ref. Once the fix lands, assert snapshot ownership and what deleting the recreated policy cleans up. Until then, implement only the binding-adoption part |
| D3 | P1 | Delete the PP's hub namespace | Policy, bindings, snapshots, Works, and member resources all cleaned up; no leaked claims |
| ~~D4~~ | Dropped | ~~CEL smoke~~ | The chart's CRDs are symlinks to `config/crd/bases`, so an E2E smoke would verify nothing an envtest API-validation IT doesn't. The CEL rules belong in that IT (none exists for `kubefleet.dev` today; see "Left to ITs/UTs") |

## Helpers

New files `placement_policy_utils_test.go` and `placement_policy_actuals_test.go`. Make them scope-agnostic through the
`*Accessor` interfaces in [interface.go](../../../apis/kubefleet.dev/placement/v1alpha1/interface.go):

- Builders: `newPolicy(scope, name, ns, selectors, resourceSelectors, syncStrategy)`, `aliasSelector(cluster)`,
  `labelSelector(k, v, count)`, `newStrategy` / `newRun` (both scopes).
- Actuals:
  - `policyStatusActual(key, wantConds, wantCounts)`: `cmp.Diff`, ignoring `LastTransitionTime`/`Message`.
  - `bindingsActual(key, wantClusterToSnapshot)`.
  - `bindingFailedResourcesActual`.
  - `snapshotsOfPolicy` / `latestPrimarySnapshot`.
  - `worksOfBinding`.
  - `stagedUpdateRunStatusActual`: ignoring timestamps and messages.
  - `appConfigMapDataOnClustersActual(wantData, clusters...)`.
  - `orphanedOnClusterActual`: object present, no KubeFleet owner refs.
- Mutators: `updateAppConfigMapData`, `approveApprovalRequest(key)` (status update `Approved=True`),
  `setBindingSuspended(key, bool)`, and `setMemberClusterLabels` / `addTaint` with `DeferCleanup` restore. All
  mutators retry on conflict.
- Claims: `claimsOfPolicyActual(key, wantTerms)` (asserts the claim stays unfulfilled) and
  `noClaimsOfPolicyActual(key)`.
- Fleet changes: reuse `setMemberClusterToLeave` / `checkIfMemberClusterHasLeft` and `createMemberCluster` /
  `checkIfMemberClusterHasJoined` from [utils_test.go](../../../test/e2e/utils_test.go). Add
  `rejoinMemberClusterWithExtraLabels(cluster, labels)` for L4.
- Reuse `createWorkResources`, `cleanupWorkResourcesOnHubAndMembers`, `claimsOfPolicy`, and
  `setClusterSelectorsAnnotation`.

## Left to ITs/UTs (not E2E)

The full sync-strategy matrix, waves and backoff, scoring math, snapshot naming and hashing, claim budget with
provisioner finalizers, CEL rule matrix, run status internals, and auto-rollout requeue arithmetic. Claim
**fulfillment** by a provisioner (the `Completed` condition, `provisionedClusterName`, and replacing a completed
claim) is out of E2E scope until a provider exists (decision 9).

## Phasing and rough cost

| Phase | Content | Est. added wall clock |
| --- | --- | --- |
| 0 | P-1, P-3, P-4 (scheme, helpers, label/lane) | none |
| 1 | P0: S1–S5, R1–R4, A1–A4, U1–U3, AR1–AR2, D1, and L4 (Serial; it brings in the leave/rejoin helpers early) | ~18–23 min + ~3 min serial |
| 2 | P1 across sections (U7 runs its four timeout runs one at a time, alongside the lane). L1/L2 are skipped (Q10) | ~20 min |
| 3 | P2 + remaining L (Serial, same lane) | ~10 min + ~6 min serial |

The estimates are for the `placementpolicy` lane only, since it no longer shares the default lane's 70m budget. Serial
contexts (S5, S11–S13, all of L) run one after another at the end of the lane, so keep them small. Measure after
Phase 1 and revisit the estimates. Summed, the phases (~57–62 min) plus the two existing FEP files come close to the
70m ginkgo timeout that `make e2e-tests` hard-codes. Before Phase 3, either make the timeout configurable for this
lane or keep the P2s out.

## Open questions

1. ~~**Auto-rollout wiring (P-2):** wire it now, or park AR?~~ **Resolved:** wire it now (done; see P-2). The AR
   P0s move into Phase 1.
2. ~~**Rejoin:** is `NotTakenOver` on rejoin (default `ReportError`) intended, or a bug?~~ **Resolved:** expected.
   Resources previously applied by a cluster that left and rejoined may need to be taken over again; L2 pins this
   once the Q10 fix lands (B2 covers the re-takeover mechanics until then).
3. ~~**X2:** should the legacy applier refuse objects owned by the new AppliedWork even with `allowCoOwnership`?~~
   **Resolved:** yes, it should be blocked. That is left alone for now; the fix and X2 come in a separate PR.
4. ~~**Annotation placement:** should generated policies enable auto-rollout by default?~~ **Deferred:** leave it
   alone for now and decide later. Tests assume rollout is set up separately (AR5: the auto-rollout annotation on
   the generated policy; U13: a staged update run) and do not pin the default for generated policies.
5. ~~**CI:** a dedicated `placementpolicy` lane, or keep the default lane?~~ **Resolved:** a dedicated lane and label
   for every new-API E2E, including the existing FEP files and section L (see P-4).
6. ~~**Binding suspension:** is `binding.spec.suspended` user-facing (U14), or internal only?~~ **Resolved:** users
   don't typically edit it directly (upcoming APIs build on it), but the E2Es may edit it. Section B was added and U14
   is kept.
7. ~~**`minCount`:** reaching the floor reports `Scheduled=False` (notes item 4). Pin it in S6, or wait for the FEP
   decision?~~ **Resolved:** the current behavior (`Scheduled=False`) is the way to go for now; S6 pins it.
8. ~~**VAPs:** the API comment says some validations moved to VAPs, but none ship. Planned?~~ **Resolved:** VAPs are
   added separately; these E2Es don't touch them.
9. **Cluster claims (raised in review):** **Decided:** treat claims as never fulfilled. No provider resolves them now.
   Tests may cover claim creation but must not rely on fulfillment. They must still cover new clusters fulfilling a
   specific selector: L4 (a cluster joins), S13 (an existing cluster starts matching), and L5 (a cluster that never
   joins does not count).
10. **Leave race (from the 22:47 review):** **Resolved, known bug.** The expected behavior is that a leaving
    cluster's binding is deleted only **after the cluster has left**. Today the placement policy controller deletes it
    as soon as the `MemberCluster` gets a deletion timestamp, before the agent has left. This is unexpected and will
    be fixed separately. Until then, L1 and L2 are skipped, and L6 (a deterministic hub-side check on a virtual
    cluster) is deferred to the fix PR. L4 asserts a precondition that keeps it clear of the bug. Details, for the fix:
    - **Why:** the eligibility checker reports a deleting cluster as ineligible, and `evaluateSelectors` keeps it in
      neither the schedulable nor the sticky set ("A cluster on its way out is left out, so that its binding is
      released", [scheduling.go](../../../pkg/v1/controllers/placementpolicy/scheduling.go)). The member-cluster
      watch fires on the deletion timestamp.
    - **Probe (23:09):** an envtest probe confirmed this. A bound cluster held in `Terminating` by a finalizer lost
      its binding within seconds. The existing IT ("unbinds a cluster that leaves the fleet") deletes a
      finalizer-less `MemberCluster`, so it never covered the leaving window.
    - **Race:** the hub member-cluster controller keeps the cluster's namespace and Works until the agent reports it
      has left. The binding deletion bypasses that: the work generator deletes the Works right away. If the member
      applier handles that deletion before the member agent calls `Leave()`, it deletes the resources (default
      `CleanUpResources`). `Leave()` sets the applier not ready and orphans AppliedWorks of Works that still carry its
      finalizer. Which happens first is a race between two chains of watches.
    - **Legacy contrast:** the legacy scheduler doesn't delete such a binding; it marks it `Unscheduled`. Deletion
      then goes through the rollout controller (gated by the unavailability budget, so legacy can race too), or
      under the External strategy through the update run's delete stage. That is what makes the staged-run E2E's
      "resources are kept" deterministic.
11. **Orphan-cascade recreate (from the 22:47 review; affects D2):** **Open, fix in progress (by the user).** Not
    intended. Leave this open until the user confirms the fix has landed. Deleting a policy with
    `propagationPolicy: Orphan` also orphans its snapshots. The recreated policy finds them by label and reuses them
    without an owner ref, since `retrieveLatestSnapshot` and `isSnapshotUpToDate` don't check ownership. Paths that
    do check, such as `IsSnapshotSynchronized` and the GC's in-use check, expect one. Until the fix lands, D2's
    snapshot assertions stay pending; its binding-adoption part is unaffected.
12. ~~**v1 applier backoff in E2E (from the 22:47 review):** wire the `workApplierRequeueRateLimiter*` flags into the
    v1 work applier, so E2E can cap its backoff?~~ **Resolved (done, see P-5):** don't share configuration. The v1
    controllers in both agents get fixed static values and no new flags, since v1 is experimental. E2E caps the v1
    applier's requeue delays through a test-only env var that `setup.sh` sets.
13. ~~**R3 contents (raised while implementing, 01:09):** adding the CM selector to the live CPP is a resource change,
    so the policy only reports `ResourcesChangedPendingRollout` and places nothing until a rollout.~~ **Decided:**
    delete and re-create the CPP with the selector added. Before re-creating, R3 waits for the deleted CPP's bindings
    and snapshots to be garbage-collected. Binding names are deterministic, and `retrieveLatestSnapshot` doesn't check
    ownership (the Q11 root cause), so a quick re-create could pick up the old snapshot.
14. **Scale-down tie-break (found on Kind, 01:50):** **Open, for the user to decide.** `chooseClusters` trims the
    newest bindings by `creationTimestamp`, which has one-second resolution, and breaks ties by cluster name. Two
    bindings created within the same second (seen on Kind: 14:40:00.53 and 14:40:00.90) tie, so a scale-down can
    trim the earlier pick when its cluster name sorts later. The code comment ("oldest and name breaking ties")
    documents the tie-break, but "a scale-down undoes the latest picks" holds only at one-second granularity. S4
    works around it by scaling up only after the first binding's creation second has passed. Options: accept it as
    a documented limitation, or record a finer bind time on the binding (e.g., an RFC3339Nano annotation) for the
    ordering.

## Status

- 21:38 — Plan drafted from the API types, the existing FEP E2Es, the controller code and notes, and the IT plans for
  the staged update run, auto-rollout, v1 work applier, and snapshot manager. Waiting for review and answers to the
  open questions before implementation.
- 22:02 — Q1 answered: wire auto-rollout now. P-2 is implemented in
  [setup.go](../../../cmd/hubagent/workload/setup.go) and the hub-agent chart
  [rbac.yaml](../../../charts/hub-agent/templates/rbac.yaml). Validation: build, vet, `make lint` (0 issues), and
  `helm template` (`patch` rendered with the flag on). The controller name `auto-rollout` is unique in the manager.
  The auto-rollout IT suite builds the controller the same way (`New` + `SetupWithManager` +
  `mgr.GetEventRecorder`) and passes (325s). Not verified live on Kind, since Docker is unavailable in this
  environment; AR1–AR2 will cover it. Questions 2–8 are still open.
- 22:08 — Q2 answered: `NotTakenOver` on rejoin is expected, since previously applied resources may need to be taken
  over again. L2 now pins it and adds the re-takeover path. Questions 3–8 are still open.
- 22:09 — Q3 answered: the legacy applier should block objects owned by the new APIs even with `allowCoOwnership`.
  That will be addressed in a separate PR, so X2 is deferred to it and X1/X3 stay as planned. Questions 4–8 are
  still open.
- 22:10 — Q4 deferred: no change to annotation placement for now. Tests assume rollout is set up separately, and
  AR5 no longer pins the default for generated policies. Questions 5–8 are still open.
- 22:12 — Q5 answered: every new-API E2E gets the `placementpolicy` label and its own CI lane. P-4 is updated.
  Section L moves from the joinleave lane into the new lane as `Serial` contexts that restore the fleet. Questions
  6–8 are still open.
- 22:13 — Q6 answered: `suspended` is not normally edited by users (upcoming APIs build on it), but the E2Es may edit
  it. Added section B (B1–B3) after confirming the work generator and placement policy controller behavior. U14 no
  longer depends on Q6. Questions 7–8 are still open.
- 22:17 — Q7 answered: keep the current `minCount` behavior (`Scheduled=False` when only the floor is met); S6 pins
  it. Question 8 is still open.
- 22:17 — Q8 answered: VAPs are added separately and stay out of these E2Es. R4 notes that its namespace-mismatch case
  may move to admission once they land. **All open questions are now resolved or deferred; the plan is ready for
  implementation approval.**
- 22:19 — Decision 9: no claim provisioner. Claims may be created and withdrawn in tests but are never fulfilled.
  Hub agent check: there is no switch that turns claims on or off, so this is purely a test assumption. Added an
  environment fact and L4 (P0: a cluster joins and fulfills a waiting selector; reuses the existing leave/join
  helpers). Also added L5 (P1: a registered but never-joined cluster keeps the claim) and reworded S13 as the
  existing-cluster variant. Claim fulfillment moves to out of scope, and L4 moves into Phase 1.
- 22:47 — Review pass for logic issues and inconsistencies, with each claim checked against the code. Fixed:
  - S8/S9: added `count: All` (the default count of 1 made the planned sets impossible).
  - S15: now expects resource churn; deleting a binding deletes its Works.
  - R7: switched to CPP to match U9, and now builds on U9's staged runs (auto-rollout can't leave a snapshot
    referenced).
  - A2: timing note.
  - A7: added a precondition; its duplicate pin now lives in D1.
  - X3: correct failure reasons, plus a forced creation order.
  - U3: defined the policy whose count it raises.
  - U7: added a way to make exactly one cluster fail.
  - U12: precise expectations.
  - AR4: an explicit order, and timing that reflects auto-rollout's claim backoff.
  - B2: the takeover step is now explicit.
  - L4: decoupled from L1/L2 (L4 is in Phase 1; they aren't), and the shared context now uses distinct ConfigMaps.
  - D4: dropped, since the chart CRDs are symlinks to the generated ones.

  Added environment facts on the v1 applier backoff, auto-rollout ignoring role releases, and the virtual fixture
  clusters. Added a stickiness exception for leaving clusters and a 70m-timeout risk note. Opened Q10 (leave race;
  L1/L2 expectations pending), Q11 (orphaned snapshots on recreate; D2), and Q12 (v1 applier E2E backoff flags).
- 23:09 — Re-checked Q10 after the user recalled that leaving clusters keep their bindings. A throwaway envtest
  probe (written, run, and deleted) bound a cluster, held its `MemberCluster` in `Terminating` with a finalizer, and
  watched the binding get deleted within seconds. So the binding release is confirmed, and the resource outcome is
  still a race. Corrected the plan's description of legacy behavior: the legacy scheduler marks the binding
  `Unscheduled` instead of keeping it bound, and deletion goes through rollout or the update run's delete stage.
  Q10 is still open.
- 23:27 — Q10 answered: the binding should be deleted only after the cluster has left; deleting it at the deletion
  timestamp is a bug to be fixed separately. L1 and L2 are marked skipped. Added L6 (a virtual-cluster check of
  binding timing during the 1m force-delete window), deferred to the fix PR. Added a precondition to L4 (no binding
  targets cluster-3 when it leaves). Questions 11–12 are still open.
- 23:30 — Q11: not intended; the user is preparing a fix. Kept open until the user confirms it has landed. D2 keeps its
  binding-adoption assertions; its snapshot-ownership assertions are pending the fix. Q12 is still open.
- 23:45 — Q12 answered and implemented (P-5). Decisions:
  - The v1 setup must not share configuration with v1beta1.
  - No new flags; use fixed static values in both agents.
  - Keep the placeability filters shared.
  - E2E caps the v1 applier's backoff through a test-only env var.

  Code changes:
  - Hub: three static concurrency constants.
  - Member: static concurrency and cleanup wait, plus the env var helper.
  - v1 work applier package: named default backoff constants and `NewDefaultRequeueRateLimiterWithMaxDelay`.
  - `setup.sh`: sets the env var on each member agent and waits for the rollout.
  - Chart READMEs updated.

  Validation:
  - Build and vet.
  - `make lint`: 0 issues, after one gofmt fix.
  - New unit tests pass: `TestNewDefaultRequeueRateLimiterWithMaxDelay` (6 cases) and
    `TestPlacementPolicyWorkApplierRequeueRateLimiter` (5 cases).
  - Existing backoff tests and `cmd/...` tests pass.
  - `bash -n setup.sh` passes.
  - Not run on Kind (no Docker here).

  Updated A2 (detection within ~5s) and the related environment fact. Only Q11 remains open.

  After this entry was written, the hub constants in `setup.go` were found at 10/3/3 (modified at 12:39 UTC,
  apparently by the user) rather than the 100/10/10 set here; P-5 reflects the current code. That file's comment
  still says the values match the v1beta1 defaults, which is no longer true.
- 23:51 — Pre-implementation check. Confirmed in `process.go` that a staged update run reconcile holds its worker for
  up to `refreshPeriod` (30s). With 3 workers and ~15 parallel ginkgo processes, U7's four long-blocking runs would
  starve the lane's other staged update runs, so U7 is now `Serial` (environment fact added; phasing updated).
  Remaining items before implementation are listed in the reply: the stale `setup.go` comment, how to land the
  uncommitted code changes, no local Kind environment, and the Phase 0 prerequisites.
- 23:54 — User decisions:
  - Hub concurrency is now 10/5/5 (work generator / staged update runs / auto-rollout); the `setup.go` comment no
    longer claims parity with the v1beta1 defaults (build and vet pass).
  - U7 can take longer, so it is no longer `Serial`. Its four runs execute one at a time, holding at most one of the 5
    workers, in parallel with the lane (~5–6 min).
  - The user will handle commits and PRs for the code changes.
  - The user will enable Docker Desktop WSL integration so the E2Es can be run locally (setup-clusters and focused
    specs).
- 00:05 (Oct 23) — Implemented S1 and S2 as requested; the user asked not to run the E2Es yet.
  - [placement_policy_scheduling_test.go](../../../test/e2e/placement_policy_scheduling_test.go) carries
    `Label("placementpolicy")` and two Ordered contexts. S1: a PP with no selectors. S2: a CPP with `env=prod`,
    `count: All`, plus a Consistently check that the canary cluster gets neither a binding nor the ConfigMap.
  - P-3 has started with scope-agnostic helpers (key with an empty namespace = cluster scope).
    [placement_policy_utils_test.go](../../../test/e2e/placement_policy_utils_test.go) builds, retrieves and removes
    policies, lists bindings by policy name, finds the latest primary snapshot through the owner annotation, and has
    the all-synchronized status builder and cmp options.
    [placement_policy_actuals_test.go](../../../test/e2e/placement_policy_actuals_test.go) compares the whole policy
    status and, per cluster, each binding's controller ownership, spec and status in one shot.
  - The existing ConfigMap and cleanup helpers in placement_policy_test.go are reused.
  - Validation: gofmt, `go vet ./test/e2e/`, a compile of the test binary, and a Ginkgo dry run with
    `label-filter=placementpolicy` (selects exactly the 7 new specs) all pass; `make lint` reports 0 issues.
  - Not done yet: P-4's CI lane, and the label on the two existing FEP files. Until then the new specs run in the
    default lane.
- 00:19 — Implemented R1, A1, U1, and AR1 as requested, without running them. P-1 is done: `kfrolloutv1alpha1` is
  registered in the E2E scheme in [setup_test.go](../../../test/e2e/setup_test.go).
  - [placement_policy_resources_test.go](../../../test/e2e/placement_policy_resources_test.go) (R1, PP): two labeled
    ConfigMaps are placed and the unlabeled app ConfigMap is not. After an edit plus a new labeled ConfigMap, the
    policy reports `ResourcesChangedPendingRollout`, with the latest revision, bindings, member data and the absent
    ConfigMap all held steady (Consistently).
  - [placement_policy_sync_strategy_test.go](../../../test/e2e/placement_policy_sync_strategy_test.go) (A1, CPP): a
    pre-existing ConfigMap on cluster-1 gets `NotTakenOver` in the binding's `failedResources`. The policy reports
    2/3 synchronized and available, and the member data stays untouched. Switching to `AlwaysTakeOver` updates every
    binding's sync strategy on the same snapshot, takes over the ConfigMap (AppliedWork owner reference, hub data),
    and leaves the policy fully synchronized on the same revision.
  - [placement_policy_staged_update_run_test.go](../../../test/e2e/placement_policy_staged_update_run_test.go) (U1,
    both scopes via a loop): the run is initialized with a new snapshot and the policy's latest revision moves. The
    canary gets v1 while prod stays on v0, and the policy reports `SomeClustersOnOlderResourceRevision` with the run
    holding the binding manager role. The approval request is checked for spec and controller ownership, prod is held
    until approval (Consistently), the approval is accepted, and v1 reaches every cluster. The whole run status
    (stages, clusters, tasks, conditions) is compared, and the policy ends fully synchronized with the role released.
  - [placement_policy_auto_rollout_test.go](../../../test/e2e/placement_policy_auto_rollout_test.go) (AR1, PP):
    there is no rollout attempt for the initial placement. After a hub edit, a new snapshot reaches every binding and
    member without a run, the timestamp annotation parses, and the policy ends fully synchronized with the role
    released.
  - Helpers:
    - Generalized bindings actual (per-cluster snapshot and status; S1/S2 use a wrapper).
    - Member ConfigMap actuals: data, absence, taken over.
    - Run-status and approval-request actuals.
    - Mutators: policy update, hub ConfigMap data, unmanaged member ConfigMap, approval.
    - Scope-agnostic run, strategy and approval request builders and getters, plus cleanup.
    - `placementPolicyRolloutEventuallyDuration` (90s).
  - Validation: gofmt, `go vet ./test/e2e/`, a Ginkgo dry run with `label-filter=placementpolicy` (52 specs: 7 for
    S1/S2 plus 45 new), and `make lint` (0 issues).
- 00:51 — First trial run on Kind. Setup was `test/e2e/setup.sh 3`, then
  `ginkgo -v -p --label-filter=placementpolicy` (only the new specs), then teardown with `stop.sh 3`.
  - Run 1: 48 passed, 2 failed, 2 skipped. Both failures were U1's "should complete the run successfully" (PP and
    CPP). `PerStageStatus.StartedTimestamp`/`CompletedTimestamp` are `*metav1.Time`, and
    `cmpopts.IgnoreTypes(metav1.Time{})` doesn't cover pointers: cmp calls `(*metav1.Time).Equal` first.
  - Fix: `compareTimePointersByPresence` in
    [placement_policy_actuals_test.go](../../../test/e2e/placement_policy_actuals_test.go). It compares only whether
    the pointers are set, so the wanted `&metav1.Time{}` still checks that the stage timestamps are set.
  - Re-runs: U1 alone passed 26/26; all new specs passed 52/52 in 1m28s. `make lint` reports 0 issues.
  - Assumptions now confirmed on Kind: `ActiveClusterClaims=0` is set, the defaulted sync strategy equals the
    binding's copy, `EquateEmpty` handles the prod stage's nil `AfterStageTasks`, and the cluster-scoped run and
    approval flow works.
  - Local environment notes (not repo changes):
    - The stale buildx builder `img-builder` (dead Docker Desktop bind mount) was removed; the Makefile recreates it.
    - `~/.docker/config.json` uses `credsStore: desktop.exe`, which can't run without WSL interop, so the run used a
      temporary `DOCKER_CONFIG` with an empty config.
- 01:09 — Implemented the remaining S and R P0s (S3, S4, S5, R2, R3, R4) as requested, without running them. Each
  expectation was checked against the controller code first (`chooseClusters`, `aggregateCounts`,
  `summarizeResourceStatus`, the snapshot manager's selection errors, the claim lifecycle).
  - [placement_policy_scheduling_test.go](../../../test/e2e/placement_policy_scheduling_test.go):
    - S3: the per-selector `desired=scheduled=3` against the per-cluster `synchronized=available=2`.
    - S4: the first pick is learned from its binding (ranking isn't asserted), and its binding UID must survive both
      the scale-up and the scale-down.
    - S5: `Serial`; `AfterAll` restores cluster-2's `env` label in a defer. The changed terms (`env=canary` plus
      `region=east`) match nothing, giving one claim carrying them, `desired=1`, `scheduled=0`, and, with no target
      cluster left, `Synchronized=True` and `Available=True`.
  - [placement_policy_resources_test.go](../../../test/e2e/placement_policy_resources_test.go):
    - R2: both east bindings are on the initial snapshot with v0 data, and the policy is still pending a rollout.
    - R3: decision 13.
    - R4: two contexts. While a selection error stands, the policy has no snapshot, no bindings, and no latest
      revision.
  - Helpers added:
    - Bindings by cluster; wait for a single binding; the other east cluster.
    - Member cluster lookup and label setter; an AppliedWork-ownership check, now shared with the takeover actual.
    - Pending-rollout and not-collected status builders (R1 now uses the former).
    - A status actual for policies without a revision.
    - Actuals: no bindings, leftovers removed, claim, namespace placed/absent, ClusterRole placed/absent.
  - Validation: gofmt, `go vet ./test/e2e/`, a Ginkgo dry run with `label-filter=placementpolicy` (88 specs, 36 new),
    and `make lint` (0 issues).
- 01:50 — Ran the `placementpolicy` specs on Kind (setup via `setup.sh 3` with a temporary `DOCKER_CONFIG`;
  teardown via `stop.sh 3`).
  - Run 1: 88/88 passed (2m11s). S5 ran as a `Serial` context after the parallel specs.
  - Run 2: 87/88. S4 failed because the first pick (cluster-2) was trimmed on scale-down. The hub logs showed both
    bindings created within the same second, so their creation timestamps tied and the name tie-break kept
    cluster-1 (Q14).
  - Fix: S4 now scales up only after the first binding's creation second has passed (plus 250ms).
  - Re-runs: S4 alone with `--repeat=4` passed 5/5, with the hub logs confirming that the newer binding was trimmed
    each time. A final full run passed 88/88 (1m57s). `make lint` reports 0 issues.
- 02:37 — Implemented the remaining A, U, and AR P0s (A2, A3, A4, U2, U3, AR2) as requested, without running them.
  Expectations were checked against the code: the v1 work generator's binding status (failed resources copy the
  applier's `Applied` condition and diff details), pre-apply drift detection, `cleanupWhenBindingDeleted` (orphan
  propagation), run initialization (an explicit `resourceSnapshotName` must be a primary snapshot the policy controls;
  no policy re-validation mid-run), and auto-rollout's exact `"true"` check.
  - [placement_policy_sync_strategy_test.go](../../../test/e2e/placement_policy_sync_strategy_test.go):
    - A2: PP on cluster-1 only. The default `ApplyAnyway` overwrites a member edit. After switching to `ReportError`
      (no rollout), the next edit shows on the binding as `Applied=False/FoundDrifts` with
      `diffDetails{observedInMemberClusterGeneration: 0, /data/data: drifted vs test}`, and the policy is 0/1
      synchronized. The drift is left as is (Consistently); reverting it recovers.
    - A3: `OrphanResources`. After the policy is deleted, its bindings, snapshots, and Works are gone from the hub,
      and the ConfigMap stays on every member with the same data and no AppliedWork owner (Eventually, then
      Consistently).
    - A4: the ConfigMap plus `appDeployment()`. The Deployment is available on every member (2m wait for the image
      pull), every binding reports 2/2/2, and the policy is fully available.
  - [placement_policy_staged_update_run_test.go](../../../test/e2e/placement_policy_staged_update_run_test.go):
    - U2: a forward run takes index 1. A rollback run names index 0; every binding returns to it and the members get
      v0. The status is pinned as described in the row.
    - U3: the held-back state (bindings, no ConfigMap on Y, policy status with the run as binding manager) is checked
      with Eventually, then Consistently. After approval the run succeeds and Y binds to the new snapshot.
  - [placement_policy_auto_rollout_test.go](../../../test/e2e/placement_policy_auto_rollout_test.go):
    - AR2: a pending change, the initial version on every member, and no attempt annotation, held for 30s without
      the annotation and for another 30s with it set to `"false"`.
  - Helpers:
    - `deletePlacementPolicyAndWait`, now used by `ensurePlacementPolicyRemoved`.
    - `configMapNotAppliedPlacementBindingStatus`, which `notTakenOverPlacementBindingStatus` now wraps.
    - `someClustersNotSynchronizedPlacementPolicyStatus`, now also used by A1.
    - `waitForAfterStageApprovalRequest`, now also used by U1.
    - `otherProdClusterName`, `updateConfigMapDataOnMemberCluster`, `noTermsSelectorsForAllMemberClusters`.
    - Actuals for no Works left, orphaned ConfigMaps, available Deployments, and successful runs.
    - The bindings actual now also ignores time values, for `diffDetails.firstDiffedObservedTimestamp`.
  - Validation: gofmt, `go vet ./test/e2e/`, a Ginkgo dry run with `label-filter=placementpolicy` (126 specs, 38 new),
    and `make lint` (0 issues).
- 02:55 — Ran the `placementpolicy` specs on Kind (setup via `setup.sh 3` with a temporary `DOCKER_CONFIG`;
  teardown via `stop.sh 3`). Run 1 passed 126/126 (2m47s), and run 2, a flakiness check, passed 126/126 (2m40s).
  No code changes were needed. The longest new spec is AR2's second Consistently (about 70s with `AfterAll`). A4's
  Deployment became available well within its 2m wait. The other new contexts end in the usual ~40s cleanup.
- 03:22 — The user picked the U and AR P1s plus the remaining P0s; L4 is skipped for now; P-4 is done as well.
  Implemented U4–U10, AR3–AR5, and D1, without running them. Expectations were checked against the code first:
  - Staged update runs: timed wait, before-stage approval, suspension (a suspended run gives up the binding manager
    role and its cleanup finalizer), and the failure policy (`maxAllowedClusterFailures` defaults to 1 and fails the
    run once exceeded; a cluster fails once `maxWaitTimePerClusterMinutes` passes without it becoming synchronized
    and available).
  - Initialization failures: a missing policy goes through the terminal-state path, but still ends at
    `Initialized=False/Failed`.
  - The v1 Deployment availability rule (updated = available = desired, none unavailable).
  - Auto-rollout: requeues on the remaining wait period; on a failed claim, exponential backoff with no reaction to a
    role release.
  - The annotation controller leaves extra annotations alone.

  New files:
  - [placement_policy_staged_update_run_stages_test.go](../../../test/e2e/placement_policy_staged_update_run_stages_test.go):
    - U4: compares the whole run status, including the TimedWait task, and checks that the prod stage starts no
      earlier than the wait start + 30s - 1s. The start is recorded to the second, so the wait can end up to 1s
      early.
    - U5: the approval request is found in `beforeStageTasks`.
    - U9: the stages are `canary` then `east`, so cluster-3 is in no stage. The run succeeds with results for
      clusters 2 and 1 only, and the policy reports `SomeClustersOnOlderResourceRevision` 2/3.
  - [placement_policy_staged_update_run_interruptions_test.go](../../../test/e2e/placement_policy_staged_update_run_interruptions_test.go):
    - U6, both scopes: created suspended (initialized, new snapshot, role released, nothing rolled out, no `Started`),
      then resumed, then suspended at the approval. The approval is neither accepted nor acted on while suspended,
      and is accepted after resuming.
    - U8: the deleted run's approval request is garbage-collected and the role released. The partial state is kept
      (policy 1/3 synchronized). A follow-up run rolls out the same snapshot and completes.
  - [placement_policy_staged_update_run_failures_test.go](../../../test/e2e/placement_policy_staged_update_run_failures_test.go):
    - U7: four runs in one Ordered context, each with its own Deployment and policy:
      - (a), PP then CPP: an unpullable image with `maxAllowedClusterFailures: 0`. The run fails at canary, prod
        stays on the initial snapshot, and the role is released.
      - A `pods: 0` quota is then added on cluster-2.
      - (b), PP then CPP: a pod-template change with 1 failure allowed ends `PartiallySucceeded`; only canary fails.
    - U10: missing strategy, missing policy, another policy's snapshot, and a missing sorting label. Each run's whole
      status is just `Initialized=False/Failed`, and the change stays pending (no new snapshot, role released,
      members on v0).
  - [placement_policy_lifecycle_test.go](../../../test/e2e/placement_policy_lifecycle_test.go):
    - D1: the Works and AppliedWorks are recorded first. Deletion removes the hub's bindings, snapshots, and Works,
      and the members' AppliedWorks and ConfigMap. The applier-created member namespace stays, unmanaged
      (Consistently).

  Changes to existing files:
  - [placement_policy_auto_rollout_test.go](../../../test/e2e/placement_policy_auto_rollout_test.go):
    - AR3 extends AR1: the second edit is held until the last attempt + 1m (Consistently until 5s before), then
      rolled out, with the second attempt ≥ 1m after the first.
    - AR4: the run holds the role (rolling out the unchanged initial snapshot) while the edit stays pending with no
      attempt recorded. After approval and completion, auto-rollout rolls out on its backoff (3m wait).
    - AR5: auto-rollout annotations added to an annotation-generated PP (`env=canary`) roll out a hub edit to
      cluster-2, and the annotations survive.
    - AR1 now uses the shared `autoRolloutAnnotations()`.
  - Helpers:
    - Generalized `waitForStageApprovalRequest` (before/after).
    - Run-status builders moved to package level (U1 uses them).
    - `updateStagedUpdateRun`, `waitForPlacementPolicySynchronized`, `placementPolicyWorks`, and a few more.
    - Actuals: run condition, per-cluster run results, binding manager, binding snapshots, approval request gone,
      AppliedWorks gone, unmanaged namespace, only-canary-updated.
    - `someClustersOnOlderResourceRevisionPlacementPolicyStatus`.
  - P-4: the `placementpolicy` CI lane (same settings as the default lane), `&& !placementpolicy` in the default
    lane, and `Label("placementpolicy")` on the three Describes of the two older FEP files.

  Validation:
  - gofmt, `go vet ./test/e2e/`, and `make lint` (0 issues).
  - Ginkgo dry runs: `placementpolicy` selects 230 specs, now including the two older FEP files; the default lane's
    new filter selects 951 specs, none of them FEP.
  - `ci.yml` parses (checked with `sigs.k8s.io/yaml` in a throwaway test, since removed).
  - Expected lane cost: U7 adds about 5–6 minutes on one process, AR4 up to about 3, AR3 about 1.
- 03:56 — Ran the `placementpolicy` lane on Kind (setup via `setup.sh 3` with a temporary `DOCKER_CONFIG`; teardown
  via `stop.sh 3`). It now has 230 specs, including the two older FEP files labeled under P-4.
  - Run 1: 229/230 passed (9m53s). The one failure was U10's `AfterAll`, not an assertion. It removed the second
    policy first through `ensurePlacementPolicyRemoved`, which waits for the app ConfigMap to leave the members, but
    the main policy still placed it.
  - Fix in [placement_policy_staged_update_run_failures_test.go](../../../test/e2e/placement_policy_staged_update_run_failures_test.go):
    the main policy goes first, then the second one through `deletePlacementPolicyAndWait`, plus a wait for its
    ConfigMap to leave the members. No other `AfterAll` has this ordering issue: U7's policies place Deployments, so
    the app ConfigMap check passes trivially there.
  - U10 alone then passed 7/7, and a second full run passed 230/230 (8m22s). `make lint` reports 0 issues.
  - Timing: U7's four runs took about 61s, 61s, 96s, and 142s (the last includes `AfterAll`), well within their 3m
    waits. AR3 and AR4 passed on both runs.
