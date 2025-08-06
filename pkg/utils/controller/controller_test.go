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

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/test/utils/resource"
)

const (
	defaultPlacementName = "my-test-crp"
	defaultNamespace     = "test-namespace"
)

// lessFuncResourceIdentifier is a less function for sorting resource identifiers
// copied from the common as there is cyclical imports
var lessFuncResourceIdentifier = func(a, b fleetv1beta1.ResourceIdentifier) bool {
	aStr := fmt.Sprintf("%s/%s/%s/%s/%s", a.Group, a.Version, a.Kind, a.Namespace, a.Name)
	bStr := fmt.Sprintf("%s/%s/%s/%s/%s", b.Group, b.Version, b.Kind, b.Namespace, b.Name)
	return aStr < bStr
}

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
			name:      "reading from cache: apiServerError",
			fromCache: true,
			err:       apierrors.NewConflict(schema.GroupResource{}, "conflict", nil),
			wantErr:   ErrAPIServerError,
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
		{
			name:      "reading from API server: context canceled",
			fromCache: false,
			err:       fmt.Errorf("client rate limiter Wait returned an error: %w", context.Canceled),
			wantErr:   ErrAPIServerError,
		},
		{
			name:      "reading from API server: deadline exceeded",
			fromCache: false,
			err:       fmt.Errorf("client rate limiter Wait returned an error: %w", context.DeadlineExceeded),
			wantErr:   ErrAPIServerError,
		},
		{
			name:      "reading from cache: context canceled",
			fromCache: true,
			err:       fmt.Errorf("client rate limiter Wait returned an error: %w", context.Canceled),
			wantErr:   ErrAPIServerError,
		},
		{
			name:      "reading from cache: deadline exceeded",
			fromCache: true,
			err:       fmt.Errorf("client rate limiter Wait returned an error: %w", context.DeadlineExceeded),
			wantErr:   ErrAPIServerError,
		},
		{
			name:      "reading from cache: missing kind error",
			fromCache: true,
			err:       runtime.NewMissingKindErr("unstructured object has no kind"),
			wantErr:   ErrUnexpectedBehavior,
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

func TestCollectResourceIdentifiersFromResourceSnapshot(t *testing.T) {
	namespaceResourceContent := *resource.NamespaceResourceContentForTest(t)
	deploymentResourceContent := *resource.DeploymentResourceContentForTest(t)
	clusterResourceEnvelopeContent := *resource.ClusterResourceEnvelopeResourceContentForTest(t)
	resourceEnvelopeContent := *resource.ResourceEnvelopeResourceContentForTest(t)

	tests := []struct {
		name                  string
		resourceSnapshotIndex string
		snapshots             []fleetv1beta1.ResourceSnapshotObj
		want                  []fleetv1beta1.ResourceIdentifier
		wantErr               error
	}{
		{
			name:                  "no resource snapshots found",
			resourceSnapshotIndex: "0",
			snapshots:             []fleetv1beta1.ResourceSnapshotObj{},
			want:                  nil,
			wantErr:               nil,
		},
		{
			name:                  "no master cluster resource snapshot found",
			resourceSnapshotIndex: "0",
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
			},
			want:    []fleetv1beta1.ResourceIdentifier{},
			wantErr: ErrUnexpectedBehavior,
		},
		{
			name:                  "no master namespaced resource snapshot found",
			resourceSnapshotIndex: "0",
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
			},
			want:    []fleetv1beta1.ResourceIdentifier{},
			wantErr: ErrUnexpectedBehavior,
		},
		{
			name:                  "cluster resource snapshot without any resources",
			resourceSnapshotIndex: "0",
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
				},
			},
			want:    []fleetv1beta1.ResourceIdentifier{},
			wantErr: nil,
		},
		{
			name:                  "namespaced resource snapshot without any resources",
			resourceSnapshotIndex: "0",
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
				},
			},
			want:    []fleetv1beta1.ResourceIdentifier{},
			wantErr: nil,
		},
		{
			name:                  "only master cluster resource snapshot found with cluster-scoped resource, namespace-scoped resource and resource wrapped with envelope",
			resourceSnapshotIndex: "0",
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							namespaceResourceContent,
							deploymentResourceContent,
							clusterResourceEnvelopeContent,
							resourceEnvelopeContent,
						},
					},
				},
			},
			want: []fleetv1beta1.ResourceIdentifier{
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
				// The envelope resources themselves are included, not the wrapped resources.
				{
					Group:     "placement.kubernetes-fleet.io",
					Version:   "v1beta1",
					Kind:      "ClusterResourceEnvelope",
					Namespace: "",
					Name:      "test-cluster-resource-envelope",
				},
				{
					Group:     "placement.kubernetes-fleet.io",
					Version:   "v1beta1",
					Kind:      "ResourceEnvelope",
					Namespace: "test-namespace",
					Name:      "test-resource-envelope",
				},
			},
			wantErr: nil,
		},
		{
			name:                  "both master and subindex cluster resource snapshots found with cluster-scoped resource, namespace-scoped resource and resource wrapped with envelope",
			resourceSnapshotIndex: "0",
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "4",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							namespaceResourceContent,
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
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
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							clusterResourceEnvelopeContent,
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 2),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							resourceEnvelopeContent,
						},
					},
				},
			},
			want: []fleetv1beta1.ResourceIdentifier{
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
				// The envelope resources themselves are included, not the wrapped resources.
				{
					Group:     "placement.kubernetes-fleet.io",
					Version:   "v1beta1",
					Kind:      "ClusterResourceEnvelope",
					Namespace: "",
					Name:      "test-cluster-resource-envelope",
				},
				{
					Group:     "placement.kubernetes-fleet.io",
					Version:   "v1beta1",
					Kind:      "ResourceEnvelope",
					Namespace: "test-namespace",
					Name:      "test-resource-envelope",
				},
			},
			wantErr: nil,
		},
		{
			name:                  "both master and subindex namespaced resource snapshots found with cluster-scoped resource, namespace-scoped resource and resource wrapped with envelope",
			resourceSnapshotIndex: "0",
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "4",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							namespaceResourceContent,
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
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
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 1),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							clusterResourceEnvelopeContent,
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 2),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							resourceEnvelopeContent,
						},
					},
				},
			},
			want: []fleetv1beta1.ResourceIdentifier{
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
				// The envelope resources themselves are included, not the wrapped resources.
				{
					Group:     "placement.kubernetes-fleet.io",
					Version:   "v1beta1",
					Kind:      "ClusterResourceEnvelope",
					Namespace: "",
					Name:      "test-cluster-resource-envelope",
				},
				{
					Group:     "placement.kubernetes-fleet.io",
					Version:   "v1beta1",
					Kind:      "ResourceEnvelope",
					Namespace: "test-namespace",
					Name:      "test-resource-envelope",
				},
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := serviceScheme(t)
			objects := []client.Object{}
			for i := range tc.snapshots {
				objects = append(objects, tc.snapshots[i])
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			placementKey := defaultPlacementName
			if len(tc.snapshots) > 0 && tc.snapshots[0].GetNamespace() != "" {
				// Namespaced snapshot
				placementKey = defaultNamespace + namespaceSeparator + defaultPlacementName
			}
			got, err := CollectResourceIdentifiersFromResourceSnapshot(context.Background(), fakeClient, placementKey, tc.resourceSnapshotIndex)
			if gotErr, wantErr := err != nil, tc.wantErr != nil; gotErr != wantErr || !errors.Is(err, tc.wantErr) {
				t.Fatalf("CollectResourceIdentifiersFromClusterResourceSnapshot() got error %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}

			// Use cmp.Diff with SortSlices to ensure deterministic comparison
			options := []cmp.Option{cmpopts.SortSlices(lessFuncResourceIdentifier)}
			if diff := cmp.Diff(tc.want, got, options...); diff != "" {
				t.Errorf("FetchAllClusterResourceSnapshots() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestCollectResourceIdentifiersUsingMasterResourceSnapshot(t *testing.T) {
	namespaceResourceContent := *resource.NamespaceResourceContentForTest(t)
	deploymentResourceContent := *resource.DeploymentResourceContentForTest(t)
	clusterResourceEnvelopeContent := *resource.ClusterResourceEnvelopeResourceContentForTest(t)
	resourceEnvelopeContent := *resource.ResourceEnvelopeResourceContentForTest(t)

	tests := []struct {
		name                   string
		masterResourceSnapshot fleetv1beta1.ResourceSnapshotObj
		resourceSnapshotIndex  string
		snapshots              []fleetv1beta1.ResourceSnapshotObj
		want                   []fleetv1beta1.ResourceIdentifier
		wantErr                error
	}{
		{
			name:                  "some of resource snapshots have not been created yet",
			resourceSnapshotIndex: "0",
			masterResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
			},
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
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
			name:                  "resource snapshot without any resources",
			resourceSnapshotIndex: "0",
			masterResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
					},
				},
			},
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
				},
			},
			want: []fleetv1beta1.ResourceIdentifier{},
		},
		{
			name:                  "only master resource snapshot found with cluster-scoped resource, namespace-scoped resource and resource wrapped with envelope",
			resourceSnapshotIndex: "0",
			masterResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "2",
					},
				},
				Spec: fleetv1beta1.ResourceSnapshotSpec{
					SelectedResources: []fleetv1beta1.ResourceContent{
						namespaceResourceContent,
						deploymentResourceContent,
						clusterResourceEnvelopeContent,
						resourceEnvelopeContent,
					},
				},
			},
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							namespaceResourceContent,
							deploymentResourceContent,
							clusterResourceEnvelopeContent,
							resourceEnvelopeContent,
						},
					},
				},
			},
			want: []fleetv1beta1.ResourceIdentifier{
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
				// The envelope resources themselves are included, not the wrapped resources.
				{
					Group:     "placement.kubernetes-fleet.io",
					Version:   "v1beta1",
					Kind:      "ClusterResourceEnvelope",
					Namespace: "",
					Name:      "test-cluster-resource-envelope",
				},
				{
					Group:     "placement.kubernetes-fleet.io",
					Version:   "v1beta1",
					Kind:      "ResourceEnvelope",
					Namespace: "test-namespace",
					Name:      "test-resource-envelope",
				},
			},
		},
		{
			name:                  "both master and subindex resource snapshots found with cluster-scoped resource, namespace-scoped resource and resource wrapped with envelope",
			resourceSnapshotIndex: "1",
			masterResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 1),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "1",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "4",
						fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "2",
					},
				},
				Spec: fleetv1beta1.ResourceSnapshotSpec{
					SelectedResources: []fleetv1beta1.ResourceContent{
						namespaceResourceContent,
					},
				},
			},
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "4",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							namespaceResourceContent,
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
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
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 1, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							clusterResourceEnvelopeContent,
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 1, 2),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							resourceEnvelopeContent,
						},
					},
				},
			},
			want: []fleetv1beta1.ResourceIdentifier{
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
				// The envelope resources themselves are included, not the wrapped resources.
				{
					Group:     "placement.kubernetes-fleet.io",
					Version:   "v1beta1",
					Kind:      "ClusterResourceEnvelope",
					Namespace: "",
					Name:      "test-cluster-resource-envelope",
				},
				{
					Group:     "placement.kubernetes-fleet.io",
					Version:   "v1beta1",
					Kind:      "ResourceEnvelope",
					Namespace: "test-namespace",
					Name:      "test-resource-envelope",
				},
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := serviceScheme(t)
			objects := []client.Object{}
			for i := range tc.snapshots {
				objects = append(objects, tc.snapshots[i])
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			got, err := CollectResourceIdentifiersUsingMasterResourceSnapshot(context.Background(), fakeClient, defaultPlacementName, tc.masterResourceSnapshot, tc.resourceSnapshotIndex)
			if gotErr, wantErr := err != nil, tc.wantErr != nil; gotErr != wantErr || !errors.Is(err, tc.wantErr) {
				t.Fatalf("CollectResourceIdentifiersFromClusterResourceSnapshot() got error %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}

			// Use cmp.Diff with SortSlices to ensure deterministic comparison
			options := []cmp.Option{cmpopts.SortSlices(lessFuncResourceIdentifier)}

			if diff := cmp.Diff(tc.want, got, options...); diff != "" {
				t.Errorf("FetchAllClusterResourceSnapshots() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
