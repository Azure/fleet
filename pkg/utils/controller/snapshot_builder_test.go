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

package controller

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/test/utils/resource"
)

func TestSplitSelectedResources(t *testing.T) {
	// test service is 383 bytes in size.
	serviceResourceContent := *resource.ServiceResourceContentForTest(t)
	// test deployment 390 bytes in size.
	deploymentResourceContent := *resource.DeploymentResourceContentForTest(t)
	// test secret is 152 bytes in size.
	secretResourceContent := *resource.SecretResourceContentForTest(t)
	tests := []struct {
		name                       string
		selectedResourcesSizeLimit int
		selectedResources          []fleetv1beta1.ResourceContent
		wantSplitSelectedResources [][]fleetv1beta1.ResourceContent
	}{
		{
			name:                       "empty split selected resources - empty list of selectedResources",
			selectedResources:          []fleetv1beta1.ResourceContent{},
			wantSplitSelectedResources: nil,
		},
		{
			name:                       "selected resources don't cross individual clusterResourceSnapshot size limit",
			selectedResourcesSizeLimit: 1000,
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, serviceResourceContent, deploymentResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent, serviceResourceContent, deploymentResourceContent}},
		},
		{
			name:                       "selected resource cross clusterResourceSnapshot size limit - each resource in separate list, each resource is larger than the size limit",
			selectedResourcesSizeLimit: 100,
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, serviceResourceContent, deploymentResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent}, {serviceResourceContent}, {deploymentResourceContent}},
		},
		{
			name:                       "selected resources cross individual clusterResourceSnapshot size limit - each resource in separate list, any grouping of resources is larger than the size limit",
			selectedResourcesSizeLimit: 500,
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, serviceResourceContent, deploymentResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent}, {serviceResourceContent}, {deploymentResourceContent}},
		},
		{
			name:                       "selected resources cross individual clusterResourceSnapshot size limit - two resources in first list, one resource in second list",
			selectedResourcesSizeLimit: 600,
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, serviceResourceContent, deploymentResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent, serviceResourceContent}, {deploymentResourceContent}},
		},
		{
			name:                       "selected resources cross individual clusterResourceSnapshot size limit - one resource in first list, two resources in second list",
			selectedResourcesSizeLimit: 600,
			selectedResources:          []fleetv1beta1.ResourceContent{serviceResourceContent, deploymentResourceContent, secretResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{serviceResourceContent}, {deploymentResourceContent, secretResourceContent}},
		},
		{
			name:                       "single resource fits within size limit",
			selectedResourcesSizeLimit: 200,
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent}},
		},
		{
			name:                       "single resource larger than size limit",
			selectedResourcesSizeLimit: 100,
			selectedResources:          []fleetv1beta1.ResourceContent{serviceResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{serviceResourceContent}},
		},
		{
			name:                       "zero size limit - all resources in separate lists",
			selectedResourcesSizeLimit: 0,
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, serviceResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent}, {serviceResourceContent}},
		},
		{
			name:                       "exact size match - two resources exactly at limit",
			selectedResourcesSizeLimit: 535, // secret (152) + service (383) = 535
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, serviceResourceContent, deploymentResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent, serviceResourceContent}, {deploymentResourceContent}},
		},
		{
			name:                       "multiple small resources within limit",
			selectedResourcesSizeLimit: 1000,
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, secretResourceContent, secretResourceContent, secretResourceContent}, // 4 * 152 = 608
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent, secretResourceContent, secretResourceContent, secretResourceContent}},
		},
		{
			name:                       "multiple small resources exceed limit",
			selectedResourcesSizeLimit: 300,
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, secretResourceContent, secretResourceContent}, // 3 * 152 = 456
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent}, {secretResourceContent}, {secretResourceContent}},
		},
		{
			name:                       "multiple small resources - some fit together",
			selectedResourcesSizeLimit: 320, // Can fit 2 secrets (2 * 152 = 304)
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, secretResourceContent, secretResourceContent, secretResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent, secretResourceContent}, {secretResourceContent, secretResourceContent}},
		},
		{
			name:                       "alternating large and small resources",
			selectedResourcesSizeLimit: 600,
			selectedResources:          []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent, deploymentResourceContent, secretResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{serviceResourceContent, secretResourceContent}, {deploymentResourceContent, secretResourceContent}},
		},
		{
			name:                       "very large size limit - all resources in one list",
			selectedResourcesSizeLimit: 10000,
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, serviceResourceContent, deploymentResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent, serviceResourceContent, deploymentResourceContent}},
		},
		{
			name:                       "boundary case - size limit equals largest resource",
			selectedResourcesSizeLimit: 390, // deployment size
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, deploymentResourceContent, serviceResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent}, {deploymentResourceContent}, {serviceResourceContent}},
		},
		{
			name:                       "boundary case - size limit equals smallest resource",
			selectedResourcesSizeLimit: 152, // secret size
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, serviceResourceContent, deploymentResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent}, {serviceResourceContent}, {deploymentResourceContent}},
		},
		{
			name:                       "many resources with small limit",
			selectedResourcesSizeLimit: 200,
			selectedResources:          []fleetv1beta1.ResourceContent{secretResourceContent, secretResourceContent, secretResourceContent, secretResourceContent, secretResourceContent, secretResourceContent},
			wantSplitSelectedResources: [][]fleetv1beta1.ResourceContent{{secretResourceContent}, {secretResourceContent}, {secretResourceContent}, {secretResourceContent}, {secretResourceContent}, {secretResourceContent}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSplitSelectedResources := SplitSelectedResources(tc.selectedResources, tc.selectedResourcesSizeLimit)
			if diff := cmp.Diff(tc.wantSplitSelectedResources, gotSplitSelectedResources); diff != "" {
				t.Errorf("splitSelectedResources List() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
