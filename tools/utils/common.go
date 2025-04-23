/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
)

var (
	kubeConfigPath = os.Getenv("KUBECONFIG")
	CordonTaint    = clusterv1beta1.Taint{
		Key:    "cordon-key",
		Value:  "cordon-value",
		Effect: "NoSchedule",
	}
)

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
