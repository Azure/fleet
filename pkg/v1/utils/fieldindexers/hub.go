/*
Copyright 2026 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package fieldindexers provides utilities for setting up and using field-based indexes for KubeFleet API objects on the KubeFleet agent side.
package fieldindexers

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils/errors"
)

const (
	// The field-based indexes set up for KubeFleet API objects (on the hub agent side).
	//
	// Important: many KubeFleet components on the hub agent side run under the assumption that proper custom fields
	// have been added and indexed in the cache when running. Failure to complete such prior setup **before
	// the manager starts** will result in unexpected behaviors/failures. Make sure that all applicable components
	// are properly set up using the client provided by the hub controller manager, and
	// `SetupWithHubAgentControllerManager` is called before the manager starts.

	// PlacementResourceSnapshotOwnedByAndSubIndexedCustomFieldName is the name of the custom field that indexes
	// placement resource snapshots by their owner placement policies and their sub-indices.
	//
	// This is added to help the placement resource snapshot manager retrieve all primary placement resource
	// snapshots (i.e., those with a sub-index of 0) associated with a placement policy.
	PlacementResourceSnapshotOwnedByAndSubIndexedCustomFieldName = "ownedByWithSubIndex"

	// PlacementResourceSnapshotOwnedByAndIndexedCustomFieldName is the name of the custom field that indexes
	// placement resource snapshots by their owner placement policies and their indices.
	//
	// This is added to help the placement resource snapshot manager retrieve all placement resource snapshots of a
	// specific index associated with a placement policy.
	PlacementResourceSnapshotOwnedByAndIndexedCustomFieldName = "ownedByWithIndex"

	// WorkOwnedByBindingCustomFieldName is the name of the custom field that indexes Work objects by their owner
	// placement bindings.
	WorkOwnedByBindingCustomFieldName = "workOwnedByBinding"

	// PlacementBindingOwnedByCustomFieldName is the name of the custom field that indexes placement binding objects
	// by their owner placement policies.
	PlacementBindingOwnedByCustomFieldName = "placementBindingOwnedBy"

	// PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName is the name of the custom field that indexes
	// placement bindings by their owner placement policies and the (primary) resource snapshots they are using.
	PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName = "ownedByAndInUseOfPrimaryResourceSnapshot"

	// ClusterClaimOwnedByPlacementPolicyCustomFieldName is the name of the custom field that indexes cluster
	// claims by the placement policy that their immutable spec.placementPolicyRef names.
	//
	// This is added to help the placement policy controller retrieve the claims of a policy without scanning
	// every claim in the fleet; the ownership labels are mutable, so the reference is what gets indexed.
	ClusterClaimOwnedByPlacementPolicyCustomFieldName = "clusterClaimOwnedByPlacementPolicy"
)

const (
	// The format of the custom field values for the field-based indexes defined above.

	// PlacementResourceSnapshotOwnedByAndSubIndexedCustomFieldValFmt is used to format the value for the custom field,
	// `PlacementResourceSnapshotOwnedByAndSubIndexedCustomFieldName`.
	//
	// Note that slashes are used to avoid unexpected collisions.
	PlacementResourceSnapshotOwnedByAndSubIndexedCustomFieldValFmt = "%s/%s"

	// PlacementResourceSnapshotOwnedByAndIndexedCustomFieldValFmt is used to format the value for the custom field,
	// `PlacementResourceSnapshotOwnedByAndIndexedCustomFieldName`.
	//
	// Note that slashes are used to avoid unexpected collisions.
	PlacementResourceSnapshotOwnedByAndIndexedCustomFieldValFmt = "%s/%s"

	// WorkOwnedByBindingCustomFieldValFmt is used to format the value for the custom field,
	// `WorkOwnedByBindingCustomFieldName`.
	//
	// The first value is the owner binding namespace, and the second value is the owner binding name.
	WorkOwnedByBindingCustomFieldValFmt = "%s/%s"

	// ClusterClaimOwnedByPlacementPolicyCustomFieldValFmt is used to format the value for the custom field,
	// `ClusterClaimOwnedByPlacementPolicyCustomFieldName`.
	//
	// The values are the API group, kind, namespace, and name of the referenced placement policy; the API
	// version is left out so that an API promotion does not orphan the claims of a policy.
	ClusterClaimOwnedByPlacementPolicyCustomFieldValFmt = "%s/%s/%s/%s"

	// PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt is used to format the value for the custom field,
	// `PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName`.
	//
	// The first value is the owner placement policy, and the second value is the primary resource snapshot being used.
	PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt = "%s/%s"
)

var (
	placementResourceSnapshotOwnedByAndSubIdxedFieldExtractor fieldValueExtractor = func(obj client.Object) ([]string, error) {
		ownedBy := obj.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey]
		subIndex := obj.GetLabels()[placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey]
		if ownedBy == "" || subIndex == "" {
			wrappedErr := errors.NewUnexpectedError(nil, "placement resource snapshot is missing required labels or annotations")
			return nil, wrappedErr
		}
		return []string{fmt.Sprintf(PlacementResourceSnapshotOwnedByAndSubIndexedCustomFieldValFmt, ownedBy, subIndex)}, nil
	}

	placementResourceSnapshotOwnedByAndIdxedFieldExtractor fieldValueExtractor = func(obj client.Object) ([]string, error) {
		ownedBy := obj.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey]
		index := obj.GetLabels()[placementv1alpha1.PlacementResourceSnapshotIndexLabelKey]
		if ownedBy == "" || index == "" {
			wrappedErr := errors.NewUnexpectedError(nil, "placement resource snapshot is missing required labels or annotations")
			return nil, wrappedErr
		}
		return []string{fmt.Sprintf(PlacementResourceSnapshotOwnedByAndIndexedCustomFieldValFmt, ownedBy, index)}, nil
	}

	workOwnedByBindingFieldExtractor fieldValueExtractor = func(obj client.Object) ([]string, error) {
		ownerNS, ownerNSFound := obj.GetLabels()[placementv1alpha1.WorkOwnerNamespaceLabelKey]
		ownerBinding := obj.GetLabels()[placementv1alpha1.WorkOwnedByPlacementBindingLabelKey]
		if !ownerNSFound || ownerBinding == "" {
			wrappedErr := errors.NewUnexpectedError(nil, "work is missing required owner binding metadata")
			return nil, wrappedErr
		}
		return []string{fmt.Sprintf(WorkOwnedByBindingCustomFieldValFmt, ownerNS, ownerBinding)}, nil
	}

	placementBindingOwnedByFieldExtractor fieldValueExtractor = func(obj client.Object) ([]string, error) {
		binding, ok := obj.(placementv1alpha1.PlacementBindingAccessor)
		if !ok {
			return nil, errors.NewUnexpectedError(nil, "object is not a placement binding")
		}
		placementPolicyName := binding.GetSpec().PlacementPolicyName
		if placementPolicyName == "" {
			return nil, errors.NewUnexpectedError(nil, "placement binding is missing the placement policy name in its spec")
		}
		return []string{placementPolicyName}, nil
	}

	placementBindingOwnedByAndInUseOfPrimaryResourceSnapshotFieldExtractor fieldValueExtractor = func(obj client.Object) ([]string, error) {
		placementBinding, ok := obj.(placementv1alpha1.PlacementBindingAccessor)
		if !ok {
			wrappedErr := errors.NewUnexpectedError(nil, "object is not a placement binding", "type", fmt.Sprintf("%T", obj))
			return nil, wrappedErr
		}
		spec := placementBinding.GetSpec()
		if spec.PlacementPolicyName == "" || spec.ResourceSnapshotName == "" {
			wrappedErr := errors.NewUnexpectedError(nil, "placement binding is missing the owner placement policy name or the resource snapshot name")
			return nil, wrappedErr
		}
		return []string{fmt.Sprintf(PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt, spec.PlacementPolicyName, spec.ResourceSnapshotName)}, nil
	}

	clusterClaimOwnedByPlacementPolicyFieldExtractor fieldValueExtractor = func(obj client.Object) ([]string, error) {
		claim, ok := obj.(*placementv1alpha1.ClusterClaim)
		if !ok || claim.Spec.PlacementPolicyRef == nil {
			wrappedErr := errors.NewUnexpectedError(nil, "object is not a cluster claim with a placement policy reference")
			return nil, wrappedErr
		}
		return []string{ClusterClaimOwnedByPlacementPolicyCustomFieldVal(claim.Spec.PlacementPolicyRef)}, nil
	}
)

// ClusterClaimOwnedByPlacementPolicyCustomFieldVal formats a placement policy reference as the value of the
// custom field, `ClusterClaimOwnedByPlacementPolicyCustomFieldName`.
func ClusterClaimOwnedByPlacementPolicyCustomFieldVal(ref *placementv1alpha1.ObjectReference) string {
	return fmt.Sprintf(ClusterClaimOwnedByPlacementPolicyCustomFieldValFmt, ref.APIGroup, ref.Kind, ref.Namespace, ref.Name)
}

// SetupWithHubAgentControllerManager sets up the indices that controllers from the KubeFleet hub agent need to run properly.
//
// It must be called before the manager starts.
func SetupWithHubAgentControllerManager(ctx context.Context, mgr ctrl.Manager) error {
	fieldIdxer := mgr.GetFieldIndexer()

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.PlacementResourceSnapshot{},
		PlacementResourceSnapshotOwnedByAndSubIndexedCustomFieldName, placementResourceSnapshotOwnedByAndSubIdxedFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up placement resource snapshot owner and sub-index field index")
	}

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.PlacementResourceSnapshot{},
		PlacementResourceSnapshotOwnedByAndIndexedCustomFieldName, placementResourceSnapshotOwnedByAndIdxedFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up placement resource snapshot owner and index field index")
	}

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.ClusterPlacementResourceSnapshot{},
		PlacementResourceSnapshotOwnedByAndSubIndexedCustomFieldName, placementResourceSnapshotOwnedByAndSubIdxedFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up cluster placement resource snapshot owner and sub-index field index")
	}

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.ClusterPlacementResourceSnapshot{},
		PlacementResourceSnapshotOwnedByAndIndexedCustomFieldName, placementResourceSnapshotOwnedByAndIdxedFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up cluster placement resource snapshot owner and index field index")
	}

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.Work{},
		WorkOwnedByBindingCustomFieldName, workOwnedByBindingFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up work owner binding field index")
	}

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.PlacementBinding{},
		PlacementBindingOwnedByCustomFieldName, placementBindingOwnedByFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up placement binding owner field index")
	}

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.PlacementBinding{},
		PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName, placementBindingOwnedByAndInUseOfPrimaryResourceSnapshotFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up placement binding owner and in-use primary resource snapshot field index")
	}

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.ClusterPlacementBinding{},
		PlacementBindingOwnedByCustomFieldName, placementBindingOwnedByFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up cluster placement binding owner field index")
	}

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.ClusterPlacementBinding{},
		PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName, placementBindingOwnedByAndInUseOfPrimaryResourceSnapshotFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up cluster placement binding owner and in-use primary resource snapshot field index")
	}

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.ClusterClaim{},
		ClusterClaimOwnedByPlacementPolicyCustomFieldName, clusterClaimOwnedByPlacementPolicyFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up cluster claim owner placement policy field index")
	}

	return nil
}
