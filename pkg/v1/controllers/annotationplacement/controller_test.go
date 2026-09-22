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
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

var (
	deploymentGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	namespaceGVR  = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
)

const (
	testNamespace = "prod"
	testName      = "web"

	oneSelector  = "env=staging"
	twoSelectors = "env=staging,count=All;env=canary,region=eastus"
)

// newSource builds the annotated resource as the API server would return it.
func newSource(gvk schema.GroupVersionKind, namespace, name string, annotations map[string]string) *unstructured.Unstructured {
	source := sourceObject(gvk, namespace, name)
	if annotations != nil {
		source.SetAnnotations(annotations)
	}
	return source
}

// requestFor builds the request the resource watcher would enqueue for a resource.
func requestFor(gvk schema.GroupVersionKind, namespace, name string) Request {
	return Request{
		GroupVersionKind: gvk,
		NamespacedName:   client.ObjectKey{Namespace: namespace, Name: name},
	}
}

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	return scheme
}

// newRESTMapper knows the served kinds these tests read: a namespaced Deployment and a cluster
// scoped Namespace. A kind or a version outside it resolves to a no-match error, as a kind whose
// CRD was removed would at the API server.
func newRESTMapper() meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{deploymentGVK.GroupVersion(), namespaceGVK.GroupVersion()})
	mapper.AddSpecific(deploymentGVK, deploymentGVR, deploymentGVR, meta.RESTScopeNamespace)
	mapper.AddSpecific(namespaceGVK, namespaceGVR, namespaceGVR, meta.RESTScopeRoot)
	return mapper
}

// newReconciler wires a reconciler whose API server holds the given annotated resources and
// policies. One fake client backs both the policy client and the API reader, so the two are always
// consistent, which is what every test here wants.
func newReconciler(t *testing.T, funcs interceptor.Funcs, objects ...client.Object) (*Reconciler, *events.FakeRecorder) {
	t.Helper()
	recorder := events.NewFakeRecorder(10)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(objects...).WithInterceptorFuncs(funcs).Build()
	return &Reconciler{
		Client:     c,
		APIReader:  c,
		RESTMapper: newRESTMapper(),
		Recorder:   recorder,
	}, recorder
}

// recordedReasons drains the recorder and returns the reason of every event it holds.
func recordedReasons(recorder *events.FakeRecorder) []string {
	reasons := make([]string, 0, len(recorder.Events))
	for {
		select {
		case event := <-recorder.Events:
			// A recorded event reads "Normal <Reason> <message>".
			fields := strings.SplitN(event, " ", 3)
			if len(fields) < 2 {
				reasons = append(reasons, event)
				continue
			}
			reasons = append(reasons, fields[1])
		default:
			return reasons
		}
	}
}

// policyFrom reads the generated policy for a resource back out of the client, or reports that none
// exists.
func policyFrom(ctx context.Context, t *testing.T, r *Reconciler, source *unstructured.Unstructured) (kfplacementv1alpha1.PlacementPolicyAccessor, bool) {
	t.Helper()
	namespace := source.GetNamespace()
	policy := emptyPolicyForScope(namespace)
	name := generatedPolicyName(source.GroupVersionKind(), namespace, source.GetName())
	err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, policy)
	switch {
	case apierrors.IsNotFound(err):
		return nil, false
	case err != nil:
		t.Fatalf("Get(%s) = %v, want no error", name, err)
	}
	return policy, true
}

// policyIgnoreOpts drop the fields the API server owns, which no expectation here can predict.
var policyIgnoreOpts = cmp.Options{
	cmpopts.IgnoreFields(metav1.TypeMeta{}, "Kind", "APIVersion"),
	cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "Generation", "CreationTimestamp", "ManagedFields"),
}

func mustParse(t *testing.T, value string) []kfplacementv1alpha1.ClusterSelector {
	t.Helper()
	selectors, err := parseClusterSelectors(value)
	if err != nil {
		t.Fatalf("parseClusterSelectors(%q) = %v, want no error", value, err)
	}
	return selectors
}

func TestReconcile(t *testing.T) {
	// The desired policy is built with desiredPolicy, which policy_test.go covers on its own; what
	// is under test here is whether the reconciler puts that object in the cluster, takes it away
	// again, and says so.
	annotatedSource := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	bareSource := newSource(deploymentGVK, testNamespace, testName, nil)
	clusterScopedSource := newSource(namespaceGVK, "", testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})

	testCases := []struct {
		name string
		// source is the resource the API server holds, and the resource the request points at.
		source *unstructured.Unstructured
		// existing is the annotation value a policy was already generated from, if any.
		existing    string
		wantPolicy  bool
		wantValue   string
		wantReasons []string
	}{
		{
			name:        "an annotated resource with no policy yet gets one",
			source:      annotatedSource,
			wantPolicy:  true,
			wantValue:   oneSelector,
			wantReasons: []string{EventReasonPolicyCreated},
		},
		{
			name:        "a policy that already matches the annotation is left alone",
			source:      annotatedSource,
			existing:    oneSelector,
			wantPolicy:  true,
			wantValue:   oneSelector,
			wantReasons: nil,
		},
		{
			name:        "a changed annotation reaches the policy",
			source:      newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: twoSelectors}),
			existing:    oneSelector,
			wantPolicy:  true,
			wantValue:   twoSelectors,
			wantReasons: []string{EventReasonPolicyUpdated},
		},
		{
			name:        "removing the annotation deletes the policy",
			source:      bareSource,
			existing:    oneSelector,
			wantPolicy:  false,
			wantReasons: []string{EventReasonPolicyDeleted},
		},
		{
			name:        "a resource that was never annotated is left alone",
			source:      bareSource,
			wantPolicy:  false,
			wantReasons: nil,
		},
		{
			name:        "a cluster scoped resource generates a cluster scoped policy",
			source:      clusterScopedSource,
			wantPolicy:  true,
			wantValue:   oneSelector,
			wantReasons: []string{EventReasonPolicyCreated},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			objects := []client.Object{tc.source}
			if tc.existing != "" {
				objects = append(objects, desiredPolicy(tc.source, mustParse(t, tc.existing)))
			}
			r, recorder := newReconciler(t, interceptor.Funcs{}, objects...)

			req := RequestFor(tc.source)
			if _, err := r.Reconcile(ctx, req); err != nil {
				t.Fatalf("Reconcile(%v) = %v, want no error", req, err)
			}

			got, found := policyFrom(ctx, t, r, tc.source)
			if found != tc.wantPolicy {
				t.Fatalf("Reconcile(%v) left a generated policy = %v, want %v", req, found, tc.wantPolicy)
			}
			if tc.wantPolicy {
				want := desiredPolicy(tc.source, mustParse(t, tc.wantValue))
				if diff := cmp.Diff(got, want, policyIgnoreOpts); diff != "" {
					t.Errorf("Reconcile(%v) generated policy mismatch (-got, +want):\n%s", req, diff)
				}
			}
			if diff := cmp.Diff(recordedReasons(recorder), tc.wantReasons, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Reconcile(%v) recorded events mismatch (-got, +want):\n%s", req, diff)
			}
		})
	}
}

// TestReconcileInvalidAnnotation covers the case the parser rejects: the user hears about it, the
// request is not retried, and a policy generated from an earlier valid annotation stays up.
func TestReconcileInvalidAnnotation(t *testing.T) {
	ctx := context.Background()
	valid := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	existing := desiredPolicy(valid, mustParse(t, oneSelector))

	broken := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: "env"})
	r, recorder := newReconciler(t, interceptor.Funcs{}, broken, existing)

	req := RequestFor(broken)
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile(%v) = %v, want no error, so that a malformed annotation is not retried", req, err)
	}

	got, found := policyFrom(ctx, t, r, broken)
	if !found {
		t.Fatalf("Reconcile(%v) removed the policy generated from the previous annotation, want it left in place", req)
	}
	if diff := cmp.Diff(got, existing, policyIgnoreOpts); diff != "" {
		t.Errorf("Reconcile(%v) changed the policy generated from the previous annotation (-got, +want):\n%s", req, diff)
	}
	if diff := cmp.Diff(recordedReasons(recorder), []string{EventReasonInvalidAnnotation}); diff != "" {
		t.Errorf("Reconcile(%v) recorded events mismatch (-got, +want):\n%s", req, diff)
	}
}

// TestReconcileDeletedResource covers a request for a resource that is already gone. The reconciler
// deletes the generated policy itself rather than deferring to garbage collection, which removes a
// dependent only once every owner reference on it is gone -- and the merge deliberately preserves
// owner references that other parties added, any live one of which would keep the policy standing.
// No event is recorded, since the resource an event would be recorded on no longer exists.
func TestReconcileDeletedResource(t *testing.T) {
	ctx := context.Background()
	source := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	existing := desiredPolicy(source, mustParse(t, oneSelector))
	// The other party whose owner reference would hold the policy back from garbage collection.
	otherOwner := metav1.OwnerReference{APIVersion: "example.com/v1", Kind: "Widget", Name: "unrelated", UID: "00000000-0000-0000-0000-0000000000ff"}
	existing.SetOwnerReferences(append(existing.GetOwnerReferences(), otherOwner))

	r, recorder := newReconciler(t, interceptor.Funcs{}, existing)

	req := RequestFor(source)
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile(%v) = %v, want no error", req, err)
	}
	if _, found := policyFrom(ctx, t, r, source); found {
		t.Errorf("Reconcile(%v) left the generated policy standing, want it deleted", req)
	}
	if got := recordedReasons(recorder); len(got) != 0 {
		t.Errorf("Reconcile(%v) recorded events = %v, want none", req, got)
	}
}

// TestReconcileIneligibleResource covers a resource that exists but fails the placement eligibility
// test -- for instance one in a skipped namespace, or a ReplicaSet that a Deployment has adopted.
// Its generated policy is deleted: the resource watcher reports such a resource as deleted and then
// never again, so this reconciliation is the last chance to clean up.
func TestReconcileIneligibleResource(t *testing.T) {
	ctx := context.Background()
	source := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	existing := desiredPolicy(source, mustParse(t, oneSelector))

	r, recorder := newReconciler(t, interceptor.Funcs{}, source, existing)
	r.ShouldPlace = func(*unstructured.Unstructured) (bool, error) { return false, nil }

	req := RequestFor(source)
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile(%v) = %v, want no error", req, err)
	}
	if _, found := policyFrom(ctx, t, r, source); found {
		t.Errorf("Reconcile(%v) left the generated policy standing, want it deleted", req)
	}
	if diff := cmp.Diff(recordedReasons(recorder), []string{EventReasonPolicyDeleted}); diff != "" {
		t.Errorf("Reconcile(%v) recorded events mismatch (-got, +want):\n%s", req, diff)
	}

	// A second pass over a resource that never generated anything must stay silent.
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile(%v) = %v, want no error", req, err)
	}
	if got := recordedReasons(recorder); len(got) != 0 {
		t.Errorf("Reconcile(%v) recorded events = %v, want none on the second pass", req, got)
	}
}

// TestReconcileEligibilityError covers the eligibility test itself failing, which must surface as a
// retryable error rather than as either verdict.
func TestReconcileEligibilityError(t *testing.T) {
	source := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	r, _ := newReconciler(t, interceptor.Funcs{}, source)
	wantErr := errors.New("the eligibility test is unwell")
	r.ShouldPlace = func(*unstructured.Unstructured) (bool, error) { return false, wantErr }

	req := RequestFor(source)
	if _, err := r.Reconcile(context.Background(), req); !errors.Is(err, wantErr) {
		t.Errorf("Reconcile(%v) = %v, want an error wrapping %v", req, err, wantErr)
	}
	if _, found := policyFrom(context.Background(), t, r, source); found {
		t.Errorf("Reconcile(%v) acted on the policy despite the eligibility error", req)
	}
}

// TestReconcileAPIServerErrors covers the paths where a read or a write fails. Each has to come
// back as a retryable error rather than as a silent success, because the annotation and the cluster
// have disagreed at that point and only another pass can settle it.
func TestReconcileAPIServerErrors(t *testing.T) {
	annotated := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	bare := newSource(deploymentGVK, testNamespace, testName, nil)
	existing := desiredPolicy(annotated, mustParse(t, oneSelector))
	failure := apierrors.NewInternalError(errors.New("the api server is unwell"))

	testCases := []struct {
		name        string
		source      *unstructured.Unstructured
		existing    []client.Object
		interceptor interceptor.Funcs
	}{
		{
			name:   "the annotated resource cannot be read",
			source: annotated,
			interceptor: interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, isSource := obj.(*unstructured.Unstructured); isSource {
						return failure
					}
					return c.Get(ctx, key, obj, opts...)
				},
			},
		},
		{
			name:   "the policy cannot be read",
			source: annotated,
			interceptor: interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, isSource := obj.(*unstructured.Unstructured); isSource {
						return c.Get(ctx, key, obj, opts...)
					}
					return failure
				},
			},
		},
		{
			name:   "the policy cannot be created",
			source: annotated,
			interceptor: interceptor.Funcs{
				Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
					return failure
				},
			},
		},
		{
			name:     "the policy cannot be updated",
			source:   newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: twoSelectors}),
			existing: []client.Object{existing},
			interceptor: interceptor.Funcs{
				Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
					return failure
				},
			},
		},
		{
			name:     "the policy cannot be deleted",
			source:   bare,
			existing: []client.Object{existing},
			interceptor: interceptor.Funcs{
				Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
					return failure
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r, recorder := newReconciler(t, tc.interceptor, append([]client.Object{tc.source}, tc.existing...)...)

			req := RequestFor(tc.source)
			_, err := r.Reconcile(context.Background(), req)
			if !errors.Is(err, failure) {
				t.Errorf("Reconcile(%v) = %v, want an error wrapping %v", req, err, failure)
			}
			// A write that failed must not be announced as though it had happened.
			if got := recordedReasons(recorder); len(got) != 0 {
				t.Errorf("Reconcile(%v) recorded events = %v, want none", req, got)
			}
		})
	}
}

// TestReconcileRetriesWhenCreateRacesTheCache covers a create that finds the policy already there:
// the cache has yet to see the policy this controller created a moment ago, and a second, unrelated
// event for the source arrived in that window. The pass must come back as a retryable error, not
// as a failure, and must not announce a creation it did not perform.
func TestReconcileRetriesWhenCreateRacesTheCache(t *testing.T) {
	source := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	alreadyExists := apierrors.NewAlreadyExists(schema.GroupResource{Group: kfplacementv1alpha1.GroupVersion.Group, Resource: "placementpolicies"}, generatedPolicyName(deploymentGVK, testNamespace, testName))
	raced := interceptor.Funcs{
		Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
			return alreadyExists
		},
	}
	r, recorder := newReconciler(t, raced, source)

	req := RequestFor(source)
	if _, err := r.Reconcile(context.Background(), req); !errors.Is(err, alreadyExists) {
		t.Errorf("Reconcile(%v) = %v, want an error wrapping %v so the source is retried", req, err, alreadyExists)
	}
	if got := recordedReasons(recorder); len(got) != 0 {
		t.Errorf("Reconcile(%v) recorded events = %v, want none for a creation this pass did not perform", req, got)
	}
}

// TestDeleteRacesAnotherDeletion covers a cached read handing the reconciler a policy that is
// already gone by the time it deletes -- typically its own deletion re-entered through the
// generated policy watch. The pass must not claim, in logs or events, a deletion it did not do.
func TestDeleteRacesAnotherDeletion(t *testing.T) {
	ctx := context.Background()
	source := newSource(deploymentGVK, testNamespace, testName, nil)
	annotated := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	existing := desiredPolicy(annotated, mustParse(t, oneSelector))

	// The interceptor stands in for the stale cache: the Get finds the policy, the Delete
	// discovers someone else got there first.
	raced := interceptor.Funcs{
		Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
			return apierrors.NewNotFound(schema.GroupResource{Group: kfplacementv1alpha1.GroupVersion.Group, Resource: "placementpolicies"}, existing.GetName())
		},
	}
	r, recorder := newReconciler(t, raced, source, existing)

	req := RequestFor(source)
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile(%v) = %v, want no error", req, err)
	}
	if got := recordedReasons(recorder); len(got) != 0 {
		t.Errorf("Reconcile(%v) recorded events = %v, want none for a deletion this pass did not perform", req, got)
	}
}

// TestReconcileDoesNotDeleteAReplacementPolicy covers the window between the read that confirms a
// policy is this controller's and the delete that removes it. If the policy is replaced in that
// window -- overwritten with its provenance stripped, or deleted and a hand-authored one created at
// the same name -- an unconditioned delete would remove the replacement, since it targets the name
// alone. The resource-version precondition must turn that into a conflict, and the retry must then
// read the current object and decline it.
func TestReconcileDoesNotDeleteAReplacementPolicy(t *testing.T) {
	ctx := context.Background()
	bare := newSource(deploymentGVK, testNamespace, testName, nil)
	annotated := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	ours := desiredPolicy(annotated, mustParse(t, oneSelector))

	// A spec no generated policy would carry, marking the object that takes the name.
	foreignSpec := kfplacementv1alpha1.PlacementPolicySpec{
		ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{{APIGroup: "example.com", APIVersion: "v1", Kind: "Widget", Name: "hand-authored"}},
	}

	replaced := false
	raced := interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if !replaced {
				replaced = true
				// Stand in for the replacement: the live object loses its provenance and its resource
				// version moves on, so it is no longer the one the precondition names.
				live := &kfplacementv1alpha1.PlacementPolicy{}
				if err := c.Get(ctx, client.ObjectKeyFromObject(obj), live); err != nil {
					return err
				}
				live.SetLabels(nil)
				live.SetOwnerReferences(nil)
				live.Spec = foreignSpec
				if err := c.Update(ctx, live); err != nil {
					return err
				}
			}
			return c.Delete(ctx, obj, opts...)
		},
	}
	r, recorder := newReconciler(t, raced, bare, ours)

	req := RequestFor(bare)
	// First pass: the policy is replaced after it is read, so the guarded delete conflicts and the
	// pass returns a retryable error rather than removing the replacement.
	if _, err := r.Reconcile(ctx, req); err == nil {
		t.Fatalf("Reconcile(%v) = nil, want a conflict error so the delete of a replaced policy is retried", req)
	}
	got, found := policyFrom(ctx, t, r, bare)
	if !found {
		t.Fatalf("Reconcile(%v) deleted the replacement policy, want it left in place", req)
	}
	if diff := cmp.Diff(got.(*kfplacementv1alpha1.PlacementPolicy).Spec, foreignSpec); diff != "" {
		t.Errorf("Reconcile(%v) changed the replacement policy (-got, +want):\n%s", req, diff)
	}

	// Second pass: the reconciler reads the current, now foreign object, declines it, and records no
	// deletion it did not perform.
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile(%v) = %v, want no error once the replacement is recognized as foreign", req, err)
	}
	if _, found := policyFrom(ctx, t, r, bare); !found {
		t.Errorf("Reconcile(%v) deleted the foreign replacement on retry, want it left in place", req)
	}
	if got := recordedReasons(recorder); slices.Contains(got, EventReasonPolicyDeleted) {
		t.Errorf("Reconcile(%v) recorded events = %v, want no deletion of a policy it did not generate", req, got)
	}
}

func TestApplyDesiredPolicy(t *testing.T) {
	source := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	desired := desiredPolicy(source, mustParse(t, oneSelector))
	scheme := newScheme(t)

	otherOwner := metav1.OwnerReference{
		APIVersion: "example.com/v1",
		Kind:       "Widget",
		Name:       "unrelated",
		UID:        "00000000-0000-0000-0000-0000000000ff",
	}

	testCases := []struct {
		name string
		// mutate turns the desired policy into the live one the reconciler would read back.
		mutate      func(kfplacementv1alpha1.PlacementPolicyAccessor)
		wantChanged bool
		// check asserts on whatever the merge is supposed to preserve.
		check func(*testing.T, kfplacementv1alpha1.PlacementPolicyAccessor)
	}{
		{
			name:        "an untouched policy needs no update",
			mutate:      func(kfplacementv1alpha1.PlacementPolicyAccessor) {},
			wantChanged: false,
		},
		{
			name: "a drifted spec is restored",
			mutate: func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().ClusterSelectors = nil
			},
			wantChanged: true,
			check: func(t *testing.T, policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				if diff := cmp.Diff(policy.GetSpec(), desired.GetSpec()); diff != "" {
					t.Errorf("applyDesiredPolicy() spec mismatch (-got, +want):\n%s", diff)
				}
			},
		},
		{
			name: "a missing provenance label is restored",
			mutate: func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				labels := policy.GetLabels()
				delete(labels, kfplacementv1alpha1.ParentKindLabel)
				policy.SetLabels(labels)
			},
			wantChanged: true,
			check: func(t *testing.T, policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				if diff := cmp.Diff(policy.GetLabels(), desired.GetLabels()); diff != "" {
					t.Errorf("applyDesiredPolicy() labels mismatch (-got, +want):\n%s", diff)
				}
			},
		},
		{
			name: "a policy stripped of every label gets the provenance labels back",
			mutate: func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.SetLabels(nil)
			},
			wantChanged: true,
			check: func(t *testing.T, policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				if diff := cmp.Diff(policy.GetLabels(), desired.GetLabels()); diff != "" {
					t.Errorf("applyDesiredPolicy() labels mismatch (-got, +want):\n%s", diff)
				}
			},
		},
		{
			name: "labels added by something else are kept",
			mutate: func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				labels := policy.GetLabels()
				labels["example.com/managed-by"] = "gitops"
				delete(labels, kfplacementv1alpha1.ParentKindLabel)
				policy.SetLabels(labels)
			},
			wantChanged: true,
			check: func(t *testing.T, policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				if got := policy.GetLabels()["example.com/managed-by"]; got != "gitops" {
					t.Errorf("applyDesiredPolicy() label example.com/managed-by = %q, want %q", got, "gitops")
				}
			},
		},
		{
			name: "an owner reference that drifted is corrected in place",
			mutate: func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				drifted := parentOwnerReference(source)
				drifted.APIVersion = "apps/v1beta1"
				policy.SetOwnerReferences([]metav1.OwnerReference{drifted})
			},
			wantChanged: true,
			check: func(t *testing.T, policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				want := []metav1.OwnerReference{parentOwnerReference(source)}
				if diff := cmp.Diff(policy.GetOwnerReferences(), want); diff != "" {
					t.Errorf("applyDesiredPolicy() owner references mismatch (-got, +want):\n%s", diff)
				}
			},
		},
		{
			name: "a missing owner reference is restored alongside any other",
			mutate: func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.SetOwnerReferences([]metav1.OwnerReference{otherOwner})
			},
			wantChanged: true,
			check: func(t *testing.T, policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				want := []metav1.OwnerReference{otherOwner, parentOwnerReference(source)}
				if diff := cmp.Diff(policy.GetOwnerReferences(), want); diff != "" {
					t.Errorf("applyDesiredPolicy() owner references mismatch (-got, +want):\n%s", diff)
				}
			},
		},
		{
			// The source was deleted and recreated under the same name, so its reference carries the
			// old UID. It must be updated in place, not left dangling while a second one is appended --
			// otherwise the list grows by one on every such cycle.
			name: "an owner reference left by a recreated source is replaced, not appended",
			mutate: func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				stale := parentOwnerReference(source)
				stale.UID = "00000000-0000-0000-0000-00000000dead"
				policy.SetOwnerReferences([]metav1.OwnerReference{stale})
			},
			wantChanged: true,
			check: func(t *testing.T, policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				want := []metav1.OwnerReference{parentOwnerReference(source)}
				if diff := cmp.Diff(policy.GetOwnerReferences(), want); diff != "" {
					t.Errorf("applyDesiredPolicy() owner references mismatch (-got, +want):\n%s", diff)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := desired.DeepCopyObject().(kfplacementv1alpha1.PlacementPolicyAccessor)
			tc.mutate(actual)
			before := actual.DeepCopyObject()

			if err := applyDesiredPolicy(actual, desired, source, scheme); err != nil {
				t.Fatalf("applyDesiredPolicy() = %v, want no error", err)
			}
			if got := !equality.Semantic.DeepEqual(before, actual); got != tc.wantChanged {
				t.Errorf("applyDesiredPolicy() changed the policy = %v, want %v", got, tc.wantChanged)
			}
			if tc.check != nil {
				tc.check(t, actual)
			}
			// Whatever the merge did, a second pass over its own output must find nothing left to
			// do; otherwise the reconciler would issue an update on every single pass.
			settled := actual.DeepCopyObject()
			if err := applyDesiredPolicy(actual, desired, source, scheme); err != nil {
				t.Fatalf("applyDesiredPolicy() = %v on the second pass, want no error", err)
			}
			if !equality.Semantic.DeepEqual(settled, actual) {
				t.Errorf("applyDesiredPolicy() changed the policy on the second pass, want it settled")
			}
		})
	}
}

// TestEventMessagesNameTheGeneratedKind pins that an event's message names the kind actually
// generated: the two scopes generate different kinds, and a message claiming a PlacementPolicy for
// what is really a ClusterPlacementPolicy sends the user's kubectl to a resource that is not there.
func TestEventMessagesNameTheGeneratedKind(t *testing.T) {
	testCases := []struct {
		name     string
		source   *unstructured.Unstructured
		wantKind string
	}{
		{
			name:     "a namespaced source names PlacementPolicy",
			source:   newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector}),
			wantKind: "PlacementPolicy",
		},
		{
			name:     "a cluster scoped source names ClusterPlacementPolicy",
			source:   newSource(namespaceGVK, "", testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector}),
			wantKind: "ClusterPlacementPolicy",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r, recorder := newReconciler(t, interceptor.Funcs{}, tc.source)

			req := RequestFor(tc.source)
			if _, err := r.Reconcile(context.Background(), req); err != nil {
				t.Fatalf("Reconcile(%v) = %v, want no error", req, err)
			}

			select {
			case event := <-recorder.Events:
				// The kind is matched with its surrounding spaces: "ClusterPlacementPolicy"
				// contains "PlacementPolicy" as a bare substring, so an unanchored check would
				// pass the namespaced case even if the wrong kind were named.
				if !strings.Contains(event, " the "+tc.wantKind+" ") {
					t.Errorf("Reconcile(%v) recorded event %q, want it to name the kind %q", req, event, tc.wantKind)
				}
			default:
				t.Fatalf("Reconcile(%v) recorded no event, want one naming the kind %q", req, tc.wantKind)
			}
		})
	}
}

// TestReconcileUnknownKind covers a request whose kind the API server does not know, which the
// generated policy watch can produce by enqueuing whatever a policy names as its owner. The kind
// being gone means the source's own CRD was removed, so any policy generated from it is stale and is
// deleted; when none exists the request is simply dropped, since no retry can make the kind exist
// and an error would keep it backing off forever.
func TestReconcileUnknownKind(t *testing.T) {
	widgetGVK := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
	// The resource whose kind is gone. It is only used to derive the generated policy the reconciler
	// must find and delete; the REST mapper deliberately does not know its kind.
	widget := newSource(widgetGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	stale := desiredPolicy(widget, mustParse(t, oneSelector))

	testCases := []struct {
		name       string
		existing   []client.Object
		wantPolicy bool
	}{
		{name: "no policy to clean up, request is dropped", existing: nil, wantPolicy: false},
		{name: "a stale policy left by the gone kind is deleted", existing: []client.Object{stale}, wantPolicy: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r, recorder := newReconciler(t, interceptor.Funcs{}, tc.existing...)
			req := RequestFor(widget)
			if _, err := r.Reconcile(context.Background(), req); err != nil {
				t.Errorf("Reconcile(%v) = %v, want no error, so that a kind that does not exist is not retried", req, err)
			}
			if _, found := policyFrom(context.Background(), t, r, widget); found != tc.wantPolicy {
				t.Errorf("Reconcile(%v) left a generated policy = %v, want %v", req, found, tc.wantPolicy)
			}
			// No event is recorded either way: the resource an event would attach to is gone.
			if got := recordedReasons(recorder); len(got) != 0 {
				t.Errorf("Reconcile(%v) recorded events = %v, want none", req, got)
			}
		})
	}
}

// TestReconcileKeepsPolicyWhenOnlyVersionRetired covers a request whose recorded version is no
// longer served while the kind lives on under another. The generated policy watch can enqueue such
// a request, and treating the retired version as a gone kind would delete a policy that is still
// wanted. The resource is deliberately not re-read under the served version -- that version may not
// exist on the member clusters -- so the round is retried instead, and API discovery re-reports the
// resource under the served version.
func TestReconcileKeepsPolicyWhenOnlyVersionRetired(t *testing.T) {
	testCases := []struct {
		name   string
		source *unstructured.Unstructured
		// req names the source under a version the REST mapper no longer knows.
		req Request
	}{
		{
			name:   "a namespaced source served under apps/v1 is reached through apps/v2",
			source: newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector}),
			req:    requestFor(schema.GroupVersionKind{Group: "apps", Version: "v2", Kind: "Deployment"}, testNamespace, testName),
		},
		{
			name:   "a cluster scoped source served under v1 is reached through v2",
			source: newSource(namespaceGVK, "", testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector}),
			req:    requestFor(schema.GroupVersionKind{Group: "", Version: "v2", Kind: "Namespace"}, "", testName),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r, recorder := newReconciler(t, interceptor.Funcs{}, tc.source)

			// An existing policy stands in for one generated before the version was retired; the
			// round must leave it alone rather than take it for the policy of a gone kind.
			existing := desiredPolicy(tc.source, mustParse(t, oneSelector))
			if err := r.Create(ctx, existing); err != nil {
				t.Fatalf("Create(%v) = %v, want no error", klog.KObj(existing), err)
			}

			// Dropped, not retried: no error and no requeue, since the resource is re-reported
			// under the served version as a different request.
			res, err := r.Reconcile(ctx, tc.req)
			if err != nil {
				t.Errorf("Reconcile(%v) = %v, want the retired version dropped rather than retried", tc.req, err)
			}
			if res.RequeueAfter != 0 {
				t.Errorf("Reconcile(%v) = %+v, want no requeue for a version that will never come back", tc.req, res)
			}
			if _, found := policyFrom(ctx, t, r, tc.source); !found {
				t.Errorf("Reconcile(%v) deleted the generated policy, want it kept while the kind is still served", tc.req)
			}
			if diff := cmp.Diff(recordedReasons(recorder), []string(nil), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Reconcile(%v) recorded events mismatch (-got, +want):\n%s", tc.req, diff)
			}
		})
	}
}

// TestReconcileForeignPolicyAtGeneratedName covers a policy that already occupies a resource's
// generated name but was authored by someone else -- it carries none of this controller's provenance
// labels. The controller must neither overwrite it when the annotation asks for a policy nor delete
// it when the annotation is removed; a bare name match would do both.
func TestReconcileForeignPolicyAtGeneratedName(t *testing.T) {
	// A policy sitting at the generated name, distinguishable by a spec this controller would never
	// produce and by the absence of the provenance labels.
	foreignSpec := func() kfplacementv1alpha1.PlacementPolicySpec {
		return kfplacementv1alpha1.PlacementPolicySpec{
			ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{{APIGroup: "example.com", APIVersion: "v1", Kind: "Widget", Name: "hand-authored"}},
		}
	}
	newForeign := func() *kfplacementv1alpha1.PlacementPolicy {
		return &kfplacementv1alpha1.PlacementPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generatedPolicyName(deploymentGVK, testNamespace, testName),
				Namespace: testNamespace,
			},
			Spec: foreignSpec(),
		}
	}

	testCases := []struct {
		name        string
		source      *unstructured.Unstructured
		wantErr     error
		wantReasons []string
	}{
		{
			name:   "the annotation asks for a policy, the foreign one is not overwritten",
			source: newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector}),
			// The source is retried with backoff -- nothing else brings it back once the conflict is
			// cleared -- and the user hears why the placement they asked for is not running.
			wantErr:     errForeignPolicy,
			wantReasons: []string{EventReasonPolicyConflict},
		},
		{
			name: "the annotation is absent, the foreign one is not deleted",
			// Nothing asked for a policy here, so declining to delete a policy that was never this
			// controller's is silent -- the same as a resource that never generated anything.
			source:      newSource(deploymentGVK, testNamespace, testName, nil),
			wantReasons: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r, recorder := newReconciler(t, interceptor.Funcs{}, tc.source, newForeign())

			req := RequestFor(tc.source)
			if _, err := r.Reconcile(ctx, req); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Reconcile(%v) = %v, want %v", req, err, tc.wantErr)
			}

			got, found := policyFrom(ctx, t, r, tc.source)
			if !found {
				t.Fatalf("Reconcile(%v) removed the foreign policy, want it left in place", req)
			}
			gotSpec := got.(*kfplacementv1alpha1.PlacementPolicy).Spec
			if diff := cmp.Diff(gotSpec, foreignSpec()); diff != "" {
				t.Errorf("Reconcile(%v) changed the foreign policy's spec (-got, +want):\n%s", req, diff)
			}
			if labels := got.GetLabels(); len(labels) != 0 {
				t.Errorf("Reconcile(%v) added labels %v to the foreign policy, want it left untouched", req, labels)
			}
			if diff := cmp.Diff(recordedReasons(recorder), tc.wantReasons, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Reconcile(%v) recorded events mismatch (-got, +want):\n%s", req, diff)
			}
		})
	}
}

// TestReconcileRepairsDriftedProvenance covers a policy this controller generated whose provenance
// labels were later edited away. Its owner reference still identifies it as ours, so it must be
// repaired while the annotation stands and deleted once the annotation is removed -- never stranded
// as though it were foreign, which would leave its placement running with nothing able to reconcile
// or remove it.
func TestReconcileRepairsDriftedProvenance(t *testing.T) {
	annotated := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	selectors := mustParse(t, oneSelector)
	// A generated policy whose provenance labels have drifted -- one label is gone -- while the owner
	// reference this controller set is untouched, so the policy is still recognizable as ours.
	drifted := func() client.Object {
		policy := desiredPolicy(annotated, selectors)
		labels := policy.GetLabels()
		delete(labels, kfplacementv1alpha1.ParentKindLabel)
		policy.SetLabels(labels)
		return policy
	}

	testCases := []struct {
		name       string
		source     *unstructured.Unstructured
		wantPolicy bool
		wantReason string
	}{
		{
			name:       "the annotation stands, so the drifted labels are repaired",
			source:     annotated,
			wantPolicy: true,
			wantReason: EventReasonPolicyUpdated,
		},
		{
			name:       "the annotation is gone, so the policy is deleted",
			source:     newSource(deploymentGVK, testNamespace, testName, nil),
			wantPolicy: false,
			wantReason: EventReasonPolicyDeleted,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r, recorder := newReconciler(t, interceptor.Funcs{}, tc.source, drifted())

			req := RequestFor(tc.source)
			if _, err := r.Reconcile(ctx, req); err != nil {
				t.Fatalf("Reconcile(%v) = %v, want no error", req, err)
			}

			got, found := policyFrom(ctx, t, r, annotated)
			if found != tc.wantPolicy {
				t.Fatalf("Reconcile(%v) left a generated policy = %v, want %v", req, found, tc.wantPolicy)
			}
			if tc.wantPolicy {
				if diff := cmp.Diff(got.GetLabels(), desiredPolicy(annotated, selectors).GetLabels()); diff != "" {
					t.Errorf("Reconcile(%v) did not restore the drifted provenance labels (-got, +want):\n%s", req, diff)
				}
			}
			if diff := cmp.Diff(recordedReasons(recorder), []string{tc.wantReason}); diff != "" {
				t.Errorf("Reconcile(%v) recorded events mismatch (-got, +want):\n%s", req, diff)
			}
		})
	}
}

// TestReconcileResumesOnceForeignPolicyClears covers a source whose generated name is occupied by a
// foreign policy. Removing that policy fires no event that reaches the source -- it carries no owner
// reference to it -- and the annotation does not change, so the reconciler reports the conflict as
// an error to be retried with backoff; the pass after the conflict clears must then create the
// requested policy.
func TestReconcileResumesOnceForeignPolicyClears(t *testing.T) {
	ctx := context.Background()
	source := newSource(deploymentGVK, testNamespace, testName, map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector})
	foreign := &kfplacementv1alpha1.PlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generatedPolicyName(deploymentGVK, testNamespace, testName),
			Namespace: testNamespace,
		},
		Spec: kfplacementv1alpha1.PlacementPolicySpec{
			ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{{APIGroup: "example.com", APIVersion: "v1", Kind: "Widget", Name: "hand-authored"}},
		},
	}
	r, recorder := newReconciler(t, interceptor.Funcs{}, source, foreign)

	req := RequestFor(source)
	if _, err := r.Reconcile(ctx, req); !errors.Is(err, errForeignPolicy) {
		t.Fatalf("Reconcile(%v) = %v, want %v so the blocked source is retried", req, err, errForeignPolicy)
	}
	if diff := cmp.Diff(recordedReasons(recorder), []string{EventReasonPolicyConflict}); diff != "" {
		t.Errorf("Reconcile(%v) recorded events mismatch (-got, +want):\n%s", req, diff)
	}

	// The user removes the blocking policy. The next pass creates the requested policy.
	if err := r.Delete(ctx, foreign); err != nil {
		t.Fatalf("Delete(foreign) = %v, want no error", err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile(%v) = %v, want no error after the conflict cleared", req, err)
	}
	if _, found := policyFrom(ctx, t, r, source); !found {
		t.Errorf("Reconcile(%v) did not create the policy after the conflict cleared", req)
	}
	if diff := cmp.Diff(recordedReasons(recorder), []string{EventReasonPolicyCreated}); diff != "" {
		t.Errorf("Reconcile(%v) recorded events mismatch (-got, +want):\n%s", req, diff)
	}
}

func TestHasClusterSelectorsAnnotation(t *testing.T) {
	testCases := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{name: "no annotations at all", annotations: nil, want: false},
		{name: "other annotations only", annotations: map[string]string{"example.com/other": "value"}, want: false},
		{name: "the annotation is present", annotations: map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: oneSelector}, want: true},
		// An empty value is still a request for annotation-based placement; the parser is what
		// rejects it, and it can only do that if the resource reaches the queue at all.
		{name: "the annotation is present but empty", annotations: map[string]string{kfplacementv1alpha1.ClusterSelectorsAnnotation: ""}, want: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			source := newSource(deploymentGVK, testNamespace, testName, tc.annotations)
			if got := HasClusterSelectorsAnnotation(source); got != tc.want {
				t.Errorf("HasClusterSelectorsAnnotation(%v) = %v, want %v", tc.annotations, got, tc.want)
			}
		})
	}
}
