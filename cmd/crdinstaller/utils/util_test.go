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

// Package utils contains utility functions for the CRD installer.
package utils

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Test using the actual config/crd/bases directory
func TestCollectCRDFileNamesWithActualPath(t *testing.T) {
	// Use a path relative to the project root when running with make local-unit-test
	const realCRDPath = "config/crd/bases"

	// Skip this test if the directory doesn't exist
	if _, err := os.Stat(realCRDPath); os.IsNotExist(err) {
		// Try the original path (for when tests are run from the package directory)
		const fallbackPath = "../../../config/crd/bases"
		if _, fallBackPathErr := os.Stat(fallbackPath); os.IsNotExist(fallBackPathErr) {
			t.Skipf("Skipping test: neither %s nor %s exist", realCRDPath, fallbackPath)
		} else {
			// Use the fallback path if it exists
			runTest(t, fallbackPath)
		}
	} else {
		// Use the primary path if it exists
		runTest(t, realCRDPath)
	}
}

// runTest contains the actual test logic, separated to allow running with different paths
func runTest(t *testing.T, crdPath string) {
	tests := []struct {
		name           string
		mode           string
		wantedCRDFiles map[string]bool
		wantError      bool
	}{
		{
			name: "hub mode v1beta1 with actual directory",
			mode: "hub",
			wantedCRDFiles: map[string]bool{
				crdPath + "/cluster.kubernetes-fleet.io_memberclusters.yaml":                              true,
				crdPath + "/cluster.kubernetes-fleet.io_internalmemberclusters.yaml":                      true,
				crdPath + "/placement.kubernetes-fleet.io_clusterapprovalrequests.yaml":                   true,
				crdPath + "/placement.kubernetes-fleet.io_clusterresourcebindings.yaml":                   true,
				crdPath + "/placement.kubernetes-fleet.io_clusterresourceenvelopes.yaml":                  true,
				crdPath + "/placement.kubernetes-fleet.io_clusterresourceplacements.yaml":                 true,
				crdPath + "/placement.kubernetes-fleet.io_clusterresourceoverrides.yaml":                  true,
				crdPath + "/placement.kubernetes-fleet.io_clusterresourceoverridesnapshots.yaml":          true,
				crdPath + "/placement.kubernetes-fleet.io_clusterresourceplacementdisruptionbudgets.yaml": true,
				crdPath + "/placement.kubernetes-fleet.io_clusterresourceplacementevictions.yaml":         true,
				crdPath + "/placement.kubernetes-fleet.io_clusterresourcesnapshots.yaml":                  true,
				crdPath + "/placement.kubernetes-fleet.io_clusterschedulingpolicysnapshots.yaml":          true,
				crdPath + "/placement.kubernetes-fleet.io_clusterstagedupdateruns.yaml":                   true,
				crdPath + "/placement.kubernetes-fleet.io_clusterstagedupdatestrategies.yaml":             true,
				crdPath + "/placement.kubernetes-fleet.io_resourceenvelopes.yaml":                         true,
				crdPath + "/placement.kubernetes-fleet.io_resourceoverrides.yaml":                         true,
				crdPath + "/placement.kubernetes-fleet.io_resourceoverridesnapshots.yaml":                 true,
				crdPath + "/placement.kubernetes-fleet.io_works.yaml":                                     true,
				crdPath + "/multicluster.x-k8s.io_clusterprofiles.yaml":                                   true,
			},
			wantError: false,
		},
		{
			name: "member mode v1beta1 with actual directory",
			mode: "member",
			wantedCRDFiles: map[string]bool{
				crdPath + "/placement.kubernetes-fleet.io_appliedworks.yaml": true,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function
			gotCRDFiles, err := CollectCRDFileNames(crdPath, tt.mode)
			if (err != nil) != tt.wantError {
				t.Errorf("collectCRDFileNames() error = %v, wantError %v", err, tt.wantError)
			}
			if diff := cmp.Diff(tt.wantedCRDFiles, gotCRDFiles); diff != "" {
				t.Errorf("removeWaitTimeFromUpdateRunStatus() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

