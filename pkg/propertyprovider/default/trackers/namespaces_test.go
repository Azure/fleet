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

package trackers

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// namespaceTrackerCmpOptions defines the common comparison options for NamespaceTracker tests.
var namespaceTrackerCmpOptions = []cmp.Option{
	cmpopts.IgnoreFields(NamespaceTracker{}, "client", "mu"),
	cmpopts.IgnoreTypes(time.Time{}),
	cmp.AllowUnexported(NamespaceTracker{}),
}

func TestNamespaceTracker_AddOrUpdate(t *testing.T) {
	namespaceName := "test-namespace"
	newNamespaceName := "new-test-namespace"
	s := scheme.Scheme
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

	tests := []struct {
		name        string
		tracker     *NamespaceTracker
		namespace   *corev1.Namespace
		trackLimit  int
		wantTracker *NamespaceTracker
	}{
		{
			name: "add new namespace",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					namespaceName: "",
				},
				namespaceCreationTimes: map[string]time.Time{
					namespaceName: time.Now(),
				},
				client: fakeClient,
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: newNamespaceName,
				},
			},
			trackLimit: 200,
			wantTracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					namespaceName:    "",
					newNamespaceName: "",
				},
				namespaceCreationTimes: map[string]time.Time{
					namespaceName:    time.Now(),
					newNamespaceName: time.Now(),
				},
				client: fakeClient,
			},
		},
		{
			name: "update existing namespace",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					namespaceName: "",
				},
				namespaceCreationTimes: map[string]time.Time{
					namespaceName: time.Now(),
				},
				client: fakeClient,
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
					Labels: map[string]string{
						"updated": "true",
					},
				},
			},
			trackLimit: 200,
			wantTracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					namespaceName: "",
				},
				namespaceCreationTimes: map[string]time.Time{
					namespaceName: time.Now(),
				},
				client: fakeClient,
			},
		},
		{
			name: "try to add when limit reached",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					namespaceName: "",
				},
				namespaceCreationTimes: map[string]time.Time{
					namespaceName: time.Now(),
				},
				client: fakeClient,
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: newNamespaceName,
				},
			},
			trackLimit: 1,
			wantTracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					namespaceName: "",
				},
				namespaceCreationTimes: map[string]time.Time{
					namespaceName: time.Now(),
				},
				reachLimit: true,
				client:     fakeClient,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLimit := namespaceTrackerLimit
			defer func() {
				namespaceTrackerLimit = originalLimit
			}()
			namespaceTrackerLimit = tt.trackLimit
			tt.tracker.AddOrUpdate(tt.namespace)

			if diff := cmp.Diff(tt.wantTracker, tt.tracker, namespaceTrackerCmpOptions...); diff != "" {
				t.Errorf("AddOrUpdate() tracker mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNamespaceTracker_Remove(t *testing.T) {
	namespaceName := "test-namespace"
	anotherNamespaceName := "another-namespace"
	s := scheme.Scheme
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

	tests := []struct {
		name              string
		tracker           *NamespaceTracker
		trackLimit        int
		namespaceToRemove string
		wantTracker       *NamespaceTracker
	}{
		{
			name: "remove existing namespace",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					namespaceName:        "",
					anotherNamespaceName: "",
				},
				namespaceCreationTimes: map[string]time.Time{
					namespaceName:        time.Now(),
					anotherNamespaceName: time.Now(),
				},
				client: fakeClient,
			},
			trackLimit:        200,
			namespaceToRemove: namespaceName,
			wantTracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					anotherNamespaceName: "",
				},
				namespaceCreationTimes: map[string]time.Time{
					anotherNamespaceName: time.Now(),
				},
				client: fakeClient,
			},
		},
		{
			name: "remove non-existent namespace",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					namespaceName: "",
				},
				namespaceCreationTimes: map[string]time.Time{
					namespaceName: time.Now(),
				},
				client: fakeClient,
			},
			trackLimit:        200,
			namespaceToRemove: "non-existent",
			wantTracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					namespaceName: "",
				},
				namespaceCreationTimes: map[string]time.Time{
					namespaceName: time.Now(),
				},
				client: fakeClient,
			},
		},
		{
			name: "remove from empty tracker",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{},
				namespaceCreationTimes: map[string]time.Time{},
				client:                 fakeClient,
			},
			trackLimit:        200,
			namespaceToRemove: namespaceName,
			wantTracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{},
				namespaceCreationTimes: map[string]time.Time{},
				reachLimit:             false,
				client:                 fakeClient,
			},
		},
		{
			name: "remove namespace when limit reached - should reset reachLimit to false",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					namespaceName: "",
				},
				namespaceCreationTimes: map[string]time.Time{
					namespaceName: time.Now(),
				},
				reachLimit: true,
				client:     fakeClient,
			},
			trackLimit:        1,
			namespaceToRemove: namespaceName,
			wantTracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{},
				namespaceCreationTimes: map[string]time.Time{},
				client:                 fakeClient,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLimit := namespaceTrackerLimit
			defer func() {
				namespaceTrackerLimit = originalLimit
			}()
			namespaceTrackerLimit = tt.trackLimit
			tt.tracker.Remove(tt.namespaceToRemove)

			if diff := cmp.Diff(tt.wantTracker, tt.tracker, namespaceTrackerCmpOptions...); diff != "" {
				t.Errorf("Remove() tracker mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNamespaceTracker_ListNamespaces(t *testing.T) {
	workName := "test-work"
	s := scheme.Scheme
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

	tests := []struct {
		name           string
		tracker        *NamespaceTracker
		wantNamespaces map[string]string
		wantReachLimit bool
	}{
		{
			name: "empty tracker",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{},
				namespaceCreationTimes: map[string]time.Time{},
				client:                 fakeClient,
			},
			wantNamespaces: map[string]string{},
			wantReachLimit: false,
		},
		{
			name: "single namespace without owner reference",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					"ns-without-owner": "",
				},
				namespaceCreationTimes: map[string]time.Time{
					"ns-without-owner": time.Now(),
				},
				client: fakeClient,
			},
			wantNamespaces: map[string]string{
				"ns-without-owner": "",
			},
			wantReachLimit: false,
		},
		{
			name: "single namespace with AppliedWork owner reference",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					"ns-with-work": workName,
				},
				namespaceCreationTimes: map[string]time.Time{
					"ns-with-work": time.Now(),
				},
				client: fakeClient,
			},
			wantNamespaces: map[string]string{
				"ns-with-work": workName,
			},
			wantReachLimit: false,
		},
		{
			name: "single namespace with non-AppliedWork owner reference",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					"ns-with-other-owner": "",
				},
				namespaceCreationTimes: map[string]time.Time{
					"ns-with-other-owner": time.Now(),
				},
				client: fakeClient,
			},
			wantNamespaces: map[string]string{
				"ns-with-other-owner": "",
			},
			wantReachLimit: false,
		},
		{
			name: "namespace with multiple owner references including AppliedWork",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					"ns-multiple-owners": workName,
				},
				namespaceCreationTimes: map[string]time.Time{
					"ns-multiple-owners": time.Now(),
				},
				client: fakeClient,
			},
			wantNamespaces: map[string]string{
				"ns-multiple-owners": workName,
			},
			wantReachLimit: false,
		},
		{
			name: "tracker with reach limit set",
			tracker: &NamespaceTracker{
				appliedWorkByNamespace: map[string]string{
					"ns-test": "",
				},
				namespaceCreationTimes: map[string]time.Time{
					"ns-test": time.Now(),
				},
				reachLimit: true,
				client:     fakeClient,
			},
			wantNamespaces: map[string]string{
				"ns-test": "",
			},
			wantReachLimit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNamespaces, gotReachLimit := tt.tracker.ListNamespaces()

			if diff := cmp.Diff(tt.wantNamespaces, gotNamespaces); diff != "" {
				t.Errorf("ListNamespaces() namespaces mismatch (-want +got):\n%s", diff)
			}

			if gotReachLimit != tt.wantReachLimit {
				t.Errorf("ListNamespaces() reachLimit = %v, want %v", gotReachLimit, tt.wantReachLimit)
			}
		})
	}
}
