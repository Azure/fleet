package secret

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/pkg/configprovider"
	"go.goms.io/fleet/pkg/interfaces"
)

var (
	tokenDataKey = "token"
)

type secretProvider struct {
	client.Client
	SecretName string
	Namespace  string
}

func NewFactory() interfaces.AuthenticationFactory {
	return &secretProvider{
		SecretName: os.Getenv("SECRET_NAME"),
		Namespace:  os.Getenv("SECRET_NAMESPACE"),
	}
}

func (s *secretProvider) RefreshToken(ctx context.Context, tokenFile string) (*time.Time, error) {
	if s.SecretName == "" {
		return nil, errors.New("secret name cannot be empty")
	}
	if s.Namespace == "" {
		return nil, errors.New("namespace for the token secret cannot be empty")
	}
	secret := s.getSecretObj(ctx)

	token := string(secret.Data[tokenDataKey])
	if len(token) == 0 {
		t := time.Now()
		return &t, fmt.Errorf("the token data is missing or empty in secret %s", s.SecretName)
	}

	klog.Info("writing new token to the file")
	err := configprovider.WriteTokenToFile(tokenFile, secret.Data[tokenDataKey])

	if err != nil {
		t := time.Now()
		return &t, errors.Wrapf(err, "an error occurred while writing new token to a file")
	}
	//add 24 hours to the currentTime
	newExpiry := (time.Now()).Local().Add(24)

	return &newExpiry, nil
}

func (s *secretProvider) getSecretObj(ctx context.Context) *corev1.Secret {
	obj := &corev1.Secret{}

	err := s.Client.Get(ctx, types.NamespacedName{
		Namespace: s.Namespace,
		Name:      s.SecretName,
	}, obj)

	if err != nil {
		klog.Errorf("cannot get secret, name: %s, namespace: %s", s.SecretName, s.Namespace)
		return nil
	}
	return obj
}
