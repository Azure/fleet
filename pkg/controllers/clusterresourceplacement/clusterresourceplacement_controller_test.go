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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	testName = "my-crp"
)

var (
	fleetAPIVersion = fleetv1beta1.GroupVersion.String()
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
			Name: testName,
		},
		Spec: fleetv1beta1.ClusterResourcePlacementSpec{
			Policy: placementPolicyForTest(),
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
		},
	}
}

func TestGetOrCreateClusterPolicySnapshot(t *testing.T) {
	wantPolicy := placementPolicyForTest()
	wantPolicy.NumberOfClusters = nil
	jsonBytes, err := json.Marshal(wantPolicy)
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
		policySnapshots         []fleetv1beta1.ClusterSchedulingPolicySnapshot
		wantPolicySnapshots     []fleetv1beta1.ClusterSchedulingPolicySnapshot
		wantLatestSnapshotIndex int // index of the wantPolicySnapshots array
	}{
		{
			name: "new clusterResourcePolicy and no existing policy snapshots owned by my-crp",
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
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name: "crp policy has no change",
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
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
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
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
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
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
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
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
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
							fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantLatestSnapshotIndex: 2,
		},
		{
			name: "crp policy has changed and there is an active snapshot",
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
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name: "crp policy has been changed and reverted back and there is no active snapshot",
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
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
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
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
						PolicyHash: policyHash,
					},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name: "crp policy has not been changed and only the numberOfCluster is changed",
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
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
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
						},
					},
					Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
						Policy:     wantPolicy,
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
			objects := []client.Object{crp}
			for i := range tc.policySnapshots {
				objects = append(objects, &tc.policySnapshots[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{Client: fakeClient, Scheme: scheme}
			got, err := r.getOrCreateClusterPolicySnapshot(ctx, crp)
			if err != nil {
				t.Fatalf("failed to getOrCreateClusterPolicySnapshot: %v", err)
			}
			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
			}
			if diff := cmp.Diff(tc.wantPolicySnapshots[tc.wantLatestSnapshotIndex], *got, options...); diff != "" {
				t.Errorf("getOrCreateClusterPolicySnapshot() mismatch (-want, +got):\n%s", diff)
			}
			clusterPolicySnapshotList := &fleetv1beta1.ClusterSchedulingPolicySnapshotList{}
			if err := fakeClient.List(ctx, clusterPolicySnapshotList); err != nil {
				t.Fatalf("clusterPolicySnapshot List() got error %v, want no error", err)
			}
			if diff := cmp.Diff(tc.wantPolicySnapshots, clusterPolicySnapshotList.Items, options...); diff != "" {
				t.Errorf("clusterPolicysnapShot List() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestGetOrCreateClusterPolicySnapshot_failure(t *testing.T) {
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
			r := Reconciler{Client: fakeClient, Scheme: scheme}
			_, err := r.getOrCreateClusterPolicySnapshot(ctx, crp)
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

func TestGetOrCreateClusterResourceSnapshot(t *testing.T) {
	selectedResources := []fleetv1beta1.ResourceContent{
		*serviceResourceContentForTest(t),
	}
	policyA := "policy-a"
	resourceSnapshotSpecA := &fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources:  selectedResources,
		PolicySnapshotName: policyA,
	}
	jsonBytes, err := json.Marshal(resourceSnapshotSpecA)
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecA hash: %v", err)
	}
	resourceSnapshotAHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	policyB := "policy-b"
	resourceSnapshotSpecB := &fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources:  selectedResources,
		PolicySnapshotName: policyB,
	}

	jsonBytes, err = json.Marshal(resourceSnapshotSpecB)
	if err != nil {
		t.Fatalf("failed to create the policy hash: %v", err)
	}
	resourceSnapshotBHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	tests := []struct {
		name                    string
		resourceSnapshotSpec    *fleetv1beta1.ResourceSnapshotSpec
		resourceSnapshots       []fleetv1beta1.ClusterResourceSnapshot
		wantResourceSnapshots   []fleetv1beta1.ClusterResourceSnapshot
		wantLatestSnapshotIndex int // index of the wantPolicySnapshots array
	}{
		{
			name:                 "new resourceSnapshot and no existing snapshots owned by my-crp",
			resourceSnapshotSpec: resourceSnapshotSpecA,
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
							fleetv1beta1.ResourceGroupHashAnnotation: resourceSnapshotAHash,
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
							fleetv1beta1.ResourceGroupHashAnnotation: resourceSnapshotAHash,
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
							fleetv1beta1.ResourceGroupHashAnnotation: resourceSnapshotAHash,
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name: "resource has changed and there is no active snapshot",
			// It happens when last reconcile loop fails after setting the latest label to false and
			// before creating a new resource snapshot.
			resourceSnapshotSpec: resourceSnapshotSpecB,
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
							fleetv1beta1.ResourceGroupHashAnnotation: resourceSnapshotAHash,
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
							fleetv1beta1.ResourceGroupHashAnnotation: resourceSnapshotAHash,
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
							fleetv1beta1.ResourceGroupHashAnnotation: resourceSnapshotBHash,
						},
					},
					Spec: *resourceSnapshotSpecB,
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                 "resource has changed and there is an active snapshot",
			resourceSnapshotSpec: resourceSnapshotSpecB,
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
							fleetv1beta1.ResourceGroupHashAnnotation: resourceSnapshotAHash,
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
							fleetv1beta1.ResourceGroupHashAnnotation: resourceSnapshotAHash,
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
							fleetv1beta1.ResourceGroupHashAnnotation: resourceSnapshotBHash,
						},
					},
					Spec: *resourceSnapshotSpecB,
				},
			},
			wantLatestSnapshotIndex: 1,
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
							fleetv1beta1.ResourceGroupHashAnnotation: resourceSnapshotAHash,
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
							fleetv1beta1.ResourceGroupHashAnnotation: resourceSnapshotAHash,
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
			wantLatestSnapshotIndex: 0,
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
			got, err := r.getOrCreateClusterResourceSnapshot(ctx, crp, tc.resourceSnapshotSpec)
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
	policyA := "policy-a"
	resourceSnapshotSpecA := &fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources:  selectedResources,
		PolicySnapshotName: policyA,
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
			name: "no active policy snapshot exists and policySnapshot with invalid resourceIndex label",
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
			name: "multiple active policy snapshot exist",
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
			_, err := r.getOrCreateClusterResourceSnapshot(ctx, crp, resourceSnapshotSpecA)
			if err == nil { // if error is nil
				t.Fatal("getOrCreateClusterResourceSnapshot() = nil, want err")
			}
			if !errors.Is(err, controller.ErrUnexpectedBehavior) {
				t.Errorf("getOrCreateClusterResourceSnapshot() got %v, want %v type", err, controller.ErrUnexpectedBehavior)
			}
		})
	}
}
