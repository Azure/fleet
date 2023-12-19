/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package clusterresourceplacement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	testName      = "my-crp"
	crpGeneration = 15
)

var (
	fleetAPIVersion = fleetv1beta1.GroupVersion.String()
	sortOption      = cmpopts.SortSlices(func(r1, r2 fleetv1beta1.ClusterResourceSnapshot) bool {
		return r1.Name < r2.Name
	})
	cmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
		cmpopts.SortSlices(func(p1, p2 fleetv1beta1.ClusterSchedulingPolicySnapshot) bool {
			return p1.Name < p2.Name
		}),
		sortOption,
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
		NumberOfClusters: pointer.Int32(3),
		Affinity: &fleetv1beta1.Affinity{
			ClusterAffinity: &fleetv1beta1.ClusterAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &fleetv1beta1.ClusterSelector{
					ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: metav1.LabelSelector{
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
			Name:       testName,
			Generation: crpGeneration,
		},
		Spec: fleetv1beta1.ClusterResourcePlacementSpec{
			ResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
	testPolicy.NumberOfClusters = nil
	jsonBytes, err := json.Marshal(testPolicy)
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
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
						},
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterSchedulingPolicySnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
						},
					},
				},
				// new policy snapshot owned by the my-crp
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(crpGeneration),
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
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
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
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation: "1",
						},
					},
				},
				// new policy snapshot owned by the my-crp
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation: strconv.Itoa(crpGeneration),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(crpGeneration),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(crpGeneration),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 3),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "3",
							fleetv1beta1.IsLatestSnapshotLabel: "false",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.IsLatestSnapshotLabel: "false",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 3),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "3",
							fleetv1beta1.IsLatestSnapshotLabel: "false",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 4),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "4",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(crpGeneration),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(crpGeneration),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.IsLatestSnapshotLabel: "false",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel: "1",
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.IsLatestSnapshotLabel: "false",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(crpGeneration),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.IsLatestSnapshotLabel: "false",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.CRPTrackingLabel:      testName,
							fleetv1beta1.IsLatestSnapshotLabel: "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.IsLatestSnapshotLabel: "false",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
							fleetv1beta1.CRPGenerationAnnotation:    strconv.Itoa(crpGeneration),
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
			limit := fleetv1beta1.RevisionHistoryLimitDefaultValue
			if tc.revisionHistoryLimit != nil {
				limit = *tc.revisionHistoryLimit
			}
			got, err := r.getOrCreateClusterSchedulingPolicySnapshot(ctx, crp, int(limit))
			if err != nil {
				t.Fatalf("failed to getOrCreateClusterSchedulingPolicySnapshot: %v", err)
			}
			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
			}
			if diff := cmp.Diff(tc.wantPolicySnapshots[tc.wantLatestSnapshotIndex], *got, options...); diff != "" {
				t.Errorf("getOrCreateClusterSchedulingPolicySnapshot() mismatch (-want, +got):\n%s", diff)
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0bc",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel: "abc",
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel: "abc",
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.CRPTrackingLabel:      testName,
							fleetv1beta1.IsLatestSnapshotLabel: "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.CRPTrackingLabel:      testName,
							fleetv1beta1.IsLatestSnapshotLabel: "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel: "-1",
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel: "1",
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
			_, err := r.getOrCreateClusterSchedulingPolicySnapshot(ctx, crp, 1)
			if err == nil { // if error is nil
				t.Fatal("getOrCreateClusterResourceSnapshot() = nil, want err")
			}
			if !errors.Is(err, controller.ErrUnexpectedBehavior) {
				t.Errorf("getOrCreateClusterResourceSnapshot() got %v, want %v type", err, controller.ErrUnexpectedBehavior)
			}
		})
	}
}

func serviceResourceContentForTest(t *testing.T) *fleetv1beta1.ResourceContent {
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-name",
			Namespace: "svc-namespace",
			Annotations: map[string]string{
				"svc-annotation-key": "svc-object-annotation-key-value",
			},
			Labels: map[string]string{
				"region": "east",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"svc-spec-selector-key": "svc-spec-selector-value"},
			Ports: []corev1.ServicePort{
				{
					Name:        "svc-port",
					Protocol:    corev1.ProtocolTCP,
					AppProtocol: pointer.String("svc.com/my-custom-protocol"),
					Port:        9001,
				},
			},
		},
	}
	return createResourceContentForTest(t, svc)
}

func deploymentResourceContentForTest(t *testing.T) *fleetv1beta1.ResourceContent {
	d := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deployment-name",
			Namespace: "deployment-namespace",
			Labels: map[string]string{
				"app": "nginx",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "nginx",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "nginx",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.14.2",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}
	return createResourceContentForTest(t, d)
}

func secretResourceContentForTest(t *testing.T) *fleetv1beta1.ResourceContent {
	s := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-name",
			Namespace: "secret-namespace",
		},
		Data: map[string][]byte{
			".secret-file": []byte("dmFsdWUtMg0KDQo="),
		},
	}
	return createResourceContentForTest(t, s)
}

func secretResourceContentForTest1(t *testing.T, name string) *fleetv1beta1.ResourceContent {
	var secret corev1.Secret
	if err := utils.GetObjectFromManifest("../../../test/integration/manifests/resources/test-large-secret.yaml", &secret); err != nil {
		t.Fatalf("failed to read secret from manifest: %v", err)
	}
	secret.Name = name
	return createResourceContentForTest(t, &secret)
}

func TestGetOrCreateClusterResourceSnapshot(t *testing.T) {
	selectedResources := []fleetv1beta1.ResourceContent{
		*serviceResourceContentForTest(t),
	}
	resourceSnapshotSpecA := &fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources: selectedResources,
	}
	jsonBytes, err := json.Marshal(resourceSnapshotSpecA)
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecA hash: %v", err)
	}
	resourceSnapshotAHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	resourceSnapshotSpecB := &fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources: []fleetv1beta1.ResourceContent{},
	}

	jsonBytes, err = json.Marshal(resourceSnapshotSpecB)
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecB hash: %v", err)
	}
	resourceSnapshotBHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	secretZero := *secretResourceContentForTest1(t, "test-secret-0")
	secretOne := *secretResourceContentForTest1(t, "test-secret-1")
	secretTwo := *secretResourceContentForTest1(t, "test-secret-2")
	secretThree := *secretResourceContentForTest1(t, "test-secret-3")
	secretFour := *secretResourceContentForTest1(t, "test-secret-4")
	secretFive := *secretResourceContentForTest1(t, "test-secret-5")
	resourceSnapshotSpecC := &fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources: []fleetv1beta1.ResourceContent{secretZero, secretOne, secretTwo, secretThree, secretFour, secretFive},
	}
	jsonBytes, err = json.Marshal(resourceSnapshotSpecC)
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecC hash: %v", err)
	}
	resourceSnapshotCHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	resourceSnapshotSpecD := &fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources: []fleetv1beta1.ResourceContent{secretZero, secretOne, secretTwo},
	}
	resourceSnapshotSpecE := &fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources: []fleetv1beta1.ResourceContent{secretThree, secretFour, secretFive},
	}
	tests := []struct {
		name                    string
		envelopeObjCount        int
		resourceSnapshotSpec    *fleetv1beta1.ResourceSnapshotSpec
		revisionHistoryLimit    *int32
		resourceSnapshots       []fleetv1beta1.ClusterResourceSnapshot
		wantResourceSnapshots   []fleetv1beta1.ClusterResourceSnapshot
		wantLatestSnapshotIndex int // index of the wantPolicySnapshots array
	}{
		{
			name:                 "new resourceSnapshot and no existing snapshots owned by my-crp",
			resourceSnapshotSpec: resourceSnapshotSpecA,
			revisionHistoryLimit: &invalidRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
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
							fleetv1beta1.ResourceIndexLabel:    "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotAHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                 "resource has no change",
			resourceSnapshotSpec: resourceSnapshotSpecA,
			revisionHistoryLimit: &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotAHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotAHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:             "resource has changed and there is no active snapshot with single revisionLimit",
			envelopeObjCount: 2,
			// It happens when last reconcile loop fails after setting the latest label to false and
			// before creating a new resource snapshot.
			resourceSnapshotSpec: resourceSnapshotSpecB,
			revisionHistoryLimit: &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotAHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: *resourceSnapshotSpecB,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: *resourceSnapshotSpecB,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "1",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotAHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				// new resource snapshot
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 2),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "2",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotBHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "2",
						},
					},
					Spec: *resourceSnapshotSpecB,
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                 "resource has changed and there is an active snapshot with multiple revisionLimit",
			envelopeObjCount:     3,
			resourceSnapshotSpec: resourceSnapshotSpecB,
			revisionHistoryLimit: &multipleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "0",
							fleetv1beta1.CRPTrackingLabel:      testName,
							fleetv1beta1.IsLatestSnapshotLabel: "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotAHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: *resourceSnapshotSpecB,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: *resourceSnapshotSpecB,
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: *resourceSnapshotSpecB,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: *resourceSnapshotSpecB,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "0",
							fleetv1beta1.CRPTrackingLabel:      testName,
							fleetv1beta1.IsLatestSnapshotLabel: "false",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotAHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
				// new resource snapshot
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotBHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "3",
						},
					},
					Spec: *resourceSnapshotSpecB,
				},
			},
			wantLatestSnapshotIndex: 3,
		},
		{
			name:                 "resource has been changed and reverted back and there is no active snapshot",
			resourceSnapshotSpec: resourceSnapshotSpecA,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotAHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotAHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                 "multiple resource snapshot - selected resource cross 1MB limit",
			resourceSnapshotSpec: resourceSnapshotSpecC,
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: *resourceSnapshotSpecE,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotCHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: *resourceSnapshotSpecD,
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                 "multiple resource snapshot - selected resource cross 1MB limit, not all resource snapshots have been created",
			resourceSnapshotSpec: resourceSnapshotSpecC,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotCHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: *resourceSnapshotSpecD,
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: *resourceSnapshotSpecE,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotCHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: *resourceSnapshotSpecD,
				},
			},
			wantLatestSnapshotIndex: 1,
		},
	}
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
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}
			limit := fleetv1beta1.RevisionHistoryLimitDefaultValue
			if tc.revisionHistoryLimit != nil {
				limit = *tc.revisionHistoryLimit
			}
			got, err := r.getOrCreateClusterResourceSnapshot(ctx, crp, tc.envelopeObjCount, tc.resourceSnapshotSpec, int(limit))
			if err != nil {
				t.Fatalf("failed to handle getOrCreateClusterResourceSnapshot: %v", err)
			}
			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
				// Fake API server will add a newline for the runtime.RawExtension type.
				// ignoring the resourceContent field for now
				cmpopts.IgnoreFields(runtime.RawExtension{}, "Raw"),
			}
			if diff := cmp.Diff(tc.wantResourceSnapshots[tc.wantLatestSnapshotIndex], *got, options...); diff != "" {
				t.Errorf("getOrCreateClusterResourceSnapshot() mismatch (-want, +got):\n%s", diff)
			}
			clusterResourceSnapshotList := &fleetv1beta1.ClusterResourceSnapshotList{}
			if err := fakeClient.List(ctx, clusterResourceSnapshotList); err != nil {
				t.Fatalf("clusterResourceSnapshot List() got error %v, want no error", err)
			}
			options = append(options, sortOption)
			if diff := cmp.Diff(tc.wantResourceSnapshots, clusterResourceSnapshotList.Items, options...); diff != "" {
				t.Errorf("clusterResourceSnapshot List() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestGetOrCreateClusterResourceSnapshot_failure(t *testing.T) {
	selectedResources := []fleetv1beta1.ResourceContent{
		*serviceResourceContentForTest(t),
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
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
			name: "existing active policy snapshot does not have hash annotation",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
							fleetv1beta1.ResourceIndexLabel:    "0",
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:   testName,
							fleetv1beta1.ResourceIndexLabel: "abc",
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:   testName,
							fleetv1beta1.ResourceIndexLabel: "abc",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:   testName,
							fleetv1beta1.ResourceIndexLabel: "abc",
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:   testName,
							fleetv1beta1.ResourceIndexLabel: "0",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "0",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:   testName,
							fleetv1beta1.ResourceIndexLabel: "1",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:   testName,
							fleetv1beta1.ResourceIndexLabel: "0",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "0",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:   testName,
							fleetv1beta1.ResourceIndexLabel: "1",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "-12",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
			_, err := r.getOrCreateClusterResourceSnapshot(ctx, crp, 0, resourceSnapshotSpecA, 1)
			if err == nil { // if error is nil
				t.Fatal("getOrCreateClusterResourceSnapshot() = nil, want err")
			}
			if !errors.Is(err, controller.ErrUnexpectedBehavior) {
				t.Errorf("getOrCreateClusterResourceSnapshot() got %v, want %v type", err, controller.ErrUnexpectedBehavior)
			}
		})
	}
}

func TestSplitSelectedResources(t *testing.T) {
	// test service is 383 bytes in size.
	serviceResourceContent := *serviceResourceContentForTest(t)
	// test deployment 390 bytes in size.
	deploymentResourceContent := *deploymentResourceContentForTest(t)
	// test secret is 152 bytes in size.
	secretResourceContent := *secretResourceContentForTest(t)
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
	}
	originalResourceSnapshotResourceSizeLimit := resourceSnapshotResourceSizeLimit
	defer func() {
		resourceSnapshotResourceSizeLimit = originalResourceSnapshotResourceSizeLimit
	}()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resourceSnapshotResourceSizeLimit = tc.selectedResourcesSizeLimit
			gotSplitSelectedResources := splitSelectedResources(tc.selectedResources)
			if diff := cmp.Diff(tc.wantSplitSelectedResources, gotSplitSelectedResources); diff != "" {
				t.Errorf("splitSelectedResources List() mismatch (-want, +got):\n%s", diff)
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
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel:      "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
							fleetv1beta1.ResourceIndexLabel:    "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "0",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
						},
					},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
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
						Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.PolicyIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   testName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testName,
								BlockOwnerDeletion: pointer.Bool(true),
								Controller:         pointer.Bool(true),
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
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
						},
					},
				},
			},
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
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
							fleetv1beta1.PolicyIndexLabel:      "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
						},
					},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:    "1",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
							fleetv1beta1.CRPTrackingLabel:      "another-crp",
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
			crp.Finalizers = []string{fleetv1beta1.ClusterResourcePlacementCleanupFinalizer}
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
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
					ObservedGeneration: crpGeneration,
				},
			},
		},
		{
			name: "schedule has not completed",
			conditions: []metav1.Condition{
				{
					Status:             metav1.ConditionUnknown,
					Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
					ObservedGeneration: crpGeneration,
				},
			},
			want: false,
		},
		{
			name: "sync has not completed",
			conditions: []metav1.Condition{
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					ObservedGeneration: crpGeneration - 1,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
					ObservedGeneration: crpGeneration,
				},
			},
			want: false,
		},
		{
			name: "apply has not completed",
			conditions: []metav1.Condition{
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
					ObservedGeneration: crpGeneration,
				},
				{
					Status:             metav1.ConditionTrue,
					Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
					ObservedGeneration: crpGeneration - 1,
				},
				{
					Status:             metav1.ConditionFalse,
					Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
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
					Name:       testName,
					Generation: crpGeneration,
				},
			}
			got := isRolloutCompleted(crp)
			if got != tc.want {
				t.Errorf("isRolloutCompleted() got %v, want %v", got, tc.want)
			}
		})
	}
}
