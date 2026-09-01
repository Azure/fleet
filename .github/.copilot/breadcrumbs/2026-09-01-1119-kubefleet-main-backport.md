# Implementation: September 2026 KubeFleet Main Backport

## Overview

Merge the latest `kubefleet-dev/kubefleet` main branch into `Azure/fleet` main while preserving Fleet-specific module paths, build settings, chart layout, and link-check configuration.

## Plan

1. Confirm `cncf` and `upstream` remotes, fetch both main branches, and fast-forward the session branch to `upstream/main`.
2. Merge `cncf/main` with a merge commit, preferring incoming conflict resolutions and retaining incoming modify/delete files.
3. Restore Fleet conventions for Go module/import paths, repository links, Docker builder versions, CRD chart symlinks, and authenticated Slack link checking.
4. Run `make reviewable` in WSL and fix backport-related failures.
5. Commit any post-merge fixes, push the branch, and open a new pull request against `Azure/fleet:main`.

## Success Criteria

- [x] The branch begins at the latest `upstream/main`.
- [x] `cncf/main` is merged with a merge commit.
- [x] Go module and import references use `go.goms.io/fleet`.
- [x] Dockerfiles use the latest incoming Microsoft Go builder consistently.
- [x] No new CRD template symlinks remain.
- [x] `make reviewable` passes in WSL.
- [ ] A new pull request targets `Azure/fleet:main` from the pushed fork branch.

## Implementation Notes

- Base: `upstream/main` at `f21dbd0cfd3cb10923472fa8557e4c9717ba3597`.
- Incoming: `cncf/main` at `48bde0d8`.
- Merge: `533a4a8a`.
- Restored incoming Go imports in `test/e2e/join_and_leave_test.go` to `go.goms.io/fleet`.
- Confirmed all four Dockerfiles use `mcr.microsoft.com/oss/go/microsoft/golang:1.26.6-1`.
- Confirmed the Slack archives URL remains covered by `ignorePatterns`.
- Confirmed no new CRD template symlinks were introduced relative to the first merge parent.
- Ran `make reviewable` successfully in WSL with `GOTOOLCHAIN=go1.26.6`; shell scripts were normalized only in the working tree for validation and restored to the checkout line-ending convention afterward.
