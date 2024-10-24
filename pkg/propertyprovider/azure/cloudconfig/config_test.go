package cloudconfig

import (
	"fmt"
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
				UserAssignedIdentityID:      "  test  \n",
				UseManagedIdentityExtension: true,
				AADClientID:                 "\n  test  \n",
				AADClientSecret:             "  test  \n",
			},
			ClusterName:          "  test  \n",
			Location:             "  test  \n",
			SubscriptionID:       "  test  \n",
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
			ClusterName:          "test",
			Location:             "test",
			SubscriptionID:       "test",
			ClusterResourceGroup: "test",
			AzureAuthConfig: azclient.AzureAuthConfig{
				UseManagedIdentityExtension: true,
				UserAssignedIdentityID:      "test",
				AADClientID:                 "test",
				AADClientSecret:             "test",
			},
			VnetName:          "test",
			VnetResourceGroup: "test",
		}
		config.trimSpace()
		if diff := cmp.Diff(config, expected); diff != "" {
			t.Fatalf("trimSpace(), expect cloudconfig fields are trimmed, got: %v", config)
		}
	})
}

func TestDefaultAndValidate(t *testing.T) {
	tests := map[string]struct {
		config    CloudConfig
		wantError error
	}{
		"Cloud empty": {
			config: CloudConfig{
				ClusterName:          "cluster1",
				Location:             "westus",
				SubscriptionID:       "123456789",
				ClusterResourceGroup: "group",
				VnetName:             "vnet",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "  ",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
			},
			wantError: fmt.Errorf("cloud is empty"),
		},
		"ClusterName empty": {
			config: CloudConfig{
				ClusterName:          "",
				Location:             "westus",
				SubscriptionID:       "123456789",
				ClusterResourceGroup: "group",
				VnetName:             "vnet",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "AzureCloud",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
			},
			wantError: fmt.Errorf("cluster name is empty"),
		},
		"Location empty": {
			config: CloudConfig{
				ClusterName:          "cluster1",
				Location:             "",
				SubscriptionID:       "123456789",
				ClusterResourceGroup: "group",
				VnetName:             "vnet",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "AzureCloud",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
			},
			wantError: fmt.Errorf("location is empty"),
		},
		"SubscriptionID empty": {
			config: CloudConfig{
				ClusterName:          "cluster1",
				Location:             "westus",
				SubscriptionID:       "",
				ClusterResourceGroup: "group",
				VnetName:             "vnet",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "AzureCloud",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
			},
			wantError: fmt.Errorf("subscription ID is empty"),
		},
		"ClusterResourceGroup empty": {
			config: CloudConfig{
				ClusterName:          "cluster1",
				Location:             "westus",
				SubscriptionID:       "123456789",
				ClusterResourceGroup: "",
				VnetName:             "vnet",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "AzureCloud",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
			},
			wantError: fmt.Errorf("cluster resource group is empty"),
		},
		"VnetName empty": {
			config: CloudConfig{
				ClusterName:          "cluster1",
				Location:             "westus",
				SubscriptionID:       "123456789",
				ClusterResourceGroup: "group",
				VnetName:             "",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "AzureCloud",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
			},
			wantError: fmt.Errorf("virtual network name is empty"),
		},
		"UserAssignedIdentityID not empty when UseManagedIdentityExtension is false": {
			config: CloudConfig{
				ClusterName:          "cluster1",
				Location:             "westus",
				SubscriptionID:       "123456789",
				ClusterResourceGroup: "group",
				VnetName:             "vnet",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "AzureCloud",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "aaaa",
				},
			},
			wantError: fmt.Errorf("useManagedIdentityExtension needs to be true when userAssignedIdentityID is provided"),
		},
		"AADClientID empty": {
			config: CloudConfig{
				ClusterName:          "cluster1",
				Location:             "westus",
				SubscriptionID:       "123456789",
				ClusterResourceGroup: "group",
				VnetName:             "vnet",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "AzureCloud",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					AADClientID:                 "",
					AADClientSecret:             "2",
				},
			},
			wantError: fmt.Errorf("AAD client ID or AAD client secret is empty"),
		},
		"AADClientSecret empty": {
			config: CloudConfig{
				ClusterName:          "cluster1",
				Location:             "westus",
				SubscriptionID:       "123456789",
				ClusterResourceGroup: "group",
				VnetName:             "vnet",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "AzureCloud",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					AADClientID:                 "1",
					AADClientSecret:             "",
				},
			},
			wantError: fmt.Errorf("AAD client ID or AAD client secret is empty"),
		},
		"has all required properties": {
			config: CloudConfig{
				ClusterName:          "cluster1",
				Location:             "westus",
				SubscriptionID:       "123456789",
				ClusterResourceGroup: "group",
				VnetName:             "vnet",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud:     "AzureCloud",
					UserAgent: "fleet-member-agent",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
				},
			},
			wantError: nil,
		},
		"has all required properties with msi and specified values": {
			config: CloudConfig{
				ClusterName:          "cluster1",
				Location:             "westus",
				SubscriptionID:       "123456789",
				ClusterResourceGroup: "group",
				VnetName:             "vnet",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud:     "AzureCloud",
					UserAgent: "user-agent",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "u",
				},
			},
			wantError: nil,
		},
		"has all required properties with msi and disabled ratelimiter": {
			config: CloudConfig{
				ClusterName:          "cluster1",
				Location:             "westus",
				SubscriptionID:       "123456789",
				ClusterResourceGroup: "group",
				VnetName:             "vnet",
				VnetResourceGroup:    "vrg",
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud:     "AzureCloud",
					UserAgent: "user-agent",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "u",
				},
				RateLimitConfig: &RateLimitConfig{
					CloudProviderRateLimit: false,
				},
			},
			wantError: nil,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.config.defaultAndValidate()
			if test.wantError != nil {
				if diff := cmp.Diff(err.Error(), test.wantError.Error()); diff != "" {
					t.Fatalf("defaultAndValidate()  got %v, wantError %v", err, test.wantError)
				}
			} else {
				if err != nil {
					t.Fatalf("defaultAndValidate() got %v, wantError %v", err, test.wantError)
				}
			}
		})
	}
}

func TestLoadCloudConfigFromFile(t *testing.T) {
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
			filePath: "./test/not_exist.yaml",
			wantErr:  true,
		},
		"failed to unmarshal file": {
			filePath: "./test/azure_invalid_config.yaml",
			wantErr:  true,
		},
		"succeeded to load config": {
			filePath: "./test/azure_valid_config.yaml",
			wantConfig: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud:     "AzurePublicCloud",
					TenantID:  "00000000-0000-0000-0000-000000000000",
					UserAgent: "fleet-member-agent",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "",
					AADClientID:                 "",
					AADClientSecret:             "",
				},
				RateLimitConfig:      &RateLimitConfig{CloudProviderRateLimit: false},
				Location:             "westus",
				SubscriptionID:       "00000000-0000-0000-0000-000000000000",
				VnetName:             "test-vnet",
				VnetResourceGroup:    "test-rg",
				ClusterName:          "test-cluster",
				ClusterResourceGroup: "test-rg",
				CloudProviderBackoff: false,
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			config, err := LoadCloudConfigFromFile(test.filePath)
			if got := err != nil; got != test.wantErr {
				t.Fatalf("LoadCloudConfigFromFile() got %v, wantErr %v", err, test.wantErr)
			}
			if diff := cmp.Diff(config, test.wantConfig); diff != "" {
				t.Errorf("LoadCloudConfigFromFile() cloud config mismatch got %v, want %v", config, test.wantConfig)
			}
		})
	}
}
