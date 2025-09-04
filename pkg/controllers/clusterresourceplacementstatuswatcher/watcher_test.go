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
package clusterresourceplacementstatuswatcher

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func TestBuildDeleteEventPredicate(t *testing.T) {
	predicate := buildDeleteEventPredicate()

	tests := []struct {
		name     string
		testFunc func() bool
		want     bool
	}{
		{
			name: "GenericEvent should return false",
			testFunc: func() bool {
				genericEvent := event.GenericEvent{
					Object: &placementv1beta1.ClusterResourcePlacementStatus{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-crps",
							Namespace: "test-namespace",
						},
					},
				}
				return predicate.Generic(genericEvent)
			},
			want: false,
		},
		{
			name: "DeleteEvent with nil object should return false",
			testFunc: func() bool {
				deleteEvent := event.DeleteEvent{
					Object:             nil,
					DeleteStateUnknown: false,
				}
				return predicate.Delete(deleteEvent)
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.testFunc()
			if got != tt.want {
				t.Errorf("want %v, but got %v", tt.want, got)
			}
		})
	}
}
