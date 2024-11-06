/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package azure provides utilities to load, parse, and validate Azure cloud configuration.
package azure

import (
	"fmt"
	"io"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/policy/ratelimit"
	"sigs.k8s.io/cloud-provider-azure/pkg/consts"
)

// CloudConfig holds the configuration parsed from the --cloud-config flag.
type CloudConfig struct {
	azclient.ARMClientConfig `json:",inline" mapstructure:",squash"`
	azclient.AzureAuthConfig `json:",inline" mapstructure:",squash"`
	ratelimit.Config         `json:",inline" mapstructure:",squash"`

	// azure resource location
	Location string `json:"location,omitempty" mapstructure:"location,omitempty"`
	// subscription ID
	SubscriptionID string `json:"subscriptionID,omitempty" mapstructure:"subscriptionID,omitempty"`
	// default resource group where the azure resources are deployed
	ResourceGroup string `json:"resourceGroup,omitempty" mapstructure:"resourceGroup,omitempty"`
	// name of the virtual network of cluster
	VnetName string `json:"vnetName,omitempty" mapstructure:"vnetName,omitempty"`
	// name of the resource group where the virtual network is deployed
	VnetResourceGroup string `json:"vnetResourceGroup,omitempty" mapstructure:"vnetResourceGroup,omitempty"`
}

// NewCloudConfigFromFile loads cloud config from a file given the file path.
func NewCloudConfigFromFile(filePath string) (*CloudConfig, error) {
	if filePath == "" {
		return nil, fmt.Errorf("failed to load cloud config: file path is empty")
	}

	var config CloudConfig
	configReader, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open cloud config file: %w, file path: %s", err, filePath)
	}
	defer configReader.Close()

	contents, err := io.ReadAll(configReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read cloud config file: %w, file path: %s", err, filePath)
	}

	if err := yaml.Unmarshal(contents, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cloud config: %w, file path: %s", err, filePath)
	}

	config.trimSpace()
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("failed to validate cloud config: %w, file contents: `%s`", err, string(contents))
	}

	return &config, nil
}

// SetUserAgent sets the user agent string to access Azure resources.
func (cfg *CloudConfig) SetUserAgent(userAgent string) {
	cfg.UserAgent = userAgent
}

func (cfg *CloudConfig) validate() error {
	if cfg.Cloud == "" {
		return fmt.Errorf("cloud is empty")
	}

	if cfg.Location == "" {
		return fmt.Errorf("location is empty")
	}

	if cfg.SubscriptionID == "" {
		return fmt.Errorf("subscription ID is empty")
	}

	if cfg.ResourceGroup == "" {
		return fmt.Errorf("resource group is empty")
	}

	if cfg.VnetName == "" {
		return fmt.Errorf("virtual network name is empty")
	}

	if cfg.VnetResourceGroup == "" {
		cfg.VnetResourceGroup = cfg.ResourceGroup
	}

	if !cfg.UseManagedIdentityExtension {
		if cfg.UserAssignedIdentityID != "" {
			return fmt.Errorf("useManagedIdentityExtension needs to be true when userAssignedIdentityID is provided")
		}
		if cfg.AADClientID == "" || cfg.AADClientSecret == "" {
			return fmt.Errorf("AAD client ID or AAD client secret is empty")
		}
	}

	if cfg.CloudProviderRateLimit {
		// Assign read rate limit defaults if no configuration was passed in.
		if cfg.CloudProviderRateLimitQPS == 0 {
			cfg.CloudProviderRateLimitQPS = consts.RateLimitQPSDefault
		}
		if cfg.CloudProviderRateLimitBucket == 0 {
			cfg.CloudProviderRateLimitBucket = consts.RateLimitBucketDefault
		}
		// Assign write rate limit defaults if no configuration was passed in.
		if cfg.CloudProviderRateLimitQPSWrite == 0 {
			cfg.CloudProviderRateLimitQPSWrite = cfg.CloudProviderRateLimitQPS
		}
		if cfg.CloudProviderRateLimitBucketWrite == 0 {
			cfg.CloudProviderRateLimitBucketWrite = cfg.CloudProviderRateLimitBucket
		}
	}

	return nil
}

func (cfg *CloudConfig) trimSpace() {
	cfg.Cloud = strings.TrimSpace(cfg.Cloud)
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.UserAgent = strings.TrimSpace(cfg.UserAgent)
	cfg.SubscriptionID = strings.TrimSpace(cfg.SubscriptionID)
	cfg.Location = strings.TrimSpace(cfg.Location)
	cfg.ResourceGroup = strings.TrimSpace(cfg.ResourceGroup)
	cfg.UserAssignedIdentityID = strings.TrimSpace(cfg.UserAssignedIdentityID)
	cfg.AADClientID = strings.TrimSpace(cfg.AADClientID)
	cfg.AADClientSecret = strings.TrimSpace(cfg.AADClientSecret)
	cfg.VnetName = strings.TrimSpace(cfg.VnetName)
	cfg.VnetResourceGroup = strings.TrimSpace(cfg.VnetResourceGroup)
}
