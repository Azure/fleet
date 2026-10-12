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

package bindingmanager

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/google/go-cmp/cmp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
)

var _ = Describe("Claiming as the binding manager (ClusterPlacementPolicy)", func() {
	Context("when the placement policy is nil", func() {
		It("should return an error without claiming the binding manager role", func() {
			claimed, err := ClaimRoleAs(ctx, hubClient, nil, "test-controller", placementv1alpha1.ObjectReference{})
			Expect(err).To(HaveOccurred())
			Expect(claimed).To(BeFalse())
		})

		It("should return an error when a typed nil pointer is given", func() {
			var policy *placementv1alpha1.ClusterPlacementPolicy
			claimed, err := ClaimRoleAs(ctx, hubClient, policy, "test-controller", placementv1alpha1.ObjectReference{})
			Expect(err).To(HaveOccurred())
			Expect(claimed).To(BeFalse())
		})
	})

	Context("when no controller name is provided", func() {
		It("should return an error without claiming the binding manager role", func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-placement-policy",
				},
			}
			claimed, err := ClaimRoleAs(ctx, hubClient, policy, "", placementv1alpha1.ObjectReference{})
			Expect(err).To(HaveOccurred())
			Expect(claimed).To(BeFalse())
		})
	})

	DescribeTable("when the object reference is incomplete",
		func(objectRef placementv1alpha1.ObjectReference) {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster-placement-policy"},
			}
			claimed, err := ClaimRoleAs(ctx, hubClient, policy, "test-controller", objectRef)
			Expect(err).To(HaveOccurred())
			Expect(claimed).To(BeFalse())
		},
		Entry("no name", placementv1alpha1.ObjectReference{
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}),
		Entry("no API version", placementv1alpha1.ObjectReference{
			Name: "test-object",
			Kind: "DummyOwner",
		}),
		Entry("no kind", placementv1alpha1.ObjectReference{
			Name:       "test-object",
			APIVersion: placementv1alpha1.GroupVersion.Version,
		}),
	)

	Context("when the binding manager role has not been claimed yet", Ordered, func() {
		const (
			controllerName = "test-controller"
			policyName     = "fresh-claim"
		)

		objectRef := placementv1alpha1.ObjectReference{
			Name:       "test-object",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}

		BeforeAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{APIVersion: "v1", Kind: "Namespace", Name: "test-namespace"},
					},
				},
			}
			Expect(hubClient.Create(ctx, policy)).To(Succeed())
		})

		AfterAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
			}
			Expect(hubClient.Delete(ctx, policy)).To(Succeed())
		})

		It("should claim the role and record the object reference", func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, policy)).To(Succeed())

			claimed, err := ClaimRoleAs(ctx, hubClient, policy, controllerName, objectRef)
			Expect(err).ToNot(HaveOccurred())
			Expect(claimed).To(BeTrue())

			updated := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, updated)).To(Succeed())
			want := &placementv1alpha1.BindingManager{
				ControllerName: controllerName,
				ObjectRefs:     []placementv1alpha1.ObjectReference{objectRef},
			}
			Expect(cmp.Diff(updated.Status.BindingManager, want)).To(BeEmpty())
		})
	})

	Context("when the binding manager role has been claimed by another controller", Ordered, func() {
		const (
			controllerName      = "test-controller"
			otherControllerName = "other-controller"
			policyName          = "claimed-by-another-controller"
		)

		objectRef := placementv1alpha1.ObjectReference{
			Name:       "test-object",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}
		otherObjectRef := placementv1alpha1.ObjectReference{
			Name:       "other-object",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}
		wantBindingManager := &placementv1alpha1.BindingManager{
			ControllerName: otherControllerName,
			ObjectRefs:     []placementv1alpha1.ObjectReference{otherObjectRef},
		}

		BeforeAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{APIVersion: "v1", Kind: "Namespace", Name: "test-namespace"},
					},
				},
			}
			Expect(hubClient.Create(ctx, policy)).To(Succeed())
			policy.Status.BindingManager = wantBindingManager.DeepCopy()
			Expect(hubClient.Status().Update(ctx, policy)).To(Succeed())
		})

		AfterAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
			}
			Expect(hubClient.Delete(ctx, policy)).To(Succeed())
		})

		It("should not claim the role and should leave the existing claim untouched", func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, policy)).To(Succeed())

			claimed, err := ClaimRoleAs(ctx, hubClient, policy, controllerName, objectRef)
			Expect(err).ToNot(HaveOccurred())
			Expect(claimed).To(BeFalse())

			updated := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, updated)).To(Succeed())
			Expect(cmp.Diff(updated.Status.BindingManager, wantBindingManager)).To(BeEmpty())
		})
	})

	Context("when the binding manager role has already been claimed by the same controller", Ordered, func() {
		const (
			controllerName = "test-controller"
			policyName     = "claimed-by-same-controller"
		)

		existingRef := placementv1alpha1.ObjectReference{
			Name:       "existing-object",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}
		newRef := placementv1alpha1.ObjectReference{
			Name:       "new-object",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}

		BeforeAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{APIVersion: "v1", Kind: "Namespace", Name: "test-namespace"},
					},
				},
			}
			Expect(hubClient.Create(ctx, policy)).To(Succeed())
			policy.Status.BindingManager = &placementv1alpha1.BindingManager{
				ControllerName: controllerName,
				ObjectRefs:     []placementv1alpha1.ObjectReference{existingRef},
			}
			Expect(hubClient.Status().Update(ctx, policy)).To(Succeed())
		})

		AfterAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
			}
			Expect(hubClient.Delete(ctx, policy)).To(Succeed())
		})

		It("should be a no-op when the object reference is already present", func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, policy)).To(Succeed())

			claimed, err := ClaimRoleAs(ctx, hubClient, policy, controllerName, existingRef)
			Expect(err).ToNot(HaveOccurred())
			Expect(claimed).To(BeTrue())

			updated := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, updated)).To(Succeed())
			want := &placementv1alpha1.BindingManager{
				ControllerName: controllerName,
				ObjectRefs:     []placementv1alpha1.ObjectReference{existingRef},
			}
			Expect(cmp.Diff(updated.Status.BindingManager, want)).To(BeEmpty())
		})

		It("should append a new object reference to the existing claim", func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, policy)).To(Succeed())

			claimed, err := ClaimRoleAs(ctx, hubClient, policy, controllerName, newRef)
			Expect(err).ToNot(HaveOccurred())
			Expect(claimed).To(BeTrue())

			updated := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, updated)).To(Succeed())
			want := &placementv1alpha1.BindingManager{
				ControllerName: controllerName,
				ObjectRefs:     []placementv1alpha1.ObjectReference{existingRef, newRef},
			}
			Expect(cmp.Diff(updated.Status.BindingManager, want)).To(BeEmpty())
		})
	})

	Context("when the view of the placement policy is stale (dry-run verification)", Ordered, func() {
		const (
			controllerName      = "test-controller"
			otherControllerName = "other-controller"
			policyName          = "stale-view-on-claim"
		)

		objectRef := placementv1alpha1.ObjectReference{
			Name:       "existing-object",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}
		// The claim written out of band, behind the back of the stale copy.
		wantBindingManager := &placementv1alpha1.BindingManager{
			ControllerName: otherControllerName,
			ObjectRefs: []placementv1alpha1.ObjectReference{
				{
					Name:       "other-object",
					APIGroup:   placementv1alpha1.GroupVersion.Group,
					APIVersion: placementv1alpha1.GroupVersion.Version,
					Kind:       "DummyOwner",
				},
			},
		}

		// A copy of the policy that is read before the out-of-band claim below.
		var stalePolicy *placementv1alpha1.ClusterPlacementPolicy

		BeforeAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{APIVersion: "v1", Kind: "Namespace", Name: "test-namespace"},
					},
				},
			}
			Expect(hubClient.Create(ctx, policy)).To(Succeed())
			// The stale copy must carry a matching claim; otherwise the dry-run branches are never reached.
			policy.Status.BindingManager = &placementv1alpha1.BindingManager{
				ControllerName: controllerName,
				ObjectRefs:     []placementv1alpha1.ObjectReference{objectRef},
			}
			Expect(hubClient.Status().Update(ctx, policy)).To(Succeed())

			stalePolicy = &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, stalePolicy)).To(Succeed())

			latestPolicy := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, latestPolicy)).To(Succeed())
			latestPolicy.Status.BindingManager = wantBindingManager.DeepCopy()
			Expect(hubClient.Status().Update(ctx, latestPolicy)).To(Succeed())
		})

		AfterAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
			}
			Expect(hubClient.Delete(ctx, policy)).To(Succeed())
		})

		It("should report a conflict when the object reference is already present", func() {
			claimed, err := ClaimRoleAs(ctx, hubClient, stalePolicy, controllerName, objectRef)
			Expect(apierrors.IsConflict(err)).To(BeTrue(), "ClaimRoleAs() = %v, want a conflict error", err)
			Expect(claimed).To(BeFalse())
		})

		It("should leave the persisted binding manager claim untouched", func() {
			updated := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, updated)).To(Succeed())
			Expect(cmp.Diff(updated.Status.BindingManager, wantBindingManager)).To(BeEmpty())
		})
	})
})

var _ = Describe("Claiming as the binding manager (PlacementPolicy)", func() {
	Context("when the binding manager role has not been claimed yet", Ordered, func() {
		const (
			controllerName = "test-controller"
			policyName     = "ns-fresh-claim"
		)

		objectRef := placementv1alpha1.ObjectReference{
			Namespace:  playgroundNamespace,
			Name:       "test-object",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}

		BeforeAll(func() {
			policy := &placementv1alpha1.PlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Namespace: playgroundNamespace, Name: policyName},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{APIVersion: "v1", Kind: "ConfigMap", Name: "test-config-map"},
					},
				},
			}
			Expect(hubClient.Create(ctx, policy)).To(Succeed())
		})

		AfterAll(func() {
			policy := &placementv1alpha1.PlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Namespace: playgroundNamespace, Name: policyName},
			}
			Expect(hubClient.Delete(ctx, policy)).To(Succeed())
		})

		It("should claim the role and record the object reference", func() {
			policy := &placementv1alpha1.PlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: playgroundNamespace, Name: policyName}, policy)).To(Succeed())

			claimed, err := ClaimRoleAs(ctx, hubClient, policy, controllerName, objectRef)
			Expect(err).ToNot(HaveOccurred())
			Expect(claimed).To(BeTrue())

			updated := &placementv1alpha1.PlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: playgroundNamespace, Name: policyName}, updated)).To(Succeed())
			want := &placementv1alpha1.BindingManager{
				ControllerName: controllerName,
				ObjectRefs:     []placementv1alpha1.ObjectReference{objectRef},
			}
			Expect(cmp.Diff(updated.Status.BindingManager, want)).To(BeEmpty())
		})
	})
})

var _ = Describe("Relinquishing the binding manager role (ClusterPlacementPolicy)", func() {
	Context("when the placement policy is nil", func() {
		It("should return an error", func() {
			err := RelinquishRoleFor(ctx, hubClient, nil, "test-controller", placementv1alpha1.ObjectReference{})
			Expect(err).To(HaveOccurred())
		})

		It("should return an error when a typed nil pointer is given", func() {
			var policy *placementv1alpha1.ClusterPlacementPolicy
			err := RelinquishRoleFor(ctx, hubClient, policy, "test-controller", placementv1alpha1.ObjectReference{})
			Expect(err).To(HaveOccurred())
		})
	})

	Context("when no controller name is provided", func() {
		It("should return an error", func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "relinquish-no-controller-name"},
			}
			err := RelinquishRoleFor(ctx, hubClient, policy, "", placementv1alpha1.ObjectReference{})
			Expect(err).To(HaveOccurred())
		})
	})

	DescribeTable("when the object reference is incomplete",
		func(objectRef placementv1alpha1.ObjectReference) {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "relinquish-incomplete-object-ref"},
			}
			err := RelinquishRoleFor(ctx, hubClient, policy, "test-controller", objectRef)
			Expect(err).To(HaveOccurred())
		},
		Entry("no name", placementv1alpha1.ObjectReference{
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}),
		Entry("no API version", placementv1alpha1.ObjectReference{
			Name: "test-object",
			Kind: "DummyOwner",
		}),
		Entry("no kind", placementv1alpha1.ObjectReference{
			Name:       "test-object",
			APIVersion: placementv1alpha1.GroupVersion.Version,
		}),
	)

	Context("when relinquishing an object reference", Ordered, func() {
		const (
			controllerName = "test-controller"
			policyName     = "relinquish-object-ref"
		)

		refA := placementv1alpha1.ObjectReference{
			Name:       "object-a",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}
		refB := placementv1alpha1.ObjectReference{
			Name:       "object-b",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}

		BeforeAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{APIVersion: "v1", Kind: "Namespace", Name: "test-namespace"},
					},
				},
			}
			Expect(hubClient.Create(ctx, policy)).To(Succeed())
			policy.Status.BindingManager = &placementv1alpha1.BindingManager{
				ControllerName: controllerName,
				ObjectRefs:     []placementv1alpha1.ObjectReference{refA, refB},
			}
			Expect(hubClient.Status().Update(ctx, policy)).To(Succeed())
		})

		AfterAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
			}
			Expect(hubClient.Delete(ctx, policy)).To(Succeed())
		})

		It("should remove the object reference while keeping the remaining ones", func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, policy)).To(Succeed())

			err := RelinquishRoleFor(ctx, hubClient, policy, controllerName, refA)
			Expect(err).ToNot(HaveOccurred())

			updated := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, updated)).To(Succeed())
			want := &placementv1alpha1.BindingManager{
				ControllerName: controllerName,
				ObjectRefs:     []placementv1alpha1.ObjectReference{refB},
			}
			Expect(cmp.Diff(updated.Status.BindingManager, want)).To(BeEmpty())
		})

		It("should remove the binding manager claim when the last object reference is relinquished", func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, policy)).To(Succeed())

			err := RelinquishRoleFor(ctx, hubClient, policy, controllerName, refB)
			Expect(err).ToNot(HaveOccurred())

			updated := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, updated)).To(Succeed())
			Expect(updated.Status.BindingManager).To(BeNil())
		})
	})

	Context("when the view of the placement policy is stale (dry-run verification)", Ordered, func() {
		const (
			controllerName      = "test-controller"
			otherControllerName = "other-controller"
			policyName          = "stale-view-on-relinquish"
		)

		objectRef := placementv1alpha1.ObjectReference{
			Name:       "existing-object",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}
		unknownRef := placementv1alpha1.ObjectReference{
			Name:       "unknown-object",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}
		// The claim written out of band, behind the back of the stale copy.
		wantBindingManager := &placementv1alpha1.BindingManager{
			ControllerName: otherControllerName,
			ObjectRefs: []placementv1alpha1.ObjectReference{
				{
					Name:       "other-object",
					APIGroup:   placementv1alpha1.GroupVersion.Group,
					APIVersion: placementv1alpha1.GroupVersion.Version,
					Kind:       "DummyOwner",
				},
			},
		}

		// A copy of the policy that is read before the out-of-band claim below.
		var stalePolicy *placementv1alpha1.ClusterPlacementPolicy

		BeforeAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{APIVersion: "v1", Kind: "Namespace", Name: "test-namespace"},
					},
				},
			}
			Expect(hubClient.Create(ctx, policy)).To(Succeed())
			policy.Status.BindingManager = &placementv1alpha1.BindingManager{
				ControllerName: controllerName,
				ObjectRefs:     []placementv1alpha1.ObjectReference{objectRef},
			}
			Expect(hubClient.Status().Update(ctx, policy)).To(Succeed())

			stalePolicy = &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, stalePolicy)).To(Succeed())

			latestPolicy := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, latestPolicy)).To(Succeed())
			latestPolicy.Status.BindingManager = wantBindingManager.DeepCopy()
			Expect(hubClient.Status().Update(ctx, latestPolicy)).To(Succeed())
		})

		AfterAll(func() {
			policy := &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: policyName},
			}
			Expect(hubClient.Delete(ctx, policy)).To(Succeed())
		})

		It("should report a conflict when the given controller does not hold the role", func() {
			err := RelinquishRoleFor(ctx, hubClient, stalePolicy, "unknown-controller", objectRef)
			Expect(apierrors.IsConflict(err)).To(BeTrue(), "RelinquishRoleFor() = %v, want a conflict error", err)
		})

		It("should report a conflict when the object reference is not found", func() {
			err := RelinquishRoleFor(ctx, hubClient, stalePolicy, controllerName, unknownRef)
			Expect(apierrors.IsConflict(err)).To(BeTrue(), "RelinquishRoleFor() = %v, want a conflict error", err)
		})

		It("should leave the persisted binding manager claim untouched", func() {
			updated := &placementv1alpha1.ClusterPlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: policyName}, updated)).To(Succeed())
			Expect(cmp.Diff(updated.Status.BindingManager, wantBindingManager)).To(BeEmpty())
		})
	})
})

var _ = Describe("Relinquishing the binding manager role (PlacementPolicy)", func() {
	Context("when relinquishing an object reference", Ordered, func() {
		const (
			controllerName = "test-controller"
			policyName     = "ns-relinquish-object-ref"
		)

		refA := placementv1alpha1.ObjectReference{
			Namespace:  playgroundNamespace,
			Name:       "object-a",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}
		refB := placementv1alpha1.ObjectReference{
			Namespace:  playgroundNamespace,
			Name:       "object-b",
			APIGroup:   placementv1alpha1.GroupVersion.Group,
			APIVersion: placementv1alpha1.GroupVersion.Version,
			Kind:       "DummyOwner",
		}

		BeforeAll(func() {
			policy := &placementv1alpha1.PlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Namespace: playgroundNamespace, Name: policyName},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{APIVersion: "v1", Kind: "ConfigMap", Name: "test-config-map"},
					},
				},
			}
			Expect(hubClient.Create(ctx, policy)).To(Succeed())
			policy.Status.BindingManager = &placementv1alpha1.BindingManager{
				ControllerName: controllerName,
				ObjectRefs:     []placementv1alpha1.ObjectReference{refA, refB},
			}
			Expect(hubClient.Status().Update(ctx, policy)).To(Succeed())
		})

		AfterAll(func() {
			policy := &placementv1alpha1.PlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{Namespace: playgroundNamespace, Name: policyName},
			}
			Expect(hubClient.Delete(ctx, policy)).To(Succeed())
		})

		It("should remove the object reference while keeping the remaining ones", func() {
			policy := &placementv1alpha1.PlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: playgroundNamespace, Name: policyName}, policy)).To(Succeed())

			err := RelinquishRoleFor(ctx, hubClient, policy, controllerName, refA)
			Expect(err).ToNot(HaveOccurred())

			updated := &placementv1alpha1.PlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: playgroundNamespace, Name: policyName}, updated)).To(Succeed())
			want := &placementv1alpha1.BindingManager{
				ControllerName: controllerName,
				ObjectRefs:     []placementv1alpha1.ObjectReference{refB},
			}
			Expect(cmp.Diff(updated.Status.BindingManager, want)).To(BeEmpty())
		})

		It("should remove the binding manager claim when the last object reference is relinquished", func() {
			policy := &placementv1alpha1.PlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: playgroundNamespace, Name: policyName}, policy)).To(Succeed())

			err := RelinquishRoleFor(ctx, hubClient, policy, controllerName, refB)
			Expect(err).ToNot(HaveOccurred())

			updated := &placementv1alpha1.PlacementPolicy{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: playgroundNamespace, Name: policyName}, updated)).To(Succeed())
			Expect(updated.Status.BindingManager).To(BeNil())
		})
	})
})
