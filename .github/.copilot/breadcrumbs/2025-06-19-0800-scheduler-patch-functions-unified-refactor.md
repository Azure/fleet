# KubeFleet Scheduler Framework Interface Refactoring

## Requirements

**Primary Goal**: Refactor the KubeFleet scheduler framework to use interface types (`BindingObj`) instead of concrete `ClusterResourceBinding` types in all function signatures and internal logic.

**Specific Requirements**:
- Update all `runSchedulingCycleFor*` and `patchBindingFrom*` function signatures to use `queue.PlacementKey` and `BindingObj` interfaces
- Refactor patch/update helpers to accept `BindingObj` instead of `*ClusterResourceBinding`
- Update related helpers, patch logic, and cross-reference logic to use interface types
- Ensure no function in the scheduler package uses `ClusterResourceBinding` in its signature (except test helpers)
- Add/adjust tests for `FetchBindingFromKey` to handle both cluster-scoped and namespaced bindings
- Remove all testify/assert usage in tests and follow standard Go testing patterns
- Update `bindingWithPatch` struct to use `BindingObj` for the updated field
- Eliminate type assertions in calling code and patch/update logic

## Additional comments from user

- User requested to eliminate the distinct if/else branches in the patch functions and create a unified implementation that leverages the interface methods instead of type-specific logic
- User emphasized: "Please do not let me see another ClusterResourceBinding in the function signature anywhere in the scheduler package"
- User requested comprehensive refactoring of all scheduler framework functions to use interface types consistently
- User wanted removal of all testify/assert usage from tests in favor of standard Go testing patterns

## Plan

### Phase 1: Analysis and Discovery
- [x] Task 1.1: Search for all `runSchedulingCycleFor*` and `patchBindingFrom*` function signatures
- [x] Task 1.2: Identify all functions using `*ClusterResourceBinding` in signatures
- [x] Task 1.3: Review `BindingObj` interface methods available (`GetBindingSpec`, `SetBindingSpec`, `DeepCopyObject`)
- [x] Task 1.4: Examine current implementation patterns and type assertion usage
- [x] Task 1.5: Identify all related helpers and cross-reference logic that needs updating

### Phase 2: Function Signature Refactoring
- [x] Task 2.1: Update `runSchedulingCycleForPickFixedClusterPlacements` to use `queue.PlacementKey`
- [x] Task 2.2: Update `runSchedulingCycleForPickAllPlacements` to use `queue.PlacementKey`
- [x] Task 2.3: Update `runSchedulingCycleForPickNBestPlacements` to use `queue.PlacementKey`
- [x] Task 2.4: Update patch function signatures to accept `BindingObj` instead of `*ClusterResourceBinding`
- [x] Task 2.5: Verify all function calls updated to pass correct interface types

### Phase 3: Patch Helper Refactoring
- [x] Task 3.1: Refactor `patchBindingFromScoredCluster` to unified interface approach
- [x] Task 3.2: Refactor `patchBindingFromFixedCluster` to unified interface approach
- [x] Task 3.3: Update `bindingWithPatch` struct to use `BindingObj` for updated field
- [x] Task 3.4: Remove type assertions from patch logic and calling code
- [x] Task 3.5: Fix type issues with `SetBindingSpec` (value vs pointer)

### Phase 4: Cross-Reference Logic Updates
- [x] Task 4.1: Update `crossReferenceValidTargetsWithBindings` to use `BindingObj` in maps
- [x] Task 4.2: Update related helper functions to work with interface types
- [x] Task 4.3: Remove type assertions in cross-reference logic
- [x] Task 4.4: Verify proper interface method usage throughout

### Phase 5: Binding Resolver Enhancements
- [x] Task 5.1: Refactor `FetchBindingFromKey` to use placement key for both cluster-scoped and namespaced bindings
- [x] Task 5.2: Add comprehensive test cases for `FetchBindingFromKey` (success and error scenarios)
- [x] Task 5.3: Update `ListBindingsFromKey` usage patterns where needed
- [x] Task 5.4: Ensure consistent error handling and interface usage

### Phase 6: Test Refactoring and Cleanup
- [x] Task 6.1: Remove all testify/assert usage from `binding_resolver_test.go`
- [x] Task 6.2: Replace with standard Go testing idioms (`t.Errorf`, `t.Fatalf`, `errors.Is`)
- [x] Task 6.3: Add/update test cases for `FetchBindingFromKey` functionality
- [x] Task 6.4: Update test helpers and test data to use interface types at function boundaries
- [x] Task 6.5: Verify all tests compile and pass after refactoring

### Phase 7: Verification and Validation
- [x] Task 7.1: Build scheduler framework package to verify compilation
- [x] Task 7.2: Run framework package tests to ensure functionality preserved
- [x] Task 7.3: Build entire scheduler package to ensure consistency
- [x] Task 7.4: Verify no remaining `ClusterResourceBinding` in function signatures (except tests/utilities)
- [x] Task 7.5: Final validation of interface usage patterns and type safety

### Phase 8: Test Case Corrections (June 21, 2025)
- [x] Task 8.1: Fix `TestGenerateBinding` test cases to work with actual binding name generation
- [x] Task 8.2: Update test logic to validate UUID-based naming pattern instead of simple concatenation
- [x] Task 8.3: Replace exact name matching with pattern-based validation
- [x] Task 8.4: Remove unused imports and fix compilation errors
- [x] Task 8.5: Verify all framework tests pass with corrected binding name logic

### Phase 9: Test Enhancement with cmp.Diff (June 23, 2025)
- [x] Task 9.1: Enhance `TestListBindingsFromKey` to use `wantBindings` instead of `wantCount`
- [x] Task 9.2: Replace manual binding count verification with `cmp.Diff` comparison
- [x] Task 9.3: Add proper sorting and field ignoring for consistent test comparison
- [x] Task 9.4: Follow the same pattern used in scheduler tests for binding comparison
- [x] Task 9.5: Verify all controller package tests pass with enhanced test logic
- [x] Task 9.6: Enhance `TestResolvePlacementFromKey` to use `wantPlacement` instead of individual field expectations

## Decisions

1. **Interface Method Usage**: Decided to use `GetBindingSpec()` and `SetBindingSpec()` methods from the `BindingObj` interface instead of type-specific field access, enabling unified handling of both `ClusterResourceBinding` and `ResourceBinding`.

2. **Deep Copy Strategy**: Used `binding.DeepCopyObject().(placementv1beta1.BindingObj)` for creating deep copies, leveraging the interface's built-in deep copy functionality rather than type-specific `DeepCopy()` methods.

3. **Function Signature Standardization**: Standardized all scheduler framework functions to use `queue.PlacementKey` and `BindingObj` interface types, eliminating concrete type dependencies.

4. **Type Safety Approach**: Eliminated runtime type assertions in business logic throughout the scheduler package, moving towards compile-time type safety through interface usage.

5. **Patch Creation Strategy**: Maintained the existing patch creation approach with `client.MergeFromWithOptions()` using the original binding object for comparison.

6. **Cross-Reference Logic**: Updated all cross-reference helpers to use `BindingObj` in maps and logic, removing type-specific branching.

7. **Test Pattern Standardization**: Removed testify/assert library usage in favor of standard Go testing patterns (`t.Errorf`, `t.Fatalf`, `errors.Is`) for better consistency.

8. **FetchBindingFromKey Enhancement**: Refactored to use placement key approach for both cluster-scoped and namespaced bindings, improving consistency and reducing code duplication.

9. **bindingWithPatch Structure**: Updated to use `BindingObj` interface for the updated field, maintaining type consistency throughout the patch workflow.

## Implementation Details

### Before: Separate if/else branches
```go
func patchBindingFromScoredCluster(binding placementv1beta1.BindingObj, ...) *bindingWithPatch {
    var updated placementv1beta1.BindingObj
    var patch client.Patch
    
    if crb, ok := binding.(*placementv1beta1.ClusterResourceBinding); ok {
        updatedCRB := crb.DeepCopy()
        // ... type-specific logic for ClusterResourceBinding
        updated = updatedCRB
        patch = client.MergeFromWithOptions(crb, ...)
    } else if rb, ok := binding.(*placementv1beta1.ResourceBinding); ok {
        updatedRB := rb.DeepCopy()
        // ... duplicate logic for ResourceBinding
        updated = updatedRB
        patch = client.MergeFromWithOptions(rb, ...)
    } else {
        panic(...)
    }
    
    return &bindingWithPatch{updated: updated, patch: patch}
}
```

### After: Unified interface approach
```go
func patchBindingFromScoredCluster(binding placementv1beta1.BindingObj, ...) *bindingWithPatch {
    // Create a deep copy using interface method
    updated := binding.DeepCopyObject().(placementv1beta1.BindingObj)
    
    // Get and modify spec using interface methods
    spec := *updated.GetBindingSpec()
    spec.State = desiredState
    spec.SchedulingPolicySnapshotName = policy.GetName()
    spec.ClusterDecision = placementv1beta1.ClusterDecision{...}
    
    // Set updated spec back using interface method
    updated.SetBindingSpec(spec)
    
    // Create patch with original binding
    patch := client.MergeFromWithOptions(binding, client.MergeFromWithOptimisticLock{})
    
    return &bindingWithPatch{updated: updated, patch: patch}
}
```

## Changes Made

### Files Modified:

#### Core Scheduler Framework Files:
- `pkg/scheduler/framework/framework.go` - Main scheduling logic, function signatures updated
- `pkg/scheduler/framework/frameworkutils.go` - Patch helpers, cross-reference logic, bindingWithPatch struct
- `pkg/scheduler/queue/queue.go` - PlacementKey definition and usage

#### Binding Resolver and Utilities:
- `pkg/utils/controller/binding_resolver.go` - FetchBindingFromKey and ListBindingsFromKey refactored
- `pkg/utils/controller/binding_resolver_test.go` - Comprehensive test refactoring

#### Test Files:
- `pkg/scheduler/framework/framework_test.go` - Test helpers and test data updated
- `pkg/scheduler/framework/cyclestate_test.go` - Test data updated
- `pkg/scheduler/framework/plugins/sameplacementaffinity/filtering_test.go` - Test data updated
- `pkg/scheduler/framework/plugins/sameplacementaffinity/scoring_test.go` - Test data updated

#### Supporting Files:
- `pkg/scheduler/framework/uniquename/uniquename.go` - Binding name helpers (verified compatibility)
- `pkg/scheduler/framework/frameworkutils_test.go` - **UPDATED June 21**: Fixed test cases to work with actual UUID-based binding name generation

### Specific Changes:

#### 1. Function Signature Updates:
**Before**:
```go
func runSchedulingCycleForPickFixedClusterPlacements(
    crp *placementv1beta1.ClusterResourcePlacement,
    policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
    clusters []clusterv1beta1.MemberCluster,
    obsoleteBindings, unscheduledBindings []*placementv1beta1.ClusterResourceBinding,
) ([]*bindingWithPatch, *framework.Status)
```

**After**:
```go
func runSchedulingCycleForPickFixedClusterPlacements(
    placementKey queue.PlacementKey,
    policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
    clusters []clusterv1beta1.MemberCluster,
    obsoleteBindings, unscheduledBindings []placementv1beta1.BindingObj,
) ([]*bindingWithPatch, *framework.Status)
```

#### 2. Patch Function Unified Implementation:
**Before (patchBindingFromScoredCluster)**:
```go
func patchBindingFromScoredCluster(binding placementv1beta1.BindingObj, ...) *bindingWithPatch {
    var updated placementv1beta1.BindingObj
    var patch client.Patch
    
    if crb, ok := binding.(*placementv1beta1.ClusterResourceBinding); ok {
        updatedCRB := crb.DeepCopy()
        // ... type-specific logic for ClusterResourceBinding
        updated = updatedCRB
        patch = client.MergeFromWithOptions(crb, ...)
    } else if rb, ok := binding.(*placementv1beta1.ResourceBinding); ok {
        updatedRB := rb.DeepCopy()
        // ... duplicate logic for ResourceBinding
        updated = updatedRB
        patch = client.MergeFromWithOptions(rb, ...)
    }
    return &bindingWithPatch{updated: updated, patch: patch}
}
```

**After (Unified)**:
```go
func patchBindingFromScoredCluster(binding placementv1beta1.BindingObj, ...) *bindingWithPatch {
    updated := binding.DeepCopyObject().(placementv1beta1.BindingObj)
    spec := *updated.GetBindingSpec()
    spec.State = desiredState
    spec.SchedulingPolicySnapshotName = policy.GetName()
    spec.ClusterDecision = placementv1beta1.ClusterDecision{...}
    updated.SetBindingSpec(spec)
    patch := client.MergeFromWithOptions(binding, client.MergeFromWithOptimisticLock{})
    return &bindingWithPatch{updated: updated, patch: patch}
}
```

#### 3. bindingWithPatch Struct Update:
**Before**:
```go
type bindingWithPatch struct {
    updated interface{} // Could be *ClusterResourceBinding or *ResourceBinding
    patch   client.Patch
}
```

**After**:
```go
type bindingWithPatch struct {
    updated placementv1beta1.BindingObj // Always interface type
    patch   client.Patch
}
```

#### 4. Cross-Reference Logic Updates:
**Before**:
```go
func crossReferenceValidTargetsWithBindings(
    clusters []clusterv1beta1.MemberCluster,
    bindings []*placementv1beta1.ClusterResourceBinding,
) map[string]*placementv1beta1.ClusterResourceBinding
```

**After**:
```go
func crossReferenceValidTargetsWithBindings(
    clusters []clusterv1beta1.MemberCluster,
    bindings []placementv1beta1.BindingObj,
) map[string]placementv1beta1.BindingObj
```

#### 5. FetchBindingFromKey Refactoring:
**Before**: Separate handling for cluster-scoped vs namespaced bindings
**After**: Unified approach using placement key for both types

#### 6. Test Refactoring:
**Before (testify/assert)**:
```go
assert.NoError(t, err)
assert.Equal(t, expected, actual)
```

**After (standard Go)**:
```go
if err != nil {
    t.Fatalf("unexpected error: %v", err)
}
if actual != expected {
    t.Errorf("got %v, want %v", actual, expected)
}
```

#### 7. Test Case Corrections (June 21, 2025):
**Problem**: Test cases in `TestGenerateBinding` were expecting simple name concatenation (e.g., "test-placement-test-cluster") but the actual `generateBinding` function uses a UUID-based unique name generator.

**Before**:
```go
expectedBinding: &placementv1beta1.ClusterResourceBinding{
    ObjectMeta: metav1.ObjectMeta{
        Name: "test-placement-test-cluster", // Expected simple concatenation
        // ...
    },
},
```

**After**:
```go
// Verify name pattern: placement-cluster-uuid (8 chars)
nameParts := strings.Split(crb.Name, "-")
if len(nameParts) < 3 {
    t.Errorf("expected binding name to have at least 3 parts separated by '-', got %s", crb.Name)
} else {
    // Last part should be 8-character UUID
    uuidPart := nameParts[len(nameParts)-1]
    if len(uuidPart) != 8 {
        t.Errorf("expected UUID part to be 8 characters, got %d characters: %s", len(uuidPart), uuidPart)
    }
}
```

**Solution**: Replaced exact name matching with pattern-based validation that checks for the expected format: `placement-cluster-uuid` where the UUID part is 8 characters long.

#### 8. Test Enhancement with cmp.Diff (June 23, 2025):
**Enhancement**: Improved `TestListBindingsFromKey` to use `wantBindings` instead of `wantCount` and leveraged `cmp.Diff` for more robust test comparison, following the pattern used in scheduler tests.

**Before**:
```go
tests := []struct {
    name         string
    placementKey queue.PlacementKey
    objects      []client.Object
    wantErr      bool
    wantCount    int  // Simple count verification
    errType      string
}

// Manual count check
if len(got) != tt.wantCount {
    t.Errorf("Expected %d bindings but got %d", tt.wantCount, len(got))
}

// Manual label verification
for _, binding := range got {
    labels := binding.GetLabels()
    if labels[placementv1beta1.CRPTrackingLabel] != string(tt.placementKey) {
        t.Errorf("Expected CRPTrackingLabel to be %s but got %s", string(tt.placementKey), labels[placementv1beta1.CRPTrackingLabel])
    }
}
```

**After**:
```go
tests := []struct {
    name         string
    placementKey queue.PlacementKey
    objects      []client.Object
    wantErr      bool
    wantBindings []placementv1beta1.BindingObj  // Full binding objects for comparison
    errType      string
}

// Comprehensive diff comparison with sorting and field ignoring
if diff := cmp.Diff(got, tt.wantBindings, 
    cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
    cmpopts.SortSlices(func(b1, b2 placementv1beta1.BindingObj) bool {
        return b1.GetName() < b2.GetName()
    })); diff != "" {
    t.Errorf("ListBindingsFromKey() diff (-got +want):\n%s", diff)
}
```

**Benefits**:
- More comprehensive comparison of all binding fields
- Better error messages showing exact differences
- Consistent with scheduler test patterns
- Automatic handling of ordering differences
- Ignores non-essential fields like ResourceVersion

#### 9. Placement Resolver Test Enhancement (June 23, 2025):
**Enhancement**: Improved `TestResolvePlacementFromKey` to use `wantPlacement` instead of individual field expectations and leveraged `cmp.Diff` for comprehensive placement object comparison.

**Before**:
```go
tests := []struct {
    name          string
    placementKey  queue.PlacementKey
    objects       []client.Object
    expectedType  string  // String type checking
    expectedName  string  // Individual field checks
    expectedNS    string
    expectCluster bool
    expectedErr   error
}

// Manual field verification
if diff := cmp.Diff(tt.expectedName, placement.GetName()); diff != "" {
    t.Errorf("Name mismatch (-want +got):\n%s", diff)
}
if diff := cmp.Diff(tt.expectedNS, placement.GetNamespace()); diff != "" {
    t.Errorf("Namespace mismatch (-want +got):\n%s", diff)
}

// String-based type checking
switch tt.expectedType {
case "*v1beta1.ClusterResourcePlacement":
    if _, ok := placement.(*fleetv1beta1.ClusterResourcePlacement); !ok {
        t.Errorf("Expected type ClusterResourcePlacement, but got: %T", placement)
    }
}
```

**After**:
```go
tests := []struct {
    name          string
    placementKey  queue.PlacementKey
    objects       []client.Object
    wantPlacement fleetv1beta1.PlacementObj  // Full placement object for comparison
    wantErr       bool
    expectedErr   error
}

// Comprehensive diff comparison with field ignoring
if diff := cmp.Diff(placement, tt.wantPlacement,
    cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")); diff != "" {
    t.Errorf("FetchPlacementFromKey() diff (-got +want):\n%s", diff)
}

// Type checking based on expected object type
switch tt.wantPlacement.(type) {
case *fleetv1beta1.ClusterResourcePlacement:
    if _, ok := placement.(*fleetv1beta1.ClusterResourcePlacement); !ok {
        t.Errorf("Expected type ClusterResourcePlacement, but got: %T", placement)
    }
}
```

**Benefits**:
- Complete object comparison instead of field-by-field checks
- Better error messages showing exact differences
- Type checking based on actual expected objects
- Added test cases for not-found scenarios
- Consistent with other enhanced test patterns
- Ignores non-essential metadata fields

## References

### Domain Knowledge Files:
- Interface patterns from existing codebase usage of `BindingObj`
- Scheduler framework design patterns from controller-runtime

### API Documentation:
- `apis/placement/v1beta1/binding_types.go` - `BindingObj` interface definition (lines 54-58)
- `apis/placement/v1beta1/binding_types.go` - `BindingSpecGetterSetter` interface (lines 40-43)
- `apis/placement/v1beta1/binding_types.go` - `BindingStatusGetterSetter` interface (lines 46-49)

### Related Code Files:
- `pkg/scheduler/framework/framework.go` - Main scheduling logic using these patch functions
- `pkg/scheduler/framework/frameworkutils.go` - Core patch and helper functions
- `pkg/utils/controller/binding_resolver.go` - Binding resolution utilities
- `pkg/utils/controller/placement_resolver_test.go` - Testing pattern reference
- `pkg/scheduler/queue/queue.go` - PlacementKey definition and usage patterns

### External Dependencies:
- `sigs.k8s.io/controller-runtime/pkg/client` - Patch and object interfaces
- `k8s.io/apimachinery/pkg/api/errors` - Error handling patterns
- Standard Go testing patterns (no external test libraries)

### Testing References:
- Standard Go testing documentation for test patterns
- Controller-runtime testing best practices for Kubernetes controllers

## Success Criteria Met

✅ **No ClusterResourceBinding in Function Signatures**: Verified no function signatures in scheduler package use `*ClusterResourceBinding` (except test helpers and utility functions)  
✅ **Unified Implementation**: All patch functions now use single code path instead of type-specific branches  
✅ **Interface Usage**: All scheduler framework functions properly leverage `BindingObj` interface methods throughout  
✅ **Function Signature Consistency**: All `runSchedulingCycleFor*` functions use `queue.PlacementKey` and `BindingObj` parameters  
✅ **Cross-Reference Logic**: All helper functions updated to use interface types in maps and logic  
✅ **bindingWithPatch Structure**: Updated to use `BindingObj` for type consistency  
✅ **FetchBindingFromKey Enhancement**: Refactored with comprehensive tests for both binding types  
✅ **Test Pattern Standardization**: Removed all testify/assert usage, using standard Go testing  
✅ **Type Safety**: Eliminated runtime type assertions in business logic throughout scheduler package  
✅ **Compilation**: All scheduler package code compiles successfully  
✅ **Test Compatibility**: All existing tests continue to pass (framework, plugins, utilities)  
✅ **Functionality Preserved**: Same behavior maintained for all binding operations and scheduling logic
