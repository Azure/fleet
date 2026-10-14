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

// Package resourceeligibility features utilities for enforcing resource-level placement constraints in KubeFleet.
package resourceeligibility

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

// NamespaceEligibilityMode controls how a namespace is evaluated for placement eligibility.
type NamespaceEligibilityMode string

const (
	// NamespaceEligibilityModeAllowList means that namespaces that match with the checker are eligible for placement.
	NamespaceEligibilityModeAllowList NamespaceEligibilityMode = "AllowList"
	// NamespaceEligibilityModeDenyList means that namespaces that match with the checker are ineligible for placement.
	NamespaceEligibilityModeDenyList NamespaceEligibilityMode = "DenyList"
)

// GVKEligibilityMode controls how a GroupVersionKind (GVK) is evaluated for placement eligibility.
type GVKEligibilityMode string

const (
	// GVKEligibilityModeAllowList means that GVKs that match with the checker are eligible for placement.
	GVKEligibilityModeAllowList GVKEligibilityMode = "AllowList"
	// GVKEligibilityModeDenyList means that GVKs that match with the checker are ineligible for placement.
	GVKEligibilityModeDenyList GVKEligibilityMode = "DenyList"
)

// Checker reports whether a namespace, a resource type, or a resource object is eligible for placement.
//
// A Checker may wrap another Checker; the wrapped Checker is consulted after the current one, which
// allows a set of custom, user-supplied constraints to be layered on top of the KubeFleet built-in ones.
// Note that wrapping can only narrow eligibility, never widen it.
//
// Once set up, a Checker should not be modified anymore.
type Checker interface {
	SetNamespaceCheckList(mode NamespaceEligibilityMode, nsNames, nsNamePrefixes []string)
	SetGVKCheckList(mode GVKEligibilityMode, gvks []schema.GroupVersionKind) error

	Wraps(c Checker)
	DeepCopy() Checker

	AllowsNamespace(namespace string) bool
	AllowsGVK(gvk schema.GroupVersionKind) bool
	AllowsGVR(gvr schema.GroupVersionResource) (bool, error)
	AllowsObject(obj *unstructured.Unstructured) (bool, error)
}

// Verify that checker implements the Checker interface.
var _ Checker = &checker{}

// checker is the default Checker implementation; it evaluates a namespace check list and a GVK check
// list, each in either allow list or deny list mode, before deferring to any wrapped Checker.
type checker struct {
	nsEligibilityMode NamespaceEligibilityMode

	customNSList       sets.Set[string]
	customNSPrefixList sets.Set[string]

	gvkEligibilityMode GVKEligibilityMode

	customGVKList gvkMatcherTrie

	wrapped Checker

	restMapper meta.RESTMapper
}

// Baseline returns a Checker that features default namespace and GVK deny lists.
func Baseline(restMapper meta.RESTMapper) Checker {
	c := New(restMapper, nil)

	c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"default"}, []string{utils.KubeNSNamePrefix, utils.FleetNSNamePrefix})
	if err := c.SetGVKCheckList(GVKEligibilityModeDenyList, GVKsDeniedByDefault); err != nil {
		// The deny list is defined in this package, so a rejection here is a programming error.
		panic(fmt.Sprintf("failed to register the default GVK deny list: %v", err))
	}
	return c
}

// AllowAll returns a Checker that allows all namespaces and GVKs for placement.
//
// This function is added for testing purposes only.
func AllowAll(restMapper meta.RESTMapper) Checker {
	return &checker{
		nsEligibilityMode:  NamespaceEligibilityModeDenyList,
		gvkEligibilityMode: GVKEligibilityModeDenyList,
		restMapper:         restMapper,
	}
}

// DefaultForV0APIs returns a Checker for placement with v0 APIs.
func DefaultForV0APIs(restMapper meta.RESTMapper) Checker {
	c := New(restMapper, Baseline(restMapper))

	// TO-DO (chenyu1): deny envelope APIs in the kubefleet.dev API group when they are added.
	return c
}

// DefaultForV1APIs returns a Checker for placement with v1 APIs.
func DefaultForV1APIs(restMapper meta.RESTMapper) Checker {
	c := New(restMapper, Baseline(restMapper))

	if err := c.SetGVKCheckList(GVKEligibilityModeDenyList, GVKsDeniedForV1APIs); err != nil {
		// The deny list is defined in this package, so a rejection here is a programming error.
		panic(fmt.Sprintf("failed to register the default GVK deny list for the v1 APIs: %v", err))
	}
	return c
}

// New creates a new Checker.
func New(restMapper meta.RESTMapper, wrapped Checker) Checker {
	return &checker{
		nsEligibilityMode:  NamespaceEligibilityModeDenyList,
		gvkEligibilityMode: GVKEligibilityModeDenyList,
		wrapped:            wrapped,
		restMapper:         restMapper,
	}
}

// SetNamespaceCheckList sets the namespace check list for the checker, along with the mode in which
// the list is evaluated.
//
// Empty entries are ignored.
//
// For simplicity reasons, the setter will overwrite any existing check list and no concurrency control is added;
// the implementation assumes that a Checker is set up once before usage and is not modified at all afterwards.
func (c *checker) SetNamespaceCheckList(mode NamespaceEligibilityMode, nsNames, nsNamePrefixes []string) {
	c.nsEligibilityMode = mode
	c.customNSList = newNonEmptyStringSet(nsNames)
	c.customNSPrefixList = newNonEmptyStringSet(nsNamePrefixes)
}

// newNonEmptyStringSet builds a set from the given items, dropping empty ones; an empty prefix would
// otherwise match every namespace.
func newNonEmptyStringSet(items []string) sets.Set[string] {
	s := sets.New[string]()
	for _, item := range items {
		if item != "" {
			s.Insert(item)
		}
	}
	return s
}

// SetGVKCheckList sets the GVK check list for the checker, along with the mode in which the list is evaluated.
//
// For simplicity reasons, the setter will overwrite any existing check list and no concurrency control is added;
// the implementation assumes that a Checker is set up once before usage and is not modified at all afterwards.
func (c *checker) SetGVKCheckList(mode GVKEligibilityMode, gvks []schema.GroupVersionKind) error {
	// Build the trie separately so that a rejected GVK leaves the checker untouched.
	list := gvkMatcherTrie{}
	for _, gvk := range gvks {
		if err := list.Register(gvk); err != nil {
			return err
		}
	}

	c.gvkEligibilityMode = mode
	c.customGVKList = list
	return nil
}

// Wraps sets another Checker to be consulted for eligibility checks after this checker.
//
// For simplicity reasons, the setter will overwrite any existing wrapped checker and no concurrency control is added;
// the implementation assumes that a Checker is set up once before usage and is not modified at all afterwards.
func (c *checker) Wraps(wrapped Checker) {
	c.wrapped = wrapped
}

// DeepCopy creates a deep copy of the checker.
//
// The deep copy will have its own copies of the check lists and any wrapped checker,
// but will share the REST mapper reference.
func (c *checker) DeepCopy() Checker {
	cp := &checker{
		nsEligibilityMode:  c.nsEligibilityMode,
		customNSList:       c.customNSList.Clone(),
		customNSPrefixList: c.customNSPrefixList.Clone(),
		gvkEligibilityMode: c.gvkEligibilityMode,
		customGVKList:      c.customGVKList.DeepCopy(),
		// The REST mapper is a shared dependency rather than owned state, so the reference is kept.
		restMapper: c.restMapper,
	}
	if c.wrapped != nil {
		cp.wrapped = c.wrapped.DeepCopy()
	}
	return cp
}

// AllowsNamespace checks if a given namespace is eligible for placement.
func (c *checker) AllowsNamespace(namespace string) bool {
	if namespace == "" {
		// Allow cluster-scoped resources (which have an empty namespace) for placement.
		return true
	}

	hasAnyPrefix := func(prefixes sets.Set[string]) bool {
		for prefix := range prefixes {
			if strings.HasPrefix(namespace, prefix) {
				return true
			}
		}
		return false
	}

	// Check namespace eligibility based on the configured allow list or deny list.
	eligible := true
	switch c.nsEligibilityMode {
	case NamespaceEligibilityModeAllowList:
		eligible = c.customNSList.Has(namespace) || hasAnyPrefix(c.customNSPrefixList)
	case NamespaceEligibilityModeDenyList:
		eligible = !c.customNSList.Has(namespace) && !hasAnyPrefix(c.customNSPrefixList)
	}
	if !eligible {
		return false
	}

	// Consult the wrapped checker to check if the eligibility is further restricted.
	if c.wrapped != nil {
		return c.wrapped.AllowsNamespace(namespace)
	}
	return true
}

// AllowsGVK checks if a given GVK is eligible for placement.
func (c *checker) AllowsGVK(gvk schema.GroupVersionKind) bool {
	// Check GVK eligibility based on the configured allow list or deny list.
	eligible := true
	switch c.gvkEligibilityMode {
	case GVKEligibilityModeAllowList:
		eligible = c.customGVKList.Match(gvk)
	case GVKEligibilityModeDenyList:
		eligible = !c.customGVKList.Match(gvk)
	}
	if !eligible {
		return false
	}

	// Consult the wrapped checker to check if the eligibility is further restricted.
	if c.wrapped != nil {
		return c.wrapped.AllowsGVK(gvk)
	}
	return true
}

// AllowsGVR checks if a given GVR is eligible for placement.
func (c *checker) AllowsGVR(gvr schema.GroupVersionResource) (bool, error) {
	gvks, err := c.restMapper.KindsFor(gvr)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return false, errors.NewUserError(err, "no match is found given the GVR", "gvr", gvr)
		}
		return false, errors.NewUnexpectedError(err, "failed to map the resource to its kinds", "gvr", gvr)
	}
	if len(gvks) == 0 {
		noMatchErr := &meta.NoResourceMatchError{
			PartialResource: gvr,
		}
		return false, errors.NewUserError(noMatchErr, "no match is found given the GVR", "gvr", gvr)
	}

	// A resource is eligible only if every kind it maps to is eligible.
	for _, gvk := range gvks {
		if !c.AllowsGVK(gvk) {
			return false, nil
		}
	}

	// Consult the wrapped checker to check if the eligibility is further restricted.
	if c.wrapped != nil {
		return c.wrapped.AllowsGVR(gvr)
	}
	return true, nil
}

// AllowsObject checks if the given unstructured resource object is eligible for placement.
//
// This method will NOT check if the resource is eligible for placement given its namespaces, GVK (GVR); call
// the other methods for verification.
//
// Note (chenyu1): the logic defined here applies only to the v1 APIs (placement policies), as the v1 APIs
// require explicit resource selectors.
func (c *checker) AllowsObject(obj *unstructured.Unstructured) (bool, error) {
	switch {
	case obj.GetDeletionTimestamp() != nil:
		return false, errors.NewUserError(nil, "the resource is being deleted and cannot be placed")
	case len(obj.GetOwnerReferences()) > 0:
		return false, errors.NewUserError(nil, "the resource is under the management of another object; place the owner object instead if applicable")
	case obj.GroupVersionKind() == corev1.SchemeGroupVersion.WithKind(utils.ConfigMapKind) && obj.GetName() == "kube-root-ca.crt":
		return false, errors.NewUserError(nil, "the built-in CA certificate config map is created automatically in every namespace and cannot be placed")
	case obj.GroupVersionKind() == corev1.SchemeGroupVersion.WithKind("ServiceAccount") && obj.GetName() == "default":
		return false, errors.NewUserError(nil, "the default service account is created automatically in every namespace and cannot be placed")
	case obj.GroupVersionKind() == corev1.SchemeGroupVersion.WithKind(utils.ServiceKind) && obj.GetNamespace() == "default" && obj.GetName() == "kubernetes":
		return false, errors.NewUserError(nil, "the Kubernetes API service is created automatically in the default namespace and cannot be placed")
	case obj.GroupVersionKind() == corev1.SchemeGroupVersion.WithKind("Secret"):
		secretType, found, err := unstructured.NestedString(obj.Object, "type")
		if err != nil {
			return false, errors.NewUnexpectedError(err, "failed to retrieve the type of the secret")
		}
		if found && secretType == string(corev1.SecretTypeServiceAccountToken) {
			return false, errors.NewUserError(nil, "service account token secrets are created automatically along with their service accounts and cannot be placed")
		}
	}

	// Consult the wrapped checker to check if the eligibility is further restricted.
	if c.wrapped != nil {
		return c.wrapped.AllowsObject(obj)
	}
	return true, nil
}
