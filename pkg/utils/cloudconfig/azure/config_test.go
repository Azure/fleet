package azure

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient"
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
			Location:             "  test  \n",
			SubscriptionID:       "  test  \n",
			ResourceGroup:        "\r\n  test  \n",
			ClusterName:          "    test \n",
			ClusterResourceGroup: "\r\n  test  \n",
			VnetName:             "  test   ",
			VnetResourceGroup:    " \t  test   ",
		}

		expected := CloudConfig{
			ARMClientConfig: azclient.ARMClientConfig{
				Cloud:     "test",
				TenantID:  "test",
				UserAgent: "test",
			},
			Location:             "test",
			SubscriptionID:       "test",
			ResourceGroup:        "test",
			ClusterName:          "test",
			ClusterResourceGroup: "test",
			VnetName:             "test",
			VnetResourceGroup:    "test",
			AzureAuthConfig: azclient.AzureAuthConfig{
				UseManagedIdentityExtension: true,
				UserAssignedIdentityID:      "test",
				AADClientID:                 "test",
				AADClientSecret:             "test",
			},
		}
		config.trimSpace()
		if diff := cmp.Diff(config, expected); diff != "" {
			t.Fatalf("trimSpace()mismatch (-want +got):\n%s", diff)
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
		expectPass bool
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
			},
			expectPass: false,
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
				Location:             "",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
			},
			expectPass: false,
		},
		"ClusterName empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
			},
			expectPass: false,
		},
		"ClusterResourceGroup empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "",
				VnetName:             "vn",
			},
			expectPass: false,
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
				Location:             "l",
				SubscriptionID:       "",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
			},
			expectPass: false,
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
			},
			expectPass: false,
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "",
			},
			expectPass: false,
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
				VnetResourceGroup:    "",
			},
			expectPass: true,
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
			},
			expectPass: false,
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
			},
			expectPass: false,
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
			},
			expectPass: false,
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
				RateLimitConfig: &RateLimitConfig{
					CloudProviderRateLimit:            true,
					CloudProviderRateLimitQPS:         0,
					CloudProviderRateLimitBucket:      0,
					CloudProviderRateLimitQPSWrite:    0,
					CloudProviderRateLimitBucketWrite: 0,
				},
			},
			expectPass: true,
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
				RateLimitConfig: &RateLimitConfig{
					CloudProviderRateLimit:            true,
					CloudProviderRateLimitQPS:         2,
					CloudProviderRateLimitBucket:      4,
					CloudProviderRateLimitQPSWrite:    2,
					CloudProviderRateLimitBucketWrite: 4,
				},
			},
			expectPass: true,
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
				RateLimitConfig: &RateLimitConfig{
					CloudProviderRateLimit: false,
				},
			},
			expectPass: true,
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
			},
			expectPass: true,
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
				Location:             "l",
				SubscriptionID:       "s",
				ResourceGroup:        "v",
				ClusterName:          "c",
				ClusterResourceGroup: "g",
				VnetName:             "vn",
			},
			expectPass: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.config.validate()
			if got := err == nil; got != test.expectPass {
				t.Errorf("validate() = got %v, want %v", got, test.expectPass)
			}
			if name == "VnetResourceGroup empty" {
				if test.config.VnetResourceGroup != test.config.ResourceGroup {
					t.Errorf("validate() = got %v, want %v", test.config.VnetResourceGroup, test.config.ResourceGroup)
				}
			}
		})
	}
}

func TestNewCloudConfigFromFile(t *testing.T) {
	tests := map[string]struct {
		filePath       string
		expectErr      bool
		expectedConfig *CloudConfig
	}{
		"file path is empty": {
			filePath:  "",
			expectErr: true,
		},
		"failed to open file": {
			filePath:  "./test/not_exist.json",
			expectErr: true,
		},
		"failed to unmarshal file": {
			filePath:  "./test/azure_config_nojson.txt",
			expectErr: true,
		},
		"failed to validate config": {
			filePath:  "./test/azure_invalid_config.json",
			expectErr: true,
		},
		"succeeded to load config": {
			filePath: "./test/azure_valid_config.json",
			expectedConfig: &CloudConfig{
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
				Location:             "eastus",
				SubscriptionID:       "00000000-0000-0000-0000-000000000000",
				ResourceGroup:        "test-rg",
				VnetName:             "test-vnet",
				VnetResourceGroup:    "test-rg",
				ClusterName:          "test-cluster",
				ClusterResourceGroup: "test-rg",
				RateLimitConfig: &RateLimitConfig{
					CloudProviderRateLimit:            true,
					CloudProviderRateLimitQPS:         1,
					CloudProviderRateLimitQPSWrite:    1,
					CloudProviderRateLimitBucket:      5,
					CloudProviderRateLimitBucketWrite: 5,
				},
			},
			expectErr: false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			config, err := NewCloudConfigFromFile(test.filePath)
			if got := err != nil; got != test.expectErr {
				t.Errorf("Failed to run NewCloudConfigFromFile(%s): got %v, want %v", test.filePath, got, test.expectErr)
			}
			if diff := cmp.Diff(config, test.expectedConfig); diff != "" {
				t.Errorf("NewCloudConfigFromFile(%s) mismatch (-want +got):\n%s", test.filePath, diff)
			}
		})
	}
}
