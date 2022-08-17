package framework

import (
	"context"
	"fmt"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetConfigMap(cluster *Cluster, name, namespace string) (*v1.ConfigMap, error) {
	configMap, err := cluster.KubeClientSet.CoreV1().ConfigMaps(namespace).Get(context.Background(), name, metav1.GetOptions{})
	return configMap, err
}

func WaitConfigMapCreated(cluster *Cluster, name, namespace string) {
	ginkgo.By(fmt.Sprintf("Waiting for ConfigMap %s/%s to be created on cluster %s", namespace, name, cluster.ClusterName))
	gomega.Eventually(func() error {
		_, err := GetConfigMap(cluster, name, namespace)
		return err
	}, timeout, interval).Should(gomega.BeNil())
}

func WaitConfigMapUpdated(cluster *Cluster, name, namespace string, newData map[string]string) {
	ginkgo.By(fmt.Sprintf("Waiting for ConfigMap %s/%s to be updated on cluster %s", namespace, name, cluster.ClusterName))
	gomega.Eventually(func() bool {
		cm, err := GetConfigMap(cluster, name, namespace)
		if err != nil {
			return false
		}
		if len(cm.Data) != len(newData) {
			return false
		}
		for k, v := range newData {
			if cm.Data[k] != v {
				return false
			}
		}
		return true
	}, timeout, interval).Should(gomega.BeNil())
}
