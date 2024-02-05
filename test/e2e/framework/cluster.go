/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"os"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

var (
	kubeconfigPath = os.Getenv("KUBECONFIG")
)

// Cluster object defines the required clients based on the kubeconfig of the test cluster.
type Cluster struct {
	Scheme                                   *runtime.Scheme
	KubeClient                               client.Client
	ImpersonateKubeClient                    client.Client
	DynamicClient                            dynamic.Interface
	ClusterName                              string
	PresentingServiceAccountInHubClusterName string
	HubURL                                   string
	RestMapper                               meta.RESTMapper
}

func NewCluster(name, svcAccountName string, scheme *runtime.Scheme) *Cluster {
	return &Cluster{
		Scheme:                                   scheme,
		ClusterName:                              name,
		PresentingServiceAccountInHubClusterName: svcAccountName,
	}
}

// GetClusterClient returns a Cluster client for the cluster.
func GetClusterClient(cluster *Cluster) {
	clusterConfig := GetClientConfig(cluster)
	impersonateClusterConfig := GetImpersonateClientConfig(cluster)

	restConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up rest config")
	}

	impersonateRestConfig, err := impersonateClusterConfig.ClientConfig()
	if err != nil {
		gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up impersonate rest config")
	}

	cluster.KubeClient, err = client.New(restConfig, client.Options{Scheme: cluster.Scheme})
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Kube Client")

	cluster.DynamicClient, err = dynamic.NewForConfig(restConfig)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Dynamic Client")

	restHTTPClient, err := rest.HTTPClientFor(restConfig)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up REST HTTP client")

	cluster.RestMapper, err = apiutil.NewDynamicRESTMapper(restConfig, restHTTPClient)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Rest Mapper")

	cluster.ImpersonateKubeClient, err = client.New(impersonateRestConfig, client.Options{Scheme: cluster.Scheme})
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Impersonate Kube Client")
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
			AuthInfo: api.AuthInfo{
				Impersonate:       "test-user",
				ImpersonateGroups: []string{"system:authenticated"},
			},
		})
}
