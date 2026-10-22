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

package e2e

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// The event reason the hub agent records on a resource whose annotation cannot be parsed. It is
// spelled out here rather than imported so that a rename in the controller shows up as a failed
// spec, the same way it would show up for a user filtering events by reason.
const invalidClusterSelectorsAnnotationEventReason = "InvalidClusterSelectorsAnnotation"

// generatedPolicyLabels returns the provenance labels the hub agent stamps on a policy it generates
// for the given resource; listing on them is how a user, and these specs, find the policy without
// knowing the generated name.
func generatedPolicyLabels(apiGroup, kind, name string) client.MatchingLabels {
	return client.MatchingLabels{
		kfplacementv1alpha1.ParentAPIGroupLabel: apiGroup,
		kfplacementv1alpha1.ParentKindLabel:     kind,
		kfplacementv1alpha1.ParentNameLabel:     name,
	}
}

// setClusterSelectorsAnnotation sets, changes, or (with an empty value) removes the cluster
// selectors annotation on a hub resource, retrying on a conflict with the hub agent's own writes.
// Removing the annotation from a resource that is already gone counts as done, so that cleanup
// after a failed spec does not wait out the timeout on it.
func setClusterSelectorsAnnotation(object client.Object, value string) {
	Eventually(func() error {
		if err := hubClient.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
			if value == "" && apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		annotations := object.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		if value == "" {
			delete(annotations, kfplacementv1alpha1.ClusterSelectorsAnnotation)
		} else {
			annotations[kfplacementv1alpha1.ClusterSelectorsAnnotation] = value
		}
		object.SetAnnotations(annotations)
		return hubClient.Update(ctx, object)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to set the cluster selectors annotation on %s", object.GetName())
}

// annotationClusterSelector builds the cluster selector the hub agent generates from one segment of
// the annotation, with the fields the API defaults filled in as the API server stores them.
func annotationClusterSelector(matchLabels map[string]string, count intstr.IntOrString) kfplacementv1alpha1.ClusterSelector {
	selector := kfplacementv1alpha1.ClusterSelector{
		Count:           ptr.To(count),
		WhenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim,
	}
	if len(matchLabels) > 0 {
		selector.Terms = []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{MatchLabels: matchLabels}}
	}
	return selector
}

// The FEP-0001 annotation-based placement experience is alpha and runs behind the
// enable-placement-policy-apis hub agent flag, which test/e2e/setup.sh turns on. These specs
// drive the feature the way a user does, through the annotation alone, and check the whole path
// that the controller's own envtest suite cannot: the resource watcher noticing the annotation, the
// generated policy watch noticing edits to the policy, and the event recorded on the resource.
var _ = Describe("annotation based placement", Ordered, func() {
	var (
		configMap       corev1.ConfigMap
		deployment      appsv1.Deployment
		namespace       corev1.Namespace
		configMapLabels client.MatchingLabels
	)

	BeforeAll(func() {
		By("creating the work resources")
		createWorkResources()
		configMap = appConfigMap()
		namespace = appNamespace()
		configMapLabels = generatedPolicyLabels("", "ConfigMap", configMap.Name)
	})

	AfterAll(func() {
		// A wait below that fails aborts the rest of this node; the work resources must go
		// regardless, or they leak into every spec that follows in this process.
		defer cleanupWorkResourcesOnHubAndMembers()

		// The annotations come off first so that the hub agent withdraws the generated policies
		// itself, rather than leaving them to the garbage collector once the namespace goes; a
		// policy that outlived its annotation would be the feature failing, not cleanup succeeding.
		// Each object is un-annotated only if the node that created it got as far as naming it;
		// otherwise the call would wait out its timeout on an empty name.
		if configMap.Name != "" {
			setClusterSelectorsAnnotation(&configMap, "")
		}
		if deployment.Name != "" {
			setClusterSelectorsAnnotation(&deployment, "")
		}
		if namespace.Name != "" {
			setClusterSelectorsAnnotation(&namespace, "")
		}
		Eventually(func() error {
			policies := &kfplacementv1alpha1.ClusterPlacementPolicyList{}
			if err := hubClient.List(ctx, policies, generatedPolicyLabels("", "Namespace", namespace.Name)); err != nil {
				return err
			}
			if len(policies.Items) != 0 {
				return fmt.Errorf("%d cluster placement policies generated for namespace %s are still present", len(policies.Items), namespace.Name)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to clean up the generated cluster placement policy")

		// The Deployment spec placed real pods; wait for them to leave so the namespace deletes
		// that follow do not race pod termination.
		var placed []client.Object
		if deployment.Name != "" {
			placed = append(placed, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deployment.Name}})
		}
		for _, c := range allMemberClusters {
			Eventually(namespacedResourcesRemovedFromClusterActual(c, placed...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the placed resources from %s", c.ClusterName)
		}
	})

	// generatedPolicy reads back the one policy generated for the ConfigMap, failing the caller's
	// Eventually until exactly one exists.
	generatedPolicy := func() (*kfplacementv1alpha1.PlacementPolicy, error) {
		policies := &kfplacementv1alpha1.PlacementPolicyList{}
		if err := hubClient.List(ctx, policies, client.InNamespace(configMap.Namespace), configMapLabels); err != nil {
			return nil, err
		}
		if len(policies.Items) != 1 {
			return nil, fmt.Errorf("%d placement policies generated for the config map, want 1", len(policies.Items))
		}
		return &policies.Items[0], nil
	}

	noGeneratedPolicyActual := func() error {
		policies := &kfplacementv1alpha1.PlacementPolicyList{}
		if err := hubClient.List(ctx, policies, client.InNamespace(configMap.Namespace), configMapLabels); err != nil {
			return err
		}
		if len(policies.Items) != 0 {
			return fmt.Errorf("%d placement policies generated for the config map, want none", len(policies.Items))
		}
		return nil
	}

	generatedPolicySpecActual := func(wantSelectors ...kfplacementv1alpha1.ClusterSelector) func() error {
		wantSpec := kfplacementv1alpha1.PlacementPolicySpec{
			ClusterSelectors: wantSelectors,
			ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       configMap.Name,
			}},
			ResourceRevisionHistoryLimit: ptr.To(int32(3)),
		}
		return func() error {
			policy, err := generatedPolicy()
			if err != nil {
				return err
			}
			if diff := cmp.Diff(policy.Spec, wantSpec); diff != "" {
				return fmt.Errorf("generated placement policy spec diff (-got, +want):\n%s", diff)
			}
			return nil
		}
	}

	It("should seed the cluster alias label on every member cluster", func() {
		// The alias= shorthand of the annotation matches on this label; the member cluster
		// reconciler seeds it from the cluster name, so that the shorthand works out of the box.
		//
		// The label is written by the member cluster reconciler, not at join time, so it is
		// awaited rather than read once.
		for _, name := range allMemberClusterNames {
			Eventually(func() error {
				mc := &clusterv1beta1.MemberCluster{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, mc); err != nil {
					return err
				}
				if got := mc.Labels[placementv1beta1.ClusterAliasLabel]; got != name {
					return fmt.Errorf("member cluster %s has alias label %q, want %q", name, got, name)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Member cluster %s does not carry its alias label", name)
		}
	})

	It("should generate no policy for a resource that carries no annotation", func() {
		Consistently(noGeneratedPolicyActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "A policy was generated for a resource without the annotation")
	})

	It("should generate a placement policy when the annotation is set", func() {
		setClusterSelectorsAnnotation(&configMap, fmt.Sprintf("%s=%s,count=All", envLabelName, envCanary))

		wantSelector := annotationClusterSelector(map[string]string{envLabelName: envCanary}, intstr.FromString("All"))
		Eventually(generatedPolicySpecActual(wantSelector), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the placement policy")

		policy, err := generatedPolicy()
		Expect(err).Should(Succeed())
		Expect(policy.Namespace).Should(Equal(configMap.Namespace), "A namespaced resource must generate a policy in its own namespace")

		Expect(hubClient.Get(ctx, client.ObjectKeyFromObject(&configMap), &configMap)).Should(Succeed())
		wantOwnerReferences := []metav1.OwnerReference{{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       configMap.Name,
			UID:        configMap.UID,
		}}
		diff := cmp.Diff(policy.OwnerReferences, wantOwnerReferences)
		Expect(diff).To(BeEmpty(), "generated placement policy owner references diff (-got, +want):\n%s", diff)

		By("checking that the ConfigMap is applied on the canary cluster only")
		Eventually(appConfigMapPlacedOnClustersActual(memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
			Should(Succeed(), "Failed to apply the ConfigMap on the canary cluster")
		Consistently(appConfigMapRemovedFromClustersActual(memberCluster1EastProd, memberCluster3WestProd), consistentlyDuration, consistentlyInterval).
			Should(Succeed(), "The ConfigMap was applied on a cluster that was not selected")
	})

	It("should update the policy when the annotation changes", func() {
		// The region shorthand expands to the well-known topology label rather than being matched
		// literally. No e2e member carries that label, so the policy stays unscheduled; these specs
		// check the generated spec only.
		setClusterSelectorsAnnotation(&configMap, fmt.Sprintf("region=%s,count=2", regionEast))

		wantSelector := annotationClusterSelector(map[string]string{corev1.LabelTopologyRegion: regionEast}, intstr.FromInt32(2))
		Eventually(generatedPolicySpecActual(wantSelector), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the placement policy")

		By("checking that the ConfigMap is removed from the canary cluster, which no longer matches")
		Eventually(appConfigMapRemovedFromClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
			Should(Succeed(), "Failed to remove the ConfigMap from the cluster that no longer matches")
	})

	It("should restore the policy when it is edited", func() {
		// Retried: the placement policy controllers write the policy too.
		Eventually(func() error {
			policy, err := generatedPolicy()
			if err != nil {
				return err
			}
			policy.Spec.ClusterSelectors = nil
			return hubClient.Update(ctx, policy)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to edit the generated placement policy")

		// Nothing happened to the ConfigMap; only the watch on generated policies can bring the
		// hub agent back to this resource.
		wantSelector := annotationClusterSelector(map[string]string{corev1.LabelTopologyRegion: regionEast}, intstr.FromInt32(2))
		Eventually(generatedPolicySpecActual(wantSelector), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to restore the edited placement policy")
	})

	It("should recreate the policy when it is deleted", func() {
		policy, err := generatedPolicy()
		Expect(err).Should(Succeed())
		deletedUID := policy.UID
		Expect(hubClient.Delete(ctx, policy)).Should(Succeed(), "Failed to delete the generated placement policy")

		Eventually(func() error {
			policy, err := generatedPolicy()
			if err != nil {
				return err
			}
			if policy.UID == deletedUID {
				return fmt.Errorf("the deleted placement policy is still present")
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to recreate the deleted placement policy")
		wantSelector := annotationClusterSelector(map[string]string{corev1.LabelTopologyRegion: regionEast}, intstr.FromInt32(2))
		Eventually(generatedPolicySpecActual(wantSelector), eventuallyDuration, eventuallyInterval).Should(Succeed(), "The recreated placement policy does not match the annotation")
	})

	It("should keep the policy and record an event when the annotation becomes invalid", func() {
		setClusterSelectorsAnnotation(&configMap, envLabelName)

		Eventually(func() error {
			events := &corev1.EventList{}
			if err := hubClient.List(ctx, events, client.InNamespace(configMap.Namespace), client.MatchingFields{
				"involvedObject.name": configMap.Name,
				"reason":              invalidClusterSelectorsAnnotationEventReason,
			}); err != nil {
				return err
			}
			if len(events.Items) == 0 {
				return fmt.Errorf("no %s event recorded on the config map", invalidClusterSelectorsAnnotationEventReason)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to record the invalid annotation event")

		wantSelector := annotationClusterSelector(map[string]string{corev1.LabelTopologyRegion: regionEast}, intstr.FromInt32(2))
		Consistently(generatedPolicySpecActual(wantSelector), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The policy from the last valid annotation was not left in place")
	})

	It("should delete the policy when the annotation is removed", func() {
		setClusterSelectorsAnnotation(&configMap, "")
		Eventually(noGeneratedPolicyActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete the placement policy")
	})

	It("should carry the API group of a non-core resource into the generated policy", func() {
		// The ConfigMap above sits in the core group, whose empty name would also match a policy
		// that forgot the group altogether; a Deployment pins the field to a real value.
		deployment = appDeployment()
		Expect(hubClient.Create(ctx, &deployment)).Should(Succeed(), "Failed to create the deployment")
		setClusterSelectorsAnnotation(&deployment, fmt.Sprintf("%s=%s", envLabelName, envProd))

		deploymentLabels := generatedPolicyLabels("apps", "Deployment", deployment.Name)
		wantSpec := kfplacementv1alpha1.PlacementPolicySpec{
			ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
				annotationClusterSelector(map[string]string{envLabelName: envProd}, intstr.FromInt32(1)),
			},
			ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{{
				APIGroup:   "apps",
				APIVersion: "v1",
				Kind:       "Deployment",
				Name:       deployment.Name,
			}},
			ResourceRevisionHistoryLimit: ptr.To(int32(3)),
		}
		Eventually(func() error {
			policies := &kfplacementv1alpha1.PlacementPolicyList{}
			if err := hubClient.List(ctx, policies, client.InNamespace(deployment.Namespace), deploymentLabels); err != nil {
				return err
			}
			if len(policies.Items) != 1 {
				return fmt.Errorf("%d placement policies generated for the deployment, want 1", len(policies.Items))
			}
			if diff := cmp.Diff(policies.Items[0].Spec, wantSpec); diff != "" {
				return fmt.Errorf("generated placement policy spec diff (-got, +want):\n%s", diff)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the placement policy for the deployment")
	})

	It("should generate a cluster placement policy for a cluster scoped resource", func() {
		// The alias shorthand expands to the cluster alias label the hub agent seeds above.
		setClusterSelectorsAnnotation(&namespace, fmt.Sprintf("alias=%s", memberCluster1EastProdName))

		wantSpec := kfplacementv1alpha1.PlacementPolicySpec{
			ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
				annotationClusterSelector(map[string]string{placementv1beta1.ClusterAliasLabel: memberCluster1EastProdName}, intstr.FromInt32(1)),
			},
			ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{{
				APIVersion: "v1",
				Kind:       "Namespace",
				Name:       namespace.Name,
			}},
			ResourceRevisionHistoryLimit: ptr.To(int32(3)),
		}
		Eventually(func() error {
			policies := &kfplacementv1alpha1.ClusterPlacementPolicyList{}
			if err := hubClient.List(ctx, policies, generatedPolicyLabels("", "Namespace", namespace.Name)); err != nil {
				return err
			}
			if len(policies.Items) != 1 {
				return fmt.Errorf("%d cluster placement policies generated for the namespace, want 1", len(policies.Items))
			}
			if diff := cmp.Diff(policies.Items[0].Spec, wantSpec); diff != "" {
				return fmt.Errorf("generated cluster placement policy spec diff (-got, +want):\n%s", diff)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the cluster placement policy")
	})
})
