package main

import (
	"context"
	"flag"
	"log"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	toolsutils "go.goms.io/fleet/tools/utils"
)

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

	var mc clusterv1beta1.MemberCluster
	if err = hubClient.Get(ctx, types.NamespacedName{Name: *clusterName}, &mc); err != nil {
		log.Fatalf("failed to get member cluster %s: %v", *clusterName, err)
	}

	// remove existing taints from member cluster.
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mc clusterv1beta1.MemberCluster
		if err := hubClient.Get(ctx, types.NamespacedName{Name: *clusterName}, &mc); err != nil {
			return err
		}

		if mc.Spec.Taints == nil {
			return nil
		}

		// remove all taints from member cluster.
		mc.Spec.Taints = nil

		return hubClient.Update(ctx, &mc)
	})

	if err != nil {
		log.Fatalf("failed to remove taints from member cluster %s: %v", *clusterName, err)
	}

	log.Printf("uncordoned member cluster %s", *clusterName)
}
