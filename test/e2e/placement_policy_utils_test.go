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
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kfrolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

// The helpers in this file work with both scopes of the placement policy APIs: a key with an empty
// namespace refers to a ClusterPlacementPolicy, and one with a namespace to a PlacementPolicy.

// clusterSelectorCountAll is the cluster selector count that selects every matching cluster.
const clusterSelectorCountAll = "All"

// placementPolicyCmpOptions compares placement policy and binding statuses, ignoring the condition fields
// that carry no contract (transition times and messages).
var placementPolicyCmpOptions = cmp.Options{
	cmpopts.SortSlices(lessFuncCondition),
	utils.IgnoreConditionLTTAndMessageFields,
	cmpopts.EquateEmpty(),
}

// newPlacementPolicyObject returns a PlacementPolicy or a ClusterPlacementPolicy, depending on the key.
func newPlacementPolicyObject(key types.NamespacedName, spec kfplacementv1alpha1.PlacementPolicySpec) kfplacementv1alpha1.PlacementPolicyAccessor {
	if key.Namespace == "" {
		return &kfplacementv1alpha1.ClusterPlacementPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name},
			Spec:       spec,
		}
	}
	return &kfplacementv1alpha1.PlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Spec:       spec,
	}
}

// retrievePlacementPolicy reads a PlacementPolicy or a ClusterPlacementPolicy, depending on the key.
func retrievePlacementPolicy(key types.NamespacedName) (kfplacementv1alpha1.PlacementPolicyAccessor, error) {
	policy := newPlacementPolicyObject(key, kfplacementv1alpha1.PlacementPolicySpec{})
	if err := hubClient.Get(ctx, key, policy); err != nil {
		return nil, err
	}
	return policy, nil
}

// clusterSelectorMatchingLabels builds a cluster selector with a single term that matches the given labels.
func clusterSelectorMatchingLabels(matchLabels map[string]string, count intstr.IntOrString) kfplacementv1alpha1.ClusterSelector {
	return kfplacementv1alpha1.ClusterSelector{
		Terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: matchLabels},
		},
		Count: ptr.To(count),
	}
}

// noTermsSelectorsForAllMemberClusters returns, keyed by cluster, how every member cluster's binding records the
// cluster selectors of a policy that has none: as a single selector without terms.
func noTermsSelectorsForAllMemberClusters() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
	selectorsByCluster := make(map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, len(allMemberClusterNames))
	for _, clusterName := range allMemberClusterNames {
		selectorsByCluster[clusterName] = []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}}
	}
	return selectorsByCluster
}

// clusterSelectorTermsMatchingLabels returns how a binding records a cluster selector built by
// clusterSelectorMatchingLabels: its terms, without the counts.
func clusterSelectorTermsMatchingLabels(matchLabels map[string]string) kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
	return kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
		Terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: matchLabels},
		},
	}
}

// clusterSelectorWithTerms builds a cluster selector with the given terms and count.
func clusterSelectorWithTerms(terms []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm, count intstr.IntOrString) kfplacementv1alpha1.ClusterSelector {
	return kfplacementv1alpha1.ClusterSelector{
		Terms: terms,
		Count: ptr.To(count),
	}
}

// sameSelectorsForClusters returns, keyed by cluster, how the binding of each given cluster records the cluster
// selectors of a policy whose only selector, with the given terms, selects them all.
func sameSelectorsForClusters(
	terms []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm,
	clusterNames ...string,
) map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
	selectorsByCluster := make(map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, len(clusterNames))
	for _, clusterName := range clusterNames {
		selectorsByCluster[clusterName] = []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{Terms: terms}}
	}
	return selectorsByCluster
}

// waitForPlacementPolicyPlacedOnlyOn waits until the policy, whose only cluster selector has the given terms, binds
// exactly the given clusters, has the app ConfigMap placed on them and removed from every other member cluster, and
// reports all of them as synchronized and available.
func waitForPlacementPolicyPlacedOnlyOn(
	key types.NamespacedName,
	terms []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm,
	clusterNames ...string,
) {
	GinkgoHelper()

	var bound, unbound []*framework.Cluster
	for _, cluster := range allMemberClusters {
		if slices.Contains(clusterNames, cluster.ClusterName) {
			bound = append(bound, cluster)
		} else {
			unbound = append(unbound, cluster)
		}
	}

	Eventually(allSynchronizedPlacementPolicyBindingsActual(key, sameSelectorsForClusters(terms, clusterNames...), 1), workloadEventuallyDuration, eventuallyInterval).
		Should(Succeed(), "Failed to bind exactly clusters %v for placement policy %s", clusterNames, key)
	Eventually(appConfigMapPlacedOnClustersActual(bound...), workloadEventuallyDuration, eventuallyInterval).
		Should(Succeed(), "Failed to place the ConfigMap on clusters %v", clusterNames)
	Eventually(appConfigMapRemovedFromClustersActual(unbound...), workloadEventuallyDuration, eventuallyInterval).
		Should(Succeed(), "Failed to remove the ConfigMap from the clusters other than %v", clusterNames)
	Eventually(placementPolicyStatusActual(key, allSynchronizedPlacementPolicyStatus(int32(len(clusterNames)))), workloadEventuallyDuration, eventuallyInterval).
		Should(Succeed(), "Failed to report the expected status for placement policy %s", key)
}

// placementPolicyBindingUIDsByCluster returns, keyed by cluster, the UIDs of the bindings that name the policy, so
// that specs can check later that the bindings were kept rather than replaced.
func placementPolicyBindingUIDsByCluster(key types.NamespacedName) map[string]types.UID {
	GinkgoHelper()

	bindings, err := placementPolicyBindingsByCluster(key)
	Expect(err).NotTo(HaveOccurred(), "Failed to list the bindings of placement policy %s", key)
	uids := make(map[string]types.UID, len(bindings))
	for clusterName, binding := range bindings {
		uids[clusterName] = binding.GetUID()
	}
	return uids
}

// waitPastCreationSecond waits until the second after the one in which a binding was created has begun, with a
// margin (the Kind clusters share the host clock).
//
// When a selector picks too many clusters, the placement policy controller trims the newest bindings first, by
// creation timestamp, which has a resolution of one second, and breaks ties by cluster name. Specs that rely on which
// of two bindings is the newer one call this before causing the second binding, so that it is stamped a later second
// than the first.
func waitPastCreationSecond(createdAt metav1.Time) {
	if wait := time.Until(createdAt.Add(time.Second + 250*time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
}

// placementPolicyTolerationsFor returns the tolerations of the taints that buildTaints puts on the given member
// clusters.
func placementPolicyTolerationsFor(clusterNames ...string) []kfplacementv1alpha1.Toleration {
	tolerations := make([]kfplacementv1alpha1.Toleration, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
		tolerations = append(tolerations, kfplacementv1alpha1.Toleration{
			Key:      regionLabelName,
			Operator: corev1.TolerationOpEqual,
			Value:    taintTolerationMap[clusterName][regionLabelName],
			Effect:   corev1.TaintEffectNoSchedule,
		})
	}
	return tolerations
}

// placementPolicyBindingsByCluster returns, keyed by cluster, the bindings that name the policy. Unlike
// placementPolicyBindings, it needs no policy object, so it also works before the policy has a snapshot or after
// it is gone.
func placementPolicyBindingsByCluster(key types.NamespacedName) (map[string]kfplacementv1alpha1.PlacementBindingAccessor, error) {
	bindings, err := placementPolicyBindings(newPlacementPolicyObject(key, kfplacementv1alpha1.PlacementPolicySpec{}))
	if err != nil {
		return nil, err
	}
	byCluster := make(map[string]kfplacementv1alpha1.PlacementBindingAccessor, len(bindings))
	for _, binding := range bindings {
		clusterName := binding.GetSpec().ClusterName
		if _, found := byCluster[clusterName]; found {
			return nil, fmt.Errorf("placement policy %s has more than one binding for cluster %s", key, clusterName)
		}
		byCluster[clusterName] = binding
	}
	return byCluster, nil
}

// waitForSinglePlacementPolicyBinding waits until the policy has exactly one binding, and returns it. Specs use it to
// learn which cluster a ranked pick chose, since the ranking depends on live capacity.
func waitForSinglePlacementPolicyBinding(key types.NamespacedName) kfplacementv1alpha1.PlacementBindingAccessor {
	GinkgoHelper()

	var single kfplacementv1alpha1.PlacementBindingAccessor
	Eventually(func() error {
		bindings, err := placementPolicyBindingsByCluster(key)
		if err != nil {
			return err
		}
		if len(bindings) != 1 {
			return fmt.Errorf("placement policy %s has %d bindings, want 1", key, len(bindings))
		}
		for _, binding := range bindings {
			single = binding
		}
		return nil
	}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind a single cluster for placement policy %s", key)
	return single
}

// otherEastClusterName returns the name of the east member cluster other than the given one.
func otherEastClusterName(clusterName string) string {
	GinkgoHelper()

	switch clusterName {
	case memberCluster1EastProdName:
		return memberCluster2EastCanaryName
	case memberCluster2EastCanaryName:
		return memberCluster1EastProdName
	default:
		Fail(fmt.Sprintf("member cluster %s is not in the east region", clusterName))
		return ""
	}
}

// resourcesChangedPendingRolloutPlacementPolicyStatus returns the status of a policy whose clusters all hold, and have
// synchronized and available, its latest resource revision, while the selected resources on the hub have changed
// since and wait for a rollout.
//
// The latest resource revision name and the observed generations are left for the actual to fill in.
func resourcesChangedPendingRolloutPlacementPolicyStatus(clusterCount int32) kfplacementv1alpha1.PlacementPolicyStatus {
	status := allSynchronizedPlacementPolicyStatus(clusterCount)
	meta.SetStatusCondition(&status.Conditions, placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized,
		metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout))
	return status
}

// resourcesNotCollectedPlacementPolicyStatus returns the status of a policy that has found all of its clusterCount
// desired clusters but has never collected its selected resources, so it has no resource snapshot, no latest
// resource revision, and no binding.
func resourcesNotCollectedPlacementPolicyStatus(clusterCount int32) kfplacementv1alpha1.PlacementPolicyStatus {
	return kfplacementv1alpha1.PlacementPolicyStatus{
		Conditions: []metav1.Condition{
			placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected, metav1.ConditionFalse,
				kfplacementv1alpha1.PlacementPolicyResourceCollectedCondReasonFailedToCollectSomeResources),
			placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeScheduled, metav1.ConditionTrue,
				kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters),
			placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, metav1.ConditionFalse,
				kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonFailedToSynchronizeSomeClusters),
			// Availability is still judged, on the bindings alone, and there are none.
			placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, metav1.ConditionFalse,
				kfplacementv1alpha1.PlacementPolicyAvailableCondReasonSomeClustersUnavailable),
		},
		DesiredClusters:            ptr.To(clusterCount),
		ScheduledClusters:          ptr.To(clusterCount),
		SynchronizedClusters:       ptr.To(int32(0)),
		ResourcesAvailableClusters: ptr.To(int32(0)),
		ActiveClusterClaims:        ptr.To(int32(0)),
	}
}

// someClustersNotSynchronizedPlacementPolicyStatus returns the status of a policy that has found all of its
// clusterCount desired clusters, of which only synchronizedCount have the latest resource revision synchronized
// (and available), while the others failed to apply it.
//
// The latest resource revision name and the observed generations are left for the actual to fill in.
func someClustersNotSynchronizedPlacementPolicyStatus(clusterCount, synchronizedCount int32) kfplacementv1alpha1.PlacementPolicyStatus {
	status := allSynchronizedPlacementPolicyStatus(clusterCount)
	meta.SetStatusCondition(&status.Conditions, placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized,
		metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonFailedToSynchronizeSomeClusters))
	// Availability is judged only on clusters that have the latest revision synchronized.
	meta.SetStatusCondition(&status.Conditions, placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeAvailable,
		metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyAvailableCondReasonSomeClustersUnavailable))
	status.SynchronizedClusters = ptr.To(synchronizedCount)
	status.ResourcesAvailableClusters = ptr.To(synchronizedCount)
	return status
}

// waitForPlacementPolicySynchronized waits until the policy has exactly the wanted bindings, each recording the given
// cluster selectors and reporting all of its resourceCount resources as synchronized and available on the policy's
// latest resource snapshot; it returns the name of that snapshot.
func waitForPlacementPolicySynchronized(
	key types.NamespacedName,
	wantSelectorsByCluster map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly,
	resourceCount int32,
) string {
	GinkgoHelper()

	Eventually(allSynchronizedPlacementPolicyBindingsActual(key, wantSelectorsByCluster, resourceCount), workloadEventuallyDuration, eventuallyInterval).
		Should(Succeed(), "Failed to synchronize the bindings of placement policy %s", key)
	return latestPrimarySnapshotNameOfPlacementPolicy(key)
}

// latestPrimarySnapshotNameOfPlacementPolicy returns the name of the policy's latest primary resource snapshot.
func latestPrimarySnapshotNameOfPlacementPolicy(key types.NamespacedName) string {
	GinkgoHelper()

	policy, err := retrievePlacementPolicy(key)
	Expect(err).NotTo(HaveOccurred(), "Failed to get placement policy %s", key)
	snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
	Expect(err).NotTo(HaveOccurred(), "Failed to find the resource snapshot of placement policy %s", key)
	return snapshot.GetName()
}

// resourceSnapshotNameToRolloutOf returns the resource snapshot that an initialized staged update run (or cluster
// staged update run) rolls out.
func resourceSnapshotNameToRolloutOf(runKey types.NamespacedName) string {
	GinkgoHelper()

	run, err := retrieveStagedUpdateRun(runKey)
	Expect(err).NotTo(HaveOccurred(), "Failed to get staged update run %s", runKey)
	name := run.GetStatus().ResourceSnapshotNameToRollout
	Expect(name).NotTo(BeEmpty(), "Staged update run %s names no resource snapshot to roll out", runKey)
	return name
}

// someClustersOnOlderResourceRevisionPlacementPolicyStatus returns the status of a policy that has found all of its
// clusterCount desired clusters, of which only synchronizedCount have the latest resource revision synchronized (and
// available), while the others are still on an older revision.
//
// The latest resource revision name and the observed generations are left for the actual to fill in.
func someClustersOnOlderResourceRevisionPlacementPolicyStatus(clusterCount, synchronizedCount int32) kfplacementv1alpha1.PlacementPolicyStatus {
	status := someClustersNotSynchronizedPlacementPolicyStatus(clusterCount, synchronizedCount)
	meta.SetStatusCondition(&status.Conditions, placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized,
		metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonSomeClustersOnOlderResourceRevision))
	return status
}

// unfulfilledPlacementPolicyStatus returns the status of a policy that wants desiredCount clusters but has found only
// scheduledCount, with activeClaims cluster claims outstanding for the missing ones. Each found cluster has the
// latest resource revision synchronized and available; with no target cluster at all, nothing is out of sync or
// unavailable either.
//
// The latest resource revision name and the observed generations are left for the actual to fill in.
func unfulfilledPlacementPolicyStatus(desiredCount, scheduledCount, activeClaims int32) kfplacementv1alpha1.PlacementPolicyStatus {
	status := allSynchronizedPlacementPolicyStatus(scheduledCount)
	meta.SetStatusCondition(&status.Conditions, placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeScheduled,
		metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters))
	status.DesiredClusters = ptr.To(desiredCount)
	status.ActiveClusterClaims = ptr.To(activeClaims)
	return status
}

// placementPolicyScheduledCondReasonInvalidClusterSelectors is the Scheduled condition reason of a policy whose
// cluster selectors cannot be evaluated. The API defines no constant for it, so it is spelled out the way the
// placement policy controller reports it.
const placementPolicyScheduledCondReasonInvalidClusterSelectors = "InvalidClusterSelectors"

// otherProdClusterName returns the name of the prod member cluster other than the given one.
func otherProdClusterName(clusterName string) string {
	GinkgoHelper()

	switch clusterName {
	case memberCluster1EastProdName:
		return memberCluster3WestProdName
	case memberCluster3WestProdName:
		return memberCluster1EastProdName
	default:
		Fail(fmt.Sprintf("member cluster %s is not a prod cluster", clusterName))
		return ""
	}
}

// waitForStageApprovalRequest waits until a staged update run (or a cluster staged update run) has created the
// approval request of the approval task of the stage at the given index, before or after the stage, and returns its
// key.
func waitForStageApprovalRequest(runKey types.NamespacedName, stageIdx int, beforeStage bool) types.NamespacedName {
	GinkgoHelper()

	var requestKey types.NamespacedName
	Eventually(func() error {
		run, err := retrieveStagedUpdateRun(runKey)
		if err != nil {
			return err
		}
		stages := run.GetStatus().Stages
		if len(stages) <= stageIdx {
			return fmt.Errorf("staged update run %s has no status for stage %d yet", runKey, stageIdx)
		}
		tasks := stages[stageIdx].AfterStageTasks
		if beforeStage {
			tasks = stages[stageIdx].BeforeStageTasks
		}
		taskIdx := slices.IndexFunc(tasks, func(task kfrolloutv1alpha1.PerStageTaskStatus) bool {
			return task.Type == kfrolloutv1alpha1.StageTaskTypeApproval
		})
		if taskIdx < 0 {
			return fmt.Errorf("staged update run %s has no approval task status for stage %d yet (before stage: %t)", runKey, stageIdx, beforeStage)
		}
		task := tasks[taskIdx]
		if task.ApprovalRequestName == "" ||
			!meta.IsStatusConditionTrue(task.Conditions, kfrolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestCreated) {
			return fmt.Errorf("staged update run %s has not created the approval request for stage %d yet (task status: %+v)", runKey, stageIdx, task)
		}
		// An approval request lives in the scope of its run.
		requestKey = types.NamespacedName{Namespace: runKey.Namespace, Name: task.ApprovalRequestName}
		return nil
	}, placementPolicyRolloutEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the approval request of staged update run %s", runKey)
	return requestKey
}

// updateStagedUpdateRun applies the mutation to a staged update run (or a cluster staged update run), retrying on
// conflicts with the run controller's own writes.
func updateStagedUpdateRun(key types.NamespacedName, mutate func(run kfrolloutv1alpha1.StagedUpdateRunAccessor)) {
	GinkgoHelper()

	Eventually(func() error {
		run, err := retrieveStagedUpdateRun(key)
		if err != nil {
			return err
		}
		mutate(run)
		return hubClient.Update(ctx, run)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update staged update run %s", key)
}

// succeededStagedUpdateRunConditions returns the conditions of a staged update run that has completed successfully.
func succeededStagedUpdateRunConditions() []metav1.Condition {
	return []metav1.Condition{
		placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunCondTypeInitialized, metav1.ConditionTrue,
			kfrolloutv1alpha1.StagedUpdateRunInitializedCondReasonPreppedResourceSnapshotAndAllStages),
		placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunCondTypeStarted, metav1.ConditionTrue,
			kfrolloutv1alpha1.StagedUpdateRunStartedCondReasonUpdateStarted),
		placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunCondTypeCompleted, metav1.ConditionTrue,
			kfrolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
	}
}

// succeededStagedUpdateRunStageConditions returns the conditions of a stage that has completed successfully.
func succeededStagedUpdateRunStageConditions() []metav1.Condition {
	return []metav1.Condition{
		placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunPerStageCondTypeStarted, metav1.ConditionTrue,
			kfrolloutv1alpha1.StagedUpdateRunPerStageStartedCondReasonUpdateStarted),
		placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted, metav1.ConditionTrue,
			kfrolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonSucceeded),
	}
}

// succeededStagedUpdateRunClusterStatus returns the status of a cluster that a staged update run has updated
// successfully through the given binding.
func succeededStagedUpdateRunClusterStatus(clusterName, bindingName string) kfrolloutv1alpha1.PerClusterStatus {
	return kfrolloutv1alpha1.PerClusterStatus{
		ClusterName:          clusterName,
		PlacementBindingName: bindingName,
		Conditions: []metav1.Condition{
			placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted, metav1.ConditionTrue,
				kfrolloutv1alpha1.StagedUpdateRunPerClusterStartedCondReasonUpdateStarted),
			placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, metav1.ConditionTrue,
				kfrolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded),
		},
	}
}

// placementPolicyBindingNamesByCluster returns, keyed by cluster, the names of the bindings that name the policy.
func placementPolicyBindingNamesByCluster(key types.NamespacedName) map[string]string {
	GinkgoHelper()

	bindings, err := placementPolicyBindingsByCluster(key)
	Expect(err).NotTo(HaveOccurred(), "Failed to list the bindings of placement policy %s", key)
	names := make(map[string]string, len(bindings))
	for clusterName, binding := range bindings {
		names[clusterName] = binding.GetName()
	}
	return names
}

// placementPolicyWorks lists the Works on the hub that were generated for the policy's bindings. Works sit in the
// reserved namespaces of the member clusters and record their policy in an owner annotation (the owner label may
// hold a shortened name) and the policy's namespace in an owner label.
func placementPolicyWorks(key types.NamespacedName) ([]kfplacementv1alpha1.Work, error) {
	works := &kfplacementv1alpha1.WorkList{}
	if err := hubClient.List(ctx, works, client.MatchingLabels{kfplacementv1alpha1.WorkOwnerNamespaceLabelKey: key.Namespace}); err != nil {
		return nil, err
	}
	var owned []kfplacementv1alpha1.Work
	for i := range works.Items {
		if works.Items[i].GetAnnotations()[kfplacementv1alpha1.WorkOwnedByPlacementPolicyAnnotationKey] == key.Name {
			owned = append(owned, works.Items[i])
		}
	}
	return owned, nil
}

// placementPolicyWorkNamesByCluster returns, keyed by the member cluster they are for, the names of the Works on the
// hub that were generated for the policy's bindings. An AppliedWork carries the name of its Work.
func placementPolicyWorkNamesByCluster(key types.NamespacedName) (map[string][]string, error) {
	works, err := placementPolicyWorks(key)
	if err != nil {
		return nil, err
	}
	clusterNamesByReservedNamespace := make(map[string]string, len(allMemberClusterNames))
	for _, clusterName := range allMemberClusterNames {
		clusterNamesByReservedNamespace[fmt.Sprintf(utils.NamespaceNameFormat, clusterName)] = clusterName
	}
	namesByCluster := make(map[string][]string, len(allMemberClusterNames))
	for i := range works {
		clusterName, found := clusterNamesByReservedNamespace[works[i].Namespace]
		if !found {
			return nil, fmt.Errorf("work %s of placement policy %s is not in the reserved namespace of a member cluster", client.ObjectKeyFromObject(&works[i]), key)
		}
		namesByCluster[clusterName] = append(namesByCluster[clusterName], works[i].Name)
	}
	return namesByCluster, nil
}

// setPlacementPolicyBindingSuspended suspends or resumes the binding of the policy for the given cluster, retrying on
// conflicts. Users do not normally edit the field; upcoming APIs build on it, and specs set it as a stand-in for them.
func setPlacementPolicyBindingSuspended(key types.NamespacedName, clusterName string, suspended bool) {
	GinkgoHelper()

	Eventually(func() error {
		bindings, err := placementPolicyBindingsByCluster(key)
		if err != nil {
			return err
		}
		binding, found := bindings[clusterName]
		if !found {
			return fmt.Errorf("placement policy %s has no binding for cluster %s", key, clusterName)
		}
		binding.GetSpec().Suspended = suspended
		return hubClient.Update(ctx, binding)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to set suspended=%t on the binding of placement policy %s for cluster %s", suspended, key, clusterName)
}

// memberClusterNamed returns the member cluster of the given name.
func memberClusterNamed(name string) *framework.Cluster {
	GinkgoHelper()

	for _, cluster := range allMemberClusters {
		if cluster.ClusterName == name {
			return cluster
		}
	}
	Fail(fmt.Sprintf("no member cluster is named %s", name))
	return nil
}

// setMemberClusterLabel sets a label on a MemberCluster object on the hub, retrying on conflicts. Specs that call
// it must be Serial and restore the label afterwards, as every spec relies on the labels that the suite sets up.
func setMemberClusterLabel(clusterName, key, value string) {
	GinkgoHelper()

	Eventually(func() error {
		mc := &clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: clusterName}, mc); err != nil {
			return err
		}
		if mc.Labels == nil {
			mc.Labels = make(map[string]string)
		}
		mc.Labels[key] = value
		return hubClient.Update(ctx, mc)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to set label %s=%s on member cluster %s", key, value, clusterName)
}

// ownedByPlacementPolicyAppliedWork reports whether an object on a member cluster is owned by an AppliedWork of the
// placement policy APIs, i.e., the placement policy work applier manages it.
func ownedByPlacementPolicyAppliedWork(obj metav1.Object) bool {
	for _, ownerRef := range obj.GetOwnerReferences() {
		if ownerRef.APIVersion == kfplacementv1alpha1.GroupVersion.String() && ownerRef.Kind == kfplacementv1alpha1.AppliedWorkKind {
			return true
		}
	}
	return false
}

// placementPolicyBindings lists the bindings that name the policy, whether or not the policy controls them;
// callers check the controller reference, so that a binding left behind by an earlier policy of the same name
// is reported rather than silently skipped.
func placementPolicyBindings(policy kfplacementv1alpha1.PlacementPolicyAccessor) ([]kfplacementv1alpha1.PlacementBindingAccessor, error) {
	var all []kfplacementv1alpha1.PlacementBindingAccessor
	if policy.GetNamespace() == "" {
		bindings := &kfplacementv1alpha1.ClusterPlacementBindingList{}
		if err := hubClient.List(ctx, bindings); err != nil {
			return nil, err
		}
		for i := range bindings.Items {
			all = append(all, &bindings.Items[i])
		}
	} else {
		bindings := &kfplacementv1alpha1.PlacementBindingList{}
		if err := hubClient.List(ctx, bindings, client.InNamespace(policy.GetNamespace())); err != nil {
			return nil, err
		}
		for i := range bindings.Items {
			all = append(all, &bindings.Items[i])
		}
	}

	named := make([]kfplacementv1alpha1.PlacementBindingAccessor, 0, len(all))
	for _, binding := range all {
		if binding.GetSpec().PlacementPolicyName == policy.GetName() {
			named = append(named, binding)
		}
	}
	return named, nil
}

// placementPolicySnapshots lists the resource snapshots, primary and secondary, that name the policy through the owner
// annotation, which, unlike the owner label, always holds the full policy name. It needs no policy object, so it
// also finds the snapshots of a policy that is gone.
func placementPolicySnapshots(key types.NamespacedName) ([]kfplacementv1alpha1.PlacementResourceSnapshotAccessor, error) {
	var all []kfplacementv1alpha1.PlacementResourceSnapshotAccessor
	if key.Namespace == "" {
		list := &kfplacementv1alpha1.ClusterPlacementResourceSnapshotList{}
		if err := hubClient.List(ctx, list); err != nil {
			return nil, err
		}
		for i := range list.Items {
			all = append(all, &list.Items[i])
		}
	} else {
		list := &kfplacementv1alpha1.PlacementResourceSnapshotList{}
		if err := hubClient.List(ctx, list, client.InNamespace(key.Namespace)); err != nil {
			return nil, err
		}
		for i := range list.Items {
			all = append(all, &list.Items[i])
		}
	}

	named := make([]kfplacementv1alpha1.PlacementResourceSnapshotAccessor, 0, len(all))
	for _, snapshot := range all {
		if snapshot.GetAnnotations()[kfplacementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey] == key.Name {
			named = append(named, snapshot)
		}
	}
	return named, nil
}

// latestPrimarySnapshotOfPlacementPolicy returns the primary resource snapshot of the highest index that the
// policy controls.
//
// Snapshots are matched on the owner annotation, which, unlike the owner label, always holds the full
// policy name.
func latestPrimarySnapshotOfPlacementPolicy(policy kfplacementv1alpha1.PlacementPolicyAccessor) (kfplacementv1alpha1.PlacementResourceSnapshotAccessor, error) {
	primaryOnly := client.MatchingLabels{kfplacementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: "0"}
	var all []kfplacementv1alpha1.PlacementResourceSnapshotAccessor
	if policy.GetNamespace() == "" {
		snapshots := &kfplacementv1alpha1.ClusterPlacementResourceSnapshotList{}
		if err := hubClient.List(ctx, snapshots, primaryOnly); err != nil {
			return nil, err
		}
		for i := range snapshots.Items {
			all = append(all, &snapshots.Items[i])
		}
	} else {
		snapshots := &kfplacementv1alpha1.PlacementResourceSnapshotList{}
		if err := hubClient.List(ctx, snapshots, client.InNamespace(policy.GetNamespace()), primaryOnly); err != nil {
			return nil, err
		}
		for i := range snapshots.Items {
			all = append(all, &snapshots.Items[i])
		}
	}

	var latest kfplacementv1alpha1.PlacementResourceSnapshotAccessor
	latestIdx := -1
	for _, snapshot := range all {
		if snapshot.GetAnnotations()[kfplacementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey] != policy.GetName() ||
			!metav1.IsControlledBy(snapshot, policy) {
			continue
		}
		idxStr := snapshot.GetLabels()[kfplacementv1alpha1.PlacementResourceSnapshotIndexLabelKey]
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s has an invalid index label %q: %w", snapshot.GetName(), idxStr, err)
		}
		if idx > latestIdx {
			latest, latestIdx = snapshot, idx
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no primary resource snapshot found for placement policy %s", client.ObjectKeyFromObject(policy))
	}
	return latest, nil
}

// allSynchronizedPlacementPolicyStatus returns the status of a policy that has found all of its desired clusters
// and has the latest resource revision synchronized and available on each of them.
//
// The latest resource revision name and the observed generations are left for the actual to fill in.
func allSynchronizedPlacementPolicyStatus(clusterCount int32) kfplacementv1alpha1.PlacementPolicyStatus {
	return kfplacementv1alpha1.PlacementPolicyStatus{
		Conditions: []metav1.Condition{
			{
				Type:   kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected,
				Status: metav1.ConditionTrue,
				Reason: kfplacementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected,
			},
			{
				Type:   kfplacementv1alpha1.PlacementPolicyCondTypeScheduled,
				Status: metav1.ConditionTrue,
				Reason: kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters,
			},
			{
				Type:   kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized,
				Status: metav1.ConditionTrue,
				Reason: kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonAllClustersSynchronized,
			},
			{
				Type:   kfplacementv1alpha1.PlacementPolicyCondTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: kfplacementv1alpha1.PlacementPolicyAvailableCondReasonAllClustersAvailable,
			},
		},
		DesiredClusters:            ptr.To(clusterCount),
		ScheduledClusters:          ptr.To(clusterCount),
		SynchronizedClusters:       ptr.To(clusterCount),
		ResourcesAvailableClusters: ptr.To(clusterCount),
		ActiveClusterClaims:        ptr.To(int32(0)),
	}
}

// ensurePlacementPolicyRemoved deletes a placement policy and waits until it is gone and the app ConfigMap has
// been removed from every member cluster.
func ensurePlacementPolicyRemoved(key types.NamespacedName) {
	GinkgoHelper()

	deletePlacementPolicyAndWait(key)
	Eventually(appConfigMapRemovedFromClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
		Should(Succeed(), "Failed to remove the placed ConfigMap from the member clusters")
}

// deletePlacementPolicyAndWait deletes a placement policy, with the given delete options, and waits until it is gone,
// without checking what happens to the resources it placed; specs whose policy orphans its resources use it directly.
func deletePlacementPolicyAndWait(key types.NamespacedName, opts ...client.DeleteOption) {
	GinkgoHelper()

	policy := newPlacementPolicyObject(key, kfplacementv1alpha1.PlacementPolicySpec{})
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, policy, opts...))).To(Succeed(), "Failed to delete placement policy %s", key)
	// Policy deletion is finalizer-gated on cluster claim withdrawal, so waiting for the policy to disappear also
	// confirms that its claims are gone.
	Eventually(func() error {
		_, err := retrievePlacementPolicy(key)
		if err == nil {
			return fmt.Errorf("placement policy %s still exists", key)
		}
		return client.IgnoreNotFound(err)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove placement policy %s", key)
}

// stagedUpdateRunBindingManagerName is the controller name that a staged update run records when it holds the
// binding manager role of its placement policy. It is spelled out rather than imported so that a rename shows up
// as a failed spec, the same way it would for a user inspecting the policy status.
const stagedUpdateRunBindingManagerName = "staged-update-run"

// placementPolicyRolloutEventuallyDuration bounds the waits on rollouts (staged update runs and auto-rollouts). A
// staged update run reconcile can hold its worker for up to the run controller's 30s refresh period, and persists
// the run status only once it returns.
const placementPolicyRolloutEventuallyDuration = 90 * time.Second

// placementPolicyCondition builds a wanted condition; observed generations and messages are left out, as the
// actuals fill in the former and ignore the latter.
func placementPolicyCondition(condType string, status metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{Type: condType, Status: status, Reason: reason}
}

// synchronizedPlacementBindingStatus returns the status of a binding whose resourceCount resources are all
// synchronized and available on its cluster.
func synchronizedPlacementBindingStatus(resourceCount int32) kfplacementv1alpha1.PlacementBindingStatus {
	return kfplacementv1alpha1.PlacementBindingStatus{
		Conditions: []metav1.Condition{
			placementPolicyCondition(kfplacementv1alpha1.PlacementBindingCondTypeSynchronized, metav1.ConditionTrue,
				kfplacementv1alpha1.PlacementBindingSynchronizedCondReasonAllResourcesSynchronized),
			placementPolicyCondition(kfplacementv1alpha1.PlacementBindingCondTypeAvailable, metav1.ConditionTrue,
				kfplacementv1alpha1.PlacementBindingAvailableCondReasonAllResourcesAvailable),
		},
		SelectedResources:     ptr.To(resourceCount),
		SynchronizedResources: ptr.To(resourceCount),
		AvailableResources:    ptr.To(resourceCount),
	}
}

// notTakenOverPlacementBindingStatus returns the status of a binding whose only resource, a ConfigMap, already
// exists on its cluster unmanaged and has not been taken over, as the default whenAlreadyExists=ReportError asks.
func notTakenOverPlacementBindingStatus(configMapKey types.NamespacedName) kfplacementv1alpha1.PlacementBindingStatus {
	return configMapNotAppliedPlacementBindingStatus(configMapKey, "NotTakenOver", nil)
}

// configMapNotAppliedPlacementBindingStatus returns the status of a binding whose only resource, a ConfigMap, the
// work applier did not apply, for the reason given (the applier's apply result type), with the given diff details.
//
// A ConfigMap carries no generation, so the observed generation of the copied manifest condition is 0.
func configMapNotAppliedPlacementBindingStatus(
	configMapKey types.NamespacedName,
	reason string,
	diffDetails *kfplacementv1alpha1.DiffDetails,
) kfplacementv1alpha1.PlacementBindingStatus {
	return kfplacementv1alpha1.PlacementBindingStatus{
		Conditions: []metav1.Condition{
			placementPolicyCondition(kfplacementv1alpha1.PlacementBindingCondTypeSynchronized, metav1.ConditionFalse,
				kfplacementv1alpha1.PlacementBindingSynchronizedCondReasonFailedToSynchronizeSomeResources),
			placementPolicyCondition(kfplacementv1alpha1.PlacementBindingCondTypeAvailable, metav1.ConditionFalse,
				kfplacementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable),
		},
		SelectedResources:     ptr.To(int32(1)),
		SynchronizedResources: ptr.To(int32(0)),
		AvailableResources:    ptr.To(int32(0)),
		FailedResources: []kfplacementv1alpha1.FailedResource{
			{
				ObjectRef: kfplacementv1alpha1.ObjectReference{
					Namespace:  configMapKey.Namespace,
					Name:       configMapKey.Name,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				Conditions: []metav1.Condition{
					// The manifest condition reason is the work applier's apply result type.
					placementPolicyCondition(kfplacementv1alpha1.ManifestCondTypeApplied, metav1.ConditionFalse, reason),
				},
				DiffDetails: diffDetails,
			},
		},
	}
}

// updatePlacementPolicy applies the mutation to the placement policy, retrying on conflicts.
func updatePlacementPolicy(key types.NamespacedName, mutate func(policy kfplacementv1alpha1.PlacementPolicyAccessor)) {
	GinkgoHelper()

	Eventually(func() error {
		policy, err := retrievePlacementPolicy(key)
		if err != nil {
			return err
		}
		mutate(policy)
		return hubClient.Update(ctx, policy)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update placement policy %s", key)
}

// updateConfigMapDataOnHub replaces the data of a ConfigMap on the hub cluster, retrying on conflicts.
func updateConfigMapDataOnHub(key types.NamespacedName, data map[string]string) {
	GinkgoHelper()

	Eventually(func() error {
		cm := &corev1.ConfigMap{}
		if err := hubClient.Get(ctx, key, cm); err != nil {
			return err
		}
		cm.Data = maps.Clone(data)
		return hubClient.Update(ctx, cm)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ConfigMap %s on the hub cluster", key)
}

// updateConfigMapDataOnMemberCluster replaces the data of a ConfigMap directly on a member cluster, retrying on
// conflicts; the rest of the object, KubeFleet's owner references and annotations included, is kept as it is.
func updateConfigMapDataOnMemberCluster(cluster *framework.Cluster, key types.NamespacedName, data map[string]string) {
	GinkgoHelper()

	Eventually(func() error {
		cm := &corev1.ConfigMap{}
		if err := cluster.KubeClient.Get(ctx, key, cm); err != nil {
			return err
		}
		cm.Data = maps.Clone(data)
		return cluster.KubeClient.Update(ctx, cm)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ConfigMap %s on cluster %s", key, cluster.ClusterName)
}

// createConfigMapOnMemberCluster creates a ConfigMap, and its namespace if needed, directly on a member cluster, so
// that the ConfigMap is not managed by KubeFleet. Creation is retried, as the namespace might still be terminating
// after an earlier spec's cleanup.
func createConfigMapOnMemberCluster(cluster *framework.Cluster, cm *corev1.ConfigMap, opts ...client.CreateOption) {
	GinkgoHelper()

	Eventually(func() error {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cm.Namespace}}
		if err := client.IgnoreAlreadyExists(cluster.KubeClient.Create(ctx, ns)); err != nil {
			return err
		}
		return cluster.KubeClient.Create(ctx, cm.DeepCopy(), opts...)
	}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(),
		"Failed to create ConfigMap %s on cluster %s", client.ObjectKeyFromObject(cm), cluster.ClusterName)
}

// newStagedUpdateStrategyObject returns a StagedUpdateStrategy or a ClusterStagedUpdateStrategy, depending on the key.
func newStagedUpdateStrategyObject(key types.NamespacedName, spec kfrolloutv1alpha1.StagedUpdateStrategySpec) kfrolloutv1alpha1.StagedUpdateStrategyAccessor {
	if key.Namespace == "" {
		return &kfrolloutv1alpha1.ClusterStagedUpdateStrategy{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name},
			Spec:       spec,
		}
	}
	return &kfrolloutv1alpha1.StagedUpdateStrategy{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Spec:       spec,
	}
}

// newStagedUpdateRunObject returns a StagedUpdateRun or a ClusterStagedUpdateRun, depending on the key.
func newStagedUpdateRunObject(key types.NamespacedName, spec kfrolloutv1alpha1.StagedUpdateRunSpec) kfrolloutv1alpha1.StagedUpdateRunAccessor {
	if key.Namespace == "" {
		return &kfrolloutv1alpha1.ClusterStagedUpdateRun{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name},
			Spec:       spec,
		}
	}
	return &kfrolloutv1alpha1.StagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Spec:       spec,
	}
}

// retrieveStagedUpdateRun reads a StagedUpdateRun or a ClusterStagedUpdateRun, depending on the key.
func retrieveStagedUpdateRun(key types.NamespacedName) (kfrolloutv1alpha1.StagedUpdateRunAccessor, error) {
	run := newStagedUpdateRunObject(key, kfrolloutv1alpha1.StagedUpdateRunSpec{})
	if err := hubClient.Get(ctx, key, run); err != nil {
		return nil, err
	}
	return run, nil
}

// newApprovalRequestObject returns an ApprovalRequest or a ClusterApprovalRequest, depending on the key.
func newApprovalRequestObject(key types.NamespacedName) kfrolloutv1alpha1.ApprovalRequestAccessor {
	if key.Namespace == "" {
		return &kfrolloutv1alpha1.ClusterApprovalRequest{ObjectMeta: metav1.ObjectMeta{Name: key.Name}}
	}
	return &kfrolloutv1alpha1.ApprovalRequest{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
}

// retrieveApprovalRequest reads an ApprovalRequest or a ClusterApprovalRequest, depending on the key.
func retrieveApprovalRequest(key types.NamespacedName) (kfrolloutv1alpha1.ApprovalRequestAccessor, error) {
	request := newApprovalRequestObject(key)
	if err := hubClient.Get(ctx, key, request); err != nil {
		return nil, err
	}
	return request, nil
}

// approvePlacementPolicyApprovalRequest approves an approval request (or a cluster approval request) the way a user
// does without the kubectl-fleet plugin, which does not support the placement policy APIs: by adding an Approved
// condition to its status. Conflicts with the run controller's own writes are retried.
func approvePlacementPolicyApprovalRequest(key types.NamespacedName) {
	GinkgoHelper()

	Eventually(func() error {
		request, err := retrieveApprovalRequest(key)
		if err != nil {
			return err
		}
		meta.SetStatusCondition(&request.GetStatus().Conditions, metav1.Condition{
			Type:               kfrolloutv1alpha1.ApprovalRequestCondTypeApproved,
			Status:             metav1.ConditionTrue,
			Reason:             "ApprovedByE2E",
			Message:            "Approved by the E2E test",
			ObservedGeneration: request.GetGeneration(),
		})
		return hubClient.Status().Update(ctx, request)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to approve approval request %s", key)
}

// stagedUpdateRunObjectReference returns the reference that a staged update run (or a cluster staged update run)
// records on its placement policy when it holds the binding manager role.
func stagedUpdateRunObjectReference(key types.NamespacedName) kfplacementv1alpha1.ObjectReference {
	kind := kfrolloutv1alpha1.StagedUpdateRunKind
	if key.Namespace == "" {
		kind = kfrolloutv1alpha1.ClusterStagedUpdateRunKind
	}
	return kfplacementv1alpha1.ObjectReference{
		APIGroup:   kfrolloutv1alpha1.GroupVersion.Group,
		APIVersion: kfrolloutv1alpha1.GroupVersion.Version,
		Kind:       kind,
		Namespace:  key.Namespace,
		Name:       key.Name,
	}
}

// ensureStagedUpdateRunRemoved deletes a staged update run (or a cluster staged update run) and waits until it is
// gone; the run controller releases its finalizer once it has given up the binding manager role.
func ensureStagedUpdateRunRemoved(key types.NamespacedName) {
	GinkgoHelper()

	run := newStagedUpdateRunObject(key, kfrolloutv1alpha1.StagedUpdateRunSpec{})
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, run))).To(Succeed(), "Failed to delete staged update run %s", key)
	Eventually(func() error {
		_, err := retrieveStagedUpdateRun(key)
		if err == nil {
			return fmt.Errorf("staged update run %s still exists", key)
		}
		return client.IgnoreNotFound(err)
	}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove staged update run %s", key)
}

// ensureStagedUpdateStrategyRemoved deletes a staged update strategy (or a cluster staged update strategy).
func ensureStagedUpdateStrategyRemoved(key types.NamespacedName) {
	GinkgoHelper()

	strategy := newStagedUpdateStrategyObject(key, kfrolloutv1alpha1.StagedUpdateStrategySpec{})
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, strategy))).To(Succeed(), "Failed to delete staged update strategy %s", key)
}
