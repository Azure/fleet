# Admission Policy Manager Integration Test Plan

**Date**: October 22, 2026 23:00 AEDT
**Task**: Add integration tests in `pkg/admissionpolicymanager` that verify the **effects** (not only the existence) of
the validating admission policies (VAPs) from all three built-in generators. See [Status](#status) for progress.

## Requirements

1. Verify, against a real API server (envtest), that each VAP denies exactly the requests it is meant to deny and
   lets everything else through.
2. Follow repo conventions: Ginkgo/Gomega, `<file>_integration_test.go`, one `Ordered` `Describe` per generator,
   `want` naming, random name suffixes (`utils.RandStr()`), whole-struct `cmp.Diff` comparisons.
3. No production code changes, except for decisions 3–5.

## Policies under test

| Generator | Matches | Allowed when | Denial message |
|-----------|---------|--------------|----------------|
| `DenyPodsAndReplicaSetsOutsideReservedNamespaces` | CREATE `pods`, `replicasets` | namespace starts with `fleet-` or `kube-` (no exemptions for any caller) | `creating pods and replicas is disallowed in the fleet hub cluster` |
| `DenyServiceAccountsAndTokenRequestsInReservedNamespaces` | CREATE/UPDATE/DELETE `serviceaccounts`; CREATE `serviceaccounts/token` | namespace is not `fleet-*`/`kube-*`, **or** caller is `system:kube-scheduler`, `system:kube-controller-manager`, or in `system:nodes`, `system:masters`, `kubeadm:cluster-admins`, `system:serviceaccounts` (+ configured whitelists) | `writing service accounts in reserved namespaces or requesting tokens from such service accounts is disallowed` |
| `DenyPlacementAnnotationIfNoPlacementPolicyAPIAccess` | CREATE/UPDATE on all resources and subresources (`*/*`); match conditions: the new object has `kubefleet.dev/cluster-selectors`, and the annotation is added or its value changed | caller can `create` **and** `update` `placementpolicies` in the object's namespace (namespaced objects) or `clusterplacementpolicies` (cluster-scoped objects) | `adding or changing the kubefleet.dev/cluster-selectors annotation on a namespaced/cluster-scoped object requires ...` |

## Test environment findings and design implications

Verified on October 22, 2026 with envtest 1.30.0, 1.33.0 (the CI version), and 1.37.0: all policies are enforced.

1. **Enforcement lags policy creation by ~1s.** The VAP admission plugin recompiles policies on a timer
   (`policyRefreshIntervalDefault = 1 * time.Second` in `k8s.io/apiserver/pkg/admission/plugin/policy/generic`); a
   request sent right after `PolicyManager.Start` is not checked. This explains the outdated note at the end of
   `manager_integration_test.go`, which will be removed. Each `Describe`'s `BeforeAll` waits (`Eventually`) until a
   probe request that the policy must deny is denied.
2. **The envtest client is `system:masters`.** It has all permissions and is exempt from the service account policy.
   Permission-dependent cases use impersonated identities (`rest.Config.Impersonate`).
3. **RBAC runs before admission, and an RBAC denial is also `Forbidden`.** Every test identity is granted RBAC for the
   target resource, and denial assertions check `apierrors.IsForbidden` **plus** the policy name
   (`ValidatingAdmissionPolicy '<name>'`) and the validation message, so that an RBAC denial can never pass as a
   policy denial.
4. **Top-level containers run in random order.** Each `Describe` is self-contained (own namespaces, identities, and
   enforcement wait).
5. **No namespace controller, GC, or other controllers.** Namespaces cannot be deleted, so names are random and
   namespaces are not cleaned up (as in the existing test). `AfterAll` deletes the cluster-scoped objects a `Describe`
   creates (ClusterRoles, ClusterRoleBindings). Finalizer removal (D15) is done by hand.
6. **envtest disables the `ServiceAccount` admission plugin**, so pods need no service account; service account
   token signing is configured, so TokenRequests work. `NodeRestriction` is not enabled, so impersonating a node
   user is fine.
7. **One generator config per suite.** The policy manager garbage-collects policies outside its config. The suite
   switches from `DefaultPolicyGeneratorConfigsForV0APIs` to the generators of `DefaultPolicyGeneratorConfigsForV1APIs`
   (all three), with one whitelisted username and one whitelisted user group added to the service account generator
   (decision 1). The suite builds its own `PolicyGeneratorConfigs` for this rather than mutating the shared default.
8. These tests run in CI through `make local-unit-test` (`go test ./pkg/...`), serially.

## Changes

- `suite_test.go`: use the V1 generators, with the service account generator configured with
  `WhitelistedUsernames: ["whitelisted-user"]` and `WhitelistedUserGroups: ["whitelisted-group"]`; install the
  `TestResource` CRD (`test/manifests/test_testresources_crd.yaml`) through `CRDInstallOptions.Paths` for the custom
  resource cases (accessed as `unstructured.Unstructured`, so no scheme change).
- `manager_integration_test.go`: add the third policy (with its match conditions) and binding to the want lists;
  remove the outdated note.
- `utils_integration_test.go` (new): helpers — impersonated client builder, RBAC grant helpers (Role/ClusterRole +
  binding), `wantDeniedByPolicy(err, policyName, wantMsg) error`, `waitUntilPolicyIsEnforced(probe)`, object builders.
- `podsnreplicasets_integration_test.go`, `svcaccountsntokenreqs_integration_test.go`,
  `annotatedforplacementobjects_integration_test.go` (new): one `Describe` each (below).
- `annotatedforplacementobjects.go` (decision 3): match `*/*` instead of `*`.
- `cmd/hubagent/main.go`, `cmd/hubagent/options/webhooks.go` (decision 4): pick the default configuration by
  `--enable-placement-policy-apis`; update the `--admission-policy-manager-config` help text.
- `annotatedforplacementobjects.go`, `commons.go` (decision 5): guard the added-or-changed match condition with
  `objectHasAnnotation`.
- `annotatedforplacementobjects.go`, `configs.go` (review nits, October 23, 2026): drop the stale TO-DO header;
  document that the generator is enabled by setting it to an empty struct (`{}`), and disabled by an empty (null)
  value.

## Test cases

### A. Policies and bindings (`manager_integration_test.go`, existing `Describe`)

- A1. All three policies exist with the wanted spec (system defaults filled in), including the two match conditions of
  the placement annotation policy, and the whitelisted username and group in the service account policy's expression.
- A2. All three bindings exist with the wanted spec.

### B. Pods and ReplicaSets (`podsnreplicasets_integration_test.go`)

Setup: namespaces `work-<rand>` (non-reserved), `fleet-<rand>` (reserved), `fleetwork-<rand>` (prefix boundary). All
requests are made as the envtest admin, which also shows that `system:masters` is not exempt.

| ID | Request | Want |
|----|---------|------|
| B1 | create a Pod in `work-<rand>` | denied |
| B2 | create a ReplicaSet in `work-<rand>` | denied |
| B3 | create a Pod in `fleetwork-<rand>` (starts with `fleet` but not `fleet-`) | denied |
| B4 | create a Pod and a ReplicaSet in `fleet-<rand>` | allowed |
| B5 | create a Pod and a ReplicaSet in `kube-system` | allowed |
| B6 | create a Deployment in `work-<rand>` (kind not matched) | allowed |

### C. Service accounts and token requests (`svcaccountsntokenreqs_integration_test.go`)

Setup: namespaces `work-<rand>`, `fleet-<rand>` (plus the existing `kube-system`); a ClusterRole granting
`serviceaccounts` (get/create/update/delete) and `serviceaccounts/token` (create), bound to every identity below;
service accounts seeded by the admin in `fleet-<rand>` and `work-<rand>`.

Identities: `plain` (no exemption); exempt — the admin (`system:masters`), `system:kube-scheduler`,
`system:kube-controller-manager`, a user in `system:nodes`, a user in `kubeadm:cluster-admins`, a user in
`system:serviceaccounts`; whitelisted (suite config) — `whitelisted-user`, a user in `whitelisted-group`; and
`whitelisted-user-2` (not whitelisted; checks that usernames are matched exactly, not by prefix).

| ID | Request | Want |
|----|---------|------|
| C1 | `plain` creates a ServiceAccount in `fleet-<rand>` | denied |
| C2 | `plain` creates a ServiceAccount in `kube-system` | denied |
| C3 | `plain` updates the seeded ServiceAccount in `fleet-<rand>` | denied |
| C4 | `plain` deletes the seeded ServiceAccount in `fleet-<rand>` | denied |
| C5 | `plain` requests a token for the seeded ServiceAccount in `fleet-<rand>` | denied |
| C6 | `plain` creates, updates, and deletes a ServiceAccount in `work-<rand>` | allowed |
| C7 | `plain` requests a token for the seeded ServiceAccount in `work-<rand>` | allowed |
| C8–C13 | each exempt identity creates a ServiceAccount in `fleet-<rand>` and requests a token for it | allowed |
| C14 | `whitelisted-user` creates a ServiceAccount in `fleet-<rand>` and requests a token for it | allowed |
| C15 | a user in `whitelisted-group` creates a ServiceAccount in `fleet-<rand>` and requests a token for it | allowed |
| C16 | `whitelisted-user-2` creates a ServiceAccount in `fleet-<rand>` | denied |

### D. Placement annotation (`annotatedforplacementobjects_integration_test.go`)

Setup: namespaces `work-<rand>`, `other-<rand>`; a ClusterRole granting writes to the target objects (ConfigMaps,
TestResources, Namespaces and their `status`/`finalize` subresources, Deployments and their `status`/`scale`
subresources, ClusterRoles), bound to every identity below; objects seeded by the admin as needed.

| Identity | Placement policy permissions |
|----------|------------------------------|
| `no-perm` | none |
| `pp-ns` | `placementpolicies` create + update in `work-<rand>` (Role) |
| `pp-ns-create-only` | `placementpolicies` create only in `work-<rand>` (Role) |
| `pp-cluster-wide` | `placementpolicies` create + update in all namespaces (ClusterRole); no `clusterplacementpolicies` |
| `cpp` | `clusterplacementpolicies` create + update; no `placementpolicies` |

Namespaced objects (ConfigMaps, in `work-<rand>` unless noted):

| ID | Request | Want |
|----|---------|------|
| D1 | `no-perm` creates a ConfigMap without the annotation | allowed |
| D2 | `no-perm` creates an annotated ConfigMap | denied |
| D3 | `pp-ns-create-only` creates an annotated ConfigMap (both verbs are required) | denied |
| D4 | `pp-ns` creates an annotated ConfigMap | allowed |
| D5 | `pp-ns` creates an annotated ConfigMap in `other-<rand>` (permissions are per namespace) | denied |
| D6 | `pp-cluster-wide` creates an annotated ConfigMap in `other-<rand>` | allowed |
| D7 | `cpp` creates an annotated ConfigMap (cluster permissions do not apply to namespaced objects) | denied |
| D8 | the admin creates an annotated ConfigMap | allowed |
| D9 | `no-perm` adds the annotation to an existing ConfigMap (Update) | denied |
| D10 | `no-perm` adds the annotation with a JSON merge patch (the `kubectl annotate` path) | denied |
| D11 | `no-perm` changes the annotation value | denied |
| D12 | `no-perm` updates data, labels, and other annotations of an annotated ConfigMap, leaving the annotation as is | allowed |
| D13 | `no-perm` removes the annotation | allowed |
| D14 | `pp-ns` adds, then changes the annotation | allowed |
| D15 | `no-perm` deletes an annotated ConfigMap that has a finalizer, then removes the finalizer (the GC scenario) | allowed; the object is gone |

Namespaced custom resources (`TestResource`, untyped CEL path):

| ID | Request | Want |
|----|---------|------|
| D16 | `no-perm` creates an annotated TestResource | denied |
| D17 | `pp-ns` creates an annotated TestResource | allowed |

Cluster-scoped objects:

| ID | Request | Want |
|----|---------|------|
| D18 | `no-perm` creates an annotated Namespace | denied (cluster-scoped message) |
| D19 | `pp-cluster-wide` creates an annotated Namespace (`request.namespace` is set for Namespaces, but they must still be checked against `clusterplacementpolicies`) | denied |
| D20 | `cpp` creates an annotated Namespace | allowed |
| D21 | `no-perm` adds the annotation to an existing Namespace; `cpp` does the same | denied; allowed |
| D22 | `no-perm` creates an annotated ClusterRole (no rules); `cpp` does the same | denied; allowed |

Cluster-scoped custom resources need no case of their own: the policy has no logic specific to them, and the two
aspects involved are covered separately — the untyped CEL path by D16/D17, and scope detection
(`object.metadata.namespace`) by D18–D22.

Subresources (decision 3; some built-in resources keep annotation changes sent to their `status`/`finalize`
subresources):

| ID | Request | Want |
|----|---------|------|
| D23 | `no-perm` adds the annotation to a Deployment through `deployments/status` | denied |
| D24 | `no-perm` updates the status of an annotated Deployment, leaving the annotation as is | allowed |
| D25 | `no-perm` scales an annotated Deployment through `deployments/scale` (the subresource carries a `Scale` object) | allowed |
| D26 | `no-perm` adds the annotation to a Namespace through `namespaces/status`; `cpp` does the same | denied (cluster-scoped message); allowed |
| D27 | `no-perm` adds the annotation to a Namespace through `namespaces/finalize`; `cpp` does the same | denied (cluster-scoped message); allowed |

## Not covered

- Subresources of built-in resources other than Deployments and Namespaces.
- CEL evaluation errors and `failurePolicy: Fail`.

## Decisions

1. Whitelist coverage (October 22, 2026): the suite uses the V1 generators plus one whitelisted username and one
   whitelisted user group for the service account generator (C14–C16), instead of the plain defaults.
2. File layout (October 22, 2026): one file per generator plus `utils_integration_test.go`.
3. Match subresources (October 23, 2026): review found that a caller without placement policy permissions could add
   the annotation through `deployments/status`, `namespaces/status`, and `namespaces/finalize` (the built-in status
   strategies of these resources do not reset annotations; custom resources do), which the policy did not match
   (`*`). The policy now matches `*/*`; the added-or-changed match condition keeps routine status writes unchecked,
   and subresources that carry other kinds of objects (e.g., `Scale`) are skipped by the has-annotation condition.
4. Default configuration wiring (October 23, 2026): when no admission policy manager config file is given, the hub
   agent uses `DefaultPolicyGeneratorConfigsForV1APIs` if `--enable-placement-policy-apis` is set, and
   `DefaultPolicyGeneratorConfigsForV0APIs` otherwise (`cmd/hubagent/main.go`); the
   `--admission-policy-manager-config` help text is updated to match. A given config file still takes precedence.
5. Self-contained added-or-changed match condition (October 23, 2026): on annotation removal, the condition indexed
   the missing key in `object.metadata.annotations` and failed with a no-such-key error. This had no visible effect,
   as the API server stops at the first match condition that is false (has-annotation, listed first) before it
   looks at errors from later ones; with the two conditions swapped, each removal was counted in
   `apiserver_admission_match_condition_evaluation_errors_total`. The condition now also checks that the object
   carries the annotation (`objectHasAnnotation`); with the conditions swapped, no errors are counted. D13 covers
   the behavior; A1 is updated with the new expression.

## Status

- [x] Plan drafted; decisions 1 and 2 recorded
- [x] Plan approved (October 22, 2026)
- [x] Suite and existing test updates (A)
- [x] Helpers
- [x] Pods and ReplicaSets (B): B1–B6 implemented as 6 specs
- [x] Service accounts and token requests (C): C1–C16 implemented as 16 specs (C8–C15 as a `DescribeTable`; the
  `system:masters` entry impersonates a member of the group rather than using the envtest admin client)
- [x] Placement annotation (D): D1–D22 implemented as 22 specs
- [x] Subresources (D23–D27, decision 3; October 23, 2026): policy switched to `*/*`; D23–D27 implemented as 5 specs
  in a new `Context` (D26/D27 as a `DescribeTable`), with the `getAndUpdateSubResource` helper
- [x] `go test -race ./pkg/admissionpolicymanager/...` passes (envtest 1.33.0; 51/51 specs, randomized runs); no
  new lint issues. Mutation checks (October 22–23, 2026) — each breaks the policy in one way, and the expected
  spec fails:
  - D: deciding scope from `request.namespace` (D18), checking only the `create` verb (D3), dropping the
    added-or-changed match condition (D12), matching `*` instead of `*/*` (D23, D26, D27, A1; D24/D25 pass either
    way);
  - B: matching reserved prefixes without the trailing hyphen (B3), not matching `replicasets` (B2);
  - C: ignoring whitelisted usernames (C14), matching usernames by prefix (C16), not matching `DELETE` (C4), not
    matching `serviceaccounts/token` (C5).
