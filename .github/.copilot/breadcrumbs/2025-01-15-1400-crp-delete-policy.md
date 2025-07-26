# CRP Delete Policy Implementation

## Problem Analysis

The issue requests adding API options to allow customers to choose whether to delete placed resources when a CRP (ClusterResourcePlacement) is deleted.

### Current Deletion Behavior
1. When a CRP is deleted, it has two finalizers:
   - `ClusterResourcePlacementCleanupFinalizer` - handled by CRP controller to delete snapshots
   - `SchedulerCleanupFinalizer` - handled by scheduler to delete bindings
   
2. The deletion flow:
   - CRP controller removes snapshots (ClusterSchedulingPolicySnapshot, ClusterResourceSnapshot)
   - Scheduler removes bindings (ClusterResourceBinding) which triggers cleanup of placed resources
   - Currently there's no option for users to control whether placed resources are deleted

### References
- Kubernetes DeleteOptions has `PropagationPolicy` with values: `Orphan`, `Background`, `Foreground`
- AKS API has `DeletePolicy` pattern (couldn't fetch exact details but following similar pattern)

## Implementation Plan

### Phase 1: API Design
- [x] Add `DeleteStrategy` struct to `RolloutStrategy` in beta API
- [x] Define `PropagationPolicy` field with enum values: `Delete` (default), `Abandon`
- [x] Update API documentation and validation
- [x] Move DeleteStrategy inside RolloutStrategy after ApplyStrategy for consistency

### Phase 2: Implementation Details
TODO: @Arvindthiru to fill out the details for controller logic implementation.

### Phase 3: Testing
- [ ] Add unit tests for new deletion policy options
- [ ] Add integration tests to verify behavior  
- [ ] Test both `Delete` and `Abandon` scenarios

### Phase 4: Documentation & Examples
- [ ] Update CRD documentation
- [ ] Add example configurations
- [ ] Update any user-facing documentation

## Success Criteria
- [x] CRP API has `deleteStrategy` field with `Delete`/`Abandon` options inside RolloutStrategy
- [x] Default behavior (`Delete`) preserves current functionality
- [ ] `Abandon` policy leaves placed resources intact when CRP is deleted
- [ ] All tests pass including new deletion policy tests
- [x] Changes are minimal and backwards compatible

## Current API Structure

The DeleteStrategy is now part of RolloutStrategy:

```go
type RolloutStrategy struct {
    // ... other fields ...
    ApplyStrategy *ApplyStrategy `json:"applyStrategy,omitempty"`
    DeleteStrategy *DeleteStrategy `json:"deleteStrategy,omitempty"`
}

type DeleteStrategy struct {
    PropagationPolicy DeletePropagationPolicy `json:"propagationPolicy,omitempty"`
}

type DeletePropagationPolicy string

const (
    DeletePropagationPolicyDelete DeletePropagationPolicy = "Delete"    // default
    DeletePropagationPolicyAbandon DeletePropagationPolicy = "Abandon"
)
```

## Behavior Summary

- **Default (`Delete`)**: When CRP is deleted, all placed resources are removed from member clusters (current behavior)
- **Abandon**: When CRP is deleted, placed resources remain on member clusters but are no longer managed by Fleet

This provides customers with the flexibility to choose between complete cleanup or leaving resources in place when deleting a CRP.