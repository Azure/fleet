# Contributing

KubeFleet welcomes contributions and suggestions!

## Terms

All contributions to the repository must be submitted under the terms of the [Apache Public License 2.0](https://www.apache.org/licenses/LICENSE-2.0).

## Certificate of Origin

By contributing to this project, you agree to the Developer Certificate of Origin (DCO). This document was created by the Linux Kernel community and is a simple statement that you, as a contributor, have the legal right to make the contribution. See the [DCO](DCO) file for details.

## DCO Sign Off

You must sign off your commit to state that you certify the [DCO](DCO). To certify your commit for DCO, add a line like the following at the end of your commit message:

```
Signed-off-by: John Smith <john@example.com>
```

This can be done with the `--signoff` option to `git commit`. See the [Git documentation](https://git-scm.com/docs/git-commit#Documentation/git-commit.txt--s) for details.

## Code of Conduct

The KubeFleet project has adopted the CNCF Code of Conduct. Refer to our [Community Code of Conduct](CODE_OF_CONDUCT.md) for details.

## Contributing a patch

1. Submit an issue describing your proposed change to the repository in question. The repository owners will respond to your issue promptly.
2. Fork the desired repository, then develop and test your code changes.
3. Submit a pull request.

## Issue and pull request management

Anyone can comment on issues and submit reviews for pull requests. In order to be assigned an issue or pull request, you can leave a `/assign <your Github ID>` comment on the issue or pull request.

## Pull request titles

PR titles must begin with one of the following prefixes (enforced by [`pr-title-lint.yml`](.github/workflows/pr-title-lint.yml)):

`feat:`, `fix:`, `docs:`, `test:`, `style:`, `interface:`, `util:`, `chore:`, `ci:`, `perf:`, `refactor:`, `revert:`

Add `make reviewable` to your workflow before opening a PR — the PR template will remind you, but running it locally first saves a round trip.

## Release note labels

Each PR should carry one base `release-note/*` label matching its title prefix; additive labels (below) may be stacked on top.

| PR title prefix | Label |
| --- | --- |
| `feat:` / `perf:` | `release-note/feature` |
| `fix:` / `revert:` | `release-note/fix` |
| `docs:` | `release-note/docs` |
| `test:` | `release-note/test` |
| `chore:` / `ci:` / `style:` / `interface:` / `util:` | `release-note/chore` |
| `refactor:` | `release-note/refactor` |

Additive labels (stack on top of the above when applicable):

- `release-note/breaking` — a change that requires user action to upgrade (manifest edit, CRD reapply, RBAC reapply, webhook config update, member-cluster re-join, etc.) **or** that alters scheduling, override, or apply semantics in a way that re-ranks or re-applies existing placements without a manifest change. Pre-1.0, internal refactors of any alpha or beta API shape that don't require migration steps or change observable semantics do not qualify.
- `release-note/security` — security fixes or vulnerability disclosures
- `release-note/none` — suppresses the entry in release notes but keeps the PR visible in GitHub's auto-notes drafter UI
- `ignore-for-release` — hides the PR entirely from auto-generated notes. Default to this for CI-only or internal-cleanup PRs with no user impact.

PRs with no `release-note/*` label fall into "Other Changes" in the generated notes. Dependabot PRs are labeled `dependencies` automatically and land under "Maintenance and Dependencies" without a `release-note/*` label.
