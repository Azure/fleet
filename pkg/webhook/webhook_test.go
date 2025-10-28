package webhook

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kubefleet-dev/kubefleet/cmd/hubagent/options"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

func TestBuildFleetMutatingWebhooks(t *testing.T) {
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
			wantLength: 1,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.config.buildFleetMutatingWebhooks()
			if diff := cmp.Diff(testCase.wantLength, len(gotResult)); diff != "" {
				t.Errorf("buildFleetMutatingWebhooks() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

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
			wantLength: 8,
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
			wantLength: 6,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			gotResult := testCase.config.buildFleetGuardRailValidatingWebhooks()
			assert.Equal(t, testCase.wantLength, len(gotResult), utils.TestCaseMsg, testName)
		})
	}
}

func TestNewWebhookConfig(t *testing.T) {
	tests := []struct {
		name                          string
		mgr                           manager.Manager
		webhookServiceName            string
		port                          int32
		clientConnectionType          *options.WebhookClientConnectionType
		certDir                       string
		enableGuardRail               bool
		denyModifyMemberClusterLabels bool
		want                          *Config
		wantErr                       bool
	}{
		{
			name:                          "valid input",
			mgr:                           nil,
			webhookServiceName:            "test-webhook",
			port:                          8080,
			clientConnectionType:          nil,
			certDir:                       "/tmp/cert",
			enableGuardRail:               true,
			denyModifyMemberClusterLabels: true,
			want: &Config{
				serviceNamespace:              "test-namespace",
				serviceName:                   "test-webhook",
				servicePort:                   8080,
				clientConnectionType:          nil,
				enableGuardRail:               true,
				denyModifyMemberClusterLabels: true,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("POD_NAMESPACE", "test-namespace")
			defer t.Setenv("POD_NAMESPACE", "")
			got, err := NewWebhookConfig(tt.mgr, tt.webhookServiceName, tt.port, tt.clientConnectionType, tt.certDir, tt.enableGuardRail, tt.denyModifyMemberClusterLabels)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewWebhookConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got == nil || tt.want == nil {
				if got != tt.want {
					t.Errorf("NewWebhookConfig() = %v, want %v", got, tt.want)
				}
				return
			}

			opts := []cmp.Option{
				cmpopts.IgnoreFields(Config{}, "caPEM"),
			}
			opts = append(opts, cmpopts.IgnoreUnexported(Config{}))
			if diff := cmp.Diff(tt.want, got, opts...); diff != "" {
				t.Errorf("NewWebhookConfig() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
