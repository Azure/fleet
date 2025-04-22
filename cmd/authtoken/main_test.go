package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubefleet-dev/kubefleet/pkg/authtoken/providers/azure"
)

func TestParseArgs(t *testing.T) {
	t.Run("all arguments", func(t *testing.T) {
		os.Args = []string{"refreshtoken", "azure", "--clientid=test-client-id", "--scope=test-scope"}
		t.Cleanup(func() {
			os.Args = nil
		})
		tokenProvider, err := parseArgs()
		assert.NotNil(t, tokenProvider)
		assert.Nil(t, err)

		azTokenProvider, ok := tokenProvider.(*azure.AuthTokenProvider)
		assert.Equal(t, true, ok)
		assert.Equal(t, "test-scope", azTokenProvider.Scope)
	})
	t.Run("no optional arguments", func(t *testing.T) {
		os.Args = []string{"refreshtoken", "azure", "--clientid=test-client-id"}
		t.Cleanup(func() {
			os.Args = nil
		})
		tokenProvider, err := parseArgs()
		assert.NotNil(t, tokenProvider)
		assert.Nil(t, err)

		azTokenProvider, ok := tokenProvider.(*azure.AuthTokenProvider)
		assert.Equal(t, true, ok)
		assert.Equal(t, "6dae42f8-4368-4678-94ff-3960e28e3630", azTokenProvider.Scope)
	})
}
