package azure

import (
	"fmt"
	"io"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
)

type PropertyCheckerConfig struct {
	// ComputeServiceAddressWithBasePath is the compute service endpoint with base path.
	ComputeServiceAddressWithBasePath string `json:"computeServiceAddressWithBasePath,omitempty" mapstructure:"computeServiceAddressWithBasePath,omitempty"`
}

// NewPropertyCheckerConfigFromFile loads the configuration from the specified file path.
func NewPropertyCheckerConfigFromFile(configFilePath string) (*PropertyCheckerConfig, error) {
	if configFilePath == "" {
		return nil, fmt.Errorf("azure property checker config file path is required when azure property checker is enabled")
	}

	var config PropertyCheckerConfig
	configReader, err := os.Open(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open azure property checker config file %s: %w", configFilePath, err)
	}
	defer configReader.Close()

	contents, err := io.ReadAll(configReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read azure property checker config file %s: %w", configFilePath, err)
	}

	if err := yaml.Unmarshal(contents, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal azure property checker config: %w", err)
	}

	config.trimSpace()
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("failed to validate azure property checker config: %w, file contents: `%s`", err, string(contents))
	}
	return &config, nil
}

// validateConfig validates the loaded configuration.
func (pcc *PropertyCheckerConfig) validate() error {
	if pcc == nil {
		return fmt.Errorf("azure property checker config is nil")
	}

	if pcc.ComputeServiceAddressWithBasePath == "" {
		return fmt.Errorf("computeServiceAddressWithBasePath is required in azure property checker config")
	}

	return nil
}

func (pcc *PropertyCheckerConfig) trimSpace() {
	pcc.ComputeServiceAddressWithBasePath = strings.TrimSpace(pcc.ComputeServiceAddressWithBasePath)
}
