/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"log"

	"k8s.io/apimachinery/pkg/runtime"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	toolsutils "github.com/kubefleet-dev/kubefleet/tools/utils"
)

func main() {
	//TODO (arvindth): add flags for timeout, help for program.
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

	drainClusterHelper := helper{
		hubClient:   hubClient,
		clusterName: *clusterName,
	}

	isDrainSuccessful, err := drainClusterHelper.Drain(ctx)
	if err != nil {
		log.Fatalf("failed to drain member cluster %s: %v", drainClusterHelper.clusterName, err)
	}

	if isDrainSuccessful {
		log.Printf("drain was successful for cluster %s", *clusterName)
	} else {
		log.Printf("drain was not successful for cluster %s", *clusterName)
	}

	// TODO (arvindth): add a flag to specify timeout for retry to keep retrying.
	log.Printf("retrying drain to ensure all resources propagated from hub cluster are evicted")
	isDrainRetrySuccessful, err := drainClusterHelper.Drain(ctx)
	if err != nil {
		log.Fatalf("failed to drain cluster on retry %s: %v", drainClusterHelper.clusterName, err)
	}
	if isDrainRetrySuccessful {
		log.Printf("drain retry was successful for cluster %s", *clusterName)
	} else {
		log.Printf("drain retry was not successful for cluster %s", *clusterName)
	}

	log.Printf("reminder: uncordon the cluster %s to remove cordon taint if needed", *clusterName)
}
