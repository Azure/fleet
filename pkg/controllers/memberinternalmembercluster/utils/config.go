package utils

import (
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

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
