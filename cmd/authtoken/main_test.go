package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
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
	})
	t.Run("no optional arguments", func(t *testing.T) {
		os.Args = []string{"refreshtoken", "azure", "--clientid=test-client-id"}
		t.Cleanup(func() {
			os.Args = nil
		})
		tokenProvider, err := parseArgs()
		assert.NotNil(t, tokenProvider)
		assert.Nil(t, err)
	})
}
