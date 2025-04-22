package webhook

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubefleet-dev/kubefleet/cmd/hubagent/options"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

func TestBuildFleetValidatingWebhooks(t *testing.T) {
	url := options.WebhookClientConnectionType("url")
	testCases := map[string]struct {
		config     Config
		wantLength int
	}{
		"valid input": {
			config: Config{
				serviceNamespace:     "test-namespace",
				servicePort:          8080,
				serviceURL:           "test-url",
				clientConnectionType: &url,
			},
			wantLength: 7,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.config.buildFleetValidatingWebhooks()
			assert.Equal(t, testCase.wantLength, len(gotResult), utils.TestCaseMsg, testName)
		})
	}
}

func TestBuildFleetGuardRailValidatingWebhooks(t *testing.T) {
	url := options.WebhookClientConnectionType("url")
	testCases := map[string]struct {
		config     Config
		wantLength int
	}{
		"valid input": {
			config: Config{
				serviceNamespace:     "test-namespace",
				servicePort:          8080,
				serviceURL:           "test-url",
				clientConnectionType: &url,
			},
			wantLength: 7,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.config.buildFleetGuardRailValidatingWebhooks()
			assert.Equal(t, testCase.wantLength, len(gotResult), utils.TestCaseMsg, testName)
		})
	}
}
