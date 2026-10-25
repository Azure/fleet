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
	"strconv"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kfrolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	clusterPlacementPolicyManyResourcesNameTemplate        = "cpp-many-resources-%d"
	clusterPlacementPolicyHistoryLimitNameTemplate         = "cpp-history-limit-%d"
	clusterPlacementPolicyHistoryLimitStrategyNameTemplate = "cpp-history-limit-strategy-%d"
	clusterPlacementPolicyHistoryLimitRunNameTemplate      = "cpp-history-limit-%d-run-%d"

	// placementPolicyBulkLabelKey is the label that the ConfigMaps selected in bulk carry.
	placementPolicyBulkLabelKey            = "placement-policy-e2e/bulk"
	placementPolicyBulkConfigMapNameFormat = "pp-e2e-bulk-%02d"
)

const (
	// placementPolicyBulkConfigMapCount is the number of ConfigMaps selected in bulk: more than a resource snapshot
	// holds.
	placementPolicyBulkConfigMapCount = 60
	// placementPolicyMaxResourcesPerSnapshot is the number of resources that the hub agent puts in a resource snapshot
	// at most (placementresourcesnapshot.DefaultMaxPerSnapshotResourceCnt).
	placementPolicyMaxResourcesPerSnapshot = 50
	// placementPolicyBulkConfigMapsInSecondarySnapshot is the number of bulk ConfigMaps that overflow into the
	// secondary resource snapshot.
	placementPolicyBulkConfigMapsInSecondarySnapshot = placementPolicyBulkConfigMapCount - placementPolicyMaxResourcesPerSnapshot

	// placementPolicyHistoryLimitRunCount is the number of runs that roll out to a policy that keeps one revision.
	placementPolicyHistoryLimitRunCount = 3
)

// placementResourceSnapshotView is the part of a resource snapshot that the specs compare in one shot.
type placementResourceSnapshotView struct {
	Index               string
	SubIndexedCount     string
	ControlledByPolicy  bool
	SelectedResourceCnt int
}

// linkedWorksView is the part of a binding's Works, all in the namespace of one member cluster, that the specs
// compare in one shot: the primary Work, which carries the linked Work count, and the secondary Works it controls.
type linkedWorksView struct {
	PrimaryLinkedWorkCount   string
	PrimaryManifestCnt       int
	SecondaryManifestCnts    []int
	UnlinkedSecondaryWorkCnt int
}

var _ = Describe("placement policy resource snapshots", Label("placementpolicy"), func() {
	Context("cluster placement policy selecting more resources than fit in one snapshot", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicyManyResourcesNameTemplate, GinkgoParallelProcess()),
		}
		bulkLabels := map[string]string{placementPolicyBulkLabelKey: "true"}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By(fmt.Sprintf("creating %d labeled ConfigMaps", placementPolicyBulkConfigMapCount))
			for i := range placementPolicyBulkConfigMapCount {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(placementPolicyBulkConfigMapNameFormat, i),
						Namespace: nsName,
						Labels:    bulkLabels,
					},
					Data: map[string]string{"index": strconv.Itoa(i)},
				}
				Expect(hubClient.Create(ctx, cm)).To(Succeed(), "Failed to create ConfigMap %s", client.ObjectKeyFromObject(cm))
			}

			By("creating a cluster placement policy that selects the labeled ConfigMaps")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{
					{APIVersion: "v1", Kind: "ConfigMap", Namespace: nsName, LabelSelector: &metav1.LabelSelector{MatchLabels: bulkLabels}},
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources, the labeled ConfigMaps among them, must go even if removing the policy fails;
			// otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should split the resources across a primary and a secondary resource snapshot", func() {
			// The hub agent puts at most 50 resources in a resource snapshot; the ConfigMaps are small enough that the
			// count, not the size, splits them.
			want := map[string]placementResourceSnapshotView{
				"0": {Index: "0", SubIndexedCount: "2", ControlledByPolicy: true, SelectedResourceCnt: placementPolicyMaxResourcesPerSnapshot},
				"1": {Index: "0", ControlledByPolicy: true, SelectedResourceCnt: placementPolicyBulkConfigMapsInSecondarySnapshot},
			}
			Eventually(func() error {
				policy, err := retrievePlacementPolicy(policyKey)
				if err != nil {
					return err
				}
				snapshots, err := placementPolicySnapshots(policyKey)
				if err != nil {
					return err
				}
				got := make(map[string]placementResourceSnapshotView, len(snapshots))
				for _, snapshot := range snapshots {
					labels := snapshot.GetLabels()
					got[labels[kfplacementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey]] = placementResourceSnapshotView{
						Index:               labels[kfplacementv1alpha1.PlacementResourceSnapshotIndexLabelKey],
						SubIndexedCount:     labels[kfplacementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey],
						ControlledByPolicy:  metav1.IsControlledBy(snapshot, policy),
						SelectedResourceCnt: len(snapshot.GetSpec().Resources),
					}
				}
				if diff := cmp.Diff(got, want); diff != "" {
					return fmt.Errorf("resource snapshots of cluster placement policy %s diff, keyed by sub-index (-got, +want):\n%s", policyKey, diff)
				}
				return nil
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to split the resources across two resource snapshots")
		})

		It("should generate a primary Work and a linked secondary Work for every member cluster", func() {
			want := make(map[string]linkedWorksView, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				want[fmt.Sprintf(utils.NamespaceNameFormat, clusterName)] = linkedWorksView{
					PrimaryLinkedWorkCount: "2",
					PrimaryManifestCnt:     placementPolicyMaxResourcesPerSnapshot,
					SecondaryManifestCnts:  []int{placementPolicyBulkConfigMapsInSecondarySnapshot},
				}
			}
			Eventually(func() error {
				works, err := placementPolicyWorks(policyKey)
				if err != nil {
					return err
				}
				// The primary Work is the one that carries the linked Work count; it controls the others.
				primaryNames := make(map[string]string, len(want))
				got := make(map[string]linkedWorksView, len(want))
				for i := range works {
					work := &works[i]
					if count, found := work.GetAnnotations()[kfplacementv1alpha1.LinkedWorkCountAnnotationKey]; found {
						primaryNames[work.Namespace] = work.Name
						view := got[work.Namespace]
						view.PrimaryLinkedWorkCount, view.PrimaryManifestCnt = count, len(work.Spec.Manifests)
						got[work.Namespace] = view
					}
				}
				for i := range works {
					work := &works[i]
					if primaryNames[work.Namespace] == work.Name {
						continue
					}
					view := got[work.Namespace]
					if controller := metav1.GetControllerOf(work); controller != nil && controller.Kind == "Work" && controller.Name == primaryNames[work.Namespace] {
						view.SecondaryManifestCnts = append(view.SecondaryManifestCnts, len(work.Spec.Manifests))
					} else {
						view.UnlinkedSecondaryWorkCnt++
					}
					got[work.Namespace] = view
				}
				if diff := cmp.Diff(got, want); diff != "" {
					return fmt.Errorf("works of cluster placement policy %s diff, keyed by namespace (-got, +want):\n%s", policyKey, diff)
				}
				return nil
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the linked Works")
		})

		It("should place every ConfigMap on every member cluster", func() {
			Eventually(func() error {
				for _, cluster := range allMemberClusters {
					cms := &corev1.ConfigMapList{}
					if err := cluster.KubeClient.List(ctx, cms, client.InNamespace(nsName), client.MatchingLabels(bulkLabels)); err != nil {
						return fmt.Errorf("cluster %s: failed to list ConfigMaps: %w", cluster.ClusterName, err)
					}
					if len(cms.Items) != placementPolicyBulkConfigMapCount {
						return fmt.Errorf("cluster %s has %d labeled ConfigMaps, want %d", cluster.ClusterName, len(cms.Items), placementPolicyBulkConfigMapCount)
					}
				}
				return nil
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place every ConfigMap on every member cluster")
		})

		It("should report every resource as synchronized and available on every binding", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), placementPolicyBulkConfigMapCount),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the resources as synchronized and available")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})
	})

	Context("cluster placement policy keeping one resource revision, rolled out by a strategy that leaves out a cluster", Ordered, func() {
		policyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyHistoryLimitNameTemplate, GinkgoParallelProcess())}
		strategyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyHistoryLimitStrategyNameTemplate, GinkgoParallelProcess())}
		runKeys := make([]types.NamespacedName, placementPolicyHistoryLimitRunCount)
		for i := range runKeys {
			runKeys[i] = types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyHistoryLimitRunNameTemplate, GinkgoParallelProcess(), i+1)}
		}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data

		var initialSnapshotName string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that keeps a single resource revision")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors:            placementPolicyResourceSelectors(),
				ResourceRevisionHistoryLimit: ptr.To(int32(1)),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)

			By("creating a strategy whose stages cover the east region only")
			// As in the strategy that leaves out a cluster: the east stage gets cluster-1 alone, and cluster-3, in the
			// west, is in no stage at all.
			strategy := newStagedUpdateStrategyObject(strategyKey, kfrolloutv1alpha1.StagedUpdateStrategySpec{
				Stages: []kfrolloutv1alpha1.Stage{
					{
						Name:          placementPolicyCanaryStageName,
						LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{envLabelName: envCanary}},
					},
					{
						Name:          placementPolicyEastStageName,
						LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{regionLabelName: regionEast}},
					},
				},
			})
			Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create cluster staged update strategy %s", strategyKey)
		})

		AfterAll(func() {
			// The work resources must go even if the cleanup below fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			for _, runKey := range runKeys {
				ensureStagedUpdateRunRemoved(runKey)
			}
			ensureStagedUpdateStrategyRemoved(strategyKey)
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the initial version on every member cluster", func() {
			initialSnapshotName = waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on every member cluster")
		})

		for i, runKey := range runKeys {
			data := map[string]string{"data": fmt.Sprintf("test-v%d", i+1)}
			var newSnapshotName string

			It(fmt.Sprintf("can roll out change %d with a cluster staged update run", i+1), func() {
				updateConfigMapDataOnHub(cmKey, data)
				run := newStagedUpdateRunObject(runKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
					PlacementPolicyName:      policyKey.Name,
					StagedUpdateStrategyName: strategyKey.Name,
				})
				Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create cluster staged update run %s", runKey)
			})

			It(fmt.Sprintf("should complete run %d, leaving the cluster outside every stage on the initial revision", i+1), func() {
				Eventually(stagedUpdateRunSucceededActual(runKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to complete cluster staged update run %s", runKey)
				newSnapshotName = resourceSnapshotNameToRolloutOf(runKey)
				Eventually(placementPolicyBindingSnapshotsActual(policyKey, map[string]string{
					memberCluster2EastCanaryName: newSnapshotName,
					memberCluster1EastProdName:   newSnapshotName,
					memberCluster3WestProdName:   initialSnapshotName,
				}), workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to leave cluster-3 on the initial resource snapshot")
				Eventually(configMapDataOnClustersActual(cmKey, data, memberCluster1EastProd, memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to place change %d on the east clusters", i+1)
			})

			It(fmt.Sprintf("should keep only the latest resource snapshot and the one still in use, after run %d", i+1), func() {
				// With a history limit of 1, only the latest snapshot is kept, except that a snapshot a binding still
				// points to is never collected. The snapshots of earlier runs are no longer in use.
				Eventually(placementPolicyPrimarySnapshotNamesActual(policyKey, initialSnapshotName, newSnapshotName), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to garbage-collect the unused resource snapshots")
			})
		}

		It("should leave cluster-3 on the initial version, reported as on an older resource revision", func() {
			Expect(configMapDataOnClustersActual(cmKey, v0Data, memberCluster3WestProd)()).To(Succeed(), "A change reached cluster-3")
			wantStatus := someClustersOnOlderResourceRevisionPlacementPolicyStatus(int32(len(allMemberClusters)), 2)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})
	})
})
