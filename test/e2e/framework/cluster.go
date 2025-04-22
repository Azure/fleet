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

	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider/azure/trackers"
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
	PricingProvider                          trackers.PricingProvider
}

func NewCluster(name, svcAccountName string, scheme *runtime.Scheme, pp trackers.PricingProvider) *Cluster {
	return &Cluster{
		Scheme:                                   scheme,
		ClusterName:                              name,
		PresentingServiceAccountInHubClusterName: svcAccountName,
		PricingProvider:                          pp,
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

	httpClient, err := rest.HTTPClientFor(restConfig)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up HTTP client")

	cluster.RestMapper, err = apiutil.NewDynamicRESTMapper(restConfig, httpClient)
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
