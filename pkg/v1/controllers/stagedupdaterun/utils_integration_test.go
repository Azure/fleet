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

package stagedupdaterun

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/managers/placementresourcesnapshot/uniquename"
)

const (
	configMapNameTemplate            = "configmap-%s"
	placementPolicyNameTemplate      = "policy-%s"
	placementBindingNameTemplate     = "binding-%s"
	stagedUpdateStrategyNameTemplate = "strategy-%s"
	stagedUpdateRunNameTemplate      = "run-%s"
	// Member clusters are numbered from 0 (zero-padded), so that the lexical order of their names matches the
	// numerical order of their indexes.
	memberClusterNameTemplate = "cluster-%s-%02d"
	// The approval requests that test cases create (and delete) only to trigger the processing of a staged update
	// run (see triggerStagedUpdateRunReconciliation).
	triggerApprovalRequestNameTemplate = "trigger-%s"

	// The resource snapshot name that placement bindings point to before a staged update run rolls out
	// a new one; no snapshot of this name needs to exist, as no work generator runs in the test environment.
	previousResourceSnapshotName = "previous-resource-snapshot"

	envLabelKey          = "env"
	envLabelValueCanary  = "canary"
	envLabelValueProd    = "prod"
	envLabelValueStaging = "staging"

	regionLabelKey       = "region"
	regionLabelValueEast = "east"
	regionLabelValueWest = "west"

	groupLabelKey = "group"
	orderLabelKey = "order"

	canaryStageName = "canary"
	prodStageName   = "prod"
	eastStageName   = "east"
	emptyStageName  = "empty"
	restStageName   = "rest"

	configMapDataKey   = "foo"
	configMapDataValue = "bar"
	// An alternative ConfigMap data value, used to simulate changes to the selected resources.
	altConfigMapDataValue = "baz"
)

// The namespace of cluster-scoped objects (i.e., none); the test helpers use it to tell cluster-scoped objects
// (e.g., ClusterStagedUpdateRun objects) apart from their namespace-scoped counterparts.
const clusterScopedNS = ""

const (
	eventuallyDuration   = time.Second * 20
	eventuallyInterval   = time.Millisecond * 500
	consistentlyDuration = time.Second * 5
	consistentlyInterval = time.Millisecond * 500

	// The controller persists the progress of a rollout only at the end of a reconciliation, i.e., when the
	// processing time budget (the refresh period) runs out if the rollout is blocked; checks on the progress of
	// in-flight rollouts use a longer timeout that covers a budget expiry plus some leeway.
	eventuallyDurationForBudgetExpiry = time.Second * 20
	// Checks that span multiple processing time budget expiries (i.e., multiple reconciliations of an in-flight
	// rollout) use a longer duration.
	consistentlyDurationAcrossBudgetExpiries = 2*refreshPeriodForTesting + 2*time.Second

	// The API requires the per-cluster wait time limit to be at least one minute; checks that wait for a member
	// cluster to time out use a longer timeout that covers the limit plus some leeway.
	eventuallyDurationForClusterTimeout = time.Minute * 2
)

// The reasons that test cases use when they simulate placement binding status changes.
const (
	markedAsSynchronizedReason    = "MarkedAsSynchronized"
	markedAsNotSynchronizedReason = "MarkedAsNotSynchronized"
	markedAsAvailableReason       = "MarkedAsAvailable"
	markedAsUnavailableReason     = "MarkedAsUnavailable"
)

// The reasons that test cases use when they approve or reject approval requests, as a user would do.
const (
	markedAsApprovedReason = "MarkedAsApproved"
	markedAsRejectedReason = "MarkedAsRejected"
)

// A finalizer that test cases add to objects, so that the objects stay around (with a deletion timestamp set)
// after being deleted.
const testFinalizer = "kubefleet.dev/integration-test"

var (
	ignoreFieldConditionLTTMsg = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")

	// The option for comparing the status of a staged update run that has started rolling out, where the stage
	// timestamps are set as the rollout progresses; test cases check the timestamps separately.
	ignoreFieldsPerStageStatusTimestamps = cmpopts.IgnoreFields(rolloutv1alpha1.PerStageStatus{}, "StartedTimestamp", "CompletedTimestamp")

	// The options for comparing the layout of the stages in a staged update run (i.e., the stages, their clusters
	// and tasks, and the resolved max concurrency), with all the progress information ignored.
	ignoreFieldsPerStageStatusProgress   = cmpopts.IgnoreFields(rolloutv1alpha1.PerStageStatus{}, "Conditions", "StartedTimestamp", "CompletedTimestamp")
	ignoreFieldsPerClusterStatusProgress = cmpopts.IgnoreFields(rolloutv1alpha1.PerClusterStatus{}, "Conditions")
	ignoreFieldsPerStageTaskProgress     = cmpopts.IgnoreFields(rolloutv1alpha1.PerStageTaskStatus{}, "Conditions", "ApprovalRequestName")

	lessFuncCondition = func(a, b metav1.Condition) bool {
		return a.Type < b.Type
	}
	lessFuncObjectRef = func(a, b placementv1alpha1.ObjectReference) bool {
		return fmt.Sprintf("%s/%s/%s", a.Kind, a.Namespace, a.Name) < fmt.Sprintf("%s/%s/%s", b.Kind, b.Namespace, b.Name)
	}
)

// placementPolicyKind returns the kind of the placement policy of the scope that the given namespace implies.
func placementPolicyKind(policyNSName string) string {
	if policyNSName == clusterScopedNS {
		return placementv1alpha1.ClusterPlacementPolicyKind
	}
	return placementv1alpha1.PlacementPolicyKind
}

// stagedUpdateRunKind returns the kind of the staged update run of the scope that the given namespace implies.
func stagedUpdateRunKind(runNSName string) string {
	if runNSName == clusterScopedNS {
		return rolloutv1alpha1.ClusterStagedUpdateRunKind
	}
	return rolloutv1alpha1.StagedUpdateRunKind
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

// newStagedUpdateStrategyObj returns a staged update strategy object of the appropriate scope with only the namespace
// and name set: a ClusterStagedUpdateStrategy object if the namespace is empty, or a StagedUpdateStrategy object
// otherwise.
func newStagedUpdateStrategyObj(strategyNS, strategyName string) rolloutv1alpha1.StagedUpdateStrategyAccessor {
	if strategyNS == clusterScopedNS {
		return &rolloutv1alpha1.ClusterStagedUpdateStrategy{
			ObjectMeta: metav1.ObjectMeta{
				Name: strategyName,
			},
		}
	}
	return &rolloutv1alpha1.StagedUpdateStrategy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: strategyNS,
			Name:      strategyName,
		},
	}
}

// newStagedUpdateRunObj returns a staged update run object of the appropriate scope with only the namespace and name
// set: a ClusterStagedUpdateRun object if the namespace is empty, or a StagedUpdateRun object otherwise.
func newStagedUpdateRunObj(runNSName, runName string) rolloutv1alpha1.StagedUpdateRunAccessor {
	if runNSName == clusterScopedNS {
		return &rolloutv1alpha1.ClusterStagedUpdateRun{
			ObjectMeta: metav1.ObjectMeta{
				Name: runName,
			},
		}
	}
	return &rolloutv1alpha1.StagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: runNSName,
			Name:      runName,
		},
	}
}

// newApprovalRequestObj returns an approval request object of the appropriate scope with only the namespace and name
// set: a ClusterApprovalRequest object if the namespace is empty, or an ApprovalRequest object otherwise.
func newApprovalRequestObj(requestNS, requestName string) rolloutv1alpha1.ApprovalRequestAccessor {
	if requestNS == clusterScopedNS {
		return &rolloutv1alpha1.ClusterApprovalRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: requestName,
			},
		}
	}
	return &rolloutv1alpha1.ApprovalRequest{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: requestNS,
			Name:      requestName,
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

// createPlacementPolicy creates a placement policy that selects a ConfigMap (by name) in the app namespace; it returns
// the policy as written to the API server.
//
// If the namespace is empty, a ClusterPlacementPolicy object is created; otherwise, a PlacementPolicy object is
// created, which must be in the app namespace to select the ConfigMap.
func createPlacementPolicy(policyNSName, policyName, configMapName string) placementv1alpha1.PlacementPolicyAccessor {
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
	policy.SetSpec(placementv1alpha1.PlacementPolicySpec{
		ResourceSelectors: []placementv1alpha1.ResourceSelector{resourceSelector},
	})
	Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create the placement policy")
	return policy
}

// createMemberCluster creates a member cluster object of the given name with the given labels.
func createMemberCluster(clusterName string, labels map[string]string) *clusterv1beta1.MemberCluster {
	memberCluster := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   clusterName,
			Labels: labels,
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      clusterName,
				Namespace: "fleet-system",
			},
		},
	}
	Expect(hubClient.Create(ctx, memberCluster)).To(Succeed(), "Failed to create the member cluster")
	return memberCluster
}

// createPlacementBinding creates a placement binding that binds a placement resource snapshot of a placement policy
// to a member cluster, as the placement policy controller would do; it returns the binding as written to the API server.
//
// If the namespace is empty, a ClusterPlacementBinding object is created; otherwise, a PlacementBinding object
// is created.
func createPlacementBinding(bindingNS, bindingName, policyName, clusterName, snapshotName string) placementv1alpha1.PlacementBindingAccessor {
	binding := newPlacementBindingObj(bindingNS, bindingName)
	binding.SetSpec(placementv1alpha1.PlacementBindingSpec{
		PlacementPolicyName:  policyName,
		ClusterName:          clusterName,
		ResourceSnapshotName: snapshotName,
	})
	Expect(hubClient.Create(ctx, binding)).To(Succeed(), "Failed to create the placement binding")
	return binding
}

// memberClusterAndBindingNames returns the names of the given number of member clusters, along with the names of
// the placement bindings that bind a placement policy to them (at matching indexes).
func memberClusterAndBindingNames(randStr string, count int) (memberClusterNames, bindingNames []string) {
	memberClusterNames = make([]string, 0, count)
	bindingNames = make([]string, 0, count)
	for idx := 0; idx < count; idx++ {
		memberClusterName := fmt.Sprintf(memberClusterNameTemplate, randStr, idx)
		memberClusterNames = append(memberClusterNames, memberClusterName)
		bindingNames = append(bindingNames, fmt.Sprintf(placementBindingNameTemplate, memberClusterName))
	}
	return memberClusterNames, bindingNames
}

// createMemberClustersAndBindings creates a member cluster with the given labels for each of the given names, along
// with a placement binding that binds the placement policy to the member cluster (pointing to the previous resource
// snapshot); it returns the created objects.
//
// The objects are created in the reverse order of the names, so that the order of the clusters in a stage is never
// a by-product of the creation order.
func createMemberClustersAndBindings(
	bindingNS, policyName string,
	memberClusterNames, bindingNames []string,
	memberClusterLabels []map[string]string,
) []client.Object {
	objs := make([]client.Object, 0, 2*len(memberClusterNames))
	for idx := len(memberClusterNames) - 1; idx >= 0; idx-- {
		memberCluster := createMemberCluster(memberClusterNames[idx], memberClusterLabels[idx])
		binding := createPlacementBinding(bindingNS, bindingNames[idx], policyName, memberClusterNames[idx], previousResourceSnapshotName)
		objs = append(objs, memberCluster, binding)
	}
	return objs
}

// memberClusterAndBindingObjs returns objects (with only the namespace and name set) for the given member clusters
// and their placement bindings, e.g., for cleanup.
func memberClusterAndBindingObjs(bindingNS string, memberClusterNames, bindingNames []string) []client.Object {
	objs := make([]client.Object, 0, 2*len(memberClusterNames))
	for idx := range memberClusterNames {
		objs = append(objs,
			newPlacementBindingObj(bindingNS, bindingNames[idx]),
			&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[idx]}},
		)
	}
	return objs
}

// createStagedUpdateStrategy creates a staged update strategy with the given stages; it returns the strategy as
// written to the API server.
//
// If the namespace is empty, a ClusterStagedUpdateStrategy object is created; otherwise, a StagedUpdateStrategy
// object is created.
func createStagedUpdateStrategy(strategyNS, strategyName string, stages []rolloutv1alpha1.Stage) rolloutv1alpha1.StagedUpdateStrategyAccessor {
	strategy := newStagedUpdateStrategyObj(strategyNS, strategyName)
	strategy.SetSpec(rolloutv1alpha1.StagedUpdateStrategySpec{
		Stages: stages,
	})
	Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create the staged update strategy")
	return strategy
}

// createStagedUpdateRun creates a staged update run with the given spec; it returns the run as written to the
// API server.
//
// If the namespace is empty, a ClusterStagedUpdateRun object is created; otherwise, a StagedUpdateRun object
// is created.
func createStagedUpdateRun(runNSName, runName string, spec rolloutv1alpha1.StagedUpdateRunSpec) rolloutv1alpha1.StagedUpdateRunAccessor {
	run := newStagedUpdateRunObj(runNSName, runName)
	run.SetSpec(spec)
	Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create the staged update run")
	return run
}

// createPlacementResourceSnapshot creates a placement resource snapshot (with no resources) of the given index and
// sub-index, controlled by the given placement policy; it returns the snapshot as written to the API server.
//
// The snapshot has the labels and the controller owner reference that the staged update run controller checks when
// a staged update run names the snapshot to roll out explicitly. It does not have the annotations that the placement
// resource snapshot manager adds (the resource contents hash and the owner), which the manager relies on when it
// looks up the latest snapshot and when it garbage collects snapshots; use it only with staged update runs that
// name their snapshot explicitly.
//
// The snapshot is created in the namespace of the placement policy (i.e., it is a ClusterPlacementResourceSnapshot
// object if the placement policy is cluster-scoped). The count of snapshots sharing the same index is set (as a
// label) only on the primary snapshot (sub-index 0).
func createPlacementResourceSnapshot(
	snapshotName string,
	ownerPolicy placementv1alpha1.PlacementPolicyAccessor,
	index, subIndex, subIndexedSnapshotCount int,
) placementv1alpha1.PlacementResourceSnapshotAccessor {
	snapshot := newPlacementResourceSnapshotObj(ownerPolicy.GetNamespace(), snapshotName)
	labels := map[string]string{
		placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  ownerPolicy.GetName(),
		placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    strconv.Itoa(index),
		placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: strconv.Itoa(subIndex),
	}
	if subIndex == 0 {
		labels[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey] = strconv.Itoa(subIndexedSnapshotCount)
	}
	snapshot.SetLabels(labels)
	snapshot.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(ownerPolicy, placementv1alpha1.GroupVersion.WithKind(placementPolicyKind(ownerPolicy.GetNamespace()))),
	})
	Expect(hubClient.Create(ctx, snapshot)).To(Succeed(), "Failed to create the placement resource snapshot")
	return snapshot
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

// updateMemberClusterLabels replaces the labels of a member cluster with the given ones.
func updateMemberClusterLabels(clusterName string, labels map[string]string) {
	Eventually(func() error {
		memberCluster := &clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: clusterName}, memberCluster); err != nil {
			return fmt.Errorf("failed to retrieve the member cluster: %w", err)
		}
		memberCluster.Labels = labels
		return hubClient.Update(ctx, memberCluster)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the labels of the member cluster")
}

// updateStagedUpdateStrategyStages replaces the stages of a staged update strategy with the given ones.
func updateStagedUpdateStrategyStages(strategyNS, strategyName string, stages []rolloutv1alpha1.Stage) {
	Eventually(func() error {
		strategy := newStagedUpdateStrategyObj(strategyNS, strategyName)
		if err := hubClient.Get(ctx, client.ObjectKeyFromObject(strategy), strategy); err != nil {
			return fmt.Errorf("failed to retrieve the staged update strategy: %w", err)
		}
		strategy.GetSpec().Stages = stages
		return hubClient.Update(ctx, strategy)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the stages of the staged update strategy")
}

// updateStagedUpdateRunSuspended suspends or resumes a staged update run.
func updateStagedUpdateRunSuspended(runNSName, runName string, suspended bool) {
	Eventually(func() error {
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}
		run.GetSpec().Suspended = suspended
		return hubClient.Update(ctx, run)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the suspended flag of the staged update run")
}

// markPlacementBindingAsAvailable marks a placement binding as synchronized and available, as the work generator
// would do once the resources in the binding have been applied and become available on the member cluster.
//
// See setPlacementBindingConditions for how the function avoids conflicts with the controller.
func markPlacementBindingAsAvailable(bindingNS, bindingName, wantSnapshotName string) {
	setPlacementBindingConditions(bindingNS, bindingName, wantSnapshotName, metav1.ConditionTrue, metav1.ConditionTrue, 0)
}

// setPlacementBindingConditions sets the Synchronized and Available conditions of a placement binding to the given
// statuses, as the work generator would do.
//
// The function first waits until the binding points to the given resource snapshot, i.e., the staged update run
// has rolled out to the member cluster, and then sets the conditions as observed at the generation of the binding at
// that point plus the given offset (e.g., -1 for a stale observation); this way, the status update does not conflict
// with the update the controller makes to the binding spec.
func setPlacementBindingConditions(
	bindingNS, bindingName, wantSnapshotName string,
	synchronizedStatus, availableStatus metav1.ConditionStatus,
	observedGenerationOffset int64,
) {
	synchronizedReason := markedAsSynchronizedReason
	if synchronizedStatus != metav1.ConditionTrue {
		synchronizedReason = markedAsNotSynchronizedReason
	}
	availableReason := markedAsAvailableReason
	if availableStatus != metav1.ConditionTrue {
		availableReason = markedAsUnavailableReason
	}

	Eventually(func() error {
		binding := newPlacementBindingObj(bindingNS, bindingName)
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: bindingNS, Name: bindingName}, binding); err != nil {
			return fmt.Errorf("failed to retrieve the placement binding: %w", err)
		}
		if gotSnapshotName := binding.GetSpec().ResourceSnapshotName; gotSnapshotName != wantSnapshotName {
			return fmt.Errorf("placement binding %s points to resource snapshot %s, want %s", bindingName, gotSnapshotName, wantSnapshotName)
		}

		observedGeneration := binding.GetGeneration() + observedGenerationOffset
		bindingStatus := binding.GetStatus()
		meta.SetStatusCondition(&bindingStatus.Conditions, metav1.Condition{
			Type:               placementv1alpha1.PlacementBindingCondTypeSynchronized,
			Status:             synchronizedStatus,
			Reason:             synchronizedReason,
			ObservedGeneration: observedGeneration,
		})
		meta.SetStatusCondition(&bindingStatus.Conditions, metav1.Condition{
			Type:               placementv1alpha1.PlacementBindingCondTypeAvailable,
			Status:             availableStatus,
			Reason:             availableReason,
			ObservedGeneration: observedGeneration,
		})
		return hubClient.Status().Update(ctx, binding)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to set the conditions of the placement binding")
}

// updatePlacementBindingSuspended suspends or resumes a placement binding; it waits until the cache-backed client
// (which the controller shares) observes the change.
func updatePlacementBindingSuspended(bindingNS, bindingName string, suspended bool) {
	updatePlacementBindingSpec(bindingNS, bindingName, func(spec *placementv1alpha1.PlacementBindingSpec) {
		spec.Suspended = suspended
	})
}

// updatePlacementBindingSpec updates the spec of a placement binding with the given function, e.g., to simulate
// changes that other parties make to the binding; it waits until the cache-backed client (which the controller
// shares) observes the update.
func updatePlacementBindingSpec(bindingNS, bindingName string, mutate func(spec *placementv1alpha1.PlacementBindingSpec)) {
	key := client.ObjectKey{Namespace: bindingNS, Name: bindingName}
	var updatedGeneration int64
	Eventually(func() error {
		binding := newPlacementBindingObj(bindingNS, bindingName)
		if err := hubClient.Get(ctx, key, binding); err != nil {
			return fmt.Errorf("failed to retrieve the placement binding: %w", err)
		}
		mutate(binding.GetSpec())
		if err := hubClient.Update(ctx, binding); err != nil {
			return fmt.Errorf("failed to update the placement binding: %w", err)
		}
		updatedGeneration = binding.GetGeneration()
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the spec of the placement binding")

	Eventually(func() error {
		binding := newPlacementBindingObj(bindingNS, bindingName)
		if err := hubClient.Get(ctx, key, binding); err != nil {
			return fmt.Errorf("failed to retrieve the placement binding: %w", err)
		}
		if binding.GetGeneration() < updatedGeneration {
			return fmt.Errorf("the cache has yet to observe the update to placement binding %s", bindingName)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the update to the placement binding in the cache")
}

// approveApprovalRequest approves an approval request (cluster-scoped if the namespace is empty), as a user would
// do, by setting its Approved condition to True.
func approveApprovalRequest(requestNS, requestName string) {
	setApprovalRequestApproved(requestNS, requestName, metav1.ConditionTrue)
}

// rejectApprovalRequest rejects an approval request (cluster-scoped if the namespace is empty), as a user would do,
// by setting its Approved condition to False.
func rejectApprovalRequest(requestNS, requestName string) {
	setApprovalRequestApproved(requestNS, requestName, metav1.ConditionFalse)
}

// setApprovalRequestApproved sets the Approved condition of an approval request (cluster-scoped if the namespace is
// empty) to the given status, observed at the current generation of the request, and waits until the cache (which
// the controller shares) observes the new status; this way, test cases that check the approval request right
// afterwards (which read from the cache too) do not see a stale copy.
func setApprovalRequestApproved(requestNS, requestName string, status metav1.ConditionStatus) {
	Eventually(func() error {
		request := newApprovalRequestObj(requestNS, requestName)
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: requestNS, Name: requestName}, request); err != nil {
			return fmt.Errorf("failed to retrieve the approval request: %w", err)
		}
		approvedCond := approvalRequestApprovedCond(status)
		approvedCond.ObservedGeneration = request.GetGeneration()
		meta.SetStatusCondition(&request.GetStatus().Conditions, approvedCond)
		return hubClient.Status().Update(ctx, request)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to set the Approved condition of the approval request")

	Eventually(func() error {
		request, err := getApprovalRequest(requestNS, requestName)
		if err != nil {
			return err
		}
		approvedCond := meta.FindStatusCondition(request.GetStatus().Conditions, rolloutv1alpha1.ApprovalRequestCondTypeApproved)
		if approvedCond == nil || approvedCond.Status != status {
			return fmt.Errorf("the Approved condition of the approval request is %+v, want status %s", approvedCond, status)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the Approved condition of the approval request in the cache")
}

// createManualApprovalRequest creates an approval request (cluster-scoped if the namespace is empty) for the given
// stage of a staged update run, as a user (rather than the controller) would do, i.e., with no owner references.
func createManualApprovalRequest(requestNS, requestName, runName, stageName string) rolloutv1alpha1.ApprovalRequestAccessor {
	request := newApprovalRequestObj(requestNS, requestName)
	request.SetSpec(rolloutv1alpha1.ApprovalRequestSpec{
		StagedUpdateRunName: runName,
		StageName:           stageName,
	})
	Expect(hubClient.Create(ctx, request)).To(Succeed(), "Failed to create the approval request")
	return request
}

// triggerStagedUpdateRunReconciliation makes the controller process a staged update run (cluster-scoped if the
// namespace is empty) again, without changing the run itself. The controller does not watch most of the objects
// that test cases change (e.g., placement policies and bindings), but it does process a staged update run when an
// approval request that references the run is deleted; the function creates such an approval request (with the
// given name, for the given stage) and deletes it, and returns once the cache has observed the deletion.
func triggerStagedUpdateRunReconciliation(runNSName, runName, stageName, requestName string) {
	request := createManualApprovalRequest(runNSName, requestName, runName, stageName)
	waitForObjectsToBeCached(request)
	Expect(hubClient.Delete(ctx, request)).To(Succeed(), "Failed to delete the approval request")
	Eventually(objectRemovedActual(request), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the deletion of the approval request in the cache")
}

// getApprovalRequest retrieves an approval request (cluster-scoped if the namespace is empty).
func getApprovalRequest(requestNS, requestName string) (rolloutv1alpha1.ApprovalRequestAccessor, error) {
	request := newApprovalRequestObj(requestNS, requestName)
	if err := hubClient.Get(ctx, client.ObjectKey{Namespace: requestNS, Name: requestName}, request); err != nil {
		return nil, fmt.Errorf("failed to retrieve the approval request: %w", err)
	}
	return request, nil
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
// The controller reads placement policies, staged update strategies, placement bindings, and member clusters from
// the cache, and treats some of them being absent as a terminal initialization failure; test cases should call this
// function on their fixtures before creating a staged update run, so that the run is never initialized against a
// cache that has yet to catch up.
func waitForObjectsToBeCached(objs ...client.Object) {
	for _, obj := range objs {
		Eventually(func() error {
			// Read into a copy so that the object passed in is not modified.
			objCopy := obj.DeepCopyObject().(client.Object)
			return hubClient.Get(ctx, client.ObjectKeyFromObject(obj), objCopy)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe object %T %s in the cache", obj, client.ObjectKeyFromObject(obj))
	}
}

// getStagedUpdateRun retrieves a staged update run (cluster-scoped if the namespace is empty).
func getStagedUpdateRun(runNSName, runName string) (rolloutv1alpha1.StagedUpdateRunAccessor, error) {
	run := newStagedUpdateRunObj(runNSName, runName)
	if err := hubClient.Get(ctx, client.ObjectKey{Namespace: runNSName, Name: runName}, run); err != nil {
		return nil, fmt.Errorf("failed to retrieve the staged update run: %w", err)
	}
	return run, nil
}

// primaryPlacementResourceSnapshotName returns the name of the primary placement resource snapshot of the given
// index for a placement policy.
func primaryPlacementResourceSnapshotName(policy placementv1alpha1.PlacementPolicyAccessor, index int) string {
	return uniquename.PrimaryPlacementResourceSnapshotName(policy.GetName(), policy.GetUID(), index)
}

// secondaryPlacementResourceSnapshotName returns the name of the secondary placement resource snapshot of the given
// index and sub-index for a placement policy.
func secondaryPlacementResourceSnapshotName(policy placementv1alpha1.PlacementPolicyAccessor, index, subIndex int) string {
	return uniquename.SecondaryPlacementResourceSnapshotName(policy.GetName(), policy.GetUID(), index, subIndex)
}

// wantPerClusterStatuses builds the per-cluster statuses (with no conditions) of the member clusters at the given
// indexes, in the given order, along with their placement bindings.
func wantPerClusterStatuses(memberClusterNames, bindingNames []string, idxs ...int) []rolloutv1alpha1.PerClusterStatus {
	perClusterStatuses := make([]rolloutv1alpha1.PerClusterStatus, 0, len(idxs))
	for _, idx := range idxs {
		perClusterStatuses = append(perClusterStatuses, wantPerClusterStatus(memberClusterNames, bindingNames, idx))
	}
	return perClusterStatuses
}

// wantPerClusterStatus builds the per-cluster status of the member cluster at the given index (along with its
// placement binding), with the given conditions.
func wantPerClusterStatus(memberClusterNames, bindingNames []string, idx int, conds ...metav1.Condition) rolloutv1alpha1.PerClusterStatus {
	return rolloutv1alpha1.PerClusterStatus{
		ClusterName:          memberClusterNames[idx],
		PlacementBindingName: bindingNames[idx],
		Conditions:           conds,
	}
}

// The builders below return the conditions that the controller sets on staged update runs, with only the type,
// the status, and the reason set; the status checkers fill in the observed generations, and ignore the last
// transition times and the messages.

// stagedUpdateRunInitializedCond returns the Initialized condition of a successfully initialized staged update run.
func stagedUpdateRunInitializedCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunCondTypeInitialized,
		Status: metav1.ConditionTrue,
		Reason: rolloutv1alpha1.StagedUpdateRunInitializedCondReasonPreppedResourceSnapshotAndAllStages,
	}
}

// stagedUpdateRunStartedCond returns the Started condition of a staged update run.
func stagedUpdateRunStartedCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunCondTypeStarted,
		Status: metav1.ConditionTrue,
		Reason: rolloutv1alpha1.StagedUpdateRunStartedCondReasonUpdateStarted,
	}
}

// stagedUpdateRunSuspendedCond returns the Suspended condition of a suspended staged update run.
func stagedUpdateRunSuspendedCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunCondTypeSuspended,
		Status: metav1.ConditionTrue,
		Reason: rolloutv1alpha1.StagedUpdateRunSuspendedCondReasonSuspended,
	}
}

// stagedUpdateRunResumedCond returns the Suspended condition of a staged update run that has been resumed.
func stagedUpdateRunResumedCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunCondTypeSuspended,
		Status: metav1.ConditionFalse,
		Reason: rolloutv1alpha1.StagedUpdateRunSuspendedCondReasonResumed,
	}
}

// stagedUpdateRunCompletedCond returns the Completed condition of a staged update run with the given reason.
func stagedUpdateRunCompletedCond(reason string) metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunCondTypeCompleted,
		Status: metav1.ConditionTrue,
		Reason: reason,
	}
}

// perStageStartedCond returns the Started condition of a stage.
func perStageStartedCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunPerStageCondTypeStarted,
		Status: metav1.ConditionTrue,
		Reason: rolloutv1alpha1.StagedUpdateRunPerStageStartedCondReasonUpdateStarted,
	}
}

// perStageCompletedCond returns the Completed condition of a stage with the given reason.
func perStageCompletedCond(reason string) metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted,
		Status: metav1.ConditionTrue,
		Reason: reason,
	}
}

// perClusterStartedCond returns the Started condition of a cluster.
func perClusterStartedCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted,
		Status: metav1.ConditionTrue,
		Reason: rolloutv1alpha1.StagedUpdateRunPerClusterStartedCondReasonUpdateStarted,
	}
}

// perClusterCompletedCond returns the Completed condition of a cluster with the given reason.
func perClusterCompletedCond(reason string) metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted,
		Status: metav1.ConditionTrue,
		Reason: reason,
	}
}

// perClusterUpdateSucceededConds returns the conditions of a cluster that has been successfully updated.
func perClusterUpdateSucceededConds() []metav1.Condition {
	return []metav1.Condition{
		perClusterStartedCond(),
		perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded),
	}
}

// perClusterUpdateFailedConds returns the conditions of a cluster that has failed to be updated after the update
// started.
func perClusterUpdateFailedConds() []metav1.Condition {
	return []metav1.Condition{
		perClusterStartedCond(),
		perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed),
	}
}

// perStageTaskTimedWaitStartedCond returns the TimedWaitStarted condition of a TimedWait stage task.
func perStageTaskTimedWaitStartedCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunTaskCondTypeTimedWaitStarted,
		Status: metav1.ConditionTrue,
		Reason: rolloutv1alpha1.StagedUpdateRunTaskTimedWaitStartedCondReasonTimerStarted,
	}
}

// perStageTaskWaitTimeElapsedCond returns the WaitTimeElapsed condition of a TimedWait stage task.
func perStageTaskWaitTimeElapsedCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunTaskCondTypeWaitTimeElapsed,
		Status: metav1.ConditionTrue,
		Reason: rolloutv1alpha1.StagedUpdateRunTaskWaitTimeElapsedCondReasonTimerElapsed,
	}
}

// perStageTaskApprovalRequestCreatedCond returns the ApprovalRequestCreated condition of an Approval stage task whose
// approval request has been created.
func perStageTaskApprovalRequestCreatedCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestCreated,
		Status: metav1.ConditionTrue,
		Reason: rolloutv1alpha1.StagedUpdateRunTaskApprovalRequestCreatedCondReasonCreated,
	}
}

// perStageTaskApprovalRequestApprovedCond returns the ApprovalRequestApproved condition of an Approval stage task
// whose approval request has been approved.
func perStageTaskApprovalRequestApprovedCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestApproved,
		Status: metav1.ConditionTrue,
		Reason: rolloutv1alpha1.StagedUpdateRunTaskApprovalRequestApprovedCondReasonApproved,
	}
}

// perStageTaskApprovalRequestOwnershipMismatchCond returns the ApprovalRequestCreated condition of an Approval stage
// task whose approval request exists but is not owned by the staged update run.
func perStageTaskApprovalRequestOwnershipMismatchCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestCreated,
		Status: metav1.ConditionFalse,
		Reason: rolloutv1alpha1.StagedUpdateRunTaskApprovalRequestCreatedCondReasonOwnershipMismatch,
	}
}

// approvalRequestApprovedCond returns the Approved condition of an approval request with the given status, as the
// test cases set it.
func approvalRequestApprovedCond(status metav1.ConditionStatus) metav1.Condition {
	reason := markedAsApprovedReason
	if status != metav1.ConditionTrue {
		reason = markedAsRejectedReason
	}
	return metav1.Condition{
		Type:   rolloutv1alpha1.ApprovalRequestCondTypeApproved,
		Status: status,
		Reason: reason,
	}
}

// approvalRequestApprovalAcceptedCond returns the ApprovalAccepted condition that the controller sets on an approval
// request once it accepts the approval.
func approvalRequestApprovalAcceptedCond() metav1.Condition {
	return metav1.Condition{
		Type:   rolloutv1alpha1.ApprovalRequestCondTypeApprovalAccepted,
		Status: metav1.ConditionTrue,
		Reason: rolloutv1alpha1.ApprovalRequestApprovalAcceptedCondReasonAccepted,
	}
}

// wantApprovalTaskStatus builds the status of an Approval stage task with the given approval request name and
// conditions.
func wantApprovalTaskStatus(approvalRequestName string, conds ...metav1.Condition) rolloutv1alpha1.PerStageTaskStatus {
	return rolloutv1alpha1.PerStageTaskStatus{
		Type:                rolloutv1alpha1.StageTaskTypeApproval,
		ApprovalRequestName: approvalRequestName,
		Conditions:          conds,
	}
}

// wantTimedWaitTaskStatus builds the status of a TimedWait stage task with the given wait time and conditions.
func wantTimedWaitTaskStatus(waitTime time.Duration, conds ...metav1.Condition) rolloutv1alpha1.PerStageTaskStatus {
	return rolloutv1alpha1.PerStageTaskStatus{
		Type:       rolloutv1alpha1.StageTaskTypeTimedWait,
		WaitTime:   &metav1.Duration{Duration: waitTime},
		Conditions: conds,
	}
}

// perStageSucceededConds returns the conditions of a stage that has completed successfully.
func perStageSucceededConds() []metav1.Condition {
	return []metav1.Condition{
		perStageStartedCond(),
		perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonSucceeded),
	}
}

// wantStagedUpdateRunStatusInitializedAndSuspended builds the status that a staged update run created with the
// suspended flag set is expected to have, once it has been initialized with the given resource snapshot and stages.
func wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName string, stages []rolloutv1alpha1.PerStageStatus) *rolloutv1alpha1.StagedUpdateRunStatus {
	return &rolloutv1alpha1.StagedUpdateRunStatus{
		Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunSuspendedCond()},
		ResourceSnapshotNameToRollout: snapshotName,
		Stages:                        stages,
	}
}

// setObservedGenerationOnAllConditions sets the observed generation of all the conditions in a staged update run
// status (run-level, per-stage, per-cluster, and per-task ones) to the given generation.
func setObservedGenerationOnAllConditions(status *rolloutv1alpha1.StagedUpdateRunStatus, generation int64) {
	setObsGen := func(conds []metav1.Condition) {
		for idx := range conds {
			conds[idx].ObservedGeneration = generation
		}
	}

	setObsGen(status.Conditions)
	for stageIdx := range status.Stages {
		stage := &status.Stages[stageIdx]
		setObsGen(stage.Conditions)
		for clusterIdx := range stage.Clusters {
			setObsGen(stage.Clusters[clusterIdx].Conditions)
		}
		for taskIdx := range stage.BeforeStageTasks {
			setObsGen(stage.BeforeStageTasks[taskIdx].Conditions)
		}
		for taskIdx := range stage.AfterStageTasks {
			setObsGen(stage.AfterStageTasks[taskIdx].Conditions)
		}
	}
}

// stagedUpdateRunStatusUpdatedActual verifies that the status of a staged update run matches the given one; the
// observed generations of all the conditions are expected to match the current generation of the run.
func stagedUpdateRunStatusUpdatedActual(runNSName, runName string, wantStatus *rolloutv1alpha1.StagedUpdateRunStatus) func() error {
	return stagedUpdateRunStatusMatchesActual(runNSName, runName, wantStatus, true)
}

// stagedUpdateRunRolloutStatusActual is the same as stagedUpdateRunStatusUpdatedActual, except that it ignores the
// timestamps of the stages, which are set as a rollout progresses; see stagedUpdateRunStagesCompletedInOrderActual
// for checking the timestamps.
func stagedUpdateRunRolloutStatusActual(runNSName, runName string, wantStatus *rolloutv1alpha1.StagedUpdateRunStatus) func() error {
	return stagedUpdateRunStatusMatchesActual(runNSName, runName, wantStatus, true, ignoreFieldsPerStageStatusTimestamps)
}

// stagedUpdateRunRolloutStatusAsIsActual is the same as stagedUpdateRunRolloutStatusActual, except that it compares
// the observed generations of the conditions as given in the expected status (see observedAt), rather than expecting
// all of them to match the current generation of the run; this is useful once the generation of a run has changed
// (e.g., after it is suspended), as the controller refreshes the observed generations of some conditions only.
func stagedUpdateRunRolloutStatusAsIsActual(runNSName, runName string, wantStatus *rolloutv1alpha1.StagedUpdateRunStatus) func() error {
	return stagedUpdateRunStatusMatchesActual(runNSName, runName, wantStatus, false, ignoreFieldsPerStageStatusTimestamps)
}

// observedAt returns a copy of the given condition, observed at the given generation.
func observedAt(generation int64, cond metav1.Condition) metav1.Condition {
	cond.ObservedGeneration = generation
	return cond
}

// stagedUpdateRunStatusMatchesActual verifies that the status of a staged update run matches the given one, with
// the given additional comparison options. If setObservedGenerations is true, the observed generations of all the
// conditions are expected to match the current generation of the run; otherwise, they are compared as given.
func stagedUpdateRunStatusMatchesActual(
	runNSName, runName string,
	wantStatus *rolloutv1alpha1.StagedUpdateRunStatus,
	setObservedGenerations bool,
	additionalCmpOpts ...cmp.Option,
) func() error {
	return func() error {
		// Retrieve the staged update run.
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}

		// Prepare the expected status; update the conditions with the observed generation if applicable.
		wantStatusWithObsGen := wantStatus.DeepCopy()
		if setObservedGenerations {
			setObservedGenerationOnAllConditions(wantStatusWithObsGen, run.GetGeneration())
		}

		// Check that the staged update run status has been updated as expected.
		cmpOpts := append([]cmp.Option{
			ignoreFieldConditionLTTMsg,
			cmpopts.SortSlices(lessFuncCondition),
			cmpopts.EquateEmpty(),
		}, additionalCmpOpts...)
		if diff := cmp.Diff(*run.GetStatus(), *wantStatusWithObsGen, cmpOpts...); diff != "" {
			return fmt.Errorf("staged update run status diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// stagedUpdateRunStageLastedAtLeastActual verifies that a stage of a staged update run has started and completed,
// and lasted at least the given duration, as its timestamps show.
//
// Note that the timestamps have a precision of seconds; as the controller computes elapsed wait times against
// persisted (i.e., truncated) timestamps as well, a stage that waits for a given duration still lasts at least that
// long by its timestamps.
func stagedUpdateRunStageLastedAtLeastActual(runNSName, runName, stageName string, minDuration time.Duration) func() error {
	return func() error {
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}

		for _, stage := range run.GetStatus().Stages {
			if stage.StageName != stageName {
				continue
			}
			if stage.StartedTimestamp == nil || stage.CompletedTimestamp == nil {
				return fmt.Errorf("stage %s has not started or has not completed yet (started: %v, completed: %v)",
					stageName, stage.StartedTimestamp, stage.CompletedTimestamp)
			}
			if lasted := stage.CompletedTimestamp.Sub(stage.StartedTimestamp.Time); lasted < minDuration {
				return fmt.Errorf("stage %s lasted %v, want at least %v", stageName, lasted, minDuration)
			}
			return nil
		}
		return fmt.Errorf("stage %s is not found in the staged update run", stageName)
	}
}

// stagedUpdateRunStagesCompletedInOrderActual verifies that all the stages of a staged update run have started and
// completed, one after another, as their timestamps show.
//
// Note that the timestamps have a precision of seconds; a stage might start in the same second as the previous one
// completes.
func stagedUpdateRunStagesCompletedInOrderActual(runNSName, runName string) func() error {
	return func() error {
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}

		var prevStageCompletedTimestamp *metav1.Time
		for _, stage := range run.GetStatus().Stages {
			if stage.StartedTimestamp == nil || stage.CompletedTimestamp == nil {
				return fmt.Errorf("stage %s has not started or has not completed yet (started: %v, completed: %v)",
					stage.StageName, stage.StartedTimestamp, stage.CompletedTimestamp)
			}
			if stage.CompletedTimestamp.Before(stage.StartedTimestamp) {
				return fmt.Errorf("stage %s completed (%v) before it started (%v)",
					stage.StageName, stage.CompletedTimestamp, stage.StartedTimestamp)
			}
			if prevStageCompletedTimestamp != nil && stage.StartedTimestamp.Before(prevStageCompletedTimestamp) {
				return fmt.Errorf("stage %s started (%v) before the previous stage completed (%v)",
					stage.StageName, stage.StartedTimestamp, prevStageCompletedTimestamp)
			}
			prevStageCompletedTimestamp = stage.CompletedTimestamp
		}
		return nil
	}
}

// stagedUpdateRunFailedToInitializeActual verifies that a staged update run has failed to initialize, i.e., its status
// has only the Initialized condition set to False, and the message of the condition contains the given substring
// (so that test cases can tell the causes of failures apart).
func stagedUpdateRunFailedToInitializeActual(runNSName, runName, wantMsgSubstr string) func() error {
	wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
		Conditions: []metav1.Condition{
			{
				Type:   rolloutv1alpha1.StagedUpdateRunCondTypeInitialized,
				Status: metav1.ConditionFalse,
				Reason: rolloutv1alpha1.StagedUpdateRunInitializedCondReasonFailed,
			},
		},
	}
	statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(runNSName, runName, wantStatus)

	return func() error {
		if err := statusUpdatedActual(); err != nil {
			return err
		}

		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}
		initCond := meta.FindStatusCondition(run.GetStatus().Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeInitialized)
		if initCond == nil || !strings.Contains(initCond.Message, wantMsgSubstr) {
			return fmt.Errorf("the Initialized condition (%+v) does not have a message that contains %q", initCond, wantMsgSubstr)
		}
		return nil
	}
}

// stagedUpdateRunConditionActual verifies that a staged update run has the given run-level condition, observed at
// the current generation of the run.
func stagedUpdateRunConditionActual(runNSName, runName string, wantCond metav1.Condition) func() error {
	return func() error {
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}

		wantCondWithObsGen := wantCond.DeepCopy()
		wantCondWithObsGen.ObservedGeneration = run.GetGeneration()
		gotCond := meta.FindStatusCondition(run.GetStatus().Conditions, wantCond.Type)
		if diff := cmp.Diff(gotCond, wantCondWithObsGen, ignoreFieldConditionLTTMsg); diff != "" {
			return fmt.Errorf("staged update run condition %s diff (-got +want):\n%s", wantCond.Type, diff)
		}
		return nil
	}
}

// stagedUpdateRunStageLayoutActual verifies the layout of the stages in a staged update run, i.e., the stages, their
// clusters and tasks, and the resolved max concurrency; the progress information (conditions, timestamps, and
// approval request names) is ignored.
func stagedUpdateRunStageLayoutActual(runNSName, runName string, wantStages []rolloutv1alpha1.PerStageStatus) func() error {
	return func() error {
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}

		if diff := cmp.Diff(
			run.GetStatus().Stages, wantStages,
			ignoreFieldsPerStageStatusProgress,
			ignoreFieldsPerClusterStatusProgress,
			ignoreFieldsPerStageTaskProgress,
			cmpopts.EquateEmpty(),
		); diff != "" {
			return fmt.Errorf("staged update run stages diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// stagedUpdateRunCleanupFinalizerRemovedActual verifies that a staged update run does not have the cleanup finalizer.
func stagedUpdateRunCleanupFinalizerRemovedActual(runNSName, runName string) func() error {
	return func() error {
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}
		if controllerutil.ContainsFinalizer(run, stagedUpdateRunCleanupFinalizer) {
			return fmt.Errorf("the cleanup finalizer has not been removed yet")
		}
		return nil
	}
}

// stagedUpdateRunCleanupFinalizerAddedActual verifies that a staged update run has the cleanup finalizer.
func stagedUpdateRunCleanupFinalizerAddedActual(runNSName, runName string) func() error {
	return func() error {
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}
		if !controllerutil.ContainsFinalizer(run, stagedUpdateRunCleanupFinalizer) {
			return fmt.Errorf("the cleanup finalizer has not been added yet")
		}
		return nil
	}
}

// wantStagedUpdateRunObjectRef returns the reference to a staged update run (cluster-scoped if the namespace is
// empty) that the controller registers in the binding manager claim of a placement policy.
func wantStagedUpdateRunObjectRef(runNSName, runName string) placementv1alpha1.ObjectReference {
	return placementv1alpha1.ObjectReference{
		APIGroup:   rolloutv1alpha1.GroupVersion.Group,
		APIVersion: rolloutv1alpha1.GroupVersion.Version,
		Kind:       stagedUpdateRunKind(runNSName),
		Namespace:  runNSName,
		Name:       runName,
	}
}

// bindingManagerRoleHeldActual verifies that the staged update run controller holds the binding manager role of a
// placement policy, on behalf of exactly the given staged update runs (in any order).
func bindingManagerRoleHeldActual(policyNSName, policyName string, wantObjectRefs []placementv1alpha1.ObjectReference) func() error {
	return bindingManagerActual(policyNSName, policyName, &placementv1alpha1.BindingManager{
		ControllerName: controllerName,
		ObjectRefs:     wantObjectRefs,
	})
}

// bindingManagerActual verifies that the binding manager of a placement policy matches the given one (nil if no one
// holds the role); the object references may come in any order.
func bindingManagerActual(policyNSName, policyName string, wantBindingManager *placementv1alpha1.BindingManager) func() error {
	return func() error {
		policy := newPlacementPolicyObj(policyNSName, policyName)
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: policyNSName, Name: policyName}, policy); err != nil {
			return fmt.Errorf("failed to retrieve the placement policy: %w", err)
		}

		if diff := cmp.Diff(
			policy.GetStatus().BindingManager, wantBindingManager,
			cmpopts.SortSlices(lessFuncObjectRef),
		); diff != "" {
			return fmt.Errorf("binding manager diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// updatePlacementPolicyBindingManager sets the binding manager of a placement policy to the given one (nil to clear
// it), e.g., to simulate another controller holding (or releasing) the role.
//
// Note that the function does not wait for the cache-backed client (which the controller shares) to observe the
// change, as the controller may act on it right away (e.g., claim a released role).
func updatePlacementPolicyBindingManager(policyNSName, policyName string, bindingManager *placementv1alpha1.BindingManager) {
	Eventually(func() error {
		policy := newPlacementPolicyObj(policyNSName, policyName)
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: policyNSName, Name: policyName}, policy); err != nil {
			return fmt.Errorf("failed to retrieve the placement policy: %w", err)
		}
		policy.GetStatus().BindingManager = bindingManager.DeepCopy()
		return hubClient.Status().Update(ctx, policy)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the binding manager of the placement policy")
}

// wantStagedUpdateRunOwnerRefs returns the owner references that the controller sets on the objects (e.g., approval
// requests) that a staged update run (cluster-scoped if the namespace is empty) owns.
func wantStagedUpdateRunOwnerRefs(runNSName, runName string, runUID types.UID) []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         rolloutv1alpha1.GroupVersion.String(),
			Kind:               stagedUpdateRunKind(runNSName),
			Name:               runName,
			UID:                runUID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
}

// stagedUpdateRunConditionMessageActual verifies that the run-level condition of the given type of a staged update
// run has a message that contains the given substring.
func stagedUpdateRunConditionMessageActual(runNSName, runName, condType, wantMsgSubstr string) func() error {
	return func() error {
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}
		cond := meta.FindStatusCondition(run.GetStatus().Conditions, condType)
		if cond == nil || !strings.Contains(cond.Message, wantMsgSubstr) {
			return fmt.Errorf("the %s condition (%+v) does not have a message that contains %q", condType, cond, wantMsgSubstr)
		}
		return nil
	}
}

// bindingManagerRoleReleasedActual verifies that no one holds the binding manager role of a placement policy.
func bindingManagerRoleReleasedActual(policyNSName, policyName string) func() error {
	return func() error {
		policy := newPlacementPolicyObj(policyNSName, policyName)
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: policyNSName, Name: policyName}, policy); err != nil {
			return fmt.Errorf("failed to retrieve the placement policy: %w", err)
		}
		if bindingManager := policy.GetStatus().BindingManager; bindingManager != nil {
			return fmt.Errorf("the binding manager role is still held: %+v", *bindingManager)
		}
		return nil
	}
}

// placementBindingResourceSnapshotActual verifies that a placement binding points to the given resource snapshot.
func placementBindingResourceSnapshotActual(bindingNS, bindingName, wantSnapshotName string) func() error {
	return func() error {
		binding := newPlacementBindingObj(bindingNS, bindingName)
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: bindingNS, Name: bindingName}, binding); err != nil {
			return fmt.Errorf("failed to retrieve the placement binding: %w", err)
		}
		if gotSnapshotName := binding.GetSpec().ResourceSnapshotName; gotSnapshotName != wantSnapshotName {
			return fmt.Errorf("placement binding %s points to resource snapshot %s, want %s", bindingName, gotSnapshotName, wantSnapshotName)
		}
		return nil
	}
}

// placementBindingsResourceSnapshotActual verifies that all the given placement bindings point to the given resource
// snapshot.
func placementBindingsResourceSnapshotActual(bindingNS string, bindingNames []string, wantSnapshotName string) func() error {
	return func() error {
		for _, bindingName := range bindingNames {
			if err := placementBindingResourceSnapshotActual(bindingNS, bindingName, wantSnapshotName)(); err != nil {
				return err
			}
		}
		return nil
	}
}

// placementBindingGenerationActual verifies that a placement binding is at the given generation, e.g., that its
// spec has not been updated since it was created.
func placementBindingGenerationActual(bindingNS, bindingName string, wantGeneration int64) func() error {
	return func() error {
		binding := newPlacementBindingObj(bindingNS, bindingName)
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: bindingNS, Name: bindingName}, binding); err != nil {
			return fmt.Errorf("failed to retrieve the placement binding: %w", err)
		}
		if gotGeneration := binding.GetGeneration(); gotGeneration != wantGeneration {
			return fmt.Errorf("placement binding %s is at generation %d, want %d", bindingName, gotGeneration, wantGeneration)
		}
		return nil
	}
}

// getPerClusterCondition retrieves the condition of the given type (nil if absent) of a cluster in a staged update run.
func getPerClusterCondition(runNSName, runName, clusterName, condType string) (*metav1.Condition, error) {
	run, err := getStagedUpdateRun(runNSName, runName)
	if err != nil {
		return nil, err
	}
	for _, stage := range run.GetStatus().Stages {
		for _, cluster := range stage.Clusters {
			if cluster.ClusterName == clusterName {
				return meta.FindStatusCondition(cluster.Conditions, condType), nil
			}
		}
	}
	return nil, fmt.Errorf("cluster %s is not found in the staged update run", clusterName)
}

// perClusterConditionMessageActual verifies that the condition of the given type of a cluster in a staged update run
// has a message that contains the given substring (so that test cases can tell the causes of outcomes apart).
func perClusterConditionMessageActual(runNSName, runName, clusterName, condType, wantMsgSubstr string) func() error {
	return func() error {
		cond, err := getPerClusterCondition(runNSName, runName, clusterName, condType)
		if err != nil {
			return err
		}
		if cond == nil || !strings.Contains(cond.Message, wantMsgSubstr) {
			return fmt.Errorf("the %s condition (%+v) of cluster %s does not have a message that contains %q", condType, cond, clusterName, wantMsgSubstr)
		}
		return nil
	}
}

// perStageConditionMessageActual verifies that the condition of the given type of a stage in a staged update run
// has a message that contains the given substring.
func perStageConditionMessageActual(runNSName, runName, stageName, condType, wantMsgSubstr string) func() error {
	return func() error {
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}
		for _, stage := range run.GetStatus().Stages {
			if stage.StageName != stageName {
				continue
			}
			cond := meta.FindStatusCondition(stage.Conditions, condType)
			if cond == nil || !strings.Contains(cond.Message, wantMsgSubstr) {
				return fmt.Errorf("the %s condition (%+v) of stage %s does not have a message that contains %q", condType, cond, stageName, wantMsgSubstr)
			}
			return nil
		}
		return fmt.Errorf("stage %s is not found in the staged update run", stageName)
	}
}

// approvalRequestCreatedActual verifies that an approval request has been created for the given stage of a staged
// update run, i.e., its spec references the run and the stage, and the run is its controller (owner).
//
// The approval request is in the namespace of the staged update run (i.e., it is a ClusterApprovalRequest object if
// the staged update run is cluster-scoped).
func approvalRequestCreatedActual(runNSName, runName, requestName, wantStageName string) func() error {
	return func() error {
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}

		request := newApprovalRequestObj(runNSName, requestName)
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: runNSName, Name: requestName}, request); err != nil {
			return fmt.Errorf("failed to retrieve the approval request: %w", err)
		}

		wantSpec := rolloutv1alpha1.ApprovalRequestSpec{
			StagedUpdateRunName: runName,
			StageName:           wantStageName,
		}
		if diff := cmp.Diff(*request.GetSpec(), wantSpec); diff != "" {
			return fmt.Errorf("approval request spec diff (-got +want):\n%s", diff)
		}

		wantOwnerRefs := wantStagedUpdateRunOwnerRefs(runNSName, runName, run.GetUID())
		if diff := cmp.Diff(request.GetOwnerReferences(), wantOwnerRefs); diff != "" {
			return fmt.Errorf("approval request owner references diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// approvalRequestOwnerRefsActual verifies that an approval request (cluster-scoped if the namespace is empty) exists
// with exactly the given owner references.
func approvalRequestOwnerRefsActual(requestNS, requestName string, wantOwnerRefs []metav1.OwnerReference) func() error {
	return func() error {
		request, err := getApprovalRequest(requestNS, requestName)
		if err != nil {
			return err
		}
		if diff := cmp.Diff(request.GetOwnerReferences(), wantOwnerRefs); diff != "" {
			return fmt.Errorf("approval request owner references diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// approvalRequestApprovalAcceptedActual verifies that the controller has accepted the approval of an approval request
// (cluster-scoped if the namespace is empty), i.e., the request has both the Approved condition (as set by the test
// case) and the ApprovalAccepted condition set to True, observed at its current generation.
func approvalRequestApprovalAcceptedActual(requestNS, requestName string) func() error {
	return approvalRequestConditionsActual(requestNS, requestName, []metav1.Condition{
		approvalRequestApprovedCond(metav1.ConditionTrue),
		approvalRequestApprovalAcceptedCond(),
	})
}

// approvalRequestConditionsActual verifies that an approval request (cluster-scoped if the namespace is empty) has
// exactly the given conditions, observed at its current generation.
func approvalRequestConditionsActual(requestNS, requestName string, wantConds []metav1.Condition) func() error {
	return func() error {
		request, err := getApprovalRequest(requestNS, requestName)
		if err != nil {
			return err
		}

		wantCondsWithObsGen := make([]metav1.Condition, 0, len(wantConds))
		for _, cond := range wantConds {
			cond.ObservedGeneration = request.GetGeneration()
			wantCondsWithObsGen = append(wantCondsWithObsGen, cond)
		}
		if diff := cmp.Diff(
			request.GetStatus().Conditions, wantCondsWithObsGen,
			ignoreFieldConditionLTTMsg,
			cmpopts.SortSlices(lessFuncCondition),
			cmpopts.EquateEmpty(),
		); diff != "" {
			return fmt.Errorf("approval request conditions diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// approvalRequestAbsentActual verifies that an approval request (cluster-scoped if the namespace is empty) does not
// exist.
func approvalRequestAbsentActual(requestNS, requestName string) func() error {
	return func() error {
		request := newApprovalRequestObj(requestNS, requestName)
		err := hubClient.Get(ctx, client.ObjectKey{Namespace: requestNS, Name: requestName}, request)
		switch {
		case err == nil:
			return fmt.Errorf("approval request %s still exists", requestName)
		case !apierrors.IsNotFound(err):
			return fmt.Errorf("failed to retrieve approval request %s: %w", requestName, err)
		}
		return nil
	}
}

// approvalRequestReplacedActual verifies that an approval request (cluster-scoped if the namespace is empty) exists,
// and is not the object of the given UID, i.e., it has been deleted and re-created.
func approvalRequestReplacedActual(requestNS, requestName string, oldUID types.UID) func() error {
	return func() error {
		request, err := getApprovalRequest(requestNS, requestName)
		if err != nil {
			return err
		}
		if request.GetUID() == oldUID {
			return fmt.Errorf("approval request %s has not been replaced (UID: %s)", requestName, oldUID)
		}
		return nil
	}
}

// perStageTaskTimedWaitLastedAtLeastActual verifies that the after-stage TimedWait task of a stage in a staged update
// run has started and elapsed, at least the given duration apart (as the last transition times of its conditions
// show), and that the stage has completed no earlier than the wait time elapsed.
func perStageTaskTimedWaitLastedAtLeastActual(runNSName, runName, stageName string, minDuration time.Duration) func() error {
	return func() error {
		run, err := getStagedUpdateRun(runNSName, runName)
		if err != nil {
			return err
		}

		for _, stage := range run.GetStatus().Stages {
			if stage.StageName != stageName {
				continue
			}
			for _, task := range stage.AfterStageTasks {
				if task.Type != rolloutv1alpha1.StageTaskTypeTimedWait {
					continue
				}
				startedCond := meta.FindStatusCondition(task.Conditions, rolloutv1alpha1.StagedUpdateRunTaskCondTypeTimedWaitStarted)
				elapsedCond := meta.FindStatusCondition(task.Conditions, rolloutv1alpha1.StagedUpdateRunTaskCondTypeWaitTimeElapsed)
				if startedCond == nil || elapsedCond == nil {
					return fmt.Errorf("the timed wait task of stage %s has not started or its wait time has not elapsed yet (started: %+v, elapsed: %+v)",
						stageName, startedCond, elapsedCond)
				}
				if waited := elapsedCond.LastTransitionTime.Sub(startedCond.LastTransitionTime.Time); waited < minDuration {
					return fmt.Errorf("the timed wait task of stage %s waited for %v, want at least %v", stageName, waited, minDuration)
				}
				if stage.CompletedTimestamp == nil || stage.CompletedTimestamp.Before(&elapsedCond.LastTransitionTime) {
					return fmt.Errorf("stage %s has not completed after the wait time elapsed (completed: %v, elapsed: %v)",
						stageName, stage.CompletedTimestamp, elapsedCond.LastTransitionTime)
				}
				return nil
			}
			return fmt.Errorf("stage %s has no after-stage timed wait task", stageName)
		}
		return fmt.Errorf("stage %s is not found in the staged update run", stageName)
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

// removeObjects deletes the given objects, and waits until all of them are gone.
//
// The envtest environment does not run the garbage collector; test cases must remove all the objects they create
// (including those created by the controller on their behalf) explicitly.
//
// Approval requests are removed last, only after all the other objects (staged update runs in particular) are gone:
// the deletion of an approval request makes the controller process the staged update run that the request
// references, and a run that is still around (as far as the cache is concerned) might have its approval request
// re-created, which would then never go away.
func removeObjects(objs ...client.Object) {
	approvalRequests := make([]client.Object, 0, len(objs))
	others := make([]client.Object, 0, len(objs))
	for _, obj := range objs {
		switch obj.(type) {
		case *rolloutv1alpha1.ApprovalRequest, *rolloutv1alpha1.ClusterApprovalRequest:
			approvalRequests = append(approvalRequests, obj)
		default:
			others = append(others, obj)
		}
	}

	deleteObjectsAndWaitForRemoval(others)
	deleteObjectsAndWaitForRemoval(approvalRequests)
}

// deleteObjectsAndWaitForRemoval deletes the given objects, and waits until all of them are gone. All the deletions
// are issued before any wait, so that the cleanup does not take one polling interval per object.
func deleteObjectsAndWaitForRemoval(objs []client.Object) {
	for _, obj := range objs {
		Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T %s", obj, client.ObjectKeyFromObject(obj))
	}
	for _, obj := range objs {
		Eventually(objectRemovedActual(obj), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove object %T %s", obj, client.ObjectKeyFromObject(obj))
	}
}
