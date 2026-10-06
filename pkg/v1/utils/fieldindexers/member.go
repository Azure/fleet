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

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

const (
	// The field-based indexes set up for KubeFleet API objects (on the member agent side).
	//
	// Important: many KubeFleet components on the member agent side run under the assumption that proper custom fields
	// have been added and indexed in the cache when running. Failure to complete such prior setup **before
	// the manager starts** will result in unexpected behaviors/failures. Make sure that all applicable components
	// are properly set up using the client provided by the member controller manager, and
	// `SetupWithMemberAgentControllerManager` is called before the manager starts.

	// WorkOwnedByPlacementPolicyCustomFieldName is the name of the custom field that indexes
	// work objects by their owner placement policies.
	//
	// This is added to help the work applier retrieve all work objects associated with a placement policy.
	WorkOwnedByPlacementPolicyCustomFieldName = "ownedByPlacementPolicy"

	// WorkOwnedByPlacementBindingCustomFieldName is the name of the custom field that indexes
	// work objects by their owner placement bindings.
	//
	// This is added to help the work applier retrieve all the work objects associated with a placement binding
	// in one batch.
	WorkOwnedByPlacementBindingCustomFieldName = "ownedByPlacementBinding"
)

const (
	// The format of the custom field values for the field-based indexes defined above.

	// WorkOwnedByPlacementPolicyCustomFieldValFmt is used to format the value for the custom field,
	// `WorkOwnedByPlacementPolicyCustomFieldName`, in the form of `[OWNER-NAMESPACE]/[OWNER-NAME]`. The owner
	// namespace is empty for cluster-scoped placement policies.
	//
	// Note that slashes are used to avoid unexpected collisions.
	WorkOwnedByPlacementPolicyCustomFieldValFmt = "%s/%s"

	// WorkOwnedByPlacementBindingCustomFieldValFmt is used to format the value for the custom field,
	// `WorkOwnedByPlacementBindingCustomFieldName`, in the form of `[OWNER-NAMESPACE]/[OWNER-NAME]`. The owner
	// namespace is empty for cluster-scoped placement bindings.
	//
	// Note that slashes are used to avoid unexpected collisions.
	WorkOwnedByPlacementBindingCustomFieldValFmt = "%s/%s"
)

var (
	workOwnedByPlacementPolicyFieldExtractor fieldValueExtractor = func(obj client.Object) ([]string, error) {
		ownerNSName := obj.GetLabels()[placementv1alpha1.WorkOwnerNamespaceLabelKey]
		ownerName := obj.GetLabels()[placementv1alpha1.WorkOwnedByPlacementPolicyLabelKey]
		if ownerName == "" {
			wrappedErr := errors.NewUnexpectedError(nil, "work object is missing required labels")
			return nil, wrappedErr
		}
		return []string{fmt.Sprintf(WorkOwnedByPlacementPolicyCustomFieldValFmt, ownerNSName, ownerName)}, nil
	}

	workOwnedByPlacementBindingFieldExtractor fieldValueExtractor = func(obj client.Object) ([]string, error) {
		ownerNSName := obj.GetLabels()[placementv1alpha1.WorkOwnerNamespaceLabelKey]
		ownerName := obj.GetLabels()[placementv1alpha1.WorkOwnedByPlacementBindingLabelKey]
		if ownerName == "" {
			wrappedErr := errors.NewUnexpectedError(nil, "work object is missing required labels")
			return nil, wrappedErr
		}
		return []string{fmt.Sprintf(WorkOwnedByPlacementBindingCustomFieldValFmt, ownerNSName, ownerName)}, nil
	}
)

// SetupWithMemberAgentControllerManager sets up the indices that controllers from the KubeFleet member agent need to
// run properly.
//
// It must be called before the manager starts.
func SetupWithMemberAgentControllerManager(ctx context.Context, mgr ctrl.Manager) error {
	fieldIdxer := mgr.GetFieldIndexer()

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.Work{},
		WorkOwnedByPlacementPolicyCustomFieldName, workOwnedByPlacementPolicyFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up work owner and placement policy field index")
	}

	if err := indexCompositeField(ctx, fieldIdxer,
		&placementv1alpha1.Work{},
		WorkOwnedByPlacementBindingCustomFieldName, workOwnedByPlacementBindingFieldExtractor,
	); err != nil {
		return errors.Wraps(err, "failed to set up work owner and placement binding field index")
	}

	return nil
}
