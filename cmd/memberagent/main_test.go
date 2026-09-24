package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/rest"

	"github.com/kubefleet-dev/kubefleet/cmd/memberagent/options"
)

func Test_buildHubConfig(t *testing.T) {
	t.Run("use CA auth, no key file - error", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: true, UseInsecureTLSClient: false})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use CA auth, no cert file - error", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: true, UseInsecureTLSClient: false})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use CA auth  - success", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: true, UseInsecureTLSClient: false})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, rest.Config{
			Host: "https://hub.domain.com",
			TLSClientConfig: rest.TLSClientConfig{
				KeyFile:  "/path/to/key",
				CertFile: "/path/to/cert",
			},
		}, *config)
	})
	t.Run("empty CA bundle - error", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		t.Setenv("CA_BUNDLE", "")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: true, UseInsecureTLSClient: false})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use CA bundle - success", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		t.Setenv("CA_BUNDLE", "/path/to/ca/bundle")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: true, UseInsecureTLSClient: false})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, rest.Config{
			Host: "https://hub.domain.com",
			TLSClientConfig: rest.TLSClientConfig{
				KeyFile:  "/path/to/key",
				CertFile: "/path/to/cert",
				CAFile:   "/path/to/ca/bundle",
			},
		}, *config)
	})
	t.Run("use CA data - success", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "./testdata/token")
		t.Setenv("HUB_CERTIFICATE_AUTHORITY", "dGhpcyBpcyBhIGZha2UgY2E=")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: false, UseInsecureTLSClient: false})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, rest.Config{
			Host:            "https://hub.domain.com",
			BearerTokenFile: "./testdata/token",
			TLSClientConfig: rest.TLSClientConfig{
				CAData: []byte("this is a fake ca"),
			},
		}, *config)
	})
	t.Run("empty CA data - error", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "./testdata/token")
		t.Setenv("HUB_CERTIFICATE_AUTHORITY", "")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: false, UseInsecureTLSClient: false})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("both of CA bundle and CA data present - error", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "./testdata/token")
		t.Setenv("HUB_CERTIFICATE_AUTHORITY", "dGhpcyBpcyBhIGZha2UgY2E=")
		t.Setenv("CA_BUNDLE", "/path/to/ca/bundle")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: false, UseInsecureTLSClient: false})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use token auth, no token path - error", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: false, UseInsecureTLSClient: false})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use token auth, not exists token path - error", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "/hot/exists/token/path")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: false, UseInsecureTLSClient: false})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use token auth - success", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "./testdata/token")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: false, UseInsecureTLSClient: false})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, rest.Config{
			Host:            "https://hub.domain.com",
			BearerTokenFile: "./testdata/token",
		}, *config)
	})
	t.Run("No CA bundle, no Hub CA, not insecure - success", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "./testdata/token")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: false, UseInsecureTLSClient: false})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, rest.Config{
			Host:            "https://hub.domain.com",
			BearerTokenFile: "./testdata/token",
		}, *config)
	})
	t.Run("use insecure client - success", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "./testdata/token")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: false, UseInsecureTLSClient: true})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, rest.Config{
			Host:            "https://hub.domain.com",
			BearerTokenFile: "./testdata/token",
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: true,
			},
		}, *config)
	})
	t.Run("use insecure client and custom header - success", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "./testdata/token")
		t.Setenv("HUB_KUBE_HEADER", "Member-Resource-ID: some-id")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: false, UseInsecureTLSClient: true})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.NotNil(t, config.WrapTransport)
	})
	t.Run("use CA auth and insecure client - success", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: true, UseInsecureTLSClient: true})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, rest.Config{
			Host: "https://hub.domain.com",
			TLSClientConfig: rest.TLSClientConfig{
				KeyFile:  "/path/to/key",
				CertFile: "/path/to/cert",
				Insecure: true,
			},
		}, *config)
	})
	t.Run("use CA auth and CA data - success", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		t.Setenv("HUB_CERTIFICATE_AUTHORITY", "dGhpcyBpcyBhIGZha2UgY2E=")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: true, UseInsecureTLSClient: false})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, rest.Config{
			Host: "https://hub.domain.com",
			TLSClientConfig: rest.TLSClientConfig{
				KeyFile:  "/path/to/key",
				CertFile: "/path/to/cert",
				CAData:   []byte("this is a fake ca"),
			},
		}, *config)
	})
	t.Run("use CA auth, empty CA data - error", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		t.Setenv("HUB_CERTIFICATE_AUTHORITY", "")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: true, UseInsecureTLSClient: false})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use CA auth, both of CA bundle and CA data present - error", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		t.Setenv("HUB_CERTIFICATE_AUTHORITY", "dGhpcyBpcyBhIGZha2UgY2E=")
		t.Setenv("CA_BUNDLE", "/path/to/ca/bundle")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: true, UseInsecureTLSClient: false})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use CA auth with custom header - success", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		t.Setenv("HUB_KUBE_HEADER", "Member-Resource-ID: some-id")
		config, err := buildHubConfig("https://hub.domain.com", options.HubConnectivityOptions{UseCertificateAuth: true, UseInsecureTLSClient: false})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.NotNil(t, config.WrapTransport)
	})
	t.Run("use hub kubeconfig - success", func(t *testing.T) {
		// hubURL is deliberately left empty here, mirroring main()'s behavior of skipping the
		// HUB_SERVER_URL read entirely when UseKubeConfig is set: the kubeconfig's own
		// cluster server URL is authoritative.
		t.Setenv("KUBE_CONFIG_PATH", "./testdata/kubeconfig")
		config, err := buildHubConfig("", options.HubConnectivityOptions{
			UseKubeConfig: true,
		})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, "https://hub.fixture.example.com", config.Host)
		assert.Equal(t, "fixture-bearer-token", config.BearerToken)
	})
	t.Run("use hub kubeconfig with an exec credential plugin - success", func(t *testing.T) {
		// Exercises the users[].user.exec flow this feature exists for (a federated-identity
		// exec plugin, e.g. kubelogin/aws-iam-authenticator/gke-gcloud-auth-plugin), as opposed
		// to the plain-bearer-token fixture above. clientcmd populates rest.Config.ExecProvider
		// from the exec stanza but does not invoke the plugin binary at ClientConfig() time -
		// that only happens lazily on the first real HTTP request - so this can assert on the
		// parsed exec configuration without needing "kubelogin" to actually be on PATH.
		t.Setenv("KUBE_CONFIG_PATH", "./testdata/kubeconfig-exec")
		config, err := buildHubConfig("", options.HubConnectivityOptions{
			UseKubeConfig: true,
		})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, "https://hub.fixture.example.com", config.Host)
		if assert.NotNil(t, config.ExecProvider) {
			assert.Equal(t, "kubelogin", config.ExecProvider.Command)
			assert.Equal(t, []string{"get-token", "--server-id", "fixture-server-id", "--login", "workloadidentity"}, config.ExecProvider.Args)
			assert.Equal(t, "client.authentication.k8s.io/v1", config.ExecProvider.APIVersion)
		}
	})
	t.Run("use hub kubeconfig, no path - error", func(t *testing.T) {
		config, err := buildHubConfig("", options.HubConnectivityOptions{
			UseKubeConfig: true,
		})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use hub kubeconfig, not exists - error", func(t *testing.T) {
		t.Setenv("KUBE_CONFIG_PATH", "./testdata/does-not-exist")
		config, err := buildHubConfig("", options.HubConnectivityOptions{
			UseKubeConfig: true,
		})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use hub kubeconfig with custom header - success", func(t *testing.T) {
		t.Setenv("HUB_KUBE_HEADER", "Member-Resource-ID: some-id")
		t.Setenv("KUBE_CONFIG_PATH", "./testdata/kubeconfig")
		config, err := buildHubConfig("", options.HubConnectivityOptions{
			UseKubeConfig: true,
		})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.NotNil(t, config.WrapTransport)
	})
	t.Run("use hub kubeconfig, malformed content - error", func(t *testing.T) {
		t.Setenv("KUBE_CONFIG_PATH", "./testdata/kubeconfig-malformed")
		config, err := buildHubConfig("", options.HubConnectivityOptions{
			UseKubeConfig: true,
		})
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use hub kubeconfig, hubURL argument is ignored - success", func(t *testing.T) {
		// The kubeconfig's own cluster server URL is authoritative; a non-empty hubURL passed
		// in (which main() only does when UseKubeConfig is unset) must not leak through.
		t.Setenv("KUBE_CONFIG_PATH", "./testdata/kubeconfig")
		config, err := buildHubConfig("https://should-be-ignored.example.com", options.HubConnectivityOptions{
			UseKubeConfig: true,
		})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, "https://hub.fixture.example.com", config.Host)
	})
	t.Run("use hub kubeconfig, TLSClientConfig.Insecure from kubeconfig is not overridden - success", func(t *testing.T) {
		// The fixture kubeconfig sets insecure-skip-tls-verify: true; UseInsecureTLSClient is
		// left at its false zero-value here to confirm the kubeconfig's own setting wins,
		// rather than being reset by the (ignored, per hub.go's doc comment) opts field.
		t.Setenv("KUBE_CONFIG_PATH", "./testdata/kubeconfig")
		config, err := buildHubConfig("", options.HubConnectivityOptions{
			UseKubeConfig:        true,
			UseInsecureTLSClient: false,
		})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.True(t, config.TLSClientConfig.Insecure)
	})
	t.Run("use hub kubeconfig takes precedence over CA auth - success", func(t *testing.T) {
		// options.Validate() rejects UseCertificateAuth+UseKubeConfig together in practice, but
		// buildHubConfig itself does not re-validate; this locks in that UseKubeConfig, checked
		// first in the if/else chain, wins if it is ever called with both set.
		t.Setenv("KUBE_CONFIG_PATH", "./testdata/kubeconfig")
		config, err := buildHubConfig("", options.HubConnectivityOptions{
			UseKubeConfig:      true,
			UseCertificateAuth: true,
		})
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, "https://hub.fixture.example.com", config.Host)
		assert.Empty(t, config.TLSClientConfig.CertFile)
		assert.Empty(t, config.TLSClientConfig.KeyFile)
	})
}
