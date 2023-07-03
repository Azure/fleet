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
	crpName     = "app"
	clusterName = "bravelion"

	longName = "c7t2c6oppjnryqcihwweexeobs7tlmf08ha4qb5htc4cifzpalhb5ec2lbh3" +
		"j73reciaz2f0jfd2rl5qba6rzuuwgyw6d9e6la19bo89k41lphln4s4dy1gr" +
		"h1dvua17iu4ro61dxo91ayovns8cgnmshlsflmi68e3najm7dw5dqe17pih7" +
		"up0dtyvrqxyp90sxedbf"
)

// TO-DO (chenyu1): Expand the test cases as development proceeds.

// TestClusterResourceBindingUniqueName tests the ClusterResourceBindingUniqueName function.
func TestClusterResourceBindingUniqueName(t *testing.T) {
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
			wantPrefix:  fmt.Sprintf("%s-%s", longName[:122], longName[:122]),
			wantLength:  252,
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
					t.Errorf("ClusterResourceBindingUniqueName(%s, %s) = %v, %v, want error", tc.crpName, tc.clusterName, name, err)
				}
				return
			}
			if err != nil {
				t.Errorf("ClusterResourceBindingUniqueName(%s, %s) = %v, %v, want no error", tc.crpName, tc.clusterName, name, err)
			}
			if !strings.HasPrefix(name, tc.wantPrefix) {
				t.Errorf("ClusterResourceBindingUniqueName(%s, %s) = %s, want to have prefix %s", tc.crpName, tc.clusterName, name, tc.wantPrefix)
			}
			if len(name) != tc.wantLength {
				t.Errorf("ClusterResourceBindingUniqueName(%s, %s) = %s, want to have length %d", tc.crpName, tc.clusterName, name, tc.wantLength)
			}
		})
	}
}
