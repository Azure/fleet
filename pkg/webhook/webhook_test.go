package webhook

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.goms.io/fleet/cmd/hubagent/options"
	"go.goms.io/fleet/pkg/utils"
)

func TestBuildValidatingWebhooks(t *testing.T) {
	url := options.WebhookClientConnectionType("url")
	testCases := map[string]struct {
		config     Config
		wantLength int
	}{
		"disable guard rail": {
			config: Config{
				serviceNamespace:     "test-namespace",
				servicePort:          8080,
				serviceURL:           "test-url",
				clientConnectionType: &url,
				enableGuardRail:      false,
			},
			wantLength: 4,
		},
		"enable guard rail": {
			config: Config{
				serviceNamespace:     "test-namespace",
				servicePort:          8080,
				serviceURL:           "test-url",
				clientConnectionType: &url,
				enableGuardRail:      true,
			},
			wantLength: 10,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.config.buildValidatingWebHooks()
			assert.Equal(t, testCase.wantLength, len(gotResult), utils.TestCaseMsg, testName)
		})
	}
}
