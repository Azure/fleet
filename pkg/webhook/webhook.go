/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	admv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/cmd/hubagent/options"
	"go.goms.io/fleet/pkg/webhook/clusterresourceplacement"
	"go.goms.io/fleet/pkg/webhook/pod"
	"go.goms.io/fleet/pkg/webhook/replicaset"
)

const (
	FleetWebhookCertFileName = "tls.crt"
	FleetWebhookKeyFileName  = "tls.key"
	FleetWebhookCfgName      = "fleet-validating-webhook-configuration"
	FleetWebhookSvcName      = "fleetwebhook"
)

var AddToManagerFuncs []func(manager.Manager) error

// AddToManager adds all Controllers to the Manager
func AddToManager(m manager.Manager) error {
	for _, f := range AddToManagerFuncs {
		if err := f(m); err != nil {
			return err
		}
	}
	return nil
}

type Config struct {
	mgr manager.Manager

	// webhook server info
	serviceNamespace string
	servicePort      int32
	serviceURL       string

	// caPEM is a PEM encoded CA bundle which will be used to validate the webhook's server certificate.
	caPEM []byte

	clientConnectionType *options.WebhookClientConnectionType
}

func NewWebhookConfig(mgr manager.Manager, port int, clientConnectionType *options.WebhookClientConnectionType, certDir string) (*Config, error) {
	// We assume the Pod namespace should be passed to env through downward API in the Pod spec.
	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		return nil, errors.New("fail to obtain Pod namespace from POD_NAMESPACE")
	}
	w := Config{
		mgr:                  mgr,
		servicePort:          int32(port),
		serviceNamespace:     namespace,
		serviceURL:           fmt.Sprintf("https://%s.%s.svc.cluster.local:%d", FleetWebhookSvcName, namespace, port),
		clientConnectionType: clientConnectionType,
	}
	caPEM, err := w.genCertificate(certDir)
	if err != nil {
		return nil, err
	}
	w.caPEM = caPEM
	return &w, err
}

func (w *Config) Start(ctx context.Context) error {
	klog.V(2).InfoS("setting up webhooks in apiserver from the leader")
	if err := w.createFleetWebhookConfiguration(ctx); err != nil {
		klog.ErrorS(err, "unable to setup webhook configurations in apiserver")
		return err
	}
	return nil
}

// createFleetWebhookConfiguration creates the ValidatingWebhookConfiguration object for the webhook.
func (w *Config) createFleetWebhookConfiguration(ctx context.Context) error {
	failPolicy := admv1.Fail
	sideEffortsNone := admv1.SideEffectClassNone
	namespacedScope := admv1.NamespacedScope
	clusterScope := admv1.ClusterScope

	whCfg := admv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: FleetWebhookCfgName,
			Labels: map[string]string{
				"admissions.enforcer/disabled": "true",
			},
		},
		Webhooks: []admv1.ValidatingWebhook{
			{
				Name:                    "fleet.pod.validating",
				ClientConfig:            w.createClientConfig(corev1.Pod{}),
				FailurePolicy:           &failPolicy,
				SideEffects:             &sideEffortsNone,
				AdmissionReviewVersions: []string{"v1", "v1beta1"},

				Rules: []admv1.RuleWithOperations{
					{
						Operations: []admv1.OperationType{
							admv1.Create,
						},
						Rule: admv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
							Scope:       &namespacedScope,
						},
					},
				},
			},
			{
				Name:                    "fleet.clusterresourceplacement.validating",
				ClientConfig:            w.createClientConfig(fleetv1alpha1.ClusterResourcePlacement{}),
				FailurePolicy:           &failPolicy,
				SideEffects:             &sideEffortsNone,
				AdmissionReviewVersions: []string{"v1", "v1beta1"},

				Rules: []admv1.RuleWithOperations{
					{
						Operations: []admv1.OperationType{
							admv1.Create,
							admv1.Update,
						},
						Rule: admv1.Rule{
							APIGroups:   []string{"fleet.azure.com"},
							APIVersions: []string{"v1alpha1"},
							Resources:   []string{fleetv1alpha1.ClusterResourcePlacementResource},
							Scope:       &clusterScope,
						},
					},
				},
			},
			{
				Name:                    "fleet.replicaset.validating",
				ClientConfig:            w.createClientConfig(appsv1.ReplicaSet{}),
				FailurePolicy:           &failPolicy,
				SideEffects:             &sideEffortsNone,
				AdmissionReviewVersions: []string{"v1", "v1beta1"},
				Rules: []admv1.RuleWithOperations{
					{
						Operations: []admv1.OperationType{
							admv1.Create,
						},
						Rule: admv1.Rule{
							APIGroups:   []string{"apps"},
							APIVersions: []string{"v1"},
							Resources:   []string{"replicasets"},
							Scope:       &namespacedScope,
						},
					},
				},
			},
		},
	}

	if err := w.mgr.GetClient().Create(ctx, &whCfg); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		klog.V(2).InfoS("validatingwebhookconfiguration exists, need to update", "name", FleetWebhookCfgName)
		// Here we simply use delete/create pattern to implement full overwrite
		err := w.mgr.GetClient().Delete(ctx, &whCfg)
		if err != nil {
			return err
		}
		err = w.mgr.GetClient().Create(ctx, &whCfg)
		if err != nil {
			return err
		}
		return nil
	}
	klog.V(2).InfoS("successfully created validatingwebhookconfiguration", "name", FleetWebhookCfgName)
	return nil
}

// createClientConfig generates the client configuration with either service ref or URL for the argued interface.
func (w *Config) createClientConfig(webhookInterface interface{}) admv1.WebhookClientConfig {
	serviceRef := admv1.ServiceReference{
		Namespace: w.serviceNamespace,
		Name:      FleetWebhookSvcName,
		Port:      pointer.Int32(w.servicePort),
	}
	var serviceEndpoint string
	switch webhookInterface.(type) {
	case corev1.Pod:
		serviceEndpoint = w.serviceURL + pod.ValidationPath
		serviceRef.Path = pointer.String(pod.ValidationPath)
	case fleetv1alpha1.ClusterResourcePlacement:
		serviceEndpoint = w.serviceURL + clusterresourceplacement.ValidationPath
		serviceRef.Path = pointer.String(clusterresourceplacement.ValidationPath)
	case appsv1.ReplicaSet:
		serviceEndpoint = w.serviceURL + replicaset.ValidationPath
		serviceRef.Path = pointer.String(replicaset.ValidationPath)
	}

	config := admv1.WebhookClientConfig{
		CABundle: w.caPEM,
	}
	switch *w.clientConnectionType {
	case options.Service:
		config.Service = &serviceRef
	case options.URL:
		config.URL = pointer.String(serviceEndpoint)
	}
	return config
}

// genCertificate generates the serving cerficiate for the webhook server.
func (w *Config) genCertificate(certDir string) ([]byte, error) {
	caPEM, certPEM, keyPEM, err := w.genSelfSignedCert()
	if err != nil {
		klog.ErrorS(err, "fail to generate self-signed cert")
		return nil, err
	}

	// generate certificate files (i.e., tls.crt and tls.key)
	if err := genCertAndKeyFile(certPEM, keyPEM, certDir); err != nil {
		klog.ErrorS(err, "fail to generate certificate and key files")
		return nil, err
	}
	return caPEM, nil
}

// genSelfSignedCert generates the self signed Certificate/Key pair
func (w *Config) genSelfSignedCert() (caPEMByte, certPEMByte, keyPEMByte []byte, err error) {
	// CA config
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(2022),
		Subject: pkix.Name{
			CommonName:         "fleet.azure.com",
			OrganizationalUnit: []string{"Azure Kubernetes Service"},
			Organization:       []string{"Microsoft"},
			Locality:           []string{"Redmond"},
			Province:           []string{"Washington"},
			Country:            []string{"United States of America"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // Set expiration time to be 10 years for now
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// CA private key
	caPrvKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, nil, err
	}

	// Self signed CA certificate
	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrvKey.PublicKey, caPrvKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// PEM encode CA cert
	caPEM := new(bytes.Buffer)
	if err := pem.Encode(caPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	}); err != nil {
		return nil, nil, nil, err
	}
	caPEMByte = caPEM.Bytes()

	dnsNames := []string{
		fmt.Sprintf("%s.%s.svc", FleetWebhookSvcName, w.serviceNamespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", FleetWebhookSvcName, w.serviceNamespace),
	}
	// server cert config
	cert := &x509.Certificate{
		DNSNames:     dnsNames,
		SerialNumber: big.NewInt(2022),
		Subject: pkix.Name{
			CommonName:         fmt.Sprintf("%s.cert.server", FleetWebhookSvcName),
			OrganizationalUnit: []string{"Azure Kubernetes Service"},
			Organization:       []string{"Microsoft"},
			Locality:           []string{"Redmond"},
			Province:           []string{"Washington"},
			Country:            []string{"United States of America"},
		},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		SubjectKeyId: []byte{1, 2, 3, 4, 5},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	// server private key
	certPrvKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, nil, err
	}

	// sign the server cert
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &certPrvKey.PublicKey, caPrvKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// PEM encode the  server cert and key
	certPEM := new(bytes.Buffer)
	if err := pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}); err != nil {
		return nil, nil, nil, err
	}
	certPEMByte = certPEM.Bytes()

	certPrvKeyPEM := new(bytes.Buffer)
	if err := pem.Encode(certPrvKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certPrvKey),
	}); err != nil {
		return nil, nil, nil, err
	}
	keyPEMByte = certPrvKeyPEM.Bytes()
	return caPEMByte, certPEMByte, keyPEMByte, nil
}

// genCertAndKeyFile creates the serving certificate/key files for the webhook server
func genCertAndKeyFile(certData, keyData []byte, certDir string) error {
	// always remove first
	if err := os.RemoveAll(certDir); err != nil {
		return fmt.Errorf("fail to remove certificates: %w", err)
	}
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return fmt.Errorf("could not create directory %q to store certificates: %w", certDir, err)
	}
	certPath := filepath.Join(certDir, FleetWebhookCertFileName)
	f, err := os.OpenFile(filepath.Clean(certPath), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("could not open %q: %w", certPath, err)
	}
	defer f.Close()
	certBlock, _ := pem.Decode(certData)
	if certBlock == nil {
		return fmt.Errorf("invalid certificate data")
	}
	if err := pem.Encode(f, certBlock); err != nil {
		return err
	}

	keyPath := filepath.Join(certDir, FleetWebhookKeyFileName)
	kf, err := os.OpenFile(filepath.Clean(keyPath), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("could not open %q: %w", keyPath, err)
	}

	keyBlock, _ := pem.Decode(keyData)
	if keyBlock == nil {
		return fmt.Errorf("invalid key data")
	}
	if err := pem.Encode(kf, keyBlock); err != nil {
		return err
	}
	klog.V(2).InfoS("successfully generate certificate and key files")
	return nil
}
