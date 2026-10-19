# Enforce selected-resource authorization

**Date**: October 8, 2026 17:29 UTC
**Task**: Prevent placement and override APIs from using Fleet's controller permissions to access resources the requester could not access directly.

## Requirements

1. Enforce selected-resource authorization for ClusterResourcePlacement, ResourcePlacement, ClusterResourceOverride, and ResourceOverride create and update requests.
2. Build SubjectAccessReview requests from the admission request's complete user information: username, UID, groups, and extras.
3. Fail closed when resource resolution or authorization cannot be completed.
4. Require `get` for resources selected by name.
5. For label and all-of-kind selectors, require `list` in the relevant scope and `get` for every currently selected object.
6. For namespace selection, authorize the Namespace selection and, for NamespaceWithResources, require `list` per selected namespaced resource type and `get` for every currently selected resource.
7. Ignore resources that begin matching dynamic selectors after admission in this implementation.
8. Require `patch` for JSONPatch overrides and `delete` for Delete overrides.
9. Require Kubernetes RBAC privilege-protection checks when overrides affect Role, ClusterRole, RoleBinding, or ClusterRoleBinding resources.
10. Reauthorize the complete selected set on every update, including updates that do not change selectors.
11. Allow finalizer-removal updates on objects already being deleted without selected-resource reauthorization.
12. Enable enforcement by default and provide a temporary feature flag to disable it.

## Plan

### Phase 1: Authorization foundation and tests

- [x] Add a selected-resource reviewer that creates Kubernetes SubjectAccessReviews and preserves all admission user information.
- [x] Add RESTMapper-based conversion from selector GVKs to the plural GVRs required by SubjectAccessReview.
- [x] Deduplicate identical checks within one authorization batch and use bounded concurrency for object-level reviews.
- [x] Add unit tests for allowed, denied, no-opinion, API-error, timeout, identity-copying, and duplicate-review cases.
- **Success criterion:** Authorization behavior is independently testable and every unresolved or unsuccessful review fails closed.

### Phase 2: Placement authorization

- [ ] Resolve the current selected set using live hub API reads rather than stale informer data.
- [ ] Authorize CRP and RP name, label, all-of-kind, NamespaceOnly, NamespaceWithResourceSelectors, and NamespaceWithResources selection modes.
- [ ] Require selector-level `list` permission where Fleet enumerates resources and object-level `get` permission for every currently selected object.
- [ ] Preserve the existing deletion/finalizer cleanup escape hatch.
- [ ] Add CRP and RP webhook tests covering scope, selector modes, overlapping selectors, update reauthorization, denied access, and API failures.
- **Success criterion:** A placement is admitted only when the requester can list each expanded scope and get every currently selected source object.

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
- [ ] Run `make reviewable` before submission.
- [ ] Update this breadcrumb with implementation details, changed files, and validation results.
- **Success criterion:** Focused and repository-required checks pass with no authorization bypass or unrelated behavior regression.

## Decisions

1. Cover all four v1beta1 APIs in the first implementation: CRP, RP, CRO, and RO.
2. Dynamic resources that match after admission are an acknowledged limitation and are not blocked because dynamic selection is a core Fleet feature.
3. Label and all-of-kind selection requires `list`, followed by `get` checks for the currently resolved objects.
4. NamespaceWithResources checks the currently selected resources by requiring `list` for each relevant namespaced GVR and `get` for each selected object.
5. Delete overrides require `delete`; JSONPatch overrides require `patch`.
6. Every update reauthorizes the selected set using the updating requester's identity.
7. Finalizer removal during deletion bypasses selected-resource authorization.
8. Enforcement is enabled by default behind a temporary disable flag.

## Non-goals

1. Continuous reauthorization when a new resource begins matching an existing dynamic selector.
2. Persisting creator identity for controller-time authorization.
3. Replacing Kubernetes RBAC or supporting label-restricted authorization policies beyond what SubjectAccessReview reports.

## Status

Plan approved by the user. Phase 1 is complete; Phases 2-5 have not started.

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
