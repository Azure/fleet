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

package autorollout

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

// The number of placement bindings that each placement policy in the lifecycle test cases has.
const lifecycleBindingCount = 3

// The binding manager claim details of another (fictional) controller.
const (
	otherBindingManagerControllerName = "other-controller"
	otherBindingManagerAPIGroup       = "example.kubefleet.dev"
	otherBindingManagerAPIVersion     = "v1"
	otherBindingManagerKind           = "OtherRollout"
	otherBindingManagerNameTemplate   = "other-rollout-%s"
)

// otherBindingManager returns the binding manager claim of another (fictional) controller on a placement policy.
func otherBindingManager(randStr string) *placementv1alpha1.BindingManager {
	return &placementv1alpha1.BindingManager{
		ControllerName: otherBindingManagerControllerName,
		ObjectRefs: []placementv1alpha1.ObjectReference{
			{
				APIGroup:   otherBindingManagerAPIGroup,
				APIVersion: otherBindingManagerAPIVersion,
				Kind:       otherBindingManagerKind,
				Name:       fmt.Sprintf(otherBindingManagerNameTemplate, randStr),
			},
		},
	}
}

// markPlacementPolicyAsPendingRolloutWithAutoRolloutClaim marks a placement policy as having resources pending rollout
// (with the conditions that pendingRolloutConds returns for ResourcesChangedPendingRollout) and leaves a binding
// manager claim of the auto-rollout controller on it, in one status write; this simulates a claim that the
// controller has left behind, e.g., after a restart in the middle of a rollout.
//
// A single write makes sure that the controller never observes one change without the other.
func markPlacementPolicyAsPendingRolloutWithAutoRolloutClaim(policyNSName, policyName string) {
	updatePlacementPolicyStatus(policyNSName, policyName, func(policy placementv1alpha1.PlacementPolicyAccessor) {
		setConditionsOnPlacementPolicy(policy, pendingRolloutConds(placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)...)
		policy.GetStatus().BindingManager = wantAutoRolloutBindingManager(policyNSName, policyName)
	})
}

// placementPolicySpecAndAnnotationsUntouchedActual verifies that a placement policy is still at its first generation
// (i.e., its spec has not been changed) and that, apart from the last rollout attempt timestamp, its annotations are
// exactly the given ones.
func placementPolicySpecAndAnnotationsUntouchedActual(policyNSName, policyName string, wantAnnotations map[string]string) func() error {
	return func() error {
		policy, err := getPlacementPolicy(policyNSName, policyName)
		if err != nil {
			return err
		}
		if gotGeneration := policy.GetGeneration(); gotGeneration != 1 {
			return fmt.Errorf("the placement policy is at generation %d, want 1", gotGeneration)
		}
		gotAnnotations := make(map[string]string, len(policy.GetAnnotations()))
		for k, v := range policy.GetAnnotations() {
			if k != rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey {
				gotAnnotations[k] = v
			}
		}
		if diff := cmp.Diff(gotAnnotations, wantAnnotations); diff != "" {
			return fmt.Errorf("placement policy annotations (other than the last rollout attempt timestamp) diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

var _ = Describe("gating auto-rollouts of placement policies", func() {
	Context("auto-rollouts of placement policies with resources pending rollout", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotNames := []string{
			primaryPlacementResourceSnapshotName(policyName, 0),
			primaryPlacementResourceSnapshotName(policyName, 1),
		}

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time
		var wantLastRolloutAttemptTimestamp string

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources to all the placement bindings", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		It("should keep the spec and the other annotations of the placement policy as they are", func() {
			untouched := placementPolicySpecAndAnnotationsUntouchedActual(appNamespaceName, policyName, autoRolloutEnabledAnnotations())
			Expect(untouched()).To(Succeed(), "Changed the placement policy unexpectedly")
		})

		It("can mark the placement policy as synchronized", func() {
			wantLastRolloutAttemptTimestamp = lastRolloutAttemptTimestamp(appNamespaceName, policyName)
			markPlacementPolicyAsSynchronized(appNamespaceName, policyName)
			forcePlacementPolicyReconciliation(appNamespaceName, policyName)
		})

		It("should not roll out the placement policy again", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotNames[1]),
				placementBindingsActual(appNamespaceName, bindingNames, snapshotNames[0], 2),
				placementPolicyAtRestActual(appNamespaceName, policyName, wantLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotNames[0]),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotNames[1]),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})

	Context("placement policies with auto-rollout disabled", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time

		noRolloutActual := func() func() error {
			return allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotName),
				placementBindingsActual(appNamespaceName, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(appNamespaceName, policyName, ""),
			)
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			// The placement policy has no auto-rollout enabled annotation.
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, nil)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the placement policy", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can set the auto-rollout enabled annotation to a value other than the enabling one", func() {
			updatePlacementPolicyAnnotations(appNamespaceName, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutEnabledAnnotationKey] = "True"
			})
		})

		It("should still not roll out the placement policy", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can enable auto-rollout for the placement policy", func() {
			notBefore = time.Now()
			updatePlacementPolicyAnnotations(appNamespaceName, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutEnabledAnnotationKey] = rolloutv1alpha1.AutoRolloutEnabledAnnotationValue
			})
		})

		It("should roll out the latest resources to all the placement bindings", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})

	Context("placement policies with auto-rollout disabled after a rollout", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotNames := []string{
			primaryPlacementResourceSnapshotName(policyName, 0),
			primaryPlacementResourceSnapshotName(policyName, 1),
		}

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time
		// A last rollout attempt timestamp long enough ago that the minimum wait period never holds a rollout back.
		backdatedLastRolloutAttemptTimestamp := backdatedTimestamp(time.Hour * 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources to all the placement bindings", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		It("can mark the placement policy as synchronized", func() {
			markPlacementPolicyAsSynchronized(appNamespaceName, policyName)
		})

		It("can disable auto-rollout for the placement policy", func() {
			updatePlacementPolicyAnnotations(appNamespaceName, policyName, func(annotations map[string]string) {
				delete(annotations, rolloutv1alpha1.AutoRolloutEnabledAnnotationKey)
			})
		})

		It("can change the selected resources and backdate the last rollout attempt timestamp", func() {
			updateConfigMapData(configMapName, map[string]string{configMapDataKey: altConfigMapDataValue})
			updatePlacementPolicyAnnotations(appNamespaceName, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = backdatedLastRolloutAttemptTimestamp
			})
		})

		It("can mark the placement policy as having resources pending rollout again", func() {
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the placement policy again", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotNames[1]),
				placementBindingsActual(appNamespaceName, bindingNames, snapshotNames[0], 2),
				placementPolicyAtRestActual(appNamespaceName, policyName, backdatedLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotNames[0]),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotNames[1]),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})

	Context("placement policy conditions that do or do not call for a rollout", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time

		noRolloutActual := func() func() error {
			return allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotName),
				placementBindingsActual(appNamespaceName, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(appNamespaceName, policyName, ""),
			)
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can force a reconciliation of the placement policy with no conditions", func() {
			forcePlacementPolicyReconciliation(appNamespaceName, policyName)
		})

		It("should not roll out the placement policy with no conditions", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can mark the placement policy as having failed to collect some resources", func() {
			setPlacementPolicyConditions(appNamespaceName, policyName,
				resourceCollectedCond(metav1.ConditionFalse, placementv1alpha1.PlacementPolicyResourceCollectedCondReasonFailedToCollectSomeResources),
				synchronizedCond(metav1.ConditionFalse, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout),
			)
			forcePlacementPolicyReconciliation(appNamespaceName, policyName)
		})

		It("should not roll out the placement policy with resources not collected", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can mark the placement policy as synchronized", func() {
			markPlacementPolicyAsSynchronized(appNamespaceName, policyName)
			forcePlacementPolicyReconciliation(appNamespaceName, policyName)
		})

		It("should not roll out the placement policy with resources synchronized", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can mark the placement policy as having failed to synchronize resources on some clusters", func() {
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonFailedToSynchronizeSomeClusters)
			forcePlacementPolicyReconciliation(appNamespaceName, policyName)
		})

		It("should not roll out the placement policy with resources failed to synchronize", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can mark the placement policy as having resources pending rollout at a stale generation", func() {
			// Bump the generation first; writing the conditions at the current generation first would call for a
			// rollout right away.
			generation := bumpPlacementPolicyGeneration(appNamespaceName, policyName)
			staleResourceCollectedCond := resourceCollectedCond(metav1.ConditionTrue, placementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected)
			staleResourceCollectedCond.ObservedGeneration = generation - 1
			staleSynchronizedCond := synchronizedCond(metav1.ConditionFalse, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
			staleSynchronizedCond.ObservedGeneration = generation - 1
			setPlacementPolicyConditions(appNamespaceName, policyName, staleResourceCollectedCond, staleSynchronizedCond)
			forcePlacementPolicyReconciliation(appNamespaceName, policyName)
		})

		It("should not roll out the placement policy with stale conditions", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can mark the placement policy as having some clusters on an older resource revision", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonSomeClustersOnOlderResourceRevision)
		})

		It("should roll out the latest resources to all the placement bindings", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})

	Context("placement policies marked for deletion", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var bindingNames []string

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy for deletion", func() {
			markObjectForDeletion(newPlacementPolicyObj(appNamespaceName, policyName))
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the placement policy", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotName),
				placementBindingsActual(appNamespaceName, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(appNamespaceName, policyName, ""),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can leave a binding manager claim of the auto-rollout controller on the placement policy", func() {
			updatePlacementPolicyBindingManager(appNamespaceName, policyName, wantAutoRolloutBindingManager(appNamespaceName, policyName))
		})

		It("should relinquish the binding manager role", func() {
			released := bindingManagerReleasedOnAPIServerActual(appNamespaceName, policyName)
			Eventually(released, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to relinquish the binding manager role")
		})

		It("can remove the test finalizer from the placement policy", func() {
			policy := newPlacementPolicyObj(appNamespaceName, policyName)
			removeTestFinalizer(policy)
			Eventually(objectRemovedActual(policy), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement policy")
		})

		AfterAll(func() {
			removeTestFinalizer(newPlacementPolicyObj(appNamespaceName, policyName))
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})
})

var _ = Describe("gating auto-rollouts of cluster placement policies", func() {
	Context("auto-rollouts of cluster placement policies with resources pending rollout", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotNames := []string{
			primaryPlacementResourceSnapshotName(policyName, 0),
			primaryPlacementResourceSnapshotName(policyName, 1),
		}

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time
		var wantLastRolloutAttemptTimestamp string

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources to all the cluster placement bindings", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		It("should keep the spec and the other annotations of the cluster placement policy as they are", func() {
			untouched := placementPolicySpecAndAnnotationsUntouchedActual(clusterScopedNS, policyName, autoRolloutEnabledAnnotations())
			Expect(untouched()).To(Succeed(), "Changed the cluster placement policy unexpectedly")
		})

		It("can mark the cluster placement policy as synchronized", func() {
			wantLastRolloutAttemptTimestamp = lastRolloutAttemptTimestamp(clusterScopedNS, policyName)
			markPlacementPolicyAsSynchronized(clusterScopedNS, policyName)
			forcePlacementPolicyReconciliation(clusterScopedNS, policyName)
		})

		It("should not roll out the cluster placement policy again", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotNames[1]),
				placementBindingsActual(clusterScopedNS, bindingNames, snapshotNames[0], 2),
				placementPolicyAtRestActual(clusterScopedNS, policyName, wantLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotNames[0]),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotNames[1]),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})

	Context("cluster placement policies with auto-rollout disabled", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time

		noRolloutActual := func() func() error {
			return allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotName),
				placementBindingsActual(clusterScopedNS, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(clusterScopedNS, policyName, ""),
			)
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			// The cluster placement policy has no auto-rollout enabled annotation.
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, nil)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the cluster placement policy", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can set the auto-rollout enabled annotation to a value other than the enabling one", func() {
			updatePlacementPolicyAnnotations(clusterScopedNS, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutEnabledAnnotationKey] = "True"
			})
		})

		It("should still not roll out the cluster placement policy", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can enable auto-rollout for the cluster placement policy", func() {
			notBefore = time.Now()
			updatePlacementPolicyAnnotations(clusterScopedNS, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutEnabledAnnotationKey] = rolloutv1alpha1.AutoRolloutEnabledAnnotationValue
			})
		})

		It("should roll out the latest resources to all the cluster placement bindings", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})

	Context("cluster placement policies with auto-rollout disabled after a rollout", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotNames := []string{
			primaryPlacementResourceSnapshotName(policyName, 0),
			primaryPlacementResourceSnapshotName(policyName, 1),
		}

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time
		// A last rollout attempt timestamp long enough ago that the minimum wait period never holds a rollout back.
		backdatedLastRolloutAttemptTimestamp := backdatedTimestamp(time.Hour * 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources to all the cluster placement bindings", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		It("can mark the cluster placement policy as synchronized", func() {
			markPlacementPolicyAsSynchronized(clusterScopedNS, policyName)
		})

		It("can disable auto-rollout for the cluster placement policy", func() {
			updatePlacementPolicyAnnotations(clusterScopedNS, policyName, func(annotations map[string]string) {
				delete(annotations, rolloutv1alpha1.AutoRolloutEnabledAnnotationKey)
			})
		})

		It("can change the selected resources and backdate the last rollout attempt timestamp", func() {
			updateConfigMapData(configMapName, map[string]string{configMapDataKey: altConfigMapDataValue})
			updatePlacementPolicyAnnotations(clusterScopedNS, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = backdatedLastRolloutAttemptTimestamp
			})
		})

		It("can mark the cluster placement policy as having resources pending rollout again", func() {
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the cluster placement policy again", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotNames[1]),
				placementBindingsActual(clusterScopedNS, bindingNames, snapshotNames[0], 2),
				placementPolicyAtRestActual(clusterScopedNS, policyName, backdatedLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotNames[0]),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotNames[1]),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})

	Context("cluster placement policy conditions that do or do not call for a rollout", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time

		noRolloutActual := func() func() error {
			return allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotName),
				placementBindingsActual(clusterScopedNS, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(clusterScopedNS, policyName, ""),
			)
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can force a reconciliation of the cluster placement policy with no conditions", func() {
			forcePlacementPolicyReconciliation(clusterScopedNS, policyName)
		})

		It("should not roll out the cluster placement policy with no conditions", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can mark the cluster placement policy as having failed to collect some resources", func() {
			setPlacementPolicyConditions(clusterScopedNS, policyName,
				resourceCollectedCond(metav1.ConditionFalse, placementv1alpha1.PlacementPolicyResourceCollectedCondReasonFailedToCollectSomeResources),
				synchronizedCond(metav1.ConditionFalse, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout),
			)
			forcePlacementPolicyReconciliation(clusterScopedNS, policyName)
		})

		It("should not roll out the cluster placement policy with resources not collected", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can mark the cluster placement policy as synchronized", func() {
			markPlacementPolicyAsSynchronized(clusterScopedNS, policyName)
			forcePlacementPolicyReconciliation(clusterScopedNS, policyName)
		})

		It("should not roll out the cluster placement policy with resources synchronized", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can mark the cluster placement policy as having failed to synchronize resources on some clusters", func() {
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonFailedToSynchronizeSomeClusters)
			forcePlacementPolicyReconciliation(clusterScopedNS, policyName)
		})

		It("should not roll out the cluster placement policy with resources failed to synchronize", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can mark the cluster placement policy as having resources pending rollout at a stale generation", func() {
			// Bump the generation first; writing the conditions at the current generation first would call for a
			// rollout right away.
			generation := bumpPlacementPolicyGeneration(clusterScopedNS, policyName)
			staleResourceCollectedCond := resourceCollectedCond(metav1.ConditionTrue, placementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected)
			staleResourceCollectedCond.ObservedGeneration = generation - 1
			staleSynchronizedCond := synchronizedCond(metav1.ConditionFalse, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
			staleSynchronizedCond.ObservedGeneration = generation - 1
			setPlacementPolicyConditions(clusterScopedNS, policyName, staleResourceCollectedCond, staleSynchronizedCond)
			forcePlacementPolicyReconciliation(clusterScopedNS, policyName)
		})

		It("should not roll out the cluster placement policy with stale conditions", func() {
			Consistently(noRolloutActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can mark the cluster placement policy as having some clusters on an older resource revision", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonSomeClustersOnOlderResourceRevision)
		})

		It("should roll out the latest resources to all the cluster placement bindings", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})

	Context("cluster placement policies marked for deletion", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var bindingNames []string

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy for deletion", func() {
			markObjectForDeletion(newPlacementPolicyObj(clusterScopedNS, policyName))
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the cluster placement policy", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotName),
				placementBindingsActual(clusterScopedNS, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(clusterScopedNS, policyName, ""),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can leave a binding manager claim of the auto-rollout controller on the cluster placement policy", func() {
			updatePlacementPolicyBindingManager(clusterScopedNS, policyName, wantAutoRolloutBindingManager(clusterScopedNS, policyName))
		})

		It("should relinquish the binding manager role", func() {
			released := bindingManagerReleasedOnAPIServerActual(clusterScopedNS, policyName)
			Eventually(released, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to relinquish the binding manager role")
		})

		It("can remove the test finalizer from the cluster placement policy", func() {
			policy := newPlacementPolicyObj(clusterScopedNS, policyName)
			removeTestFinalizer(policy)
			Eventually(objectRemovedActual(policy), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement policy")
		})

		AfterAll(func() {
			removeTestFinalizer(newPlacementPolicyObj(clusterScopedNS, policyName))
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})
})

var _ = Describe("managing the binding manager role of placement policies", func() {
	Context("placement policies whose binding manager role another controller holds", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)
		otherClaim := otherBindingManager(randStr)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			updatePlacementPolicyBindingManager(appNamespaceName, policyName, otherClaim)
			// Wait until the cache (which the controller shares) observes the claim of the other controller, so that the
			// controller never sees the role as free.
			otherClaimObserved := placementPolicyAutoRolloutStateActual(appNamespaceName, policyName, otherClaim, "")
			Eventually(otherClaimObserved, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the claim of the other controller")
			waitForObjectsToBeCached(placementBindingObjs(appNamespaceName, bindingNames)...)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the placement policy while another controller holds the binding manager role", func() {
			// The controller keeps retrying the claim (with backoff); in the meantime, the claim of the other
			// controller stays, no timestamp is written (the controller writes it only after a rollout), and nothing
			// is rolled out.
			noRollout := allActual(
				placementPolicyAutoRolloutStateActual(appNamespaceName, policyName, otherClaim, ""),
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotName),
				placementBindingsActual(appNamespaceName, bindingNames, previousResourceSnapshotName, 1),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can release the binding manager role held by the other controller", func() {
			notBefore = time.Now()
			updatePlacementPolicyBindingManager(appNamespaceName, policyName, nil)
		})

		It("should roll out the latest resources to all the placement bindings", func() {
			// The release does not make the controller process the placement policy; the next claim retry does.
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})

	Context("placement policies with auto-rollout disabled and a leftover claim of the auto-rollout controller", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var bindingNames []string

		BeforeAll(func() {
			createConfigMap(configMapName)
			// The placement policy has no auto-rollout enabled annotation.
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName, nil)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy as having resources pending rollout, with a claim of the auto-rollout controller left on it", func() {
			markPlacementPolicyAsPendingRolloutWithAutoRolloutClaim(appNamespaceName, policyName)
		})

		It("should relinquish the binding manager role", func() {
			released := bindingManagerReleasedOnAPIServerActual(appNamespaceName, policyName)
			Eventually(released, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to relinquish the binding manager role")
		})

		It("should not roll out the placement policy", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotName),
				placementBindingsActual(appNamespaceName, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(appNamespaceName, policyName, ""),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})

	Context("placement policies within the minimum wait period and a leftover claim of the auto-rollout controller", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var bindingNames []string
		var recentLastRolloutAttemptTimestamp string

		BeforeAll(func() {
			createConfigMap(configMapName)
			// The placement policy was rolled out just now; the (default) minimum wait period holds the next
			// rollout back for longer than the test case runs.
			recentLastRolloutAttemptTimestamp = backdatedTimestamp(0)
			annotations := autoRolloutEnabledAnnotations()
			annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = recentLastRolloutAttemptTimestamp
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName, annotations)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy as having resources pending rollout, with a claim of the auto-rollout controller left on it", func() {
			markPlacementPolicyAsPendingRolloutWithAutoRolloutClaim(appNamespaceName, policyName)
		})

		It("should relinquish the binding manager role", func() {
			released := bindingManagerReleasedOnAPIServerActual(appNamespaceName, policyName)
			Eventually(released, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to relinquish the binding manager role")
		})

		It("should not roll out the placement policy within the minimum wait period", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotName),
				placementBindingsActual(appNamespaceName, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(appNamespaceName, policyName, recentLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})

	Context("placement policies with resources pending rollout and a leftover claim of the auto-rollout controller", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy as having resources pending rollout, with a claim of the auto-rollout controller left on it", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRolloutWithAutoRolloutClaim(appNamespaceName, policyName)
		})

		It("should roll out the latest resources to all the placement bindings, then relinquish the binding manager role", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})
})

var _ = Describe("managing the binding manager role of cluster placement policies", func() {
	Context("cluster placement policies whose binding manager role another controller holds", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)
		otherClaim := otherBindingManager(randStr)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			updatePlacementPolicyBindingManager(clusterScopedNS, policyName, otherClaim)
			// Wait until the cache (which the controller shares) observes the claim of the other controller, so that the
			// controller never sees the role as free.
			otherClaimObserved := placementPolicyAutoRolloutStateActual(clusterScopedNS, policyName, otherClaim, "")
			Eventually(otherClaimObserved, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the claim of the other controller")
			waitForObjectsToBeCached(placementBindingObjs(clusterScopedNS, bindingNames)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the cluster placement policy while another controller holds the binding manager role", func() {
			// The controller keeps retrying the claim (with backoff); in the meantime, the claim of the other
			// controller stays, no timestamp is written (the controller writes it only after a rollout), and nothing
			// is rolled out.
			noRollout := allActual(
				placementPolicyAutoRolloutStateActual(clusterScopedNS, policyName, otherClaim, ""),
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotName),
				placementBindingsActual(clusterScopedNS, bindingNames, previousResourceSnapshotName, 1),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can release the binding manager role held by the other controller", func() {
			notBefore = time.Now()
			updatePlacementPolicyBindingManager(clusterScopedNS, policyName, nil)
		})

		It("should roll out the latest resources to all the cluster placement bindings", func() {
			// The release does not make the controller process the cluster placement policy; the next claim retry
			// does.
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})

	Context("cluster placement policies with auto-rollout disabled and a leftover claim of the auto-rollout controller", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var bindingNames []string

		BeforeAll(func() {
			createConfigMap(configMapName)
			// The cluster placement policy has no auto-rollout enabled annotation.
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName, nil)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout, with a claim of the auto-rollout controller left on it", func() {
			markPlacementPolicyAsPendingRolloutWithAutoRolloutClaim(clusterScopedNS, policyName)
		})

		It("should relinquish the binding manager role", func() {
			released := bindingManagerReleasedOnAPIServerActual(clusterScopedNS, policyName)
			Eventually(released, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to relinquish the binding manager role")
		})

		It("should not roll out the cluster placement policy", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotName),
				placementBindingsActual(clusterScopedNS, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(clusterScopedNS, policyName, ""),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})

	Context("cluster placement policies within the minimum wait period and a leftover claim of the auto-rollout controller", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var bindingNames []string
		var recentLastRolloutAttemptTimestamp string

		BeforeAll(func() {
			createConfigMap(configMapName)
			// The cluster placement policy was rolled out just now; the (default) minimum wait period holds the next
			// rollout back for longer than the test case runs.
			recentLastRolloutAttemptTimestamp = backdatedTimestamp(0)
			annotations := autoRolloutEnabledAnnotations()
			annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = recentLastRolloutAttemptTimestamp
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName, annotations)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout, with a claim of the auto-rollout controller left on it", func() {
			markPlacementPolicyAsPendingRolloutWithAutoRolloutClaim(clusterScopedNS, policyName)
		})

		It("should relinquish the binding manager role", func() {
			released := bindingManagerReleasedOnAPIServerActual(clusterScopedNS, policyName)
			Eventually(released, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to relinquish the binding manager role")
		})

		It("should not roll out the cluster placement policy within the minimum wait period", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotName),
				placementBindingsActual(clusterScopedNS, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(clusterScopedNS, policyName, recentLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})

	Context("cluster placement policies with resources pending rollout and a leftover claim of the auto-rollout controller", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout, with a claim of the auto-rollout controller left on it", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRolloutWithAutoRolloutClaim(clusterScopedNS, policyName)
		})

		It("should roll out the latest resources to all the cluster placement bindings, then relinquish the binding manager role", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})
})

var _ = Describe("enforcing the minimum wait period between auto-rollouts of placement policies", func() {
	Context("placement policies rolled out within the minimum wait period", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var wantLastRolloutAttemptTimestamp string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			// The placement policy was rolled out two minutes ago; the default minimum wait period (3 minutes) holds
			// the next rollout back for about another minute.
			wantLastRolloutAttemptTimestamp = backdatedTimestamp(time.Minute * 2)
			annotations := autoRolloutEnabledAnnotations()
			annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = wantLastRolloutAttemptTimestamp
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, annotations)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the placement policy within the minimum wait period", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotName),
				placementBindingsActual(appNamespaceName, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(appNamespaceName, policyName, wantLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can shorten the minimum wait period of the placement policy", func() {
			notBefore = time.Now()
			updatePlacementPolicyAnnotations(appNamespaceName, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "1m"
			})
		})

		It("should roll out the latest resources to all the placement bindings right away", func() {
			// The controller would otherwise come back to the placement policy only once the default minimum wait
			// period has passed, i.e., in about a minute, which is longer than the check waits.
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})

	Context("placement policies waiting for the minimum wait period to pass", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var wantLastRolloutAttemptTimestamp string

		BeforeAll(func() {
			createConfigMap(configMapName)
			annotations := autoRolloutEnabledAnnotations()
			annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "1m"
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, annotations)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy as rolled out 45 seconds ago and as having resources pending rollout", func() {
			// Write the timestamp only now (rather than at creation), so that the setup does not eat into the
			// remaining 15 seconds of the minimum wait period; the timestamp change alone does not make the controller
			// process the placement policy.
			wantLastRolloutAttemptTimestamp = backdatedTimestamp(time.Second * 45)
			updatePlacementPolicyAnnotations(appNamespaceName, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = wantLastRolloutAttemptTimestamp
			})
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the placement policy before the minimum wait period passes", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotName),
				placementBindingsActual(appNamespaceName, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(appNamespaceName, policyName, wantLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("should roll out the latest resources to all the placement bindings once the minimum wait period passes, with no further changes", func() {
			lastRolloutAttempt, err := time.Parse(time.RFC3339, wantLastRolloutAttemptTimestamp)
			Expect(err).To(Succeed(), "Failed to parse the last rollout attempt timestamp")
			// The new rollout must not happen before the minimum wait period (1 minute) has passed since the last one.
			rolledOut := rolledOutActual(policy, bindingNames, 0, lastRolloutAttempt.Add(time.Minute))
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})

	Context("placement policies with minimum wait periods out of the allowed range", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var wantLastRolloutAttemptTimestamp string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			// A minimum wait period below the lower bound (1 minute), which the controller clamps up to the bound; the
			// placement policy was rolled out 10 seconds ago.
			wantLastRolloutAttemptTimestamp = backdatedTimestamp(time.Second * 10)
			annotations := autoRolloutEnabledAnnotations()
			annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "1s"
			annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = wantLastRolloutAttemptTimestamp
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, annotations)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the placement policy within the lower bound of the minimum wait period", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotName),
				placementBindingsActual(appNamespaceName, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(appNamespaceName, policyName, wantLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can set a minimum wait period above the upper bound and backdate the last rollout attempt timestamp", func() {
			// A minimum wait period above the upper bound (1 hour), which the controller clamps down to the bound; the
			// last rollout attempt of the placement policy was 2 hours ago. Both annotations change in one write.
			notBefore = time.Now()
			updatePlacementPolicyAnnotations(appNamespaceName, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "5h"
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = backdatedTimestamp(time.Hour * 2)
			})
		})

		It("should roll out the latest resources to all the placement bindings right away", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})

	Context("placement policies with malformed auto-rollout annotations", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var wantLastRolloutAttemptTimestamp string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			// A malformed minimum wait period, for which the controller uses the default (3 minutes); the placement
			// policy was rolled out two minutes ago, i.e., within the default period but not within the lower bound
			// (1 minute).
			wantLastRolloutAttemptTimestamp = backdatedTimestamp(time.Minute * 2)
			annotations := autoRolloutEnabledAnnotations()
			annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "soon"
			annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = wantLastRolloutAttemptTimestamp
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, annotations)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the placement policy within the default minimum wait period", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotName),
				placementBindingsActual(appNamespaceName, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(appNamespaceName, policyName, wantLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the placement policy unexpectedly")
		})

		It("can set a malformed last rollout attempt timestamp", func() {
			// The controller treats a placement policy with a malformed last rollout attempt timestamp as never rolled
			// out. A timestamp change alone does not make the controller process the placement policy; change the
			// (still malformed) minimum wait period in the same write as well.
			notBefore = time.Now()
			updatePlacementPolicyAnnotations(appNamespaceName, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = "not-a-timestamp"
				annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "later"
			})
		})

		It("should roll out the latest resources to all the placement bindings right away, replacing the malformed timestamp", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})
})

var _ = Describe("enforcing the minimum wait period between auto-rollouts of cluster placement policies", func() {
	Context("cluster placement policies rolled out within the minimum wait period", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var wantLastRolloutAttemptTimestamp string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			// The cluster placement policy was rolled out two minutes ago; the default minimum wait period (3 minutes)
			// holds the next rollout back for about another minute.
			wantLastRolloutAttemptTimestamp = backdatedTimestamp(time.Minute * 2)
			annotations := autoRolloutEnabledAnnotations()
			annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = wantLastRolloutAttemptTimestamp
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, annotations)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the cluster placement policy within the minimum wait period", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotName),
				placementBindingsActual(clusterScopedNS, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(clusterScopedNS, policyName, wantLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can shorten the minimum wait period of the cluster placement policy", func() {
			notBefore = time.Now()
			updatePlacementPolicyAnnotations(clusterScopedNS, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "1m"
			})
		})

		It("should roll out the latest resources to all the cluster placement bindings right away", func() {
			// The controller would otherwise come back to the cluster placement policy only once the default minimum
			// wait period has passed, i.e., in about a minute, which is longer than the check waits.
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})

	Context("cluster placement policies waiting for the minimum wait period to pass", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var wantLastRolloutAttemptTimestamp string

		BeforeAll(func() {
			createConfigMap(configMapName)
			annotations := autoRolloutEnabledAnnotations()
			annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "1m"
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, annotations)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy as rolled out 45 seconds ago and as having resources pending rollout", func() {
			// Write the timestamp only now (rather than at creation), so that the setup does not eat into the
			// remaining 15 seconds of the minimum wait period; the timestamp change alone does not make the controller
			// process the cluster placement policy.
			wantLastRolloutAttemptTimestamp = backdatedTimestamp(time.Second * 45)
			updatePlacementPolicyAnnotations(clusterScopedNS, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = wantLastRolloutAttemptTimestamp
			})
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the cluster placement policy before the minimum wait period passes", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotName),
				placementBindingsActual(clusterScopedNS, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(clusterScopedNS, policyName, wantLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("should roll out the latest resources to all the cluster placement bindings once the minimum wait period passes, with no further changes", func() {
			lastRolloutAttempt, err := time.Parse(time.RFC3339, wantLastRolloutAttemptTimestamp)
			Expect(err).To(Succeed(), "Failed to parse the last rollout attempt timestamp")
			// The new rollout must not happen before the minimum wait period (1 minute) has passed since the last one.
			rolledOut := rolledOutActual(policy, bindingNames, 0, lastRolloutAttempt.Add(time.Minute))
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})

	Context("cluster placement policies with minimum wait periods out of the allowed range", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var wantLastRolloutAttemptTimestamp string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			// A minimum wait period below the lower bound (1 minute), which the controller clamps up to the bound; the
			// cluster placement policy was rolled out 10 seconds ago.
			wantLastRolloutAttemptTimestamp = backdatedTimestamp(time.Second * 10)
			annotations := autoRolloutEnabledAnnotations()
			annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "1s"
			annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = wantLastRolloutAttemptTimestamp
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, annotations)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the cluster placement policy within the lower bound of the minimum wait period", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotName),
				placementBindingsActual(clusterScopedNS, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(clusterScopedNS, policyName, wantLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can set a minimum wait period above the upper bound and backdate the last rollout attempt timestamp", func() {
			// A minimum wait period above the upper bound (1 hour), which the controller clamps down to the bound; the
			// last rollout attempt of the cluster placement policy was 2 hours ago. Both annotations change in one
			// write.
			notBefore = time.Now()
			updatePlacementPolicyAnnotations(clusterScopedNS, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "5h"
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = backdatedTimestamp(time.Hour * 2)
			})
		})

		It("should roll out the latest resources to all the cluster placement bindings right away", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})

	Context("cluster placement policies with malformed auto-rollout annotations", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var bindingNames []string
		var wantLastRolloutAttemptTimestamp string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			// A malformed minimum wait period, for which the controller uses the default (3 minutes); the placement
			// policy was rolled out two minutes ago, i.e., within the default period but not within the lower bound
			// (1 minute).
			wantLastRolloutAttemptTimestamp = backdatedTimestamp(time.Minute * 2)
			annotations := autoRolloutEnabledAnnotations()
			annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "soon"
			annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = wantLastRolloutAttemptTimestamp
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, annotations)
			bindingNames = createPlacementBindings(policy, randStr, lifecycleBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should not roll out the cluster placement policy within the default minimum wait period", func() {
			noRollout := allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotName),
				placementBindingsActual(clusterScopedNS, bindingNames, previousResourceSnapshotName, 1),
				placementPolicyAtRestActual(clusterScopedNS, policyName, wantLastRolloutAttemptTimestamp),
			)
			Consistently(noRollout, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out the cluster placement policy unexpectedly")
		})

		It("can set a malformed last rollout attempt timestamp", func() {
			// The controller treats a cluster placement policy with a malformed last rollout attempt timestamp as never
			// rolled out. A timestamp change alone does not make the controller process the cluster placement policy;
			// change the (still malformed) minimum wait period in the same write as well.
			notBefore = time.Now()
			updatePlacementPolicyAnnotations(clusterScopedNS, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = "not-a-timestamp"
				annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = "later"
			})
		})

		It("should roll out the latest resources to all the cluster placement bindings right away, replacing the malformed timestamp", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})
})
