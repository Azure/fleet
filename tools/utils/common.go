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
)

type UncordonCluster struct {
	HubClient   client.Client
	ClusterName string
}

func (u *UncordonCluster) Uncordon(ctx context.Context) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mc clusterv1beta1.MemberCluster
		if err := u.HubClient.Get(ctx, types.NamespacedName{Name: u.ClusterName}, &mc); err != nil {
			return err
		}

		if mc.Spec.Taints == nil {
			return nil
		}

		// remove all taints from member cluster.
		mc.Spec.Taints = nil

		return u.HubClient.Update(ctx, &mc)
	})
}

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
