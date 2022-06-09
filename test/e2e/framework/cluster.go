/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package framework

import (
	"os"

	"github.com/onsi/gomega"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfigPath = os.Getenv("KUBECONFIG")
)

type Cluster struct {
	KubeClient       *kubernetes.Clientset
	DynamicClientSet dynamic.Interface
	ClusterName      string
}

func NewCluster(name string) *Cluster {
	return &Cluster{
		ClusterName: name,
	}
}

// GetClusterClient returns a Cluster client for the cluster.
func GetClusterClient(cluster *Cluster) {
	clusterConfig := GetClientConfig()

	restConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	}

	if restConfig != nil {
		cluster.KubeClient = kubernetes.NewForConfigOrDie(restConfig)
	} else {
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	}
}

// GetClusterDynamicClient returns a DynamicCluster dynamic client for the cluster.
func GetClusterDynamicClient(cluster *Cluster) {
	clusterConfig := GetClientConfig()

	restConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	}

	if restConfig != nil {
		cluster.DynamicClientSet = dynamic.NewForConfigOrDie(restConfig)
	} else {
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	}
}

func GetClientConfig() clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: "",
		})
}
