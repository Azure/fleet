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

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kfrolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

// placementPolicyStatusActual compares the status of a placement policy with the wanted one in one shot.
//
// Unless the wanted status names a latest resource revision, the actual fills in the name of the policy's latest
// primary resource snapshot; it also sets the observed generation of every wanted condition to the policy's
// generation.
func placementPolicyStatusActual(key types.NamespacedName, wantStatus kfplacementv1alpha1.PlacementPolicyStatus) func() error {
	return comparePlacementPolicyStatusActual(key, wantStatus, true)
}

// placementPolicyStatusWithoutResourceRevisionActual is placementPolicyStatusActual for a policy that has no
// resource snapshot yet, so its status names no latest resource revision either.
func placementPolicyStatusWithoutResourceRevisionActual(key types.NamespacedName, wantStatus kfplacementv1alpha1.PlacementPolicyStatus) func() error {
	return comparePlacementPolicyStatusActual(key, wantStatus, false)
}

func comparePlacementPolicyStatusActual(key types.NamespacedName, wantStatus kfplacementv1alpha1.PlacementPolicyStatus, fillInLatestRevision bool) func() error {
	return func() error {
		policy, err := retrievePlacementPolicy(key)
		if err != nil {
			return err
		}

		want := wantStatus.DeepCopy()
		if fillInLatestRevision && want.LatestResourceRevisionName == nil {
			latest, err := latestPrimarySnapshotOfPlacementPolicy(policy)
			if err != nil {
				return err
			}
			want.LatestResourceRevisionName = ptr.To(latest.GetName())
		}
		for i := range want.Conditions {
			want.Conditions[i].ObservedGeneration = policy.GetGeneration()
		}

		if diff := cmp.Diff(*policy.GetStatus(), *want, placementPolicyCmpOptions); diff != "" {
			return fmt.Errorf("placement policy %s status diff (-got, +want):\n%s", key, diff)
		}
		return nil
	}
}

// placementBindingView is the part of a placement binding that the specs compare in one shot.
type placementBindingView struct {
	ControlledByPolicy bool
	Deleting           bool
	Spec               kfplacementv1alpha1.PlacementBindingSpec
	Status             kfplacementv1alpha1.PlacementBindingStatus
}

// wantPlacementBinding describes the binding that a spec expects for one cluster.
type wantPlacementBinding struct {
	selectors []kfplacementv1alpha1.ClusterSelectorWithTermsOnly
	// snapshotName is the resource snapshot that the binding points to and has processed; empty means the policy's
	// latest primary snapshot.
	snapshotName string
	// status is the wanted binding status. The actual fills in the last processed snapshot name and the observed
	// generations of the binding-level conditions.
	status kfplacementv1alpha1.PlacementBindingStatus
}

// placementPolicyBindingsActual compares, keyed by cluster, the bindings that name the policy with the wanted ones
// in one shot. Each wanted binding is controlled by the policy, not being deleted, and carries the policy's sync
// strategy.
func placementPolicyBindingsActual(key types.NamespacedName, wantBindings map[string]wantPlacementBinding) func() error {
	return func() error {
		policy, err := retrievePlacementPolicy(key)
		if err != nil {
			return err
		}
		latest, err := latestPrimarySnapshotOfPlacementPolicy(policy)
		if err != nil {
			return err
		}
		bindings, err := placementPolicyBindings(policy)
		if err != nil {
			return err
		}

		got := make(map[string]placementBindingView, len(bindings))
		generations := make(map[string]int64, len(bindings))
		for _, binding := range bindings {
			clusterName := binding.GetSpec().ClusterName
			if _, found := got[clusterName]; found {
				return fmt.Errorf("placement policy %s has more than one binding for cluster %s", key, clusterName)
			}
			got[clusterName] = placementBindingView{
				ControlledByPolicy: metav1.IsControlledBy(binding, policy),
				Deleting:           !binding.GetDeletionTimestamp().IsZero(),
				Spec:               *binding.GetSpec(),
				Status:             *binding.GetStatus(),
			}
			generations[clusterName] = binding.GetGeneration()
		}

		want := make(map[string]placementBindingView, len(wantBindings))
		for clusterName, wantBinding := range wantBindings {
			snapshotName := wantBinding.snapshotName
			if snapshotName == "" {
				snapshotName = latest.GetName()
			}
			wantStatus := wantBinding.status.DeepCopy()
			wantStatus.LastProcessedResourceSnapshotName = ptr.To(snapshotName)
			for i := range wantStatus.Conditions {
				wantStatus.Conditions[i].ObservedGeneration = generations[clusterName]
			}
			want[clusterName] = placementBindingView{
				ControlledByPolicy: true,
				Spec: kfplacementv1alpha1.PlacementBindingSpec{
					PlacementPolicyName:  key.Name,
					ClusterSelectors:     wantBinding.selectors,
					ClusterName:          clusterName,
					ResourceSnapshotName: snapshotName,
					SyncStrategy:         policy.GetSpec().SyncStrategy,
				},
				Status: *wantStatus,
			}
		}

		// Time values (e.g., when a diff was first observed) carry no contract either.
		if diff := cmp.Diff(got, want, placementPolicyCmpOptions, ignoreTimeTypeFields); diff != "" {
			return fmt.Errorf("placement policy %s bindings diff, keyed by cluster (-got, +want):\n%s", key, diff)
		}
		return nil
	}
}

// allSynchronizedPlacementPolicyBindingsActual checks that the policy has exactly one binding per wanted cluster,
// each pointing to the policy's latest primary resource snapshot, recording the given cluster selectors, and
// reporting all of its wantResourceCount resources as synchronized and available.
func allSynchronizedPlacementPolicyBindingsActual(
	key types.NamespacedName,
	wantSelectorsByCluster map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly,
	wantResourceCount int32,
) func() error {
	wantBindings := make(map[string]wantPlacementBinding, len(wantSelectorsByCluster))
	for clusterName, selectors := range wantSelectorsByCluster {
		wantBindings[clusterName] = wantPlacementBinding{
			selectors: selectors,
			status:    synchronizedPlacementBindingStatus(wantResourceCount),
		}
	}
	return placementPolicyBindingsActual(key, wantBindings)
}

// configMapDataOnClustersActual checks that the ConfigMap exists on each given cluster with exactly the given data.
func configMapDataOnClustersActual(key types.NamespacedName, wantData map[string]string, clusters ...*framework.Cluster) func() error {
	return func() error {
		for _, c := range clusters {
			cm := &corev1.ConfigMap{}
			if err := c.KubeClient.Get(ctx, key, cm); err != nil {
				return fmt.Errorf("cluster %s: failed to get ConfigMap %s: %w", c.ClusterName, key, err)
			}
			if diff := cmp.Diff(cm.Data, wantData); diff != "" {
				return fmt.Errorf("cluster %s: ConfigMap %s data diff (-got, +want):\n%s", c.ClusterName, key, diff)
			}
		}
		return nil
	}
}

// configMapAbsentOnClustersActual checks that none of the given clusters has the ConfigMap.
func configMapAbsentOnClustersActual(key types.NamespacedName, clusters ...*framework.Cluster) func() error {
	return func() error {
		for _, c := range clusters {
			err := c.KubeClient.Get(ctx, key, &corev1.ConfigMap{})
			switch {
			case err == nil:
				return fmt.Errorf("cluster %s: ConfigMap %s exists", c.ClusterName, key)
			case !apierrors.IsNotFound(err):
				return fmt.Errorf("cluster %s: failed to get ConfigMap %s: %w", c.ClusterName, key, err)
			}
		}
		return nil
	}
}

// configMapTakenOverOnClusterActual checks that the ConfigMap on the cluster matches its hub copy and is owned by an
// AppliedWork of the placement policy APIs, i.e., the placement policy work applier has taken it over.
func configMapTakenOverOnClusterActual(cluster *framework.Cluster, key types.NamespacedName) func() error {
	return func() error {
		if err := validateConfigMapOnCluster(cluster, key); err != nil {
			return fmt.Errorf("cluster %s: %w", cluster.ClusterName, err)
		}
		cm := &corev1.ConfigMap{}
		if err := cluster.KubeClient.Get(ctx, key, cm); err != nil {
			return fmt.Errorf("cluster %s: failed to get ConfigMap %s: %w", cluster.ClusterName, key, err)
		}
		if !ownedByPlacementPolicyAppliedWork(cm) {
			return fmt.Errorf("cluster %s: ConfigMap %s is not owned by an AppliedWork of the placement policy APIs (owner references: %+v)",
				cluster.ClusterName, key, cm.OwnerReferences)
		}
		return nil
	}
}

// noPlacementPolicyWorksActual checks that no Work on the hub was generated for the policy's bindings.
func noPlacementPolicyWorksActual(key types.NamespacedName) func() error {
	return func() error {
		works, err := placementPolicyWorks(key)
		if err != nil {
			return err
		}
		if len(works) > 0 {
			return fmt.Errorf("work %s of placement policy %s still exists", client.ObjectKeyFromObject(&works[0]), key)
		}
		return nil
	}
}

// configMapOrphanedOnClustersActual checks that the ConfigMap stays on each given cluster with the given data, but is
// no longer owned by any AppliedWork, i.e., KubeFleet has let go of it.
func configMapOrphanedOnClustersActual(key types.NamespacedName, wantData map[string]string, clusters ...*framework.Cluster) func() error {
	dataActual := configMapDataOnClustersActual(key, wantData, clusters...)
	return func() error {
		if err := dataActual(); err != nil {
			return err
		}
		for _, c := range clusters {
			cm := &corev1.ConfigMap{}
			if err := c.KubeClient.Get(ctx, key, cm); err != nil {
				return fmt.Errorf("cluster %s: failed to get ConfigMap %s: %w", c.ClusterName, key, err)
			}
			for _, ownerRef := range cm.OwnerReferences {
				if ownerRef.Kind == kfplacementv1alpha1.AppliedWorkKind {
					return fmt.Errorf("cluster %s: ConfigMap %s is still owned by AppliedWork %s (%s)", c.ClusterName, key, ownerRef.Name, ownerRef.APIVersion)
				}
			}
		}
		return nil
	}
}

// deploymentAvailableOnClustersActual checks that the Deployment on each given cluster has rolled out its current
// generation and has all of its replicas available.
func deploymentAvailableOnClustersActual(key types.NamespacedName, clusters ...*framework.Cluster) func() error {
	return func() error {
		for _, c := range clusters {
			deploy := &appsv1.Deployment{}
			if err := c.KubeClient.Get(ctx, key, deploy); err != nil {
				return fmt.Errorf("cluster %s: failed to get Deployment %s: %w", c.ClusterName, key, err)
			}
			wantReplicas := ptr.Deref(deploy.Spec.Replicas, 1)
			if deploy.Status.ObservedGeneration != deploy.Generation || deploy.Status.AvailableReplicas != wantReplicas {
				return fmt.Errorf("cluster %s: Deployment %s has %d of %d replicas available (observed generation %d, generation %d)",
					c.ClusterName, key, deploy.Status.AvailableReplicas, wantReplicas, deploy.Status.ObservedGeneration, deploy.Generation)
			}
		}
		return nil
	}
}

// stagedUpdateRunSucceededActual checks that a staged update run (or a cluster staged update run) has completed
// successfully.
func stagedUpdateRunSucceededActual(key types.NamespacedName) func() error {
	return func() error {
		run, err := retrieveStagedUpdateRun(key)
		if err != nil {
			return err
		}
		cond := meta.FindStatusCondition(run.GetStatus().Conditions, kfrolloutv1alpha1.StagedUpdateRunCondTypeCompleted)
		if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != kfrolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded ||
			cond.ObservedGeneration != run.GetGeneration() {
			return fmt.Errorf("staged update run %s has not completed successfully yet (Completed condition: %+v)", key, cond)
		}
		return nil
	}
}

// onlyCanaryUpdatedActual checks the state of a policy over all three member clusters midway through a rollout whose
// first stage is the canary cluster: the canary cluster's binding points to the new resource snapshot and the cluster
// has the new data, while the prod clusters' bindings still point to the initial snapshot and the clusters keep the
// initial data.
func onlyCanaryUpdatedActual(
	policyKey, cmKey types.NamespacedName,
	initialSnapshotName, newSnapshotName string,
	initialData, newData map[string]string,
) func() error {
	snapshotsActual := placementPolicyBindingSnapshotsActual(policyKey, map[string]string{
		memberCluster2EastCanaryName: newSnapshotName,
		memberCluster1EastProdName:   initialSnapshotName,
		memberCluster3WestProdName:   initialSnapshotName,
	})
	canaryActual := configMapDataOnClustersActual(cmKey, newData, memberCluster2EastCanary)
	prodActual := configMapDataOnClustersActual(cmKey, initialData, memberCluster1EastProd, memberCluster3WestProd)
	return func() error {
		if err := snapshotsActual(); err != nil {
			return err
		}
		if err := canaryActual(); err != nil {
			return err
		}
		return prodActual()
	}
}

// stagedUpdateRunConditionActual checks that a staged update run (or a cluster staged update run) reports the given
// condition with the given status and reason, for its current generation.
func stagedUpdateRunConditionActual(key types.NamespacedName, condType string, wantStatus metav1.ConditionStatus, wantReason string) func() error {
	return func() error {
		run, err := retrieveStagedUpdateRun(key)
		if err != nil {
			return err
		}
		cond := meta.FindStatusCondition(run.GetStatus().Conditions, condType)
		if cond == nil || cond.Status != wantStatus || cond.Reason != wantReason || cond.ObservedGeneration != run.GetGeneration() {
			return fmt.Errorf("staged update run %s reports %s condition %+v, want status %s and reason %s for generation %d",
				key, condType, cond, wantStatus, wantReason, run.GetGeneration())
		}
		return nil
	}
}

// stagedUpdateRunClusterResultsActual compares, keyed by cluster, how a staged update run (or a cluster staged update
// run) has ended the update of each cluster in any of its stages: the reason of the cluster's Completed condition, or
// an empty string for a cluster whose update has not completed (or not started).
func stagedUpdateRunClusterResultsActual(key types.NamespacedName, wantResults map[string]string) func() error {
	return func() error {
		run, err := retrieveStagedUpdateRun(key)
		if err != nil {
			return err
		}
		got := map[string]string{}
		for _, stage := range run.GetStatus().Stages {
			for _, cluster := range stage.Clusters {
				result := ""
				if cond := meta.FindStatusCondition(cluster.Conditions, kfrolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted); cond != nil && cond.Status == metav1.ConditionTrue {
					result = cond.Reason
				}
				got[cluster.ClusterName] = result
			}
		}
		if diff := cmp.Diff(got, wantResults); diff != "" {
			return fmt.Errorf("staged update run %s cluster results diff, keyed by cluster (-got, +want):\n%s", key, diff)
		}
		return nil
	}
}

// placementPolicyBindingManagerActual checks which process, if any (a nil wantManager), holds the binding manager role
// of the policy.
func placementPolicyBindingManagerActual(key types.NamespacedName, wantManager *kfplacementv1alpha1.BindingManager) func() error {
	return func() error {
		policy, err := retrievePlacementPolicy(key)
		if err != nil {
			return err
		}
		if diff := cmp.Diff(policy.GetStatus().BindingManager, wantManager); diff != "" {
			return fmt.Errorf("placement policy %s binding manager diff (-got, +want):\n%s", key, diff)
		}
		return nil
	}
}

// placementPolicyBindingSnapshotsActual compares, keyed by cluster, the resource snapshots that the bindings naming
// the policy point to.
func placementPolicyBindingSnapshotsActual(key types.NamespacedName, wantSnapshotNames map[string]string) func() error {
	return func() error {
		bindings, err := placementPolicyBindingsByCluster(key)
		if err != nil {
			return err
		}
		got := make(map[string]string, len(bindings))
		for clusterName, binding := range bindings {
			got[clusterName] = binding.GetSpec().ResourceSnapshotName
		}
		if diff := cmp.Diff(got, wantSnapshotNames); diff != "" {
			return fmt.Errorf("placement policy %s binding resource snapshots diff, keyed by cluster (-got, +want):\n%s", key, diff)
		}
		return nil
	}
}

// approvalRequestAbsentActual checks that the approval request (or cluster approval request) is gone.
func approvalRequestAbsentActual(key types.NamespacedName) func() error {
	return func() error {
		_, err := retrieveApprovalRequest(key)
		switch {
		case err == nil:
			return fmt.Errorf("approval request %s still exists", key)
		case !apierrors.IsNotFound(err):
			return err
		}
		return nil
	}
}

// appliedWorksAbsentOnClustersActual checks that none of the given AppliedWorks, keyed by the name of the member
// cluster they would be on, exists. An AppliedWork carries the name of its Work.
func appliedWorksAbsentOnClustersActual(namesByCluster map[string][]string) func() error {
	return func() error {
		for clusterName, names := range namesByCluster {
			cluster := memberClusterNamed(clusterName)
			for _, name := range names {
				err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: name}, &kfplacementv1alpha1.AppliedWork{})
				switch {
				case err == nil:
					return fmt.Errorf("cluster %s: AppliedWork %s still exists", clusterName, name)
				case !apierrors.IsNotFound(err):
					return fmt.Errorf("cluster %s: failed to get AppliedWork %s: %w", clusterName, name, err)
				}
			}
		}
		return nil
	}
}

// namespaceLeftUnmanagedOnClustersActual checks that the namespace exists on each given cluster, not being deleted
// and not owned by any AppliedWork: the work applier created it for a namespaced resource and leaves it behind.
func namespaceLeftUnmanagedOnClustersActual(name string, clusters ...*framework.Cluster) func() error {
	return func() error {
		for _, c := range clusters {
			ns := &corev1.Namespace{}
			if err := c.KubeClient.Get(ctx, types.NamespacedName{Name: name}, ns); err != nil {
				return fmt.Errorf("cluster %s: failed to get namespace %s: %w", c.ClusterName, name, err)
			}
			if !ns.DeletionTimestamp.IsZero() {
				return fmt.Errorf("cluster %s: namespace %s is being deleted", c.ClusterName, name)
			}
			for _, ownerRef := range ns.OwnerReferences {
				if ownerRef.Kind == kfplacementv1alpha1.AppliedWorkKind {
					return fmt.Errorf("cluster %s: namespace %s is owned by AppliedWork %s (%s)", c.ClusterName, name, ownerRef.Name, ownerRef.APIVersion)
				}
			}
		}
		return nil
	}
}

// noPlacementPolicyBindingsActual checks that no binding names the policy.
func noPlacementPolicyBindingsActual(key types.NamespacedName) func() error {
	return func() error {
		bindings, err := placementPolicyBindingsByCluster(key)
		if err != nil {
			return err
		}
		if len(bindings) > 0 {
			return fmt.Errorf("placement policy %s has bindings for clusters %v, want none", key, slices.Sorted(maps.Keys(bindings)))
		}
		return nil
	}
}

// placementPolicyLeftoversRemovedActual checks that neither a binding nor a resource snapshot of a deleted policy
// is left on the hub. The garbage collector removes them only after the policy is gone, and a policy re-created
// under the same name before then would find them; specs that re-create a policy wait for this first.
func placementPolicyLeftoversRemovedActual(key types.NamespacedName) func() error {
	return func() error {
		if err := noPlacementPolicyBindingsActual(key)(); err != nil {
			return err
		}

		var snapshots []kfplacementv1alpha1.PlacementResourceSnapshotAccessor
		if key.Namespace == "" {
			list := &kfplacementv1alpha1.ClusterPlacementResourceSnapshotList{}
			if err := hubClient.List(ctx, list); err != nil {
				return err
			}
			for i := range list.Items {
				snapshots = append(snapshots, &list.Items[i])
			}
		} else {
			list := &kfplacementv1alpha1.PlacementResourceSnapshotList{}
			if err := hubClient.List(ctx, list, client.InNamespace(key.Namespace)); err != nil {
				return err
			}
			for i := range list.Items {
				snapshots = append(snapshots, &list.Items[i])
			}
		}
		for _, snapshot := range snapshots {
			if snapshot.GetAnnotations()[kfplacementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey] == key.Name {
				return fmt.Errorf("resource snapshot %s of placement policy %s still exists", snapshot.GetName(), key)
			}
		}
		return nil
	}
}

// placementPolicyClaimActual checks that the policy has exactly one cluster claim outstanding, referencing the
// policy and carrying the given selector terms.
func placementPolicyClaimActual(key types.NamespacedName, wantTerms []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm) func() error {
	kind := kfplacementv1alpha1.PlacementPolicyKind
	if key.Namespace == "" {
		kind = kfplacementv1alpha1.ClusterPlacementPolicyKind
	}
	wantSpec := kfplacementv1alpha1.ClusterClaimSpec{
		PlacementPolicyRef: &kfplacementv1alpha1.ObjectReference{
			APIGroup:   kfplacementv1alpha1.GroupVersion.Group,
			APIVersion: kfplacementv1alpha1.GroupVersion.Version,
			Kind:       kind,
			Namespace:  key.Namespace,
			Name:       key.Name,
		},
		ClusterSelectorTerms: wantTerms,
	}
	return func() error {
		claims, err := claimsOfPolicy(key.Name, key.Namespace)
		if err != nil {
			return err
		}
		if len(claims.Items) != 1 {
			return fmt.Errorf("placement policy %s has %d cluster claims, want 1", key, len(claims.Items))
		}
		claim := &claims.Items[0]
		if !claim.DeletionTimestamp.IsZero() {
			return fmt.Errorf("cluster claim %s of placement policy %s is being withdrawn", claim.Name, key)
		}
		if diff := cmp.Diff(claim.Spec, wantSpec); diff != "" {
			return fmt.Errorf("cluster claim %s of placement policy %s spec diff (-got, +want):\n%s", claim.Name, key, diff)
		}
		return nil
	}
}

// namespacePlacedOnClustersActual checks that the namespace exists on each given cluster and is managed by the
// placement policy work applier. The applier also creates the namespace of a namespaced resource it applies, but
// leaves such a namespace unmanaged; only a selected Namespace object is owned by its AppliedWork.
func namespacePlacedOnClustersActual(name string, clusters ...*framework.Cluster) func() error {
	return func() error {
		for _, c := range clusters {
			ns := &corev1.Namespace{}
			if err := c.KubeClient.Get(ctx, types.NamespacedName{Name: name}, ns); err != nil {
				return fmt.Errorf("cluster %s: failed to get namespace %s: %w", c.ClusterName, name, err)
			}
			if !ns.DeletionTimestamp.IsZero() {
				return fmt.Errorf("cluster %s: namespace %s is being deleted", c.ClusterName, name)
			}
			if !ownedByPlacementPolicyAppliedWork(ns) {
				return fmt.Errorf("cluster %s: namespace %s is not owned by an AppliedWork of the placement policy APIs (owner references: %+v)",
					c.ClusterName, name, ns.OwnerReferences)
			}
		}
		return nil
	}
}

// namespaceAbsentOnClustersActual checks that none of the given clusters has the namespace, terminating or not.
func namespaceAbsentOnClustersActual(name string, clusters ...*framework.Cluster) func() error {
	return func() error {
		for _, c := range clusters {
			err := c.KubeClient.Get(ctx, types.NamespacedName{Name: name}, &corev1.Namespace{})
			switch {
			case err == nil:
				return fmt.Errorf("cluster %s: namespace %s exists", c.ClusterName, name)
			case !apierrors.IsNotFound(err):
				return fmt.Errorf("cluster %s: failed to get namespace %s: %w", c.ClusterName, name, err)
			}
		}
		return nil
	}
}

// clusterRolePlacedOnClustersActual checks that the ClusterRole on each given cluster has the rules of its hub copy.
func clusterRolePlacedOnClustersActual(name string, clusters ...*framework.Cluster) func() error {
	return func() error {
		want := &rbacv1.ClusterRole{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, want); err != nil {
			return fmt.Errorf("failed to get ClusterRole %s on the hub cluster: %w", name, err)
		}
		for _, c := range clusters {
			got := &rbacv1.ClusterRole{}
			if err := c.KubeClient.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
				return fmt.Errorf("cluster %s: failed to get ClusterRole %s: %w", c.ClusterName, name, err)
			}
			if diff := cmp.Diff(got.Rules, want.Rules); diff != "" {
				return fmt.Errorf("cluster %s: ClusterRole %s rules diff (-got, +want):\n%s", c.ClusterName, name, diff)
			}
		}
		return nil
	}
}

// clusterRoleAbsentOnClustersActual checks that none of the given clusters has the ClusterRole.
func clusterRoleAbsentOnClustersActual(name string, clusters ...*framework.Cluster) func() error {
	return func() error {
		for _, c := range clusters {
			err := c.KubeClient.Get(ctx, types.NamespacedName{Name: name}, &rbacv1.ClusterRole{})
			switch {
			case err == nil:
				return fmt.Errorf("cluster %s: ClusterRole %s exists", c.ClusterName, name)
			case !apierrors.IsNotFound(err):
				return fmt.Errorf("cluster %s: failed to get ClusterRole %s: %w", c.ClusterName, name, err)
			}
		}
		return nil
	}
}

// compareTimePointersByPresence treats two *metav1.Time values as equal when both or neither are set, so that a
// wanted status can check that a timestamp is set without pinning its value. cmpopts.IgnoreTypes(metav1.Time{})
// does not cover pointers: cmp compares them with (*metav1.Time).Equal before it ever dereferences them.
var compareTimePointersByPresence = cmp.Comparer(func(a, b *metav1.Time) bool {
	return (a == nil) == (b == nil)
})

// stagedUpdateRunStatusActual compares the status of a staged update run (or a cluster staged update run) with the
// wanted one in one shot, ignoring condition messages and timestamp values; a wanted timestamp that is set (to any
// value) checks only that the actual one is set too. The observed generation of every wanted condition, at any
// level, is set to the run's generation, as the run controller stamps them all with it.
func stagedUpdateRunStatusActual(key types.NamespacedName, wantStatus kfrolloutv1alpha1.StagedUpdateRunStatus) func() error {
	return func() error {
		run, err := retrieveStagedUpdateRun(key)
		if err != nil {
			return err
		}

		want := wantStatus.DeepCopy()
		generation := run.GetGeneration()
		setObservedGeneration := func(conds []metav1.Condition) {
			for i := range conds {
				conds[i].ObservedGeneration = generation
			}
		}
		setObservedGeneration(want.Conditions)
		for i := range want.Stages {
			stage := &want.Stages[i]
			setObservedGeneration(stage.Conditions)
			for j := range stage.Clusters {
				setObservedGeneration(stage.Clusters[j].Conditions)
			}
			for j := range stage.BeforeStageTasks {
				setObservedGeneration(stage.BeforeStageTasks[j].Conditions)
			}
			for j := range stage.AfterStageTasks {
				setObservedGeneration(stage.AfterStageTasks[j].Conditions)
			}
		}

		if diff := cmp.Diff(*run.GetStatus(), *want, placementPolicyCmpOptions, ignoreTimeTypeFields, compareTimePointersByPresence); diff != "" {
			return fmt.Errorf("staged update run %s status diff (-got, +want):\n%s", key, diff)
		}
		return nil
	}
}

// approvalRequestActual checks that the approval request (or cluster approval request) exists for the given stage of
// the run, is controlled by the run, and, if wantAccepted is set, that the run has accepted its approval.
func approvalRequestActual(key, runKey types.NamespacedName, wantStageName string, wantAccepted bool) func() error {
	return func() error {
		run, err := retrieveStagedUpdateRun(runKey)
		if err != nil {
			return err
		}
		request, err := retrieveApprovalRequest(key)
		if err != nil {
			return err
		}

		if !metav1.IsControlledBy(request, run) {
			return fmt.Errorf("approval request %s is not controlled by staged update run %s (owner references: %+v)",
				key, runKey, request.GetOwnerReferences())
		}
		wantSpec := kfrolloutv1alpha1.ApprovalRequestSpec{
			StagedUpdateRunName: runKey.Name,
			StageName:           wantStageName,
		}
		if diff := cmp.Diff(*request.GetSpec(), wantSpec); diff != "" {
			return fmt.Errorf("approval request %s spec diff (-got, +want):\n%s", key, diff)
		}
		if wantAccepted && !meta.IsStatusConditionTrue(request.GetStatus().Conditions, kfrolloutv1alpha1.ApprovalRequestCondTypeApprovalAccepted) {
			return fmt.Errorf("approval request %s has not been accepted yet (conditions: %+v)", key, request.GetStatus().Conditions)
		}
		return nil
	}
}
