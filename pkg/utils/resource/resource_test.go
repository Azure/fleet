/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package resource

import (
	"testing"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestHashOf(t *testing.T) {
	testCases := []struct {
		name  string
		input any
	}{
		{
			name: "resource snapshot spec",
			input: &placementv1beta1.ResourceSnapshotSpec{
				SelectedResources: []placementv1beta1.ResourceContent{},
			},
		},
		{
			name:  "nil resource",
			input: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := HashOf(tc.input)
			if err != nil {
				t.Fatalf("HashOf() got error %v, want nil", err)
			}
			if len(got) == 0 {
				t.Errorf("HashOf() got empty, want not empty")
			}
		})
	}
}
