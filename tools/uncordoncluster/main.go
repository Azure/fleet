/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"flag"
	"log"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	toolsutils "go.goms.io/fleet/tools/utils"
)

type UncordonCluster struct {
	HubClient   client.Client
	ClusterName string
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

	uncordonCluster := UncordonCluster{
		HubClient:   hubClient,
		ClusterName: *clusterName,
	}

	if err = uncordonCluster.uncordon(ctx); err != nil {
		log.Fatalf("failed to uncordon cluster %s: %v", *clusterName, err)
	}

	log.Printf("uncordoned member cluster %s", *clusterName)
}

// Uncordon removes the taint from the member cluster.
func (u *UncordonCluster) uncordon(ctx context.Context) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mc clusterv1beta1.MemberCluster
		if err := u.HubClient.Get(ctx, types.NamespacedName{Name: u.ClusterName}, &mc); err != nil {
			return err
		}

		if len(mc.Spec.Taints) == 0 {
			return nil
		}

		// remove cordon taint from member cluster.
		cordonTaintIndex := -1
		for i := range mc.Spec.Taints {
			taint := mc.Spec.Taints[i]
			if taint == toolsutils.CordonTaint {
				cordonTaintIndex = i
				break
			}
		}

		if cordonTaintIndex >= 0 {
			mc.Spec.Taints = append(mc.Spec.Taints[:cordonTaintIndex], mc.Spec.Taints[cordonTaintIndex+1:]...)
		} else {
			return nil
		}

		return u.HubClient.Update(ctx, &mc)
	})
}
