/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
)

const (
	timeout            = time.Second * 5
	interval           = time.Millisecond * 250
	consistentTimeout  = time.Second * 20
	consistentInterval = time.Second * 1
)

var (
	cmpOptions = []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
		cmpopts.SortSlices(func(c1, c2 metav1.Condition) bool {
			return c1.Type < c2.Type
		}),
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
		cmpopts.IgnoreFields(metav1.Condition{}, "Message"),
	}
)

var _ = Describe("Test the ClusterStagedUpdateRun Controller", func() {
	var testCRPName string
	var crp *placementv1beta1.ClusterResourcePlacement
	var testUpdateRunName string
	var updateRun *placementv1alpha1.ClusterStagedUpdateRun
	var policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot
	var testUpdateStrategyName string
	var updateStrategy *placementv1alpha1.ClusterStagedUpdateStrategy
	var masterSnapshot *placementv1beta1.ClusterResourceSnapshot
	numberOfClustersAnnotation := 2
	numTargetCluster := 10 // make it easier to sort the clusters by its name
	numDeletingCluster := 3

	BeforeEach(func() {
		testCRPName = "crp-" + utils.RandStr()
		testUpdateRunName = "updaterun-" + utils.RandStr()
		testUpdateStrategyName = "updatestrategy-" + utils.RandStr()
		crp = &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: testCRPName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "test-namespace",
					},
				},
				Strategy: placementv1beta1.RolloutStrategy{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type:           placementv1beta1.ApplyStrategyTypeReportDiff,
						WhenToTakeOver: placementv1beta1.WhenToTakeOverTypeIfNoDiff,
					},
				},
			},
		}
		policySnapshot = &placementv1beta1.ClusterSchedulingPolicySnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf(placementv1beta1.PolicySnapshotNameFmt, testCRPName, 2),
				Labels: map[string]string{
					placementv1beta1.CRPTrackingLabel:      testCRPName,
					placementv1beta1.IsLatestSnapshotLabel: "true",
				},
			},

			Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
				PolicyHash: []byte("hash"),
			},
		}
		updateStrategy = &placementv1alpha1.ClusterStagedUpdateStrategy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testUpdateStrategyName,
				Namespace: "default",
			},
		}
		// create the resourcesnapshot
		masterSnapshot = generateResourceSnapshot(testCRPName, 1, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		updateRun = &placementv1alpha1.ClusterStagedUpdateRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testUpdateRunName,
				Namespace: "default",
			},
			Spec: placementv1alpha1.StagedUpdateRunSpec{
				PlacementName:            testCRPName,
				ResourceSnapshotIndex:    masterSnapshot.Name,
				StagedUpdateStrategyName: testUpdateStrategyName,
			},
		}
	})

	AfterEach(func() {
		By("Deleting ClusterStagedUpdateRun")
		Expect(k8sClient.Delete(ctx, updateRun)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		updateRun = nil
		By("Deleting StagedUpdateStrategy")
		Expect(k8sClient.Delete(ctx, updateStrategy)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		updateStrategy = nil
		By("Deleting ClusterResourcePlacement")
		Expect(k8sClient.Delete(ctx, crp)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		crp = nil
		By("Deleting ClusterResourceSnapshot")
		Expect(k8sClient.Delete(ctx, masterSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		masterSnapshot = nil
		By("Deleting ClusterSchedulingPolicySnapshot")
		Expect(k8sClient.Delete(ctx, policySnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		policySnapshot = nil
	})

	Describe("Test the validateCRP function", func() {
		It("Should fail validation for non-existent CRP", func() {
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
			// we didn't create crp, so it should fail validation
			Eventually(func() error {
				return verifyFailedInitCondition(updateRun, "Parent placement not found")
			}, timeout, interval).Should(Succeed())
		})

		It("Should fail validation for CRP without external rollout strategy", func() {
			// Create a CRP object without an external rollout strategy
			crp.Spec.Strategy.Type = placementv1beta1.RollingUpdateRolloutStrategyType
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
			Eventually(func() error {
				return verifyFailedInitCondition(updateRun, "The ClusterResourcePlacement does not have an external rollout strategy")
			}, timeout, interval).Should(Succeed())
		})
	})

	Describe("Test the initialize function", func() {
		BeforeEach(func() {
			crp.Spec.Strategy.Type = placementv1beta1.ExternalRolloutStrategyType
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed())
		})

		It("Should fail validation for non-existent latest policySnapshot", func() {
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
			// we didn't create the policy snapshot, so it should fail validation
			Eventually(func() error {
				return verifyFailedInitCondition(updateRun, "no latest policy snapshot associated with cluster resource placement")
			}, timeout, interval).Should(Succeed())
		})

		It("Should fail validation for latest policySnapshot without node count annotation", func() {
			// create the policy snapshot without the number of node annotation
			Expect(k8sClient.Create(ctx, policySnapshot)).Should(Succeed())
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
			Eventually(func() error {
				return verifyFailedInitCondition(updateRun, "doesn't have node count annotation")
			}, timeout, interval).Should(Succeed())
		})

		It("Should fail validation for not scheduled policySnapshot", func() {
			// create the policy snapshot with the number of node annotation
			policySnapshot.Annotations = map[string]string{
				placementv1beta1.NumberOfClustersAnnotation: strconv.Itoa(numberOfClustersAnnotation),
			}
			Expect(k8sClient.Create(ctx, policySnapshot)).Should(Succeed())
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
			Eventually(func() error {
				return verifyFailedInitCondition(updateRun, "policy snapshot not fully scheduled yet")
			}, timeout, interval).Should(Succeed())
		})

		It("Should fail validation for no selected binding", func() {
			// create the policy snapshot with the number of node annotation and mark it as scheduled but no binding
			policySnapshot.Annotations = map[string]string{
				placementv1beta1.NumberOfClustersAnnotation: strconv.Itoa(numberOfClustersAnnotation),
			}
			Expect(k8sClient.Create(ctx, policySnapshot)).Should(Succeed())
			meta.SetStatusCondition(&policySnapshot.Status.Conditions, metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: policySnapshot.Generation,
				Reason:             "scheduled",
			})
			Expect(k8sClient.Status().Update(ctx, policySnapshot)).Should(Succeed())
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
			Eventually(func() error {
				return verifyFailedInitCondition(updateRun, "no scheduled bindings found for the policy snapshot")
			}, timeout, interval).Should(Succeed())
			By("Deleting policySnapshot")
			Expect(k8sClient.Delete(ctx, policySnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		})

		Describe("Test the stage initialization", func() {
			var bindings []*placementv1beta1.ClusterResourceBinding
			var memberClusters []*clusterv1beta1.MemberCluster
			BeforeEach(func() {
				// create the policy snapshot with the number of node annotation and mark it as scheduled
				policySnapshot.Annotations = map[string]string{
					placementv1beta1.NumberOfClustersAnnotation: strconv.Itoa(numberOfClustersAnnotation),
				}
				Expect(k8sClient.Create(ctx, policySnapshot)).Should(Succeed())
				meta.SetStatusCondition(&policySnapshot.Status.Conditions, metav1.Condition{
					Type:               string(placementv1beta1.PolicySnapshotScheduled),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: policySnapshot.Generation,
					Reason:             "scheduled",
				})
				Expect(k8sClient.Status().Update(ctx, policySnapshot)).Should(Succeed())
				clusters := make([]string, numTargetCluster)
				deletingClusters := make([]string, numDeletingCluster)
				// create scheduled bindings for master snapshot on target clusters
				for i := 0; i < numTargetCluster; i++ {
					clusters[i] = "cluster-" + strconv.Itoa(i)
					binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, testCRPName, masterSnapshot.Name, policySnapshot.Name, clusters[i])
					Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
					By(fmt.Sprintf("target resource binding  %s created", binding.Name))
					bindings = append(bindings, binding)
					mc := generateCluster(clusters[i], map[string]string{"group": "prod"})
					Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
					By(fmt.Sprintf("member cluster %s created", mc.Name))
					memberClusters = append(memberClusters, mc)
				}
				// create unscheduled bindings for master snapshot on target clusters
				for i := 0; i < numDeletingCluster; i++ {
					deletingClusters[i] = "deleting-cluster-" + strconv.Itoa(i)
					binding := generateDeletingClusterResourceBinding(testCRPName, deletingClusters[i])
					Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
					By(fmt.Sprintf("deleting resource binding  %s created", binding.Name))
					bindings = append(bindings, binding)
					mc := generateCluster(deletingClusters[i], map[string]string{"group": "staging"})
					Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
					By(fmt.Sprintf("member cluster %s created", mc.Name))
					memberClusters = append(memberClusters, mc)
				}
			})

			AfterEach(func() {
				By("Deleting ClusterResourceBindings")
				for _, binding := range bindings {
					Expect(k8sClient.Delete(ctx, binding)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
				}
				bindings = nil
				By("Deleting MemberClusters")
				for _, cluster := range memberClusters {
					Expect(k8sClient.Delete(ctx, cluster)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
				}
				memberClusters = nil
			})

			It("Should fail validation for no staged update strategy", func() {
				Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
				Eventually(func() error {
					return verifyFailedInitCondition(updateRun, "referenced update strategy not found")
				}, timeout, interval).Should(Succeed())
			})

			It("Should fail validation for no cluster selected in stage", func() {
				emptyStageName := "emptyStage"
				updateStrategy.Spec = placementv1alpha1.StagedUpdateStrategySpec{
					Stages: []placementv1alpha1.StageConfig{
						{
							Name: "duplicateStage1",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"group": "prod",
								},
							},
						},
						{
							Name: emptyStageName,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"group": "non-existent",
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, updateStrategy)).Should(Succeed())
				Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
				Eventually(func() error {
					errMsg := fmt.Sprintf("stage '%s' has no clusters selected", emptyStageName)
					return verifyFailedInitCondition(updateRun, errMsg)
				}, timeout, interval).Should(Succeed())
			})

			It("Should fail validation for one cluster selected in two stages", func() {
				mc := memberClusters[0]
				mc.Labels = map[string]string{"app": "test", "group": "prod"}
				Expect(k8sClient.Update(ctx, mc)).Should(Succeed())
				By(fmt.Sprintf("member cluster %s updated with more label", mc.Name))
				memberClusters = append(memberClusters, mc)
				updateStrategy.Spec = placementv1alpha1.StagedUpdateStrategySpec{
					Stages: []placementv1alpha1.StageConfig{
						{
							Name: "stage",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"group": "prod",
								},
							},
						},
						{
							Name: "duplicateStage",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "test",
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, updateStrategy)).Should(Succeed())
				Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
				Eventually(func() error {
					errMsg := fmt.Sprintf("cluster `%s` appears in more than one stage", mc.Name)
					return verifyFailedInitCondition(updateRun, errMsg)
				}, timeout, interval).Should(Succeed())
			})

			It("Should fail validation for cluster not selected by any stage", func() {
				// create an extra binding that is not selected by the stage
				binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, testCRPName, masterSnapshot.Name, policySnapshot.Name, "extra-cluster")
				Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("target resource binding  %s created", binding.Name))
				bindings = append(bindings, binding)
				// create a strategy with a stage that doesn't select the extra cluster
				updateStrategy.Spec = placementv1alpha1.StagedUpdateStrategySpec{
					Stages: []placementv1alpha1.StageConfig{
						{
							Name: "partialStage",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"group": "prod",
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, updateStrategy)).Should(Succeed())
				Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
				Eventually(func() error {
					return verifyFailedInitCondition(updateRun, "some clusters are not placed in any stage")
				}, timeout, interval).Should(Succeed())
			})

			It("Should fail validation for cluster not having the sorting key", func() {
				sortingKey := "order"
				// create a strategy with a stage that has a sorting key not exist in the cluster
				updateStrategy.Spec = placementv1alpha1.StagedUpdateStrategySpec{
					Stages: []placementv1alpha1.StageConfig{
						{
							Name: "partialStage",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"group": "prod",
								},
							},
							SortingLabelKey: &sortingKey,
						},
					},
				}
				Expect(k8sClient.Create(ctx, updateStrategy)).Should(Succeed())
				Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
				Eventually(func() error {
					errMsg := fmt.Sprintf("the sorting label `%s` on cluster", sortingKey)
					return verifyFailedInitCondition(updateRun, errMsg)
				}, timeout, interval).Should(Succeed())
			})

			It("Should pass validation with correct stage status with not selected clusters", func() {
				mc := generateCluster("extra-cluster", map[string]string{"group": "prod"})
				Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
				By(fmt.Sprintf("member cluster %s created", mc.Name))
				memberClusters = append(memberClusters, mc)
				// create a strategy with a stage that doesn't select the extra cluster
				updateStrategy.Spec = placementv1alpha1.StagedUpdateStrategySpec{
					Stages: []placementv1alpha1.StageConfig{
						{
							Name: "prodSage",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"group": "prod",
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, updateStrategy)).Should(Succeed())
				Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
				stagesStatus := []placementv1alpha1.StageUpdatingStatus{
					{
						StageName: updateStrategy.Spec.Stages[0].Name,
					},
				}
				deletionStageStatus := &placementv1alpha1.StageUpdatingStatus{
					StageName: placementv1alpha1.UpdateRunDeleteStageName,
				}
				i := 0
				// add the first numTargetCluster clusters to the first stage
				for i = 0; i < numTargetCluster; i++ {
					stagesStatus[0].Clusters = append(stagesStatus[0].Clusters, placementv1alpha1.ClusterUpdatingStatus{
						ClusterName: memberClusters[i].Name,
					})
				}
				// add the rest of the cluster to the deletion stage
				for ; i < numTargetCluster+numDeletingCluster; i++ {
					deletionStageStatus.Clusters = append(deletionStageStatus.Clusters, placementv1alpha1.ClusterUpdatingStatus{
						ClusterName: memberClusters[i].Name,
					})
				}
				Eventually(func() error {
					return validateSuccessfulInitStatus(updateRun, policySnapshot.Name, numberOfClustersAnnotation, crp.Spec.Strategy.ApplyStrategy, &updateStrategy.Spec, stagesStatus, deletionStageStatus)
				}, timeout, interval).Should(Succeed())
			})

			It("Should pass validation with correct stage status with multiple stages clusters", func() {
				// create extra scheduled bindings for master snapshot on target clusters as canary group
				numCanaryCluster := 2
				for i := numTargetCluster; i < numTargetCluster+numCanaryCluster; i++ {
					clusterName := "cluster-" + strconv.Itoa(i)
					binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, testCRPName, masterSnapshot.Name, policySnapshot.Name, clusterName)
					Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
					By(fmt.Sprintf("target resource binding  %s created", binding.Name))
					bindings = append(bindings, binding)
					mc := generateCluster(clusterName, map[string]string{"group": "canary"})
					Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
					By(fmt.Sprintf("member cluster %s created", mc.Name))
					memberClusters = append(memberClusters, mc)
				}
				// generate some not selected clusters
				mc := generateCluster("extra-prod-cluster", map[string]string{"group": "prod"})
				Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
				By(fmt.Sprintf("member cluster %s created", mc.Name))
				memberClusters = append(memberClusters, mc)
				mc = generateCluster("extra-canary-cluster", map[string]string{"group": "canary"})
				Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
				By(fmt.Sprintf("member cluster %s created", mc.Name))
				memberClusters = append(memberClusters, mc)
				// create a strategy with a stage that doesn't select the extra cluster
				updateStrategy.Spec = placementv1alpha1.StagedUpdateStrategySpec{
					Stages: []placementv1alpha1.StageConfig{
						{
							Name: "canaryStage",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"group": "canary",
								},
							},
						},
						{
							Name: "prodStage",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"group": "prod",
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, updateStrategy)).Should(Succeed())
				Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
				stagesStatus := []placementv1alpha1.StageUpdatingStatus{
					{
						StageName: updateStrategy.Spec.Stages[0].Name,
					},
					{
						StageName: updateStrategy.Spec.Stages[1].Name,
					},
				}
				deletionStageStatus := &placementv1alpha1.StageUpdatingStatus{
					StageName: placementv1alpha1.UpdateRunDeleteStageName,
				}
				i := 0
				// add the first numTargetCluster clusters to the prod stage
				for i = 0; i < numTargetCluster; i++ {
					stagesStatus[1].Clusters = append(stagesStatus[1].Clusters, placementv1alpha1.ClusterUpdatingStatus{
						ClusterName: memberClusters[i].Name,
					})
				}
				// add the next batch of the cluster to the deletion stage
				for ; i < numTargetCluster+numDeletingCluster; i++ {
					deletionStageStatus.Clusters = append(deletionStageStatus.Clusters, placementv1alpha1.ClusterUpdatingStatus{
						ClusterName: memberClusters[i].Name,
					})
				}
				// add the rest of the cluster to the canary stage
				for ; i < numTargetCluster+numDeletingCluster+numCanaryCluster; i++ {
					stagesStatus[0].Clusters = append(stagesStatus[0].Clusters, placementv1alpha1.ClusterUpdatingStatus{
						ClusterName: memberClusters[i].Name,
					})
				}
				Eventually(func() error {
					return validateSuccessfulInitStatus(updateRun, policySnapshot.Name, numberOfClustersAnnotation, crp.Spec.Strategy.ApplyStrategy, &updateStrategy.Spec, stagesStatus, deletionStageStatus)
				}, timeout, interval).Should(Succeed())
			})

			It("Should pass validation with correct stage status with sorting key", func() {
				// create extra scheduled bindings for master snapshot on target clusters as canary group
				numCanaryCluster := 6
				sortingKey := "order"
				for i := numTargetCluster; i < numTargetCluster+numCanaryCluster; i++ {
					clusterName := "cluster-" + strconv.Itoa(i)
					binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, testCRPName, masterSnapshot.Name, policySnapshot.Name, clusterName)
					Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
					By(fmt.Sprintf("target resource binding  %s created", binding.Name))
					bindings = append(bindings, binding)
					// set the order key reverse of the name so that the order is different from the name
					mc := generateCluster(clusterName, map[string]string{"group": "canary", sortingKey: strconv.Itoa(numTargetCluster + numCanaryCluster - i)})
					Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
					By(fmt.Sprintf("member cluster %s created", mc.Name))
					memberClusters = append(memberClusters, mc)
				}
				// generate some not selected clusters
				mc := generateCluster("extra-prod-cluster2", map[string]string{"group": "prod"})
				Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
				By(fmt.Sprintf("member cluster %s created", mc.Name))
				memberClusters = append(memberClusters, mc)
				mc = generateCluster("extra-canary-cluster3", map[string]string{"group": "canary"})
				Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
				By(fmt.Sprintf("member cluster %s created", mc.Name))
				memberClusters = append(memberClusters, mc)
				// create a strategy with a stage that doesn't select the extra cluster
				updateStrategy.Spec = placementv1alpha1.StagedUpdateStrategySpec{
					Stages: []placementv1alpha1.StageConfig{
						{
							Name: "canaryStage",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"group": "canary",
								},
							},
							SortingLabelKey: &sortingKey,
						},
						{
							Name: "prodStage",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"group": "prod",
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, updateStrategy)).Should(Succeed())
				Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())
				stagesStatus := []placementv1alpha1.StageUpdatingStatus{
					{
						StageName: updateStrategy.Spec.Stages[0].Name,
					},
					{
						StageName: updateStrategy.Spec.Stages[1].Name,
					},
				}
				deletionStageStatus := &placementv1alpha1.StageUpdatingStatus{
					StageName: placementv1alpha1.UpdateRunDeleteStageName,
				}
				i := 0
				// add the first numTargetCluster clusters to the prod stage
				for i = 0; i < numTargetCluster; i++ {
					stagesStatus[1].Clusters = append(stagesStatus[1].Clusters, placementv1alpha1.ClusterUpdatingStatus{
						ClusterName: memberClusters[i].Name,
					})
				}
				// add the next batch of the cluster to the deletion stage
				for ; i < numTargetCluster+numDeletingCluster; i++ {
					deletionStageStatus.Clusters = append(deletionStageStatus.Clusters, placementv1alpha1.ClusterUpdatingStatus{
						ClusterName: memberClusters[i].Name,
					})
				}
				// add the rest of the cluster to the canary stage in the reverse order as the sorting key is reversed
				for i = numTargetCluster + numDeletingCluster + numCanaryCluster - 1; i >= numTargetCluster+numDeletingCluster; i-- {
					stagesStatus[0].Clusters = append(stagesStatus[0].Clusters, placementv1alpha1.ClusterUpdatingStatus{
						ClusterName: memberClusters[i].Name,
					})
				}
				Eventually(func() error {
					return validateSuccessfulInitStatus(updateRun, policySnapshot.Name, numberOfClustersAnnotation, crp.Spec.Strategy.ApplyStrategy, &updateStrategy.Spec, stagesStatus, deletionStageStatus)
				}, timeout, interval).Should(Succeed())
			})
		})
	})
})

func verifyFailedInitCondition(updateRun *placementv1alpha1.ClusterStagedUpdateRun, message string) error {
	var latestUpdateRun placementv1alpha1.ClusterStagedUpdateRun
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: updateRun.Name, Namespace: updateRun.Namespace}, &latestUpdateRun); err != nil {
		return err
	}
	expectedCondition := []metav1.Condition{
		{
			Type:               string(placementv1alpha1.StagedUpdateRunConditionInitialized),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: latestUpdateRun.Generation,
			Reason:             condition.UpdateRunInitializeFailedReason,
		},
	}
	diff := cmp.Diff(expectedCondition, latestUpdateRun.Status.Conditions, cmpOptions...)
	if diff != "" {
		return fmt.Errorf("condition mismatch (-want +got):\n%s", diff)
	}
	initializedCondition := meta.FindStatusCondition(latestUpdateRun.Status.Conditions, string(placementv1alpha1.StagedUpdateRunConditionInitialized))
	if !strings.Contains(initializedCondition.Message, message) {
		return fmt.Errorf("message mismatch: %s", initializedCondition.Message)
	}
	return nil
}

func validateSuccessfulInitStatus(updateRun *placementv1alpha1.ClusterStagedUpdateRun, policySnapshotIndexUsed string,
	policyObservedClusterCount int, applyStrategy *placementv1beta1.ApplyStrategy, stagedUpdateStrategySnapshot *placementv1alpha1.StagedUpdateStrategySpec,
	stagesStatus []placementv1alpha1.StageUpdatingStatus, deletionStageStatus *placementv1alpha1.StageUpdatingStatus) error {
	var latestUpdateRun placementv1alpha1.ClusterStagedUpdateRun
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: updateRun.Name, Namespace: updateRun.Namespace}, &latestUpdateRun); err != nil {
		return err
	}
	expectedStatus := placementv1alpha1.StagedUpdateRunStatus{
		PolicySnapshotIndexUsed:      policySnapshotIndexUsed,
		PolicyObservedClusterCount:   policyObservedClusterCount,
		ApplyStrategy:                applyStrategy,
		StagedUpdateStrategySnapshot: stagedUpdateStrategySnapshot,
		StagesStatus:                 stagesStatus,
		DeletionStageStatus:          deletionStageStatus,
		Conditions: []metav1.Condition{
			{
				Type:               string(placementv1alpha1.StagedUpdateRunConditionInitialized),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: latestUpdateRun.Generation,
				Reason:             condition.UpdateRunInitializeSucceededReason,
			},
		},
	}
	diff := cmp.Diff(expectedStatus, latestUpdateRun.Status, cmpOptions...)
	if diff != "" {
		return fmt.Errorf("condition mismatch (-want +got):\n%s", diff)
	}
	return nil
}

func generateCluster(clusterName string, labels map[string]string) *clusterv1beta1.MemberCluster {
	return &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   clusterName,
			Labels: labels,
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Name:      "testUser",
				Kind:      "ServiceAccount",
				Namespace: utils.FleetSystemNamespace,
			},
			HeartbeatPeriodSeconds: 60,
		},
	}
}

func generateClusterResourceBinding(state placementv1beta1.BindingState, testCRPName, resourceSnapshotName,
	policySnapshotName, targetCluster string) *placementv1beta1.ClusterResourceBinding {
	binding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-" + resourceSnapshotName + "-" + targetCluster,
			Labels: map[string]string{
				placementv1beta1.CRPTrackingLabel: testCRPName,
			},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:                        state,
			TargetCluster:                targetCluster,
			SchedulingPolicySnapshotName: policySnapshotName,
		},
	}
	if binding.Spec.State == placementv1beta1.BindingStateBound {
		binding.Spec.ResourceSnapshotName = resourceSnapshotName
	}
	return binding
}

func generateResourceSnapshot(testCRPName string, resourceIndex int, isLatest bool) *placementv1beta1.ClusterResourceSnapshot {
	clusterResourceSnapshot := &placementv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, testCRPName, resourceIndex),
			Labels: map[string]string{
				placementv1beta1.CRPTrackingLabel:      testCRPName,
				placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(isLatest),
			},
			Annotations: map[string]string{
				placementv1beta1.ResourceGroupHashAnnotation: "hash",
			},
		},
	}
	rawContents := [][]byte{
		testResourceCRD, testNameSpace, testResource, testConfigMap, testDeployment, testService,
	}
	for _, rawContent := range rawContents {
		clusterResourceSnapshot.Spec.SelectedResources = append(clusterResourceSnapshot.Spec.SelectedResources,
			placementv1beta1.ResourceContent{
				RawExtension: runtime.RawExtension{Raw: rawContent},
			},
		)
	}
	return clusterResourceSnapshot
}

func generateDeletingClusterResourceBinding(testCRPName, targetCluster string) *placementv1beta1.ClusterResourceBinding {
	binding := generateClusterResourceBinding(placementv1beta1.BindingStateUnscheduled, testCRPName, "resourcesnapshotname", "policysnapshotname", targetCluster)
	binding.DeletionTimestamp = &metav1.Time{
		Time: time.Now(),
	}
	return binding
}
