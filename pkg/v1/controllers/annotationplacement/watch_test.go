/*
Copyright 2026 The KubeFleet Authors.

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

package annotationplacement

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

var configMapSourceGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

// generatedPolicyOwnedBy builds a policy as the cache would deliver it: correctly named for the
// resource that generated it, owned by that resource, plus any extra owner references a caller
// wants to plant. A policy whose name does not derive from an owner is that owner's, which is what
// lets the map function tell the true source from a foreign owner or a hand-authored policy.
func generatedPolicyOwnedBy(source *unstructured.Unstructured, extraOwners ...metav1.OwnerReference) client.Object {
	namespace := source.GetNamespace()
	policy := emptyPolicyForScope(namespace)
	policy.SetNamespace(namespace)
	policy.SetName(generatedPolicyName(source.GroupVersionKind(), namespace, source.GetName()))
	policy.SetOwnerReferences(append([]metav1.OwnerReference{parentOwnerReference(source)}, extraOwners...))
	return policy
}

// namedPolicy builds a policy with an explicit name (not derived from any owner), for the cases a
// generating source is not supposed to be found -- a hand-authored policy, or one whose only owners
// are foreign.
func namedPolicy(namespace, name string, owners ...metav1.OwnerReference) client.Object {
	policy := &kfplacementv1alpha1.PlacementPolicy{}
	policy.SetNamespace(namespace)
	policy.SetName(name)
	policy.SetOwnerReferences(owners)
	return policy
}

func ownerRef(apiVersion, kind, name string) metav1.OwnerReference {
	return metav1.OwnerReference{APIVersion: apiVersion, Kind: kind, Name: name, UID: "00000000-0000-0000-0000-000000000001"}
}

func TestMapGeneratedPolicyToSource(t *testing.T) {
	deploymentSource := sourceObject(deploymentGVK, "prod", "web")
	configMapSource := sourceObject(configMapSourceGVK, "prod", "app")
	namespaceSource := sourceObject(namespaceGVK, "", "team")

	deploymentRequest := Request{GroupVersionKind: deploymentGVK, NamespacedName: client.ObjectKey{Namespace: "prod", Name: "web"}}

	testCases := []struct {
		name   string
		policy client.Object
		want   []Request
	}{
		{
			name:   "a generated policy enqueues the resource that generated it",
			policy: generatedPolicyOwnedBy(deploymentSource),
			want:   []Request{deploymentRequest},
		},
		{
			name:   "a core group generating resource keeps its empty group",
			policy: generatedPolicyOwnedBy(configMapSource),
			want:   []Request{{GroupVersionKind: configMapSourceGVK, NamespacedName: client.ObjectKey{Namespace: "prod", Name: "app"}}},
		},
		{
			// A foreign owner reference -- one applyDesiredPolicy preserves, or any owner on a policy
			// that merely shares these watches -- must not be followed, or the queue would hold a
			// request for a kind the resource watcher never selected.
			name:   "a foreign owner reference is not followed",
			policy: generatedPolicyOwnedBy(deploymentSource, ownerRef("v1", "Pod", "some-pod"), ownerRef("apps/v1", "Deployment", "unrelated")),
			want:   []Request{deploymentRequest},
		},
		{
			// A hand-authored policy is owned by nothing this controller generated: its name does
			// not derive from its owner, so no owner is followed.
			name:   "a policy whose name does not derive from its owner enqueues nothing",
			policy: namedPolicy("prod", "hand-authored", ownerRef("apps/v1", "Deployment", "web")),
			want:   nil,
		},
		{
			name:   "a policy with no owners enqueues nothing",
			policy: namedPolicy("prod", "hand-authored"),
			want:   nil,
		},
		{
			// ParseGroupVersion rejects a second slash; the owner is skipped rather than turned
			// into a bogus kind on the queue.
			name:   "an owner with an unparsable API version is skipped",
			policy: generatedPolicyOwnedBy(deploymentSource, ownerRef("apps/v1/extra", "Deployment", "web")),
			want:   []Request{deploymentRequest},
		},
		{
			name:   "a cluster scoped policy enqueues a cluster scoped source",
			policy: generatedPolicyOwnedBy(namespaceSource),
			want:   []Request{{GroupVersionKind: namespaceGVK, NamespacedName: client.ObjectKey{Name: "team"}}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapGeneratedPolicyToSource(context.Background(), tc.policy)
			if diff := cmp.Diff(got, tc.want, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("mapGeneratedPolicyToSource() mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

// newTestQueue returns the controller workqueue a source is started with, so that a test can see
// exactly what a SourceQueue enqueued.
func newTestQueue() *testQueue {
	return &testQueue{TypedRateLimitingInterface: workqueue.NewTypedRateLimitingQueue[Request](workqueue.DefaultTypedControllerRateLimiter[Request]())}
}

type testQueue struct {
	workqueue.TypedRateLimitingInterface[Request]
}

// drain returns every queued request in order, leaving the queue empty.
func (q *testQueue) drain() []Request {
	var got []Request
	for q.Len() > 0 {
		req, shutdown := q.Get()
		if shutdown {
			break
		}
		got = append(got, req)
		q.Done(req)
	}
	return got
}

func TestSourceQueueAdd(t *testing.T) {
	testCases := []struct {
		name   string
		source client.Object
		want   []Request
	}{
		{
			name:   "an annotated resource is enqueued under its kind and namespaced name",
			source: sourceObject(deploymentGVK, "prod", "web"),
			want:   []Request{{GroupVersionKind: deploymentGVK, NamespacedName: client.ObjectKey{Namespace: "prod", Name: "web"}}},
		},
		{
			name:   "a cluster scoped resource is enqueued with no namespace",
			source: sourceObject(namespaceGVK, "", "team"),
			want:   []Request{{GroupVersionKind: namespaceGVK, NamespacedName: client.ObjectKey{Name: "team"}}},
		},
		{
			// A typed object routinely arrives with an empty kind; a request built from it would
			// name no API to read the resource from, so it is dropped rather than enqueued.
			name:   "an object that names no kind is dropped",
			source: &kfplacementv1alpha1.PlacementPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "web"}},
			want:   nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			q := &SourceQueue{}
			queue := newTestQueue()
			if err := q.Start(context.Background(), queue); err != nil {
				t.Fatalf("Start() = %v, want no error", err)
			}
			q.Add(tc.source)

			if diff := cmp.Diff(queue.drain(), tc.want, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("SourceQueue.Add() enqueued (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestSourceNamespace covers the skip-check namespace: a namespaced resource uses its own
// namespace, but a Namespace object -- which carries none -- is itself the namespace.
func TestSourceNamespace(t *testing.T) {
	testCases := []struct {
		name   string
		source *unstructured.Unstructured
		want   string
	}{
		{name: "a namespaced resource uses its namespace", source: sourceObject(deploymentGVK, "prod", "web"), want: "prod"},
		{name: "a Namespace object is itself the namespace", source: sourceObject(namespaceGVK, "", "team"), want: "team"},
		{name: "another cluster scoped resource has no namespace", source: sourceObject(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}, "", "admin"), want: ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SourceNamespace(tc.source); got != tc.want {
				t.Errorf("SourceNamespace(%s %s) = %q, want %q", tc.source.GetKind(), tc.source.GetName(), got, tc.want)
			}
		})
	}
}

// TestGeneratedPolicyDrift pins which policy updates reach the reconciler: what applyDesiredPolicy
// writes must, a status write must not.
func TestGeneratedPolicyDrift(t *testing.T) {
	base := func() *kfplacementv1alpha1.PlacementPolicy {
		policy := generatedPolicyOwnedBy(sourceObject(deploymentGVK, "prod", "web")).(*kfplacementv1alpha1.PlacementPolicy)
		policy.Generation = 1
		return policy
	}

	testCases := []struct {
		name   string
		mutate func(*kfplacementv1alpha1.PlacementPolicy)
		want   bool
	}{
		{name: "status only", mutate: func(p *kfplacementv1alpha1.PlacementPolicy) {
			p.ResourceVersion = "2"
			p.Status.Conditions = []metav1.Condition{{Type: "Scheduled", Status: metav1.ConditionTrue}}
		}},
		{name: "spec edit bumps the generation", mutate: func(p *kfplacementv1alpha1.PlacementPolicy) { p.Generation = 2 }, want: true},
		{name: "label change", mutate: func(p *kfplacementv1alpha1.PlacementPolicy) { p.Labels = map[string]string{"drift": "yes"} }, want: true},
		{name: "owner reference stripped", mutate: func(p *kfplacementv1alpha1.PlacementPolicy) { p.OwnerReferences = nil }, want: true},
	}

	pred := generatedPolicyDrift()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updated := base()
			tc.mutate(updated)
			if got := pred.Update(event.UpdateEvent{ObjectOld: base(), ObjectNew: updated}); got != tc.want {
				t.Errorf("generatedPolicyDrift().Update() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSourceQueueHoldsUntilStarted pins that a resource reported before the manager started is not
// lost: the watcher does not have to know when this controller became ready.
func TestSourceQueueHoldsUntilStarted(t *testing.T) {
	q := &SourceQueue{}
	q.Add(sourceObject(deploymentGVK, "prod", "web"))

	queue := newTestQueue()
	if err := q.Start(context.Background(), queue); err != nil {
		t.Fatalf("Start() = %v, want no error", err)
	}

	want := []Request{{GroupVersionKind: deploymentGVK, NamespacedName: client.ObjectKey{Namespace: "prod", Name: "web"}}}
	if diff := cmp.Diff(queue.drain(), want); diff != "" {
		t.Errorf("SourceQueue.Add() before Start enqueued (-got, +want):\n%s", diff)
	}
}

// TestSourceQueueDeduplicates pins the reason this is a queue rather than a channel: a burst of
// reports for one resource costs one reconcile, not one per report.
func TestSourceQueueDeduplicates(t *testing.T) {
	q := &SourceQueue{}
	queue := newTestQueue()
	if err := q.Start(context.Background(), queue); err != nil {
		t.Fatalf("Start() = %v, want no error", err)
	}

	source := sourceObject(deploymentGVK, "prod", "web")
	for i := 0; i < 5; i++ {
		q.Add(source)
	}

	if got := queue.Len(); got != 1 {
		t.Errorf("SourceQueue.Add() x5 queued %d items, want 1", got)
	}
}
