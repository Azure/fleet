/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package work

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
)

var (
	testWorkNamespace = "test-work-namespace"
)

func serviceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return scheme
}

func TestFindConflictedWork(t *testing.T) {
	tests := []struct {
		name          string
		applyStrategy placementv1beta1.ApplyStrategy
		ownerRefs     []metav1.OwnerReference
		works         []placementv1beta1.Work
		wantWorkName  string
		wantErr       error
	}{
		{
			name: "no conflicted work",
			applyStrategy: placementv1beta1.ApplyStrategy{
				Type: placementv1beta1.ApplyStrategyTypeClientSideApply,
			},
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work1",
				},
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work2",
				},
			},
			works: []placementv1beta1.Work{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work1",
					},
					Spec: placementv1beta1.WorkSpec{
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							Type: placementv1beta1.ApplyStrategyTypeClientSideApply,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work2",
					},
				},
			},
		},
		{
			name: "owner is not a work",
			applyStrategy: placementv1beta1.ApplyStrategy{
				Type: placementv1beta1.ApplyStrategyTypeClientSideApply,
			},
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: "invalid",
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work1",
				},
				{
					APIVersion: "invalid",
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work2",
				},
			},
			wantWorkName: "",
		},
		{
			name: "conflicted work found for failIfExists strategy",
			applyStrategy: placementv1beta1.ApplyStrategy{
				Type: placementv1beta1.ApplyStrategyTypeClientSideApply,
			},
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work1",
				},
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work2",
				},
			},
			works: []placementv1beta1.Work{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work1",
					},
					Spec: placementv1beta1.WorkSpec{
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							Type: placementv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work2",
					},
				},
			},
			wantWorkName: "work1",
		},
		{
			name: "conflicted work found for serverSideApply strategy",
			applyStrategy: placementv1beta1.ApplyStrategy{
				Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
				ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: false},
			},
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work1",
				},
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work2",
				},
			},
			works: []placementv1beta1.Work{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work1",
					},
					Spec: placementv1beta1.WorkSpec{
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							Type: placementv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work2",
					},
					Spec: placementv1beta1.WorkSpec{
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
							ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: true},
						},
					},
				},
			},
			wantWorkName: "work2",
		},
		{
			name: "work not found",
			applyStrategy: placementv1beta1.ApplyStrategy{
				Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
				ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: false},
			},
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work1",
				},
			},
			wantErr: controller.ErrExpectedBehavior,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			var objects []client.Object
			for i := range tc.works {
				objects = append(objects, &tc.works[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			got, err := findConflictedWork(ctx, fakeClient, testWorkNamespace, &tc.applyStrategy, tc.ownerRefs)
			if gotErr, wantErr := err != nil, tc.wantErr != nil; gotErr != wantErr || !errors.Is(err, tc.wantErr) {
				t.Fatalf("findConflictedWork() got error %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if got == nil && tc.wantWorkName != "" || got != nil && got.Name != tc.wantWorkName {
				t.Errorf("findConflictedWork() got %v, want %v", got.Name, tc.wantWorkName)
			}
		})
	}
}

func TestValidateOwnerReference(t *testing.T) {
	tests := []struct {
		name          string
		applyStrategy placementv1beta1.ApplyStrategy
		ownerRefs     []metav1.OwnerReference
		works         []placementv1beta1.Work
		want          ApplyAction
		wantErr       error
	}{
		{
			name: "conflicted work found for serverSideApply strategy",
			applyStrategy: placementv1beta1.ApplyStrategy{
				Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
				ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: false},
			},
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work1",
				},
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work2",
				},
			},
			works: []placementv1beta1.Work{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work1",
					},
					Spec: placementv1beta1.WorkSpec{
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							Type: placementv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work2",
					},
					Spec: placementv1beta1.WorkSpec{
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
							ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: true},
						},
					},
				},
			},
			want:    applyConflictBetweenPlacements,
			wantErr: controller.ErrUserError,
		},
		{
			name: "work not found",
			applyStrategy: placementv1beta1.ApplyStrategy{
				Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
				ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: false},
			},
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work1",
				},
			},
			want:    errorApplyAction,
			wantErr: controller.ErrExpectedBehavior,
		},
		{
			name: "no conflicted work and not owned by others",
			applyStrategy: placementv1beta1.ApplyStrategy{
				Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
				AllowCoOwnership: false,
			},
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work1",
				},
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work2",
				},
			},
			works: []placementv1beta1.Work{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work1",
					},
					Spec: placementv1beta1.WorkSpec{
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
							AllowCoOwnership: false,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work2",
					},
				},
			},
		},
		{
			name: "no conflicted work and owned by others (not allowed)",
			applyStrategy: placementv1beta1.ApplyStrategy{
				Type: placementv1beta1.ApplyStrategyTypeClientSideApply,
			},
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       "another-type",
					Name:       "work1",
				},
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work2",
				},
			},
			works: []placementv1beta1.Work{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work2",
					},
				},
			},
			want:    manifestAlreadyOwnedByOthers,
			wantErr: controller.ErrUserError,
		},
		{
			name: "no conflicted work and owned by others (allowed)",
			applyStrategy: placementv1beta1.ApplyStrategy{
				Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
				AllowCoOwnership: true,
			},
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       "another-type",
					Name:       "work1",
				},
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work2",
				},
			},
			works: []placementv1beta1.Work{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work2",
					},
					Spec: placementv1beta1.WorkSpec{
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
							AllowCoOwnership: true,
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			var objects []client.Object
			for i := range tc.works {
				objects = append(objects, &tc.works[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			got, err := validateOwnerReference(ctx, fakeClient, testWorkNamespace, &tc.applyStrategy, tc.ownerRefs)
			if gotErr, wantErr := err != nil, tc.wantErr != nil; gotErr != wantErr || !errors.Is(err, tc.wantErr) {
				t.Fatalf("validateOwnerReference() got error %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if got != tc.want {
				t.Errorf("validateOwnerReference() got %v, want %v", got, tc.want)
			}
		})
	}
}
