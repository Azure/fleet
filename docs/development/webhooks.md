# Fleet Webhooks

This document provides an overview of the webhooks used in the Fleet project, including implementation details and usage of Common Expression Language (CEL) for validation.

## Overview

Fleet uses validating webhooks to enforce security and policy controls on resource operations. The webhooks restrict actions on fleet resources based on user permissions and resource types.

## Webhook Handlers

The main webhook handlers in Fleet include:

- `fleetResourceValidator` - Validates operations on Fleet resources
- `clusterResourcePlacementValidator` - Validates cluster resource placement configurations
- `podValidator` - Validates pod operations

## Common Expression Language (CEL) Validation

Fleet uses Common Expression Language (CEL) for certain validation rules. CEL provides a concise and powerful way to express validation logic declaratively, improving both readability and performance.

### CEL Implementation in Fleet

The implementation is found in `pkg/webhook/validation/cel_validation.go` and provides:

1. A `CELEnvironment` struct that holds compiled CEL expressions
2. Functions that use CEL to validate permissions and resource operations
3. Graceful fallback to standard validation if CEL evaluation fails

### CEL Validation Examples

Fleet currently uses CEL to validate:

1. CRD group membership: `group in fleetCRDGroups`
2. User permissions: `username in whiteListedUsers || userGroups.exists(g, g in adminGroups)`

### Benefits of CEL

Using CEL for validation provides several advantages:

- More declarative and concise validation rules
- Potential for better performance through pre-compilation
- Alignment with Kubernetes-native validation approaches
- Easier to maintain and update validation logic

### Usage

The webhook handler initializes a CEL environment during startup and uses it for validation during request processing. If CEL validation encounters an error, it falls back to the standard validation implementation.