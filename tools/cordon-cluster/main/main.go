package main

import (
	"context"
	"flag"
	"log"
	"os"

	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var (
	scheme         = runtime.NewScheme()
	ctx            = context.Background()
	kubeconfigPath = os.Getenv("KUBECONFIG")
)

func main() {
	clusterName := flag.String("clusterName", "", "name of the cluster to cordon")
	flag.Parse()

	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add custom APIs (cluster) to the runtime scheme: %v", err)
	}
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add custom APIs (placement) to the runtime scheme: %v", err)
	}

	clusterConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: "kind-hub",
		})

	restConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		log.Fatalf("failed to set up rest config: %v", err)
	}

	hubClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("failed to hub cluster client")
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

	var crbList placementv1beta1.ClusterResourceBindingList
	if err := hubClient.List(ctx, &crbList); err != nil {
		log.Fatalf("failed to list cluster resource bindings: %v", err)
	}

	// find all unique CRP names for which eviction need to occur.
	crpNameMap := make(map[string]bool)
	for i := range crbList.Items {
		if crbList.Items[i].Spec.TargetCluster == *clusterName {
			if !crpNameMap[crbList.Items[i].Labels[placementv1beta1.CRPTrackingLabel]] {
				crpNameMap[crbList.Items[i].Labels[placementv1beta1.CRPTrackingLabel]] = true
			}
		}
	}

	if len(crpNameMap) == 0 {
		log.Fatalf("failed to find any CRP which has propagated resource to cluster %s", *clusterName)
	}

	// retry until update succeeds or non conflict error occurs.
	for {
		err := updateMemberClusterWithTaint(ctx, hubClient, *clusterName)
		if err == nil || !k8sErrors.IsConflict(err) {
			break
		}
	}

	// create eviction objects for all <crpName, targetCluster>.
	for crpName, _ := range crpNameMap {
		eviction := placementv1beta1.ClusterResourcePlacementEviction{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-eviction" + crpName,
			},
			Spec: placementv1beta1.PlacementEvictionSpec{
				PlacementName: crpName,
				ClusterName:   *clusterName,
			},
		}
		if err := hubClient.Create(ctx, &eviction); err != nil {
			log.Fatalf("failed to create CRP eviction for CRP %s", crpName)
		}
	}
}

func updateMemberClusterWithTaint(ctx context.Context, hubClient client.Client, mcName string) error {
	var mc clusterv1beta1.MemberCluster
	if err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc); err != nil {
		return err
	}

	// add taint to member cluster to cordon.
	mc.Spec.Taints = []clusterv1beta1.Taint{
		{
			Key:    "cordon-key",
			Value:  "cordon-value",
			Effect: "NoSchedule",
		},
	}

	return hubClient.Update(ctx, &mc)
}
