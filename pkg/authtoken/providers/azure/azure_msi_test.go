/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package azure

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	testCases := []struct {
		name            string
		clientID        string
		scope           string
		envValue        string
		shouldSetEnv    bool
		expectedScope   string
	}{
		{
			name:          "with direct scope",
			clientID:      "test-client-id",
			scope:         "custom-scope",
			shouldSetEnv:  false,
			expectedScope: "custom-scope",
		},
		{
			name:          "with env var scope",
			clientID:      "test-client-id",
			scope:         "",
			envValue:      "env-var-scope",
			shouldSetEnv:  true,
			expectedScope: "env-var-scope",
		},
		{
			name:          "with default scope",
			clientID:      "test-client-id",
			scope:         "",
			shouldSetEnv:  false,
			expectedScope: aksScope,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set environment variable if needed
			if tc.shouldSetEnv {
				os.Setenv(aksScopeEnvVarName, tc.envValue)
				defer os.Unsetenv(aksScopeEnvVarName)
			}

			// Create provider
			provider := New(tc.clientID, tc.scope)

			// Assert it's the correct type
			azProvider, ok := provider.(*AuthTokenProvider)
			assert.True(t, ok, "Provider should be of type *AuthTokenProvider")

			// Assert the scope is set correctly
			assert.Equal(t, tc.expectedScope, azProvider.Scope, "Scope should be set correctly")
		})
	}
}