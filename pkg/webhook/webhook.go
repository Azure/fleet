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
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	admv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	FleetWebhookCertFileName = "tls.crt"
	FleetWebhookKeyFileName  = "tls.key"
	FleetWebhookCfgName      = "fleet-validating-webhook-configuration"
)

var WebhookServiceNs string

func init() {
	// We assume the Pod namespace should be passed to env through downward API in the Pod spec
	WebhookServiceNs = os.Getenv("POD_NAMESPACE")
	if WebhookServiceNs == "" {
		panic("Fail to obtain Pod namespace from env")
	}
}

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

// CreateValidatingWebhookConfiguration creates the validatingwebhookconfiguration object for the webhook
func CreateFleetWebhookConfiguration(ctx context.Context, client client.Client, caPEM []byte, port int) error {
	failPolicy := admv1.Fail // reject request if the webhook doesn't work
	sideEffortsNone := admv1.SideEffectClassNone

	// We assume a headless service named fleetwebhook has been created in the pod namespace (e.g., via helm chart)
	podWebhookURL := fmt.Sprintf("https://fleetwebhook.%s.svc.cluster.local:%d/validate-v1-pod", WebhookServiceNs, port)
	scope := admv1.NamespacedScope
	whCfg := admv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: FleetWebhookCfgName,
			Labels: map[string]string{
				"fleet-webhook":                "true",
				"admissions.enforcer/disabled": "true",
			},
		},
		Webhooks: []admv1.ValidatingWebhook{
			{
				Name: "fleet.pod.validating",
				ClientConfig: admv1.WebhookClientConfig{
					URL:      &podWebhookURL,
					CABundle: caPEM,
				},
				FailurePolicy:           &failPolicy,
				SideEffects:             &sideEffortsNone,
				AdmissionReviewVersions: []string{"v1", "v1beta1"},

				Rules: []admv1.RuleWithOperations{
					{
						Operations: []admv1.OperationType{
							admv1.OperationAll,
						},
						Rule: admv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
							Scope:       &scope,
						},
					},
				},
			},
		},
	}

	if err := client.Create(ctx, &whCfg); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		klog.V(2).InfoS("validatingwebhookconfiguration exists, need to update", "name", FleetWebhookCfgName)
		// Here we simply use delete/create pattern instead of using update to avoid any retry due to update conflict
		err := client.Delete(context.TODO(), &whCfg)
		if err != nil {
			return err
		}
		err = client.Create(context.TODO(), &whCfg)
		if err != nil {
			return err
		}
		return nil
	}
	klog.V(2).InfoS("successfully created validatingwebhookconfiguration", "name", FleetWebhookCfgName)
	return nil
}

// GenCertificate generates the serving cerficiate for the webhook server
func GenCertificate(certDir string) ([]byte, error) {
	caPEM, certPEM, keyPEM, err := genSelfSignedCert()
	if err != nil {
		klog.V(2).ErrorS(err, "fail to generate self-signed certificate")
		return nil, err
	}

	// generate certificate files (i.e., tls.crt and tls.key)
	if err := genCertAndKeyFile(certPEM, keyPEM, certDir); err != nil {
		return nil, fmt.Errorf("fail to generate certificate and key: %w", err)
	}

	return caPEM, nil
}

// genSelfSignedCert generates the self signed Certificate/Key pair
func genSelfSignedCert() (caPEMByte, certPEMByte, keyPEMByte []byte, err error) {
	// CA config
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(2022),
		Subject: pkix.Name{
			Organization: []string{"fleet.azure.com"},
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
	_ = pem.Encode(caPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})
	caPEMByte = caPEM.Bytes()

	dnsNames := []string{
		fmt.Sprintf("fleetwebhook.%s.svc.cluster.local", WebhookServiceNs),
	}
	// server cert config
	cert := &x509.Certificate{
		DNSNames:     dnsNames,
		SerialNumber: big.NewInt(2022),
		Subject: pkix.Name{
			CommonName:   "fleetWebhookServer",
			Organization: []string{"fleet.azure.com"},
		},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
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
	_ = pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})
	certPEMByte = certPEM.Bytes()

	certPrvKeyPEM := new(bytes.Buffer)
	_ = pem.Encode(certPrvKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certPrvKey),
	})
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
