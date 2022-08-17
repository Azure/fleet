package framework

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetDeployment(cluster *Cluster, name, namespace string) (v1.Deployment, error) {
	deployment, err := cluster.KubeClientSet.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
	return *deployment, err
}

func WaitDeploymentCreated(cluster *Cluster, name, namespace string) {
	ginkgo.By(fmt.Sprintf("Waiting for Deployment %s/%s to be created on cluster %s", namespace, name, cluster.ClusterName))
	gomega.Eventually(func() error {
		_, err := GetDeployment(cluster, name, namespace)
		return err
	}, timeout, interval).Should(gomega.BeNil())
}
