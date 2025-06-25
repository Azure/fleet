/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package utils contains utility functions for the CRD installer.
package utils

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	lessFunc = func(s1, s2 string) bool {
		return s1 < s2
	}
)

// Test using the actual config/crd/bases directory.
func TestCollectCRDFileNamesWithActualPath(t *testing.T) {
	// Use a path relative to the project root when running with make local-unit-test
	const realCRDPath = "config/crd/bases"

	// Skip this test if the directory doesn't exist.
	if _, err := os.Stat(realCRDPath); os.IsNotExist(err) {
		// Try the original path (for when tests are run from the package directory).
		const fallbackPath = "../../../config/crd/bases"
		if _, fallBackPathErr := os.Stat(fallbackPath); os.IsNotExist(fallBackPathErr) {
			t.Skipf("Skipping test: neither %s nor %s exist", realCRDPath, fallbackPath)
		} else {
			// Use the fallback path if it exists.
			runTest(t, fallbackPath)
		}
	} else {
		// Use the primary path if it exists.
		runTest(t, realCRDPath)
	}
}

// runTest contains the actual test logic, separated to allow running with different paths.
func runTest(t *testing.T, crdPath string) {
	tests := []struct {
		name           string
		mode           string
		wantedCRDNames []string
		wantError      bool
	}{
		{
			name: "hub mode v1beta1 with actual directory",
			mode: "hub",
			wantedCRDNames: []string{
				"memberclusters.cluster.kubernetes-fleet.io",
				"internalmemberclusters.cluster.kubernetes-fleet.io",
				"clusterapprovalrequests.placement.kubernetes-fleet.io",
				"clusterresourcebindings.placement.kubernetes-fleet.io",
				"clusterresourceenvelopes.placement.kubernetes-fleet.io",
				"clusterresourceplacements.placement.kubernetes-fleet.io",
				"clusterresourceoverrides.placement.kubernetes-fleet.io",
				"clusterresourceoverridesnapshots.placement.kubernetes-fleet.io",
				"clusterresourceplacementdisruptionbudgets.placement.kubernetes-fleet.io",
				"clusterresourceplacementevictions.placement.kubernetes-fleet.io",
				"clusterresourcesnapshots.placement.kubernetes-fleet.io",
				"clusterschedulingpolicysnapshots.placement.kubernetes-fleet.io",
				"clusterstagedupdateruns.placement.kubernetes-fleet.io",
				"clusterstagedupdatestrategies.placement.kubernetes-fleet.io",
				"resourcebindings.placement.kubernetes-fleet.io",
				"resourceenvelopes.placement.kubernetes-fleet.io",
				"resourceoverrides.placement.kubernetes-fleet.io",
				"resourceoverridesnapshots.placement.kubernetes-fleet.io",
				"resourceplacements.placement.kubernetes-fleet.io",
				"resourcesnapshots.placement.kubernetes-fleet.io",
				"schedulingpolicysnapshots.placement.kubernetes-fleet.io",
				"works.placement.kubernetes-fleet.io",
				"clusterprofiles.multicluster.x-k8s.io",
			},
			wantError: false,
		},
		{
			name: "member mode v1beta1 with actual directory",
			mode: "member",
			wantedCRDNames: []string{
				"appliedworks.placement.kubernetes-fleet.io",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		scheme := runtime.NewScheme()
		if err := apiextensionsv1.AddToScheme(scheme); err != nil {
			t.Fatalf("Failed to add apiextensions scheme: %v", err)
		}
		t.Run(tt.name, func(t *testing.T) {
			// Call the function.
			gotCRDs, err := CollectCRDs(crdPath, tt.mode, scheme)
			if (err != nil) != tt.wantError {
				t.Errorf("collectCRDs() error = %v, wantError %v", err, tt.wantError)
			}
			gotCRDNames := make([]string, len(gotCRDs))
			for i, crd := range gotCRDs {
				gotCRDNames[i] = crd.Name
			}
			// Sort the names for comparison.
			if diff := cmp.Diff(tt.wantedCRDNames, gotCRDNames, cmpopts.SortSlices(lessFunc)); diff != "" {
				t.Errorf("CRD names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
