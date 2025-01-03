/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"fmt"
	"testing"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// TestFormatWRIString tests the formatWRIString function.
func TestFormatWRIString(t *testing.T) {
	testCases := []struct {
		name          string
		wri           *fleetv1beta1.WorkResourceIdentifier
		wantWRIString string
		wantErred     bool
	}{
		{
			name: "ordinal only",
			wri: &fleetv1beta1.WorkResourceIdentifier{
				Ordinal: 0,
			},
			wantErred: true,
		},
		{
			name:          "regular object",
			wri:           deployWRI(2, nsName, deployName),
			wantWRIString: fmt.Sprintf("GV=apps/v1, Kind=Deployment, Namespace=%s, Name=%s", nsName, deployName),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			wriString, err := formatWRIString(tc.wri)
			if tc.wantErred {
				if err == nil {
					t.Errorf("formatWRIString() = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("formatWRIString() = %v, want no error", err)
			}

			if wriString != tc.wantWRIString {
				t.Errorf("formatWRIString() mismatches: got %q, want %q", wriString, tc.wantWRIString)
			}
		})
	}
}

// TestIsPlacedByFleetInDuplicate tests the isPlacedByFleetInDuplicate function.
func TestIsPlacedByFleetInDuplicate(t *testing.T) {
	testCases := []struct {
		name string
	}{
		{
			name: "in duplicate",
		},
		{
			name: "not in duplicate",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
		})
	}
}
