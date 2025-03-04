package utils

import (
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	kubeConfigPath = os.Getenv("KUBECONFIG")
)

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
