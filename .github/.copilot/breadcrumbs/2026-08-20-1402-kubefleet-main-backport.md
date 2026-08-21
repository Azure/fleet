# Backport: kubefleet Main into Fleet

## Overview

Merge the latest `kubefleet-dev/kubefleet` main branch into `Azure/fleet`, preserving incoming CNCF changes while adapting module references for the Fleet repository.

## Plan

1. Synchronize the branch with `Azure/fleet` main and fetch `kubefleet-dev/kubefleet` main.
2. Merge `cncf/main`, preferring incoming changes for conflicts.
3. Rewrite CNCF module imports to use `go.goms.io/fleet`.
4. Remove new CRD symbolic links from the hub-agent and member-agent charts.
5. Run `make reviewable`, resolve failures caused by the backport, and commit the merge.
6. Push the branch and open a pull request against `Azure/fleet`.

## Success Criteria

- [x] The latest CNCF main commits are present in the merge.
- [x] No CNCF module references remain.
- [x] No new chart CRD symbolic links remain.
- [x] `make reviewable` passes.
- [ ] The merge commit is pushed and a PR is open against `Azure/fleet`.

## Implementation Notes

- Retained the incoming version of `.squad/templates/skills/humanizer/SKILL.md` to resolve the merge's modify/delete conflict.
- Repointed incoming support links to `Azure/fleet`.
- Removed a duplicate generated import introduced by the merge.
- Ran `make reviewable` under WSL with `GOTOOLCHAIN=go1.26.6`; all checks passed.
