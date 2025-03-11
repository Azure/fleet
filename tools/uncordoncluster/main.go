package main

import (
	"context"
	"flag"
	"log"

	"k8s.io/apimachinery/pkg/runtime"

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

	uncordonCluster := toolsutils.UncordonCluster{
		HubClient:   hubClient,
		ClusterName: *clusterName,
	}

	if err = uncordonCluster.Uncordon(ctx); err != nil {
		log.Fatalf("failed to uncordon cluster %s: %v", *clusterName, err)
	}

	log.Printf("uncordoned member cluster %s", *clusterName)
}
