/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package framework

import (
	"os"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	kubeconfigPath = os.Getenv("KUBECONFIG")
)

type Cluster struct {
	Scheme           *runtime.Scheme
	KubeClient       client.Client
	DynamicClientSet dynamic.Interface
	ClusterName      string
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
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	}

	client, err := client.New(restConfig, client.Options{Scheme: cluster.Scheme})
	gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

	cluster.KubeClient = client
}

// GetClusterDynamicClient returns a DynamicCluster dynamic client for the cluster.
func GetClusterDynamicClient(cluster *Cluster) {
	clusterConfig := GetClientConfig(cluster)

	restConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	}

	cluster.DynamicClientSet = dynamic.NewForConfigOrDie(restConfig)
	gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
}

func GetClientConfig(cluster *Cluster) clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: cluster.ClusterName,
		})
}
