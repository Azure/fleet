/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package secret

import (
	"context"
	"fmt"
	"go.goms.io/fleet/pkg/authtoken"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	tokenKey = "token"
)

type secretAuthTokenProvider struct {
	client          client.Client
	secretName      string
	secretNamespace string
}

func New(secretName, namespace string) (authtoken.AuthTokenProvider, error) {
	client, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("an error occurred will creating client: %w", err)
	}
	return &secretAuthTokenProvider{
		client:          client,
		secretName:      secretName,
		secretNamespace: namespace,
	}, nil
}

func (s *secretAuthTokenProvider) FetchToken(ctx context.Context) (authtoken.AuthToken, error) {
	klog.V(2).InfoS("fetching token from secret", "secret", klog.KRef(s.secretName, s.secretNamespace))
	token := authtoken.AuthToken{}
	secret, err := s.fetchSecret(ctx)
	if err != nil {
		return token, fmt.Errorf("cannot get the secret: %w", err)
	}

	if len(secret.Data[tokenKey]) == 0 {
		return token, fmt.Errorf("the token data is missing or empty in secret %s", secret.Name)
	}

	token.Token = string(secret.Data[tokenKey])
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

func getClient() (client.Client, error) {
	restConfig := ctrl.GetConfigOrDie()

	client, err := client.New(restConfig, client.Options{})
	if err != nil {
		klog.Error(err, "unable to create client")
		return nil, err
	}
	return client, nil
}
