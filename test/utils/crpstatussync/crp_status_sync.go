package crpstatussync

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

var (
	// Define comparison options for ignoring auto-generated and time-dependent fields.
	crpsCmpOpts = []cmp.Option{
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "UID", "CreationTimestamp", "Generation", "ManagedFields"),
		cmpopts.IgnoreFields(placementv1beta1.ClusterResourcePlacementStatus{}, "LastUpdatedTime"),
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
	}
)

func CRPSStatusMatchesCRPActual(ctx context.Context, client client.Client, crpName, targetNamespace string) func() error {
	return func() error {
		crpStatus := &placementv1beta1.ClusterResourcePlacementStatus{}
		crpStatusKey := types.NamespacedName{
			Name:      crpName,
			Namespace: targetNamespace,
		}

		if err := client.Get(ctx, crpStatusKey, crpStatus); err != nil {
			return fmt.Errorf("failed to get CRPS: %w", err)
		}

		// Get latest CRP status.
		crp := &placementv1beta1.ClusterResourcePlacement{}
		if err := client.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return fmt.Errorf("failed to get CRP: %w", err)
		}

		// Construct expected CRPS.
		wantCRPS := &placementv1beta1.ClusterResourcePlacementStatus{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crpName,
				Namespace: targetNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         placementv1beta1.GroupVersion.String(),
						Kind:               "ClusterResourcePlacement",
						Name:               crpName,
						UID:                crp.UID,
						Controller:         ptr.To(true),
						BlockOwnerDeletion: ptr.To(true),
					},
				},
			},
			PlacementStatus: crp.Status,
		}

		// Compare CRPS with expected, ignoring fields that vary.
		if diff := cmp.Diff(wantCRPS, crpStatus, crpsCmpOpts...); diff != "" {
			return fmt.Errorf("CRPS does not match expected (-want, +got): %s", diff)
		}

		return nil
	}
}
