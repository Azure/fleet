# Enforce selected-resource authorization

**Date**: October 8, 2026 17:29 UTC
**Task**: Prevent placement and override APIs from using Fleet's controller permissions to access resources the requester could not access directly.

## Requirements

1. Enforce selected-resource authorization for ClusterResourcePlacement, ResourcePlacement, ClusterResourceOverride, and ResourceOverride create and update requests.
2. Build SubjectAccessReview requests from the admission request's complete user information: username, UID, groups, and extras.
3. Fail closed when resource resolution or authorization cannot be completed.
4. Require `get` for resources selected by name.
5. For CRP/RP label and all-of-kind selectors, require `list` in the relevant scope, without individual `get` checks for enumerated objects.
6. For namespace selection, authorize the Namespace selection using `get` by name or `list` for label/all-of-kind selection. For NamespaceWithResources, require `list` per eligible expanded namespaced resource type, without individual `get` checks for their objects.
7. Ignore resources that begin matching dynamic selectors after admission in this implementation.
8. Require `patch` for JSONPatch overrides and `delete` for Delete overrides.
9. Require Kubernetes RBAC privilege-protection checks when overrides affect Role, ClusterRole, RoleBinding, or ClusterRoleBinding resources.
10. For CRP/RP, authorize creates and reauthorize the complete selected set on spec-changing updates, even if selectors are unchanged. Overrides retain the original every-update reauthorization requirement.
11. For CRP/RP, skip selected-resource authorization on updates with semantically unchanged specs, including label, annotation, and finalizer changes regardless of deletion state. Preserve existing validation behavior. For overrides, retain the deletion/finalizer cleanup exception.
12. Enable enforcement by default and provide a temporary feature flag to disable it.

## Plan

### Phase 1: Authorization foundation and tests

- [x] Add a selected-resource reviewer that creates Kubernetes SubjectAccessReviews and preserves all admission user information.
- [x] Add RESTMapper-based conversion from selector GVKs to the plural GVRs required by SubjectAccessReview.
- [x] Deduplicate identical checks within one authorization batch and use bounded concurrency for object-level reviews.
- [x] Add unit tests for allowed, denied, no-opinion, API-error, timeout, identity-copying, and duplicate-review cases.
- **Success criterion:** Authorization behavior is independently testable and every unresolved or unsuccessful review fails closed.

### Phase 2a: ResourcePlacement authorization (current slice)

- [x] Build RP permission checks from selectors and REST mappings without fetching source objects.
- [x] Require `get` for named selections and `list` for label/all-of-kind selections in the RP namespace.
- [x] Authorize creates and spec-changing updates; preserve existing validation and deletion cleanup behavior.
- [x] Add RP constructor, scope, selector, overlapping-review, identity, denial, and failure tests.
- **Success criterion:** RP authorization is independently testable, fails closed, and derives get/list checks without fetching source objects.

### Phase 2b: ClusterResourcePlacement authorization (follow-up slice)

- [x] Add direct cluster-scoped resource authorization.
- [x] Resolve selected Namespaces using live hub API reads.
- [x] Add NamespaceOnly, NamespaceWithResourceSelectors, and NamespaceWithResources authorization.
- [x] Add focused CRP authorizer unit tests for namespace modes, scope, overlapping selectors, eligibility, fresh reads, and failures.
- **Success criterion:** CRP authorization follows the agreed get/list policy without expanding the RP-only review scope.

### Phase 3: Override authorization

- [ ] Authorize every currently selected CRO and RO target.
- [ ] Require `patch` for JSONPatch rules, including `remove` operations on fields, and `delete` when a Delete rule removes the entire selected resource from a member cluster.
- [ ] Apply or inspect override patches sufficiently to perform correct `escalate` checks for Role/ClusterRole rule changes and `bind` checks against the resulting RoleBinding/ClusterRoleBinding role reference.
- [ ] Add CRO and RO webhook tests for patch, delete, escalate, bind, namespaced scope, update reauthorization, and fail-closed behavior.
- **Success criterion:** Overrides cannot mutate, suppress, escalate, or bind selected RBAC resources beyond the requester's direct authority.

### Phase 4: Wiring, RBAC, and rollout

- [ ] Add a default-enabled hub-agent feature flag that can temporarily disable selected-resource authorization.
- [ ] Pass the RESTMapper, uncached/live resource reader, and SAR client into all four validating webhooks.
- [ ] Grant the hub-agent service account `create` on `authorization.k8s.io/subjectaccessreviews`.
- [ ] Update directly related Helm values and user-facing flag documentation.
- **Success criterion:** Default installations enforce authorization, while operators have an explicit temporary rollback switch.

### Phase 5: Verification

- [ ] Run formatting and focused authorization, webhook, and option tests.
- [ ] Run the smallest relevant integration tests using real Kubernetes RBAC authorization.
- [ ] Add focused RP/CRP webhook authorization wiring and update-gating coverage.
- [ ] Run `make reviewable` before submission.
- [ ] Update this breadcrumb with implementation details, changed files, and validation results.
- **Success criterion:** Focused and repository-required checks pass with no authorization bypass or unrelated behavior regression.

## Decisions

1. Cover all four v1beta1 APIs in separate reviewable slices: RP first, then CRP, CRO, and RO.
2. Dynamic resources that match after admission are an acknowledged limitation and are not blocked because dynamic selection is a core Fleet feature.
3. Placement label and all-of-kind selection requires `list` only. Named selection requires `get` for the named object.
4. NamespaceWithResources authorizes the Namespace selection and requires `list` for each relevant namespaced GVR, without individual `get` checks for enumerated objects.
5. Delete overrides require `delete`; JSONPatch overrides require `patch`.
6. CRP/RP creates and spec-changing updates authorize the complete selected set using the current requester's identity. Unchanged-spec updates skip selected-resource authorization but still undergo existing validation. Override updates retain every-update reauthorization.
7. Finalizer removal during deletion bypasses selected-resource authorization.
8. Enforcement is enabled by default behind a temporary disable flag.

## Non-goals

1. Continuous reauthorization when a new resource begins matching an existing dynamic selector.
2. Persisting creator identity for controller-time authorization.
3. Replacing Kubernetes RBAC or supporting label-restricted authorization policies beyond what SubjectAccessReview reports.

## Status

Plan approved by the user. Phase 1 and the Phase 2a RP-only slice are complete on
`feat/selected-resource-authorization-phase-2`. Phase 2b authorizer logic and unit
tests are complete on `feat/selected-resource-authorization-crp`; Phases 3-5 remain.
On October 19, the user requested a smaller review scope. The full implementation
is preserved in pushed commit `e640d9b2`; the follow-up slice retains RP only.

Phase 2a merged as kubefleet-dev/kubefleet#988. Phase 2b is based on that merged RP
implementation. Production webhook wiring, SAR RBAC, and the default-on flag
remain Phase 4.

## Phase 2a implementation details

- Added the RP authorizer in `pkg/utils/authorization/placement.go`. Phase 2b
  generalizes its type and construction for both placement APIs. RP requests
  still need no Namespace resolution or ordinary object reads. The explicit
  RP/CRP type switch initially deferred CRP; Phase 2b fills in that branch.
- Build get checks for named selectors and list checks for label/all-of-kind
  selectors in the RP namespace. No source objects are fetched. Reuse the Phase 1
  reviewer's identity preservation, deduplication, and bounded concurrency.
- Use explicit Allowed, Denied, and Indeterminate decisions. Denials take
  precedence over additional review errors; malformed results fail closed.
- Add an optional RP webhook authorizer through shared placement validation.
  Authorize creates and spec-changing updates only. Unchanged-spec updates still
  run existing validation, and the old invalid-placement deletion exception is
  unchanged. Authorizer unit tests cover scope, selectors, denials, and SAR errors;
  shared validator tests cover admission decision mapping. The dedicated RP
  webhook authorization test file was removed at the user's request; its wiring
  and update-gating coverage is deferred.
- Defer CRP production wiring and webhook tests. Its handler passes nil to the shared
  authorization parameter, preserving existing behavior. The controller's resource
  selector resolver is unchanged.
- Production registration still does not inject the RP authorizer. Default-on
  enforcement, rollout wiring, and SAR RBAC remain Phase 4.

## Phase 2b implementation details

- A single `NewPlacementAuthorizer` creates the shared `PlacementAuthorizer` for
  both RP and CRP. It validates the mapper, reviewer, eligibility checker,
  uncached Namespace reader, and resource-type inventory up front. All five are
  required even though RP requests do not use the Namespace dependencies.
  The RP/CRP-specific constructors and RP-specific type alias were removed.
  One fully configured instance can later be injected into both webhooks.
- CRP resolution composes two helpers: Namespace selection returns checks and a
  `namespaceScope` value; non-Namespace selection consumes that scope and returns
  its own checks. The outer method only validates the CRP and combines results,
  without sharing mutable state between the two passes.
  NamespaceOnly, NamespaceWithResources (including default), and
  NamespaceWithResourceSelectors have explicit separate cases. Selective mode
  requires exactly one named active Namespace and cannot mix Namespace selectors.
- NamespaceOnly builds get/list checks without any Namespace reader calls or
  existence checks. Mapping, kind eligibility, and selector validation still run.
  Expansion and selective modes resolve Namespaces live, filter deleting objects,
  and enforce content-namespace eligibility; NamespaceOnly does not apply those
  content exclusions, matching the controller.
- Before any CRP Namespace reads, authorize all Namespace get/list checks derived
  from selector metadata. Denied, no-opinion, and indeterminate Namespace reviews
  stop resolution immediately, so responses cannot disclose discovered Namespace
  names or existence through lookup errors or expanded denial reasons. After an
  allowed preflight, resolve expansion and authorize the full batch. Namespace
  checks are intentionally repeated in that batch to keep the implementation
  simple; no cross-phase filtering or deduplication optimization is used.
- Expansion builds list checks for each eligible tracked namespaced resource type
  in each active selected Namespace. Ordinary resource checks remain metadata-only:
  get by name or list for label/all-of-kind, with no per-object get checks.
- Namespace expansion skips inventory entries whose kinds no longer map
  (`NoMatch`), with a debug log, matching the controller's removed-CRD handling.
  Other mapper/eligibility failures and explicit unknown selectors still fail
  closed. Tests retain valid checks before and after a stale inventory entry.
- Added focused tests for exact attributes, all modes, multiple Namespaces,
  ordering, overlaps, exclusions, missing/deleting Namespaces, malformed selectors,
  mapping/inventory/API failures, denials, and fresh Namespace reads using the
  current requester. Tests fail on any non-Namespace reader operation.
  NamespaceOnly tests additionally require exactly zero reader calls for named,
  label, all-of-kind, and nonexistent named selections. Namespace read failures
  are tested in expansion modes, and non-NoMatch inventory mapper errors remain
  explicit failures.
  Preflight regressions assert zero Namespace reads and no expanded reviews on
  Namespace denial, no opinion, API failure, or evaluation error, including named
  missing Namespaces, label/all selections, and selective mode. Label/all failure
  responses must not contain discovered Namespace names.
- No controller resolver, shared validation, or webhook code changed. No new
  webhook test scaffold or production enforcement wiring is included.

## Phase 2b validation

- Focused uncached race tests and `go vet` passed for authorization, validator, and
  both placement webhook packages.
- `go build ./cmd/hubagent/...`, new-code lint, repository `make lint` using the
  installed v2.13.2 linter, and `git diff --check` passed.
- Go commands use `MS_GOTOOLCHAIN_ALLOW_NON_LOCAL=1 GOTOOLCHAIN=auto`.

## Phase 2a validation

- Focused race tests passed for `pkg/utils/authorization`, `pkg/utils/validator`,
  `pkg/webhook/clusterresourceplacement`, and `pkg/webhook/resourceplacement`.
- Existing CRP webhook tests passed unchanged.
- `go vet` passed for the authorization, validator, and placement webhook packages.
- `go build ./cmd/hubagent/...` passed.
- Before committing, `make lint GOLANGCI_LINT=/home/weiweng/go/bin/golangci-lint`
  passed repository-wide with the installed repository-version linter (v2.13.2).
- Repository-configured lint passed with `--new-from-rev=main`,
  `--max-issues-per-linter=0`, and `--max-same-issues=0` for the affected packages.
  Unfiltered package lint also reports pre-existing issues in the CRP mutating
  webhook tests; these were not changed.
- `git diff --check` passed. Go commands use
  `MS_GOTOOLCHAIN_ALLOW_NON_LOCAL=1 GOTOOLCHAIN=auto`, as in Phase 1.
- Full `make reviewable` and real Kubernetes RBAC integration remain Phase 5
  verification, before PR submission.

## Phase 1 implementation details

- Added `pkg/utils/authorization` with a reusable, stateless batch Reviewer.
- Preserved username, UID, groups, and deep-copied extras in every SubjectAccessReview.
- Added RESTMapper-based GVK-to-GVR conversion for correctly pluralized authorization attributes.
- Deduplicated identical attributes within each authorization batch; separate calls always perform fresh reviews.
- Limited concurrent SubjectAccessReviews per request, collected results in stable input order, and recorded explicit failures for reviews not submitted after request cancellation.
- Returned structured results that aggregate explicit denials separately from joined evaluation, API, and malformed-response errors; conclusive denials take precedence while errors remain available for logging.
- Added focused unit coverage for resource mapping, identity propagation, aggregated denial and error results, timeout handling, deduplication, and bounded concurrency.

## Phase 1 validation

- `MS_GOTOOLCHAIN_ALLOW_NON_LOCAL=1 GOTOOLCHAIN=auto go test -race ./pkg/utils/authorization` passed.
- `MS_GOTOOLCHAIN_ALLOW_NON_LOCAL=1 GOTOOLCHAIN=auto go vet ./pkg/utils/authorization` passed.
- Automatic toolchain selection was required because `go.mod` requires Go 1.26.6 while the installed local Microsoft Go toolchain is 1.26.5.

## References

- `pkg/utils/validator/placement.go` - shared CRP/RP admission validation.
- `pkg/webhook/clusterresourceplacement` - CRP mutating and validating webhooks.
- `pkg/webhook/resourceplacement` - RP validating webhook.
- `pkg/webhook/clusterresourceoverride` - CRO validating webhook.
- `pkg/webhook/resourceoverride` - RO validating webhook.
- `pkg/utils/controller/resource_selector_resolver.go` - current placement selector resolution semantics.
- `cmd/hubagent/options/featureflags.go` - hub-agent feature flag conventions.
- `charts/hub-agent/templates/rbac.yaml` - hub-agent service account permissions.
- No applicable specification directory exists under `.github/.copilot/`, and no selected-resource authorization entry was found in the domain knowledge files.
