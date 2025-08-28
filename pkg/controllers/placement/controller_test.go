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
package placement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/defaulter"
	"go.goms.io/fleet/test/utils/resource"
)

const (
	testCRPName         = "my-crp"
	placementGeneration = 15
)

var (
	fleetAPIVersion                   = fleetv1beta1.GroupVersion.String()
	sortClusterResourceSnapshotOption = cmpopts.SortSlices(func(r1, r2 fleetv1beta1.ClusterResourceSnapshot) bool {
		return r1.Name < r2.Name
	})
	cmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
		cmpopts.SortSlices(func(p1, p2 fleetv1beta1.ClusterSchedulingPolicySnapshot) bool {
			return p1.Name < p2.Name
		}),
		sortClusterResourceSnapshotOption,
	}
	singleRevisionLimit   = int32(1)
	multipleRevisionLimit = int32(2)
	invalidRevisionLimit  = int32(0)
)

func serviceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return scheme
}

func placementPolicyForTest() *fleetv1beta1.PlacementPolicy {
	return &fleetv1beta1.PlacementPolicy{
		PlacementType:    fleetv1beta1.PickNPlacementType,
		NumberOfClusters: ptr.To(int32(3)),
		Affinity: &fleetv1beta1.Affinity{
			ClusterAffinity: &fleetv1beta1.ClusterAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &fleetv1beta1.ClusterSelector{
					ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value1",
								},
							},
						},
					},
				},
			},
		},
	}
}

func clusterResourcePlacementForTest() *fleetv1beta1.ClusterResourcePlacement {
	return &fleetv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testCRPName,
			Generation: placementGeneration,
		},
		Spec: fleetv1beta1.PlacementSpec{
			ResourceSelectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   corev1.GroupName,
					Version: "v1",
					Kind:    "Service",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"region": "east"},
					},
				},
			},
			Policy: placementPolicyForTest(),
		},
	}
}

func TestGetOrCreateClusterSchedulingPolicySnapshot(t *testing.T) {
	testPolicy := placementPolicyForTest()
	testPolicyHash := testPolicy.DeepCopy()
	testPolicyHash.NumberOfClusters = nil
	jsonBytes, err := json.Marshal(testPolicyHash)
	if err != nil {
		t.Fatalf("failed to create the policy hash: %v", err)
	}
	policyHash := []byte(fmt.Sprintf("%x", sha256.Sum256(jsonBytes)))
	jsonBytes, err = json.Marshal(nil)
	if err != nil {
		t.Fatalf("failed to create the policy hash: %v", err)
	}
	unspecifiedPolicyHash := []byte(fmt.Sprintf("%x", sha256.Sum256(jsonBytes)))
	tests := []struct {
		name                    string
		policy                  *fleetv1beta1.PlacementPolicy
		revisionHistoryLimit    *int32
		policySnapshots         []fleetv1beta1.ClusterSchedulingPolicySnapshot
		wantPolicySnapshots     []fleetv1beta1.ClusterSchedulingPolicySnapshot
		wantLatestSnapshotIndex int // index of the wantPolicySnapshots array
	}{
		{
			name:   "new clusterResourcePolicy and no existing policy snapshots owned by my-crp",
			policy: placementPolicyForTest(),
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
					},
				},
				// new policy snapshot owned by the my-crp
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(placementGeneration),
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     testPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name: "new clusterResourcePolicy (unspecified policy) and no existing policy snapshots owned by my-crp",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation: "1",
						},
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation: "1",
						},
					},
				},
				// new policy snapshot owned by the my-crp
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation: strconv.Itoa(placementGeneration),
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						PolicyHash: unspecifiedPolicyHash,
					},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                 "crp policy has no change",
			policy:               placementPolicyForTest(),
			revisionHistoryLimit: &singleRevisionLimit,
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(placementGeneration),
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     testPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(placementGeneration),
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     testPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name: "crp policy has changed and there is no active snapshot",
			// It happens when last reconcile loop fails after setting the latest label to false and
			// before creating a new policy snapshot.
			policy:               placementPolicyForTest(),
			revisionHistoryLimit: &multipleRevisionLimit,
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 3),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "3",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 3),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "3",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 4),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "4",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(placementGeneration),
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     testPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                 "crp policy has changed and there is an active snapshot",
			policy:               placementPolicyForTest(),
			revisionHistoryLimit: &singleRevisionLimit,
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(placementGeneration),
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     testPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:   "crp policy has been changed and reverted back and there is no active snapshot",
			policy: placementPolicyForTest(),
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
							fleetv1beta1.CRPGenerationAnnotation:    "2",
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     testPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(placementGeneration),
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     testPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name: "crp policy has not been changed and only the numberOfCluster is changed",
			// cause no new policy snapshot is created, it does not trigger the history limit check.
			policy:               placementPolicyForTest(),
			revisionHistoryLimit: &singleRevisionLimit,
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(1),
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(2),
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     testPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(placementGeneration),
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     testPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			crp := clusterResourcePlacementForTest()
			crp.Spec.Policy = tc.policy
			crp.Spec.RevisionHistoryLimit = tc.revisionHistoryLimit
			objects := []client.Object{crp}
			for i := range tc.policySnapshots {
				objects = append(objects, &tc.policySnapshots[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}
			limit := int32(defaulter.DefaultRevisionHistoryLimitValue)
			if tc.revisionHistoryLimit != nil {
				limit = *tc.revisionHistoryLimit
			}
			got, err := r.getOrCreateSchedulingPolicySnapshot(ctx, crp, int(limit))
			if err != nil {
				t.Fatalf("failed to getOrCreateSchedulingPolicySnapshot: %v", err)
			}

			// Convert interface to concrete type for comparison
			gotSnapshot, ok := got.(*fleetv1beta1.ClusterSchedulingPolicySnapshot)
			if !ok {
				t.Fatalf("getOrCreateSchedulingPolicySnapshot() got %T, want *ClusterSchedulingPolicySnapshot", got)
			}

			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
			}
			if diff := cmp.Diff(tc.wantPolicySnapshots[tc.wantLatestSnapshotIndex], *gotSnapshot, options...); diff != "" {
				t.Errorf("getOrCreateSchedulingPolicySnapshot() mismatch (-want, +got):\n%s", diff)
			}
			clusterPolicySnapshotList := &fleetv1beta1.ClusterSchedulingPolicySnapshotList{}
			if err := fakeClient.List(ctx, clusterPolicySnapshotList); err != nil {
				t.Fatalf("clusterPolicySnapshot List() got error %v, want no error", err)
			}
			if diff := cmp.Diff(tc.wantPolicySnapshots, clusterPolicySnapshotList.Items, cmpOptions...); diff != "" {
				t.Errorf("clusterPolicysnapShot List() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestGetOrCreateClusterSchedulingPolicySnapshot_failure(t *testing.T) {
	wantPolicy := placementPolicyForTest()
	wantPolicy.NumberOfClusters = nil
	jsonBytes, err := json.Marshal(wantPolicy)
	if err != nil {
		t.Fatalf("failed to create the policy hash: %v", err)
	}
	policyHash := []byte(fmt.Sprintf("%x", sha256.Sum256(jsonBytes)))
	tests := []struct {
		name            string
		policySnapshots []fleetv1beta1.ClusterSchedulingPolicySnapshot
	}{
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterPolicySnapshot.
			name: "existing active policy snapshot does not have policyIndex label",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
							},
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterPolicySnapshot.
			name: "existing active policy snapshot has an invalid policyIndex label",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0bc",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
							},
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterPolicySnapshot.
			name: "no active policy snapshot exists and policySnapshot with invalid policyIndex label",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "abc",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "abc",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
							},
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterPolicySnapshot.
			name: "multiple active policy snapshot exist",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
							},
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterPolicySnapshot.
			name: "no active policy snapshot exists and policySnapshot with invalid policyIndex label (negative value)",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "-1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
							},
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterPolicySnapshot.
			name: "active policy snapshot exists and policySnapshot with invalid numberOfClusters annotation",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: "invalid",
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
						PolicyHash: policyHash,
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterPolicySnapshot.
			name: "no active policy snapshot exists and policySnapshot with invalid numberOfClusters annotation (negative)",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: "-123",
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
						PolicyHash: policyHash,
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterPolicySnapshot.
			name: "active policy snapshot exists and policySnapshot without crp generation annotation",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: "12",
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
						PolicyHash: policyHash,
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			crp := clusterResourcePlacementForTest()
			objects := []client.Object{crp}
			for i := range tc.policySnapshots {
				objects = append(objects, &tc.policySnapshots[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}
			_, err := r.getOrCreateSchedulingPolicySnapshot(ctx, crp, 1)
			if err == nil { // if error is nil
				t.Fatal("getOrCreateClusterResourceSnapshot() = nil, want err")
			}
			if !errors.Is(err, controller.ErrUnexpectedBehavior) {
				t.Errorf("getOrCreateClusterResourceSnapshot() got %v, want %v type", err, controller.ErrUnexpectedBehavior)
			}
		})
	}
}

func TestGetOrCreateClusterResourceSnapshot(t *testing.T) {
	// test service is 383 bytes in size.
	serviceResourceContent := *resource.ServiceResourceContentForTest(t)
	// test deployment 390 bytes in size.
	deploymentResourceContent := *resource.DeploymentResourceContentForTest(t)
	// test secret is 152 bytes in size.
	secretResourceContent := *resource.SecretResourceContentForTest(t)

	jsonBytes, err := json.Marshal(&fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}})
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecWithSingleResourceHash hash: %v", err)
	}
	resourceSnapshotSpecWithEmptyResourceHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	jsonBytes, err = json.Marshal(&fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}})
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecWithSingleResource hash: %v", err)
	}
	resourceSnapshotSpecWithServiceResourceHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	jsonBytes, err = json.Marshal(&fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}})
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecWithMultipleResources hash: %v", err)
	}
	resourceSnapshotSpecWithTwoResourcesHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	jsonBytes, err = json.Marshal(&fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent, deploymentResourceContent}})
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecWithMultipleResources hash: %v", err)
	}
	resourceSnapshotSpecWithMultipleResourcesHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	now := metav1.Now()
	nowToString := now.Time.Format(time.RFC3339)
	tests := []struct {
		name                       string
		envelopeObjCount           int
		selectedResourcesSizeLimit int
		resourceSnapshotSpec       *fleetv1beta1.ResourceSnapshotSpec
		revisionHistoryLimit       *int32
		resourceSnapshots          []fleetv1beta1.ClusterResourceSnapshot
		wantResourceSnapshots      []fleetv1beta1.ClusterResourceSnapshot
		wantLatestSnapshotIndex    int // index of the wantPolicySnapshots array
		wantRequeue                bool
	}{
		{
			name:                 "new resourceSnapshot and no existing snapshots owned by my-crp",
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
			revisionHistoryLimit: &invalidRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
				},
				// new resource snapshot owned by the my-crp
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                 "resource has no change",
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
			revisionHistoryLimit: &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:             "resource has changed and there is no active snapshot with single revisionLimit",
			envelopeObjCount: 2,
			// It happens when last reconcile loop fails after setting the latest label to false and
			// before creating a new resource snapshot.
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
			revisionHistoryLimit: &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				// new resource snapshot
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 2),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithEmptyResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                 "resource has changed too fast and there is an active snapshot with multiple revisionLimit",
			envelopeObjCount:     3,
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
			revisionHistoryLimit: &multipleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: nowToString,
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
			},
			wantRequeue:             true,
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                 "resource has changed and there is an active snapshot with multiple revisionLimit",
			envelopeObjCount:     3,
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
			revisionHistoryLimit: &multipleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
						CreationTimestamp: metav1.NewTime(now.Time.Add(-1 * time.Hour)),
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				// new resource snapshot
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithEmptyResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "3",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
			},
			wantLatestSnapshotIndex: 3,
		},
		{
			name:                 "resource has been changed and reverted back and there is no active snapshot",
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                       "selected resource cross clusterResourceSnapshot size limit, no existing clusterResourceSnapshots",
			selectedResourcesSizeLimit: 600,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent, deploymentResourceContent}},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{deploymentResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "selected resource cross clusterResourceSnapshot size limit, master clusterResourceSnapshot created but not all sub-indexed clusterResourceSnapshots have been created",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent, deploymentResourceContent}},
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{deploymentResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "selected resources cross clusterResourceSnapshot limit, revision limit is 1, delete existing clusterResourceSnapshots & create new clusterResourceSnapshots",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
			revisionHistoryLimit:       &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:                   "0",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
						CreationTimestamp: metav1.NewTime(now.Time.Add(-1 * time.Hour)),
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{deploymentResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "resource has changed too fast, selected resources cross clusterResourceSnapshot limit, revision limit is 1",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
			revisionHistoryLimit:       &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{deploymentResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:                   "0",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: nowToString,
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{deploymentResourceContent}},
				},
			},
			wantRequeue:             true,
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "selected resources cross clusterResourceSnapshot limit, revision limit is 1, delete existing clusterResourceSnapshot with missing sub-indexed snapshots & create new clusterResourceSnapshots",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
			revisionHistoryLimit:       &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:                   "0",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
						CreationTimestamp: metav1.NewTime(now.Time.Add(-1 * time.Hour)),
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "selected resources cross clusterResourceSnapshot limit, revision limit is 2, don't delete existing clusterResourceSnapshots & create new clusterResourceSnapshots",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
			revisionHistoryLimit:       &multipleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:                   "0",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
						CreationTimestamp: metav1.NewTime(now.Time.Add(-1 * time.Hour)),
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:                   "0",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 2),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 2, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                       "selected resource cross clusterResourceSnapshot size limit, all clusterResourceSnapshots remain the same since no change",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
			revisionHistoryLimit:       &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "selected resource cross clusterResourceSnapshot size limit, all clusterResourceSnapshots remain the same, but IsLatestSnapshotLabel is set to false",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
			revisionHistoryLimit:       &multipleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
	}
	originalResourceSnapshotResourceSizeLimit := resourceSnapshotResourceSizeLimit
	defer func() {
		resourceSnapshotResourceSizeLimit = originalResourceSnapshotResourceSizeLimit
	}()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			crp := clusterResourcePlacementForTest()
			crp.Spec.RevisionHistoryLimit = tc.revisionHistoryLimit
			objects := []client.Object{crp}
			for i := range tc.resourceSnapshots {
				objects = append(objects, &tc.resourceSnapshots[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client:                                  fakeClient,
				Scheme:                                  scheme,
				Recorder:                                record.NewFakeRecorder(10),
				ResourceSnapshotCreationMinimumInterval: 1 * time.Minute,
			}
			limit := int32(defaulter.DefaultRevisionHistoryLimitValue)
			if tc.revisionHistoryLimit != nil {
				limit = *tc.revisionHistoryLimit
			}
			resourceSnapshotResourceSizeLimit = tc.selectedResourcesSizeLimit
			res, got, err := r.getOrCreateResourceSnapshot(ctx, crp, tc.envelopeObjCount, tc.resourceSnapshotSpec, int(limit))
			if err != nil {
				t.Fatalf("failed to handle getOrCreateResourceSnapshot: %v", err)
			}
			if res.Requeue != tc.wantRequeue {
				t.Fatalf("getOrCreateResourceSnapshot() got Requeue %v, want %v", res.Requeue, tc.wantRequeue)
			}

			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "CreationTimestamp"),
				// Fake API server will add a newline for the runtime.RawExtension type.
				// ignoring the resourceContent field for now
				cmpopts.IgnoreFields(runtime.RawExtension{}, "Raw"),
			}
			if tc.wantRequeue {
				if res.RequeueAfter <= 0 {
					t.Fatalf("getOrCreateResourceSnapshot() got RequeueAfter %v, want greater than zero value", res.RequeueAfter)
				}
			}
			annotationOption := cmp.Transformer("NormalizeAnnotations", func(m map[string]string) map[string]string {
				normalized := map[string]string{}
				for k, v := range m {
					if k == fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation {
						// Normalize the resource group hash annotation to a fixed value for comparison.
						if _, err := time.Parse(time.RFC3339, v); err != nil {
							normalized[k] = ""
						}
						normalized[k] = nowToString
					} else {
						normalized[k] = v
					}
				}
				return normalized
			})
			options = append(options, sortClusterResourceSnapshotOption, annotationOption)
			gotSnapshot, ok := got.(*fleetv1beta1.ClusterResourceSnapshot)
			if !ok {
				t.Fatalf("expected *fleetv1beta1.ClusterResourceSnapshot, got %T", got)
			}
			if diff := cmp.Diff(tc.wantResourceSnapshots[tc.wantLatestSnapshotIndex], *gotSnapshot, options...); diff != "" {
				t.Errorf("getOrCreateResourceSnapshot() mismatch (-want, +got):\n%s", diff)
			}
			clusterResourceSnapshotList := &fleetv1beta1.ClusterResourceSnapshotList{}
			if err := fakeClient.List(ctx, clusterResourceSnapshotList); err != nil {
				t.Fatalf("clusterResourceSnapshot List() got error %v, want no error", err)
			}
			if diff := cmp.Diff(tc.wantResourceSnapshots, clusterResourceSnapshotList.Items, options...); diff != "" {
				t.Errorf("clusterResourceSnapshot List() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestGetOrCreateClusterResourceSnapshot_failure(t *testing.T) {
	selectedResources := []fleetv1beta1.ResourceContent{
		*resource.ServiceResourceContentForTest(t),
	}
	resourceSnapshotSpecA := &fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources: selectedResources,
	}
	tests := []struct {
		name              string
		resourceSnapshots []fleetv1beta1.ClusterResourceSnapshot
	}{
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "existing active resource snapshot does not have resourceIndex label",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "existing active resource snapshot does not have hash annotation",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "0",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "no active resource snapshot exists and resourceSnapshot with invalid resourceIndex label",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "abc",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "no active resource snapshot exists and multiple resourceSnapshots with invalid resourceIndex label",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "abc",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "abc",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "no active resource snapshot exists and multiple resourceSnapshots with invalid subindex annotation",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "0",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "0",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "abc",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "1",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "abc",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "no active resource snapshot exists and multiple resourceSnapshots with invalid subindex (<0) annotation",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "0",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "0",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "-1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "1",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "-1",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "multiple active resource snapshot exist",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "hashA",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "hashA",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterPolicySnapshot.
			name: "no active resource snapshot exists and resourceSnapshot with invalid resourceIndex label (negative value)",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "-12",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "hashA",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			crp := clusterResourcePlacementForTest()
			objects := []client.Object{crp}
			for i := range tc.resourceSnapshots {
				objects = append(objects, &tc.resourceSnapshots[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
				Scheme: scheme,
			}
			res, _, err := r.getOrCreateResourceSnapshot(ctx, crp, 0, resourceSnapshotSpecA, 1)
			if err == nil { // if error is nil
				t.Fatal("getOrCreateClusterResourceSnapshot() = nil, want err")
			}
			if res.Requeue {
				t.Fatal("getOrCreateClusterResourceSnapshot() requeue = true, want false")
			}
			if !errors.Is(err, controller.ErrUnexpectedBehavior) {
				t.Errorf("getOrCreateClusterResourceSnapshot() got %v, want %v type", err, controller.ErrUnexpectedBehavior)
			}
		})
	}
}

func TestHandleDelete(t *testing.T) {
	tests := []struct {
		name                  string
		policySnapshots       []fleetv1beta1.ClusterSchedulingPolicySnapshot
		resourceSnapshots     []fleetv1beta1.ClusterResourceSnapshot
		wantPolicySnapshots   []fleetv1beta1.ClusterSchedulingPolicySnapshot
		wantResourceSnapshots []fleetv1beta1.ClusterResourceSnapshot
	}{
		{
			name: "have active snapshots",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
						},
					},
				},
			},
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
					},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
			},
		},
		{
			name: "have no active snapshots",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
						},
					},
				},
			},
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
					},
				},
			},
			wantPolicySnapshots:   []fleetv1beta1.ClusterSchedulingPolicySnapshot{},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{},
		},
		{
			name: "have no snapshots",
			policySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
					},
				},
			},
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:       "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
					},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			crp := clusterResourcePlacementForTest()
			crp.Finalizers = []string{fleetv1beta1.PlacementCleanupFinalizer}
			now := metav1.Now()
			crp.DeletionTimestamp = &now
			objects := []client.Object{crp}
			for i := range tc.policySnapshots {
				objects = append(objects, &tc.policySnapshots[i])
			}
			for i := range tc.resourceSnapshots {
				objects = append(objects, &tc.resourceSnapshots[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client:         fakeClient,
				Scheme:         scheme,
				UncachedReader: fakeClient,
				Recorder:       record.NewFakeRecorder(10),
			}
			got, err := r.handleDelete(ctx, crp)
			if err != nil {
				t.Fatalf("failed to handle delete: %v", err)
			}
			want := ctrl.Result{}
			if !cmp.Equal(got, want) {
				t.Errorf("handleDelete() = %+v, want %+v", got, want)
			}
			clusterPolicySnapshotList := &fleetv1beta1.ClusterSchedulingPolicySnapshotList{}
			if err := fakeClient.List(ctx, clusterPolicySnapshotList); err != nil {
				t.Fatalf("clusterPolicySnapshot List() got error %v, want no error", err)
			}
			if diff := cmp.Diff(tc.wantPolicySnapshots, clusterPolicySnapshotList.Items, cmpOptions...); diff != "" {
				t.Errorf("clusterPolicysnapShot List() mismatch (-want, +got):\n%s", diff)
			}
			clusterResourceSnapshotList := &fleetv1beta1.ClusterResourceSnapshotList{}
			if err := fakeClient.List(ctx, clusterResourceSnapshotList); err != nil {
				t.Fatalf("clusterResourceSnapshot List() got error %v, want no error", err)
			}
			if diff := cmp.Diff(tc.wantResourceSnapshots, clusterResourceSnapshotList.Items, cmpOptions...); diff != "" {
				t.Errorf("clusterResourceSnapshot List() mismatch (-want, +got):\n%s", diff)
			}
			gotCRP := fleetv1beta1.ClusterResourcePlacement{}
			if err := fakeClient.Get(ctx, types.NamespacedName{Name: crp.GetName()}, &gotCRP); !apierrors.IsNotFound(err) {
				t.Errorf("clusterResourcePlacement Get() = %+v, got error %v, want not found error", gotCRP, err)
			}
		})
	}
}

func TestIsRolloutComplete(t *testing.T) {
	crpGeneration := int64(25)
	tests := []struct {
		name       string
		conditions []metav1.Condition
		want       bool
	}{
		{
			name: "rollout is completed",
			conditions: []metav1.Condition{
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
					ObservedGeneration: crpGeneration,
				},
			},
			want: true,
		},
		{
			name: "schedule condition is unknown",
			conditions: []metav1.Condition{
				{
					Status:             metav1.ConditionUnknown,
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					ObservedGeneration: crpGeneration,
				},
			},
			want: false,
		},
		{
			name: "rollout condition is nil",
			conditions: []metav1.Condition{
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					ObservedGeneration: crpGeneration,
				},
			},
			want: false,
		},
		{
			name: "overridden condition is not the latest",
			conditions: []metav1.Condition{
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
					ObservedGeneration: 1,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					ObservedGeneration: crpGeneration,
				},
			},
			want: false,
		},
		{
			name: "workCreated condition is false",
			conditions: []metav1.Condition{
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionFalse,
					Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
					ObservedGeneration: crpGeneration,
				},
			},
			want: false,
		},
		{
			name: "applied condition is nil",
			conditions: []metav1.Condition{
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
					ObservedGeneration: crpGeneration,
				},
			},
			want: false,
		},
		{
			name: "available condition is false",
			conditions: []metav1.Condition{
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionFalse,
					Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
					ObservedGeneration: crpGeneration,
				},
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			crp := &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Generation: crpGeneration,
				},
				Status: fleetv1beta1.PlacementStatus{
					Conditions: tc.conditions,
				},
			}
			got := isRolloutCompleted(crp)
			if got != tc.want {
				t.Errorf("isRolloutCompleted() got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDetermineRolloutStateForPlacementWithExternalRolloutStrategy(t *testing.T) {
	namespaceResourceContent := *resource.NamespaceResourceContentForTest(t)
	deploymentResourceContent := *resource.DeploymentResourceContentForTest(t)

	tests := []struct {
		name                          string
		selected                      []*fleetv1beta1.ClusterDecision
		allRPS                        []fleetv1beta1.PerClusterPlacementStatus
		resourceSnapshots             []*fleetv1beta1.ClusterResourceSnapshot
		selectedResources             []fleetv1beta1.ResourceIdentifier
		existingObservedResourceIndex string
		existingConditions            []metav1.Condition
		wantRolloutUnknown            bool
		wantObservedResourceIndex     string
		wantSelectedResources         []fleetv1beta1.ResourceIdentifier
		wantConditions                []metav1.Condition
		wantErr                       bool
	}{
		{
			name:               "no selected clusters", // This should not happen in normal cases.
			selected:           []*fleetv1beta1.ClusterDecision{},
			allRPS:             []fleetv1beta1.PerClusterPlacementStatus{},
			resourceSnapshots:  []*fleetv1beta1.ClusterResourceSnapshot{},
			existingConditions: []metav1.Condition{},
			wantErr:            true,
		},
		{
			name: "selected clusters with different observed resource indices",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
				{
					ClusterName: "cluster2",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "0",
				},
				{
					ClusterName:           "cluster2",
					ObservedResourceIndex: "1",
				},
			},
			resourceSnapshots:         []*fleetv1beta1.ClusterResourceSnapshot{},
			existingConditions:        []metav1.Condition{},
			wantRolloutUnknown:        true,
			wantObservedResourceIndex: "",
			wantSelectedResources:     []fleetv1beta1.ResourceIdentifier{},
			wantConditions: []metav1.Condition{
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionUnknown,
					Reason:             "RolloutControlledByExternalController",
					Message:            "Rollout is controlled by an external controller and different resource snapshot versions are observed across clusters",
					ObservedGeneration: 1,
				},
			},
			wantErr: false,
		},
		{
			name: "selected clusters with different observed resource indices and an empty ObservedResourceIndex",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
				{
					ClusterName: "cluster2",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "",
				},
				{
					ClusterName:           "cluster2",
					ObservedResourceIndex: "1",
				},
			},
			resourceSnapshots:         []*fleetv1beta1.ClusterResourceSnapshot{},
			wantRolloutUnknown:        true,
			wantObservedResourceIndex: "",
			wantSelectedResources:     []fleetv1beta1.ResourceIdentifier{},
			wantConditions: []metav1.Condition{
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionUnknown,
					Reason:             "RolloutControlledByExternalController",
					Message:            "Rollout is controlled by an external controller and different resource snapshot versions are observed across clusters",
					ObservedGeneration: 1,
				},
			},
			wantErr: false,
		},
		{
			name: "selected clusters with different observed resource indices and crp has some conditions already",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
				{
					ClusterName: "cluster2",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "",
				},
				{
					ClusterName:           "cluster2",
					ObservedResourceIndex: "1",
				},
			},
			resourceSnapshots: []*fleetv1beta1.ClusterResourceSnapshot{},
			existingConditions: []metav1.Condition{
				{
					// Scheduled condition should be kept.
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Scheduled",
					Message:            "Scheduling is complete",
					ObservedGeneration: 1,
				},
				{
					// RolloutStarted condition should be updated.
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					Message:            "Rollout is started",
					ObservedGeneration: 0,
				},
				{
					// Overridden condition should be removed.
					Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Overridden",
					Message:            "Overridden",
					ObservedGeneration: 0,
				},
			},
			wantRolloutUnknown:        true,
			wantObservedResourceIndex: "",
			wantSelectedResources:     []fleetv1beta1.ResourceIdentifier{},
			wantConditions: []metav1.Condition{
				{
					// Scheduled condition should be kept.
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Scheduled",
					Message:            "Scheduling is complete",
					ObservedGeneration: 1,
				},
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionUnknown,
					Reason:             "RolloutControlledByExternalController",
					Message:            "Rollout is controlled by an external controller and different resource snapshot versions are observed across clusters",
					ObservedGeneration: 1,
				},
			},
			wantErr: false,
		},
		{
			name: "selected clusters all with empty ObservedResourceIndex",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
				{
					ClusterName: "cluster2",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "",
				},
				{
					ClusterName:           "cluster2",
					ObservedResourceIndex: "",
				},
				{
					ClusterName:           "cluster-unselected",
					ObservedResourceIndex: "1", // This should not be considered.
				},
			},
			resourceSnapshots:         []*fleetv1beta1.ClusterResourceSnapshot{},
			existingConditions:        []metav1.Condition{},
			wantRolloutUnknown:        true,
			wantObservedResourceIndex: "",
			wantSelectedResources:     []fleetv1beta1.ResourceIdentifier{},
			wantConditions: []metav1.Condition{
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionUnknown,
					Reason:             "RolloutControlledByExternalController",
					Message:            "Rollout is controlled by an external controller and no resource snapshot name is observed across clusters, probably rollout has not started yet",
					ObservedGeneration: 1,
				},
			},
			wantErr: false,
		},
		{
			name: "selected clusters all with empty ObservedResourceIndex and crp has some conditions already",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
				{
					ClusterName: "cluster2",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "",
				},
				{
					ClusterName:           "cluster2",
					ObservedResourceIndex: "",
				},
				{
					ClusterName:           "cluster-unselected",
					ObservedResourceIndex: "1", // This should not be considered.
				},
			},
			resourceSnapshots: []*fleetv1beta1.ClusterResourceSnapshot{},
			existingConditions: []metav1.Condition{
				{
					// Scheduled condition should be kept.
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Scheduled",
					Message:            "Scheduling is complete",
					ObservedGeneration: 1,
				},
				{
					// RolloutStarted condition should be updated.
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					Message:            "Rollout is started",
					ObservedGeneration: 0,
				},
				{
					// Overridden condition should be removed.
					Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Overridden",
					Message:            "Overridden",
					ObservedGeneration: 0,
				},
			},
			wantRolloutUnknown:        true,
			wantObservedResourceIndex: "",
			wantSelectedResources:     []fleetv1beta1.ResourceIdentifier{},
			wantConditions: []metav1.Condition{
				{
					// Scheduled condition should be kept.
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Scheduled",
					Message:            "Scheduling is complete",
					ObservedGeneration: 1,
				},
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionUnknown,
					Reason:             "RolloutControlledByExternalController",
					Message:            "Rollout is controlled by an external controller and no resource snapshot name is observed across clusters, probably rollout has not started yet",
					ObservedGeneration: 1,
				},
			},
			wantErr: false,
		},
		{
			name: "single selected cluster with empty ObservedResourceIndex",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "",
				},
				{
					ClusterName:           "cluster-unselected",
					ObservedResourceIndex: "1", // This should not be considered.
				},
			},
			resourceSnapshots:         []*fleetv1beta1.ClusterResourceSnapshot{},
			existingConditions:        []metav1.Condition{},
			wantRolloutUnknown:        true,
			wantObservedResourceIndex: "",
			wantSelectedResources:     []fleetv1beta1.ResourceIdentifier{},
			wantConditions: []metav1.Condition{
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionUnknown,
					Reason:             "RolloutControlledByExternalController",
					Message:            "Rollout is controlled by an external controller and no resource snapshot name is observed across clusters, probably rollout has not started yet",
					ObservedGeneration: 1,
				},
			},
			wantErr: false,
		},
		{
			name: "single selected cluster with valid ObservedResourceIndex but no clusterResourceSnapshots found",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "2",
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
				{
					ClusterName:           "cluster-unselected",
					ObservedResourceIndex: "1", // This should not be considered.
				},
			},
			existingConditions:        []metav1.Condition{},
			wantRolloutUnknown:        false,
			wantObservedResourceIndex: "2",
			wantSelectedResources:     nil,
			wantConditions:            []metav1.Condition{},
			wantErr:                   false,
		},
		{
			name: "single selected cluster with valid ObservedResourceIndex but no master clusterResourceSnapshots with the specified index found",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "2",
				},
				{
					ClusterName:           "cluster-unselected",
					ObservedResourceIndex: "1", // This should not be considered.
				},
			},
			resourceSnapshots: []*fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 2, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							namespaceResourceContent,
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "selected clusters with valid ObservedResourceIndex but no rollout started condition",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
				{
					ClusterName: "cluster2",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "2",
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
				{
					ClusterName:           "cluster2",
					ObservedResourceIndex: "2",
				},
				{
					ClusterName:           "cluster-unselected",
					ObservedResourceIndex: "1", // This should not be considered.
				},
			},
			resourceSnapshots: []*fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 2),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							namespaceResourceContent,
						},
					},
				},
			},
			wantRolloutUnknown:        true,
			wantObservedResourceIndex: "2",
			wantSelectedResources: []fleetv1beta1.ResourceIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Namespace",
					Namespace: "",
					Name:      "namespace-name",
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionUnknown,
					Reason:             "RolloutControlledByExternalController",
					Message:            "Rollout is controlled by an external controller and cluster cluster2 is in RolloutStarted Unknown state",
					ObservedGeneration: 1,
				},
			},
			wantErr: false,
		},
		{
			name: "single selected cluster with valid ObservedResourceIndex",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "2",
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
				{
					ClusterName:           "cluster-unselected",
					ObservedResourceIndex: "1", // This should not be considered.
				},
			},
			resourceSnapshots: []*fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 2),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							namespaceResourceContent,
						},
					},
				},
			},
			existingConditions:        []metav1.Condition{},
			wantRolloutUnknown:        false,
			wantObservedResourceIndex: "2",
			wantSelectedResources: []fleetv1beta1.ResourceIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Namespace",
					Namespace: "",
					Name:      "namespace-name",
				},
			},
			wantConditions: []metav1.Condition{},
			wantErr:        false,
		},
		{
			name: "multiple selected clusters with the same valid ObservedResourceIndex and crp has some conditions already",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
				{
					ClusterName: "cluster2",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "2",
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
				{
					ClusterName:           "cluster2",
					ObservedResourceIndex: "2",
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
				{
					ClusterName:           "cluster-unselected",
					ObservedResourceIndex: "1", // This should not be considered.
				},
			},
			resourceSnapshots: []*fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 2),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							namespaceResourceContent,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 2, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							deploymentResourceContent,
						},
					},
				},
			},
			existingConditions: []metav1.Condition{
				// All conditions should be kept, which will be updated later in setCRPConditions.
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Scheduled",
					Message:            "Scheduling is complete",
					ObservedGeneration: 1,
				},
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					Message:            "Rollout is started",
					ObservedGeneration: 0,
				},
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Overridden",
					Message:            "Overridden",
					ObservedGeneration: 0,
				},
			},
			wantRolloutUnknown:        false,
			wantObservedResourceIndex: "2",
			wantSelectedResources: []fleetv1beta1.ResourceIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Namespace",
					Namespace: "",
					Name:      "namespace-name",
				},
				{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Namespace: "deployment-namespace",
					Name:      "deployment-name",
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Scheduled",
					Message:            "Scheduling is complete",
					ObservedGeneration: 1,
				},
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					Message:            "Rollout is started",
					ObservedGeneration: 0,
				},
				{
					Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Overridden",
					Message:            "Overridden",
					ObservedGeneration: 0,
				},
			},
			wantErr: false,
		},
		{
			name: "multiple selected clusters with the same valid ObservedResourceIndex and multiple clusterResourceSnapshots found",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
				{
					ClusterName: "cluster2",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "2",
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
				{
					ClusterName:           "cluster2",
					ObservedResourceIndex: "2",
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
				{
					ClusterName:           "cluster-unselected",
					ObservedResourceIndex: "1", // This should not be considered.
				},
			},
			resourceSnapshots: []*fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 2),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							namespaceResourceContent,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 2, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							deploymentResourceContent,
						},
					},
				},
			},
			existingConditions:        []metav1.Condition{},
			wantRolloutUnknown:        false,
			wantObservedResourceIndex: "2",
			wantSelectedResources: []fleetv1beta1.ResourceIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Namespace",
					Namespace: "",
					Name:      "namespace-name",
				},
				{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Namespace: "deployment-namespace",
					Name:      "deployment-name",
				},
			},
			wantConditions: []metav1.Condition{},
			wantErr:        false,
		},
		{
			name: "use selected resources passed in if clusters are on latest resource snapshot",
			selected: []*fleetv1beta1.ClusterDecision{
				{
					ClusterName: "cluster1",
					Selected:    true,
				},
			},
			allRPS: []fleetv1beta1.PerClusterPlacementStatus{
				{
					ClusterName:           "cluster1",
					ObservedResourceIndex: "2",
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
				{
					ClusterName:           "cluster-unselected",
					ObservedResourceIndex: "1", // This should not be considered.
				},
			},
			existingObservedResourceIndex: "2",
			existingConditions:            []metav1.Condition{},
			selectedResources: []fleetv1beta1.ResourceIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Namespace",
					Namespace: "",
					Name:      "namespace-name",
				},
			},
			wantRolloutUnknown:        false,
			wantObservedResourceIndex: "2",
			wantSelectedResources: []fleetv1beta1.ResourceIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Namespace",
					Namespace: "",
					Name:      "namespace-name",
				},
			},
			wantConditions: []metav1.Condition{},
			wantErr:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			crp := &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Generation: 1,
				},
				Status: fleetv1beta1.PlacementStatus{
					ObservedResourceIndex: tc.existingObservedResourceIndex,
					Conditions:            tc.existingConditions,
				},
			}
			objects := []client.Object{}
			for _, snapshot := range tc.resourceSnapshots {
				objects = append(objects, snapshot)
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			var cmpOptions = []cmp.Option{
				// ignore the message as we may change the message in the future
				cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
				cmpopts.SortSlices(utils.LessFuncResourceIdentifier),
			}
			gotRolloutUnknown, gotErr := r.determineRolloutStateForPlacementWithExternalRolloutStrategy(context.Background(), crp, tc.selected, tc.allRPS, tc.selectedResources)
			if (gotErr != nil) != tc.wantErr {
				t.Errorf("determineRolloutStateForPlacementWithExternalRolloutStrategy() got error %v, want error %t", gotErr, tc.wantErr)
			}
			if !tc.wantErr {
				if gotRolloutUnknown != tc.wantRolloutUnknown {
					t.Errorf("determineRolloutStateForPlacementWithExternalRolloutStrategy() got RolloutUnknown set to %v, want %v", gotRolloutUnknown, tc.wantRolloutUnknown)
				}
				if crp.Status.ObservedResourceIndex != tc.wantObservedResourceIndex {
					t.Errorf("determineRolloutStateForPlacementWithExternalRolloutStrategy() got crp.Status.ObservedResourceIndex set to %v, want %v", crp.Status.ObservedResourceIndex, tc.wantObservedResourceIndex)
				}
				if diff := cmp.Diff(tc.wantSelectedResources, crp.Status.SelectedResources, cmpOptions...); diff != "" {
					t.Errorf("determineRolloutStateForPlacementWithExternalRolloutStrategy() got crp.Status.SelectedResources mismatch (-want, +got):\n%s", diff)
				}
				if diff := cmp.Diff(tc.wantConditions, crp.Status.Conditions, cmpOptions...); diff != "" {
					t.Errorf("determineRolloutStateForPlacementWithExternalRolloutStrategy() got crp.Status.Conditions mismatch (-want, +got):\n%s", diff)
				}
			}
		})
	}
}

func TestShouldCreateNewResourceSnapshotNow(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name               string
		creationInterval   time.Duration
		collectionDuration time.Duration
		creationTime       time.Time
		annotationValue    string
		wantAnnoation      bool
		wantRequeue        ctrl.Result
	}{
		{
			name:               "ResourceSnapshotCreationMinimumInterval and ResourceChangesCollectionDuration are 0",
			creationInterval:   0,
			collectionDuration: 0,
			wantRequeue:        ctrl.Result{Requeue: false},
		},
		{
			name:               "ResourceSnapshotCreationMinimumInterval is 0",
			creationInterval:   0,
			collectionDuration: 30 * time.Second,
			annotationValue:    now.Add(-10 * time.Second).Format(time.RFC3339),
			wantAnnoation:      true,
			wantRequeue:        ctrl.Result{Requeue: true, RequeueAfter: 20 * time.Second},
		},
		{
			name:               "ResourceChangesCollectionDuration is 0",
			creationInterval:   300 * time.Second,
			collectionDuration: 0,
			creationTime:       now.Add(-5 * time.Second),
			// no annotation → sets it and requeues
			annotationValue: "",
			wantAnnoation:   true,
			wantRequeue:     ctrl.Result{Requeue: true, RequeueAfter: 295 * time.Second},
		},
		{
			name:               "next detection time (now) + collection duration < latest resource snapshot creation time + creation interval",
			creationInterval:   300 * time.Second,
			collectionDuration: 30 * time.Second,
			creationTime:       now.Add(-5 * time.Second),
			// no annotation → sets it and requeues
			annotationValue: "",
			wantAnnoation:   true,
			wantRequeue:     ctrl.Result{Requeue: true, RequeueAfter: 295 * time.Second},
		},
		{
			name:               "next detection time (annotation) + collection duration < latest resource snapshot creation time + creation interval",
			creationInterval:   300 * time.Second,
			collectionDuration: 30 * time.Second,
			creationTime:       now.Add(-10 * time.Second),
			annotationValue:    now.Add(-5 * time.Second).Format(time.RFC3339),
			wantAnnoation:      true,
			wantRequeue:        ctrl.Result{Requeue: true, RequeueAfter: 290 * time.Second},
		},
		{
			name:               "last resource snapshot created long time before",
			creationInterval:   60 * time.Second,
			collectionDuration: 30 * time.Second,
			creationTime:       now.Add(-1 * time.Hour),
			wantAnnoation:      true,
			wantRequeue:        ctrl.Result{Requeue: true, RequeueAfter: 30 * time.Second},
		},
		{
			name:               "next detection time (now) + collection duration >= latest resource snapshot creation time + creation interval",
			creationInterval:   60 * time.Second,
			collectionDuration: 60 * time.Second,
			creationTime:       now.Add(-40 * time.Second),
			wantAnnoation:      true,
			wantRequeue:        ctrl.Result{Requeue: true, RequeueAfter: 60 * time.Second},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// initialize a snapshot with given creation time and annotation
			snapshot := &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-snapshot",
					CreationTimestamp: metav1.Time{Time: tc.creationTime},
					Annotations:       map[string]string{},
				},
			}
			if tc.annotationValue != "" {
				snapshot.Annotations[fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation] = tc.annotationValue
			}

			// use fake client seeded with the snapshot
			scheme := serviceScheme(t)
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(snapshot.DeepCopy()).
				Build()

			r := &Reconciler{
				Client:                                  client,
				ResourceSnapshotCreationMinimumInterval: tc.creationInterval,
				ResourceChangesCollectionDuration:       tc.collectionDuration,
			}

			ctx := context.Background()
			if err := client.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot); err != nil {
				t.Fatalf("Failed to get snapshot: %v", err)
			}
			got, err := r.shouldCreateNewResourceSnapshotNow(ctx, snapshot)
			if err != nil {
				t.Fatalf("shouldCreateNewResourceSnapshotNow() failed: %v", err)
			}
			cmpOptions := []cmp.Option{cmp.Comparer(func(d1, d2 time.Duration) bool {
				if d1 == 0 {
					return d2 == 0 // both are zero
				}
				return time.Duration.Abs(d1-d2) < 3*time.Second // allow 1 second difference
			})}
			if !cmp.Equal(got, tc.wantRequeue, cmpOptions...) {
				t.Errorf("shouldCreateNewResourceSnapshotNow() = %v, want %v", got, tc.wantRequeue)
			}
			if err := client.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot); err != nil {
				t.Fatalf("failed to get snapshot after shouldCreateNewResourceSnapshotNow: %v", err)
			}
			if gotAnnotation := len(snapshot.Annotations[fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation]) != 0; tc.wantAnnoation != gotAnnotation {
				t.Errorf("shouldCreateNewResourceSnapshotNow() = annotation %v, want %v", snapshot.Annotations[fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation], tc.wantAnnoation)
			}
		})
	}
}
