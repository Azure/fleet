package azure

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestTrimSpace(t *testing.T) {
	t.Run("test spaces are trimmed", func(t *testing.T) {
		config := PropertyCheckerConfig{
			ComputeServiceAddressWithBasePath: "  test  ",
		}

		want := PropertyCheckerConfig{
			ComputeServiceAddressWithBasePath: "test",
		}
		config.trimSpace()
		if diff := cmp.Diff(config, want); diff != "" {
			t.Errorf("trimSpace() mismatch (-got +want):\n%s", diff)
		}
	})
}

func TestValidate(t *testing.T) {
	tests := map[string]struct {
		config     *PropertyCheckerConfig
		wantConfig *PropertyCheckerConfig
		wantPass   bool
	}{
		"valid config": {
			config: &PropertyCheckerConfig{
				ComputeServiceAddressWithBasePath: "https://localhost:8090/",
			},
			wantConfig: &PropertyCheckerConfig{
				ComputeServiceAddressWithBasePath: "https://localhost:8090/",
			},
			wantPass: true,
		},
		"ComputeServiceAddressWithBasePath empty": {
			config: &PropertyCheckerConfig{
				ComputeServiceAddressWithBasePath: "",
			},
			wantPass: false,
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

func TestNewPropertyCheckerConfigFromFile(t *testing.T) {
	tests := map[string]struct {
		filePath   string
		wantErr    bool
		wantConfig *PropertyCheckerConfig
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
			wantConfig: &PropertyCheckerConfig{
				ComputeServiceAddressWithBasePath: "http://localhost:8421/compute",
			},
			wantErr: false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			config, err := NewPropertyCheckerConfigFromFile(test.filePath)
			if got := err != nil; got != test.wantErr {
				t.Fatalf("Failed to run NewPropertyCheckerConfigFromFile(%s): got %v, want %v", test.filePath, got, test.wantErr)
			}
			if diff := cmp.Diff(config, test.wantConfig); diff != "" {
				t.Errorf("NewPropertyCheckerConfigFromFile(%s) mismatch (-got +want):\n%s", test.filePath, diff)
			}
		})
	}
}
