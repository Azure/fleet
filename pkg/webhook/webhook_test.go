package webhook

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	admv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kubefleet-dev/kubefleet/cmd/hubagent/options"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	testmanager "github.com/kubefleet-dev/kubefleet/test/utils/manager"
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
		"enable workload": {
			config: Config{
				serviceNamespace:     "test-namespace",
				servicePort:          8080,
				serviceURL:           "test-url",
				clientConnectionType: &url,
				enableWorkload:       true,
			},
			wantLength: 6,
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
		enableWorkload                bool
		useCertManager                bool
		want                          *Config
		wantErr                       bool
	}{
		{
			name:                          "valid input",
			mgr:                           nil,
			webhookServiceName:            "test-webhook",
			port:                          8080,
			clientConnectionType:          nil,
			certDir:                       t.TempDir(),
			enableGuardRail:               true,
			denyModifyMemberClusterLabels: true,
			enableWorkload:                false,
			useCertManager:                false,
			want: &Config{
				serviceNamespace:              "test-namespace",
				serviceName:                   "test-webhook",
				servicePort:                   8080,
				clientConnectionType:          nil,
				enableGuardRail:               true,
				denyModifyMemberClusterLabels: true,
				enableWorkload:                false,
				useCertManager:                false,
			},
			wantErr: false,
		},
		{
			name:                          "valid input with cert-manager",
			mgr:                           nil,
			webhookServiceName:            "test-webhook",
			port:                          8080,
			clientConnectionType:          nil,
			certDir:                       t.TempDir(),
			enableGuardRail:               true,
			denyModifyMemberClusterLabels: true,
			enableWorkload:                true,
			useCertManager:                true,
			want: &Config{
				serviceNamespace:              "test-namespace",
				serviceName:                   "test-webhook",
				servicePort:                   8080,
				clientConnectionType:          nil,
				enableGuardRail:               true,
				denyModifyMemberClusterLabels: true,
				enableWorkload:                true,
				useCertManager:                true,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("POD_NAMESPACE", "test-namespace")

			got, err := NewWebhookConfig(tt.mgr, tt.webhookServiceName, tt.port, tt.clientConnectionType, tt.certDir, tt.enableGuardRail, tt.denyModifyMemberClusterLabels, tt.enableWorkload, tt.useCertManager, "fleet-webhook-server-cert", nil, false)
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

func TestNewWebhookConfig_SelfSignedCertError(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "test-namespace")

	// Use an invalid certDir (read-only location) to force genCertificate to fail
	invalidCertDir := "/proc/invalid-cert-dir"

	clientConnectionType := options.Service
	_, err := NewWebhookConfig(
		nil,
		"test-service",
		443,
		&clientConnectionType,
		invalidCertDir,
		false,                       // enableGuardRail
		false,                       // denyModifyMemberClusterLabels
		false,                       // enableWorkload
		false,                       // useCertManager = false to trigger self-signed path
		"fleet-webhook-server-cert", // webhookCertName
		nil,                         // whiteListedUsers
		false,                       // networkingAgentsEnabled
	)

	if err == nil {
		t.Fatal("Expected error when genCertificate fails, got nil")
	}

	expectedErrMsg := "failed to generate self-signed certificate"
	if !strings.Contains(err.Error(), expectedErrMsg) {
		t.Errorf("Expected error to contain '%s', got: %v", expectedErrMsg, err)
	}
}

func TestNewWebhookConfigFromOptions(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "test-namespace")

	testCases := map[string]struct {
		opts       *options.Options
		wantErr    bool
		wantConfig *Config
	}{
		"valid options with cert-manager": {
			opts: &options.Options{
				WebhookServiceName:            "test-webhook",
				WebhookClientConnectionType:   "service",
				EnableGuardRail:               true,
				DenyModifyMemberClusterLabels: true,
				EnableWorkload:                true,
				UseCertManager:                true,
				WhiteListedUsers:              "user1,user2,user3",
				NetworkingAgentsEnabled:       true,
			},
			wantErr: false,
			wantConfig: &Config{
				serviceNamespace:              "test-namespace",
				serviceName:                   "test-webhook",
				servicePort:                   8443,
				enableGuardRail:               true,
				denyModifyMemberClusterLabels: true,
				enableWorkload:                true,
				useCertManager:                true,
				webhookCertName:               "fleet-webhook-certificate",
				whiteListedUsers:              []string{"user1", "user2", "user3"},
				networkingAgentsEnabled:       true,
			},
		},
		"valid options without cert-manager": {
			opts: &options.Options{
				WebhookServiceName:            "test-webhook",
				WebhookClientConnectionType:   "url",
				EnableGuardRail:               false,
				DenyModifyMemberClusterLabels: false,
				EnableWorkload:                false,
				UseCertManager:                false,
				WhiteListedUsers:              "admin",
				NetworkingAgentsEnabled:       false,
			},
			wantErr: false,
			wantConfig: &Config{
				serviceNamespace:              "test-namespace",
				serviceName:                   "test-webhook",
				servicePort:                   8443,
				enableGuardRail:               false,
				denyModifyMemberClusterLabels: false,
				enableWorkload:                false,
				useCertManager:                false,
				webhookCertName:               "fleet-webhook-certificate",
				whiteListedUsers:              []string{"admin"},
				networkingAgentsEnabled:       false,
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got, err := NewWebhookConfigFromOptions(nil, tc.opts, 8443)
			if (err != nil) != tc.wantErr {
				t.Errorf("NewWebhookConfigFromOptions() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			// Verify key fields are set correctly
			opts := []cmp.Option{
				cmpopts.IgnoreFields(Config{}, "caPEM", "mgr", "clientConnectionType", "serviceURL"),
			}
			opts = append(opts, cmpopts.IgnoreUnexported(Config{}))
			if diff := cmp.Diff(tc.wantConfig, got, opts...); diff != "" {
				t.Errorf("NewWebhookConfigFromOptions() mismatch (-want +got):\n%s", diff)
			}

			// Verify whiteListedUsers were split correctly
			if diff := cmp.Diff(tc.wantConfig.whiteListedUsers, got.whiteListedUsers); diff != "" {
				t.Errorf("whiteListedUsers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildWebhookAnnotations(t *testing.T) {
	testCases := map[string]struct {
		config            Config
		expectedAnnotions map[string]string
	}{
		"useCertManager is false - returns empty map": {
			config: Config{
				useCertManager:   false,
				serviceNamespace: "fleet-system",
				webhookCertName:  "fleet-webhook-server-cert",
			},
			expectedAnnotions: map[string]string{},
		},
		"useCertManager is true - returns annotation with correct format": {
			config: Config{
				useCertManager:   true,
				serviceNamespace: "fleet-system",
				webhookCertName:  "fleet-webhook-server-cert",
			},
			expectedAnnotions: map[string]string{
				"cert-manager.io/inject-ca-from": "fleet-system/fleet-webhook-server-cert",
			},
		},
		"useCertManager is true with custom namespace and cert name": {
			config: Config{
				useCertManager:   true,
				serviceNamespace: "custom-namespace",
				webhookCertName:  "custom-webhook-cert",
			},
			expectedAnnotions: map[string]string{
				"cert-manager.io/inject-ca-from": "custom-namespace/custom-webhook-cert",
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := tc.config.buildWebhookAnnotations()
			if diff := cmp.Diff(tc.expectedAnnotions, got); diff != "" {
				t.Errorf("buildWebhookAnnotations() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCreateClientConfig(t *testing.T) {
	serviceConnectionType := options.Service
	testCases := map[string]struct {
		config         Config
		validationPath string
		expectCABundle bool
	}{
		"useCertManager is false - CABundle should be set": {
			config: Config{
				useCertManager:       false,
				caPEM:                []byte("test-ca-bundle"),
				serviceNamespace:     "fleet-system",
				serviceName:          "fleet-webhook-service",
				servicePort:          443,
				clientConnectionType: &serviceConnectionType,
			},
			validationPath: "/validate",
			expectCABundle: true,
		},
		"useCertManager is true - CABundle should be empty for cert-manager injection": {
			config: Config{
				useCertManager:       true,
				caPEM:                []byte("test-ca-bundle"),
				serviceNamespace:     "fleet-system",
				serviceName:          "fleet-webhook-service",
				servicePort:          443,
				clientConnectionType: &serviceConnectionType,
			},
			validationPath: "/validate",
			expectCABundle: false,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			clientConfig := tc.config.createClientConfig(tc.validationPath)

			if tc.expectCABundle {
				if len(clientConfig.CABundle) == 0 {
					t.Errorf("Expected CABundle to be set, but it was empty")
				}
				if diff := cmp.Diff(tc.config.caPEM, clientConfig.CABundle); diff != "" {
					t.Errorf("CABundle mismatch (-want +got):\n%s", diff)
				}
			} else {
				if len(clientConfig.CABundle) != 0 {
					t.Errorf("Expected CABundle to be empty for cert-manager injection, but got: %v", clientConfig.CABundle)
				}
			}

			// Verify service reference is set correctly
			if clientConfig.Service == nil {
				t.Errorf("Expected Service to be set")
			} else {
				if clientConfig.Service.Namespace != tc.config.serviceNamespace {
					t.Errorf("Expected Service.Namespace=%s, got %s", tc.config.serviceNamespace, clientConfig.Service.Namespace)
				}
				if clientConfig.Service.Name != tc.config.serviceName {
					t.Errorf("Expected Service.Name=%s, got %s", tc.config.serviceName, clientConfig.Service.Name)
				}
				if *clientConfig.Service.Port != tc.config.servicePort {
					t.Errorf("Expected Service.Port=%d, got %d", tc.config.servicePort, *clientConfig.Service.Port)
				}
				if *clientConfig.Service.Path != tc.validationPath {
					t.Errorf("Expected Service.Path=%s, got %s", tc.validationPath, *clientConfig.Service.Path)
				}
			}
		})
	}
}

func TestCheckCAInjection(t *testing.T) {
	testCases := map[string]struct {
		config           Config
		existingObjects  []client.Object
		expectError      bool
		expectedErrorMsg string
	}{
		"useCertManager is false - returns nil without checking": {
			config: Config{
				useCertManager:  false,
				enableGuardRail: false,
			},
			expectError: false,
		},
		"useCertManager is true, all CA bundles present": {
			config: Config{
				useCertManager:  true,
				enableGuardRail: false,
			},
			existingObjects: []client.Object{
				&admv1.MutatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetMutatingWebhookCfgName},
					Webhooks: []admv1.MutatingWebhook{
						{
							Name: "test-mutating-webhook",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte("fake-ca-bundle"),
							},
						},
					},
				},
				&admv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetValidatingWebhookCfgName},
					Webhooks: []admv1.ValidatingWebhook{
						{
							Name: "test-validating-webhook-1",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte("fake-ca-bundle"),
							},
						},
						{
							Name: "test-validating-webhook-2",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte("fake-ca-bundle"),
							},
						},
					},
				},
			},
			expectError: false,
		},
		"useCertManager is true, mutating webhook missing CA bundle": {
			config: Config{
				useCertManager:  true,
				enableGuardRail: false,
			},
			existingObjects: []client.Object{
				&admv1.MutatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetMutatingWebhookCfgName},
					Webhooks: []admv1.MutatingWebhook{
						{
							Name: "test-mutating-webhook",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: nil, // Missing CA bundle
							},
						},
					},
				},
				&admv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetValidatingWebhookCfgName},
					Webhooks: []admv1.ValidatingWebhook{
						{
							Name: "test-validating-webhook",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte("fake-ca-bundle"),
							},
						},
					},
				},
			},
			expectError:      true,
			expectedErrorMsg: "test-mutating-webhook is missing CA bundle",
		},
		"useCertManager is true, validating webhook missing CA bundle": {
			config: Config{
				useCertManager:  true,
				enableGuardRail: false,
			},
			existingObjects: []client.Object{
				&admv1.MutatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetMutatingWebhookCfgName},
					Webhooks: []admv1.MutatingWebhook{
						{
							Name: "test-mutating-webhook",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte("fake-ca-bundle"),
							},
						},
					},
				},
				&admv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetValidatingWebhookCfgName},
					Webhooks: []admv1.ValidatingWebhook{
						{
							Name: "test-validating-webhook-1",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte("fake-ca-bundle"),
							},
						},
						{
							Name: "test-validating-webhook-2",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte{}, // Empty CA bundle
							},
						},
					},
				},
			},
			expectError:      true,
			expectedErrorMsg: "test-validating-webhook-2 is missing CA bundle",
		},
		"useCertManager is true with guard rail, all CA bundles present": {
			config: Config{
				useCertManager:  true,
				enableGuardRail: true,
			},
			existingObjects: []client.Object{
				&admv1.MutatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetMutatingWebhookCfgName},
					Webhooks: []admv1.MutatingWebhook{
						{
							Name: "test-mutating-webhook",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte("fake-ca-bundle"),
							},
						},
					},
				},
				&admv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetValidatingWebhookCfgName},
					Webhooks: []admv1.ValidatingWebhook{
						{
							Name: "test-validating-webhook",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte("fake-ca-bundle"),
							},
						},
					},
				},
				&admv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetGuardRailWebhookCfgName},
					Webhooks: []admv1.ValidatingWebhook{
						{
							Name: "test-guard-rail-webhook",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte("fake-ca-bundle"),
							},
						},
					},
				},
			},
			expectError: false,
		},
		"useCertManager is true with guard rail, guard rail webhook missing CA bundle": {
			config: Config{
				useCertManager:  true,
				enableGuardRail: true,
			},
			existingObjects: []client.Object{
				&admv1.MutatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetMutatingWebhookCfgName},
					Webhooks: []admv1.MutatingWebhook{
						{
							Name: "test-mutating-webhook",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte("fake-ca-bundle"),
							},
						},
					},
				},
				&admv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetValidatingWebhookCfgName},
					Webhooks: []admv1.ValidatingWebhook{
						{
							Name: "test-validating-webhook",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: []byte("fake-ca-bundle"),
							},
						},
					},
				},
				&admv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: fleetGuardRailWebhookCfgName},
					Webhooks: []admv1.ValidatingWebhook{
						{
							Name: "test-guard-rail-webhook",
							ClientConfig: admv1.WebhookClientConfig{
								CABundle: nil, // Missing CA bundle
							},
						},
					},
				},
			},
			expectError:      true,
			expectedErrorMsg: "test-guard-rail-webhook is missing CA bundle",
		},
		"useCertManager is true, webhook configuration not found": {
			config: Config{
				useCertManager:  true,
				enableGuardRail: false,
			},
			existingObjects:  []client.Object{},
			expectError:      true,
			expectedErrorMsg: "failed to get MutatingWebhookConfiguration",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// Create a fake client with a proper scheme
			scheme := runtime.NewScheme()
			_ = admv1.AddToScheme(scheme)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingObjects...).
				Build()

			// Create a fake manager that returns our fake client
			tc.config.mgr = &testmanager.FakeManager{Client: fakeClient}

			err := tc.config.CheckCAInjection(context.Background())

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				} else if !strings.Contains(err.Error(), tc.expectedErrorMsg) {
					t.Errorf("Expected error message to contain %q, but got: %v", tc.expectedErrorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}
