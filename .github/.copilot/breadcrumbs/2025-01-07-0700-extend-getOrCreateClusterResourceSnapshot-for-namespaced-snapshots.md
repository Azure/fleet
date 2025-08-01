# Extend getOrCreateClusterResourceSnapshot to Handle Both Snapshot Types

## Context

Following successful interface refactoring and deletion function optimizations, user requested: "can you modify the function getOrCreateClusterResourceSnapshot to handle both clusterresourcesnapshot and resourcesnapshot?"

## Problem

The `getOrCreateClusterResourceSnapshot` function in `/Users/ryanzhang/Workspace/github/kubefleet/pkg/controllers/clusterresourceplacement/controller.go` (lines 468-650) currently only handles `ClusterResourceSnapshot` (cluster-scoped) resources. We need to extend it to also handle `ResourceSnapshot` (namespace-scoped) resources using the common `ResourceSnapshotObj` interface.

## Research Findings

### ResourceSnapshotObj Interface Structure
```go
type ResourceSnapshotObj interface {
    apis.ConditionedObj
    ResourceSnapshotSpecGetterSetter
    ResourceSnapshotStatusGetterSetter
}

type ResourceSnapshotSpecGetterSetter interface {
    GetResourceSnapshotSpec() *ResourceSnapshotSpec
    SetResourceSnapshotSpec(ResourceSnapshotSpec)
}

type ResourceSnapshotStatusGetterSetter interface {
    GetResourceSnapshotStatus() *ResourceSnapshotStatus
    SetResourceSnapshotStatus(ResourceSnapshotStatus)
}
```

### Implementation Types
- **ClusterResourceSnapshot** (cluster-scoped, v1beta1) - implements ResourceSnapshotObj
- **ResourceSnapshot** (namespace-scoped, v1beta1) - implements ResourceSnapshotObj

Both types share:
- Same `ResourceSnapshotSpec` structure
- Same interface methods for spec/status access
- Same labeling and annotation patterns
- Different scoping (cluster vs namespace)

### Pattern Precedent
The delete functions already follow this pattern:
- `deleteClusterSchedulingPolicySnapshots` handles both ClusterSchedulingPolicySnapshot and SchedulingPolicySnapshot
- `deleteResourceSnapshots` uses `DeleteAllOf` with namespace awareness

## Plan

### Phase 1: Function Signature Updates
- [x] Task 1.1: Change function signature to accept `PlacementObj` interface instead of concrete type
- [x] Task 1.2: Update return type to use `ResourceSnapshotObj` interface
- [x] Task 1.3: Update function name to `getOrCreateResourceSnapshot` for clarity

### Phase 2: Namespace Detection Logic
- [x] Task 2.1: Add helper function to determine if placement is namespace-scoped
- [x] Task 2.2: Implement logic to choose between ClusterResourceSnapshot and ResourceSnapshot based on placement scope

### Phase 3: Snapshot Creation Logic
- [ ] Task 3.1: Extend `buildMasterClusterResourceSnapshot` to handle both types
- [ ] Task 3.2: Extend `buildSubIndexResourceSnapshot` to handle both types
- [ ] Task 3.3: Update snapshot creation logic to use proper type based on scope

### Phase 4: Helper Functions Updates
- [ ] Task 4.1: Update `lookupLatestResourceSnapshot` to handle both types
- [ ] Task 4.2: Update `listSortedResourceSnapshots` to handle both types
- [ ] Task 4.3: Update `createResourceSnapshot` to handle both types

### Phase 5: Testing and Validation
- [ ] Task 5.1: Run existing unit tests to ensure no regression
- [ ] Task 5.2: Verify function works with both cluster-scoped and namespace-scoped placements
- [ ] Task 5.3: Ensure interface compatibility throughout the codebase

## Success Criteria
- Function accepts both ClusterResourcePlacement and ResourcePlacement
- Creates appropriate snapshot type based on placement scope
- Maintains all existing functionality for cluster-scoped resources
- Uses ResourceSnapshotObj interface throughout
- No breaking changes to existing API
- All tests pass

## Implementation Notes
- Follow established namespace-aware pattern from delete functions
- Use ResourceSnapshotObj interface consistently
- Maintain backward compatibility
- Apply same labeling/annotation patterns for both types
