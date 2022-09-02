/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package framework

import (
	"os"

	"github.com/onsi/gomega"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

var (
	kubeconfigPath = os.Getenv("KUBECONFIG")
)

// Cluster object defines the required clients based on the kubeconfig of the test cluster.
type Cluster struct {
	Scheme             *runtime.Scheme
	KubeClient         client.Client
	KubeClientSet      kubernetes.Interface
	APIExtensionClient *clientset.Clientset
	DynamicClient      dynamic.Interface
	ClusterName        string
	HubURL             string
	RestMapper         meta.RESTMapper
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

	restConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up rest config")
	}

	cluster.KubeClient, err = client.New(restConfig, client.Options{Scheme: cluster.Scheme})
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Kube Client")

	cluster.KubeClientSet, err = kubernetes.NewForConfig(restConfig)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up KubeClient Set")

	cluster.APIExtensionClient, err = clientset.NewForConfig(restConfig)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up API Extension Client.")

	cluster.DynamicClient, err = dynamic.NewForConfig(restConfig)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Dynamic Client")

	cluster.RestMapper, err = apiutil.NewDynamicRESTMapper(restConfig, apiutil.WithLazyDiscovery)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Rest Mapper")
}

func GetClientConfig(cluster *Cluster) clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: cluster.ClusterName,
		})
}
