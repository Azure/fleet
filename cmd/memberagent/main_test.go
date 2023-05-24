package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/rest"
)

func Test_buildHubConfig(t *testing.T) {
	t.Run("use CA auth, no key file - error", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		config, err := buildHubConfig("https://hub.domain.com", true, false)
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use CA auth, no cert file - error", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "")
		config, err := buildHubConfig("https://hub.domain.com", true, false)
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use CA auth  - success", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		config, err := buildHubConfig("https://hub.domain.com", true, false)
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
		config, err := buildHubConfig("https://hub.domain.com", true, false)
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use CA bundle - success", func(t *testing.T) {
		t.Setenv("IDENTITY_KEY", "/path/to/key")
		t.Setenv("IDENTITY_CERT", "/path/to/cert")
		t.Setenv("CA_BUNDLE", "/path/to/ca/bundle")
		config, err := buildHubConfig("https://hub.domain.com", true, false)
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
		config, err := buildHubConfig("https://hub.domain.com", false, false)
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
		config, err := buildHubConfig("https://hub.domain.com", false, false)
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("both of CA bundle and CA data present - error", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "./testdata/token")
		t.Setenv("HUB_CERTIFICATE_AUTHORITY", "dGhpcyBpcyBhIGZha2UgY2E=")
		t.Setenv("CA_BUNDLE", "/path/to/ca/bundle")
		config, err := buildHubConfig("https://hub.domain.com", false, false)
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use token auth, no toke path - error", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "")
		config, err := buildHubConfig("https://hub.domain.com", false, false)
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use token auth, not exists toke path - error", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "/hot/exists/token/path")
		config, err := buildHubConfig("https://hub.domain.com", false, false)
		assert.Nil(t, config)
		assert.NotNil(t, err)
	})
	t.Run("use token auth - success", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "./testdata/token")
		config, err := buildHubConfig("https://hub.domain.com", false, false)
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, rest.Config{
			Host:            "https://hub.domain.com",
			BearerTokenFile: "./testdata/token",
		}, *config)
	})
	t.Run("No CA bundle, no Hub CA, not insecure - success", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "./testdata/token")
		config, err := buildHubConfig("https://hub.domain.com", false, false)
		assert.NotNil(t, config)
		assert.Nil(t, err)
		assert.Equal(t, rest.Config{
			Host:            "https://hub.domain.com",
			BearerTokenFile: "./testdata/token",
		}, *config)
	})
	t.Run("use insecure client - success", func(t *testing.T) {
		t.Setenv("CONFIG_PATH", "./testdata/token")
		config, err := buildHubConfig("https://hub.domain.com", false, true)
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
}
