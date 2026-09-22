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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

// The reasons of the events this controller records on the annotated resource. Each reason is the
// same whether the generated object is a PlacementPolicy or a ClusterPlacementPolicy -- the reason
// names the action, and the event message names the concrete kind.
//
// The events are recorded on the resource the user annotated rather than on the policy generated
// from it, because the resource is where a user who has just run kubectl annotate is looking, and
// because a rejected annotation generates no policy for an event to be attached to.
const (
	// EventReasonPolicyCreated is recorded when a generated policy is created for a resource.
	EventReasonPolicyCreated = "PlacementPolicyCreated"
	// EventReasonPolicyUpdated is recorded when an annotation change reaches its generated policy.
	EventReasonPolicyUpdated = "PlacementPolicyUpdated"
	// EventReasonPolicyDeleted is recorded when the policy generated for a resource is deleted --
	// because the annotation was removed, or because the resource stopped being eligible for
	// placement. The event's message names which.
	EventReasonPolicyDeleted = "PlacementPolicyDeleted"
	// EventReasonInvalidAnnotation is recorded when an annotation cannot be parsed. It is a warning
	// rather than an error on the queue: no amount of retrying makes a malformed annotation parse,
	// so the event is the only way the user learns that the placement they asked for is not running.
	EventReasonInvalidAnnotation = "InvalidClusterSelectorsAnnotation"
	// EventReasonPolicyConflict is recorded when a policy already exists at a resource's generated
	// name but is not one this controller generated. It is a warning: the controller neither
	// overwrites nor deletes the pre-existing policy, so the placement the annotation asks for is not
	// running, and the event is how the user learns why.
	EventReasonPolicyConflict = "PlacementPolicyConflict"
)

// errForeignPolicy reports that a policy occupies a resource's generated name but is not one this
// controller generated.
//
// It is returned as an error rather than swallowed with a fixed requeue so that the source is
// retried with the queue's exponential backoff: nothing else brings it back, since the blocking
// policy carries no owner reference to the source and the annotation does not change, yet a
// conflict left in place must not be re-examined at a fixed, busy pace forever.
var errForeignPolicy = errors.New("a policy at the generated name was not generated from the annotation")

// errRetiredVersion reports that the version a request names is no longer served while its kind
// still is. The request cannot be retried as it stands -- the resource is re-reported under the
// served version as a different request, which is what reconciles it -- so it is dropped rather
// than requeued, and the generated policy is left alone.
var errRetiredVersion = errors.New("the version the resource was reported under is no longer served")

// Request identifies the resource a reconciliation is for.
//
// A plain namespaced name would not do: this controller reconciles resources of any kind the hub
// agent watches, and the name alone does not say which API to read the resource from.
type Request struct {
	schema.GroupVersionKind
	types.NamespacedName
}

// RequestFor returns the request that reconciles the given resource, which must carry its kind.
func RequestFor(object client.Object) Request {
	return Request{
		GroupVersionKind: object.GetObjectKind().GroupVersionKind(),
		NamespacedName:   client.ObjectKeyFromObject(object),
	}
}

// String formats the request for logs.
func (r Request) String() string {
	return fmt.Sprintf("%s %s", r.GroupVersionKind.String(), r.NamespacedName.String())
}

// Reconciler keeps the placement policy generated from a resource's cluster-selectors annotation in
// sync with that annotation.
//
// Its queue holds requests for resources of any kind the hub agent watches, not for the generated
// policies themselves; an event on a generated policy is mapped back to the resource it came from.
type Reconciler struct {
	// Client reads and writes the generated placement policies.
	//
	// It may be backed by the manager's cache: the generated policies are watched through the same
	// cache, so a policy event is never delivered ahead of the state this reads, and a write that
	// races a change the cache has yet to see fails with a conflict and is retried. The one thing
	// the cache can lag is this controller's own writes, which is why an already-exists error on a
	// create is treated as transient rather than as a failure.
	client.Client

	// APIReader reads the annotated resources straight from the API server.
	//
	// The resources are of any kind the hub agent watches, and a cache-backed read of a kind the
	// cache has not seen would start an informer for it -- for a kind that cannot be listed or
	// watched, one that never syncs. Only annotated resources and the owners of generated policies
	// reach this controller's queue, so the direct read is not on the hot path of every watched
	// resource in the cluster.
	APIReader client.Reader

	// RESTMapper resolves the kind a request names into the resource it is read through, and the
	// scope that decides whether the read is namespaced.
	RESTMapper meta.RESTMapper

	// Recorder records the outcome of a reconciliation on the annotated resource.
	Recorder events.EventRecorder

	// ShouldPlace reports whether a resource is one KubeFleet places at all, mirroring the filter
	// the resource watcher applies to its events. For example, a Deployment in a user namespace
	// should place (true); a ConfigMap in a skipped namespace like kube-system, or a ReplicaSet a
	// Deployment already owns, should not (false).
	//
	// The reconciler applies it again because it can be reached for a resource the watcher would
	// filter out: the watcher reports a resource that stops passing its filter as a deletion, and
	// the generated policy watch enqueues whatever a policy names as its owner. A resource that
	// fails the check has its generated policy deleted.
	//
	// Eligibility can change over a resource's life -- an owned ReplicaSet becomes eligible again
	// once orphaned, a resource stays ineligible while in a skipped namespace. The transitions that
	// matter are edits to the resource itself (its owner references, its labels), so each fires an
	// event that re-runs this check; a resource that becomes eligible again and still carries the
	// annotation has its policy regenerated on that event.
	//
	// Left nil, every resource is eligible.
	ShouldPlace func(source *unstructured.Unstructured) (bool, error)
}

// Reconcile brings the generated policy for one resource in line with that resource's annotation.
func (r *Reconciler) Reconcile(ctx context.Context, req Request) (ctrl.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("Reconciling annotation-based placement", "obj", req)
	defer func() {
		klog.V(2).InfoS("Annotation-based placement reconciliation loop ends", "obj", req, "latency", time.Since(startTime).Milliseconds())
	}()

	source, err := r.sourceObject(ctx, req)
	switch {
	case errors.Is(err, errRetiredVersion):
		// Dropped rather than retried: this request names a version that is gone, and no number of
		// retries brings it back. API discovery asks for server-preferred versions, so the resource
		// is re-reported under the served one as a fresh request, and that is what reconciles it.
		// The generated policy stands untouched in the meantime, since its kind still places.
		klog.V(2).InfoS("Dropped a request naming a version that is no longer served", "obj", req)
		return ctrl.Result{}, nil
	case apierrors.IsNotFound(err), meta.IsNoMatchError(err):
		// The resource is gone, or its whole kind is (the CRD was removed; the generated policy
		// watch enqueues a stale policy's owner). The generated policy is KubeFleet's own object,
		// so it is always deleted here rather than left to garbage collection, which would wait on
		// every owner reference -- including the foreign ones the merge deliberately preserves.
		_, deleted, err := r.deleteGeneratedPolicy(ctx, req.GroupVersionKind, req.Namespace, req.Name)
		switch {
		case err != nil:
			klog.ErrorS(err, "Failed to delete the policy generated for a resource that is gone", kferrors.Args(err, "obj", req)...)
		case deleted:
			klog.V(2).InfoS("Deleted the policy generated for a resource that is gone", "obj", req)
		}
		return ctrl.Result{}, err
	case err != nil:
		klog.ErrorS(err, "Failed to get the annotated resource", kferrors.Args(err)...)
		return ctrl.Result{}, err
	}

	if r.ShouldPlace != nil {
		eligible, err := r.ShouldPlace(source)
		if err != nil {
			klog.ErrorS(err, "Failed to decide whether the resource is eligible for placement", "obj", req)
			return ctrl.Result{}, err
		}
		if !eligible {
			// A resource KubeFleet does not place cannot keep a generated policy either; without
			// this, a resource that stops being eligible (for instance a ReplicaSet adopted by a
			// Deployment) would leave its policy behind, invisible to the watcher from then on.
			return ctrl.Result{}, r.deletePolicy(ctx, source, "the resource is not eligible for placement")
		}
	}

	value, annotated := source.GetAnnotations()[kfplacementv1alpha1.ClusterSelectorsAnnotation]
	if !annotated {
		return ctrl.Result{}, r.deletePolicy(ctx, source, "the "+kfplacementv1alpha1.ClusterSelectorsAnnotation+" annotation was removed")
	}

	selectors, err := parseClusterSelectors(value)
	if err != nil {
		// The annotation is the user's to fix, so the failure is reported to the user and the
		// request is dropped. Any policy generated from an earlier, valid annotation is deliberately
		// left standing: the desired state is now unknown, and tearing down a running placement is
		// a worse answer to a typo than leaving the last one the user did express.
		klog.V(2).InfoS("The annotation cannot be parsed", "obj", req, "err", err)
		r.Recorder.Eventf(source, nil, corev1.EventTypeWarning, EventReasonInvalidAnnotation, "ParseAnnotation",
			"The %s annotation is not valid and no placement policy was generated from it: %s", kfplacementv1alpha1.ClusterSelectorsAnnotation, err)
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, r.syncPolicy(ctx, source, selectors)
}

// sourceObject reads the annotated resource a request refers to.
//
// A not-found error and a no-match error (the kind itself is unknown) are returned as they are, so
// that the caller can tell the two apart from a read that failed.
func (r *Reconciler) sourceObject(ctx context.Context, req Request) (*unstructured.Unstructured, error) {
	// The resource is read under the version the request names and no other. Falling back to
	// whatever version is served now would quietly retarget placement at a version the member
	// clusters may not serve, turning a clear failure here into a confusing one later; and it
	// would buy nothing, since API discovery asks for server-preferred versions, so a resource
	// seen under a retired version is re-reported under the current one and reconciled again.
	restMapping, err := r.RESTMapper.RESTMapping(req.GroupKind(), req.Version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			// A no-match means either that the kind is gone or that only this version is. The two
			// call for opposite answers -- a gone kind should take its generated policy with it,
			// while a retired version should not, since the kind still places -- so they are told
			// apart by asking for the kind without a version. Only the truly gone kind is reported
			// as a no-match; a retired version is transient, and the re-report that discovery
			// produces under the served version reconciles it.
			if _, kindErr := r.RESTMapper.RESTMapping(req.GroupKind()); kindErr == nil {
				return nil, errRetiredVersion
			}
			return nil, err
		}
		return nil, kferrors.NewUnexpectedError(err, "failed to resolve the resource of the annotated object", "obj", req)
	}

	source := &unstructured.Unstructured{}
	source.SetGroupVersionKind(restMapping.GroupVersionKind)
	// Scope comes from the mapping rather than from the request, since the mapping is what the read
	// actually goes through. A cluster-scoped read must not name a namespace.
	key := client.ObjectKey{Name: req.Name}
	if restMapping.Scope.Name() != meta.RESTScopeNameRoot {
		key.Namespace = req.Namespace
	}
	if err := r.APIReader.Get(ctx, key, source); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, err
		}
		return nil, kferrors.NewAPIServerError(err, "failed to get the annotated resource", false, "obj", req)
	}
	return source, nil
}

// syncPolicy creates or updates the policy generated from a resource's annotation.
func (r *Reconciler) syncPolicy(ctx context.Context, source *unstructured.Unstructured, selectors []kfplacementv1alpha1.ClusterSelector) error {
	desired := desiredPolicy(source, selectors)
	policy := emptyPolicyForScope(source.GetNamespace())
	policy.SetName(desired.GetName())
	policy.SetNamespace(desired.GetNamespace())

	// The mutation runs only once the read behind it has succeeded, so whether it ran tells a failed
	// read apart from a failed write.
	read := false
	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, policy, func() error {
		read = true
		if policy.GetResourceVersion() != "" && !isGeneratedFor(policy, source.GroupVersionKind(), source.GetName(), r.Scheme()) {
			// A policy already occupies this resource's generated name, but it is not one this
			// controller produced -- a user or another tool authored a policy that happens to collide
			// with the deterministic name. Overwriting its spec would silently commandeer it, so it is
			// left exactly as found and the conflict is surfaced to the user instead. The name mixes in
			// a hash of the resource's identity, so a genuine collision is near impossible and almost
			// always means the name was chosen deliberately.
			return errForeignPolicy
		}
		return applyDesiredPolicy(policy, desired, source, r.Scheme())
	})
	switch {
	case errors.Is(err, errForeignPolicy):
		klog.V(2).InfoS("A policy at the generated name was not generated by this controller; leaving it untouched", "obj", klog.KObj(source), "policy", klog.KObj(policy))
		r.Recorder.Eventf(source, nil, corev1.EventTypeWarning, EventReasonPolicyConflict, "GeneratePolicy",
			"A %s named %s already exists and was not generated from the %s annotation; it was left unchanged", generatedPolicyKind(source.GetNamespace()), policy.GetName(), kfplacementv1alpha1.ClusterSelectorsAnnotation)
		return kferrors.NewUserError(err, "the generated name is taken", "obj", klog.KObj(source), "policy", klog.KObj(policy))
	case apierrors.IsAlreadyExists(err):
		// The cache has yet to see a policy this controller created moments ago, and an unrelated
		// second event for the source arrived in that window. The next pass, after the queue's
		// backoff, reads the policy the watch has delivered by then.
		klog.V(2).InfoS("The generated placement policy was created but is not in the cache yet; retrying", "obj", klog.KObj(source), "policy", klog.KObj(policy))
		return kferrors.NewTransientError(err, "the generated placement policy is not in the cache yet", "obj", klog.KObj(source), "policy", klog.KObj(policy))
	case err != nil:
		err = kferrors.NewAPIServerError(err, "failed to create or update the generated placement policy", !read, "obj", klog.KObj(source), "policy", klog.KObj(policy))
		klog.ErrorS(err, "Failed to sync the generated placement policy", kferrors.Args(err)...)
		return err
	}

	switch result {
	case controllerutil.OperationResultCreated:
		klog.V(2).InfoS("Created the generated placement policy", "obj", klog.KObj(source), "policy", klog.KObj(policy))
		r.Recorder.Eventf(source, nil, corev1.EventTypeNormal, EventReasonPolicyCreated, "GeneratePolicy",
			"Created the %s %s from the %s annotation", generatedPolicyKind(source.GetNamespace()), policy.GetName(), kfplacementv1alpha1.ClusterSelectorsAnnotation)
	case controllerutil.OperationResultUpdated:
		klog.V(2).InfoS("Updated the generated placement policy", "obj", klog.KObj(source), "policy", klog.KObj(policy))
		r.Recorder.Eventf(source, nil, corev1.EventTypeNormal, EventReasonPolicyUpdated, "GeneratePolicy",
			"Updated the %s %s from the %s annotation", generatedPolicyKind(source.GetNamespace()), policy.GetName(), kfplacementv1alpha1.ClusterSelectorsAnnotation)
	default:
		klog.V(3).InfoS("The generated placement policy is already up to date", "obj", klog.KObj(source), "policy", klog.KObj(policy))
	}
	return nil
}

// deletePolicy removes the policy generated for a resource that should not have one -- because the
// annotation was removed, or because the resource is not eligible for placement -- and tells the
// user which through an event carrying the given cause.
//
// The cause is a plain string, never a format: keeping the only format string in the Eventf call
// below constant is what lets go vet check it.
func (r *Reconciler) deletePolicy(ctx context.Context, source *unstructured.Unstructured, cause string) error {
	name, deleted, err := r.deleteGeneratedPolicy(ctx, source.GroupVersionKind(), source.GetNamespace(), source.GetName())
	if err != nil {
		klog.ErrorS(err, "Failed to delete the generated placement policy", kferrors.Args(err, "obj", klog.KObj(source), "policy", klog.KRef(source.GetNamespace(), name))...)
		return err
	}
	if !deleted {
		// The common case by far: a resource nobody annotated.
		return nil
	}
	klog.V(2).InfoS("Deleted the generated placement policy", "obj", klog.KObj(source), "policy", klog.KRef(source.GetNamespace(), name))
	r.Recorder.Eventf(source, nil, corev1.EventTypeNormal, EventReasonPolicyDeleted, "DeletePolicy", "Deleted the %s %s because %s", generatedPolicyKind(source.GetNamespace()), name, cause)
	return nil
}

// deleteGeneratedPolicy deletes the policy generated for the given resource identity, reporting the
// policy's name and whether this pass performed the deletion. The name is returned so a caller that
// logs or records an event about the deletion need not derive it a second time.
func (r *Reconciler) deleteGeneratedPolicy(ctx context.Context, gvk schema.GroupVersionKind, namespace, name string) (string, bool, error) {
	actual := emptyPolicyForScope(namespace)
	policyName := generatedPolicyName(gvk, namespace, name)

	err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: policyName}, actual)
	switch {
	case apierrors.IsNotFound(err):
		return policyName, false, nil
	case err != nil:
		return policyName, false, kferrors.NewAPIServerError(err, "failed to get the generated placement policy", true)
	}

	if !isGeneratedFor(actual, gvk, name, r.Scheme()) {
		// A policy occupies the generated name but this controller did not create it, so deleting it
		// would destroy a user's or another tool's object. It is never this controller's to remove;
		// the deletion is declined and reported as though nothing was there to delete.
		klog.V(2).InfoS("A policy at the generated name was not generated by this controller; declining to delete it", "policy", klog.KRef(namespace, policyName))
		return policyName, false, nil
	}

	// The delete carries the resource version the read returned as a precondition, so it removes only
	// the exact object this pass read and confirmed was one it generated. Between that read and here
	// the policy could be replaced -- deleted and a hand-authored one created at the same name, or
	// overwritten in place with its provenance stripped -- and an unconditioned delete, which targets
	// the name alone, would then remove whatever now sits there. The precondition turns that into a
	// conflict instead; the conflict requeues, and the next pass reads the current object and declines
	// it if it is no longer one this controller generated.
	resourceVersion := actual.GetResourceVersion()
	if err := r.Delete(ctx, actual, client.Preconditions{ResourceVersion: &resourceVersion}); err != nil {
		if apierrors.IsNotFound(err) {
			// The read above raced a deletion that already happened -- typically this controller's
			// own, re-entered through the generated policy watch moments later. Nothing was deleted
			// here, and reporting otherwise would log, and on some paths announce to the user, a
			// deletion that this pass did not perform.
			return policyName, false, nil
		}
		return policyName, false, kferrors.NewAPIServerError(err, "failed to delete the generated placement policy", false)
	}
	return policyName, true, nil
}

// applyDesiredPolicy brings a live generated policy in line with the desired one.
//
// Only what this controller generates is overwritten: the spec, the provenance labels, and the owner
// reference to the annotated resource. Labels and annotations that something else added are left
// alone, so that a generated policy can be labelled by an operator or a GitOps tool without this
// controller and that tool taking turns undoing each other. Whether anything changed is for the
// caller to judge by comparing the object before and after.
func applyDesiredPolicy(actual, desired kfplacementv1alpha1.PlacementPolicyAccessor, source *unstructured.Unstructured, scheme *runtime.Scheme) error {
	desired.GetSpec().DeepCopyInto(actual.GetSpec())

	labels := actual.GetLabels()
	if labels == nil {
		labels = make(map[string]string, len(desired.GetLabels()))
	}
	for key, value := range desired.GetLabels() {
		labels[key] = value
	}
	actual.SetLabels(labels)

	// The owner reference is matched on the source's group, kind, and name and replaced in place,
	// so a reference left by a source that was deleted and recreated under the same name (a new UID)
	// or written under a since-retired version is corrected rather than joined by a second one.
	return controllerutil.SetOwnerReference(source, actual, scheme)
}

// HasClusterSelectorsAnnotation reports whether an object carries the annotation this controller
// acts on. It is what keeps every unrelated resource in the cluster out of this controller's queue:
// whichever event source feeds the controller is expected to call it on every event for every
// watched resource, and to enqueue only the objects it reports true for.
func HasClusterSelectorsAnnotation(object metav1.Object) bool {
	_, found := object.GetAnnotations()[kfplacementv1alpha1.ClusterSelectorsAnnotation]
	return found
}
