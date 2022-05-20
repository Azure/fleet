/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	NamespaceNameFormat   = "fleet-%s"
	RoleNameFormat        = "fleet-role-%s"
	RoleBindingNameFormat = "fleet-rolebinding-%s"
)

// GetConfigWithSecret gets the cluster config from kubernetes secret
func GetConfigWithSecret(secret v1.Secret) (rest.Config, error) {
	kubeConfig, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeConfig) == 0 {
		return rest.Config{}, errors.New("secret does not contain kubeconfig")
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeConfig)
	if err != nil {
		return rest.Config{}, err
	}

	return *(restConfig), nil
}

func GetKubeConfigOfCurrentCluster() (rest.Config, error) {
	var kubeConfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeConfig = flag.String("kubeConfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeConfig file")
	} else {
		kubeConfig = flag.String("kubeConfig", "", "absolute path to the kubeConfig file")
	}
	flag.Parse()
	cf, err := clientcmd.BuildConfigFromFlags("", *(kubeConfig))
	return *(cf), err
}

func GetEventWatcherForCurrentCluster(ctx context.Context, namespace string) (watch.Interface, error) {
	config, err := GetKubeConfigOfCurrentCluster()
	if err != nil {
		log.Fatal(err.Error())
	}
	clientSet, err := kubernetes.NewForConfig(&config)
	if err != nil {
		log.Fatal(err.Error())
	}

	return clientSet.CoreV1().Events(namespace).Watch(ctx, metav1.ListOptions{})
}
