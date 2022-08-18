package framework

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetPod(cluster *Cluster, name, namespace string) (v1.Pod, error) {
	pod, err := cluster.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
	return *pod, err
}

func WaitPodCreated(cluster *Cluster, name, namespace string) {
	ginkgo.By(fmt.Sprintf("Waiting for Pod %s/%s to be created on cluster %s", namespace, name, cluster.ClusterName))
	gomega.Eventually(func() error {
		_, err := GetPod(cluster, name, namespace)
		return err
	}, timeout, interval).Should(gomega.BeNil())
}
