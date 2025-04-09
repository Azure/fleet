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
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	evictionutils "go.goms.io/fleet/pkg/utils/eviction"
	toolsutils "go.goms.io/fleet/tools/utils"
)

const (
	uuidLength                  = 8
	drainEvictionNameFormat     = "drain-eviction-%s-%s-%s"
	resourceIdentifierKeyFormat = "%s/%s/%s/%s/%s"
)

type Helper struct {
	HubClient   client.Client
	ClusterName string
}

func (h *Helper) Drain(ctx context.Context) (bool, error) {
	if err := h.cordon(ctx); err != nil {
		return false, fmt.Errorf("failed to cordon member cluster %s: %w", h.ClusterName, err)
	}
	log.Printf("Successfully cordoned member cluster %s by adding cordon taint", h.ClusterName)

	crpNames, err := h.fetchClusterResourcePlacementNamesToEvict(ctx)
	if err != nil {
		return false, err
	}

	if len(crpNames) == 0 {
		log.Printf("There are currently no resources propagated to %s from fleet using ClusterResourcePlacement resources", h.ClusterName)
		return true, nil
	}

	isDrainSuccessful := true
	// create eviction objects for all <crpName, targetCluster>.
	for _, crpName := range crpNames {
		evictionName, err := generateDrainEvictionName(crpName, h.ClusterName)
		if err != nil {
			return false, err
		}

		err = retry.OnError(retry.DefaultBackoff, func(err error) bool {
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

		// wait until evictions reach a terminal state.
		var eviction placementv1beta1.ClusterResourcePlacementEviction
		err = wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff, func(ctx context.Context) (bool, error) {
			if err := h.HubClient.Get(ctx, types.NamespacedName{Name: evictionName}, &eviction); err != nil {
				return false, fmt.Errorf("failed to get eviction %s: %w", evictionName, err)
			}
			return evictionutils.IsEvictionInTerminalState(&eviction), nil
		})

		if err != nil {
			return false, fmt.Errorf("failed to wait for evictions to reach terminal state: %w", err)
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
		// log each cluster scoped resource evicted for CRP.
		clusterScopedResourceIdentifiers, err := h.collectClusterScopedResourcesSelectedByCRP(ctx, crpName)
		if err != nil {
			log.Printf("failed to collect cluster scoped resources selected by CRP %s: %v", crpName, err)
			continue
		}
		for _, resourceIdentifier := range clusterScopedResourceIdentifiers {
			log.Printf("evicted resource %s propagated by CRP %s", generateResourceIdentifierKey(resourceIdentifier), crpName)
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

func (h *Helper) fetchClusterResourcePlacementNamesToEvict(ctx context.Context) ([]string, error) {
	var crbList placementv1beta1.ClusterResourceBindingList
	if err := h.HubClient.List(ctx, &crbList); err != nil {
		return []string{}, fmt.Errorf("failed to list cluster resource bindings: %w", err)
	}

	crpNameMap := make(map[string]bool)
	// find all unique CRP names for which eviction needs to occur.
	for i := range crbList.Items {
		crb := crbList.Items[i]
		if crb.Spec.TargetCluster == h.ClusterName {
			crpName, ok := crb.GetLabels()[placementv1beta1.CRPTrackingLabel]
			if !ok {
				return []string{}, fmt.Errorf("failed to get CRP name from binding %s", crb.Name)
			}
			if !crpNameMap[crpName] {
				crpNameMap[crpName] = true
			}
		}
	}

	crpNames := make([]string, len(crpNameMap))
	i := 0
	for crpName := range crpNameMap {
		crpNames[i] = crpName
		i++
	}

	return crpNames, nil
}

func (h *Helper) collectClusterScopedResourcesSelectedByCRP(ctx context.Context, crpName string) ([]placementv1beta1.ResourceIdentifier, error) {
	var crp placementv1beta1.ClusterResourcePlacement
	if err := h.HubClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp); err != nil {
		return nil, fmt.Errorf("failed to get ClusterResourcePlacement %s: %w", crpName, err)
	}

	var resourcesPropagated []placementv1beta1.ResourceIdentifier
	for _, selectedResource := range crp.Status.SelectedResources {
		// only collect cluster scoped resources.
		if len(selectedResource.Namespace) == 0 {
			resourcesPropagated = append(resourcesPropagated, selectedResource)
		}
	}
	return resourcesPropagated, nil
}

func generateDrainEvictionName(crpName, targetCluster string) (string, error) {
	evictionName := fmt.Sprintf(drainEvictionNameFormat, crpName, targetCluster, uuid.NewUUID()[:uuidLength])

	if errs := validation.IsQualifiedName(evictionName); len(errs) != 0 {
		return "", fmt.Errorf("failed to format a qualified name for drain eviction object with CRP name %s, cluster name %s: %v", crpName, targetCluster, errs)
	}
	return evictionName, nil
}

func generateResourceIdentifierKey(r placementv1beta1.ResourceIdentifier) string {
	if len(r.Group) == 0 && len(r.Namespace) == 0 {
		return fmt.Sprintf(resourceIdentifierKeyFormat, "''", r.Version, r.Kind, "''", r.Name)
	}
	if len(r.Namespace) == 0 {
		return fmt.Sprintf(resourceIdentifierKeyFormat, r.Group, r.Version, r.Kind, "''", r.Name)
	}
	return fmt.Sprintf(resourceIdentifierKeyFormat, r.Group, r.Version, r.Kind, r.Namespace, r.Name)
}
