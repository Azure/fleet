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

package resourcewatcher

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/controllers/annotationplacement"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
)

func TestHandleTombStoneObj(t *testing.T) {
	var (
		secretObj = &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
			},
		}
		clusterRoleObj = &rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Role",
				APIVersion: "rbac.authorization.k8s.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
			},
		}

		deletedRole = &rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Role",
				APIVersion: "rbac.authorization.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
			},
		}
	)
	tests := []struct {
		name    string
		object  interface{}
		wantErr bool
		want    client.Object
	}{
		{
			name:    "namespace scoped resource in core group",
			object:  secretObj,
			wantErr: false,
			want:    secretObj,
		},
		{
			name:    "cluster scoped resource",
			object:  clusterRoleObj,
			wantErr: false,
			want:    clusterRoleObj,
		},
		{
			name: "tomestone object",
			object: cache.DeletedFinalStateUnknown{
				Key: "foo",
				Obj: deletedRole,
			},
			wantErr: false,
			want:    deletedRole,
		},
		{
			name: "none runtime object should be error",
			object: fleetv1beta1.ResourceIdentifier{
				Namespace: "foo",
				Name:      "bar",
			},
			wantErr: true,
		},
		{
			name:    "nil object should be error",
			object:  nil,
			wantErr: true,
		},
	}

	for _, test := range tests {
		tt := test
		t.Run(tt.name, func(t *testing.T) {
			got, err := handleTombStoneObj(tt.object)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleTombStoneObj() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("handleTombStoneObj() got = %v, want %v", got, tt.want)
			}
		})
	}
}

var _ controller.Controller = &fakeController{}

// fakeController just record if there is an enqueue request or not
type fakeController struct {
	Enqueued bool
}

func (t *fakeController) Enqueue(_ interface{}) {
	t.Enqueued = true
}

// Run is a no-op; the fake is only used to verify that Enqueue is called.
func (t *fakeController) Run(_ context.Context, _ int) error {
	return nil
}

type noopController struct{}

func (noopController) Enqueue(interface{})            {}
func (noopController) Run(context.Context, int) error { return nil }

// TestReportAnnotated pins which events reach annotation-based placement: an annotated add, an
// update that keeps or removes the annotation, and an annotated delete; never an unannotated one.
func TestReportAnnotated(t *testing.T) {
	configMap := func(name string, annotated bool, rv string) *corev1.ConfigMap {
		cm := &corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: name, ResourceVersion: rv},
		}
		if annotated {
			cm.Annotations = map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: "region=east"}
		}
		return cm
	}
	sources := &annotationplacement.SourceQueue{}
	d := &ChangeDetector{ResourceChangeController: noopController{}, AnnotatedSources: sources}

	d.onResourceAdded(configMap("added", true, "1"))
	d.onResourceAdded(configMap("plain", false, "1"))
	d.onResourceUpdated(configMap("removed", true, "1"), configMap("removed", false, "2"))
	d.onResourceUpdated(configMap("plain-updated", false, "1"), configMap("plain-updated", false, "2"))
	d.onResourceDeleted(cache.DeletedFinalStateUnknown{Obj: configMap("deleted", true, "1")})
	d.onResourceDeleted(configMap("plain-deleted", false, "1"))

	wq := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[annotationplacement.Request]())
	defer wq.ShutDown()
	if err := sources.Start(context.Background(), wq); err != nil {
		t.Fatalf("Start() = %v, want no error", err)
	}
	var got []string
	for wq.Len() > 0 {
		req, _ := wq.Get()
		got = append(got, req.Name)
		wq.Done(req)
	}
	slices.Sort(got)
	if diff := cmp.Diff([]string{"added", "deleted", "removed"}, got); diff != "" {
		t.Errorf("reported resources mismatch (-want +got):\n%s", diff)
	}

	// Off when the feature is: no queue, no panic.
	(&ChangeDetector{ResourceChangeController: noopController{}}).onResourceAdded(configMap("added", true, "1"))
}
