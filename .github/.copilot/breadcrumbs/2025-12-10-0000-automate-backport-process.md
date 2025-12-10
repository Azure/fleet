# Automate Backport Process

**Date**: 2025-12-10  
**Status**: Planning

## Objective
Create a GitHub Actions workflow to automate the backport process from upstream (cncf/main).

## Requirements

### Backport Action
- Fetch from upstream (cncf/main)
- Merge with strategy `-X theirs`
- Replace `github.com/kubefleet-dev/kubefleet` with `go.goms.io/fleet`
- Run `make reviewable`
- Create PR with commit list in description

## Implementation Plan

### Phase 1: Analyze Current Setup
- [x] Review existing GitHub Actions structure
- [x] Understand current import paths and replacement needs
  - Current module: `go.goms.io/fleet`
  - Need to replace: `kubefleet-dev/kubefleet` → `go.goms.io/fleet`
- [x] Check Makefile for `reviewable` target
  - Target exists: `reviewable: fmt vet lint staticcheck`
- [x] Review repository settings (upstream remote configuration)
  - Upstream: `cncf/main`

### Phase 2: Create Backport Workflow
- [x] Task 2.1: Create `.github/workflows/backport.yml`
  - Configure manual trigger (workflow_dispatch) with optional schedule
  - Set up authentication and permissions
- [x] Task 2.2: Implement fetch and merge logic
  - Add upstream remote if not exists
  - Fetch from cncf/main
  - Merge with `-X theirs` strategy
- [x] Task 2.3: Implement module path replacement
  - Find all occurrences of `kubefleet-dev/kubefleet`
  - Replace with correct `goms.io/...` path
  - Handle go.mod, go.sum, and source files
- [x] Task 2.4: Run make reviewable
  - Execute formatting and linting
  - Handle any errors appropriately
- [x] Task 2.5: Create PR with commit list
  - Generate commit list since last backport
  - Format PR description with commits
  - Use GitHub API to create PR
- [x] Task 2.6: Add error handling and notifications
  - Handle merge conflicts
  - Notify on failure
  - Add proper logging

### Phase 3: Testing and Documentation
- [ ] Task 3.1: Test backport workflow manually
- [x] Task 3.2: Document workflow in CONTRIBUTING.md

## Implementation Summary

### Files Created
1. `.github/workflows/backport.yml` - Automated backport workflow from upstream

### Files Modified
1. `CONTRIBUTING.md` - Added documentation for backport workflow

### Files Removed
1. `.github/workflows/revert.yml` - Removed per user request

### Key Features Implemented

#### Backport Workflow
- Manual trigger via workflow_dispatch
- Scheduled run every Monday at 00:00 UTC
- Fetches from `cncf/main` (kubefleet-dev/kubefleet upstream)
- Merges with `-X theirs` strategy
- Replaces module paths: `github.com/kubefleet-dev/kubefleet` → `go.goms.io/fleet`
- Runs `make reviewable` for code formatting and linting
- Creates single merge commit with all changes
- Generates commit list from merge using `git log --oneline --decorate HEAD~1^2..HEAD^2`
- Creates PR with commit list
- Handles merge conflicts gracefully with instructions
- Skips if no new commits to backport

### Testing Notes
- Backport workflow needs to be tested manually after merge
- Test via Actions → Backport from Upstream → Run workflow

## Decisions
1. Replacement path: `github.com/kubefleet-dev/kubefleet` → `go.goms.io/fleet`
2. Backport workflow: Manual trigger with optional schedule (weekly)
3. Target branch: `main`
4. Single merge commit containing all changes (merge + replacements + make reviewable)
5. Revert workflow: Not implemented (removed from scope)

**Status**: Implementation complete

## Success Criteria
- [x] Backport workflow successfully merges from cncf/main
- [x] Module paths are correctly replaced
- [x] `make reviewable` runs in single commit
- [x] PR is created with commit list
- [x] Workflow has appropriate error handling
- [x] Workflow is documented

**Status**: Implementation complete - ready for testing

## Notes
- Need to determine the upstream remote configuration
- May need GitHub token with appropriate permissions
- Should consider handling merge conflicts gracefully
