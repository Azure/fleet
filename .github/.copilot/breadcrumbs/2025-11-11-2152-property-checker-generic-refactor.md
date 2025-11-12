# Property Checker Generic Refactor

## Understanding the Problem

Based on my analysis of the codebase, I found that:

1. **Current State**: The property checker is currently Azure-specific and tightly coupled to Azure VM size validation
2. **Property Provider Pattern**: The property provider follows a clean generic interface pattern with Azure-specific implementation
3. **Usage Pattern**: The property checker seems to be used specifically for SKU capacity validation during scheduling

## Current Structure Analysis

### Property Provider Structure (Reference)
```
pkg/propertyprovider/
├── interface.go           # Generic PropertyProvider interface  
├── commons.go            # Common utilities
└── azure/
    └── provider.go       # Azure-specific implementation
```

### Current Property Checker Structure  
```
pkg/propertychecker/
└── azure/
    ├── checker.go        # Azure-specific property checker
    └── checker_test.go   # Tests
```

## Key Findings

1. **PropertyChecker Usage**: The `CheckIfMeetSKUCapacityRequirement` method is Azure-specific and validates VM SKU capacity using Azure's AttributeBasedVMSizeRecommenderClient
2. **No Generic Interface**: Unlike PropertyProvider, there's no generic PropertyChecker interface
3. **Azure Dependencies**: Heavy coupling to Azure-specific types and Azure compute client
4. **Limited Usage**: From my search, the property checker doesn't seem to be actively used in the scheduler framework plugins yet

## Proposed Solution

Create a generic PropertyChecker interface similar to PropertyProvider, with Azure-specific implementation.

### Target Structure
```
pkg/propertychecker/
├── interface.go           # Generic PropertyChecker interface
├── commons.go            # Common utilities (if needed)
└── azure/
    ├── checker.go        # Azure-specific implementation
    └── checker_test.go   # Tests
```

## Implementation Plan

### Phase 1: Create Generic Interface
- [ ] Design PropertyChecker interface with generic validation methods
- [ ] Create common types and utilities

### Phase 2: Refactor Azure Implementation  
- [ ] Move current Azure PropertyChecker to implement generic interface
- [ ] Maintain all existing Azure-specific functionality
- [ ] Update method signatures to align with generic interface

### Phase 3: Update Integration Points
- [ ] Update any imports/usage to use generic interface
- [ ] Update tests to work with new structure

### Phase 4: Documentation and Cleanup
- [ ] Update package documentation
- [ ] Add examples for implementing other cloud providers
- [ ] Ensure consistent patterns with PropertyProvider

## Success Criteria
- [ ] Generic PropertyChecker interface exists and follows same patterns as PropertyProvider
- [ ] Azure implementation works exactly as before but implements generic interface
- [ ] All existing tests pass
- [ ] Future cloud providers can easily implement the interface
- [ ] No breaking changes to existing functionality