package clusterresourceplacement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
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
		},
	}
}

func TestHandleUpdate(t *testing.T) {
	jsonBytes, err := json.Marshal(placementPolicyForTest())
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
		name                string
		policySnapshots     []fleetv1beta1.ClusterPolicySnapshot
		wantPolicySnapshots []fleetv1beta1.ClusterPolicySnapshot
	}{
		{
			name: "new clusterResourcePolicy and no existing policy snapshots owned by my-crp",
			policySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
			wantPolicySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
					},
					Spec: fleetv1beta1.PolicySnapshotSpec{
						Policy:     placementPolicyForTest(),
						PolicyHash: policyHash,
					},
				},
			},
		},
		{
			name: "crp policy has no change",
			policySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
					Spec: fleetv1beta1.PolicySnapshotSpec{
						Policy:     placementPolicyForTest(),
						PolicyHash: policyHash,
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
					Spec: fleetv1beta1.PolicySnapshotSpec{
						Policy:     placementPolicyForTest(),
						PolicyHash: policyHash,
					},
				},
			},
		},
		{
			name: "crp policy has changed and there is no active snapshot",
			// It happens when last reconcile loop fails after setting the latest label to false and
			// before creating a new policy snapshot.
			policySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
					Spec: fleetv1beta1.PolicySnapshotSpec{
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
					Spec: fleetv1beta1.PolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
					Spec: fleetv1beta1.PolicySnapshotSpec{
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
					Spec: fleetv1beta1.PolicySnapshotSpec{
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
					},
					Spec: fleetv1beta1.PolicySnapshotSpec{
						Policy:     placementPolicyForTest(),
						PolicyHash: policyHash,
					},
				},
			},
		},
		{
			name: "crp policy has changed and there is an active snapshot",
			policySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
					Spec: fleetv1beta1.PolicySnapshotSpec{
						// Policy is not specified.
						PolicyHash: unspecifiedPolicyHash,
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
					Spec: fleetv1beta1.PolicySnapshotSpec{
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
					},
					Spec: fleetv1beta1.PolicySnapshotSpec{
						Policy:     placementPolicyForTest(),
						PolicyHash: policyHash,
					},
				},
			},
		},
		{
			name: "crp policy has been changed and reverted back and there is no active snapshot",
			policySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
					Spec: fleetv1beta1.PolicySnapshotSpec{
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
					},
					Spec: fleetv1beta1.PolicySnapshotSpec{
						Policy:     placementPolicyForTest(),
						PolicyHash: policyHash,
					},
				},
			},
			wantPolicySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
					Spec: fleetv1beta1.PolicySnapshotSpec{
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
					},
					Spec: fleetv1beta1.PolicySnapshotSpec{
						Policy:     placementPolicyForTest(),
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
			got, err := r.handleUpdate(ctx, crp)
			if err != nil {
				t.Fatalf("failed to handle update: %v", err)
			}
			want := ctrl.Result{}
			if !cmp.Equal(got, want) {
				t.Errorf("handleUpdate() = %+v, want %+v", got, want)
			}
			clusterPolicySnapshotList := &fleetv1beta1.ClusterPolicySnapshotList{}
			if err := fakeClient.List(ctx, clusterPolicySnapshotList); err != nil {
				t.Fatalf("clusterPolicySnapshot List() got error %v, want no error", err)
			}
			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
			}
			if diff := cmp.Diff(tc.wantPolicySnapshots, clusterPolicySnapshotList.Items, options...); diff != "" {
				t.Errorf("clusterPolicysnapShot List() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestHandleUpdate_failure(t *testing.T) {
	tests := []struct {
		name            string
		policySnapshots []fleetv1beta1.ClusterPolicySnapshot
	}{
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterPolicySnapshot.
			name: "existing active policy snapshot does not have policyIndex label",
			policySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
			policySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
			policySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
			policySnapshots: []fleetv1beta1.ClusterPolicySnapshot{
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
			got, err := r.handleUpdate(ctx, crp)
			if err == nil { // if error is nil
				t.Fatal("handleUpdate = nil, want err")
			}
			if !errors.Is(err, controller.ErrUnexpectedBehavior) {
				t.Errorf("handleUpdate() got %v, want %v type", err, controller.ErrUnexpectedBehavior)
			}
			want := ctrl.Result{}
			if !cmp.Equal(got, want) {
				t.Errorf("handleUpdate() = %+v, want %+v", got, want)
			}
		})
	}
}
