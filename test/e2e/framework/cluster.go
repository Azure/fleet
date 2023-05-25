/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package framework

import (
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"os"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

var (
	kubeconfigPath = os.Getenv("KUBECONFIG")
)

// Cluster object defines the required clients based on the kubeconfig of the test cluster.
type Cluster struct {
	Scheme                *runtime.Scheme
	KubeClient            client.Client
	ImpersonateKubeClient client.Client
	DynamicClient         dynamic.Interface
	ClusterName           string
	HubURL                string
	RestMapper            meta.RESTMapper
}

func NewCluster(name string, scheme *runtime.Scheme) *Cluster {
	return &Cluster{
		Scheme:      scheme,
		ClusterName: name,
	}
}

// GetClusterClient returns a Cluster client for the cluster.
func GetClusterClient(cluster *Cluster) {
	clusterConfig := GetClientConfig(cluster)
	impersonateClusterConfig := GetImpersonateClientConfig(cluster)

	validRestConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up valid rest config")
	}

	impersonateRestConfig, err := impersonateClusterConfig.ClientConfig()
	if err != nil {
		gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up impersonate rest config")
	}

	cluster.KubeClient, err = client.New(validRestConfig, client.Options{Scheme: cluster.Scheme})
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Kube Client")

	cluster.ImpersonateKubeClient, err = client.New(impersonateRestConfig, client.Options{Scheme: cluster.Scheme})
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Impersonate Kube Client")

	cluster.DynamicClient, err = dynamic.NewForConfig(validRestConfig)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Dynamic Client")

	cluster.RestMapper, err = apiutil.NewDynamicRESTMapper(validRestConfig, apiutil.WithLazyDiscovery)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Rest Mapper")
}

func GetClientConfig(cluster *Cluster) clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: cluster.ClusterName,
		})
}

func GetImpersonateClientConfig(cluster *Cluster) clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: cluster.ClusterName,
			AuthInfo: clientcmdapi.AuthInfo{
				Impersonate:       "bad-user",
				ImpersonateGroups: []string{"user:unauthenticated"},
			},
		})
}
