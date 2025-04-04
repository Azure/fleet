/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package drain

import (
	"context"
	"fmt"
	"log"

	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	evictionutils "go.goms.io/fleet/pkg/utils/eviction"
	toolsutils "go.goms.io/fleet/tools/utils"
)

const (
	drainEvictionNameFormat     = "drain-eviction-%s-%s"
	resourceIdentifierKeyFormat = "%s/%s/%s/%s/%s"
)

type Helper struct {
	HubClient                            client.Client
	ClusterName                          string
	ClusterResourcePlacementResourcesMap map[string][]placementv1beta1.ResourceIdentifier
}

func (h *Helper) Drain(ctx context.Context) (bool, error) {
	if err := h.cordon(ctx); err != nil {
		return false, fmt.Errorf("failed to cordon member cluster %s: %w", h.ClusterName, err)
	}

	if err := h.fetchClusterResourcePlacementToEvict(ctx); err != nil {
		return false, err
	}

	if len(h.ClusterResourcePlacementResourcesMap) == 0 {
		log.Printf("There are currently no resources propagated to %s from fleet using ClusterResourcePlacement resources", h.ClusterName)
		return true, nil
	}

	// create eviction objects for all <crpName, targetCluster>.
	for crpName := range h.ClusterResourcePlacementResourcesMap {
		evictionName := fmt.Sprintf(drainEvictionNameFormat, crpName, h.ClusterName)

		if err := removeExistingEviction(ctx, h.HubClient, evictionName); err != nil {
			return false, fmt.Errorf("failed to remove existing eviction for CRP %s", crpName)
		}

		err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
			return k8errors.IsAlreadyExists(err)
		}, func() error {
			eviction := placementv1beta1.ClusterResourcePlacementEviction{
				ObjectMeta: metav1.ObjectMeta{
					Name: evictionName,
				},
				Spec: placementv1beta1.PlacementEvictionSpec{
					PlacementName: crpName,
					ClusterName:   h.ClusterName,
				},
			}
			return h.HubClient.Create(ctx, &eviction)
		})

		if err != nil {
			return false, fmt.Errorf("failed to create eviction for CRP %s: %w", crpName, err)
		}
	}

	// wait until all evictions reach a terminal state.
	for crpName := range h.ClusterResourcePlacementResourcesMap {
		err := wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff, func(ctx context.Context) (bool, error) {
			evictionName := fmt.Sprintf(drainEvictionNameFormat, crpName, h.ClusterName)
			eviction := placementv1beta1.ClusterResourcePlacementEviction{}
			if err := h.HubClient.Get(ctx, types.NamespacedName{Name: evictionName}, &eviction); err != nil {
				return false, fmt.Errorf("failed to get eviction %s: %w", evictionName, err)
			}
			return evictionutils.IsEvictionInTerminalState(&eviction), nil
		})

		if err != nil {
			return false, fmt.Errorf("failed to wait for evictions to reach terminal state: %w", err)
		}
	}

	isDrainSuccessful := true
	for crpName := range h.ClusterResourcePlacementResourcesMap {
		evictionName := fmt.Sprintf(drainEvictionNameFormat, crpName, h.ClusterName)
		eviction := placementv1beta1.ClusterResourcePlacementEviction{}
		if err := h.HubClient.Get(ctx, types.NamespacedName{Name: evictionName}, &eviction); err != nil {
			return false, fmt.Errorf("failed to get eviction %s: %w", evictionName, err)
		}
		validCondition := eviction.GetCondition(string(placementv1beta1.PlacementEvictionConditionTypeValid))
		if validCondition != nil && validCondition.Status == metav1.ConditionFalse {
			// check to see if CRP is missing or CRP is being deleted or CRB is missing.
			if validCondition.Reason == condition.EvictionInvalidMissingCRPMessage ||
				validCondition.Reason == condition.EvictionInvalidDeletingCRPMessage ||
				validCondition.Reason == condition.EvictionInvalidMissingCRBMessage {
				continue
			}
		}
		executedCondition := eviction.GetCondition(string(placementv1beta1.PlacementEvictionConditionTypeExecuted))
		if executedCondition == nil || executedCondition.Status == metav1.ConditionFalse {
			isDrainSuccessful = false
			log.Printf("eviction %s was not executed successfully for CRP %s", evictionName, crpName)
			continue
		}
		log.Printf("eviction %s was executed successfully for CRP %s", evictionName, crpName)
		// log each resource evicted by CRP.
		for i := range h.ClusterResourcePlacementResourcesMap[crpName] {
			resourceIdentifier := h.ClusterResourcePlacementResourcesMap[crpName][i]
			log.Printf("evicted resource %s propagated by CRP %s", fmt.Sprintf(resourceIdentifierKeyFormat, resourceIdentifier.Group, resourceIdentifier.Version, resourceIdentifier.Kind, resourceIdentifier.Name, resourceIdentifier.Namespace), crpName)
		}
	}

	return isDrainSuccessful, nil
}

func (h *Helper) cordon(ctx context.Context) error {
	// add taint to member cluster to ensure resources aren't scheduled on it.
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mc clusterv1beta1.MemberCluster
		if err := h.HubClient.Get(ctx, types.NamespacedName{Name: h.ClusterName}, &mc); err != nil {
			return err
		}

		// search to see cordonTaint already exists on the cluster.
		for i := range mc.Spec.Taints {
			if mc.Spec.Taints[i] == toolsutils.CordonTaint {
				return nil
			}
		}

		// add taint to member cluster to cordon.
		mc.Spec.Taints = append(mc.Spec.Taints, toolsutils.CordonTaint)

		return h.HubClient.Update(ctx, &mc)
	})
}

func (h *Helper) fetchClusterResourcePlacementToEvict(ctx context.Context) error {
	var crbList placementv1beta1.ClusterResourceBindingList
	if err := h.HubClient.List(ctx, &crbList); err != nil {
		return fmt.Errorf("failed to list cluster resource bindings: %w", err)
	}

	// find all unique CRP names for which eviction needs to occur.
	for i := range crbList.Items {
		crb := crbList.Items[i]
		var targetBinding *placementv1beta1.ClusterResourceBinding
		if crb.Spec.TargetCluster == h.ClusterName {
			targetBinding = &crb
		}

		if targetBinding != nil {
			err := wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff, func(ctx context.Context) (bool, error) {
				if getErr := h.HubClient.Get(ctx, types.NamespacedName{Name: targetBinding.Name}, targetBinding); getErr != nil {
					// binding may have been deleted.
					if k8errors.IsNotFound(getErr) {
						return true, nil
					}
					return false, getErr
				}
				if !evictionutils.IsPlacementPresent(targetBinding) {
					// need to wait until placement is present on member cluster.
					return false, nil
				}
				return true, nil
			})

			if err != nil {
				return fmt.Errorf("failed to wait for placement to be present on member cluster: %w", err)
			} else {
				// At this point, the binding could be deleting or deleted we will still try to collect resources
				// propagated by the CRP and issue eviction.
				// get all successfully applied resources for the CRP.
				crpName := crb.GetLabels()[placementv1beta1.CRPTrackingLabel]
				resourcesPropagated, err := h.collectResourcesPropagatedByCRP(ctx, crpName)
				if err != nil {
					return fmt.Errorf("failed to get resources propagated by CRP %s: %w", crpName, err)
				}
				h.ClusterResourcePlacementResourcesMap[crpName] = resourcesPropagated
			}
		}
	}
	return nil
}

func (h *Helper) collectResourcesPropagatedByCRP(ctx context.Context, crpName string) ([]placementv1beta1.ResourceIdentifier, error) {
	var workList placementv1beta1.WorkList
	if err := h.HubClient.List(ctx, &workList, client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crpName}, client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, h.ClusterName))); err != nil {
		return nil, fmt.Errorf("failed to get work %s: %w", crpName, err)
	}

	var resourcesPropagated []placementv1beta1.ResourceIdentifier
	for i := range workList.Items {
		work := workList.Items[i]
		manifestConditions := work.Status.ManifestConditions
		for j := range manifestConditions {
			manifestCondition := manifestConditions[j]
			// skip namespace scoped resources.
			if len(manifestCondition.Identifier.Namespace) != 0 {
				continue
			}
			for k := range manifestCondition.Conditions {
				c := manifestCondition.Conditions[k]
				if c.Type == placementv1beta1.WorkConditionTypeApplied && c.Status == metav1.ConditionTrue {
					propagatedResource := placementv1beta1.ResourceIdentifier{
						Group:     manifestCondition.Identifier.Group,
						Version:   manifestCondition.Identifier.Version,
						Kind:      manifestCondition.Identifier.Kind,
						Name:      manifestCondition.Identifier.Name,
						Namespace: manifestCondition.Identifier.Namespace,
					}
					resourcesPropagated = append(resourcesPropagated, propagatedResource)
				}
			}
		}
	}
	return resourcesPropagated, nil
}

func removeExistingEviction(ctx context.Context, client client.Client, evictionName string) error {
	eviction := placementv1beta1.ClusterResourcePlacementEviction{}
	if err := client.Get(ctx, types.NamespacedName{Name: evictionName}, &eviction); err != nil {
		if k8errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return client.Delete(ctx, &eviction)
}
