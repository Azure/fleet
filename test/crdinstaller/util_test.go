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

package crdinstaller

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

	cmdCRDInstaller "go.goms.io/fleet/cmd/crdinstaller/utils"
)

// Test using the actual config/crd/bases directory
func TestCollectCRDFileNamesWithActualPath(t *testing.T) {
	const realCRDPath = "../../config/crd/bases"

	// Skip this test if the directory doesn't exist
	if _, err := os.Stat(realCRDPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: directory %s does not exist", realCRDPath)
	}

	tests := []struct {
		name              string
		mode              string
		enablev1beta1API  bool
		enablev1alpha1API bool
		wantedCRDFiles    map[string]bool
		wantError         bool
	}{
		{
			name: "hub mode v1beta1 with actual directory",
			mode: "hub",
			wantedCRDFiles: map[string]bool{
				"../../config/crd/bases/cluster.kubernetes-fleet.io_memberclusters.yaml":                              true,
				"../../config/crd/bases/cluster.kubernetes-fleet.io_internalmemberclusters.yaml":                      true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterapprovalrequests.yaml":                   true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterresourcebindings.yaml":                   true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterresourceenvelopes.yaml":                  true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterresourceplacements.yaml":                 true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterresourceoverrides.yaml":                  true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterresourceoverridesnapshots.yaml":          true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterresourceplacementdisruptionbudgets.yaml": true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterresourceplacementevictions.yaml":         true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterresourcesnapshots.yaml":                  true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterschedulingpolicysnapshots.yaml":          true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterstagedupdateruns.yaml":                   true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_clusterstagedupdatestrategies.yaml":             true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_resourceenvelopes.yaml":                         true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_resourceoverrides.yaml":                         true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_resourceoverridesnapshots.yaml":                 true,
				"../../config/crd/bases/placement.kubernetes-fleet.io_works.yaml":                                     true,
				"../../config/crd/bases/multicluster.x-k8s.io_clusterprofiles.yaml":                                   true,
			},
			wantError: false,
		},
		{
			name: "member mode v1beta1 with actual directory",
			mode: "member",
			wantedCRDFiles: map[string]bool{
				"../../config/crd/bases/placement.kubernetes-fleet.io_appliedworks.yaml": true,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function
			gotCRDFiles, err := cmdCRDInstaller.CollectCRDFileNames(realCRDPath, tt.mode)
			if (err != nil) != tt.wantError {
				t.Errorf("collectCRDFileNames() error = %v, wantError %v", err, tt.wantError)
			}
			if diff := cmp.Diff(tt.wantedCRDFiles, gotCRDFiles); diff != "" {
				t.Errorf("removeWaitTimeFromUpdateRunStatus() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
