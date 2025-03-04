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
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
)

var (
	scheme         = runtime.NewScheme()
	ctx            = context.Background()
	kubeconfigPath = os.Getenv("KUBECONFIG")
)

const (
	testEvictionNameFormat = "test-eviction-%s-%s"
)

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

	clusterConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: *hubClusterContext,
		})

	restConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		log.Fatalf("failed to set up rest config: %v", err)
	}

	hubClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("failed to create hub cluster client")
	}

	var memberClusterList clusterv1beta1.MemberClusterList
	if err := hubClient.List(ctx, &memberClusterList); err != nil {
		log.Fatalf("failed to list member clusters: %v", err)
	}

	var memberCluster *clusterv1beta1.MemberCluster
	for i := range memberClusterList.Items {
		if memberClusterList.Items[i].Name == *clusterName {
			memberCluster = &memberClusterList.Items[i]
			break
		}
	}

	if memberCluster == nil {
		log.Fatalf("failed to find member cluster %s to cordon", *clusterName)
	}

	var crpList placementv1beta1.ClusterResourcePlacementList
	if err := hubClient.List(ctx, &crpList); err != nil {
		log.Fatalf("failed to list cluster resource placements: %v", err)
	}

	// find all unique CRP names for which eviction needs to occur.
	crpNameMap := make(map[string]bool)
	for i := range crpList.Items {
		for j := range crpList.Items[i].Status.PlacementStatuses {
			if crpList.Items[i].Status.PlacementStatuses[j].ClusterName == *clusterName {
				crpNameMap[crpList.Items[i].Name] = true
				break
			}
		}
	}

	if len(crpNameMap) == 0 {
		log.Fatalf("failed to find any CRP which has propagated resource to cluster %s", *clusterName)
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mc clusterv1beta1.MemberCluster
		if err := hubClient.Get(ctx, types.NamespacedName{Name: *clusterName}, &mc); err != nil {
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

		return hubClient.Update(ctx, &mc)
	})

	if err != nil {
		log.Fatalf("failed to update member cluster with taint: %v", err)
	}

	// create eviction objects for all <crpName, targetCluster>.
	for crpName := range crpNameMap {
		evictionName := fmt.Sprintf(testEvictionNameFormat, crpName, *clusterName)

		if err = removeExistingEviction(ctx, hubClient, evictionName); err != nil {
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
			return hubClient.Create(ctx, &eviction)
		})

		if err != nil {
			log.Fatalf("failed to create eviction for CRP %s: %v", crpName, err)
		}
	}

	// wait until all evictions reach a terminal state.
	// TODO: move isEvictionInTerminalState to a function in the utils package.
	for crpName := range crpNameMap {
		err = wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff, func(ctx context.Context) (bool, error) {
			evictionName := fmt.Sprintf(testEvictionNameFormat, crpName, *clusterName)
			eviction := placementv1beta1.ClusterResourcePlacementEviction{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: evictionName}, &eviction); err != nil {
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

	log.Printf("Issued evictions to cordon cluster %s, please verify to ensure evictions were successfully executed", *clusterName)
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
