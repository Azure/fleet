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
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/naming"
)

const (
	// maxKindSegmentLength caps how much of the kind appears in a generated name. Every kind a user
	// is likely to annotate is far shorter; the cap exists so that a pathologically long custom
	// kind cannot crowd out the part of the name that identifies the resource.
	maxKindSegmentLength = 40

	// separatorCount is the number of separators a generated name spends joining its three parts.
	separatorCount = 2
)

// The values the placement policy API defaults these fields to.
//
// A generated policy sets them explicitly, rather than letting the API server fill them in, so
// that the object built here equals the one the API server stores. Left unset, every field the API
// defaults would read as a difference on every pass and provoke an update that changes nothing.
//
// whenUnfulfilled deliberately follows the API default of requesting a cluster: asking the platform
// for a cluster that does not exist yet is the point of these APIs, and the annotation is meant to
// be the lowest-friction way to do so. The grammar has no slot for the field, so guarding against
// a selector nothing can ever satisfy (a mistyped region, say) is the claim fulfillment layer's
// job -- quota, approval, expiry -- not a softer default here.
const (
	defaultResourceRevisionHistoryLimit int32 = 3

	defaultWhenUnfulfilled = kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim
)

// generatedPolicyName derives the name of the placement policy generated for a resource.
//
// The name is deterministic, so that reconciling the same resource twice converges on one policy
// instead of accumulating them, and it is readable, so that the policy can be recognized in a
// listing. Neither property may come at the cost of the third: the hash is taken over the
// resource's whole identity, including the parts that do not appear in the name at all, so two
// resources cannot generate one name even when truncation erases what distinguishes them.
func generatedPolicyName(gvk schema.GroupVersionKind, namespace, name string) string {
	// The kind and the name are sanitized before they appear in the name: a resource legal for its
	// own API can carry characters a DNS-1123 object name forbids (an RBAC name like
	// system:aggregate-to-admin), and copying them through would produce a policy the API server
	// rejects, hot-looping the reconciler. The hash below is taken over the unsanitized identity, so
	// sanitizing the visible parts costs no uniqueness.
	kindSegment := naming.Truncate(naming.Sanitize(gvk.Kind), maxKindSegmentLength)

	// Whatever the kind and the hash do not use is available to the resource's own name.
	nameBudget := validation.DNS1123SubdomainMaxLength - len(kindSegment) - naming.HashLength - separatorCount
	nameSegment := naming.Truncate(naming.Sanitize(name), nameBudget)

	// Empty segments are dropped rather than joined. An object always has a name, and one read
	// from the API server always has a kind, but joining an empty leading segment would put a
	// separator at the front of the name, where the API server does not allow one.
	segments := make([]string, 0, 3)
	for _, segment := range []string{kindSegment, nameSegment} {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	identity := fmt.Sprintf("%s/%s/%s/%s", gvk.Group, gvk.Kind, namespace, name)
	segments = append(segments, naming.Hash(identity))

	return strings.Join(segments, "-")
}

// parentLabels returns the labels recording which resource a generated policy came from.
//
// Every value is shortened to fit a label value, including the API group: a custom resource's
// group is validated as a DNS-1123 subdomain and so may run to 253 bytes, well past what a label
// value holds. A kind cannot exceed the limit today, but it is shortened alongside the others
// rather than relying on a bound that belongs to a different validation rule than this one.
func parentLabels(gvk schema.GroupVersionKind, name string) map[string]string {
	return map[string]string{
		kfplacementv1alpha1.ParentAPIGroupLabel: naming.LabelValue(gvk.Group),
		kfplacementv1alpha1.ParentKindLabel:     naming.LabelValue(gvk.Kind),
		kfplacementv1alpha1.ParentNameLabel:     naming.LabelValue(name),
	}
}

// isGeneratedFor reports whether a live policy is one this controller generated for the given
// resource identity. It is what stops the controller from overwriting or deleting a policy a user or
// another tool authored at the deterministic name this resource happens to generate.
//
// Ownership is recognized from either marker this controller stamps -- the owner reference back to
// the source, or the provenance labels -- and does not require both. Both are things applyDesiredPolicy
// repairs, so demanding both be intact would let a single edited label or stripped owner reference
// classify one of this controller's own policies as foreign: it would then be denied the very repair
// that would restore the marker and, worse, denied deletion when its annotation is removed, leaving a
// placement running with no way to reconcile or clean it up. Recognizing either marker keeps the
// policy repairable as long as one survives; a foreign policy that merely collides with the name
// carries neither and is still left untouched. Both markers are derived from the resource's stable
// identity rather than its UID, so the judgment holds across a delete and recreate of the resource.
//
// The one residual gap is a policy that loses both markers at once, which is then indistinguishable
// from a foreign object and abandoned. It is reachable without malice -- a tool that rewrites the
// whole object, such as a plain kubectl apply of a hand-kept manifest or a non-server-side-apply
// Update, drops metadata.labels and metadata.ownerReferences together -- but a policy this controller
// generated is not one a user is expected to manage that way, and requiring both markers to survive
// is the accepted price of never touching a policy that is genuinely someone else's.
func isGeneratedFor(policy client.Object, gvk schema.GroupVersionKind, name string, scheme *runtime.Scheme) bool {
	// The owner reference is matched on the source's group, kind, and name -- deliberately not on
	// its version or its UID, both of which can change while the resource keeps its name: a source
	// deleted and recreated comes back with a new UID, and a served version can be retired.
	source := &unstructured.Unstructured{}
	source.SetGroupVersionKind(gvk)
	source.SetName(name)
	if owned, err := controllerutil.HasOwnerReference(policy.GetOwnerReferences(), source, scheme); err == nil && owned {
		return true
	}

	labels := policy.GetLabels()
	for key, want := range parentLabels(gvk, name) {
		if labels[key] != want {
			return false
		}
	}
	return true
}

// parentOwnerReference returns the owner reference that ties a generated policy to the resource it
// was generated from, so that deleting the resource collects the policy with it.
//
// The reference deliberately claims neither controller nor blocking ownership: setting either
// requires permission to update the finalizers subresource of the owner, and the owner here can be
// a resource of any kind at all. Cascading deletion, the only property this needs, works the same
// without them.
func parentOwnerReference(source *unstructured.Unstructured) metav1.OwnerReference {
	gvk := source.GroupVersionKind()
	return metav1.OwnerReference{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       source.GetName(),
		UID:        source.GetUID(),
	}
}

// parentResourceSelector returns the selector that places the annotated resource itself.
//
// It is the only resource selector a generated policy has: an annotation asks for the resource it is
// written on to be placed, and nothing else. Should that ever stop being true, note that the
// reconciler replaces the whole spec of a generated policy, so a selector added from elsewhere would
// have to be reconciled here rather than merely appended to the live object.
func parentResourceSelector(source *unstructured.Unstructured) kfplacementv1alpha1.ResourceSelector {
	gvk := source.GroupVersionKind()
	// The namespace is deliberately left unset. A generated policy for a namespaced resource lives
	// in that resource's own namespace, where the API requires the selector's namespace to be
	// empty or identical; a generated policy for a cluster-scoped resource has no namespace to name.
	return kfplacementv1alpha1.ResourceSelector{
		APIGroup:   gvk.Group,
		APIVersion: gvk.Version,
		Kind:       gvk.Kind,
		Name:       source.GetName(),
	}
}

// withAPIDefaults returns the selectors with every field the API defaults set explicitly, leaving
// the caller's own slice untouched.
func withAPIDefaults(selectors []kfplacementv1alpha1.ClusterSelector) []kfplacementv1alpha1.ClusterSelector {
	if selectors == nil {
		return nil
	}
	defaulted := make([]kfplacementv1alpha1.ClusterSelector, len(selectors))
	for i, selector := range selectors {
		selector.DeepCopyInto(&defaulted[i])
		if defaulted[i].WhenUnfulfilled == "" {
			defaulted[i].WhenUnfulfilled = defaultWhenUnfulfilled
		}
	}
	return defaulted
}

// desiredPolicy builds the placement policy that should exist for an annotated resource.
//
// The policy's scope follows the resource's own: a namespaced resource yields a PlacementPolicy in
// its namespace, and a cluster-scoped resource yields a ClusterPlacementPolicy. That is what keeps
// the owner reference valid, since a cluster-scoped object owned by a namespaced one is accepted
// on creation and then never collected at all.
//
// Scope is read from the resource's own namespace, which the API server has already reconciled
// with the scope its kind is registered under; the source must therefore be an object read from
// the API server rather than one assembled in memory, which is also what guarantees it carries the
// UID and the kind that the owner reference and the generated name are built from. Taking an
// unstructured object rather than a client.Object is deliberate for the same reason: a typed
// object routinely arrives with an empty kind, which would silently change the generated name.
func desiredPolicy(source *unstructured.Unstructured, selectors []kfplacementv1alpha1.ClusterSelector) kfplacementv1alpha1.PlacementPolicyAccessor {
	gvk := source.GroupVersionKind()
	namespace := source.GetNamespace()

	policy := emptyPolicyForScope(namespace)
	policy.SetName(generatedPolicyName(gvk, namespace, source.GetName()))
	policy.SetNamespace(namespace)
	policy.SetLabels(parentLabels(gvk, source.GetName()))
	policy.SetOwnerReferences([]metav1.OwnerReference{parentOwnerReference(source)})
	*policy.GetSpec() = kfplacementv1alpha1.PlacementPolicySpec{
		ClusterSelectors: withAPIDefaults(selectors),
		// Only the annotated resource is selected; nothing it depends on. Selecting dependencies
		// is tracked in #927, where the questions are which references count, whether following
		// them should be opt-in, and how the closure stays fresh as the source changes.
		ResourceSelectors:            []kfplacementv1alpha1.ResourceSelector{parentResourceSelector(source)},
		ResourceRevisionHistoryLimit: ptr.To(defaultResourceRevisionHistoryLimit),
	}
	return policy
}

// emptyPolicyForScope returns an empty generated policy of the scope that a resource in the given
// namespace generates: a namespaced resource yields a PlacementPolicy in its own namespace, and a
// cluster-scoped resource, whose namespace is empty, yields a ClusterPlacementPolicy.
//
// This is the single place the scope is decided. The reconciler needs the same answer to read and to
// delete a generated policy as it does to build one, and a disagreement between those would leave a
// policy behind rather than fail.
func emptyPolicyForScope(namespace string) kfplacementv1alpha1.PlacementPolicyAccessor {
	if namespace == "" {
		return &kfplacementv1alpha1.ClusterPlacementPolicy{}
	}
	return &kfplacementv1alpha1.PlacementPolicy{}
}

// generatedPolicyKind returns the kind of the policy generated for a resource in the given
// namespace, spelled as the kind itself is, so that a message carrying it can be pasted straight
// into a kubectl command against the right resource.
func generatedPolicyKind(namespace string) string {
	if namespace == "" {
		return "ClusterPlacementPolicy"
	}
	return "PlacementPolicy"
}
