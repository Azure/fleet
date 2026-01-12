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

package statusbackreporter

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	// The linter in use mistakenly recognizes some of the names as potential hardcoded credentials;
	// as a result, gosec linter warnings are suppressed for these variables.
	crpName1               = "crp-1"
	rpName1                = "rp-1"
	nsName                 = "work"
	clusterResEnvelopeName = "cluster-res-envelope-1"
	resEnvelopeName        = "res-envelope-1"
	cluster1               = "cluster-1"
	cluster2               = "cluster-2"

	crpWorkName1 = "crp-1-work"
	rpWorkName1  = "work.test.app-work" //nolint:gosec
	rpWorkName2  = "work.app-work"      //nolint:gosec
)

func TestMain(m *testing.M) {
	// Set up the scheme.
	if err := clientgoscheme.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add default set of APIs to the runtime scheme: %v", err)
	}
	if err := placementv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs (placement/v1beta1) to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestFormatResourceIdentifier tests the formatResourceIdentifier function.
func TestFormatResourceIdentifier(t *testing.T) {
	testCases := []struct {
		name               string
		resourceIdentifier *placementv1beta1.ResourceIdentifier
		wantIdStr          string
	}{
		{
			name: "cluster-scoped object (core API group)",
			resourceIdentifier: &placementv1beta1.ResourceIdentifier{
				Group:     "",
				Version:   "v1",
				Kind:      "Namespace",
				Namespace: "",
				Name:      nsName,
			},
			wantIdStr: "/v1/Namespace//work",
		},
		{
			name: "cluster-scoped object (non-core API group)",
			resourceIdentifier: &placementv1beta1.ResourceIdentifier{
				Group:     "rbac.authorization.k8s.io",
				Version:   "v1",
				Kind:      "ClusterRole",
				Namespace: "",
				Name:      "admin",
			},
			wantIdStr: "rbac.authorization.k8s.io/v1/ClusterRole//admin",
		},
		{
			name: "namespace-scoped object (core API group)",
			resourceIdentifier: &placementv1beta1.ResourceIdentifier{
				Group:     "",
				Version:   "v1",
				Kind:      "Pod",
				Namespace: "default",
				Name:      "nginx-pod",
			},
			wantIdStr: "/v1/Pod/default/nginx-pod",
		},
		{
			name: "namespace-scoped object (non-core API group)",
			resourceIdentifier: &placementv1beta1.ResourceIdentifier{
				Group:     "apps",
				Version:   "v1",
				Kind:      "Deployment",
				Namespace: "default",
				Name:      "nginx",
			},
			wantIdStr: "apps/v1/Deployment/default/nginx",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			idStr := formatResourceIdentifier(tc.resourceIdentifier)
			if !cmp.Equal(idStr, tc.wantIdStr) {
				t.Errorf("formatResourceIdentifier() = %v, want %v", idStr, tc.wantIdStr)
			}
		})
	}
}

// TestFormatWorkResourceIdentifier tests the formatWorkResourceIdentifier function.
func TestFormatWorkResourceIdentifier(t *testing.T) {
	testCases := []struct {
		name                   string
		workResourceIdentifier *placementv1beta1.WorkResourceIdentifier
		wantIdStr              string
	}{
		{
			name: "cluster-scoped object (core API group)",
			workResourceIdentifier: &placementv1beta1.WorkResourceIdentifier{
				Ordinal:   0,
				Group:     "",
				Version:   "v1",
				Kind:      "Namespace",
				Resource:  "namespaces",
				Namespace: "",
				Name:      "work",
			},
			wantIdStr: "/v1/Namespace//work",
		},
		{
			name: "cluster-scoped object (non-core API group)",
			workResourceIdentifier: &placementv1beta1.WorkResourceIdentifier{
				Ordinal:   1,
				Group:     "rbac.authorization.k8s.io",
				Version:   "v1",
				Kind:      "ClusterRole",
				Resource:  "clusterroles",
				Namespace: "",
				Name:      "admin",
			},
			wantIdStr: "rbac.authorization.k8s.io/v1/ClusterRole//admin",
		},
		{
			name: "namespace-scoped object (core API group)",
			workResourceIdentifier: &placementv1beta1.WorkResourceIdentifier{
				Ordinal:   2,
				Group:     "",
				Version:   "v1",
				Kind:      "Pod",
				Resource:  "pods",
				Namespace: "default",
				Name:      "nginx-pod",
			},
			wantIdStr: "/v1/Pod/default/nginx-pod",
		},
		{
			name: "namespace-scoped object (non-core API group)",
			workResourceIdentifier: &placementv1beta1.WorkResourceIdentifier{
				Ordinal:   3,
				Group:     "apps",
				Version:   "v1",
				Kind:      "Deployment",
				Resource:  "deployments",
				Namespace: "default",
				Name:      "nginx",
			},
			wantIdStr: "apps/v1/Deployment/default/nginx",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			idStr := formatWorkResourceIdentifier(tc.workResourceIdentifier)
			if !cmp.Equal(idStr, tc.wantIdStr) {
				t.Errorf("formatWorkResourceIdentifier() = %v, want %v", idStr, tc.wantIdStr)
			}
		})
	}
}

// TestPrepareIsResEnvelopedMap tests the prepareIsResEnvelopedMap function.
func TestPrepareIsResEnvelopedMap(t *testing.T) {
	testCases := []struct {
		name                  string
		placementObj          placementv1beta1.PlacementObj
		wantIsResEnvelopedMap map[string]bool
	}{
		{
			name: "CRP object with regular and enveloped objects",
			placementObj: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName1,
				},
				Status: placementv1beta1.PlacementStatus{
					SelectedResources: []placementv1beta1.ResourceIdentifier{
						{
							Group:     "",
							Version:   "v1",
							Kind:      "Namespace",
							Namespace: "",
							Name:      nsName,
						},
						{
							Group:     "rbac.authorization.k8s.io",
							Version:   "v1",
							Kind:      "ClusterRole",
							Namespace: "",
							Name:      "admin",
						},
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRoleBinding",
							Name:    "admin-users",
							Envelope: &placementv1beta1.EnvelopeIdentifier{
								Name: clusterResEnvelopeName,
								Type: placementv1beta1.ClusterResourceEnvelopeType,
							},
						},
					},
				},
			},
			wantIsResEnvelopedMap: map[string]bool{
				"/v1/Namespace//work":                                          false,
				"rbac.authorization.k8s.io/v1/ClusterRole//admin":              false,
				"rbac.authorization.k8s.io/v1/ClusterRoleBinding//admin-users": true,
			},
		},
		{
			name: "RP object with regular and enveloped objects",
			placementObj: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: nsName,
					Name:      rpName1,
				},
				Status: placementv1beta1.PlacementStatus{
					SelectedResources: []placementv1beta1.ResourceIdentifier{
						{
							Group:     "",
							Version:   "v1",
							Kind:      "Pod",
							Namespace: "default",
							Name:      "nginx-pod",
						},
						{
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Namespace: "default",
							Name:      "nginx",
						},
						{
							Group:     "apps",
							Version:   "v1",
							Kind:      "ResourceQuota",
							Namespace: "default",
							Name:      "all",
							Envelope: &placementv1beta1.EnvelopeIdentifier{
								Name: resEnvelopeName,
								Type: placementv1beta1.ResourceEnvelopeType,
							},
						},
					},
				},
			},
			wantIsResEnvelopedMap: map[string]bool{
				"/v1/Pod/default/nginx-pod":         false,
				"apps/v1/Deployment/default/nginx":  false,
				"apps/v1/ResourceQuota/default/all": true,
			},
		},
		{
			name: "empty map",
			placementObj: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: nsName,
					Name:      rpName1,
				},
				Status: placementv1beta1.PlacementStatus{
					SelectedResources: []placementv1beta1.ResourceIdentifier{},
				},
			},
			wantIsResEnvelopedMap: map[string]bool{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isResEnvelopedMap := prepareIsResEnvelopedMap(tc.placementObj)
			if diff := cmp.Diff(isResEnvelopedMap, tc.wantIsResEnvelopedMap); diff != "" {
				t.Errorf("prepareIsResEnvelopedMap() isResEnvelopedMaps mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestValidatePlacementObjectForOriginalResourceStatusBackReporting tests the validatePlacementObjectForOriginalResourceStatusBackReporting function.
func TestValidatePlacementObjectForOriginalResourceStatusBackReporting(t *testing.T) {
	testCases := []struct {
		name                string
		placementObj        placementv1beta1.PlacementObj
		work                *placementv1beta1.Work
		wantShouldSkip      bool
		wantErred           bool
		wantErrStrSubString string
		// The method returns the placement object as it is; for simplicity reasons the test spec here
		// will no longer check the returned placement object here.
	}{
		{
			name: "no placement tracking label",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpWorkName1,
				},
			},
			wantErred:           true,
			wantErrStrSubString: "the placement tracking label is absent or invalid",
		},
		{
			name: "empty placement tracking label",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpWorkName1,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: "",
					},
				},
			},
			wantErred:           true,
			wantErrStrSubString: "the placement tracking label is absent or invalid",
		},
		{
			name: "work associated with rp, invalid scheduling policy (nil)",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: rpWorkName1,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: rpName1,
						placementv1beta1.ParentNamespaceLabel:   nsName,
					},
				},
			},
			placementObj: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName1,
					Namespace: nsName,
				},
				Spec: placementv1beta1.PlacementSpec{
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type:        placementv1beta1.ReportBackStrategyTypeMirror,
							Destination: ptr.To(placementv1beta1.ReportBackDestinationOriginalResource),
						},
					},
				},
			},
			wantErred:           true,
			wantErrStrSubString: "no scheduling policy specified (the PickAll type is in use)",
		},
		{
			name: "work associated with crp, invalid scheduling policy (nil)",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpWorkName1,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: crpName1,
					},
				},
			},
			placementObj: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName1,
				},
				Spec: placementv1beta1.PlacementSpec{
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type:        placementv1beta1.ReportBackStrategyTypeMirror,
							Destination: ptr.To(placementv1beta1.ReportBackDestinationOriginalResource),
						},
					},
				},
			},
			wantErred:           true,
			wantErrStrSubString: "no scheduling policy specified (the PickAll type is in use)",
		},
		{
			name: "work associated with rp, rp not found",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: rpWorkName2,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: rpName1,
						placementv1beta1.ParentNamespaceLabel:   nsName,
					},
				},
			},
			wantErred:           true,
			wantErrStrSubString: "failed to retrieve RP object",
		},
		{
			name: "work associated with crp, crp not found",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpWorkName1,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: crpName1,
					},
				},
			},
			wantErred:           true,
			wantErrStrSubString: "failed to retrieve CRP object",
		},
		{
			name: "work associated with rp, with PickAll scheduling policy",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: rpWorkName2,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: rpName1,
						placementv1beta1.ParentNamespaceLabel:   nsName,
					},
				},
			},
			placementObj: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName1,
					Namespace: nsName,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type:        placementv1beta1.ReportBackStrategyTypeMirror,
							Destination: ptr.To(placementv1beta1.ReportBackDestinationOriginalResource),
						},
					},
				},
			},
			wantErred:           true,
			wantErrStrSubString: "the scheduling policy in use is of the PickAll type",
		},
		{
			name: "work associated with rp, with PickFixed placement type and more than 1 selected clusters",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: rpWorkName2,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: rpName1,
						placementv1beta1.ParentNamespaceLabel:   nsName,
					},
				},
			},
			placementObj: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName1,
					Namespace: nsName,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames: []string{
							cluster1,
							cluster2,
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type:        placementv1beta1.ReportBackStrategyTypeMirror,
							Destination: ptr.To(placementv1beta1.ReportBackDestinationOriginalResource),
						},
					},
				},
			},
			wantErred:           true,
			wantErrStrSubString: "the scheduling policy in use is of the PickFixed type, but it has more than one target cluster",
		},
		{
			name: "work associated with rp, with PickN placement type and more than 1 clusters to select",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: rpWorkName2,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: rpName1,
						placementv1beta1.ParentNamespaceLabel:   nsName,
					},
				},
			},
			placementObj: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName1,
					Namespace: nsName,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(2)),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type:        placementv1beta1.ReportBackStrategyTypeMirror,
							Destination: ptr.To(placementv1beta1.ReportBackDestinationOriginalResource),
						},
					},
				},
			},
			wantErred:           true,
			wantErrStrSubString: "the scheduling policy in use is of the PickN type, but the number of target clusters is not set to 1",
		},
		{
			// Normally this will never occur.
			name: "work associated with rp, with PickN placement type and no number of target clusters",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: rpWorkName2,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: rpName1,
						placementv1beta1.ParentNamespaceLabel:   nsName,
					},
				},
			},
			placementObj: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName1,
					Namespace: nsName,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickNPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type:        placementv1beta1.ReportBackStrategyTypeMirror,
							Destination: ptr.To(placementv1beta1.ReportBackDestinationOriginalResource),
						},
					},
				},
			},
			wantErred:           true,
			wantErrStrSubString: "the scheduling policy in use is of the PickN type, but no number of target clusters is specified",
		},
		{
			name: "work associated with crp, with PickFixed placement type and one selected cluster",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpWorkName1,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: crpName1,
					},
				},
			},
			placementObj: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName1,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames: []string{
							cluster1,
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type:        placementv1beta1.ReportBackStrategyTypeMirror,
							Destination: ptr.To(placementv1beta1.ReportBackDestinationOriginalResource),
						},
					},
				},
			},
		},
		{
			name: "work associated with crp, with PickN placement type and 1 target cluster to select",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpWorkName1,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: crpName1,
					},
				},
			},
			placementObj: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName1,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type:        placementv1beta1.ReportBackStrategyTypeMirror,
							Destination: ptr.To(placementv1beta1.ReportBackDestinationOriginalResource),
						},
					},
				},
			},
		},
		{
			name: "work associated with rp, no report back strategy (nil)",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: rpWorkName2,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: rpName1,
						placementv1beta1.ParentNamespaceLabel:   nsName,
					},
				},
			},
			placementObj: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName1,
					Namespace: nsName,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
					},
					Strategy: placementv1beta1.RolloutStrategy{},
				},
			},
			wantShouldSkip: true,
		},
		{
			name: "work associated with rp, report back strategy not set to Mirror type",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: rpWorkName2,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: rpName1,
						placementv1beta1.ParentNamespaceLabel:   nsName,
					},
				},
			},
			placementObj: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName1,
					Namespace: nsName,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type: placementv1beta1.ReportBackStrategyTypeDisabled,
						},
					},
				},
			},
			wantShouldSkip: true,
		},
		{
			name: "work associated with crp, report back strategy destination not set",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpWorkName1,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: crpName1,
					},
				},
			},
			placementObj: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName1,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type: placementv1beta1.ReportBackStrategyTypeMirror,
						},
					},
				},
			},
			wantShouldSkip: true,
		},
		{
			name: "work associated with crp, report back strategy destination not set to OriginalResource",
			work: &placementv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpWorkName1,
					Labels: map[string]string{
						placementv1beta1.PlacementTrackingLabel: crpName1,
					},
				},
			},
			placementObj: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName1,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type:        placementv1beta1.ReportBackStrategyTypeMirror,
							Destination: ptr.To(placementv1beta1.ReportBackDestinationWorkAPI),
						},
					},
				},
			},
			wantShouldSkip: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fakeClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tc.placementObj != nil {
				fakeClientBuilder.WithObjects(tc.placementObj)
			}
			fakeClient := fakeClientBuilder.Build()

			r := NewReconciler(fakeClient, nil, nil)

			_, shouldSkip, err := r.validatePlacementObjectForOriginalResourceStatusBackReporting(ctx, tc.work)
			if tc.wantErred {
				if err == nil {
					t.Fatalf("validatePlacementObjectForOriginalResourceStatusBackReporting() = nil, want erred")
					return
				}
				if !strings.Contains(err.Error(), tc.wantErrStrSubString) {
					t.Fatalf("validatePlacementObjectForOriginalResourceStatusBackReporting() = %v, want to have prefix %s", err, tc.wantErrStrSubString)
					return
				}
			}
			if shouldSkip != tc.wantShouldSkip {
				t.Errorf("validatePlacementObjectForOriginalResourceStatusBackReporting() shouldSkip = %v, want %v", shouldSkip, tc.wantShouldSkip)
			}
		})
	}
}
