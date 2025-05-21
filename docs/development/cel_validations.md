# CEL Validations in Fleet APIs

This document outlines the Common Expression Language (CEL) validations that have been added to the Fleet APIs to replace or supplement traditional webhook validations.

## Overview

Common Expression Language (CEL) annotations provide a declarative way to express validation logic directly in the API type definitions. This approach offers several benefits:

- More concise and readable validation logic
- Validations are defined closer to the data structures they validate
- Reduced need for imperative validation code in webhooks
- Better compatibility with Kubernetes admission mechanisms

## MemberCluster CEL Validations

The following CEL validations have been added to the `MemberCluster` API:

1. **Name Length Validation**:
   ```
   +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 64",message="metadata.name max length is 63"
   ```

2. **Unique Taints Validation**:
   ```
   +kubebuilder:validation:XValidation:rule="!self.spec.taints.exists(t1, self.spec.taints.exists(t2, t1 != t2 && t1.key == t2.key && t1.effect == t2.effect))",message="taints must be unique"
   ```

3. **Taint Key and Value Validation** (on the `Taint` struct):
   ```
   +kubebuilder:validation:XValidation:rule="self.key.matches('^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*)?$')",message="taint key must be a valid label name"
   +kubebuilder:validation:XValidation:rule="self.value == '' || self.value.matches('^([a-z0-9]([-a-z0-9_.]*[a-z0-9])?)?$')",message="taint value must be a valid label value"
   ```

## ClusterResourcePlacement CEL Validations

The following CEL validations have been added to the `ClusterResourcePlacement` API:

1. **Name Length Validation**:
   ```
   +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 63",message="metadata.name max length is 63"
   ```

2. **Immutable Placement Type**:
   ```
   +kubebuilder:validation:XValidation:rule="oldSelf == null || oldSelf.spec.policy == null || self.spec.policy == null || oldSelf.spec.policy.placementType == self.spec.policy.placementType",message="placement type is immutable"
   ```

3. **Mutual Exclusivity for Resource Selector Fields**:
   ```
   +kubebuilder:validation:XValidation:rule="!self.spec.resourceSelectors.exists(s, s.labelSelector != null && s.name != '')",message="the labelSelector and name fields are mutually exclusive in resource selectors"
   ```

4. **Preventing Updates or Deletions of Existing Tolerations**:
   ```
   +kubebuilder:validation:XValidation:rule="oldSelf == null || oldSelf.spec.policy == null || self.spec.policy == null || oldSelf.spec.policy.tolerations == null || self.spec.policy.tolerations == null || oldSelf.spec.policy.tolerations.all(t, self.spec.policy.tolerations.exists(nt, nt == t))",message="tolerations cannot be updated or deleted, only additions are allowed"
   ```

5. **Toleration Validation** (on the `Toleration` struct):
   ```
   +kubebuilder:validation:XValidation:rule="self.key == '' || self.key.matches('^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*)?$')",message="toleration key must be a valid label name"
   +kubebuilder:validation:XValidation:rule="self.value == '' || self.value.matches('^([a-z0-9]([-a-z0-9_.]*[a-z0-9])?)?$')",message="toleration value must be a valid label value"
   +kubebuilder:validation:XValidation:rule="self.operator != 'Exists' || self.value == ''",message="toleration value needs to be empty when operator is Exists"
   +kubebuilder:validation:XValidation:rule="self.operator != 'Equal' || self.key != ''",message="toleration key cannot be empty when operator is Equal"
   ```

## Limitations

It's important to note that some validations cannot be moved from webhooks to CEL annotations due to limitations in the CEL validation mechanism:

1. **User Authentication & Authorization Checks**: These validations depend on the user info which is not available in CEL annotations.
2. **Cross-Resource Validations**: CEL annotations can only validate a single resource instance at a time.
3. **Complex External Dependencies**: Validations that depend on external resources or services are still best handled by webhooks.

## Future Enhancements

Additional CEL validations could be added for:

1. More comprehensive cross-field validations within resources
2. Additional resource types in the Fleet project
3. Further immutability rules for fields that shouldn't change after creation