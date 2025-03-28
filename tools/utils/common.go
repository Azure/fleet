/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"context"
	"os"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	kubeConfigPath = os.Getenv("KUBECONFIG")
	CordonTaint    = clusterv1beta1.Taint{
		Key:    "cordon-key",
		Value:  "cordon-value",
		Effect: "NoSchedule",
	}
)

type UncordonCluster struct {
	HubClient   client.Client
	ClusterName string
}

// Uncordon removes the taint from the member cluster.
func (u *UncordonCluster) Uncordon(ctx context.Context) error {
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
			if taint == CordonTaint {
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

// GetClusterClientFromClusterContext creates a new client.Client for the given cluster context and scheme.
func GetClusterClientFromClusterContext(clusterContext string, scheme *runtime.Scheme) (client.Client, error) {
	clusterConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: clusterContext,
		})

	restConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	hubClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	return hubClient, err
}
