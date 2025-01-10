/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package uniquename

import (
	"fmt"
	"strings"
	"testing"
)

const (
	crpName       = "app"
	clusterName   = "bravelion"
	updateRunName = "updaterun"
	stageName     = "stage"

	longName          = "c7t2c6oppjnryqcihwweexeobs7tlmf08ha4qb5htc4cifzpalhb5ec2lbh3et8dcbgh9w"
	longSubdomainName = longName + longName // len = 140
)

// TO-DO (chenyu1): Expand the test cases as development proceeds.

// TestNewClusterResourceBindingName tests the NewClusterResourceBindingName function.
func TestNewClusterResourceBindingName(t *testing.T) {
	testCases := []struct {
		name           string
		crpName        string
		clusterName    string
		wantPrefix     string
		wantLength     int
		expectedToFail bool
	}{
		{
			name:        "valid name",
			crpName:     crpName,
			clusterName: clusterName,
			wantPrefix:  fmt.Sprintf("%s-%s", crpName, clusterName),
			wantLength:  len(crpName) + len(clusterName) + 2 + uuidLength,
		},
		{
			name:        "valid name (truncated)",
			crpName:     longName,
			clusterName: longName,
			wantPrefix:  fmt.Sprintf("%s-%s", longName[:26], longName[:27]),
			wantLength:  63,
		},
		{
			name:           "invalid name",
			crpName:        crpName,
			clusterName:    clusterName + "!",
			expectedToFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			name, err := NewClusterResourceBindingName(tc.crpName, tc.clusterName)

			if tc.expectedToFail {
				if err == nil {
					t.Errorf("NewClusterResourceBindingName(%s, %s) = %v, %v, want error", tc.crpName, tc.clusterName, name, err)
				}
				return
			}
			if err != nil {
				t.Errorf("NewClusterResourceBindingName(%s, %s) = %v, %v, want no error", tc.crpName, tc.clusterName, name, err)
			}
			if !strings.HasPrefix(name, tc.wantPrefix) {
				t.Errorf("NewClusterResourceBindingName(%s, %s) = %s, want to have prefix %s", tc.crpName, tc.clusterName, name, tc.wantPrefix)
			}
			if len(name) != tc.wantLength {
				t.Errorf("NewClusterResourceBindingName(%s, %s) = %s, want to have length %d", tc.crpName, tc.clusterName, name, tc.wantLength)
			}
		})
	}
}

// NewClusterApprovalRequestName tests the NewClusterApprovalRequestName function.
func TestNewClusterApprovalRequestName(t *testing.T) {
	testCases := []struct {
		name           string
		updateRunName  string
		stageName      string
		wantPrefix     string
		wantLength     int
		expectedToFail bool
	}{
		{
			name:          "valid name",
			updateRunName: updateRunName,
			stageName:     stageName,
			wantPrefix:    fmt.Sprintf("%s-%s", updateRunName, stageName),
			wantLength:    len(updateRunName) + len(stageName) + 2 + uuidLength,
		},
		{
			name:          "name with capital letters",
			updateRunName: updateRunName,
			stageName:     strings.ToUpper(stageName),
			wantPrefix:    fmt.Sprintf("%s-%s", updateRunName, stageName),
			wantLength:    len(updateRunName) + len(stageName) + 2 + uuidLength,
		},
		{
			name:          "valid name (both truncated)",
			updateRunName: longSubdomainName,
			stageName:     longSubdomainName,
			wantPrefix:    fmt.Sprintf("%s-%s", longSubdomainName[:121], longSubdomainName[:122]),
			wantLength:    253,
		},
		{
			name:          "valid name (updateRunName truncated)",
			updateRunName: longSubdomainName,
			stageName:     stageName,
			wantPrefix:    fmt.Sprintf("%s-%s", longSubdomainName[:121], stageName),
			wantLength:    121 + len(stageName) + 2 + uuidLength,
		},
		{
			name:          "valid name (stageName truncated)",
			updateRunName: updateRunName,
			stageName:     longSubdomainName,
			wantPrefix:    fmt.Sprintf("%s-%s", updateRunName, longSubdomainName[:122]),
			wantLength:    122 + len(updateRunName) + 2 + uuidLength,
		},
		{
			name:           "invalid name with special characters",
			updateRunName:  updateRunName,
			stageName:      stageName + "!",
			expectedToFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			name, err := NewClusterApprovalRequestName(tc.updateRunName, tc.stageName)

			if tc.expectedToFail {
				if err == nil {
					t.Errorf("NewClusterApprovalRequestName(%s, %s) = %v, %v, want error", tc.updateRunName, tc.stageName, name, err)
				}
				return
			}
			if err != nil {
				t.Errorf("NewClusterApprovalRequestName(%s, %s) = %v, %v, want no error", tc.updateRunName, tc.stageName, name, err)
			}
			if !strings.HasPrefix(name, tc.wantPrefix) {
				t.Errorf("NewClusterApprovalRequestName(%s, %s) = %s, want to have prefix %s", tc.updateRunName, tc.stageName, name, tc.wantPrefix)
			}
			if len(name) != tc.wantLength {
				t.Errorf("NewClusterApprovalRequestName(%s, %s) = %s, got length %d, want to have length %d", tc.updateRunName, tc.stageName, name, len(name), tc.wantLength)
			}
		})
	}
}
