/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	evictionutils "go.goms.io/fleet/pkg/utils/eviction"
	toolsutils "go.goms.io/fleet/tools/utils"
)

const (
	drainEvictionNameFormat     = "drain-eviction-%s-%s"
	resourceIdentifierKeyFormat = "%s/%s/%s/%s/%s"
)

type DrainCluster struct {
	hubClient                            client.Client
	ClusterName                          string
	ClusterResourcePlacementResourcesMap map[string][]placementv1beta1.ResourceIdentifier
}

func main() {
	scheme := runtime.NewScheme()
	ctx := context.Background()

	hubClusterContext := flag.String("hubClusterContext", "", "the kubectl context for the hub cluster")
	clusterName := flag.String("clusterName", "", "name of the cluster to cordon")
	flag.Parse()

	if *hubClusterContext == "" {
		log.Fatalf("hub cluster context for kubectl cannot be empty")
	}
	if *clusterName == "" {
		log.Fatalf("cluster name to cordon cannot be empty")
	}

	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add custom APIs (cluster) to the runtime scheme: %v", err)
	}
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add custom APIs (placement) to the runtime scheme: %v", err)
	}

	hubClient, err := toolsutils.GetClusterClientFromClusterContext(*hubClusterContext, scheme)
	if err != nil {
		log.Fatalf("failed to create hub cluster client: %v", err)
	}

	drainCluster := DrainCluster{
		hubClient:                            hubClient,
		ClusterName:                          *clusterName,
		ClusterResourcePlacementResourcesMap: make(map[string][]placementv1beta1.ResourceIdentifier),
	}

	if err = drainCluster.cordon(ctx); err != nil {
		log.Fatalf("failed to cordon member cluster %s: %v", drainCluster.ClusterName, err)
	}

	isDrainSuccessful, err := drainCluster.drain(ctx)
	if err != nil {
		log.Fatalf("failed to drain member cluster %s: %v", drainCluster.ClusterName, err)
	}

	if isDrainSuccessful {
		log.Printf("drain was successful for cluster %s", *clusterName)
	} else {
		log.Printf("drain was not successful for cluster %s, some evictions were not successfully executed, "+
			"retrying drain to evict the resources that were not successfully removed", *clusterName)
		// reset ClusterResourcePlacementResourcesMap for retry.
		drainCluster.ClusterResourcePlacementResourcesMap = map[string][]placementv1beta1.ResourceIdentifier{}
		isDrainRetrySuccessful, err := drainCluster.drain(ctx)
		if err != nil {
			log.Fatalf("failed to drain cluster on retry %s: %v", drainCluster.ClusterName, err)
		}
		if isDrainRetrySuccessful {
			log.Printf("drain retry was successful for cluster %s", *clusterName)
		} else {
			uncordonCluster := toolsutils.UncordonCluster{
				HubClient:   hubClient,
				ClusterName: *clusterName,
			}

			if err = uncordonCluster.Uncordon(ctx); err != nil {
				log.Fatalf("failed to uncordon cluster %s: %v", *clusterName, err)
			}
			log.Printf("drain retry was not successful for cluster %s, some evictions were not successfully executed", *clusterName)
		}
	}
}

func (d *DrainCluster) cordon(ctx context.Context) error {
	// add taint to member cluster to ensure resources aren't scheduled on it.
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mc clusterv1beta1.MemberCluster
		if err := d.hubClient.Get(ctx, types.NamespacedName{Name: d.ClusterName}, &mc); err != nil {
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

		return d.hubClient.Update(ctx, &mc)
	})
}

func (d *DrainCluster) drain(ctx context.Context) (bool, error) {
	var crpList placementv1beta1.ClusterResourcePlacementList
	if err := d.hubClient.List(ctx, &crpList); err != nil {
		return false, fmt.Errorf("failed to list cluster resource placements: %w", err)
	}

	// find all unique CRP names for which eviction needs to occur.
	for i := range crpList.Items {
		// list all bindings for the CRP.
		crp := crpList.Items[i]
		var crbList placementv1beta1.ClusterResourceBindingList
		if err := d.hubClient.List(ctx, &crbList, client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crp.Name}); err != nil {
			return false, fmt.Errorf("failed to list cluster resource bindings for CRP %s: %w", crp.Name, err)
		}
		var targetBinding *placementv1beta1.ClusterResourceBinding
		for j := range crbList.Items {
			crb := crbList.Items[j]
			if crb.Spec.TargetCluster == d.ClusterName {
				targetBinding = &crb
			}
		}
		if targetBinding != nil && targetBinding.DeletionTimestamp == nil {
			// check if placement is present.
			if !evictionutils.IsPlacementPresent(targetBinding) {
				return false, fmt.Errorf("placement is not present in cluster %s for CRP %s, "+
					"please retry drain once resources are propagated to the cluster", d.ClusterName, crp.Name)
			}
			// get all successfully applied resources for the CRP.
			resourcesPropagated, err := resourcesPropagatedByCRP(ctx, d.hubClient, crp.Name, d.ClusterName)
			if err != nil {
				return false, fmt.Errorf("failed to get resources propagated by CRP %s: %w", crp.Name, err)
			}
			d.ClusterResourcePlacementResourcesMap[crp.Name] = resourcesPropagated
		}
	}

	if len(d.ClusterResourcePlacementResourcesMap) == 0 {
		log.Printf("There are no resources propagated to %s from fleet using ClusterResourcePlacement resources", d.ClusterName)
		return true, nil
	}

	// create eviction objects for all <crpName, targetCluster>.
	for crpName := range d.ClusterResourcePlacementResourcesMap {
		evictionName := fmt.Sprintf(drainEvictionNameFormat, crpName, d.ClusterName)

		if err := removeExistingEviction(ctx, d.hubClient, evictionName); err != nil {
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
					ClusterName:   d.ClusterName,
				},
			}
			return d.hubClient.Create(ctx, &eviction)
		})

		if err != nil {
			return false, fmt.Errorf("failed to create eviction for CRP %s: %w", crpName, err)
		}
	}

	// wait until all evictions reach a terminal state.
	for crpName := range d.ClusterResourcePlacementResourcesMap {
		err := wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff, func(ctx context.Context) (bool, error) {
			evictionName := fmt.Sprintf(drainEvictionNameFormat, crpName, d.ClusterName)
			eviction := placementv1beta1.ClusterResourcePlacementEviction{}
			if err := d.hubClient.Get(ctx, types.NamespacedName{Name: evictionName}, &eviction); err != nil {
				return false, err
			}
			return evictionutils.IsEvictionInTerminalState(&eviction), nil
		})

		if err != nil {
			return false, fmt.Errorf("failed to wait for evictions to reach terminal state: %w", err)
		}
	}

	isDrainSuccessful := true
	// check if all evictions have been executed.
	for crpName := range d.ClusterResourcePlacementResourcesMap {
		evictionName := fmt.Sprintf(drainEvictionNameFormat, crpName, d.ClusterName)
		eviction := placementv1beta1.ClusterResourcePlacementEviction{}
		if err := d.hubClient.Get(ctx, types.NamespacedName{Name: evictionName}, &eviction); err != nil {
			return false, fmt.Errorf("failed to get eviction %s: %w", evictionName, err)
		}
		executedCondition := eviction.GetCondition(string(placementv1beta1.PlacementEvictionConditionTypeExecuted))
		if executedCondition == nil || executedCondition.Status == metav1.ConditionFalse {
			isDrainSuccessful = false
			log.Printf("eviction %s was not executed successfully for CRP %s", evictionName, crpName)
			continue
		}
		log.Printf("eviction %s was executed successfully for CRP %s", evictionName, crpName)
		// log each resource evicted by CRP.
		for i := range d.ClusterResourcePlacementResourcesMap[crpName] {
			resourceIdentifier := d.ClusterResourcePlacementResourcesMap[crpName][i]
			log.Printf("evicted resource %s propagated by CRP %s", fmt.Sprintf(resourceIdentifierKeyFormat, resourceIdentifier.Group, resourceIdentifier.Version, resourceIdentifier.Kind, resourceIdentifier.Name, resourceIdentifier.Namespace), crpName)
		}
	}

	return isDrainSuccessful, nil
}

func resourcesPropagatedByCRP(ctx context.Context, hubClient client.Client, crpName, clusterName string) ([]placementv1beta1.ResourceIdentifier, error) {
	var work placementv1beta1.Work
	workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, crpName)
	clusterNamespace := fmt.Sprintf(utils.NamespaceNameFormat, clusterName)
	if err := hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: clusterNamespace}, &work); err != nil {
		return nil, fmt.Errorf("failed to get work %s: %w", crpName, err)
	}

	var resourcesPropagated []placementv1beta1.ResourceIdentifier
	manifestConditions := work.Status.ManifestConditions
	for i := range manifestConditions {
		manifestCondition := manifestConditions[i]
		for j := range manifestCondition.Conditions {
			condition := manifestCondition.Conditions[j]
			if condition.Type == placementv1beta1.WorkConditionTypeApplied && condition.Status == metav1.ConditionTrue {
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
