# Auto-Rollout Controller (v1) Integration Test Plan

**Date**: October 21, 2026 21:43 AEDT
**Task**: Plan and implement the integration tests for `pkg/v1/controllers/autorollout`, following the patterns of the
staged update run controller tests (see [the staged update run plan](./2026-10-20-1723-stagedupdaterun-integration-test-plan.md)).
All planned scenarios are implemented; see [Status](#status) for the progress log.

## Requirements

1. Cover the controller end to end against a real API server (envtest), for both `ClusterPlacementPolicy` and
   `PlacementPolicy`.
2. Follow repo and v1 conventions: Ginkgo/Gomega, `<file>_integration_test.go`, `Ordered` contexts with step-wise
   `It`s, one `Describe` block per scope, whole-struct `cmp.Diff` comparisons, `want` naming, random name suffixes
   (`utils.RandStr()`) so that contexts never collide.
3. No production code changes are needed to make the controller testable (see constraint 4).

## Controller behavior under test

Reconcile flow (see [controller.go](../../../pkg/v1/controllers/autorollout/controller.go)):

1. Get the policy from the cache (`ClusterPlacementPolicy` if the request has no namespace). Not found → forget the
   request, done.
2. Deleting → relinquish the binding manager role if the cached policy shows it held (no API call otherwise; the event
   handler always enqueues a policy whose role this controller holds, so a cache that has yet to show the claim is
   followed by another reconcile), done. No finalizer is ever added.
3. `shouldSkipProcessing` → relinquish the role (if held, as in step 2) and stop when:
   - auto-rollout is disabled (`kubefleet.dev/auto-rollout-enabled` is not exactly `"true"`) → no requeue;
   - no rollout is needed → no requeue. A rollout is needed iff `ResourceCollected=True` **and** `Synchronized=False`
     with reason `ResourcesChangedPendingRollout` or `SomeClustersOnOlderResourceRevision`, both with
     `observedGeneration == generation`;
   - the minimum wait period since `kubefleet.dev/auto-rollout-last-rollout-attempt-timestamp` has not passed → requeue
     after the remaining time. The period comes from `kubefleet.dev/auto-rollout-minimum-wait-period` (default 3m;
     unparsable → default; clamped to [1m, 1h]). A missing or malformed timestamp counts as never rolled out; one in
     the future counts as just rolled out (the remaining time never exceeds the period).
4. Claim the binding manager role (`status.bindingManager = {controllerName: auto-rollout, objectRefs: [the policy
   itself]}`). Held by another controller → requeue with the claim rate limiter; nothing is written.
5. Rollout: list bindings by the `spec.placementPolicyName` field index (in the policy's namespace for namespaced
   policies), keep only those not being deleted **and** controlled by this policy instance (`IsControlledBy`, i.e.
   UID match); `SnapshotResourcesIfStale` (create index 0 / reuse the latest / create the next index; an empty result
   is an unexpected error); set `spec.resourceSnapshotName` to the latest primary snapshot on every binding that does
   not already point to it (parallelizer, fail fast on the first error). Failure:
   - a **user error** (in practice from snapshotting, e.g., a selected resource too large for a snapshot) → a Warning
     event regarding the policy (reason `FailedToSnapshotResourcesDuringAutoRollout`, action `SnapshotResources`),
     write `last-rollout-attempt-timestamp` (as in step 6), relinquish the role, and requeue after the minimum wait period
     with no error, so that the wait period spaces out the retries (C6; behavior 8);
   - any other error → return the error **with the role still held**; controller-runtime retries with its default
     backoff (5ms doubling, up to ~16m), and no timestamp is written (C8).
6. Success, or a user error (step 5) → write `last-rollout-attempt-timestamp` (RFC3339Nano, UTC) with a JSON merge patch
   that carries only that annotation and no resource version (changed on October 21, 2026: this used to be a
   resource-version-guarded `Update` made **before** the rollout).
7. On success, requeue after 200ms; that reconcile hits the wait period skip (step 3) and relinquishes the role.

Watches: `ClusterPlacementPolicy` and `PlacementPolicy` only (see
[eventhandler.go](../../../pkg/v1/controllers/autorollout/eventhandler.go)):

- Create: enqueue if enabled or holding the role (the latter is only reachable on a controller restart).
- Update: always enqueue if the new object holds the role (the controller's own claim included, which is why a
  user-error failure must write the timestamp; behavior 8); otherwise only if old or new is enabled **and**
  `autoRolloutRelevantChange` (deletion timestamp, the enabled/min-wait annotations, or — when the new object needs a
  rollout — a change in status/reason/observedGeneration of `ResourceCollected` or `Synchronized`).
- Not watched: bindings, snapshots, selected resources (e.g., ConfigMaps), and the last-rollout-attempt annotation.

## Test environment constraints and design implications

1. **Only this controller (plus the snapshot manager) runs.** No placement policy controller: tests set
   `ResourceCollected`/`Synchronized` on the policy status by hand (stamped with the current generation), and create
   bindings by hand with `spec.placementPolicyName` **and a controller owner reference to the policy** (unlike the
   staged update run tests, whose bindings carry no owner reference — the auto-rollout controller would skip those).
   No GC: tests remove the snapshots the manager creates.
2. **Trigger last.** The controller reads bindings from the shared cache; a binding the cache has yet to see is
   silently skipped (the rollout still counts as a success), and is not retried until the next rollout (≥ 1m later).
   Set up the policy, bindings, and any
   pre-set annotations, `waitForObjectsToBeCached`, and only then write the conditions that call for a rollout (the
   trigger).
3. **Only relevant changes trigger a reconcile** (see Watches). Hence:
   - backdate `last-rollout-attempt-timestamp` **before** the trigger; changing it afterwards does nothing;
   - for a second rollout, flip `Synchronized` to `True/AllClustersSynchronized` and back to `False` (as the
     placement policy controller would); rewriting identical conditions does not enqueue;
   - a "nothing happens" `Consistently` check after a non-triggering write proves little; where a test needs the
     controller's own skip logic to run, pair the write with a min-wait annotation change (alternate `"1m"`/`"2m"`;
     harmless when no timestamp exists).
4. **Minimum wait period.** The bounds are package constants (≥ 1m), so tests never wait it out: they write a
   backdated timestamp (`time.Now().Add(-d).UTC().Format(time.RFC3339)`) instead. Only D2 relies on the controller's
   own `RequeueAfter`, with ~15s remaining. No refresh-period-style override (as in the staged update run suite) is
   needed.
5. **Re-rollouts in steady state.** Conditions never change on their own here, so after a rollout the controller
   comes back after the wait period (3m by default) and rolls out again if the conditions still call for it. Contexts
   finish well within 3m; contexts that continue past a rollout first mark the policy `Synchronized=True`.
6. **The claim is transient on success.** Claim → refresh → timestamp → 200ms → relinquish; the held state is not
   reliably observable, so tests assert the end state (role released, i.e. `bindingManager == nil`). Two states are
   observable: claim-blocked (no timestamp is written, since the timestamp comes last) and a rollout failing with a
   non-user error (the role stays held across retries, C8). A user-error failure is transient too (claim → snapshot
   fails → event → timestamp → relinquish), so C6 asserts its end state and the event.
7. **Fast claim retries.** Releasing another controller's claim is not a relevant change; the retry comes from the
   claim rate limiter, which the suite sets to 250ms–2s (vs. 5s–180s by default).
8. **Snapshots.** Names are deterministic (`primaryPlacementResourceSnapshotName(policyName, idx)`); the default
   revision history limit is 3, so no snapshot is garbage collected with ≤ 3 snapshots per policy. Hand-made
   snapshots are not used: the manager relies on annotations (resource hash) that only it writes. To cause a user
   error, the suite caps the resource data per snapshot at 16 KiB (the default is ~1.2 MiB), and C6 grows its
   ConfigMap past it; the manager checks the cap only when it creates a snapshot, so nothing is created on failure.
9. **Write conflicts.** Test writes to a policy (annotations, status) can race with the controller's claim and
   timestamp write; helpers re-read and retry in `Eventually`, and the controller retries on conflict by itself. No
   placement policy controller runs here, so the production race in behavior 1 (a status write between the claim
   and the timestamp `Update`) is not exercised.

## Suite changes (`suite_test.go`)

Mirror [the staged update run suite](../../../pkg/v1/controllers/stagedupdaterun/suite_test.go):

- envtest with `config/crd/bases`; `placementv1alpha1` in the scheme (`clusterv1beta1` and `rolloutv1alpha1` are not
  needed: no member clusters are involved, and the auto-rollout API items are plain annotation keys).
- Manager (metrics off); `hubClient = hubMgr.GetClient()` (cache-backed, shared with the controller);
  `fieldindexers.SetupWithHubAgentControllerManager`; the placement resource snapshot manager (dynamic client, empty
  informer manager, baseline eligibility checker, and a 16 KiB cap on the resource data per snapshot, see constraint
  8), added to the manager.
- `New(hubMgr.GetClient(), snapshotManager, parallelizer.NewParallelizer(workerCount /* 4 */),
  workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](250ms, 2s), hubMgr.GetEventRecorder(controllerName))`;
  `SetupWithManager(hubMgr, maxConcurrentReconciles /* 4 */)`.
- Namespaces `app` and `other-app`, created once per process.
- No `TestMain` CI skip: the package runs in `make test` (decision 2).
- `hubAPIReader = hubMgr.GetAPIReader()` (uncached), used only where a read must reflect a write the test case has
  just made, e.g., that the controller released a claim the test wrote (A5); a cached read might predate the claim.
  C6 also reads the policy's resource version and the `events.k8s.io/v1` events through it (the cache does not watch
  events).

Implemented with group A ([suite_test.go](../../../pkg/v1/controllers/autorollout/suite_test.go)).

## Shared test utilities (`utils_integration_test.go`)

Package-local copies/adaptations of the staged update run helpers (test helpers cannot be imported across packages;
extracting a shared package is out of scope), scope picked by the namespace (`clusterScopedNS == ""`):

- **Constants**: name templates (`configmap-%s`, `policy-%s`, `binding-%s-%02d`, `cluster-%s-%02d`),
  `previousResourceSnapshotName`, `testFinalizer`, the other controller's claim details, timings (`eventuallyDuration`
  20s, `eventuallyInterval` 500ms, `consistentlyDuration` 5s, `consistentlyInterval` 500ms).
- **Stubs**: `newPlacementPolicyObj`, `newPlacementBindingObj`, `newPlacementResourceSnapshotObj`, `newConfigMapObj`.
- **Fixtures**: `createConfigMap`; `createPlacementPolicy(ns, name, configMapName, annotations)`;
  `createPlacementBinding(policy, bindingName, clusterName, snapshotName)` (controller owner ref to the policy);
  `createPlacementBindingWithOwnerRefs(...)` for the exclusion cases; `createPlacementBindings(policy, randStr, n)`
  returning names and objects.
- **Mutations**: `updateConfigMapData`; `updatePlacementPolicyAnnotations(ns, name, func(map[string]string))`;
  `setPlacementPolicyConditions(ns, name, conds...)` (stamps the current generation unless the condition already
  carries one) with builders `resourceCollectedCond(status, reason)` / `synchronizedCond(status, reason)` and
  shorthands `markPlacementPolicyAsPendingRollout(ns, name, reason)` / `markPlacementPolicyAsSynchronized(ns, name)`;
  `updatePlacementPolicyStatus(ns, name, func(*PlacementPolicyStatus))` (conditions and claim in one write, for B3/B4);
  `updatePlacementPolicyBindingManager`; `bumpPlacementPolicyGeneration` (e.g., set
  `resourceRevisionHistoryLimit` to 5); `backdatedTimestamp(d)`; `touchPlacementPolicy(ns, name)` (sets a harmless
  test annotation to a fresh value, to enqueue a role-holding policy right away, for C8).
- **Admission (C8)**: `createSnapshotChangeDenyPolicy(randStr)` creates the `ValidatingAdmissionPolicy` and its
  binding (selecting the per-context label) and returns both for cleanup; `bindingSnapshotChangeDeniedActual(ns,
  name)` / `bindingSnapshotChangeAllowedActual(ns, name)` make a dry-run snapshot change on a binding to tell whether
  the rule is in force. The admission types come with client-go's `scheme.Scheme`, which the suite already uses.
- **Lifecycle**: `markObjectForDeletion`, `removeTestFinalizer`, `waitForObjectsToBeCached`, `objectRemovedActual`,
  `removeObjects` / `deleteObjectsAndWaitForRemoval` (no approval request ordering needed).
- **Assertions** (`...Actual` closures): `primaryPlacementResourceSnapshotCreatedActual(policy, idx)` (labels and
  owner ref, as in the staged update run tests); `placementResourceSnapshotAbsentActual(ns, name)`;
  `placementBindingsResourceSnapshotActual`; `placementBindingGenerationActual`;
  `placementBindingsUntouchedActual(ns, names)` (previous snapshot and generation 1); `bindingManagerActual`;
  `bindingManagerRoleReleasedActual`; `wantAutoRolloutObjectRef(ns, name)` (the policy itself);
  `lastRolloutAttemptTimestampActual(ns, name, notBefore)` (present, RFC3339Nano-parsable, UTC, within
  `[notBefore, now]`); `lastRolloutAttemptTimestampUnchangedActual(ns, name, wantVal)` (`""` = absent).
- **Composites**: `rolledOutActual(policy, bindingNames, idx, notBefore)` (snapshot `idx` created, bindings point to
  it, timestamp ≥ `notBefore`, role released) and `notRolledOutActual(policy, bindingNames, nextIdx, wantTimestamp,
  wantBindingManager)` for `Consistently`.

**Implemented with group A** (only what A uses, to keep the `unused` linter quiet; B–D add their own), with a few
deviations from the list above:

- `createPlacementBindings(policy, randStr, n)` returns only the names; call sites wait for the cache with
  `placementBindingObjs(ns, names)` stubs (returning objects tripped the `prealloc` linter at every call site).
- `allActual(actuals...)` combines checks for `Consistently`, in place of a fixed `notRolledOutActual`; A's
  "no rollout" checks are `allActual(placementResourceSnapshotAbsentActual, placementBindingsActual(ns, names,
  snapshot, generation), placementPolicyAtRestActual(ns, name, wantTimestamp))`.
- `placementPolicyRolledOutActual(ns, name, notBefore)` checks the timestamp and the released role on **one** read
  of the policy: the timestamp is written after the claim, so a read with the timestamp but no binding manager
  cannot predate the claim. `placementPolicyAtRestActual` checks no binding manager and an exact timestamp value
  (`""` = absent).
- `bindingManagerReleasedOnAPIServerActual(ns, name)` (via `hubAPIReader`), for releases of claims the test wrote.
- `forcePlacementPolicyReconciliation(ns, name)` switches the min-wait annotation between `"1m"` and `"2m"`
  (constraint 3); `lastRolloutAttemptTimestamp(ns, name)` captures the current value for exact comparisons.
- `placementPolicySpecAndAnnotationsUntouchedActual` (A1 only) lives in `lifecycle_integration_test.go`.

**Added with the C6 user-error steps (October 22, 2026):**

- In `utils_integration_test.go`: `placementPolicyResourceVersionOnAPIServer(ns, name)` and
  `placementPolicyUnwrittenOnAPIServerActual(ns, name, wantResourceVersion)` (both via `hubAPIReader`; a retry claims
  and releases the role, i.e., writes the policy, so an unchanged resource version shows that no retry happened).
  `placementPolicyRolledOutActual` now also covers a user-error failure, which writes the timestamp too.
- In `rollout_integration_test.go`: `oversizedConfigMapData()` (twice the suite's 16 KiB cap);
  `snapshotFailedEventActual(policy)` (lists `events.k8s.io/v1` events in the policy's namespace, or `default` for a
  cluster-scoped policy, and checks the type, reason, action, reporting controller, and regarding object of the one
  regarding the policy in one `cmp.Diff`, plus that its note mentions the too-large resource).

**Added with group C:**

- In `utils_integration_test.go`: `controllerOwnerRefTo(policy)`; `newOwnedPlacementBinding(policy, name,
  clusterName, snapshotName)` (built but not created, so C3/C4/C8 can change owner refs, namespace, or labels first)
  with `createPlacementBinding(binding)`; `createPlacementBindingsPointingTo(policy, nameSeed, n, snapshotName)`
  (`createPlacementBindings` now wraps it with the previous snapshot); `touchPlacementPolicy(ns, name)` (sets the
  `kubefleet.dev/integration-test-touched` annotation).
- In `rollout_integration_test.go` (C-only, as scenario helpers are in the staged update run tests):
  `variantPlacementBindingNames(randStr, variant)`; the C8 admission helpers `newSnapshotChangeDenyPolicyObjs(randStr)`
  (name-only stubs, safe for cleanup even if setup fails) and `createSnapshotChangeDenyPolicy(randStr)`;
  `placementBindingSnapshotChangeActual(ns, name, wantDenied)` (one dry-run helper for both directions, matching the
  denial by its message); `placementPolicyRolloutRetryingActual(ns, name)` (role held by `auto-rollout` and no
  timestamp, on one read).

**Added with group B:**

- In `utils_integration_test.go`: `updatePlacementPolicyStatus(ns, name, mutate)` (one status write;
  `setPlacementPolicyConditions` and `updatePlacementPolicyBindingManager` now build on it);
  `setConditionsOnPlacementPolicy(policy, conds...)` (in-memory, with generation stamping); `pendingRolloutConds(reason)`
  (used by `markPlacementPolicyAsPendingRollout`); `wantAutoRolloutBindingManager(ns, name)` (now also used by A5 and
  C8); `placementPolicyAutoRolloutStateActual(ns, name, wantBindingManager, wantTimestamp)` (binding manager and
  timestamp on one read; `placementPolicyAtRestActual` and C8's `placementPolicyRolloutRetryingActual` are now thin
  wrappers around it).
- In `lifecycle_integration_test.go`: the other controller's claim (`otherBindingManager(randStr)`, as in the staged
  update run tests) and `markPlacementPolicyAsPendingRolloutWithAutoRolloutClaim(ns, name)` (pending-rollout
  conditions and an auto-rollout claim in **one** status write, so the controller never observes one without the
  other). `gatingBindingCount` was renamed `lifecycleBindingCount`, as groups A, B, and D share the file.
- B2 writes the pending-rollout conditions along with the claim (the plan only asked for the claim), so auto-rollout
  being disabled is the only thing that holds the rollout back.

**Added with group D:** no new helpers; D reuses the existing ones (`backdatedTimestamp`,
`updatePlacementPolicyAnnotations`, `placementPolicyAtRestActual`, `rolledOutActual`). Notes on the assertions:

- D1, D3 (second step), and D4 (second step) assert the rollout with the usual 20s `Eventually`, which is shorter than
  the remaining wait the controller would requeue for (about 58s in D1 and 50s in D3; never, with a 3-hour unclamped
  remainder, for D3's upper bound). A pass therefore shows that the annotation change itself triggered the rollout.
- D2 uses the written timestamp plus 1 minute as `rolledOutActual`'s `notBefore`, so it also asserts that the rollout
  did not happen before the minimum wait period had passed (measured: the check waits ~9s after the `Consistently`).

## Test plan

Two test files:

- `lifecycle_integration_test.go`: groups A (gating and lifecycle), B (binding manager role), and D (minimum wait
  period).
- `rollout_integration_test.go`: group C (binding refresh and snapshots).

Each group has its own pair of `Describe` blocks, so no two groups share a test node. The namespaced block comes first,
then the cluster-scoped one, and every scenario is its own `Ordered` `Context` with step-wise `It`s. Every group runs
for both scopes unless noted otherwise. Proposed `Describe` texts (`<scope>` is "placement policies" or "cluster
placement policies"), each usable with `--focus`:

| Group | File | `Describe` text |
| --- | --- | --- |
| A | `lifecycle_integration_test.go` | `gating auto-rollouts of <scope>` |
| B | `lifecycle_integration_test.go` | `managing the binding manager role of <scope>` |
| D | `lifecycle_integration_test.go` | `enforcing the minimum wait period between auto-rollouts of <scope>` |
| C | `rollout_integration_test.go` | `rolling out <scope>` |

### A. Gating and lifecycle (`lifecycle_integration_test.go`)

- **A1 Happy path.** Enabled at creation, 3 bindings on the previous snapshot; trigger with
  `Synchronized=False/ResourcesChangedPendingRollout` → snapshot 0 created (labels, owner ref), all bindings point to
  it, timestamp ≥ trigger time, role released; spec untouched (generation 1), the enabled annotation preserved. Then
  `Synchronized=True/AllClustersSynchronized` → `Consistently` nothing moves (bindings at generation 2, timestamp
  unchanged, no snapshot 1).
- **A2 Disabled.** No annotation; trigger → `Consistently`: no snapshot, bindings untouched, no timestamp, no claim.
  Set the annotation to `"True"` (not exactly `"true"`) → `Consistently` the same (not even enqueued, as neither
  version counts as enabled). Set `"true"` → rolled out as in A1.
- **A3 Disabled after a rollout.** Roll out; `Synchronized=True`; remove the annotation; change the ConfigMap;
  backdate the timestamp; flip `Synchronized=False/ResourcesChangedPendingRollout` → `Consistently` no snapshot 1,
  bindings stay on snapshot 0.
- **A4 Rollout-needed matrix** (one context; each step pairs the write with a min-wait toggle, then `Consistently`
  no rollout): (a) no conditions; (b) `ResourceCollected=False/FailedToCollectSomeResources`; (c)
  `Synchronized=True/AllClustersSynchronized`; (d) `Synchronized=False/FailedToSynchronizeSomeClusters`; (e) the
  right conditions at a stale generation (`bumpPlacementPolicyGeneration` first, then write them with
  `observedGeneration = generation − 1`; in the other order they would trigger a rollout); (f)
  `Synchronized=False/SomeClustersOnOlderResourceRevision` at the current generation → rolled out (covers the second
  reason).
- **A5 Deleting policy.** Enabled, with the test finalizer; mark for deletion (cache observes it); trigger →
  `Consistently` no rollout, no timestamp. Then write an auto-rollout claim (`wantAutoRolloutObjectRef`) → released.
  Remove the finalizer → gone (the NotFound path; nothing further to assert).

### B. Binding manager role (`lifecycle_integration_test.go`)

- **B1 Held by another controller.** Write the other claim and wait for the cache to see it, then trigger →
  `Consistently`: other claim intact, no timestamp, no snapshot, bindings untouched.
  Release it → rolled out within the claim backoff; role released.
- **B2 Stale own claim, disabled policy.** Write an auto-rollout claim on a policy with auto-rollout disabled →
  released (update path for role holders; skip without requeue). Bindings untouched, no timestamp.
- **B3 Stale own claim within the wait period.** Timestamp = now; one status write with the rollout-needed conditions
  and the claim → released, `Consistently` no rollout (the requeue-after skip also relinquishes).
- **B4 Own claim with a rollout needed.** One status write with the conditions and the claim → rolled out (the
  existing claim is reused via the dry-run check), then released.

### C. Binding refresh and snapshots (`rollout_integration_test.go`)

- **C1 Many bindings.** 12 bindings (> 4 workers) → all point to snapshot 0.
- **C2 Already up to date.** Some bindings pre-pointed at snapshot 0's (deterministic) name → they stay at generation
  1; the others move (generation 2).
- **C3 Excluded bindings** stay untouched: marked for deletion (test finalizer); no owner ref; controller owner ref to
  an older policy instance (same name and kind, different UID); non-controller owner ref to the policy; bound to
  another policy in the same scope. One regular binding is refreshed, as a control.
- **C4 Scope isolation.** Namespaced block: a same-named `PlacementPolicy` in `other-app` and a same-named
  `ClusterPlacementPolicy`, each with its own bindings → untouched. Also in `other-app`: a binding whose controller
  owner reference points at the `app` policy (Kubernetes treats cross-namespace owner references as absent) →
  untouched. Only the namespace the controller lists bindings in excludes this one; the others are excluded by the
  ownership (UID) check as well, so without it dropping the namespace filter went unnoticed (mutation MC6).
  Cluster-scoped block: a same-named `PlacementPolicy` in `app` with its bindings → untouched.
- **C5 No bindings.** → snapshot 0 created, timestamp written, role released.
- **C6 Successive rollouts, with a user error in between.** After rollout 1: `Synchronized=True`; grow the ConfigMap
  past the suite's per-snapshot cap (a user error); backdate the timestamp (> 3m); flip
  `Synchronized=False/ResourcesChangedPendingRollout` → a Warning event regarding the policy (constraint 8), the
  timestamp written (≥ the trigger), and the role released. Then force a reconciliation (min-wait toggle) →
  `Consistently`: the policy is not written to (its resource version stays; a retry would claim and release the role),
  no snapshot 1, bindings stay on snapshot 0. Shrink the ConfigMap (a changed value that fits), backdate the timestamp,
  and force a reconciliation → snapshot 1, bindings move to it, timestamp advances; snapshot 0 is kept (revision
  limit 3).
- **C7 Unchanged resources.** Same as C6 without the ConfigMap change (reason `SomeClustersOnOlderResourceRevision`)
  → no snapshot 1, binding generations unchanged, timestamp advances, role released.
- **C8 Update failure and retry.** A `ValidatingAdmissionPolicy` plus its binding. The policy matches `UPDATE`s of
  `placementbindings` and `clusterplacementbindings`, and its CEL rule
  (`object.spec.resourceSnapshotName == oldObject.spec.resourceSnapshotName`) rejects snapshot changes. The binding
  limits it to bindings carrying a per-context label (`matchResources.objectSelector`); other updates pass. Before the
  trigger, wait until the rule is in force (`Eventually` a dry-run snapshot change on the labeled binding is
  rejected). With one such binding among three, trigger → `Consistently`: snapshot 0 exists and no snapshot 1 (retries
  reuse it), the denied binding untouched, no timestamp, and the role held by `auto-rollout` across retries (the others
  may or may not have moved, due to fail fast). Delete the policy binding, wait until a dry-run change is accepted,
  then touch a harmless test annotation on the policy: role holders are always enqueued, which skips the backoff
  (about 10s by then). → all bindings on snapshot 0, timestamp written, role released. Both admission objects are
  cluster-scoped; each context creates its own (random names) and deletes them in `AfterAll`.

### D. Minimum wait period (`lifecycle_integration_test.go`)

- **D1 Recent rollout blocks.** Timestamp = now − 2m (default 3m); trigger → `Consistently` no rollout, timestamp
  unchanged (exact string), no claim. Set the min-wait annotation to `"1m"` → rolled out right away.
- **D2 Requeue after the remaining time.** Min wait `"1m"`; write timestamp = now − 45s right before the trigger
  (after the cache wait, so that setup time does not eat into the ~15s left) → `Consistently` (5s) no rollout, then
  `Eventually` (20s) rolled out with no further writes.
- **D3 Bounds.** Min wait `"1s"`, timestamp = now − 10s → `Consistently` no rollout (clamped up to 1m). Then min wait
  `"5h"` with timestamp = now − 2h, in one write → rolled out (clamped down to 1h).
- **D4 Malformed values.** Min wait `"soon"`, timestamp = now − 2m → `Consistently` no rollout (default 3m, not the
  1m lower bound). Then timestamp `"not-a-timestamp"` plus min wait `"later"` (to force a reconcile) → rolled out;
  the malformed timestamp is replaced.

### Better as unit tests (out of scope here; the package has no unit tests yet)

- `retrieveMinimumWaitPeriod`, `remainingWaitPeriod`, `isRolloutNeeded`, `shouldSkipProcessing` (full matrices).
- `autoRolloutRelevantChange` and the event handler funcs, including the restart path (a create event for a
  role-holding, disabled policy), which envtest cannot reach without restarting the manager; `castToPlacementPolicy`.
- `New` argument checks and defaults (including the required event recorder); `refreshBindings` fail fast, error
  aggregation, and context cancellation (fake client with `interceptor.Funcs`, as the placement policy controller unit
  tests do); rate limiter bookkeeping; the user-error branch's write order (timestamp before release) and its handling
  of a failure of either write.

## Behaviors to note (documented only; no action for now)

1. **Resolved (October 21, 2026).** The timestamp is now written only after a successful rollout (or, since
   October 22, a user-error failure; behavior 8), so other failures are retried promptly (C8). Writing it afterwards
   with the old resource-version-guarded `Update` would have opened a
   race: in production the placement policy controller owns the bindings, reacts to their generation bumps, and
   rewrites the policy's `Synchronized` condition. That changes the policy's resource version, so the `Update`
   conflicts. On the retry, if every binding already pointed to the latest snapshot, `Synchronized` would read
   `False/FailedToSynchronizeSomeClusters`; no rollout would count as needed, the role would be released, and no
   timestamp would ever be written, so the next resource change would roll out regardless of the minimum wait period.
   `bookkeepLastRolloutAttemptTimestamp` now uses `client.MergeFrom` (decided with the user over a JSON patch). Verified
   against envtest 1.33 with throwaway tests (now removed), writing from a stale copy after concurrent status, spec,
   and annotation changes:
   - full `Update` (old code): conflict;
   - `client.MergeFrom(original)`: the body is only `{"metadata":{"annotations":{"<timestamp key>":"..."}}}`, even
     when the stale copy had no annotations; the concurrent status, spec, and annotation changes all survive. Checked
     again through the real `bookkeepLastRolloutAttemptTimestamp` for both scopes;
   - JSON patch `add` on `/metadata/annotations/kubefleet.dev~1auto-rollout-last-rollout-attempt-timestamp`: works when
     the annotations map exists, but is rejected when the object has none (the parent must exist), and the `/` in the
     key must be escaped as `~1`.
   The race is not testable in the integration suite (see constraint 9); a unit test with `interceptor.Funcs` belongs
   with the follow-up unit tests.
2. Bindings missing from the cache at rollout time are skipped; the rollout still counts as a success, so they wait
   for the next one.
3. Changes to selected resources do not trigger the controller; it relies on the placement policy controller updating
   the conditions. Rewriting identical conditions does not trigger it either.
4. While the conditions keep calling for a rollout (e.g., the placement policy controller lags), the controller
   re-evaluates every wait period; with unchanged resources only the timestamp moves (C7).
5. A claim under the `auto-rollout` name whose refs do not include the policy itself is never cleaned up: claiming
   appends the policy ref, and relinquishing removes only that ref.
6. The timestamp is written as RFC3339Nano and parsed as RFC3339; Go accepts fractional seconds there, so this is
   fine, but the asymmetry is worth knowing.
7. While a rollout keeps failing with a non-user error (e.g., a binding update the API server rejects, C8), the
   controller holds the binding manager role across retries, so the placement policy controller and staged update
   runs cannot manage the bindings meanwhile. Disabling auto-rollout releases it. Retries do not wait for the minimum
   wait period, but they back off exponentially and create a new snapshot only if the selected resources changed.
8. **Resolved (October 22, 2026).** A user-error failure (in practice, from snapshotting: e.g., a selected resource
   too large for a snapshot, which only snapshot creation checks, so the placement policy controller does not report
   it) reports a Warning event, writes the timestamp, releases the role, and requeues after the minimum wait period.
   Without the timestamp (the first version of this path), C6 found a hot loop: the claim is a status update, which
   the event handler always enqueues; the next reconcile passed the wait period check (nothing recorded the failed
   attempt) and claimed, failed, and released again, about 10 times a second per policy; the returned `RequeueAfter`
   never came into play. Decided with the user over taking the snapshot before the claim (no write on a user error,
   but a snapshot could then be taken while another controller holds the role) and over an in-memory backoff (lost on
   restarts). The timestamp is written before the role is released, so the wait period holds retries back even if
   the release fails. As a consequence, the annotation records the last rollout attempt that succeeded or failed due
   to a user error, hence its name, `last-rollout-attempt-timestamp` (renamed from `last-rolled-out-timestamp`), and
   fixing the resource takes effect at the next rollout, i.e., up to one wait period later.

## Decisions (October 21, 2026)

1. **Approved.** Run every group for both scopes as two `Describe` blocks (the staged update run precedent).
2. **Approved.** Keep the package in `make test` (estimated ~4–5 min serial); no `TestMain` skip and no dedicated CI
   step.
3. **Approved.** Include C8 (failure injection with a `ValidatingAdmissionPolicy`, supported by envtest 1.33), as
   redesigned for the timestamp being written after the rollout (see behavior 1).
4. **Approved.** The unit tests listed above are a follow-up, not part of this work.
5. **Approved.** File layout: A, B, and D share `lifecycle_integration_test.go`, and C goes in
   `rollout_integration_test.go`. Each group keeps its own `Describe` blocks (see the table under Test plan).
6. **Approved (October 22, 2026).** Cover the user-error path in an existing scenario (C6, both scopes) rather than a
   new one; throttle user-error retries by writing the timestamp (behavior 8).

## Estimated runtime

About 18 five-second `Consistently` windows per scope, plus short `Eventually` waits (D2 ~10s): ~2–2.5 min per
scope, ~4–5 min serial for both, ~1.5 min wall with `ginkgo -p --procs=4`.

Measured with group A only (72 specs, both scopes): `ginkgo -p --race` ~40s of spec time, ~49s wall; `go test -race`
(serial, as `make test` runs it) ~135s. Group A holds 10 of the ~18 `Consistently` windows per scope, so the full
suite is likely around ~5 min serial.

Measured with groups A and C (138 specs, both scopes): `ginkgo -p --race` ~52s of spec time, ~55s wall.

Measured with groups A, B, and C (162 specs, both scopes): `ginkgo -p --race` ~48s of spec time, ~57s wall.

Measured with the complete suite, groups A–D (192 specs, both scopes): `ginkgo -p --race` ~52s of spec time, ~62s
wall.

Measured after the C6 user-error steps (198 specs, both scopes; one more 5-second `Consistently` per scope):
`ginkgo -p --race` ~66s of spec time, ~74s wall.

## Running the tests

Always with the Ginkgo CLI in parallel mode (user request, October 22, 2026), never serially with `go test`; this
applies to local runs only (CI keeps the package in `make test`, per decision 2):

```bash
KUBEBUILDER_ASSETS="$(make -s kubebuilder-assets-path | tail -1)" \
  go run github.com/onsi/ginkgo/v2/ginkgo -p --race ./pkg/v1/controllers/autorollout
```

Add `--focus="<Describe/Context text>"` to run a subset (see the `Describe` table under Test plan).

## Status

- October 21, 2026: plan drafted; decisions 1, 2, and 4 approved, decision 3 (C8) still open. No test code written.
- October 21, 2026 (later): the controller now writes the timestamp after a successful rollout, and the annotation was
  renamed to `last-rolled-out-timestamp`. The plan is updated to match (flow, helpers, C8 redesign, behaviors 1, 2,
  and 7). Review finding: the resource-version-guarded `Update` could lose the timestamp to a race with the
  placement policy controller (behavior 1).
- October 21, 2026 (later): `bookkeepLastRolledOutTimestamp` in `rollout.go` now writes the annotation with
  `client.MergeFrom`. Build, vet, and an envtest check of the function (both scopes, a stale copy, concurrent changes)
  pass. `golangci-lint` reports only the missing package comment (ST1000), which predates this change.
- October 21, 2026 (later): decision 3 approved (C8 included); A, B, and D moved into `lifecycle_integration_test.go`
  with their own `Describe` blocks (decision 5); C8 admission helpers added to the utilities. All decisions are
  settled; the plan is ready for implementation. No test code written yet.
- October 21, 2026 (later): **group A implemented** (A1–A5, both scopes): `suite_test.go`, `utils_integration_test.go`,
  and `lifecycle_integration_test.go`. 72 of 72 specs pass with `ginkgo -p --race` and with serial `go test -race`.
  `go vet` passes; `golangci-lint` (no issue caps) reports only ST1000 on the controller sources (pre-existing; no
  package comment). Mutation check, each mutation reverted afterwards: auto-rollout always enabled → A2 and A3 fail;
  `Synchronized` reason ignored → A4(d) fails; condition generations ignored → A4(e) fails; deletion ignored → A5
  fails; no release on the skip path → every rollout check fails (A1–A4); no release on the deletion path → A5's
  release check fails. Next: groups B, C, and D.
- October 22, 2026: **group C implemented** (C1–C8, both scopes) in `rollout_integration_test.go`; 138 of 138 specs
  (A and C) pass with `ginkgo -p --race`. `go vet` passes; `golangci-lint` reports only the pre-existing ST1000.
  Mutation check against the namespaced block, each mutation reverted afterwards: controller ownership ignored (MC1)
  → C3 fails; bindings being deleted included (MC2) → C3 fails; never re-snapshot (`SnapshotResourcesIfNoSnapshotExists`,
  MC3) → C6 fails; binding update errors ignored (MC4) → C8 fails; timestamp written before the rollout, i.e. the
  old order (MC5) → C8 fails; namespace filter dropped (MC6) → initially undetected, now C4 fails after adding the
  cross-namespace owner binding. Per the user, tests are now run only with `ginkgo -p` (see Running the tests).
  Next: groups B and D.
- October 22, 2026 (later): **group B implemented** (B1–B4, both scopes) in `lifecycle_integration_test.go`, as the
  `managing the binding manager role of <scope>` `Describe` blocks; 162 of 162 specs (A, B, and C) pass with
  `ginkgo -p --race`. `go vet` passes; `golangci-lint` reports only the pre-existing ST1000. Mutation check against
  the namespaced block, each mutation reverted afterwards: claim always succeeds (MB1) → B1's no-rollout check fails;
  role holders not enqueued on updates (MB2) → B2's release check fails; no requeue after a claim conflict (MB3) →
  B1's rollout-after-release check fails; claim kept when only the wait period holds a rollout back (MB4) → B3's
  release check fails, and so do B1's and B4's rollout checks (a completed rollout releases its claim on that path).
  Next: group D.
- October 22, 2026 (later): **group D implemented** (D1–D4, both scopes) in `lifecycle_integration_test.go`, as the
  `enforcing the minimum wait period between auto-rollouts of <scope>` `Describe` blocks. **All planned scenarios
  (A1–A5, B1–B4, C1–C8, D1–D4) are implemented**; 192 of 192 specs pass with `ginkgo -p --race` (~62s wall).
  `go vet` passes; `golangci-lint` reports only the pre-existing ST1000 on the controller sources. Mutation check
  against the namespaced block, each mutation reverted afterwards: min-wait annotation ignored (MD1) → D1 and D2 fail;
  no lower clamp (MD2) → D3's first step fails; no upper clamp (MD3) → D3's second step fails; malformed period falls
  back to the lower bound instead of the default (MD4) → D4's first step fails; malformed timestamp blocks instead of
  counting as never rolled out (MD5) → D4's second step fails; no requeue while waiting (MD6) → D2 fails; min-wait
  changes not treated as relevant by the event handler (MD7) → D1, D3, and D4 fail. Remaining follow-ups: the unit
  tests listed under "Better as unit tests" (decision 4), and a package comment for the controller (ST1000).
- October 22, 2026 (later): review fixes in the controller: relinquish only when the cached policy shows the role
  held; a user error during the rollout reports a Warning event, releases the role, and requeues after the minimum
  wait period (`New` now takes a required `events.EventRecorder`); the remaining wait is capped at the period; an
  empty snapshot list is an unexpected error. **C6 now covers the user-error path** (both scopes; decision 6). Its
  first run found the hot loop in behavior 8 (the no-retry check failed in both scopes against the code without the
  timestamp write, which doubles as the mutation check); with the fix, 198 of 198 specs pass with `ginkgo -p --race`.
  `go vet` passes; `golangci-lint` reports no issues on the package.
- October 22, 2026 (later): the timestamp annotation is renamed to `kubefleet.dev/auto-rollout-last-rollout-attempt-timestamp`
  (`AutoRolloutLastRolloutAttemptTimestampAnnotationKey`; `bookkeepLastRolloutAttemptTimestamp`), as user-error
  failures write it too (behavior 8); the code, the test helpers and step texts, and this plan (except the entries
  above) use the new name.
