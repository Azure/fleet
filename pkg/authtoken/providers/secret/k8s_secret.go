/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package secret

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/pkg/interfaces"
)

var (
	kubeconfigKey = "kubeconfig"
)

type secretAuthTokenProvider struct {
	client          client.Client
	secretName      string
	secretNamespace string
}

func New(secretName, namespace string) interfaces.AuthTokenProvider {
	return &secretAuthTokenProvider{
		client:          getClient(),
		secretName:      secretName,
		secretNamespace: namespace,
	}
}

func (s *secretAuthTokenProvider) FetchToken(ctx context.Context) (interfaces.AuthToken, error) {
	token := interfaces.AuthToken{}
	secret, err := s.fetchSecret(ctx)
	if err != nil {
		return token, errors.Wrapf(err, "cannot get secret, name: %s, namespace: %s", s.secretName, s.secretName)
	}

	if len(secret.Data[kubeconfigKey]) == 0 {
		return token, fmt.Errorf("the token data is missing or empty in secret %s", secret.Name)
	}

	token.Token = string(secret.Data[kubeconfigKey])
	//add 24 hours to the currentTime
	token.ExpiresOn = (time.Now()).Local().Add(24 * time.Hour)

	return token, nil
}

func (s *secretAuthTokenProvider) fetchSecret(ctx context.Context) (*corev1.Secret, error) {
	secret := corev1.Secret{}
	err := retry.OnError(retry.DefaultBackoff,
		func(err error) bool {
			return ctx.Err() == nil
		}, func() error {
			return s.client.Get(ctx, types.NamespacedName{
				Name:      s.secretName,
				Namespace: s.secretNamespace,
			}, &secret)
		})

	return &secret, err
}

func getClient() client.Client {
	restConfig := ctrl.GetConfigOrDie()

	client, err := client.New(restConfig, client.Options{})
	if err != nil {
		klog.Error(err, "unable to create client")
		os.Exit(1)
	}

	return client
}
