/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workgenerator

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
)

var (
	memberClusterName          string
	memberClusterNamespaceName string
	testCRPName                string

	appNamespaceName = "app"
	appNamespace     corev1.Namespace

	validClusterResourceOverrideSnapshotName   = "cro-1"
	validResourceOverrideSnapshotName          = "ro-1"
	invalidClusterResourceOverrideSnapshotName = "cro-2" // the overridden manifest is invalid

	validClusterResourceOverrideSnapshot   placementv1alpha1.ClusterResourceOverrideSnapshot
	validResourceOverrideSnapshot          placementv1alpha1.ResourceOverrideSnapshot
	invalidClusterResourceOverrideSnapshot placementv1alpha1.ClusterResourceOverrideSnapshot

	ignoreConditionOption = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")
)

const (
	timeout  = time.Second * 20
	duration = time.Second * 10
	interval = time.Millisecond * 250
)

var _ = Describe("Test Work Generator Controller", func() {

	Context("Test Bound ClusterResourceBinding", func() {
		var binding *placementv1beta1.ClusterResourceBinding
		ignoreTypeMeta := cmpopts.IgnoreFields(metav1.TypeMeta{}, "Kind", "APIVersion")
		ignoreWorkOption := cmpopts.IgnoreFields(metav1.ObjectMeta{},
			"UID", "ResourceVersion", "ManagedFields", "CreationTimestamp", "Generation")

		BeforeEach(func() {
			memberClusterName = "cluster-" + utils.RandStr()
			testCRPName = "crp" + utils.RandStr()
			memberClusterNamespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterNamespaceName,
				},
			})).Should(Succeed())
			By(fmt.Sprintf("Cluster namespace %s created", memberClusterName))

			cluster := clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
					Labels: map[string]string{
						"override": "true",
					},
				},
			}
			Expect(k8sClient.Create(ctx, &cluster)).Should(Succeed(), "Failed to create member cluster")
			By(fmt.Sprintf("Member cluster %s created", memberClusterName))
		})

		AfterEach(func() {
			By("Deleting Cluster namespace")
			Expect(k8sClient.Delete(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterNamespaceName,
				},
			})).Should(Succeed())
			By("Deleting ClusterResourceBinding")
			if binding != nil {
				Expect(k8sClient.Delete(ctx, binding)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			}

			By("Deleting the member cluster")
			Expect(k8sClient.Delete(ctx, &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
			})).Should(Succeed(), "Failed to delete the member cluster")

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName}, &clusterv1beta1.MemberCluster{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "Get() member cluster, want not found")
		})

		It("Should not create work for the binding with state scheduled", func() {
			// create master resource snapshot with 2 number of resources
			masterSnapshot := generateResourceSnapshot(1, 1, 0, [][]byte{
				testClonesetCRD, testNameSpace, testCloneset,
			})
			Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
			// create a scheduled binding
			spec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateScheduled,
				ResourceSnapshotName: masterSnapshot.Name,
				TargetCluster:        memberClusterName,
			}
			binding = generateClusterResourceBinding(spec)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			// check the work is not created since the binding state is not bound
			work := placementv1beta1.Work{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				return errors.IsNotFound(err)
			}, duration, interval).Should(BeTrue(), "controller should not create work in hub cluster until all resources are created")
			// binding should not have any finalizers
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
			Expect(len(binding.Finalizers)).Should(Equal(0))
			// flip the binding state to bound and check the work is created
			binding.Spec.State = placementv1beta1.BindingStateBound
			Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
			// check the work is created
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
			}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
			By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
			// check the binding status
			verifyBindingStatusSyncedNotApplied(binding, false, true)
		})

		It("Should only creat work after all the resource snapshots are created", func() {
			// create master resource snapshot with 1 number of resources
			masterSnapshot := generateResourceSnapshot(1, 2, 0, [][]byte{
				testClonesetCRD, testNameSpace, testCloneset,
			})
			Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
			By(fmt.Sprintf("master resource snapshot `%s` created", masterSnapshot.Name))
			// create binding
			spec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateBound,
				ResourceSnapshotName: masterSnapshot.Name,
				TargetCluster:        memberClusterName,
			}
			binding = generateClusterResourceBinding(spec)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding `%s` created", binding.Name))
			// check the work is not created since we have more resource snapshot to create
			work := placementv1beta1.Work{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				return errors.IsNotFound(err)
			}, duration, interval).Should(BeTrue(), "controller should not create work in hub cluster until all resources are created")
			// check the binding status
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
			wantStatus := placementv1beta1.ResourceBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(placementv1beta1.ResourceBindingOverridden),
						Status:             metav1.ConditionFalse,
						Reason:             condition.OverriddenFailedReason,
						ObservedGeneration: binding.GetGeneration(),
					},
				},
			}
			diff := cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
			Expect(diff).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n%s", binding.Name, diff))
			Expect(binding.GetCondition(string(placementv1beta1.ResourceBindingOverridden)).Message).Should(ContainSubstring("resource snapshots are still being created for the masterResourceSnapshot"))
			// create the second resource snapshot
			secondSnapshot := generateResourceSnapshot(1, 2, 1, [][]byte{
				testClonesetCRD, testNameSpace, testCloneset,
			})
			Expect(k8sClient.Create(ctx, secondSnapshot)).Should(Succeed())
			By(fmt.Sprintf("secondSnapshot resource snapshot `%s` created", secondSnapshot.Name))
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
			}, timeout, interval).Should(Succeed(), "should get the master work in hub cluster")
			By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: memberClusterNamespaceName}, &work)
			}, timeout, interval).Should(Succeed(), "should get the second work in hub cluster")
			By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
		})

		It("Should handle the case that the snapshot is deleted", func() {
			// generate master resource snapshot
			masterSnapshot := generateResourceSnapshot(1, 1, 0, [][]byte{
				testClonesetCRD, testNameSpace, testCloneset,
			})
			Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
			By(fmt.Sprintf("master resource snapshot `%s` created", masterSnapshot.Name))
			// create a scheduled binding pointing to the master resource snapshot
			spec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateBound,
				ResourceSnapshotName: masterSnapshot.Name,
				TargetCluster:        memberClusterName,
			}
			binding = generateClusterResourceBinding(spec)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			// check the work is created
			work := placementv1beta1.Work{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
			}, duration, interval).Should(Succeed(), "controller should create work in hub cluster")
			// check the binding status
			verifyBindingStatusSyncedNotApplied(binding, false, true)
			// delete the binding
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s is deleted", binding.Name))
			// check the binding is deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "Expect the work to be deleted in the hub cluster")
			By(fmt.Sprintf("work %s is deleted in %s", work.Name, work.Namespace))
			// check the work is deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				return errors.IsNotFound(err)
			}, duration, interval).Should(BeTrue(), "controller should delete work in hub cluster")
		})

		Context("Test Bound ClusterResourceBinding with a single resource snapshot", func() {
			var masterSnapshot *placementv1beta1.ClusterResourceSnapshot

			BeforeEach(func() {
				masterSnapshot = generateResourceSnapshot(1, 1, 0, [][]byte{
					testClonesetCRD, testNameSpace, testCloneset,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				spec := placementv1beta1.ResourceBindingSpec{
					State:                placementv1beta1.BindingStateBound,
					ResourceSnapshotName: masterSnapshot.Name,
					TargetCluster:        memberClusterName,
				}
				binding = generateClusterResourceBinding(spec)
				Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s created", binding.Name))
			})

			AfterEach(func() {
				By("Deleting master clusterResourceSnapshot")
				Expect(k8sClient.Delete(ctx, masterSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			})

			It("Should create the work in the target namespace with master resource snapshot only", func() {
				// check the binding status till the bound condition is true
				Eventually(func() bool {
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding); err != nil {
						return false
					}
					// only check the work created status as the applied status reason changes depends on where the reconcile logic is
					return condition.IsConditionStatusTrue(
						meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingWorkCreated)), binding.GetGeneration())
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("binding(%s) condition should be true", binding.Name))
				// check the work is created by now
				work := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
				//inspect the work
				wantWork := placementv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName),
						Namespace: memberClusterNamespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         placementv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: ptr.To(true),
							},
						},
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel:                 testCRPName,
							placementv1beta1.ParentBindingLabel:               binding.Name,
							placementv1beta1.ParentResourceSnapshotIndexLabel: "1",
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
								{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
								{RawExtension: runtime.RawExtension{Raw: testCloneset}},
							},
						},
					},
				}
				diff := cmp.Diff(wantWork, work, ignoreWorkOption, ignoreTypeMeta)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the binding status that it should be marked as work not applied eventually
				verifyBindingStatusSyncedNotApplied(binding, false, true)
				// mark the work applied
				markWorkApplied(&work)
				// check the binding status that it should be marked as applied true eventually
				verifyBindStatusAppliedNotAvailable(binding, false)
				// mark the work available
				markWorkAvailable(&work)
				// check the binding status that it should be marked as available true eventually
				verifyBindStatusAvail(binding, false)
			})

			It("Should treat the unscheduled binding as bound", func() {
				// check the work is created
				work := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
				// update binding to be unscheduled
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				binding.Spec.State = placementv1beta1.BindingStateUnscheduled
				Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s updated to be unscheduled", binding.Name))
				Consistently(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, duration, interval).Should(Succeed(), "controller should not remove work in hub cluster for unscheduled binding")
				//inspect the work manifest to make sure it still has the same content
				expectedManifest := []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testCloneset}},
				}
				diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the binding status
				verifyBindingStatusSyncedNotApplied(binding, false, false)
			})
		})

		Context("Test Bound ClusterResourceBinding with a single resource snapshot with envelop objects", func() {
			var masterSnapshot *placementv1beta1.ClusterResourceSnapshot

			BeforeEach(func() {
				masterSnapshot = generateResourceSnapshot(1, 1, 0, [][]byte{
					testConfigMap, testEnvelopConfigMap, testClonesetCRD, testNameSpace,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				spec := placementv1beta1.ResourceBindingSpec{
					State:                placementv1beta1.BindingStateBound,
					ResourceSnapshotName: masterSnapshot.Name,
					TargetCluster:        memberClusterName,
				}
				binding = generateClusterResourceBinding(spec)
				Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s created", binding.Name))
			})

			AfterEach(func() {
				By("Deleting master clusterResourceSnapshot")
				Expect(k8sClient.Delete(ctx, masterSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			})

			It("Should create enveloped work object in the target namespace with master resource snapshot only", func() {
				// check the work that contains none enveloped object is created by now
				work := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("normal work %s is created in %s", work.Name, work.Namespace))
				//inspect the work
				wantWork := placementv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName),
						Namespace: memberClusterNamespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         placementv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: ptr.To(true),
							},
						},
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel:                 testCRPName,
							placementv1beta1.ParentBindingLabel:               binding.Name,
							placementv1beta1.ParentResourceSnapshotIndexLabel: "1",
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testConfigMap}},
								{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
								{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
							},
						},
					},
				}
				diff := cmp.Diff(wantWork, work, ignoreWorkOption, ignoreTypeMeta)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work(%s) mismatch (-want +got):\n%s", work.Name, diff))
				//inspect the envelope work
				var workList placementv1beta1.WorkList
				fetchEnvelopedWork(&workList, binding)
				envWork := workList.Items[0]
				By(fmt.Sprintf("enveloped work %s is created in %s", envWork.Name, envWork.Namespace))
				wantWork = placementv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      envWork.Name,
						Namespace: memberClusterNamespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         placementv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: ptr.To(true),
							},
						},
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel:                 testCRPName,
							placementv1beta1.ParentBindingLabel:               binding.Name,
							placementv1beta1.ParentResourceSnapshotIndexLabel: "1",
							placementv1beta1.EnvelopeTypeLabel:                string(placementv1beta1.ConfigMapEnvelopeType),
							placementv1beta1.EnvelopeNameLabel:                "envelop-configmap",
							placementv1beta1.EnvelopeNamespaceLabel:           "app",
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testEnvelopeResourceQuota}},
								{RawExtension: runtime.RawExtension{Raw: testEnvelopeWebhook}},
							},
						},
					},
				}
				diff = cmp.Diff(wantWork, envWork, ignoreWorkOption, ignoreTypeMeta)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("enveloped work(%s) mismatch (-want +got):\n%s", envWork.Name, diff))
				// mark the enveloped work applied
				markWorkApplied(&work)
				markWorkApplied(&envWork)
				// check the binding status that it should be marked as applied true eventually
				verifyBindStatusAppliedNotAvailable(binding, false)
				// mark the enveloped work available
				markWorkAvailable(&work)
				markWorkAvailable(&envWork)
				// check the binding status that it should be marked as available true eventually
				verifyBindStatusAvail(binding, false)
			})

			It("Should modify the enveloped work object with the same name", func() {
				// make sure the enveloped work is created
				var workList placementv1beta1.WorkList
				fetchEnvelopedWork(&workList, binding)
				// create a second snapshot with a modified enveloped object
				masterSnapshot = generateResourceSnapshot(2, 1, 0, [][]byte{
					testEnvelopConfigMap2, testClonesetCRD, testNameSpace,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("another master resource snapshot  %s created", masterSnapshot.Name))
				// update binding
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				binding.Spec.ResourceSnapshotName = masterSnapshot.Name
				Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s updated", binding.Name))
				// check the binding status till the bound condition is true for the second generation
				Eventually(func() bool {
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding); err != nil {
						return false
					}
					if binding.GetGeneration() <= 1 {
						return false
					}
					// only check the work created status as the applied status reason changes depends on where the reconcile logic is
					return condition.IsConditionStatusTrue(
						meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingWorkCreated)), binding.GetGeneration())
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("binding(%s) condition should be true", binding.Name))
				By(fmt.Sprintf("resource binding  %s is reconciled", binding.Name))
				// check the work that contains none enveloped object is updated
				work := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
				//inspect the work
				wantWork := placementv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName),
						Namespace: memberClusterNamespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         placementv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: ptr.To(true),
							},
						},
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel:                 testCRPName,
							placementv1beta1.ParentBindingLabel:               binding.Name,
							placementv1beta1.ParentResourceSnapshotIndexLabel: "2",
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
								{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
							},
						},
					},
				}
				diff := cmp.Diff(wantWork, work, ignoreWorkOption, ignoreTypeMeta)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the enveloped work is updated
				fetchEnvelopedWork(&workList, binding)
				work = workList.Items[0]
				By(fmt.Sprintf("envelope work %s is updated in %s", work.Name, work.Namespace))
				//inspect the envelope work
				wantWork = placementv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      work.Name,
						Namespace: memberClusterNamespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         placementv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: ptr.To(true),
							},
						},
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel:                 testCRPName,
							placementv1beta1.ParentBindingLabel:               binding.Name,
							placementv1beta1.ParentResourceSnapshotIndexLabel: "2",
							placementv1beta1.EnvelopeTypeLabel:                string(placementv1beta1.ConfigMapEnvelopeType),
							placementv1beta1.EnvelopeNameLabel:                "envelop-configmap",
							placementv1beta1.EnvelopeNamespaceLabel:           "app",
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testEnvelopeWebhook}},
							},
						},
					},
				}
				diff = cmp.Diff(wantWork, work, ignoreWorkOption, ignoreTypeMeta)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("envelop work(%s) mismatch (-want +got):\n%s", work.Name, diff))
			})

			It("Should delete the enveloped work object in the target namespace after it's removed from snapshot", func() {
				// make sure the enveloped work is created
				var workList placementv1beta1.WorkList
				fetchEnvelopedWork(&workList, binding)
				By("create a second snapshot without an enveloped object")
				// create a second snapshot without an enveloped object
				masterSnapshot = generateResourceSnapshot(2, 1, 0, [][]byte{
					testClonesetCRD, testNameSpace,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("another master resource snapshot  %s created", masterSnapshot.Name))
				// update binding
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				binding.Spec.ResourceSnapshotName = masterSnapshot.Name
				Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s updated", binding.Name))
				// check the binding status till the bound condition is true for the second binding generation
				Eventually(func() bool {
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding); err != nil {
						return false
					}
					if binding.GetGeneration() <= 1 {
						return false
					}
					// only check the work created status as the applied status reason changes depends on where the reconcile logic is
					return condition.IsConditionStatusTrue(
						meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingWorkCreated)), binding.GetGeneration())
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("binding(%s) condition should be true", binding.Name))
				By(fmt.Sprintf("resource binding  %s is reconciled", binding.Name))
				// check the enveloped work is deleted
				Eventually(func() error {
					envelopWorkLabelMatcher := client.MatchingLabels{
						placementv1beta1.ParentBindingLabel:     binding.Name,
						placementv1beta1.CRPTrackingLabel:       testCRPName,
						placementv1beta1.EnvelopeTypeLabel:      string(placementv1beta1.ConfigMapEnvelopeType),
						placementv1beta1.EnvelopeNameLabel:      "envelop-configmap",
						placementv1beta1.EnvelopeNamespaceLabel: "app",
					}
					if err := k8sClient.List(ctx, &workList, envelopWorkLabelMatcher); err != nil {
						return err
					}
					if len(workList.Items) != 0 {
						return fmt.Errorf("expect to not get any enveloped work but got %d", len(workList.Items))
					}
					return nil
				}, timeout, interval).Should(Succeed(), "Failed to delete the expected enveloped work in hub cluster")
			})
		})

		Context("Test Bound ClusterResourceBinding with multiple resource snapshots", func() {
			var masterSnapshot, secondSnapshot *placementv1beta1.ClusterResourceSnapshot
			var binding *placementv1beta1.ClusterResourceBinding

			BeforeEach(func() {
				masterSnapshot = generateResourceSnapshot(2, 2, 0, [][]byte{
					testClonesetCRD, testNameSpace, testCloneset,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				// create binding
				spec := placementv1beta1.ResourceBindingSpec{
					State:                placementv1beta1.BindingStateBound,
					ResourceSnapshotName: masterSnapshot.Name,
					TargetCluster:        memberClusterName,
				}
				binding = generateClusterResourceBinding(spec)
				Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s created", binding.Name))
				// Now create the second resource snapshot
				secondSnapshot = generateResourceSnapshot(2, 2, 1, [][]byte{
					testConfigMap, testPdb,
				})
				Expect(k8sClient.Create(ctx, secondSnapshot)).Should(Succeed())
				By(fmt.Sprintf("secondary resource snapshot  %s created", secondSnapshot.Name))
			})

			AfterEach(func() {
				// delete the master resource snapshot
				Expect(k8sClient.Delete(ctx, masterSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
				By(fmt.Sprintf("master resource snapshot  %s deleted", masterSnapshot.Name))
				// delete the second resource snapshot
				Expect(k8sClient.Delete(ctx, secondSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
				By(fmt.Sprintf("secondary resource snapshot  %s deleted", secondSnapshot.Name))
				// delete the binding
				Expect(k8sClient.Delete(ctx, binding)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
				By(fmt.Sprintf("resource binding  %s deleted", binding.Name))
			})

			It("Should create all the work in the target namespace after all the resource snapshot are created", func() {
				// check the work for the master resource snapshot is created
				work := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is created in %s", work.Name, work.Namespace))
				//inspect the work manifest
				expectedManifest := []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testCloneset}},
				}
				diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the work for the secondary resource snapshot is created, it's name is crp-subindex
				secondWork := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: memberClusterNamespaceName}, &secondWork)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("second work %s is created in %s", secondWork.Name, secondWork.Namespace))
				//inspect the work
				wantWork := placementv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1),
						Namespace: memberClusterNamespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         placementv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: ptr.To(true),
							},
						},
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel:                 testCRPName,
							placementv1beta1.ParentResourceSnapshotIndexLabel: "2",
							placementv1beta1.ParentBindingLabel:               binding.Name,
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testConfigMap}},
								{RawExtension: runtime.RawExtension{Raw: testPdb}},
							},
						},
					},
				}
				diff = cmp.Diff(wantWork, secondWork, ignoreWorkOption, ignoreTypeMeta)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the binding status that it should be marked as applied false
				verifyBindingStatusSyncedNotApplied(binding, false, true)
				// mark both the work applied
				markWorkApplied(&work)
				// only one work applied is still just sync
				verifyBindingStatusSyncedNotApplied(binding, false, false)
				markWorkApplied(&secondWork)
				// check the binding status that it should be marked as applied true eventually
				verifyBindStatusAppliedNotAvailable(binding, false)
				// mark both the work applied
				markWorkAvailable(&work)
				// only one work available is still just applied
				verifyBindStatusAppliedNotAvailable(binding, false)
				markWorkAvailable(&secondWork)
				// check the binding status that it should be marked as applied true eventually
				verifyBindStatusAvail(binding, false)
			})

			It("Should update existing work and create more work in the target namespace when resource snapshots change", func() {
				// check the work for the master resource snapshot is created
				work := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is created in %s", work.Name, work.Namespace))
				// check the work for the secondary resource snapshot is created, it's name is crp-subindex
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("second work %s is created in %s", work.Name, work.Namespace))
				// update the master resource snapshot with 3 resources in it
				masterSnapshot = generateResourceSnapshot(3, 3, 0, [][]byte{
					testClonesetCRD, testNameSpace,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("new master resource snapshot  %s created", masterSnapshot.Name))
				// update binding
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				binding.Spec.ResourceSnapshotName = masterSnapshot.Name
				Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s updated", binding.Name))
				// Now create the second resource snapshot
				secondSnapshot = generateResourceSnapshot(3, 3, 1, [][]byte{
					testCloneset, testConfigMap,
				})
				Expect(k8sClient.Create(ctx, secondSnapshot)).Should(Succeed())
				By(fmt.Sprintf("new secondary resource snapshot  %s created", secondSnapshot.Name))
				// Now create the third resource snapshot
				thirdSnapshot := generateResourceSnapshot(3, 3, 2, [][]byte{
					testPdb,
				})
				Expect(k8sClient.Create(ctx, thirdSnapshot)).Should(Succeed())
				By(fmt.Sprintf("third resource snapshot  %s created", secondSnapshot.Name))
				// check the work for the master resource snapshot is created
				expectedManifest := []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
				}
				Eventually(func() error {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
					if err != nil {
						return err
					}
					diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
					if len(diff) != 0 {
						return fmt.Errorf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff)
					}
					return nil
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is updated in %s", work.Name, work.Namespace))
				// check the work for the secondary resource snapshot is created, it's name is crp-subindex
				expectedManifest = []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testCloneset}},
					{RawExtension: runtime.RawExtension{Raw: testConfigMap}},
				}
				Eventually(func() error {
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1),
						Namespace: memberClusterNamespaceName}, &work)
					if err != nil {
						return err
					}
					diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
					if len(diff) != 0 {
						return fmt.Errorf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff)
					}
					return nil
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("second work %s is updated in %s", work.Name, work.Namespace))
				// check the work for the third resource snapshot is created, it's name is crp-subindex
				expectedManifest = []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testPdb}},
				}
				Eventually(func() error {
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 2),
						Namespace: memberClusterNamespaceName}, &work)
					if err != nil {
						return err
					}
					diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
					if len(diff) != 0 {
						return fmt.Errorf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff)
					}
					return nil
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("third work %s is created in %s", work.Name, work.Namespace))
			})

			It("Should remove the work in the target namespace when resource snapshots change", func() {
				// check the work for the master resource snapshot is created
				work := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is created in %s", work.Name, work.Namespace))
				// check the work for the secondary resource snapshot is created, it's name is crp-subindex
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("second work %s is created in %s", work.Name, work.Namespace))
				// update the master resource snapshot with only 1 resource snapshot that contains everything in it
				masterSnapshot = generateResourceSnapshot(3, 1, 0, [][]byte{
					testClonesetCRD, testNameSpace, testCloneset, testConfigMap, testPdb,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("new master resource snapshot  %s created", masterSnapshot.Name))
				// update binding
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				binding.Spec.ResourceSnapshotName = masterSnapshot.Name
				Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s updated", binding.Name))
				//inspect the work manifest that should have been updated to contain all
				expectedManifest := []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testCloneset}},
					{RawExtension: runtime.RawExtension{Raw: testConfigMap}},
					{RawExtension: runtime.RawExtension{Raw: testPdb}},
				}
				Eventually(func() error {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
					if err != nil {
						return err
					}
					diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
					if len(diff) != 0 {
						return fmt.Errorf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff)
					}
					return nil
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is updated in %s", work.Name, work.Namespace))
				// check the second work is removed since we have less resource snapshot now
				Eventually(func() bool {
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1),
						Namespace: memberClusterNamespaceName}, &work)
					return errors.IsNotFound(err)
				}, duration, interval).Should(BeTrue(), "controller should remove work in hub cluster that is no longer needed")
				By(fmt.Sprintf("second work %s is deleted in %s", work.Name, work.Namespace))
			})

			It("Should remove binding after all work associated with deleted bind are deleted", func() {
				// check the work for the master resource snapshot is created
				work := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is created in %s", work.Name, work.Namespace))
				// check the work for the secondary resource snapshot is created, it's name is crp-subindex
				work2 := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: memberClusterNamespaceName}, &work2)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("second work %s is created in %s", work2.Name, work2.Namespace))
				// delete the binding
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s is deleted", binding.Name))

				// verify that all associated works have been deleted
				Eventually(func() error {
					workKey1 := types.NamespacedName{
						Namespace: memberClusterNamespaceName,
						Name:      fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName),
					}
					work1 := placementv1beta1.Work{}
					if err := k8sClient.Get(ctx, workKey1, &work1); !errors.IsNotFound(err) {
						return fmt.Errorf("Work %v still exists or an unexpected error has occurred", workKey1)
					}

					workKey2 := types.NamespacedName{
						Namespace: memberClusterNamespaceName,
						Name:      fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1),
					}
					work2 := placementv1beta1.Work{}
					if err := k8sClient.Get(ctx, workKey2, &work2); !errors.IsNotFound(err) {
						return fmt.Errorf("Work %v still exists or an unexpected error has occurred", workKey2)
					}

					return nil
				}, timeout, interval).Should(Succeed(), "Failed to clean out all the works")
				// check the binding is deleted
				Eventually(func() bool {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)
					return errors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is deleted in %s", work.Name, work.Namespace))
			})
		})

		Context("Test Bound ClusterResourceBinding with a single resource snapshot and valid overrides", func() {
			var masterSnapshot *placementv1beta1.ClusterResourceSnapshot

			BeforeEach(func() {

				masterSnapshot = generateResourceSnapshot(1, 1, 0, [][]byte{
					testClonesetCRD, testNameSpace, testCloneset,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				spec := placementv1beta1.ResourceBindingSpec{
					State:                placementv1beta1.BindingStateBound,
					ResourceSnapshotName: masterSnapshot.Name,
					TargetCluster:        memberClusterName,
					ClusterResourceOverrideSnapshots: []string{
						validClusterResourceOverrideSnapshotName,
					},
					ResourceOverrideSnapshots: []placementv1beta1.NamespacedName{
						{
							Name:      validResourceOverrideSnapshotName,
							Namespace: appNamespaceName,
						},
					},
				}
				binding = generateClusterResourceBinding(spec)
				Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s created", binding.Name))
			})

			AfterEach(func() {
				By("Deleting master clusterResourceSnapshot")
				Expect(k8sClient.Delete(ctx, masterSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			})

			It("Should create the work in the target namespace with master resource snapshot only", func() {
				// check the binding status till the bound condition is true
				Eventually(func() bool {
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding); err != nil {
						return false
					}
					// only check the work created status as the applied status reason changes depends on where the reconcile logic is
					return condition.IsConditionStatusTrue(
						meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingWorkCreated)), binding.GetGeneration())
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("binding(%s) condition should be true", binding.Name))
				// check the work is created by now
				work := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
				//inspect the work
				wantWork := placementv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName),
						Namespace: memberClusterNamespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         placementv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: ptr.To(true),
							},
						},
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel:                 testCRPName,
							placementv1beta1.ParentBindingLabel:               binding.Name,
							placementv1beta1.ParentResourceSnapshotIndexLabel: "1",
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
								{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
								{RawExtension: runtime.RawExtension{Raw: wantOverriddenTestCloneSet}},
							},
						},
					},
				}
				diff := cmp.Diff(wantWork, work, ignoreWorkOption, ignoreTypeMeta)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the binding status that it should be marked as work not applied eventually
				verifyBindingStatusSyncedNotApplied(binding, true, true)
				// mark the work applied
				markWorkApplied(&work)
				// check the binding status that it should be marked as applied true eventually
				verifyBindStatusAppliedNotAvailable(binding, true)
				// mark the work available
				markWorkAvailable(&work)
				// check the binding status that it should be marked as available true eventually
				verifyBindStatusAvail(binding, true)
			})

			It("Should treat the unscheduled binding as bound", func() {
				// check the work is created
				work := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
				// update binding to be unscheduled
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				binding.Spec.State = placementv1beta1.BindingStateUnscheduled
				Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s updated to be unscheduled", binding.Name))
				Consistently(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, duration, interval).Should(Succeed(), "controller should not remove work in hub cluster for unscheduled binding")
				//inspect the work manifest to make sure it still has the same content
				expectedManifest := []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: wantOverriddenTestCloneSet}},
				}
				diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the binding status
				verifyBindingStatusSyncedNotApplied(binding, true, false)
			})
		})

		Context("Test Bound ClusterResourceBinding with a single resource snapshot and invalid override", func() {
			var masterSnapshot *placementv1beta1.ClusterResourceSnapshot

			BeforeEach(func() {

				masterSnapshot = generateResourceSnapshot(1, 1, 0, [][]byte{
					testClonesetCRD, testNameSpace, testCloneset,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				spec := placementv1beta1.ResourceBindingSpec{
					State:                placementv1beta1.BindingStateBound,
					ResourceSnapshotName: masterSnapshot.Name,
					TargetCluster:        memberClusterName,
					ClusterResourceOverrideSnapshots: []string{
						invalidClusterResourceOverrideSnapshotName,
					},
				}
				binding = generateClusterResourceBinding(spec)
				Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s created", binding.Name))
			})

			AfterEach(func() {
				By("Deleting master clusterResourceSnapshot")
				Expect(k8sClient.Delete(ctx, masterSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			})

			It("Should not create the work in the target namespace", func() {
				work := placementv1beta1.Work{}
				Consistently(func() bool {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
					return errors.IsNotFound(err)
				}, duration, interval).Should(BeTrue(), "controller should not create work in hub cluster until all resources are created")
				// binding should have a finalizer
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				Expect(len(binding.Finalizers)).Should(Equal(1))

				Eventually(func() string {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
					wantStatus := placementv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourceBindingOverridden),
								Status:             metav1.ConditionFalse,
								Reason:             condition.OverriddenFailedReason,
								ObservedGeneration: binding.GetGeneration(),
							},
						},
					}
					return cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
				}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
				Expect(binding.GetCondition(string(placementv1beta1.ResourceBindingOverridden)).Message).Should(ContainSubstring("Failed to apply the override rules on the resources: add operation does not apply"))
			})
		})

		Context("Test Bound ClusterResourceBinding with a single resource snapshot and not found override", func() {
			var masterSnapshot *placementv1beta1.ClusterResourceSnapshot

			BeforeEach(func() {
				masterSnapshot = generateResourceSnapshot(1, 1, 0, [][]byte{
					testClonesetCRD, testNameSpace, testCloneset,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				spec := placementv1beta1.ResourceBindingSpec{
					State:                placementv1beta1.BindingStateBound,
					ResourceSnapshotName: masterSnapshot.Name,
					TargetCluster:        memberClusterName,
					ClusterResourceOverrideSnapshots: []string{
						"not-found-snapshot",
					},
				}
				binding = generateClusterResourceBinding(spec)
				Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s created", binding.Name))
			})

			AfterEach(func() {
				By("Deleting master clusterResourceSnapshot")
				Expect(k8sClient.Delete(ctx, masterSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			})

			It("Should not create the work in the target namespace", func() {
				work := placementv1beta1.Work{}
				Consistently(func() bool {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
					return errors.IsNotFound(err)
				}, duration, interval).Should(BeTrue(), "controller should not create work in hub cluster until all resources are created")
				// binding should have a finalizer
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				Expect(len(binding.Finalizers)).Should(Equal(1))

				Eventually(func() string {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
					wantStatus := placementv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourceBindingOverridden),
								Status:             metav1.ConditionFalse,
								Reason:             condition.OverriddenFailedReason,
								ObservedGeneration: binding.GetGeneration(),
							},
						},
					}
					return cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
				}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
			})
		})
	})

	Context("Test Bound ClusterResourceBinding with not found cluster", func() {
		var binding *placementv1beta1.ClusterResourceBinding

		BeforeEach(func() {
			spec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateBound,
				ResourceSnapshotName: "invalid-resource-snapshot",
				TargetCluster:        "non-found-cluster",
			}
			binding = generateClusterResourceBinding(spec)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
		})

		AfterEach(func() {
			By("Deleting ClusterResourceBinding")
			Expect(k8sClient.Delete(ctx, binding)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		})

		It("Should not create the work in the target namespace", func() {
			// check the work is not created since the cluster is not found
			work := placementv1beta1.Work{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				return errors.IsNotFound(err)
			}, duration, interval).Should(BeTrue(), "controller should not create work in hub cluster until all resources are created")
			// binding should not have any finalizers
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
			Expect(len(binding.Finalizers)).Should(Equal(0))
		})
	})
})

func verifyBindingStatusSyncedNotApplied(binding *placementv1beta1.ClusterResourceBinding, hasOverride, workSync bool) {
	Eventually(func() string {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
		appliedReason := workNotAppliedReason
		if workSync {
			appliedReason = workNeedSyncedReason
		}
		overrideReason := condition.OverrideNotSpecifiedReason
		if hasOverride {
			overrideReason = condition.OverriddenSucceededReason
		}

		wantStatus := placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceBindingOverridden),
					Status:             metav1.ConditionTrue,
					Reason:             overrideReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingBound),
					Status:             metav1.ConditionTrue,
					Reason:             allWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingWorkCreated),
					Status:             metav1.ConditionTrue,
					Reason:             allWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Status:             metav1.ConditionFalse,
					Type:               string(placementv1beta1.ResourceBindingApplied),
					Reason:             appliedReason,
					ObservedGeneration: binding.Generation,
				},
			},
		}
		return cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
	}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
}

func verifyBindStatusAppliedNotAvailable(binding *placementv1beta1.ClusterResourceBinding, hasOverride bool) {
	Eventually(func() string {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
		overrideReason := condition.OverrideNotSpecifiedReason
		if hasOverride {
			overrideReason = condition.OverriddenSucceededReason
		}
		wantStatus := placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceBindingOverridden),
					Status:             metav1.ConditionTrue,
					Reason:             overrideReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingBound),
					Status:             metav1.ConditionTrue,
					Reason:             allWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingWorkCreated),
					Status:             metav1.ConditionTrue,
					Reason:             allWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingApplied),
					Status:             metav1.ConditionTrue,
					Reason:             allWorkAppliedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingAvailable),
					Status:             metav1.ConditionFalse,
					Reason:             workNotAvailableReason,
					ObservedGeneration: binding.GetGeneration(),
				},
			},
		}
		return cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
	}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
}

func verifyBindStatusAvail(binding *placementv1beta1.ClusterResourceBinding, hasOverride bool) {
	Eventually(func() string {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
		overrideReason := condition.OverrideNotSpecifiedReason
		if hasOverride {
			overrideReason = condition.OverriddenSucceededReason
		}
		wantStatus := placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceBindingOverridden),
					Status:             metav1.ConditionTrue,
					Reason:             overrideReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingBound),
					Status:             metav1.ConditionTrue,
					Reason:             allWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingWorkCreated),
					Status:             metav1.ConditionTrue,
					Reason:             allWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingApplied),
					Status:             metav1.ConditionTrue,
					Reason:             allWorkAppliedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingAvailable),
					Status:             metav1.ConditionTrue,
					Reason:             allWorkAvailableReason,
					ObservedGeneration: binding.GetGeneration(),
				},
			},
		}
		return cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
	}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n", binding.Name))
}

func fetchEnvelopedWork(workList *placementv1beta1.WorkList, binding *placementv1beta1.ClusterResourceBinding) {
	// try to locate the work that contains enveloped object
	Eventually(func() error {
		envelopWorkLabelMatcher := client.MatchingLabels{
			placementv1beta1.ParentBindingLabel:     binding.Name,
			placementv1beta1.CRPTrackingLabel:       testCRPName,
			placementv1beta1.EnvelopeTypeLabel:      string(placementv1beta1.ConfigMapEnvelopeType),
			placementv1beta1.EnvelopeNameLabel:      "envelop-configmap",
			placementv1beta1.EnvelopeNamespaceLabel: "app",
		}
		if err := k8sClient.List(ctx, workList, envelopWorkLabelMatcher); err != nil {
			return err
		}
		if len(workList.Items) != 1 {
			return fmt.Errorf("expect to get one enveloped work but got %d", len(workList.Items))
		}
		return nil
	}, timeout, interval).Should(Succeed(), "Failed to get the expected enveloped work in hub cluster")
}

func generateClusterResourceBinding(spec placementv1beta1.ResourceBindingSpec) *placementv1beta1.ClusterResourceBinding {
	return &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-" + spec.ResourceSnapshotName,
			Labels: map[string]string{
				placementv1beta1.CRPTrackingLabel: testCRPName,
			},
		},
		Spec: spec,
	}
}

func generateResourceSnapshot(resourceIndex, numberResource, subIndex int, rawContents [][]byte) *placementv1beta1.ClusterResourceSnapshot {
	var snapshotName string
	clusterResourceSnapshot := &placementv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				placementv1beta1.ResourceIndexLabel: strconv.Itoa(resourceIndex),
				placementv1beta1.CRPTrackingLabel:   testCRPName,
			},
			Annotations: map[string]string{
				placementv1beta1.NumberOfResourceSnapshotsAnnotation: strconv.Itoa(numberResource),
			},
		},
	}
	if subIndex == 0 {
		// master resource snapshot
		snapshotName = fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, testCRPName, resourceIndex)
	} else {
		snapshotName = fmt.Sprintf(placementv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, resourceIndex, subIndex)
		clusterResourceSnapshot.Annotations[placementv1beta1.SubindexOfResourceSnapshotAnnotation] = strconv.Itoa(subIndex)
	}
	clusterResourceSnapshot.Name = snapshotName
	for _, rawContent := range rawContents {
		clusterResourceSnapshot.Spec.SelectedResources = append(clusterResourceSnapshot.Spec.SelectedResources, placementv1beta1.ResourceContent{
			RawExtension: runtime.RawExtension{Raw: rawContent},
		})
	}
	return clusterResourceSnapshot
}

func markWorkApplied(work *placementv1beta1.Work) {
	meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               placementv1beta1.WorkConditionTypeApplied,
		Reason:             "appliedManifest",
		Message:            "fake apply manifest",
		ObservedGeneration: work.Generation,
		LastTransitionTime: metav1.Now(),
	})
	Expect(k8sClient.Status().Update(ctx, work)).Should(Succeed())
	By(fmt.Sprintf("resource work `%s` is marked as applied", work.Name))
}

func markWorkAvailable(work *placementv1beta1.Work) {
	meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               placementv1beta1.WorkConditionTypeAvailable,
		Reason:             "availableManifest",
		Message:            "fake available manifest",
		ObservedGeneration: work.Generation,
		LastTransitionTime: metav1.Now(),
	})
	Expect(k8sClient.Status().Update(ctx, work)).Should(Succeed())
	By(fmt.Sprintf("resource work `%s` is marked as available", work.Name))
}
