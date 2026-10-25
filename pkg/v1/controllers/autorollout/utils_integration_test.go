/*
Copyright 2026 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");f
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
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/managers/placementresourcesnapshot/uniquename"
)

const (
	configMapNameTemplate       = "configmap-%s"
	placementPolicyNameTemplate = "policy-%s"
	// Placement bindings (and the member clusters they bind to) are numbered from 0 (zero-padded).
	placementBindingNameTemplate = "binding-%s-%02d"
	memberClusterNameTemplate    = "cluster-%s-%02d"

	// The resource snapshot name that placement bindings point to before an auto-rollout; no snapshot of this name
	// needs to exist, as the auto-rollout controller never reads it.
	previousResourceSnapshotName = "previous-resource-snapshot"

	configMapDataKey   = "foo"
	configMapDataValue = "bar"
	// An alternative ConfigMap data value, used to simulate changes to the selected resources.
	altConfigMapDataValue = "baz"
)

// The namespace of cluster-scoped objects (i.e., none); the test helpers use it to tell cluster-scoped objects
// (e.g., ClusterPlacementPolicy objects) apart from their namespace-scoped counterparts.
const clusterScopedNS = ""

const (
	eventuallyDuration   = time.Second * 20
	eventuallyInterval   = time.Millisecond * 500
	consistentlyDuration = time.Second * 5
	consistentlyInterval = time.Millisecond * 500
)

// The minimum wait period annotation values that forcePlacementPolicyReconciliation alternates between. Both are
// within the allowed range; they only hold an auto-rollout back if the placement policy has been rolled out recently.
const (
	minWaitPeriodForcingValue    = "1m"
	altMinWaitPeriodForcingValue = "2m"
)

// A finalizer that test cases add to objects, so that the objects stay around (with a deletion timestamp set)
// after being deleted.
const testFinalizer = "kubefleet.dev/integration-test"

// An annotation that test cases set on placement policies only to make the auto-rollout controller process them
// (see touchPlacementPolicy).
const touchedAnnotationKey = "kubefleet.dev/integration-test-touched"

// placementPolicyKind returns the kind of the placement policy of the scope that the given namespace implies.
func placementPolicyKind(policyNSName string) string {
	if policyNSName == clusterScopedNS {
		return placementv1alpha1.ClusterPlacementPolicyKind
	}
	return placementv1alpha1.PlacementPolicyKind
}

// newPlacementPolicyObj returns a placement policy object of the appropriate scope with only the namespace and name
// set: a ClusterPlacementPolicy object if the namespace is empty, or a PlacementPolicy object otherwise.
func newPlacementPolicyObj(policyNSName, policyName string) placementv1alpha1.PlacementPolicyAccessor {
	if policyNSName == clusterScopedNS {
		return &placementv1alpha1.ClusterPlacementPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: policyName,
			},
		}
	}
	return &placementv1alpha1.PlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: policyNSName,
			Name:      policyName,
		},
	}
}

// newPlacementBindingObj returns a placement binding object of the appropriate scope with only the namespace and name
// set: a ClusterPlacementBinding object if the namespace is empty, or a PlacementBinding object otherwise.
func newPlacementBindingObj(bindingNS, bindingName string) placementv1alpha1.PlacementBindingAccessor {
	if bindingNS == clusterScopedNS {
		return &placementv1alpha1.ClusterPlacementBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: bindingName,
			},
		}
	}
	return &placementv1alpha1.PlacementBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: bindingNS,
			Name:      bindingName,
		},
	}
}

// newPlacementResourceSnapshotObj returns a placement resource snapshot object of the appropriate scope with only the
// namespace and name set: a ClusterPlacementResourceSnapshot object if the namespace is empty, or a
// PlacementResourceSnapshot object otherwise.
func newPlacementResourceSnapshotObj(snapshotNS, snapshotName string) placementv1alpha1.PlacementResourceSnapshotAccessor {
	if snapshotNS == clusterScopedNS {
		return &placementv1alpha1.ClusterPlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name: snapshotName,
			},
		}
	}
	return &placementv1alpha1.PlacementResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: snapshotNS,
			Name:      snapshotName,
		},
	}
}

// newConfigMapObj returns a ConfigMap object in the app namespace with only the namespace and name set.
func newConfigMapObj(configMapName string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: appNamespaceName,
			Name:      configMapName,
		},
	}
}

// createConfigMap creates a ConfigMap object of the given name in the app namespace, for placement policies to select.
func createConfigMap(configMapName string) *corev1.ConfigMap {
	configMap := newConfigMapObj(configMapName)
	configMap.Data = map[string]string{
		configMapDataKey: configMapDataValue,
	}
	Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the ConfigMap")
	return configMap
}

// autoRolloutEnabledAnnotations returns the annotations that enable auto-rollout for a placement policy.
func autoRolloutEnabledAnnotations() map[string]string {
	return map[string]string{
		rolloutv1alpha1.AutoRolloutEnabledAnnotationKey: rolloutv1alpha1.AutoRolloutEnabledAnnotationValue,
	}
}

// createPlacementPolicy creates a placement policy with the given annotations that selects a ConfigMap (by name) in
// the app namespace; it returns the policy as written to the API server.
//
// If the namespace is empty, a ClusterPlacementPolicy object is created; otherwise, a PlacementPolicy object is
// created, which must be in the app namespace to select the ConfigMap.
func createPlacementPolicy(policyNSName, policyName, configMapName string, annotations map[string]string) placementv1alpha1.PlacementPolicyAccessor {
	resourceSelector := placementv1alpha1.ResourceSelector{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       configMapName,
	}
	if policyNSName == clusterScopedNS {
		// A cluster placement policy must specify the namespace of the namespaced resources it selects.
		resourceSelector.Namespace = appNamespaceName
	}

	policy := newPlacementPolicyObj(policyNSName, policyName)
	policy.SetAnnotations(annotations)
	policy.SetSpec(placementv1alpha1.PlacementPolicySpec{
		ResourceSelectors: []placementv1alpha1.ResourceSelector{resourceSelector},
	})
	Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create the placement policy")
	return policy
}

// controllerOwnerRefTo returns the controller owner reference to a placement policy that the placement policy
// controller sets on the placement bindings it creates.
func controllerOwnerRefTo(policy placementv1alpha1.PlacementPolicyAccessor) metav1.OwnerReference {
	return *metav1.NewControllerRef(policy, placementv1alpha1.GroupVersion.WithKind(placementPolicyKind(policy.GetNamespace())))
}

// newOwnedPlacementBinding returns (without creating it) a placement binding that binds a placement policy to a
// (fictional) member cluster and points to the given resource snapshot.
//
// As the placement policy controller would do, the binding is in the namespace of the placement policy (i.e., it is a
// ClusterPlacementBinding object if the placement policy is cluster-scoped), names the placement policy in its spec,
// and has the placement policy as its controller owner. Test cases may change it before creating it with
// createPlacementBinding, e.g., to simulate bindings that the auto-rollout controller should leave alone.
func newOwnedPlacementBinding(
	policy placementv1alpha1.PlacementPolicyAccessor,
	bindingName, clusterName, snapshotName string,
) placementv1alpha1.PlacementBindingAccessor {
	binding := newPlacementBindingObj(policy.GetNamespace(), bindingName)
	binding.SetOwnerReferences([]metav1.OwnerReference{controllerOwnerRefTo(policy)})
	binding.SetSpec(placementv1alpha1.PlacementBindingSpec{
		PlacementPolicyName:  policy.GetName(),
		ClusterName:          clusterName,
		ResourceSnapshotName: snapshotName,
	})
	return binding
}

// createPlacementBinding creates the given placement binding.
func createPlacementBinding(binding placementv1alpha1.PlacementBindingAccessor) {
	Expect(hubClient.Create(ctx, binding)).To(Succeed(), "Failed to create placement binding %s", binding.GetName())
}

// createPlacementBindings creates the given number of placement bindings (see newOwnedPlacementBinding) of a placement
// policy, all pointing to the previous resource snapshot; it returns the names of the bindings.
//
// The auto-rollout controller never reads member clusters, so no MemberCluster objects are created.
func createPlacementBindings(policy placementv1alpha1.PlacementPolicyAccessor, randStr string, count int) []string {
	return createPlacementBindingsPointingTo(policy, randStr, count, previousResourceSnapshotName)
}

// createPlacementBindingsPointingTo creates the given number of placement bindings (see newOwnedPlacementBinding) of a
// placement policy, all pointing to the given resource snapshot; it returns the names of the bindings.
//
// The names of the bindings (and of the member clusters they bind to) are derived from the given name seed; use
// different seeds for different sets of bindings in the same scope.
func createPlacementBindingsPointingTo(
	policy placementv1alpha1.PlacementPolicyAccessor,
	nameSeed string,
	count int,
	snapshotName string,
) []string {
	bindingNames := make([]string, 0, count)
	for idx := 0; idx < count; idx++ {
		bindingName := fmt.Sprintf(placementBindingNameTemplate, nameSeed, idx)
		createPlacementBinding(newOwnedPlacementBinding(policy, bindingName, fmt.Sprintf(memberClusterNameTemplate, nameSeed, idx), snapshotName))
		bindingNames = append(bindingNames, bindingName)
	}
	return bindingNames
}

// placementBindingObjs returns objects (with only the namespace and name set) for the given placement bindings,
// e.g., for cleanup.
func placementBindingObjs(bindingNS string, bindingNames []string) []client.Object {
	objs := make([]client.Object, 0, len(bindingNames))
	for _, bindingName := range bindingNames {
		objs = append(objs, newPlacementBindingObj(bindingNS, bindingName))
	}
	return objs
}

// updateConfigMapData updates the data of a ConfigMap object in the app namespace.
func updateConfigMapData(configMapName string, data map[string]string) {
	Eventually(func() error {
		configMap := newConfigMapObj(configMapName)
		if err := hubClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap); err != nil {
			return fmt.Errorf("failed to retrieve the ConfigMap: %w", err)
		}
		configMap.Data = data
		return hubClient.Update(ctx, configMap)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the ConfigMap")
}

// getPlacementPolicy retrieves a placement policy (cluster-scoped if the namespace is empty).
func getPlacementPolicy(policyNSName, policyName string) (placementv1alpha1.PlacementPolicyAccessor, error) {
	policy := newPlacementPolicyObj(policyNSName, policyName)
	if err := hubClient.Get(ctx, client.ObjectKey{Namespace: policyNSName, Name: policyName}, policy); err != nil {
		return nil, fmt.Errorf("failed to retrieve the placement policy: %w", err)
	}
	return policy, nil
}

// updatePlacementPolicyAnnotations updates the annotations of a placement policy with the given function, which
// always receives a non-nil map.
func updatePlacementPolicyAnnotations(policyNSName, policyName string, mutate func(annotations map[string]string)) {
	Eventually(func() error {
		policy, err := getPlacementPolicy(policyNSName, policyName)
		if err != nil {
			return err
		}
		annotations := policy.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		mutate(annotations)
		policy.SetAnnotations(annotations)
		return hubClient.Update(ctx, policy)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the annotations of the placement policy")
}

// forcePlacementPolicyReconciliation makes the auto-rollout controller process a placement policy (with auto-rollout
// enabled) again, by switching its minimum wait period annotation between two values, as the controller is not
// triggered by changes that cannot affect the outcome of an auto-rollout evaluation (e.g., condition changes that
// do not call for a rollout).
//
// Test cases use it to make sure that a check for the absence of an auto-rollout covers what the controller does
// when it processes the placement policy, rather than only that it has not processed the placement policy at all.
func forcePlacementPolicyReconciliation(policyNSName, policyName string) {
	updatePlacementPolicyAnnotations(policyNSName, policyName, func(annotations map[string]string) {
		if annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] == minWaitPeriodForcingValue {
			annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = altMinWaitPeriodForcingValue
			return
		}
		annotations[rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey] = minWaitPeriodForcingValue
	})
}

// touchPlacementPolicy sets a test annotation on a placement policy to a fresh value; the change has no bearing on
// auto-rollouts, but the auto-rollout controller processes every update to a placement policy whose binding manager
// role it holds, so the change makes the controller process such a placement policy right away (e.g., instead of
// after a retry backoff).
func touchPlacementPolicy(policyNSName, policyName string) {
	updatePlacementPolicyAnnotations(policyNSName, policyName, func(annotations map[string]string) {
		annotations[touchedAnnotationKey] = time.Now().UTC().Format(time.RFC3339Nano)
	})
}

// backdatedTimestamp returns a timestamp (in the format of the last rollout attempt timestamp annotation) of the given
// duration ago, e.g., to simulate a placement policy whose last rollout attempt was a while ago.
func backdatedTimestamp(ago time.Duration) string {
	return time.Now().Add(-ago).UTC().Format(time.RFC3339)
}

// lastRolloutAttemptTimestamp returns the value of the last rollout attempt timestamp annotation of a placement policy
// (empty if the annotation is absent).
func lastRolloutAttemptTimestamp(policyNSName, policyName string) string {
	policy, err := getPlacementPolicy(policyNSName, policyName)
	Expect(err).To(Succeed())
	return policy.GetAnnotations()[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey]
}

// resourceCollectedCond returns a ResourceCollected condition of a placement policy, as the placement policy controller
// would report; setPlacementPolicyConditions fills in the observed generation.
func resourceCollectedCond(status metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{
		Type:   placementv1alpha1.PlacementPolicyCondTypeResourceCollected,
		Status: status,
		Reason: reason,
	}
}

// synchronizedCond returns a Synchronized condition of a placement policy, as the placement policy controller would
// report; setPlacementPolicyConditions fills in the observed generation.
func synchronizedCond(status metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{
		Type:   placementv1alpha1.PlacementPolicyCondTypeSynchronized,
		Status: status,
		Reason: reason,
	}
}

// updatePlacementPolicyStatus updates the status of a placement policy with the given function, in one write.
//
// Note that the function does not wait for the cache-backed client (which the controller shares) to observe the
// change, as the controller may act on it right away (e.g., relinquish a binding manager claim of its own).
func updatePlacementPolicyStatus(policyNSName, policyName string, mutate func(policy placementv1alpha1.PlacementPolicyAccessor)) {
	Eventually(func() error {
		policy, err := getPlacementPolicy(policyNSName, policyName)
		if err != nil {
			return err
		}
		mutate(policy)
		return hubClient.Status().Update(ctx, policy)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of the placement policy")
}

// setConditionsOnPlacementPolicy sets the given conditions on the status of a placement policy object (without writing
// it); conditions with no observed generation set are stamped with the current generation of the placement policy.
func setConditionsOnPlacementPolicy(policy placementv1alpha1.PlacementPolicyAccessor, conds ...metav1.Condition) {
	status := policy.GetStatus()
	for _, cond := range conds {
		if cond.ObservedGeneration == 0 {
			cond.ObservedGeneration = policy.GetGeneration()
		}
		meta.SetStatusCondition(&status.Conditions, cond)
	}
}

// setPlacementPolicyConditions sets the given conditions on the status of a placement policy, as the placement policy
// controller would do; conditions with no observed generation set are stamped with the current generation of the
// placement policy.
func setPlacementPolicyConditions(policyNSName, policyName string, conds ...metav1.Condition) {
	updatePlacementPolicyStatus(policyNSName, policyName, func(policy placementv1alpha1.PlacementPolicyAccessor) {
		setConditionsOnPlacementPolicy(policy, conds...)
	})
}

// pendingRolloutConds returns the conditions of a placement policy that has collected its resources but does not
// have them synchronized for the given reason, which calls for a rollout if the reason is
// ResourcesChangedPendingRollout or SomeClustersOnOlderResourceRevision.
func pendingRolloutConds(synchronizedReason string) []metav1.Condition {
	return []metav1.Condition{
		resourceCollectedCond(metav1.ConditionTrue, placementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected),
		synchronizedCond(metav1.ConditionFalse, synchronizedReason),
	}
}

// markPlacementPolicyAsPendingRollout marks a placement policy with the conditions that pendingRolloutConds returns.
func markPlacementPolicyAsPendingRollout(policyNSName, policyName, synchronizedReason string) {
	setPlacementPolicyConditions(policyNSName, policyName, pendingRolloutConds(synchronizedReason)...)
}

// markPlacementPolicyAsSynchronized marks a placement policy as having collected its resources and having them
// synchronized on all target clusters, i.e., as the placement policy controller would do once a rollout completes.
func markPlacementPolicyAsSynchronized(policyNSName, policyName string) {
	setPlacementPolicyConditions(policyNSName, policyName,
		resourceCollectedCond(metav1.ConditionTrue, placementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected),
		synchronizedCond(metav1.ConditionTrue, placementv1alpha1.PlacementPolicySynchronizedCondReasonAllClustersSynchronized),
	)
}

// bumpPlacementPolicyGeneration makes a (harmless) change to the spec of a placement policy, which bumps its
// generation; it returns the new generation.
func bumpPlacementPolicyGeneration(policyNSName, policyName string) int64 {
	var generation int64
	Eventually(func() error {
		policy, err := getPlacementPolicy(policyNSName, policyName)
		if err != nil {
			return err
		}
		revisionHistoryLimit := ptr.Deref(policy.GetSpec().ResourceRevisionHistoryLimit, 0) + 5
		policy.GetSpec().ResourceRevisionHistoryLimit = ptr.To(revisionHistoryLimit)
		if err := hubClient.Update(ctx, policy); err != nil {
			return err
		}
		generation = policy.GetGeneration()
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bump the generation of the placement policy")
	return generation
}

// wantAutoRolloutObjectRef returns the reference to a placement policy (cluster-scoped if the namespace is empty)
// that the auto-rollout controller registers in the binding manager claim of the placement policy itself.
func wantAutoRolloutObjectRef(policyNSName, policyName string) placementv1alpha1.ObjectReference {
	return placementv1alpha1.ObjectReference{
		APIGroup:   placementv1alpha1.GroupVersion.Group,
		APIVersion: placementv1alpha1.GroupVersion.Version,
		Kind:       placementPolicyKind(policyNSName),
		Namespace:  policyNSName,
		Name:       policyName,
	}
}

// wantAutoRolloutBindingManager returns the binding manager claim that the auto-rollout controller makes on a
// placement policy (cluster-scoped if the namespace is empty) while it rolls the placement policy out.
func wantAutoRolloutBindingManager(policyNSName, policyName string) *placementv1alpha1.BindingManager {
	return &placementv1alpha1.BindingManager{
		ControllerName: controllerName,
		ObjectRefs: []placementv1alpha1.ObjectReference{
			wantAutoRolloutObjectRef(policyNSName, policyName),
		},
	}
}

// updatePlacementPolicyBindingManager sets the binding manager of a placement policy to the given one (nil to clear
// it), e.g., to simulate a claim that the auto-rollout controller (or another controller) has left behind.
//
// Note that the function does not wait for the cache-backed client (which the controller shares) to observe the
// change, as the controller may act on it right away (e.g., relinquish a claim of its own).
func updatePlacementPolicyBindingManager(policyNSName, policyName string, bindingManager *placementv1alpha1.BindingManager) {
	updatePlacementPolicyStatus(policyNSName, policyName, func(policy placementv1alpha1.PlacementPolicyAccessor) {
		policy.GetStatus().BindingManager = bindingManager.DeepCopy()
	})
}

// markObjectForDeletion adds the test finalizer to an object and then deletes it, so that the object stays around
// with a deletion timestamp set; it waits until the cache-backed client (which the controller shares) observes the
// deletion timestamp.
//
// Test cases must call removeTestFinalizer on the object as part of the cleanup.
func markObjectForDeletion(obj client.Object) {
	key := client.ObjectKeyFromObject(obj)
	Eventually(func() error {
		if err := hubClient.Get(ctx, key, obj); err != nil {
			return fmt.Errorf("failed to retrieve object %T %s: %w", obj, key, err)
		}
		controllerutil.AddFinalizer(obj, testFinalizer)
		return hubClient.Update(ctx, obj)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add the test finalizer to object %T %s", obj, key)

	Expect(hubClient.Delete(ctx, obj)).To(Succeed(), "Failed to delete object %T %s", obj, key)

	Eventually(func() error {
		objCopy := obj.DeepCopyObject().(client.Object)
		if err := hubClient.Get(ctx, key, objCopy); err != nil {
			return fmt.Errorf("failed to retrieve object %T %s: %w", obj, key, err)
		}
		if objCopy.GetDeletionTimestamp() == nil {
			return fmt.Errorf("object %T %s has not been marked for deletion yet", obj, key)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the deletion timestamp on object %T %s", obj, key)
}

// removeTestFinalizer removes the test finalizer from an object (if the object still exists), so that the deletion
// of the object can complete.
func removeTestFinalizer(obj client.Object) {
	key := client.ObjectKeyFromObject(obj)
	Eventually(func() error {
		objCopy := obj.DeepCopyObject().(client.Object)
		if err := hubClient.Get(ctx, key, objCopy); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("failed to retrieve object %T %s: %w", obj, key, err)
		}
		if !controllerutil.ContainsFinalizer(objCopy, testFinalizer) {
			return nil
		}
		controllerutil.RemoveFinalizer(objCopy, testFinalizer)
		return hubClient.Update(ctx, objCopy)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the test finalizer from object %T %s", obj, key)
}

// waitForObjectsToBeCached waits until the cache-backed client (which the controller shares) observes all the given
// objects.
//
// The controller reads placement bindings from the cache, and silently skips the ones that the cache has yet to
// observe; test cases should call this function on their fixtures before they write the conditions that call for a
// rollout, so that a rollout never runs against a cache that has yet to catch up.
func waitForObjectsToBeCached(objs ...client.Object) {
	for _, obj := range objs {
		Eventually(func() error {
			// Read into a copy so that the object passed in is not modified.
			objCopy := obj.DeepCopyObject().(client.Object)
			return hubClient.Get(ctx, client.ObjectKeyFromObject(obj), objCopy)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe object %T %s in the cache", obj, client.ObjectKeyFromObject(obj))
	}
}

// primaryPlacementResourceSnapshotName returns the name of the primary placement resource snapshot of the given
// index for a placement policy.
func primaryPlacementResourceSnapshotName(policy placementv1alpha1.PlacementPolicyAccessor, index int) string {
	return uniquename.PrimaryPlacementResourceSnapshotName(policy.GetName(), policy.GetUID(), index)
}

// allActual combines the given checks into one, which fails with the error of the first failing check.
func allActual(actuals ...func() error) func() error {
	return func() error {
		for _, actual := range actuals {
			if err := actual(); err != nil {
				return err
			}
		}
		return nil
	}
}

// primaryPlacementResourceSnapshotCreatedActual verifies that a primary placement resource snapshot of the given
// index has been created for a placement policy, as the only snapshot of its index, and is owned by the policy.
func primaryPlacementResourceSnapshotCreatedActual(policy placementv1alpha1.PlacementPolicyAccessor, index int) func() error {
	return func() error {
		// Retrieve the placement resource snapshot.
		snapshotName := primaryPlacementResourceSnapshotName(policy, index)
		snapshot := newPlacementResourceSnapshotObj(policy.GetNamespace(), snapshotName)
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: policy.GetNamespace(), Name: snapshotName}, snapshot); err != nil {
			return fmt.Errorf("failed to retrieve the placement resource snapshot: %w", err)
		}

		// Check the labels.
		wantLabels := map[string]string{
			placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         policy.GetName(),
			placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           fmt.Sprintf("%d", index),
			placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
			placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "1",
		}
		if diff := cmp.Diff(snapshot.GetLabels(), wantLabels); diff != "" {
			return fmt.Errorf("placement resource snapshot labels diff (-got +want):\n%s", diff)
		}

		// Check the owner references.
		wantOwnerRefs := []metav1.OwnerReference{
			{
				APIVersion:         placementv1alpha1.GroupVersion.String(),
				Kind:               placementPolicyKind(policy.GetNamespace()),
				Name:               policy.GetName(),
				UID:                policy.GetUID(),
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
		if diff := cmp.Diff(snapshot.GetOwnerReferences(), wantOwnerRefs); diff != "" {
			return fmt.Errorf("placement resource snapshot owner references diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// placementResourceSnapshotAbsentActual verifies that a placement resource snapshot (cluster-scoped if the namespace
// is empty) does not exist.
func placementResourceSnapshotAbsentActual(snapshotNS, snapshotName string) func() error {
	return objectRemovedActual(newPlacementResourceSnapshotObj(snapshotNS, snapshotName))
}

// placementBindingsResourceSnapshotActual verifies that all the given placement bindings point to the given resource
// snapshot.
func placementBindingsResourceSnapshotActual(bindingNS string, bindingNames []string, wantSnapshotName string) func() error {
	return func() error {
		for _, bindingName := range bindingNames {
			binding := newPlacementBindingObj(bindingNS, bindingName)
			if err := hubClient.Get(ctx, client.ObjectKey{Namespace: bindingNS, Name: bindingName}, binding); err != nil {
				return fmt.Errorf("failed to retrieve placement binding %s: %w", bindingName, err)
			}
			if gotSnapshotName := binding.GetSpec().ResourceSnapshotName; gotSnapshotName != wantSnapshotName {
				return fmt.Errorf("placement binding %s points to resource snapshot %s, want %s", bindingName, gotSnapshotName, wantSnapshotName)
			}
		}
		return nil
	}
}

// placementBindingsActual verifies that all the given placement bindings point to the given resource snapshot, at the
// given generation (i.e., their specs have been updated exactly the expected number of times since creation).
func placementBindingsActual(bindingNS string, bindingNames []string, wantSnapshotName string, wantGeneration int64) func() error {
	return func() error {
		for _, bindingName := range bindingNames {
			binding := newPlacementBindingObj(bindingNS, bindingName)
			if err := hubClient.Get(ctx, client.ObjectKey{Namespace: bindingNS, Name: bindingName}, binding); err != nil {
				return fmt.Errorf("failed to retrieve placement binding %s: %w", bindingName, err)
			}
			if gotSnapshotName := binding.GetSpec().ResourceSnapshotName; gotSnapshotName != wantSnapshotName {
				return fmt.Errorf("placement binding %s points to resource snapshot %s, want %s", bindingName, gotSnapshotName, wantSnapshotName)
			}
			if gotGeneration := binding.GetGeneration(); gotGeneration != wantGeneration {
				return fmt.Errorf("placement binding %s is at generation %d, want %d", bindingName, gotGeneration, wantGeneration)
			}
		}
		return nil
	}
}

// placementPolicyRolledOutActual verifies that a placement policy has been rolled out (or has had a rollout fail due
// to a user error) at or after the given time (per its last rollout attempt timestamp annotation, which must be in
// RFC 3339 format and in UTC), and that no one holds its binding manager role afterwards.
//
// Both are checked on the same read of the placement policy: the controller writes the timestamp after it claims the
// role, so a read that has the timestamp but no binding manager reflects a released role, rather than a cache that
// has yet to observe the claim.
func placementPolicyRolledOutActual(policyNSName, policyName string, notBefore time.Time) func() error {
	return func() error {
		policy, err := getPlacementPolicy(policyNSName, policyName)
		if err != nil {
			return err
		}

		val, found := policy.GetAnnotations()[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey]
		if !found {
			return fmt.Errorf("the placement policy has no last rollout attempt timestamp annotation")
		}
		if !strings.HasSuffix(val, "Z") {
			return fmt.Errorf("the last rollout attempt timestamp %q is not in UTC", val)
		}
		lastRolloutAttempt, err := time.Parse(time.RFC3339Nano, val)
		if err != nil {
			return fmt.Errorf("failed to parse the last rollout attempt timestamp %q: %w", val, err)
		}
		if now := time.Now(); lastRolloutAttempt.Before(notBefore) || lastRolloutAttempt.After(now) {
			return fmt.Errorf("the last rollout attempt timestamp is %s, want a time within [%s, %s]",
				lastRolloutAttempt.Format(time.RFC3339Nano), notBefore.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
		}

		if bindingManager := policy.GetStatus().BindingManager; bindingManager != nil {
			return fmt.Errorf("the binding manager role is still held: %+v", *bindingManager)
		}
		return nil
	}
}

// rolledOutActual verifies that an auto-rollout of a placement policy has completed at or after the given time:
// the primary placement resource snapshot of the given index has been created, all the given placement bindings
// point to it, the last rollout attempt timestamp has been written, and the binding manager role has been released.
func rolledOutActual(
	policy placementv1alpha1.PlacementPolicyAccessor,
	bindingNames []string,
	snapshotIndex int,
	notBefore time.Time,
) func() error {
	return allActual(
		primaryPlacementResourceSnapshotCreatedActual(policy, snapshotIndex),
		placementBindingsResourceSnapshotActual(policy.GetNamespace(), bindingNames, primaryPlacementResourceSnapshotName(policy, snapshotIndex)),
		placementPolicyRolledOutActual(policy.GetNamespace(), policy.GetName(), notBefore),
	)
}

// placementPolicyAtRestActual verifies that the auto-rollout controller has left a placement policy at rest: no one
// holds its binding manager role, and its last rollout attempt timestamp annotation has the given value (empty if the
// annotation should be absent).
func placementPolicyAtRestActual(policyNSName, policyName, wantLastRolloutAttemptTimestamp string) func() error {
	return placementPolicyAutoRolloutStateActual(policyNSName, policyName, nil, wantLastRolloutAttemptTimestamp)
}

// placementPolicyAutoRolloutStateActual verifies, on one read of a placement policy, that its binding manager is the
// given one (nil if no one should hold the role) and that its last rollout attempt timestamp annotation has the given
// value (empty if the annotation should be absent).
func placementPolicyAutoRolloutStateActual(
	policyNSName, policyName string,
	wantBindingManager *placementv1alpha1.BindingManager,
	wantLastRolloutAttemptTimestamp string,
) func() error {
	return func() error {
		policy, err := getPlacementPolicy(policyNSName, policyName)
		if err != nil {
			return err
		}
		if diff := cmp.Diff(policy.GetStatus().BindingManager, wantBindingManager); diff != "" {
			return fmt.Errorf("binding manager diff (-got +want):\n%s", diff)
		}
		val, found := policy.GetAnnotations()[rolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey]
		switch {
		case wantLastRolloutAttemptTimestamp == "" && found:
			return fmt.Errorf("the placement policy has a last rollout attempt timestamp annotation (%q), want none", val)
		case val != wantLastRolloutAttemptTimestamp:
			return fmt.Errorf("the last rollout attempt timestamp is %q, want %q", val, wantLastRolloutAttemptTimestamp)
		}
		return nil
	}
}

// bindingManagerReleasedOnAPIServerActual verifies that no one holds the binding manager role of a placement policy,
// reading the placement policy from the API server directly.
//
// Test cases use it after they write a binding manager claim themselves: a read from the cache might predate the
// claim, and would then pass even if the controller never released it.
func bindingManagerReleasedOnAPIServerActual(policyNSName, policyName string) func() error {
	return func() error {
		policy := newPlacementPolicyObj(policyNSName, policyName)
		if err := hubAPIReader.Get(ctx, client.ObjectKey{Namespace: policyNSName, Name: policyName}, policy); err != nil {
			return fmt.Errorf("failed to retrieve the placement policy from the API server: %w", err)
		}
		if bindingManager := policy.GetStatus().BindingManager; bindingManager != nil {
			return fmt.Errorf("the binding manager role is still held: %+v", *bindingManager)
		}
		return nil
	}
}

// placementPolicyResourceVersionOnAPIServer returns the resource version of a placement policy, reading the placement
// policy from the API server directly.
func placementPolicyResourceVersionOnAPIServer(policyNSName, policyName string) string {
	policy := newPlacementPolicyObj(policyNSName, policyName)
	Expect(hubAPIReader.Get(ctx, client.ObjectKey{Namespace: policyNSName, Name: policyName}, policy)).To(Succeed(), "Failed to retrieve the placement policy from the API server")
	return policy.GetResourceVersion()
}

// placementPolicyUnwrittenOnAPIServerActual verifies that no one has written to a placement policy since it was at the
// given resource version (e.g., that the controller has not claimed and released its binding manager role again),
// reading the placement policy from the API server directly.
func placementPolicyUnwrittenOnAPIServerActual(policyNSName, policyName, wantResourceVersion string) func() error {
	return func() error {
		policy := newPlacementPolicyObj(policyNSName, policyName)
		if err := hubAPIReader.Get(ctx, client.ObjectKey{Namespace: policyNSName, Name: policyName}, policy); err != nil {
			return fmt.Errorf("failed to retrieve the placement policy from the API server: %w", err)
		}
		if gotResourceVersion := policy.GetResourceVersion(); gotResourceVersion != wantResourceVersion {
			return fmt.Errorf("the placement policy is at resource version %s, want %s (it has been written to)", gotResourceVersion, wantResourceVersion)
		}
		return nil
	}
}

// objectRemovedActual verifies that an object is gone.
func objectRemovedActual(obj client.Object) func() error {
	return func() error {
		objCopy := obj.DeepCopyObject().(client.Object)
		err := hubClient.Get(ctx, client.ObjectKeyFromObject(obj), objCopy)
		switch {
		case err == nil:
			return fmt.Errorf("object %T %s still exists", obj, client.ObjectKeyFromObject(obj))
		case !apierrors.IsNotFound(err):
			return fmt.Errorf("failed to retrieve object %T %s: %w", obj, client.ObjectKeyFromObject(obj), err)
		}
		return nil
	}
}

// removeObjects deletes the given objects, and waits until all of them are gone. All the deletions are issued before
// any wait, so that the cleanup does not take one polling interval per object.
//
// The envtest environment does not run the garbage collector; test cases must remove all the objects they create
// (including those created by the controller on their behalf, e.g., placement resource snapshots) explicitly.
func removeObjects(objs ...client.Object) {
	for _, obj := range objs {
		Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T %s", obj, client.ObjectKeyFromObject(obj))
	}
	for _, obj := range objs {
		Eventually(objectRemovedActual(obj), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove object %T %s", obj, client.ObjectKeyFromObject(obj))
	}
}
