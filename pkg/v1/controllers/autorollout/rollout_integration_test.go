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
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	// The number of placement bindings that each placement policy in the rollout test cases has, unless noted
	// otherwise.
	rolloutBindingCount = 3
	// A number of placement bindings larger than the number of workers in the parallelizer that the auto-rollout
	// controller uses to refresh placement bindings.
	manyBindingCount = 12

	// How long ago test cases backdate the last rollout attempt timestamp to, so that the (default) minimum wait period
	// never holds a rollout back.
	backdatedLastRolloutAttemptTimestampAge = time.Minute * 10
)

// The UID of a (fictional) older instance of a placement policy, i.e., one that has been deleted and re-created
// under the same name since it created a placement binding.
const olderPlacementPolicyInstanceUID = types.UID("00000000-0000-0000-0000-000000000001")

// The details of the validating admission policy (and its binding) that test cases use to make the updates that the
// auto-rollout controller makes to a placement binding fail.
const (
	denySnapshotChangeNameTemplate = "deny-snapshot-change-%s"
	// The label that selects the placement bindings that the validating admission policy applies to; the value is the
	// random string of the test case, so that each test case only affects its own placement bindings.
	denySnapshotChangeLabelKey = "kubefleet.dev/integration-test-deny-snapshot-change"
	denySnapshotChangeMsg      = "changes to the resource snapshot of this placement binding are denied for testing"
	// The resource snapshot name that test cases write (as a dry run) to check whether the denial is in force.
	dryRunResourceSnapshotName = "dry-run-resource-snapshot"
)

// The details of the event that the auto-rollout controller reports when it fails to snapshot the selected resources
// of a placement policy due to a user error.
const (
	snapshotFailedEventReason = "FailedToSnapshotResourcesDuringAutoRollout"
	snapshotFailedEventAction = "SnapshotResources"
	// A part of the event note that identifies the user error that test cases cause: a selected resource that is too
	// large to fit in a placement resource snapshot.
	resourceTooLargeErrMsg = "too large to fit in a placement resource snapshot"
)

// oversizedConfigMapData returns ConfigMap data that is too large to fit in a placement resource snapshot (see
// maxPerSnapshotResourceDataSizeBytes in the suite); a placement policy that selects a ConfigMap with this data cannot
// have its resources snapshotted, which is a user error.
func oversizedConfigMapData() map[string]string {
	return map[string]string{
		configMapDataKey: strings.Repeat("x", 2*maxPerSnapshotResourceDataSizeBytes),
	}
}

// snapshotFailedEvent is the part of an event (reported on a snapshotting failure) that test cases check; the other
// fields (e.g., the name, the timestamps, and the reporting instance) vary.
type snapshotFailedEvent struct {
	Type                string
	Reason              string
	Action              string
	ReportingController string
	Regarding           corev1.ObjectReference
}

// snapshotFailedEventActual verifies that the auto-rollout controller has reported, with a Warning event regarding
// a placement policy, that it failed to snapshot the selected resources because one of them is too large.
func snapshotFailedEventActual(policy placementv1alpha1.PlacementPolicyAccessor) func() error {
	return func() error {
		// Events regarding cluster-scoped objects are recorded in the default namespace.
		eventNS := policy.GetNamespace()
		if eventNS == clusterScopedNS {
			eventNS = metav1.NamespaceDefault
		}
		// Read the events from the API server directly, so that the cache does not start watching events.
		events := &eventsv1.EventList{}
		if err := hubAPIReader.List(ctx, events, client.InNamespace(eventNS)); err != nil {
			return fmt.Errorf("failed to list events: %w", err)
		}

		wantEvent := snapshotFailedEvent{
			Type:                corev1.EventTypeWarning,
			Reason:              snapshotFailedEventReason,
			Action:              snapshotFailedEventAction,
			ReportingController: controllerName,
			Regarding: corev1.ObjectReference{
				APIVersion: placementv1alpha1.GroupVersion.String(),
				Kind:       placementPolicyKind(policy.GetNamespace()),
				Namespace:  policy.GetNamespace(),
				Name:       policy.GetName(),
				UID:        policy.GetUID(),
			},
		}
		for i := range events.Items {
			event := &events.Items[i]
			if event.Regarding.UID != policy.GetUID() || event.Reason != snapshotFailedEventReason {
				continue
			}
			gotEvent := snapshotFailedEvent{
				Type:                event.Type,
				Reason:              event.Reason,
				Action:              event.Action,
				ReportingController: event.ReportingController,
				Regarding: corev1.ObjectReference{
					APIVersion: event.Regarding.APIVersion,
					Kind:       event.Regarding.Kind,
					Namespace:  event.Regarding.Namespace,
					Name:       event.Regarding.Name,
					UID:        event.Regarding.UID,
				},
			}
			if diff := cmp.Diff(gotEvent, wantEvent); diff != "" {
				return fmt.Errorf("snapshotting failure event diff (-got +want):\n%s", diff)
			}
			if !strings.Contains(event.Note, resourceTooLargeErrMsg) {
				return fmt.Errorf("the snapshotting failure event note %q does not mention the user error (%q)", event.Note, resourceTooLargeErrMsg)
			}
			return nil
		}
		return fmt.Errorf("no snapshotting failure event regarding the placement policy has been found in namespace %q", eventNS)
	}
}

// variantPlacementBindingNames returns the name of a placement binding of the given variant (e.g., one that the
// auto-rollout controller should leave alone), along with the name of the (fictional) member cluster it binds to.
func variantPlacementBindingNames(randStr, variant string) (bindingName, clusterName string) {
	nameSeed := fmt.Sprintf("%s-%s", randStr, variant)
	return fmt.Sprintf(placementBindingNameTemplate, nameSeed, 0), fmt.Sprintf(memberClusterNameTemplate, nameSeed, 0)
}

// newSnapshotChangeDenyPolicyObjs returns the validating admission policy (and its binding) that
// createSnapshotChangeDenyPolicy creates for the given random string, with only the names set, e.g., for cleanup.
func newSnapshotChangeDenyPolicyObjs(randStr string) (*admissionregistrationv1.ValidatingAdmissionPolicy, *admissionregistrationv1.ValidatingAdmissionPolicyBinding) {
	name := fmt.Sprintf(denySnapshotChangeNameTemplate, randStr)
	return &admissionregistrationv1.ValidatingAdmissionPolicy{ObjectMeta: metav1.ObjectMeta{Name: name}},
		&admissionregistrationv1.ValidatingAdmissionPolicyBinding{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

// createSnapshotChangeDenyPolicy creates a validating admission policy that denies changes to the resource snapshot of
// placement bindings (of both scopes), along with a binding that applies it to the placement bindings with the
// denySnapshotChangeLabelKey label set to the given random string.
//
// Other updates (e.g., to the labels of a placement binding) and status updates are not affected.
func createSnapshotChangeDenyPolicy(randStr string) {
	policy, binding := newSnapshotChangeDenyPolicyObjs(randStr)
	policy.Spec = admissionregistrationv1.ValidatingAdmissionPolicySpec{
		FailurePolicy: ptr.To(admissionregistrationv1.Fail),
		MatchConstraints: &admissionregistrationv1.MatchResources{
			ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{
				{
					RuleWithOperations: admissionregistrationv1.RuleWithOperations{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{placementv1alpha1.GroupVersion.Group},
							APIVersions: []string{placementv1alpha1.GroupVersion.Version},
							Resources:   []string{"placementbindings", "clusterplacementbindings"},
						},
					},
				},
			},
		},
		Validations: []admissionregistrationv1.Validation{
			{
				Expression: "object.spec.resourceSnapshotName == oldObject.spec.resourceSnapshotName",
				Message:    denySnapshotChangeMsg,
			},
		},
	}
	Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create the validating admission policy")

	binding.Spec = admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
		PolicyName:        policy.Name,
		ValidationActions: []admissionregistrationv1.ValidationAction{admissionregistrationv1.Deny},
		MatchResources: &admissionregistrationv1.MatchResources{
			ObjectSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{denySnapshotChangeLabelKey: randStr},
			},
		},
	}
	Expect(hubClient.Create(ctx, binding)).To(Succeed(), "Failed to create the validating admission policy binding")
}

// placementBindingSnapshotChangeActual verifies whether the API server denies (wantDenied) or accepts changes to the
// resource snapshot of a placement binding, by making such a change as a dry run.
//
// The validating admission policy machinery of the API server picks up policy (binding) changes asynchronously; test
// cases use this check to wait until a change is in force.
func placementBindingSnapshotChangeActual(bindingNS, bindingName string, wantDenied bool) func() error {
	return func() error {
		binding := newPlacementBindingObj(bindingNS, bindingName)
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: bindingNS, Name: bindingName}, binding); err != nil {
			return fmt.Errorf("failed to retrieve placement binding %s: %w", bindingName, err)
		}
		binding.GetSpec().ResourceSnapshotName = dryRunResourceSnapshotName
		err := hubClient.Update(ctx, binding, client.DryRunAll)
		switch {
		case wantDenied && err == nil:
			return fmt.Errorf("the API server accepted the resource snapshot change on placement binding %s, want it denied", bindingName)
		case wantDenied && !strings.Contains(err.Error(), denySnapshotChangeMsg):
			return fmt.Errorf("the resource snapshot change on placement binding %s failed for an unexpected reason: %w", bindingName, err)
		case !wantDenied && err != nil:
			return fmt.Errorf("the API server did not accept the resource snapshot change on placement binding %s: %w", bindingName, err)
		}
		return nil
	}
}

// placementPolicyRolloutRetryingActual verifies that the auto-rollout controller is (still) in the middle of a rollout
// of a placement policy: it holds the binding manager role of the placement policy, and has not written the last
// rollout attempt timestamp (both checked on the same read of the placement policy).
func placementPolicyRolloutRetryingActual(policyNSName, policyName string) func() error {
	return placementPolicyAutoRolloutStateActual(policyNSName, policyName, wantAutoRolloutBindingManager(policyNSName, policyName), "")
}

var _ = Describe("rolling out placement policies", func() {
	Context("placement policies with many placement bindings", Ordered, func() {
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
			bindingNames = createPlacementBindings(policy, randStr, manyBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, bindingNames), policy)...)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources to all the placement bindings, updating each of them once", func() {
			rolledOut := allActual(
				rolledOutActual(policy, bindingNames, 0, notBefore),
				placementBindingsActual(appNamespaceName, bindingNames, snapshotName, 2),
			)
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

	Context("placement bindings already pointing to the latest resource snapshot", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var outdatedBindingNames, upToDateBindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			outdatedBindingNames = createPlacementBindings(policy, randStr, rolloutBindingCount)
			// These placement bindings point to the resource snapshot that the rollout is about to create (snapshot
			// names are deterministic).
			upToDateBindingNames = createPlacementBindingsPointingTo(policy, randStr+"-up-to-date", rolloutBindingCount, snapshotName)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, slices.Concat(outdatedBindingNames, upToDateBindingNames)), policy)...)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources, updating only the outdated placement bindings", func() {
			rolledOut := allActual(
				rolledOutActual(policy, slices.Concat(outdatedBindingNames, upToDateBindingNames), 0, notBefore),
				placementBindingsActual(appNamespaceName, outdatedBindingNames, snapshotName, 2),
				placementBindingsActual(appNamespaceName, upToDateBindingNames, snapshotName, 1),
			)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, slices.Concat(outdatedBindingNames, upToDateBindingNames)),
			)...)
		})
	})

	Context("placement bindings that do not count towards a rollout", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		otherPolicyName := fmt.Sprintf(placementPolicyNameTemplate, randStr+"-other")
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		deletingBindingName, deletingClusterName := variantPlacementBindingNames(randStr, "deleting")
		noOwnerBindingName, noOwnerClusterName := variantPlacementBindingNames(randStr, "no-owner")
		olderOwnerBindingName, olderOwnerClusterName := variantPlacementBindingNames(randStr, "older-owner")
		nonControllerOwnerBindingName, nonControllerOwnerClusterName := variantPlacementBindingNames(randStr, "non-controller-owner")
		otherPolicyBindingName, otherPolicyClusterName := variantPlacementBindingNames(randStr, "other-policy")
		excludedBindingNames := []string{
			deletingBindingName,
			noOwnerBindingName,
			olderOwnerBindingName,
			nonControllerOwnerBindingName,
			otherPolicyBindingName,
		}

		var policy placementv1alpha1.PlacementPolicyAccessor
		var regularBindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			// Another placement policy, with auto-rollout disabled.
			otherPolicy := createPlacementPolicy(appNamespaceName, otherPolicyName, configMapName, nil)

			// A regular placement binding, as a control.
			regularBindingNames = createPlacementBindings(policy, randStr, 1)

			// A placement binding that is marked for deletion (see below).
			createPlacementBinding(newOwnedPlacementBinding(policy, deletingBindingName, deletingClusterName, previousResourceSnapshotName))

			// A placement binding that names the placement policy, but has no owner.
			noOwnerBinding := newOwnedPlacementBinding(policy, noOwnerBindingName, noOwnerClusterName, previousResourceSnapshotName)
			noOwnerBinding.SetOwnerReferences(nil)
			createPlacementBinding(noOwnerBinding)

			// A placement binding that an older instance of the placement policy (of the same name) controls.
			olderOwnerBinding := newOwnedPlacementBinding(policy, olderOwnerBindingName, olderOwnerClusterName, previousResourceSnapshotName)
			olderOwnerRef := controllerOwnerRefTo(policy)
			olderOwnerRef.UID = olderPlacementPolicyInstanceUID
			olderOwnerBinding.SetOwnerReferences([]metav1.OwnerReference{olderOwnerRef})
			createPlacementBinding(olderOwnerBinding)

			// A placement binding that the placement policy owns, but does not control.
			nonControllerOwnerBinding := newOwnedPlacementBinding(policy, nonControllerOwnerBindingName, nonControllerOwnerClusterName, previousResourceSnapshotName)
			nonControllerOwnerRef := controllerOwnerRefTo(policy)
			nonControllerOwnerRef.Controller = ptr.To(false)
			nonControllerOwnerBinding.SetOwnerReferences([]metav1.OwnerReference{nonControllerOwnerRef})
			createPlacementBinding(nonControllerOwnerBinding)

			// A placement binding of the other placement policy.
			createPlacementBinding(newOwnedPlacementBinding(otherPolicy, otherPolicyBindingName, otherPolicyClusterName, previousResourceSnapshotName))

			markObjectForDeletion(newPlacementBindingObj(appNamespaceName, deletingBindingName))
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, slices.Concat(regularBindingNames, excludedBindingNames)), policy, otherPolicy)...)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources to the regular placement binding", func() {
			rolledOut := rolledOutActual(policy, regularBindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		It("should leave the placement bindings that do not count untouched", func() {
			untouched := placementBindingsResourceSnapshotActual(appNamespaceName, excludedBindingNames, previousResourceSnapshotName)
			Consistently(untouched, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Refreshed a placement binding that does not count unexpectedly")
		})

		AfterAll(func() {
			removeTestFinalizer(newPlacementBindingObj(appNamespaceName, deletingBindingName))
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newPlacementPolicyObj(appNamespaceName, otherPolicyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, slices.Concat(regularBindingNames, excludedBindingNames)),
			)...)
		})
	})

	Context("placement bindings of same-named placement policies in other scopes", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)
		crossNSOwnerBindingName, crossNSOwnerClusterName := variantPlacementBindingNames(randStr, "cross-namespace-owner")

		var policy placementv1alpha1.PlacementPolicyAccessor
		// The placement bindings in all scopes share the same names.
		var bindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			// Placement policies of the same name in another namespace and in the cluster scope, with auto-rollout
			// disabled.
			otherNSPolicy := createPlacementPolicy(otherAppNamespaceName, policyName, configMapName, nil)
			clusterPolicy := createPlacementPolicy(clusterScopedNS, policyName, configMapName, nil)

			bindingNames = createPlacementBindings(policy, randStr, rolloutBindingCount)
			createPlacementBindings(otherNSPolicy, randStr, rolloutBindingCount)
			createPlacementBindings(clusterPolicy, randStr, rolloutBindingCount)

			// A placement binding in the other namespace with the placement policy (in the app namespace) as its
			// controller owner. Kubernetes treats cross-namespace owner references as absent; this binding is excluded
			// only by the namespace the auto-rollout controller lists placement bindings in, unlike the other bindings
			// in the other namespace, which the controller ownership check excludes as well.
			crossNSOwnerBinding := newOwnedPlacementBinding(policy, crossNSOwnerBindingName, crossNSOwnerClusterName, previousResourceSnapshotName)
			crossNSOwnerBinding.SetNamespace(otherAppNamespaceName)
			createPlacementBinding(crossNSOwnerBinding)

			waitForObjectsToBeCached(slices.Concat(
				[]client.Object{policy, otherNSPolicy, clusterPolicy},
				placementBindingObjs(appNamespaceName, bindingNames),
				placementBindingObjs(otherAppNamespaceName, slices.Concat(bindingNames, []string{crossNSOwnerBindingName})),
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources to the placement bindings of the placement policy", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		It("should leave the placement bindings in the other namespace and in the cluster scope untouched", func() {
			untouched := allActual(
				placementBindingsActual(otherAppNamespaceName, slices.Concat(bindingNames, []string{crossNSOwnerBindingName}), previousResourceSnapshotName, 1),
				placementBindingsActual(clusterScopedNS, bindingNames, previousResourceSnapshotName, 1),
				placementResourceSnapshotAbsentActual(otherAppNamespaceName, snapshotName),
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotName),
			)
			Consistently(untouched, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to placement bindings in another scope unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newPlacementPolicyObj(otherAppNamespaceName, policyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, bindingNames),
				placementBindingObjs(otherAppNamespaceName, slices.Concat(bindingNames, []string{crossNSOwnerBindingName})),
				placementBindingObjs(clusterScopedNS, bindingNames),
			)...)
		})
	})

	Context("placement policies with no placement bindings", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			waitForObjectsToBeCached(policy)
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should snapshot the latest resources and complete the rollout", func() {
			rolledOut := rolledOutActual(policy, nil, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy")
		})

		AfterAll(func() {
			removeObjects(
				newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
				newPlacementPolicyObj(appNamespaceName, policyName),
				newConfigMapObj(configMapName),
			)
		})
	})

	Context("successive rollouts of placement policies with changed resources", Ordered, func() {
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

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, rolloutBindingCount)
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

		It("can make the selected resources too large to snapshot and backdate the last rollout attempt timestamp", func() {
			updateConfigMapData(configMapName, oversizedConfigMapData())
			updatePlacementPolicyAnnotations(appNamespaceName, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = backdatedTimestamp(backdatedLastRolloutAttemptTimestampAge)
			})
		})

		It("can mark the placement policy as having resources pending rollout again", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should report the snapshotting failure, write the last rollout attempt timestamp, and release the binding manager role", func() {
			// The controller writes the timestamp for a rollout that fails due to a user error too, so that the minimum
			// wait period spaces out the retries.
			failed := allActual(
				snapshotFailedEventActual(policy),
				placementPolicyRolledOutActual(appNamespaceName, policyName, notBefore),
			)
			Eventually(failed, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the snapshotting failure")
		})

		It("should not retry the rollout within the minimum wait period, even when it processes the placement policy again", func() {
			forcePlacementPolicyReconciliation(appNamespaceName, policyName)
			// A retry would claim (and then release) the binding manager role again, i.e., write to the placement policy.
			resourceVersion := placementPolicyResourceVersionOnAPIServer(appNamespaceName, policyName)
			noRetry := allActual(
				placementPolicyUnwrittenOnAPIServerActual(appNamespaceName, policyName, resourceVersion),
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotNames[1]),
				placementBindingsActual(appNamespaceName, bindingNames, snapshotNames[0], 2),
			)
			Consistently(noRetry, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Retried the rollout unexpectedly")
		})

		It("can change the selected resources to a size that fits, backdate the last rollout attempt timestamp, and make the controller retry the rollout", func() {
			updateConfigMapData(configMapName, map[string]string{configMapDataKey: altConfigMapDataValue})
			updatePlacementPolicyAnnotations(appNamespaceName, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = backdatedTimestamp(backdatedLastRolloutAttemptTimestampAge)
			})
			notBefore = time.Now()
			// The timestamp change alone does not make the controller process the placement policy.
			forcePlacementPolicyReconciliation(appNamespaceName, policyName)
		})

		It("should roll out the changed resources, as a new resource snapshot, to all the placement bindings", func() {
			rolledOut := allActual(
				rolledOutActual(policy, bindingNames, 1, notBefore),
				placementBindingsActual(appNamespaceName, bindingNames, snapshotNames[1], 3),
			)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy again")
		})

		It("should keep the previous resource snapshot", func() {
			Expect(primaryPlacementResourceSnapshotCreatedActual(policy, 0)()).To(Succeed(), "Failed to keep the previous resource snapshot")
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

	Context("successive rollouts of placement policies with unchanged resources", Ordered, func() {
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

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, rolloutBindingCount)
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

		It("can backdate the last rollout attempt timestamp", func() {
			updatePlacementPolicyAnnotations(appNamespaceName, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = backdatedTimestamp(backdatedLastRolloutAttemptTimestampAge)
			})
		})

		It("can mark the placement policy as having some clusters on an older resource revision", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonSomeClustersOnOlderResourceRevision)
		})

		It("should complete the rollout again", func() {
			rolledOut := placementPolicyRolledOutActual(appNamespaceName, policyName, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the placement policy again")
		})

		It("should reuse the latest resource snapshot and leave the placement bindings as they are", func() {
			untouched := allActual(
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotNames[1]),
				placementBindingsActual(appNamespaceName, bindingNames, snapshotNames[0], 2),
			)
			Consistently(untouched, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Changed the resource snapshot or the placement bindings unexpectedly")
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

	Context("placement binding updates that fail", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotNames := []string{
			primaryPlacementResourceSnapshotName(policyName, 0),
			primaryPlacementResourceSnapshotName(policyName, 1),
		}
		deniedBindingName, deniedClusterName := variantPlacementBindingNames(randStr, "denied")
		denyPolicy, denyPolicyBinding := newSnapshotChangeDenyPolicyObjs(randStr)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var regularBindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName, autoRolloutEnabledAnnotations())
			regularBindingNames = createPlacementBindings(policy, randStr, rolloutBindingCount-1)
			// The API server denies changes to the resource snapshot of this placement binding (see below).
			deniedBinding := newOwnedPlacementBinding(policy, deniedBindingName, deniedClusterName, previousResourceSnapshotName)
			deniedBinding.SetLabels(map[string]string{denySnapshotChangeLabelKey: randStr})
			createPlacementBinding(deniedBinding)

			createSnapshotChangeDenyPolicy(randStr)
			waitForObjectsToBeCached(append(placementBindingObjs(appNamespaceName, slices.Concat(regularBindingNames, []string{deniedBindingName})), policy)...)
		})

		It("should deny changes to the resource snapshot of the labeled placement binding", func() {
			denied := placementBindingSnapshotChangeActual(appNamespaceName, deniedBindingName, true)
			Eventually(denied, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to put the denial in force")
		})

		It("can mark the placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(appNamespaceName, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should snapshot the latest resources and claim the binding manager role", func() {
			started := allActual(
				primaryPlacementResourceSnapshotCreatedActual(policy, 0),
				placementPolicyRolloutRetryingActual(appNamespaceName, policyName),
			)
			Eventually(started, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to start the rollout")
		})

		It("should keep retrying the rollout, holding the binding manager role", func() {
			// The other placement bindings may or may not have been refreshed, as the controller stops refreshing
			// placement bindings at the first failure.
			retrying := allActual(
				placementPolicyRolloutRetryingActual(appNamespaceName, policyName),
				placementBindingsResourceSnapshotActual(appNamespaceName, []string{deniedBindingName}, previousResourceSnapshotName),
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotNames[1]),
			)
			Consistently(retrying, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep retrying the rollout")
		})

		It("can lift the denial", func() {
			notBefore = time.Now()
			removeObjects(denyPolicyBinding)
			allowed := placementBindingSnapshotChangeActual(appNamespaceName, deniedBindingName, false)
			Eventually(allowed, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to lift the denial")
		})

		It("can make the controller retry the rollout right away", func() {
			// The controller backs off further after each failed attempt; as it holds the binding manager role, any
			// update to the placement policy makes it retry right away instead.
			touchPlacementPolicy(appNamespaceName, policyName)
		})

		It("should complete the rollout", func() {
			rolledOut := rolledOutActual(policy, slices.Concat(regularBindingNames, []string{deniedBindingName}), 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the rollout")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					denyPolicyBinding,
					denyPolicy,
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotNames[0]),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotNames[1]),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(appNamespaceName, slices.Concat(regularBindingNames, []string{deniedBindingName})),
			)...)
		})
	})
})

var _ = Describe("rolling out cluster placement policies", func() {
	Context("cluster placement policies with many cluster placement bindings", Ordered, func() {
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
			bindingNames = createPlacementBindings(policy, randStr, manyBindingCount)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, bindingNames), policy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources to all the cluster placement bindings, updating each of them once", func() {
			rolledOut := allActual(
				rolledOutActual(policy, bindingNames, 0, notBefore),
				placementBindingsActual(clusterScopedNS, bindingNames, snapshotName, 2),
			)
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

	Context("cluster placement bindings already pointing to the latest resource snapshot", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var outdatedBindingNames, upToDateBindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			outdatedBindingNames = createPlacementBindings(policy, randStr, rolloutBindingCount)
			// These cluster placement bindings point to the resource snapshot that the rollout is about to create
			// (snapshot names are deterministic).
			upToDateBindingNames = createPlacementBindingsPointingTo(policy, randStr+"-up-to-date", rolloutBindingCount, snapshotName)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, slices.Concat(outdatedBindingNames, upToDateBindingNames)), policy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources, updating only the outdated cluster placement bindings", func() {
			rolledOut := allActual(
				rolledOutActual(policy, slices.Concat(outdatedBindingNames, upToDateBindingNames), 0, notBefore),
				placementBindingsActual(clusterScopedNS, outdatedBindingNames, snapshotName, 2),
				placementBindingsActual(clusterScopedNS, upToDateBindingNames, snapshotName, 1),
			)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, slices.Concat(outdatedBindingNames, upToDateBindingNames)),
			)...)
		})
	})

	Context("cluster placement bindings that do not count towards a rollout", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		otherPolicyName := fmt.Sprintf(placementPolicyNameTemplate, randStr+"-other")
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		deletingBindingName, deletingClusterName := variantPlacementBindingNames(randStr, "deleting")
		noOwnerBindingName, noOwnerClusterName := variantPlacementBindingNames(randStr, "no-owner")
		olderOwnerBindingName, olderOwnerClusterName := variantPlacementBindingNames(randStr, "older-owner")
		nonControllerOwnerBindingName, nonControllerOwnerClusterName := variantPlacementBindingNames(randStr, "non-controller-owner")
		otherPolicyBindingName, otherPolicyClusterName := variantPlacementBindingNames(randStr, "other-policy")
		excludedBindingNames := []string{
			deletingBindingName,
			noOwnerBindingName,
			olderOwnerBindingName,
			nonControllerOwnerBindingName,
			otherPolicyBindingName,
		}

		var policy placementv1alpha1.PlacementPolicyAccessor
		var regularBindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			// Another cluster placement policy, with auto-rollout disabled.
			otherPolicy := createPlacementPolicy(clusterScopedNS, otherPolicyName, configMapName, nil)

			// A regular cluster placement binding, as a control.
			regularBindingNames = createPlacementBindings(policy, randStr, 1)

			// A cluster placement binding that is marked for deletion (see below).
			createPlacementBinding(newOwnedPlacementBinding(policy, deletingBindingName, deletingClusterName, previousResourceSnapshotName))

			// A cluster placement binding that names the cluster placement policy, but has no owner.
			noOwnerBinding := newOwnedPlacementBinding(policy, noOwnerBindingName, noOwnerClusterName, previousResourceSnapshotName)
			noOwnerBinding.SetOwnerReferences(nil)
			createPlacementBinding(noOwnerBinding)

			// A cluster placement binding that an older instance of the cluster placement policy (of the same name)
			// controls.
			olderOwnerBinding := newOwnedPlacementBinding(policy, olderOwnerBindingName, olderOwnerClusterName, previousResourceSnapshotName)
			olderOwnerRef := controllerOwnerRefTo(policy)
			olderOwnerRef.UID = olderPlacementPolicyInstanceUID
			olderOwnerBinding.SetOwnerReferences([]metav1.OwnerReference{olderOwnerRef})
			createPlacementBinding(olderOwnerBinding)

			// A cluster placement binding that the cluster placement policy owns, but does not control.
			nonControllerOwnerBinding := newOwnedPlacementBinding(policy, nonControllerOwnerBindingName, nonControllerOwnerClusterName, previousResourceSnapshotName)
			nonControllerOwnerRef := controllerOwnerRefTo(policy)
			nonControllerOwnerRef.Controller = ptr.To(false)
			nonControllerOwnerBinding.SetOwnerReferences([]metav1.OwnerReference{nonControllerOwnerRef})
			createPlacementBinding(nonControllerOwnerBinding)

			// A cluster placement binding of the other cluster placement policy.
			createPlacementBinding(newOwnedPlacementBinding(otherPolicy, otherPolicyBindingName, otherPolicyClusterName, previousResourceSnapshotName))

			markObjectForDeletion(newPlacementBindingObj(clusterScopedNS, deletingBindingName))
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, slices.Concat(regularBindingNames, excludedBindingNames)), policy, otherPolicy)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources to the regular cluster placement binding", func() {
			rolledOut := rolledOutActual(policy, regularBindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		It("should leave the cluster placement bindings that do not count untouched", func() {
			untouched := placementBindingsResourceSnapshotActual(clusterScopedNS, excludedBindingNames, previousResourceSnapshotName)
			Consistently(untouched, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Refreshed a cluster placement binding that does not count unexpectedly")
		})

		AfterAll(func() {
			removeTestFinalizer(newPlacementBindingObj(clusterScopedNS, deletingBindingName))
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newPlacementPolicyObj(clusterScopedNS, otherPolicyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, slices.Concat(regularBindingNames, excludedBindingNames)),
			)...)
		})
	})

	Context("cluster placement bindings and the placement bindings of a same-named placement policy", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		// The placement bindings in both scopes share the same names.
		var bindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			// A placement policy of the same name in the app namespace, with auto-rollout disabled.
			nsPolicy := createPlacementPolicy(appNamespaceName, policyName, configMapName, nil)

			bindingNames = createPlacementBindings(policy, randStr, rolloutBindingCount)
			createPlacementBindings(nsPolicy, randStr, rolloutBindingCount)
			waitForObjectsToBeCached(slices.Concat(
				[]client.Object{policy, nsPolicy},
				placementBindingObjs(clusterScopedNS, bindingNames),
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should roll out the latest resources to the cluster placement bindings of the cluster placement policy", func() {
			rolledOut := rolledOutActual(policy, bindingNames, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		It("should leave the placement bindings of the same-named placement policy untouched", func() {
			untouched := allActual(
				placementBindingsActual(appNamespaceName, bindingNames, previousResourceSnapshotName, 1),
				placementResourceSnapshotAbsentActual(appNamespaceName, snapshotName),
			)
			Consistently(untouched, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to placement bindings in another scope unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, bindingNames),
				placementBindingObjs(appNamespaceName, bindingNames),
			)...)
		})
	})

	Context("cluster placement policies with no cluster placement bindings", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotName := primaryPlacementResourceSnapshotName(policyName, 0)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			waitForObjectsToBeCached(policy)
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should snapshot the latest resources and complete the rollout", func() {
			rolledOut := rolledOutActual(policy, nil, 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy")
		})

		AfterAll(func() {
			removeObjects(
				newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
				newPlacementPolicyObj(clusterScopedNS, policyName),
				newConfigMapObj(configMapName),
			)
		})
	})

	Context("successive rollouts of cluster placement policies with changed resources", Ordered, func() {
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

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, rolloutBindingCount)
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

		It("can make the selected resources too large to snapshot and backdate the last rollout attempt timestamp", func() {
			updateConfigMapData(configMapName, oversizedConfigMapData())
			updatePlacementPolicyAnnotations(clusterScopedNS, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = backdatedTimestamp(backdatedLastRolloutAttemptTimestampAge)
			})
		})

		It("can mark the cluster placement policy as having resources pending rollout again", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should report the snapshotting failure, write the last rollout attempt timestamp, and release the binding manager role", func() {
			// The controller writes the timestamp for a rollout that fails due to a user error too, so that the minimum
			// wait period spaces out the retries.
			failed := allActual(
				snapshotFailedEventActual(policy),
				placementPolicyRolledOutActual(clusterScopedNS, policyName, notBefore),
			)
			Eventually(failed, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the snapshotting failure")
		})

		It("should not retry the rollout within the minimum wait period, even when it processes the cluster placement policy again", func() {
			forcePlacementPolicyReconciliation(clusterScopedNS, policyName)
			// A retry would claim (and then release) the binding manager role again, i.e., write to the cluster placement
			// policy.
			resourceVersion := placementPolicyResourceVersionOnAPIServer(clusterScopedNS, policyName)
			noRetry := allActual(
				placementPolicyUnwrittenOnAPIServerActual(clusterScopedNS, policyName, resourceVersion),
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotNames[1]),
				placementBindingsActual(clusterScopedNS, bindingNames, snapshotNames[0], 2),
			)
			Consistently(noRetry, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Retried the rollout unexpectedly")
		})

		It("can change the selected resources to a size that fits, backdate the last rollout attempt timestamp, and make the controller retry the rollout", func() {
			updateConfigMapData(configMapName, map[string]string{configMapDataKey: altConfigMapDataValue})
			updatePlacementPolicyAnnotations(clusterScopedNS, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = backdatedTimestamp(backdatedLastRolloutAttemptTimestampAge)
			})
			notBefore = time.Now()
			// The timestamp change alone does not make the controller process the cluster placement policy.
			forcePlacementPolicyReconciliation(clusterScopedNS, policyName)
		})

		It("should roll out the changed resources, as a new resource snapshot, to all the cluster placement bindings", func() {
			rolledOut := allActual(
				rolledOutActual(policy, bindingNames, 1, notBefore),
				placementBindingsActual(clusterScopedNS, bindingNames, snapshotNames[1], 3),
			)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy again")
		})

		It("should keep the previous resource snapshot", func() {
			Expect(primaryPlacementResourceSnapshotCreatedActual(policy, 0)()).To(Succeed(), "Failed to keep the previous resource snapshot")
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

	Context("successive rollouts of cluster placement policies with unchanged resources", Ordered, func() {
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

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			bindingNames = createPlacementBindings(policy, randStr, rolloutBindingCount)
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

		It("can backdate the last rollout attempt timestamp", func() {
			updatePlacementPolicyAnnotations(clusterScopedNS, policyName, func(annotations map[string]string) {
				annotations[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey] = backdatedTimestamp(backdatedLastRolloutAttemptTimestampAge)
			})
		})

		It("can mark the cluster placement policy as having some clusters on an older resource revision", func() {
			notBefore = time.Now()
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonSomeClustersOnOlderResourceRevision)
		})

		It("should complete the rollout again", func() {
			rolledOut := placementPolicyRolledOutActual(clusterScopedNS, policyName, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out the cluster placement policy again")
		})

		It("should reuse the latest resource snapshot and leave the cluster placement bindings as they are", func() {
			untouched := allActual(
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotNames[1]),
				placementBindingsActual(clusterScopedNS, bindingNames, snapshotNames[0], 2),
			)
			Consistently(untouched, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Changed the resource snapshot or the cluster placement bindings unexpectedly")
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

	Context("cluster placement binding updates that fail", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		snapshotNames := []string{
			primaryPlacementResourceSnapshotName(policyName, 0),
			primaryPlacementResourceSnapshotName(policyName, 1),
		}
		deniedBindingName, deniedClusterName := variantPlacementBindingNames(randStr, "denied")
		denyPolicy, denyPolicyBinding := newSnapshotChangeDenyPolicyObjs(randStr)

		var policy placementv1alpha1.PlacementPolicyAccessor
		var regularBindingNames []string
		var notBefore time.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName, autoRolloutEnabledAnnotations())
			regularBindingNames = createPlacementBindings(policy, randStr, rolloutBindingCount-1)
			// The API server denies changes to the resource snapshot of this cluster placement binding (see below).
			deniedBinding := newOwnedPlacementBinding(policy, deniedBindingName, deniedClusterName, previousResourceSnapshotName)
			deniedBinding.SetLabels(map[string]string{denySnapshotChangeLabelKey: randStr})
			createPlacementBinding(deniedBinding)

			createSnapshotChangeDenyPolicy(randStr)
			waitForObjectsToBeCached(append(placementBindingObjs(clusterScopedNS, slices.Concat(regularBindingNames, []string{deniedBindingName})), policy)...)
		})

		It("should deny changes to the resource snapshot of the labeled cluster placement binding", func() {
			denied := placementBindingSnapshotChangeActual(clusterScopedNS, deniedBindingName, true)
			Eventually(denied, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to put the denial in force")
		})

		It("can mark the cluster placement policy as having resources pending rollout", func() {
			markPlacementPolicyAsPendingRollout(clusterScopedNS, policyName, placementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout)
		})

		It("should snapshot the latest resources and claim the binding manager role", func() {
			started := allActual(
				primaryPlacementResourceSnapshotCreatedActual(policy, 0),
				placementPolicyRolloutRetryingActual(clusterScopedNS, policyName),
			)
			Eventually(started, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to start the rollout")
		})

		It("should keep retrying the rollout, holding the binding manager role", func() {
			// The other cluster placement bindings may or may not have been refreshed, as the controller stops
			// refreshing cluster placement bindings at the first failure.
			retrying := allActual(
				placementPolicyRolloutRetryingActual(clusterScopedNS, policyName),
				placementBindingsResourceSnapshotActual(clusterScopedNS, []string{deniedBindingName}, previousResourceSnapshotName),
				placementResourceSnapshotAbsentActual(clusterScopedNS, snapshotNames[1]),
			)
			Consistently(retrying, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep retrying the rollout")
		})

		It("can lift the denial", func() {
			notBefore = time.Now()
			removeObjects(denyPolicyBinding)
			allowed := placementBindingSnapshotChangeActual(clusterScopedNS, deniedBindingName, false)
			Eventually(allowed, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to lift the denial")
		})

		It("can make the controller retry the rollout right away", func() {
			// The controller backs off further after each failed attempt; as it holds the binding manager role, any
			// update to the cluster placement policy makes it retry right away instead.
			touchPlacementPolicy(clusterScopedNS, policyName)
		})

		It("should complete the rollout", func() {
			rolledOut := rolledOutActual(policy, slices.Concat(regularBindingNames, []string{deniedBindingName}), 0, notBefore)
			Eventually(rolledOut, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the rollout")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					denyPolicyBinding,
					denyPolicy,
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotNames[0]),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotNames[1]),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				placementBindingObjs(clusterScopedNS, slices.Concat(regularBindingNames, []string{deniedBindingName})),
			)...)
		})
	})
})
