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
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/naming"
)

var (
	deploymentGVK = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	namespaceGVK  = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
)

// sourceObject builds the annotated resource a policy is generated from.
func sourceObject(gvk schema.GroupVersionKind, namespace, name string) *unstructured.Unstructured {
	source := &unstructured.Unstructured{}
	source.SetGroupVersionKind(gvk)
	source.SetNamespace(namespace)
	source.SetName(name)
	source.SetUID(types.UID("uid-" + name))
	return source
}

func TestGeneratedPolicyName(t *testing.T) {
	testCases := []struct {
		name      string
		gvk       schema.GroupVersionKind
		namespace string
		objName   string
		want      string
	}{
		{
			name:      "namespaced resource",
			gvk:       deploymentGVK,
			namespace: "prod",
			objName:   "app",
			want:      "deployment-app-" + hashOfIdentity("apps", "Deployment", "prod", "app"),
		},
		{
			name:    "cluster-scoped resource",
			gvk:     namespaceGVK,
			objName: "work",
			want:    "namespace-work-" + hashOfIdentity("", "Namespace", "", "work"),
		},
		{
			// The kind is lowercased so that the name is a legal DNS-1123 subdomain; the hash is
			// taken over the kind as it is really spelled, so two kinds that differ only in case
			// cannot generate the same name.
			name:      "kind is lowercased in the name but not in the identity",
			gvk:       schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "MyWidget"},
			namespace: "prod",
			objName:   "gadget",
			want:      "mywidget-gadget-" + hashOfIdentity("example.com", "MyWidget", "prod", "gadget"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := generatedPolicyName(tc.gvk, tc.namespace, tc.objName)
			if got != tc.want {
				t.Errorf("generatedPolicyName(%v, %q, %q) = %v, want %v", tc.gvk, tc.namespace, tc.objName, got, tc.want)
			}
		})
	}
}

func hashOfIdentity(group, kind, namespace, name string) string {
	return naming.Hash(fmt.Sprintf("%s/%s/%s/%s", group, kind, namespace, name))
}

// TestGeneratedPolicyNameValidity covers the inputs where the name has to be shortened to fit,
// which is where a generated name has twice gone wrong in this project: the result must stay a
// legal object name, and it must still tell two resources apart after the part that distinguishes
// them has been cut away.
func TestGeneratedPolicyNameValidity(t *testing.T) {
	longKind := "My" + strings.Repeat("Widget", 40)
	longName := strings.Repeat("a", validation.DNS1123SubdomainMaxLength)

	testCases := []struct {
		name      string
		gvk       schema.GroupVersionKind
		namespace string
		objName   string
	}{
		{name: "short everything", gvk: deploymentGVK, namespace: "prod", objName: "app"},
		{
			// An RBAC name carries a colon, legal for the resource but not for a Kubernetes object
			// name; without sanitizing it the generated name is rejected and the reconciler hot-loops.
			name:    "rbac name with a colon",
			gvk:     schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
			objName: "system:aggregate-to-admin",
		},
		{
			// A dot beside an illegal character is the case that a naive sanitizer keeping dots gets
			// wrong: it would leave a label ending in a dash, which the API server rejects. Resource
			// names commonly carry dots (domain-like custom resource names), so this is reachable.
			name:    "name with a dot beside an illegal character",
			gvk:     deploymentGVK,
			objName: "my.:app",
		},
		{name: "name at the object limit", gvk: deploymentGVK, namespace: "prod", objName: longName},
		{
			name:      "name truncated onto a separator",
			gvk:       deploymentGVK,
			namespace: "prod",
			objName:   strings.Repeat("a", 194) + "-" + strings.Repeat("b", 40),
		},
		{
			name:      "name truncated onto a dot",
			gvk:       deploymentGVK,
			namespace: "prod",
			objName:   strings.Repeat("a", 194) + "." + strings.Repeat("b", 40),
		},
		{
			name:      "kind longer than its budget",
			gvk:       schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: longKind},
			namespace: "prod",
			objName:   longName,
		},
		{
			// An object read from the API server always carries its kind, but an empty one must
			// not put a separator at the front of the generated name, where it would be rejected.
			name:      "empty kind",
			gvk:       schema.GroupVersionKind{Group: "apps", Version: "v1"},
			namespace: "prod",
			objName:   "app",
		},
		{
			name:      "empty kind and empty name",
			gvk:       schema.GroupVersionKind{Group: "apps", Version: "v1"},
			namespace: "prod",
		},
	}

	seen := make(map[string]string, len(testCases))
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := generatedPolicyName(tc.gvk, tc.namespace, tc.objName)
			if errs := validation.IsDNS1123Subdomain(got); len(errs) > 0 {
				t.Errorf("generatedPolicyName(%v, %q, %q) = %v, want a valid object name: %s",
					tc.gvk, tc.namespace, tc.objName, got, strings.Join(errs, "; "))
			}
			if len(got) > validation.DNS1123SubdomainMaxLength {
				t.Errorf("len(generatedPolicyName(%v, %q, %q)) = %d, want at most %d",
					tc.gvk, tc.namespace, tc.objName, len(got), validation.DNS1123SubdomainMaxLength)
			}
			if previous, collided := seen[got]; collided {
				t.Errorf("generatedPolicyName(%v, %q, %q) = %v, want a name distinct from that of %s",
					tc.gvk, tc.namespace, tc.objName, got, previous)
			}
			seen[got] = tc.name
		})
	}
}

// TestGeneratedPolicyNameDistinguishesIdentities pins the property the hash exists for: resources
// that differ anywhere in their identity, including in parts the readable name never shows, must
// not generate the same policy name.
func TestGeneratedPolicyNameDistinguishesIdentities(t *testing.T) {
	longName := strings.Repeat("a", 300)

	identities := []struct {
		name      string
		gvk       schema.GroupVersionKind
		namespace string
		objName   string
	}{
		{name: "baseline", gvk: deploymentGVK, namespace: "prod", objName: "app"},
		{name: "different namespace", gvk: deploymentGVK, namespace: "staging", objName: "app"},
		{name: "different api group, same kind", gvk: schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Deployment"}, namespace: "prod", objName: "app"},
		{name: "kind differing only in case", gvk: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DeploymenT"}, namespace: "prod", objName: "app"},
		// Two names that sanitize to the same visible segment -- a dot and a dash both render as a
		// dash -- must still generate distinct policy names, since the hash is over the raw identity.
		{name: "name with a dot", gvk: deploymentGVK, namespace: "prod", objName: "my.app"},
		{name: "name with a dash where the other has a dot", gvk: deploymentGVK, namespace: "prod", objName: "my-app"},
		// Two names identical up to well past the truncation point.
		{name: "long name", gvk: deploymentGVK, namespace: "prod", objName: longName + "one"},
		{name: "long name sharing a prefix", gvk: deploymentGVK, namespace: "prod", objName: longName + "two"},
	}

	seen := make(map[string]string, len(identities))
	for _, identity := range identities {
		got := generatedPolicyName(identity.gvk, identity.namespace, identity.objName)
		if previous, collided := seen[got]; collided {
			t.Errorf("generatedPolicyName for %s = %v, want a name distinct from that of %s", identity.name, got, previous)
		}
		seen[got] = identity.name
	}
}

// TestIsGeneratedFor pins the check that keeps this controller from commandeering or deleting a
// policy someone else authored at a resource's generated name, while still recognizing its own policy
// after either provenance marker has drifted so that the policy stays repairable and removable.
func TestIsGeneratedFor(t *testing.T) {
	source := sourceObject(deploymentGVK, "prod", "app")
	mine := desiredPolicy(source, nil)

	// The same policy with its provenance labels edited away; the owner reference still identifies it.
	labelsStripped := mine.DeepCopyObject().(client.Object)
	labelsStripped.SetLabels(nil)

	// The same policy with its owner reference removed; the provenance labels still identify it.
	ownerStripped := mine.DeepCopyObject().(client.Object)
	ownerStripped.SetOwnerReferences(nil)

	// The same policy stripped of both markers; nothing is left to tell it from a foreign object.
	bothStripped := mine.DeepCopyObject().(client.Object)
	bothStripped.SetLabels(nil)
	bothStripped.SetOwnerReferences(nil)

	// A policy at the same name authored by someone else, carrying neither marker.
	foreign := emptyPolicyForScope("prod")
	foreign.SetName(mine.GetName())
	foreign.SetNamespace("prod")

	// A policy that carries another resource's provenance -- the collision a bare name match would
	// miss -- and no owner reference to this source.
	otherSource := sourceObject(deploymentGVK, "prod", "other")
	otherLabelled := emptyPolicyForScope("prod")
	otherLabelled.SetName(mine.GetName())
	otherLabelled.SetNamespace("prod")
	otherLabelled.SetLabels(parentLabels(otherSource.GroupVersionKind(), otherSource.GetName()))

	testCases := []struct {
		name   string
		policy client.Object
		want   bool
	}{
		{name: "the policy this controller generated", policy: mine, want: true},
		{name: "our policy with its labels stripped is still recognized by its owner reference", policy: labelsStripped, want: true},
		{name: "our policy with its owner reference stripped is still recognized by its labels", policy: ownerStripped, want: true},
		{name: "our policy with both markers gone is indistinguishable from foreign", policy: bothStripped, want: false},
		{name: "a foreign policy with neither marker", policy: foreign, want: false},
		{name: "a policy carrying another resource's provenance", policy: otherLabelled, want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGeneratedFor(tc.policy, source.GroupVersionKind(), source.GetName(), newScheme(t)); got != tc.want {
				t.Errorf("isGeneratedFor(%q) = %v, want %v", tc.policy.GetName(), got, tc.want)
			}
		})
	}
}

func TestDesiredPolicy(t *testing.T) {
	// The parser leaves whenUnfulfilled unset, since the annotation grammar cannot express it; the
	// generated policy carries the API's default explicitly so that it does not read as drift.
	selectors := []kfplacementv1alpha1.ClusterSelector{
		{
			Count: ptr.To(intstr.FromInt32(1)),
			Terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				{MatchLabels: map[string]string{"env": "prod"}},
			},
		},
	}
	wantSelectors := []kfplacementv1alpha1.ClusterSelector{
		{
			Count: ptr.To(intstr.FromInt32(1)),
			Terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				{MatchLabels: map[string]string{"env": "prod"}},
			},
			WhenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim,
		},
	}

	testCases := []struct {
		name   string
		source *unstructured.Unstructured
		want   client.Object
	}{
		{
			name:   "namespaced resource yields a namespaced policy",
			source: sourceObject(deploymentGVK, "prod", "app"),
			want: &kfplacementv1alpha1.PlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deployment-app-" + hashOfIdentity("apps", "Deployment", "prod", "app"),
					Namespace: "prod",
					Labels: map[string]string{
						kfplacementv1alpha1.ParentAPIGroupLabel: "apps",
						kfplacementv1alpha1.ParentKindLabel:     "Deployment",
						kfplacementv1alpha1.ParentNameLabel:     "app",
					},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "app",
						UID:        types.UID("uid-app"),
					}},
				},
				Spec: kfplacementv1alpha1.PlacementPolicySpec{
					ClusterSelectors: wantSelectors,
					ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{{
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Name:       "app",
					}},
					ResourceRevisionHistoryLimit: ptr.To(defaultResourceRevisionHistoryLimit),
				},
			},
		},
		{
			name:   "cluster-scoped resource yields a cluster-scoped policy",
			source: sourceObject(namespaceGVK, "", "work"),
			want: &kfplacementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "namespace-work-" + hashOfIdentity("", "Namespace", "", "work"),
					Labels: map[string]string{
						// The core API group is the empty string, and the label is present all the
						// same, so that core-group resources can be selected like any other.
						kfplacementv1alpha1.ParentAPIGroupLabel: "",
						kfplacementv1alpha1.ParentKindLabel:     "Namespace",
						kfplacementv1alpha1.ParentNameLabel:     "work",
					},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: "v1",
						Kind:       "Namespace",
						Name:       "work",
						UID:        types.UID("uid-work"),
					}},
				},
				Spec: kfplacementv1alpha1.PlacementPolicySpec{
					ClusterSelectors: wantSelectors,
					ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{{
						APIVersion: "v1",
						Kind:       "Namespace",
						Name:       "work",
					}},
					ResourceRevisionHistoryLimit: ptr.To(defaultResourceRevisionHistoryLimit),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := desiredPolicy(tc.source, selectors)
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("desiredPolicy() mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestParentOwnerReferenceIsNotControlling pins the choice not to claim controller or blocking
// ownership: either would require permission to update the finalizers subresource of a resource of
// any kind the user cares to annotate.
func TestParentOwnerReferenceIsNotControlling(t *testing.T) {
	got := parentOwnerReference(sourceObject(deploymentGVK, "prod", "app"))
	if got.Controller != nil {
		t.Errorf("parentOwnerReference().Controller = %v, want nil", *got.Controller)
	}
	if got.BlockOwnerDeletion != nil {
		t.Errorf("parentOwnerReference().BlockOwnerDeletion = %v, want nil", *got.BlockOwnerDeletion)
	}
}

// TestParentNameLabelIsShortenedWhenTooLong covers a resource whose name cannot fit in a label
// value, where the label is knowingly lossy and the owner reference is what remains exact.
func TestParentNameLabelIsShortenedWhenTooLong(t *testing.T) {
	longName := strings.Repeat("a", 200)
	source := sourceObject(deploymentGVK, "prod", longName)

	policy := desiredPolicy(source, nil)
	labels := policy.GetLabels()
	got := labels[kfplacementv1alpha1.ParentNameLabel]

	if got == longName {
		t.Errorf("parent name label = the full name, want it shortened to fit a label value")
	}
	if errs := validation.IsValidLabelValue(got); len(errs) > 0 {
		t.Errorf("parent name label = %v, want a valid label value: %s", got, strings.Join(errs, "; "))
	}
	if wantOwner := longName; policy.GetOwnerReferences()[0].Name != wantOwner {
		t.Errorf("owner reference name = %v, want the unshortened %v", policy.GetOwnerReferences()[0].Name, wantOwner)
	}
}

// TestParentLabelsAreValid covers the label values that have to be shortened to fit. A custom
// resource's API group is validated as a DNS-1123 subdomain and so runs to 253 bytes, four times
// what a label value holds; left unshortened it would make every generated policy for that
// resource fail validation on creation, with nothing but a retrying reconciler to show for it.
func TestParentLabelsAreValid(t *testing.T) {
	testCases := []struct {
		name    string
		gvk     schema.GroupVersionKind
		objName string
	}{
		{name: "ordinary resource", gvk: deploymentGVK, objName: "app"},
		{name: "core group resource", gvk: namespaceGVK, objName: "work"},
		{
			name:    "api group too long for a label value",
			gvk:     schema.GroupVersionKind{Group: strings.Repeat("g", 90) + ".example.com", Version: "v1", Kind: "Widget"},
			objName: "gadget",
		},
		{
			name:    "kind at the longest a kind can be",
			gvk:     schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: strings.Repeat("W", 63)},
			objName: "gadget",
		},
		{name: "name too long for a label value", gvk: deploymentGVK, objName: strings.Repeat("a", 200)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range parentLabels(tc.gvk, tc.objName) {
				if errs := validation.IsValidLabelValue(value); len(errs) > 0 {
					t.Errorf("parentLabels()[%s] = %v, want a valid label value: %s", key, value, strings.Join(errs, "; "))
				}
			}
		})
	}
}

// TestDesiredPolicySetsDefaultedFields pins that the generated policy carries a value for every
// field the API server would otherwise default. A field left unset is stored with the API's
// default and then reads as a difference on the next pass, which would have the reconciler issue
// an update that changes nothing, forever.
func TestDesiredPolicySetsDefaultedFields(t *testing.T) {
	selectors := []kfplacementv1alpha1.ClusterSelector{{Count: ptr.To(intstr.FromInt32(1))}}
	spec := desiredPolicy(sourceObject(deploymentGVK, "prod", "app"), selectors).GetSpec()

	if got := spec.ResourceRevisionHistoryLimit; got == nil || *got != defaultResourceRevisionHistoryLimit {
		t.Errorf("resourceRevisionHistoryLimit = %v, want %d", got, defaultResourceRevisionHistoryLimit)
	}
	if got := spec.ClusterSelectors[0].WhenUnfulfilled; got != defaultWhenUnfulfilled {
		t.Errorf("whenUnfulfilled = %v, want %v", got, defaultWhenUnfulfilled)
	}
	// The caller's own selectors must not have been defaulted in place.
	if got := selectors[0].WhenUnfulfilled; got != "" {
		t.Errorf("the caller's selector whenUnfulfilled = %v, want it left empty", got)
	}
}
