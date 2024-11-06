package azure

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/policy/ratelimit"
)

func TestTrimSpace(t *testing.T) {
	t.Run("test spaces are trimmed", func(t *testing.T) {
		config := CloudConfig{
			ARMClientConfig: azclient.ARMClientConfig{
				Cloud:     "  test  \n",
				UserAgent: "  test  \n",
				TenantID:  "  test  \t \n",
			},
			AzureAuthConfig: azclient.AzureAuthConfig{
				UseManagedIdentityExtension: true,
				UserAssignedIdentityID:      "  test  \n",
				AADClientID:                 "\n  test  \n",
				AADClientSecret:             "  test  \n",
			},
			Location:          "  test  \n",
			SubscriptionID:    "  test  \n",
			ResourceGroup:     "\r\n  test  \n",
			VnetName:          "  test   ",
			VnetResourceGroup: " \t  test   ",
		}

		want := CloudConfig{
			ARMClientConfig: azclient.ARMClientConfig{
				Cloud:     "test",
				TenantID:  "test",
				UserAgent: "test",
			},
			Location:          "test",
			SubscriptionID:    "test",
			ResourceGroup:     "test",
			VnetName:          "test",
			VnetResourceGroup: "test",
			AzureAuthConfig: azclient.AzureAuthConfig{
				UseManagedIdentityExtension: true,
				UserAssignedIdentityID:      "test",
				AADClientID:                 "test",
				AADClientSecret:             "test",
			},
		}
		config.trimSpace()
		if diff := cmp.Diff(config, want); diff != "" {
			t.Errorf("trimSpace() mismatch (-got +want):\n%s", diff)
		}
	})
}

func TestSetUserAgent(t *testing.T) {
	config := &CloudConfig{}
	config.SetUserAgent("test")
	if config.UserAgent != "test" {
		t.Errorf("SetUserAgent(test) = %s, want test", config.UserAgent)
	}
}

func TestValidate(t *testing.T) {
	tests := map[string]struct {
		config     *CloudConfig
		wantConfig *CloudConfig
		wantPass   bool
	}{
		"Cloud empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
				VnetName:       "vn",
			},
			wantPass: false,
		},
		"Location empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:       "",
				SubscriptionID: "s",
				ResourceGroup:  "v",
				VnetName:       "vn",
			},
			wantPass: false,
		},
		"SubscriptionID empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:       "l",
				SubscriptionID: "",
				ResourceGroup:  "v",
				VnetName:       "vn",
			},
			wantPass: false,
		},
		"ResourceGroup empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "",
				VnetName:       "vn",
			},
			wantPass: false,
		},
		"VnetName empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
				VnetName:       "",
			},
			wantPass: false,
		},
		"VnetResourceGroup empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:          "l",
				SubscriptionID:    "s",
				ResourceGroup:     "v",
				VnetName:          "vn",
				VnetResourceGroup: "",
			},
			wantConfig: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:          "l",
				SubscriptionID:    "s",
				ResourceGroup:     "v",
				VnetName:          "vn",
				VnetResourceGroup: "v",
			},
			wantPass: true,
		},
		"UserAssignedIdentityID not empty when UseManagedIdentityExtension is false": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "aaaa",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
				VnetName:       "vn",
			},
			wantPass: false,
		},
		"AADClientID empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "",
					AADClientSecret:             "2",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
				VnetName:       "vn",
			},
			wantPass: false,
		},
		"AADClientSecret empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "1",
					AADClientSecret:             "",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
				VnetName:       "vn",
			},
			wantPass: false,
		},
		"RateLimitConfig values are zero": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "1",
					AADClientSecret:             "2",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
				VnetName:       "vn",
				Config: ratelimit.Config{
					CloudProviderRateLimit:            true,
					CloudProviderRateLimitQPS:         0,
					CloudProviderRateLimitBucket:      0,
					CloudProviderRateLimitQPSWrite:    0,
					CloudProviderRateLimitBucketWrite: 0,
				},
			},
			wantConfig: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "1",
					AADClientSecret:             "2",
				},
				Location:          "l",
				SubscriptionID:    "s",
				ResourceGroup:     "v",
				VnetName:          "vn",
				VnetResourceGroup: "v",
				Config: ratelimit.Config{
					CloudProviderRateLimit:            true,
					CloudProviderRateLimitQPS:         1.0,
					CloudProviderRateLimitBucket:      5,
					CloudProviderRateLimitQPSWrite:    1.0,
					CloudProviderRateLimitBucketWrite: 5,
				},
			},
			wantPass: true,
		},
		"RateLimitConfig with non-zero values": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "1",
					AADClientSecret:             "2",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
				VnetName:       "vn",
				Config: ratelimit.Config{
					CloudProviderRateLimit:            true,
					CloudProviderRateLimitQPS:         2,
					CloudProviderRateLimitBucket:      4,
					CloudProviderRateLimitQPSWrite:    2,
					CloudProviderRateLimitBucketWrite: 4,
				},
			},
			wantConfig: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "1",
					AADClientSecret:             "2",
				},
				Location:          "l",
				SubscriptionID:    "s",
				ResourceGroup:     "v",
				VnetName:          "vn",
				VnetResourceGroup: "v",
				Config: ratelimit.Config{
					CloudProviderRateLimit:            true,
					CloudProviderRateLimitQPS:         2,
					CloudProviderRateLimitBucket:      4,
					CloudProviderRateLimitQPSWrite:    2,
					CloudProviderRateLimitBucketWrite: 4,
				},
			},
			wantPass: true,
		},
		"CloudProviderRateLimit is false": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "1",
					AADClientSecret:             "2",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
				VnetName:       "vn",
				Config: ratelimit.Config{
					CloudProviderRateLimit: false,
				},
			},
			wantConfig: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "1",
					AADClientSecret:             "2",
				},
				Location:          "l",
				SubscriptionID:    "s",
				ResourceGroup:     "v",
				VnetName:          "vn",
				VnetResourceGroup: "v",
				Config: ratelimit.Config{
					CloudProviderRateLimit: false,
				},
			},
			wantPass: true,
		},
		"has all required properties with secret and default values": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "1",
					AADClientSecret:             "2",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
				VnetName:       "vn",
			},
			wantConfig: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "1",
					AADClientSecret:             "2",
				},
				Location:          "l",
				SubscriptionID:    "s",
				ResourceGroup:     "v",
				VnetName:          "vn",
				VnetResourceGroup: "v",
			},
			wantPass: true,
		},
		"has all required properties with msi and specified values": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "u",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
				VnetName:       "vn",
			},
			wantConfig: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "u",
				},
				Location:          "l",
				SubscriptionID:    "s",
				ResourceGroup:     "v",
				VnetName:          "vn",
				VnetResourceGroup: "v",
			},
			wantPass: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.config.validate()
			if got := err == nil; got != test.wantPass {
				t.Fatalf("validate() = got %v, want %v", got, test.wantPass)
			}

			if err == nil {
				if diff := cmp.Diff(test.config, test.wantConfig); diff != "" {
					t.Errorf("validate() mismatch (-got +want):\n%s", diff)
				}
			}
		})
	}
}

func TestNewCloudConfigFromFile(t *testing.T) {
	tests := map[string]struct {
		filePath   string
		wantErr    bool
		wantConfig *CloudConfig
	}{
		"file path is empty": {
			filePath: "",
			wantErr:  true,
		},
		"failed to open file": {
			filePath: "./test/not_exist.json",
			wantErr:  true,
		},
		"failed to unmarshal file": {
			filePath: "./test/azure_config_nojson.txt",
			wantErr:  true,
		},
		"failed to validate config": {
			filePath: "./test/azure_invalid_config.json",
			wantErr:  true,
		},
		"succeeded to load config": {
			filePath: "./test/azure_valid_config.json",
			wantConfig: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud:    "AzurePublicCloud",
					TenantID: "00000000-0000-0000-0000-000000000000",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "11111111-1111-1111-1111-111111111111",
					AADClientID:                 "",
					AADClientSecret:             "",
				},
				Location:          "eastus",
				SubscriptionID:    "00000000-0000-0000-0000-000000000000",
				ResourceGroup:     "test-rg",
				VnetName:          "test-vnet",
				VnetResourceGroup: "test-rg",
				Config: ratelimit.Config{
					CloudProviderRateLimit: false,
				},
			},
			wantErr: false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			config, err := NewCloudConfigFromFile(test.filePath)
			if got := err != nil; got != test.wantErr {
				t.Fatalf("Failed to run NewCloudConfigFromFile(%s): got %v, want %v", test.filePath, got, test.wantErr)
			}
			if diff := cmp.Diff(config, test.wantConfig); diff != "" {
				t.Errorf("NewCloudConfigFromFile(%s) mismatch (-got +want):\n%s", test.filePath, diff)
			}
		})
	}
}
