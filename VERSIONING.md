# Versioning and upgrades

This document describes how KubeFleet versions its releases, which agent
version combinations are supported, and how to upgrade a fleet safely. It
complements the support-window policy in [SECURITY.md](SECURITY.md) and the
contribution conventions in [CONTRIBUTING.md](CONTRIBUTING.md).

> KubeFleet is pre-1.0. The guarantees below reflect the project's current
> intent and are validated by CI today, but they may tighten as the project
> approaches a 1.0 release.

## Versioning scheme

KubeFleet releases follow [Semantic Versioning](https://semver.org/) using the
form `vMAJOR.MINOR.PATCH` (for example, `v0.4.0`). Release candidates use the
Kubernetes-style pre-release suffix `vMAJOR.MINOR.PATCH-rc.N` (for example,
`v0.4.0-rc.1`). These are the only tag formats accepted by the release tooling;
the validation lives in
[`.github/workflows/setup-release.yml`](.github/workflows/setup-release.yml).

Because KubeFleet is still in the `0.y.z` series, the usual SemVer rule that
"only a major bump may carry breaking changes" does not yet apply. While the
major version is `0`, **a minor bump (`0.Y` → `0.Y+1`) may include breaking
changes**, and patch releases (`0.Y.Z` → `0.Y.Z+1`) are reserved for
backward-compatible bug fixes and security patches. SemVer does not require this
for the `0.y.z` range — it permits anything to change at any time — but KubeFleet
commits to it explicitly so that users on a given minor can take patch and
security updates without fear of a behavior change.

### What warrants a minor versus a patch bump (0.x)

| Change | Bump |
| --- | --- |
| New CRD, or a new field/value on an existing CRD | Minor |
| Breaking change to an existing CRD (removed/renamed field, tightened validation, changed default) | Minor |
| Change to scheduling, override, rollout, or apply semantics that re-ranks or re-applies existing placements | Minor |
| A new agent flag whose default changes observable behavior | Minor |
| Backward-compatible bug fix or security patch with no API or behavior change | Patch |
| Dependency bumps with no user-visible behavior change | Patch |

When in doubt, prefer the higher bump: a minor is cheaper than a surprised user.

## Release cadence and supported versions

KubeFleet targets a roughly three-month minor-release cadence and supports the
two most recent minors (`N` and `N-1`) for security and bug-fix patches.
Cadence slippage is possible while the project is pre-1.0. The authoritative
support-window statement, including the security-patch policy, lives in
[SECURITY.md](SECURITY.md).

## Agent version skew

A KubeFleet deployment runs two agents:

| Agent | Role | Kubernetes analogue |
| --- | --- | --- |
| **hub-agent** | Runs on the hub cluster; owns scheduling, placement, and the source of truth for desired state | Control plane |
| **member-agent** | Runs on each member cluster; applies workloads and reports status and health back to the hub | kubelet |

**Supported skew: the hub-agent and member-agent may run adjacent minor
versions — at most one minor apart, in either direction.** For example, a
`v0.5.z` hub-agent is supported with `v0.4.z` or `v0.6.z` member-agents (and
vice versa), but not with `v0.3.z` ones. KubeFleet does not require the hub to
be upgraded before the members for correctness, only that the two stay within
one minor of each other.

This deliberately differs from the Kubernetes kubelet skew policy that inspired
the control-plane/kubelet analogy above. That policy is *asymmetric* (the kubelet
may trail the API server by up to three minors but must never be newer) because a
node and the control plane are loosely coupled. KubeFleet's hub and member agents
are more tightly coupled, so the project instead guarantees a *symmetric, single*
minor of skew: simpler to reason about, and validated directly in CI rather than
inherited from the Kubernetes rule.

This is exercised by the three jobs in
[`.github/workflows/upgrade.yml`](.github/workflows/upgrade.yml), which run on
pushes to `main` and `release-*` branches and on pull requests against them
(documentation-only pull requests are skipped via the workflow's `paths-ignore`).
The jobs build the previous release and the current commit and together cover
both skew directions:

| Job | Scenario it validates |
| --- | --- |
| `hub-agent-backward-compatibility` | Newer hub-agent against an older member-agent |
| `member-agent-backward-compatibility` | Newer member-agent against an older hub-agent |
| `full-backward-compatibility` | Both agents upgraded together |

The suite exercises the previous-release-to-current-commit skew, which the
release cadence keeps within one minor. Running agents more than one minor apart
is unsupported and untested; upgrade through each minor in turn rather than
skipping one.

### Recommended upgrade ordering

Although both skew directions are supported, the recommended order mirrors the
Kubernetes convention of upgrading the control plane first:

1. Upgrade the **hub-agent** on the hub cluster.
2. Upgrade the **member-agent** on each member cluster.

This keeps the hub — the source of truth for desired state — at the newest
version while members catch up, and it matches the operational model operators
already know from Kubernetes node upgrades. The supported one-minor skew gives
you a window to roll members forward without taking the whole fleet down at
once. The flow is the same one the compatibility suite drives through
[`test/upgrade/upgrade.sh`](test/upgrade/upgrade.sh).

### Cross-agent contracts held stable across a skew window

For the one-minor skew guarantee to hold, the contracts the two agents exchange
must remain backward-compatible across adjacent minors:

- **`Work` / `AppliedWork`** — the hub publishes desired manifests as `Work`
  objects; the member applies them and reports results via `AppliedWork`.
- **Member heartbeat and status** — the member reports health and resource usage
  to the hub by patching the status of `InternalMemberCluster` (in the member's
  namespace on the hub). The hub-side `MemberCluster` controller then mirrors that
  data onto `MemberCluster`; the member agent never writes to `MemberCluster`
  directly. `InternalMemberCluster` is the actual hub↔member status contract.

Changes to these contracts within a minor must be additive; a breaking change to
either is a minor bump (see the table above) and must preserve compatibility
with the immediately preceding minor so that a mid-upgrade fleet keeps working.

## API versioning and lifecycle

KubeFleet's core CRDs are served at more than one API version. For the placement
(`placement.kubernetes-fleet.io`) and cluster (`cluster.kubernetes-fleet.io`)
groups, both `v1` and `v1beta1` are served: `v1` is the externally promoted,
stable surface that `kubectl` returns by default, while `v1beta1` remains the
current storage version. Both refer to the same underlying objects, so a request
for either version returns the same resource.

Per-API maturity — the promotion path through alpha, beta, and stable, and the
deprecation windows for removing an API version — follows the upstream
[Kubernetes API deprecation policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/)
as a model rather than restating it here. Note that the upstream policy formally
governs built-in Kubernetes APIs; KubeFleet adopts it by convention for its CRDs.

When upgrading with raw manifests, apply the CRDs shipped with the target release
before rolling the agents, as you would for any Kubernetes operator. Helm-based
installs need no separate step: KubeFleet ships its CRDs under
`charts/hub-agent/templates/crds/` and `charts/member-agent/templates/crds/`
(regular templates, not Helm's special unmanaged `crds/` directory), so
`helm upgrade` applies them — ahead of the Deployments — automatically.

## See also

- [RELEASING.md](RELEASING.md) — how a release is cut and how to recover a
  failed release run.
- [SECURITY.md](SECURITY.md) — supported versions and security-patch policy.
- [CONTRIBUTING.md](CONTRIBUTING.md) — PR conventions and release-note labels.
- [Kubernetes version skew policy](https://kubernetes.io/releases/version-skew-policy/)
  — the policy whose structure inspired this document; see
  [Agent version skew](#agent-version-skew) for how KubeFleet deliberately differs.
