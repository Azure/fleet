package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/tools/utils"
)

var (
	scheme = runtime.NewScheme()
	ctx    = context.Background()
)

const (
	testEvictionNameFormat      = "test-eviction-%s-%s"
	resourceIdentifierKeyFormat = "%s/%s/%s/%s/%s"
)

type DrainCluster struct {
	hubClient                            client.Client
	ClusterName                          string
	ClusterResourcePlacementResourcesMap map[string][]placementv1beta1.ResourceIdentifier
}

func main() {
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

	hubClient, err := utils.GetClusterClientFromClusterContext(*hubClusterContext, scheme)
	if err != nil {
		log.Fatalf("failed to create hub cluster client: %v", err)
	}

	drainCluster := DrainCluster{
		hubClient:                            hubClient,
		ClusterName:                          *clusterName,
		ClusterResourcePlacementResourcesMap: make(map[string][]placementv1beta1.ResourceIdentifier),
	}

	if err = drainCluster.cordonCluster(); err != nil {
		log.Fatalf("failed to cordon member cluster %s: %v", drainCluster.ClusterName, err)
	}

	var crpList placementv1beta1.ClusterResourcePlacementList
	if err = drainCluster.hubClient.List(ctx, &crpList); err != nil {
		log.Fatalf("failed to list cluster resource placements: %v", err)
	}

	// find all unique CRP names for which eviction needs to occur.
	for i := range crpList.Items {
		crpStatus := crpList.Items[i].Status
		for j := range crpStatus.PlacementStatuses {
			placementStatus := crpStatus.PlacementStatuses[j]
			if placementStatus.ClusterName == *clusterName {
				drainCluster.ClusterResourcePlacementResourcesMap[crpList.Items[i].Name] = resourcePropagatedByCRP(crpStatus.SelectedResources, placementStatus.FailedPlacements)
				break
			}
		}
	}

	if len(drainCluster.ClusterResourcePlacementResourcesMap) == 0 {
		log.Printf("There are no resources propagated to %s from fleet using ClusterResourcePlacement resources", *clusterName)
		os.Exit(0)
	}

	// create eviction objects for all <crpName, targetCluster>.
	for crpName := range drainCluster.ClusterResourcePlacementResourcesMap {
		evictionName := fmt.Sprintf(testEvictionNameFormat, crpName, *clusterName)

		if err = removeExistingEviction(ctx, drainCluster.hubClient, evictionName); err != nil {
			log.Fatalf("failed to remove existing eviction for CRP %s", crpName)
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
					ClusterName:   *clusterName,
				},
			}
			return drainCluster.hubClient.Create(ctx, &eviction)
		})

		if err != nil {
			log.Fatalf("failed to create eviction for CRP %s: %v", crpName, err)
		}
	}

	// TODO: move isEvictionInTerminalState to a function in the pkg/utils package.
	// wait until all evictions reach a terminal state.
	for crpName := range drainCluster.ClusterResourcePlacementResourcesMap {
		err = wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff, func(ctx context.Context) (bool, error) {
			evictionName := fmt.Sprintf(testEvictionNameFormat, crpName, *clusterName)
			eviction := placementv1beta1.ClusterResourcePlacementEviction{}
			if err := drainCluster.hubClient.Get(ctx, types.NamespacedName{Name: evictionName}, &eviction); err != nil {
				return false, err
			}
			validCondition := eviction.GetCondition(string(placementv1beta1.PlacementEvictionConditionTypeValid))
			if condition.IsConditionStatusFalse(validCondition, eviction.GetGeneration()) {
				return true, nil
			}
			executedCondition := eviction.GetCondition(string(placementv1beta1.PlacementEvictionConditionTypeExecuted))
			if executedCondition != nil {
				return true, nil
			}
			return false, nil
		})

		if err != nil {
			log.Fatalf("failed to wait for evictions to reach terminal state: %v", err)
		}
	}

	isDrainSuccessful := true
	// check if all evictions have been executed.
	for crpName := range drainCluster.ClusterResourcePlacementResourcesMap {
		evictionName := fmt.Sprintf(testEvictionNameFormat, crpName, *clusterName)
		eviction := placementv1beta1.ClusterResourcePlacementEviction{}
		if err := drainCluster.hubClient.Get(ctx, types.NamespacedName{Name: evictionName}, &eviction); err != nil {
			log.Fatalf("failed to get eviction %s: %v", evictionName, err)
		}
		executedCondition := eviction.GetCondition(string(placementv1beta1.PlacementEvictionConditionTypeExecuted))
		if executedCondition == nil || executedCondition.Status == metav1.ConditionFalse {
			isDrainSuccessful = false
			log.Fatalf("eviction %s was not executed successfully for CRP %s", evictionName, crpName)
		}
		log.Printf("eviction %s was executed successfully for CRP %s", evictionName, crpName)
		// log each resource evicted by CRP.
		for i := range drainCluster.ClusterResourcePlacementResourcesMap[crpName] {
			resourceIdentifier := drainCluster.ClusterResourcePlacementResourcesMap[crpName][i]
			log.Printf("evicted resource %s propagated by CRP %s", fmt.Sprintf(resourceIdentifierKeyFormat, resourceIdentifier.Group, resourceIdentifier.Version, resourceIdentifier.Kind, resourceIdentifier.Name, resourceIdentifier.Namespace), crpName)
		}
	}

	if isDrainSuccessful {
		log.Printf("drain was successful for cluster %s", *clusterName)
	} else {
		log.Printf("drain was not successful for cluster %s, some eviction were not successfully executed", *clusterName)
	}
}

func (d *DrainCluster) cordonCluster() error {
	// add taint to member cluster to ensure resources aren't scheduled on it.
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mc clusterv1beta1.MemberCluster
		if err := d.hubClient.Get(ctx, types.NamespacedName{Name: d.ClusterName}, &mc); err != nil {
			return err
		}
		cordonTaint := clusterv1beta1.Taint{
			Key:    "cordon-key",
			Value:  "cordon-value",
			Effect: "NoSchedule",
		}

		// search to see cordonTaint already exists on the cluster.
		for i := range mc.Spec.Taints {
			if mc.Spec.Taints[i].Key == cordonTaint.Key {
				return nil
			}
		}

		// add taint to member cluster to cordon.
		mc.Spec.Taints = append(mc.Spec.Taints, cordonTaint)

		return d.hubClient.Update(ctx, &mc)
	})
}

func resourcePropagatedByCRP(selectedResources []placementv1beta1.ResourceIdentifier, failedPlacements []placementv1beta1.FailedResourcePlacement) []placementv1beta1.ResourceIdentifier {
	selectedResourcesMap := make(map[string]placementv1beta1.ResourceIdentifier, len(selectedResources))
	for i := range selectedResources {
		selectedResourcesMap[generateResourceIdentifierKey(selectedResources[i])] = selectedResources[i]
	}
	for i := range failedPlacements {
		if failedPlacements[i].Condition.Type == placementv1beta1.WorkConditionTypeApplied {
			delete(selectedResourcesMap, generateResourceIdentifierKey(failedPlacements[i].ResourceIdentifier))
		}
	}
	resourcesPropagated := make([]placementv1beta1.ResourceIdentifier, len(selectedResourcesMap))
	i := 0
	for key := range selectedResourcesMap {
		resourcesPropagated[i] = selectedResourcesMap[key]
		i++
	}
	return resourcesPropagated
}

func generateResourceIdentifierKey(resourceIdentifier placementv1beta1.ResourceIdentifier) string {
	return fmt.Sprintf(resourceIdentifierKeyFormat, resourceIdentifier.Group, resourceIdentifier.Version, resourceIdentifier.Kind, resourceIdentifier.Name, resourceIdentifier.Namespace)
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
