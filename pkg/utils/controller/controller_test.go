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
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestNewUnexpectedBehaviorError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{
			name: "nil error",
			err:  nil,
		},
		{
			name:    "unexpectedBehaviorError",
			err:     errors.New("unexpected"),
			wantErr: ErrUnexpectedBehavior,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewUnexpectedBehaviorError(tc.err)
			if tc.err == nil && got != nil {
				t.Fatalf("NewUnexpectedBehaviorError(nil) = %v, want nil", got)
			}
			if tc.err != nil && !errors.Is(got, tc.wantErr) {
				t.Fatalf("NewUnexpectedBehaviorError() = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

func TestNewExpectedBehaviorError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{
			name: "nil error",
			err:  nil,
		},
		{
			name:    "expectedBehaviorError",
			err:     errors.New("expected"),
			wantErr: ErrExpectedBehavior,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewExpectedBehaviorError(tc.err)
			if tc.err == nil && got != nil {
				t.Fatalf("NewExpectedBehaviorError(nil) = %v, want nil", got)
			}
			if tc.err != nil && !errors.Is(got, tc.wantErr) {
				t.Fatalf("NewExpectedBehaviorError() = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

func TestNewAPIServerError(t *testing.T) {
	tests := []struct {
		name      string
		fromCache bool
		err       error
		wantErr   error
	}{
		{
			name: "nil error",
			err:  nil,
		},
		{
			name:      "reading from cache: apiServerError",
			fromCache: true,
			err:       apierrors.NewNotFound(schema.GroupResource{}, "invalid"),
			wantErr:   ErrAPIServerError,
		},
		{
			name:      "reading from cache: unexpectedBehaviorError",
			fromCache: true,
			err:       apierrors.NewConflict(schema.GroupResource{}, "conflict", nil),
			wantErr:   ErrUnexpectedBehavior,
		},
		{
			name:      "reading from API server: apiServerError",
			fromCache: false,
			err:       apierrors.NewNotFound(schema.GroupResource{}, "invalid"),
			wantErr:   ErrAPIServerError,
		},
		{
			name:      "reading from API server: apiServerError",
			fromCache: false,
			err:       apierrors.NewConflict(schema.GroupResource{}, "conflict", nil),
			wantErr:   ErrAPIServerError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewAPIServerError(tc.fromCache, tc.err)
			if tc.err == nil && got != nil {
				t.Fatalf("NewAPIServerError(nil) = %v, want nil", got)
			}
			if tc.err != nil && !errors.Is(got, tc.wantErr) {
				t.Fatalf("NewAPIServerError() = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

func TestNewUserError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{
			name: "nil error",
			err:  nil,
		},
		{
			name:    "userError",
			err:     errors.New("user error"),
			wantErr: ErrUserError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewUserError(tc.err)
			if tc.err == nil && got != nil {
				t.Fatalf("NewUserError(nil) = %v, want nil", got)
			}
			if tc.err != nil && !errors.Is(got, tc.wantErr) {
				t.Fatalf("NewUserError() = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

func TestNewUpdateIgnoreConflictError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{
			name: "nil error leads to nil error",
			err:  nil,
		},
		{
			name:    "conflict error is expected",
			err:     apierrors.NewConflict(schema.GroupResource{}, "conflict", nil),
			wantErr: ErrExpectedBehavior,
		},
		{
			name:    "not found error is not expected",
			err:     apierrors.NewNotFound(schema.GroupResource{}, "bad"),
			wantErr: ErrAPIServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotError := NewUpdateIgnoreConflictError(tt.err)
			if tt.err == nil && gotError != nil {
				t.Errorf("NewUpdateIgnoreConflictError() error = %v, nil", gotError)
			}
			if tt.err != nil && !errors.Is(gotError, tt.wantErr) {
				t.Fatalf("NewUpdateIgnoreConflictError() = %v, want %v", gotError, tt.wantErr)
			}
		})
	}
}

func TestNewCreateIgnoreAlreadyExistError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{
			name: "nil error leads to nil error",
			err:  nil,
		},
		{
			name:    "already exist error is expected",
			err:     apierrors.NewAlreadyExists(schema.GroupResource{}, "conflict"),
			wantErr: ErrExpectedBehavior,
		},
		{
			name:    "NewNotFound error is not expected",
			err:     apierrors.NewNotFound(schema.GroupResource{}, "bad"),
			wantErr: ErrAPIServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotError := NewCreateIgnoreAlreadyExistError(tt.err)
			if tt.err == nil && gotError != nil {
				t.Errorf("NewCreateIgnoreAlreadyExistError() error = %v, nil", gotError)
			}
			if tt.err != nil && !errors.Is(gotError, tt.wantErr) {
				t.Fatalf("NewCreateIgnoreAlreadyExistError() = %v, want %v", gotError, tt.wantErr)
			}
		})
	}
}

func serviceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}
	return scheme
}

func TestFetchAllClusterResourceSnapshots(t *testing.T) {
	crp := "my-test-crp"
	tests := []struct {
		name      string
		master    *fleetv1beta1.ClusterResourceSnapshot
		snapshots []fleetv1beta1.ClusterResourceSnapshot
		want      map[string]*fleetv1beta1.ClusterResourceSnapshot
		wantErr   error
	}{
		{
			name: "single resource snapshot",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crp, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "0",
						fleetv1beta1.CRPTrackingLabel:   crp,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: map[string]*fleetv1beta1.ClusterResourceSnapshot{
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crp, 0): {
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crp, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   crp,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
				},
			},
		},
		{
			name: "multiple resource snapshots",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crp, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "0",
						fleetv1beta1.CRPTrackingLabel:   crp,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
			},
			snapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, crp, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   crp,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, crp, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   crp,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
				},
			},
			want: map[string]*fleetv1beta1.ClusterResourceSnapshot{
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, crp, 0, 0): {
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, crp, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   crp,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, crp, 0, 1): {
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, crp, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   crp,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
				},
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crp, 0): {
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crp, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   crp,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
					},
				},
			},
		},
		{
			name: "some of resource snapshots have not been created yet",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crp, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "0",
						fleetv1beta1.CRPTrackingLabel:   crp,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
			},
			snapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, crp, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   crp,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
			},
			wantErr: ErrExpectedBehavior,
		},
		{
			name: "invalid numberOfResourceSnapshotsAnnotation",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crp, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "0",
						fleetv1beta1.CRPTrackingLabel:   crp,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "-1",
					},
				},
			},
			wantErr: ErrUnexpectedBehavior,
		},
		{
			name: "invalid resource index label of master resource snapshot",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crp, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "-2",
						fleetv1beta1.CRPTrackingLabel:   crp,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
			},
			wantErr: ErrUnexpectedBehavior,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := serviceScheme(t)
			objects := []client.Object{tc.master}
			for i := range tc.snapshots {
				objects = append(objects, &tc.snapshots[i])
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			got, err := FetchAllClusterResourceSnapshots(context.Background(), fakeClient, crp, tc.master)
			if gotErr, wantErr := err != nil, tc.wantErr != nil; gotErr != wantErr || !errors.Is(err, tc.wantErr) {
				t.Fatalf("FetchAllClusterResourceSnapshots() got error %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
				cmpopts.SortMaps(func(s1, s2 string) bool {
					return s1 < s2
				}),
			}
			if diff := cmp.Diff(tc.want, got, options...); diff != "" {
				t.Errorf("FetchAllClusterResourceSnapshots() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
