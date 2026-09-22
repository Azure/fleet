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

package fieldindexers

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils/errors"
)

const (
	// The field-based indexes set up for KubeFleet API objects.
	//
	// Important: many KubeFleet components run under the assumption that proper custom fields
	// have been added and indexed in the cache when running. Failure to complete such prior setup **before
	// the manager starts** will result in unexpected behaviors. Make sure that all applicable components
	// are properly set up using the client provided by the hub controller manager, and `SetupWithManager` is
	// called before the manager starts.

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
)

type fieldValueExtractor func(obj client.Object) ([]string, error)

func indexCompositeField(ctx context.Context,
	fieldIdxer client.FieldIndexer,
	obj client.Object,
	fieldName string, fieldValueExt fieldValueExtractor) error {
	if err := fieldIdxer.IndexField(ctx, obj, fieldName, func(rawObj client.Object) []string {
		fieldVals, extErr := fieldValueExt(rawObj)
		if extErr != nil {
			wrappedErr := errors.NewUnexpectedError(extErr, "failed to extract field value", "object", klog.KObj(rawObj))
			klog.ErrorS(wrappedErr, "failed to index field", errors.Args(wrappedErr)...)
			return nil
		}
		return fieldVals
	}); err != nil {
		wrappedErr := errors.NewUnexpectedError(err, "", "fieldName", fieldName, "object", klog.KObj(obj))
		klog.ErrorS(wrappedErr, "failed to index field", errors.Args(wrappedErr)...)
		return wrappedErr
	}
	return nil
}

var (
	placementResourceSnapshotOwnedByAndSubIdxedFieldExtractor fieldValueExtractor = func(obj client.Object) ([]string, error) {
		ownedBy := obj.GetLabels()[placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey]
		subIndex := obj.GetLabels()[placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey]
		if ownedBy == "" || subIndex == "" {
			wrappedErr := errors.NewUnexpectedError(nil, "placement resource snapshot is missing required labels")
			return nil, wrappedErr
		}
		return []string{fmt.Sprintf(PlacementResourceSnapshotOwnedByAndSubIndexedCustomFieldValFmt, ownedBy, subIndex)}, nil
	}

	placementResourceSnapshotOwnedByAndIdxedFieldExtractor fieldValueExtractor = func(obj client.Object) ([]string, error) {
		ownedBy := obj.GetLabels()[placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey]
		index := obj.GetLabels()[placementv1alpha1.PlacementResourceSnapshotIndexLabelKey]
		if ownedBy == "" || index == "" {
			wrappedErr := errors.NewUnexpectedError(nil, "placement resource snapshot is missing required labels")
			return nil, wrappedErr
		}
		return []string{fmt.Sprintf(PlacementResourceSnapshotOwnedByAndIndexedCustomFieldValFmt, ownedBy, index)}, nil
	}
)

// SetupWithHubControllerManager sets up the indices that controllers from the KubeFleet hub agent need to run properly.
// It must be called before the manager starts.
func SetupWithHubControllerManager(ctx context.Context, mgr ctrl.Manager) error {
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

	return nil
}
