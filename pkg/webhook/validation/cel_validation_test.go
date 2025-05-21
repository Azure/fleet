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

package validation

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
)

func TestCheckCRDGroupCEL(t *testing.T) {
	env, err := cel.NewEnv(
		cel.Variable("group", cel.StringType),
		cel.Variable("fleetCRDGroups", cel.ListType(cel.StringType)),
	)
	assert.NoError(t, err)

	expression := "group in fleetCRDGroups"
	parsed, issues := env.Compile(expression)
	assert.Nil(t, issues)
	assert.NotNil(t, parsed)

	program, err := env.Program(parsed)
	assert.NoError(t, err)

	tests := []struct {
		name           string
		group          string
		fleetCRDGroups []string
		expected       bool
	}{
		{
			name:           "known group",
			group:          "fleet.azure.com",
			fleetCRDGroups: []string{"networking.fleet.azure.com", "fleet.azure.com"},
			expected:       true,
		},
		{
			name:           "unknown group",
			group:          "example.com",
			fleetCRDGroups: []string{"networking.fleet.azure.com", "fleet.azure.com"},
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, _, err := program.Eval(map[string]interface{}{
				"group":          tt.group,
				"fleetCRDGroups": tt.fleetCRDGroups,
			})
			assert.NoError(t, err)
			result, ok := val.Value().(bool)
			assert.True(t, ok)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsAdminGroupUserOrWhiteListedUserCEL(t *testing.T) {
	env, err := cel.NewEnv(
		cel.Variable("username", cel.StringType),
		cel.Variable("userGroups", cel.ListType(cel.StringType)),
		cel.Variable("whiteListedUsers", cel.ListType(cel.StringType)),
		cel.Variable("adminGroups", cel.ListType(cel.StringType)),
	)
	assert.NoError(t, err)

	expression := "username in whiteListedUsers || userGroups.exists(g, g in adminGroups)"
	parsed, issues := env.Compile(expression)
	assert.Nil(t, issues)
	assert.NotNil(t, parsed)

	program, err := env.Program(parsed)
	assert.NoError(t, err)

	tests := []struct {
		name            string
		username        string
		userGroups      []string
		whiteListedUsers []string
		adminGroups     []string
		expected        bool
	}{
		{
			name:            "whitelisted user",
			username:        "allowed-user",
			userGroups:      []string{"regular-group"},
			whiteListedUsers: []string{"allowed-user", "another-user"},
			adminGroups:     []string{"system:masters", "kubeadm:cluster-admins"},
			expected:        true,
		},
		{
			name:            "admin group user",
			username:        "admin-user",
			userGroups:      []string{"system:masters", "regular-group"},
			whiteListedUsers: []string{"allowed-user"},
			adminGroups:     []string{"system:masters", "kubeadm:cluster-admins"},
			expected:        true,
		},
		{
			name:            "regular user",
			username:        "regular-user",
			userGroups:      []string{"regular-group"},
			whiteListedUsers: []string{"allowed-user"},
			adminGroups:     []string{"system:masters", "kubeadm:cluster-admins"},
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, _, err := program.Eval(map[string]interface{}{
				"username":        tt.username,
				"userGroups":      tt.userGroups,
				"whiteListedUsers": tt.whiteListedUsers,
				"adminGroups":     tt.adminGroups,
			})
			assert.NoError(t, err)
			result, ok := val.Value().(bool)
			assert.True(t, ok)
			assert.Equal(t, tt.expected, result)
		})
	}
}