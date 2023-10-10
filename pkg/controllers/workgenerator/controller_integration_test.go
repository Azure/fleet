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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
)

var (
	memberClusterName string
	namespaceName     string
	testCRPName       string

	ignoreConditionOption = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")
)

const (
	timeout  = time.Second * 6
	duration = time.Second * 20
	interval = time.Millisecond * 250
)

var _ = Describe("Test Work Generator Controller", func() {

	Context("Test Bound ClusterResourceBinding", func() {
		var binding *fleetv1beta1.ClusterResourceBinding
		ignoreTypeMeta := cmpopts.IgnoreFields(metav1.TypeMeta{}, "Kind", "APIVersion")
		ignoreWorkOption := cmpopts.IgnoreFields(metav1.ObjectMeta{},
			"UID", "ResourceVersion", "ManagedFields", "CreationTimestamp", "Generation")

		BeforeEach(func() {
			memberClusterName = "cluster-" + utils.RandStr()
			testCRPName = "crp" + utils.RandStr()
			namespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			})).Should(Succeed())
			By(fmt.Sprintf("Cluster namespace %s created", memberClusterName))
		})

		AfterEach(func() {
			By("Deleting Cluster namespace")
			Expect(k8sClient.Delete(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			})).Should(Succeed())
			By("Deleting ClusterResourceBinding")
			if binding != nil {
				Expect(k8sClient.Delete(ctx, binding)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			}
		})

		It("Should not create work for the binding with state scheduled", func() {
			// create master resource snapshot with 2 number of resources
			masterSnapshot := generateResourceSnapshot(1, 1, 0, [][]byte{
				testClonesetCRD, testNameSpace, testCloneset,
			})
			Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
			// create a scheduled binding
			binding = generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, memberClusterName)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			// check the work is not created since the binding state is not bound
			work := fleetv1beta1.Work{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				return apierrors.IsNotFound(err)
			}, duration, interval).Should(BeTrue(), "controller should not create work in hub cluster until all resources are created")
			// binding should not have any finalizers
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
			Expect(len(binding.Finalizers)).Should(Equal(0))
			// flip the binding state to bound and check the work is created
			binding.Spec.State = fleetv1beta1.BindingStateBound
			Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
			// check the work is created
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
			}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
			By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
			// check the binding status
			wantStatus := fleetv1beta1.ResourceBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1beta1.ResourceBindingBound),
						Status:             metav1.ConditionTrue,
						Reason:             allWorkSyncedReason,
						ObservedGeneration: binding.GetGeneration(),
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ResourceBindingApplied),
						Reason:             workNotAppliedReason,
						ObservedGeneration: binding.Generation,
					},
				},
			}
			var diff string
			Eventually(func() string {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				diff = cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
				return diff
			}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n%s", binding.Name, diff))
		})

		It("Should only creat work after all the resource snapshots are created", func() {
			// create master resource snapshot with 1 number of resources
			masterSnapshot := generateResourceSnapshot(1, 2, 0, [][]byte{
				testClonesetCRD, testNameSpace, testCloneset,
			})
			Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
			By(fmt.Sprintf("master resource snapshot `%s` created", masterSnapshot.Name))
			// create binding
			binding = generateClusterResourceBinding(fleetv1beta1.BindingStateBound, masterSnapshot.Name, memberClusterName)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding `%s` created", binding.Name))
			// check the work is not created since we have more resource snapshot to create
			work := fleetv1beta1.Work{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				return apierrors.IsNotFound(err)
			}, duration, interval).Should(BeTrue(), "controller should not create work in hub cluster until all resources are created")
			// check the binding status
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
			wantStatus := fleetv1beta1.ResourceBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1beta1.ResourceBindingBound),
						Status:             metav1.ConditionFalse,
						Reason:             syncWorkFailedReason,
						ObservedGeneration: binding.GetGeneration(),
					},
				},
			}
			diff := cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
			Expect(diff).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n%s", binding.Name, diff))
			Expect(binding.GetCondition(string(fleetv1beta1.ResourceBindingBound)).Message).Should(ContainSubstring("resource snapshots are still being created for the masterResourceSnapshot"))
			// create the second resource snapshot
			secondSnapshot := generateResourceSnapshot(1, 2, 1, [][]byte{
				testClonesetCRD, testNameSpace, testCloneset,
			})
			Expect(k8sClient.Create(ctx, secondSnapshot)).Should(Succeed())
			By(fmt.Sprintf("secondSnapshot resource snapshot `%s` created", secondSnapshot.Name))
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
			}, timeout, interval).Should(Succeed(), "should get the master work in hub cluster")
			By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: namespaceName}, &work)
			}, timeout, interval).Should(Succeed(), "should get the second work in hub cluster")
			By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
		})

		It("Should handle the case that the snapshot is deleted", func() {
			// generate master resource snapshot name but not create it
			masterSnapshot := generateResourceSnapshot(1, 1, 0, [][]byte{
				testClonesetCRD, testNameSpace, testCloneset,
			})
			// create a scheduled binding pointing to the non exist master resource snapshot
			binding = generateClusterResourceBinding(fleetv1beta1.BindingStateBound, masterSnapshot.Name, memberClusterName)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			// check the work is not created since there is no resource snapshot
			work := fleetv1beta1.Work{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				return apierrors.IsNotFound(err)
			}, duration, interval).Should(BeTrue(), "controller should not create work in hub cluster until all resources are created")
			// check the binding status
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
			wantStatus := fleetv1beta1.ResourceBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1beta1.ResourceBindingBound),
						Status:             metav1.ConditionFalse,
						Reason:             syncWorkFailedReason,
						ObservedGeneration: binding.GetGeneration(),
					},
				},
			}
			diff := cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
			Expect(diff).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n%s", binding.Name, diff))
			Expect(binding.GetCondition(string(fleetv1beta1.ResourceBindingBound)).Message).Should(Equal(errResourceSnapshotNotFound.Error()))
			// delete the binding
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s is deleted", binding.Name))
			// check the binding is deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)
				return apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "Expect the work to be deleted in the hub cluster")
			By(fmt.Sprintf("work %s is deleted in %s", work.Name, work.Namespace))
		})

		Context("Test Bound ClusterResourceBinding with a single resource snapshot", func() {
			var masterSnapshot *fleetv1beta1.ClusterResourceSnapshot

			BeforeEach(func() {
				masterSnapshot = generateResourceSnapshot(1, 1, 0, [][]byte{
					testClonesetCRD, testNameSpace, testCloneset,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				binding = generateClusterResourceBinding(fleetv1beta1.BindingStateBound, masterSnapshot.Name, memberClusterName)
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
					// only check the bound status as the applied status reason changes depends on where the reconcile logic is
					return condition.IsConditionStatusTrue(
						meta.FindStatusCondition(binding.Status.Conditions, string(fleetv1beta1.ResourceBindingBound)), binding.GetGeneration())
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("binding(%s) condition should be true", binding.Name))
				// check the work is created by now
				work := fleetv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
				//inspect the work
				wantWork := fleetv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName),
						Namespace: namespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         fleetv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: pointer.Bool(true),
							},
						},
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:                 testCRPName,
							fleetv1beta1.ParentBindingLabel:               binding.Name,
							fleetv1beta1.ParentResourceSnapshotIndexLabel: "1",
						},
					},
					Spec: fleetv1beta1.WorkSpec{
						Workload: fleetv1beta1.WorkloadTemplate{
							Manifests: []fleetv1beta1.Manifest{
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
				wantStatus := fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingBound),
							Status:             metav1.ConditionTrue,
							Reason:             allWorkSyncedReason,
							ObservedGeneration: binding.GetGeneration(),
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionFalse,
							Reason:             workNotAppliedReason,
							ObservedGeneration: binding.GetGeneration(),
						},
					},
				}
				Eventually(func() string {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
					return cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
				}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n%s", binding.Name, diff))
				// mark the work applied
				markWorkApplied(&work)
				// check the binding status that it should be marked as applied true eventually
				wantStatus = fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingBound),
							Status:             metav1.ConditionTrue,
							Reason:             allWorkSyncedReason,
							ObservedGeneration: binding.GetGeneration(),
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							Reason:             allWorkAppliedReason,
							ObservedGeneration: binding.GetGeneration(),
						},
					},
				}
				Eventually(func() string {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
					diff = cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
					return diff
				}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n%s", binding.Name, diff))
			})

			It("Should treat the unscheduled binding as bound", func() {
				// check the work is created
				work := fleetv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
				// update binding to be unscheduled
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				binding.Spec.State = fleetv1beta1.BindingStateUnscheduled
				Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s updated to be unscheduled", binding.Name))
				Consistently(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, duration, interval).Should(Succeed(), "controller should not remove work in hub cluster for unscheduled binding")
				//inspect the work manifest to make sure it still has the same content
				expectedManifest := []fleetv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testCloneset}},
				}
				diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the binding status
				wantMC := fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingBound),
							Status:             metav1.ConditionTrue,
							Reason:             allWorkSyncedReason,
							ObservedGeneration: binding.GetGeneration(),
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionFalse,
							Reason:             workNotAppliedReason,
							ObservedGeneration: binding.GetGeneration(),
						},
					},
				}
				Eventually(func() string {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
					diff = cmp.Diff(wantMC, binding.Status, ignoreConditionOption)
					return diff
				}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n%s", binding.Name, diff))
			})
		})

		Context("Test Bound ClusterResourceBinding with a single resource snapshot with envelop objects", func() {
			var masterSnapshot *fleetv1beta1.ClusterResourceSnapshot

			BeforeEach(func() {
				masterSnapshot = generateResourceSnapshot(1, 1, 0, [][]byte{
					testConfigMap, testEnvelopConfigMap, testClonesetCRD, testNameSpace,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				binding = generateClusterResourceBinding(fleetv1beta1.BindingStateBound, masterSnapshot.Name, memberClusterName)
				Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s created", binding.Name))
			})

			AfterEach(func() {
				By("Deleting master clusterResourceSnapshot")
				Expect(k8sClient.Delete(ctx, masterSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			})

			It("Should create enveloped work object in the target namespace with master resource snapshot only", func() {
				// check the work that contains none enveloped object is created by now
				work := fleetv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
				//inspect the work
				wantWork := fleetv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName),
						Namespace: namespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         fleetv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: pointer.Bool(true),
							},
						},
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:                 testCRPName,
							fleetv1beta1.ParentBindingLabel:               binding.Name,
							fleetv1beta1.ParentResourceSnapshotIndexLabel: "1",
						},
					},
					Spec: fleetv1beta1.WorkSpec{
						Workload: fleetv1beta1.WorkloadTemplate{
							Manifests: []fleetv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testConfigMap}},
								{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
								{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
							},
						},
					},
				}
				diff := cmp.Diff(wantWork, work, ignoreWorkOption, ignoreTypeMeta)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work(%s) mismatch (-want +got):\n%s", work.Name, diff))
				var workList fleetv1beta1.WorkList
				fetchEnvelopedWork(&workList, binding)
				work = workList.Items[0]
				By(fmt.Sprintf("envelope work %s is created in %s", work.Name, work.Namespace))
				//inspect the envelope work
				wantWork = fleetv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      work.Name,
						Namespace: namespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         fleetv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: pointer.Bool(true),
							},
						},
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:                 testCRPName,
							fleetv1beta1.ParentBindingLabel:               binding.Name,
							fleetv1beta1.ParentResourceSnapshotIndexLabel: "1",
							fleetv1beta1.EnvelopeTypeLabel:                string(fleetv1beta1.ConfigMapEnvelopeType),
							fleetv1beta1.EnvelopeNameLabel:                "envelop-configmap",
							fleetv1beta1.EnvelopeNamespaceLabel:           "app",
						},
					},
					Spec: fleetv1beta1.WorkSpec{
						Workload: fleetv1beta1.WorkloadTemplate{
							Manifests: []fleetv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testEnvelopeResourceQuota}},
								{RawExtension: runtime.RawExtension{Raw: testEnvelopeWebhook}},
							},
						},
					},
				}
				diff = cmp.Diff(wantWork, work, ignoreWorkOption, ignoreTypeMeta)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("envelop work(%s) mismatch (-want +got):\n%s", work.Name, diff))
			})

			It("Should modify the enveloped work object with the same name", func() {
				// make sure the enveloped work is created
				var workList fleetv1beta1.WorkList
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
					// only check the bound status as the applied status reason changes depends on where the reconcile logic is
					return condition.IsConditionStatusTrue(
						meta.FindStatusCondition(binding.Status.Conditions, string(fleetv1beta1.ResourceBindingBound)), binding.GetGeneration())
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("binding(%s) condition should be true", binding.Name))
				By(fmt.Sprintf("resource binding  %s is reconciled", binding.Name))
				// check the work that contains none enveloped object is updated
				work := fleetv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
				//inspect the work
				wantWork := fleetv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName),
						Namespace: namespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         fleetv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: pointer.Bool(true),
							},
						},
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:                 testCRPName,
							fleetv1beta1.ParentBindingLabel:               binding.Name,
							fleetv1beta1.ParentResourceSnapshotIndexLabel: "2",
						},
					},
					Spec: fleetv1beta1.WorkSpec{
						Workload: fleetv1beta1.WorkloadTemplate{
							Manifests: []fleetv1beta1.Manifest{
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
				wantWork = fleetv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      work.Name,
						Namespace: namespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         fleetv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: pointer.Bool(true),
							},
						},
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:                 testCRPName,
							fleetv1beta1.ParentBindingLabel:               binding.Name,
							fleetv1beta1.ParentResourceSnapshotIndexLabel: "2",
							fleetv1beta1.EnvelopeTypeLabel:                string(fleetv1beta1.ConfigMapEnvelopeType),
							fleetv1beta1.EnvelopeNameLabel:                "envelop-configmap",
							fleetv1beta1.EnvelopeNamespaceLabel:           "app",
						},
					},
					Spec: fleetv1beta1.WorkSpec{
						Workload: fleetv1beta1.WorkloadTemplate{
							Manifests: []fleetv1beta1.Manifest{
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
				var workList fleetv1beta1.WorkList
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
					// only check the bound status as the applied status reason changes depends on where the reconcile logic is
					return condition.IsConditionStatusTrue(
						meta.FindStatusCondition(binding.Status.Conditions, string(fleetv1beta1.ResourceBindingBound)), binding.GetGeneration())
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("binding(%s) condition should be true", binding.Name))
				By(fmt.Sprintf("resource binding  %s is reconciled", binding.Name))
				// check the enveloped work is deleted
				Eventually(func() error {
					envelopWorkLabelMatcher := client.MatchingLabels{
						fleetv1beta1.ParentBindingLabel:     binding.Name,
						fleetv1beta1.CRPTrackingLabel:       testCRPName,
						fleetv1beta1.EnvelopeTypeLabel:      string(fleetv1beta1.ConfigMapEnvelopeType),
						fleetv1beta1.EnvelopeNameLabel:      "envelop-configmap",
						fleetv1beta1.EnvelopeNamespaceLabel: "app",
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
			var masterSnapshot, secondSnapshot *fleetv1beta1.ClusterResourceSnapshot
			var binding *fleetv1beta1.ClusterResourceBinding

			BeforeEach(func() {
				masterSnapshot = generateResourceSnapshot(2, 2, 0, [][]byte{
					testClonesetCRD, testNameSpace, testCloneset,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				// create binding
				binding = generateClusterResourceBinding(fleetv1beta1.BindingStateBound, masterSnapshot.Name, memberClusterName)
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
				work := fleetv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is created in %s", work.Name, work.Namespace))
				//inspect the work manifest
				expectedManifest := []fleetv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testCloneset}},
				}
				diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the work for the secondary resource snapshot is created, it's name is crp-subindex
				secondWork := fleetv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: namespaceName}, &secondWork)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("second work %s is created in %s", secondWork.Name, secondWork.Namespace))
				//inspect the work
				wantWork := fleetv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 1),
						Namespace: namespaceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         fleetv1beta1.GroupVersion.String(),
								Kind:               "ClusterResourceBinding",
								Name:               binding.Name,
								UID:                binding.UID,
								BlockOwnerDeletion: pointer.Bool(true),
							},
						},
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:                 testCRPName,
							fleetv1beta1.ParentResourceSnapshotIndexLabel: "2",
							fleetv1beta1.ParentBindingLabel:               binding.Name,
						},
					},
					Spec: fleetv1beta1.WorkSpec{
						Workload: fleetv1beta1.WorkloadTemplate{
							Manifests: []fleetv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testConfigMap}},
								{RawExtension: runtime.RawExtension{Raw: testPdb}},
							},
						},
					},
				}
				diff = cmp.Diff(wantWork, secondWork, ignoreWorkOption, ignoreTypeMeta)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the binding status that it should be marked as applied false
				wantStatus := fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingBound),
							Status:             metav1.ConditionTrue,
							Reason:             allWorkSyncedReason,
							ObservedGeneration: binding.GetGeneration(),
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionFalse,
							Reason:             workNotAppliedReason,
							ObservedGeneration: binding.GetGeneration(),
						},
					},
				}
				Eventually(func() string {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
					diff = cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
					return diff
				}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n%s", binding.Name, diff))
				// mark both the work applied
				markWorkApplied(&work)
				markWorkApplied(&secondWork)
				// check the binding status that it should be marked as applied true eventually
				wantStatus = fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingBound),
							Status:             metav1.ConditionTrue,
							Reason:             allWorkSyncedReason,
							ObservedGeneration: binding.GetGeneration(),
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							Reason:             allWorkAppliedReason,
							ObservedGeneration: binding.GetGeneration(),
						},
					},
				}
				Eventually(func() string {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
					diff = cmp.Diff(wantStatus, binding.Status, ignoreConditionOption)
					return diff
				}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n%s", binding.Name, diff))
			})

			It("Should update existing work and create more work in the target namespace when resource snapshots change", func() {
				// check the work for the master resource snapshot is created
				work := fleetv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is created in %s", work.Name, work.Namespace))
				// check the work for the secondary resource snapshot is created, it's name is crp-subindex
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: namespaceName}, &work)
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
				expectedManifest := []fleetv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
				}
				Eventually(func() error {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
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
				expectedManifest = []fleetv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testCloneset}},
					{RawExtension: runtime.RawExtension{Raw: testConfigMap}},
				}
				Eventually(func() error {
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 1),
						Namespace: namespaceName}, &work)
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
				expectedManifest = []fleetv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testPdb}},
				}
				Eventually(func() error {
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 2),
						Namespace: namespaceName}, &work)
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
				work := fleetv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is created in %s", work.Name, work.Namespace))
				// check the work for the secondary resource snapshot is created, it's name is crp-subindex
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: namespaceName}, &work)
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
				expectedManifest := []fleetv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testCloneset}},
					{RawExtension: runtime.RawExtension{Raw: testConfigMap}},
					{RawExtension: runtime.RawExtension{Raw: testPdb}},
				}
				Eventually(func() error {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
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
						Name:      fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 1),
						Namespace: namespaceName}, &work)
					return apierrors.IsNotFound(err)
				}, duration, interval).Should(BeTrue(), "controller should remove work in hub cluster that is no longer needed")
				By(fmt.Sprintf("second work %s is deleted in %s", work.Name, work.Namespace))
			})

			It("Should remove binding after all work associated with deleted bind are deleted", func() {
				// check the work for the master resource snapshot is created
				work := fleetv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is created in %s", work.Name, work.Namespace))
				// check the work for the secondary resource snapshot is created, it's name is crp-subindex
				work2 := fleetv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: namespaceName}, &work2)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("second work %s is created in %s", work2.Name, work2.Namespace))
				// delete the binding
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s is deleted", binding.Name))

				// verify that all associated works have been deleted
				Eventually(func() error {
					workKey1 := types.NamespacedName{
						Namespace: namespaceName,
						Name:      fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName),
					}
					work1 := fleetv1beta1.Work{}
					if err := k8sClient.Get(ctx, workKey1, &work1); !apierrors.IsNotFound(err) {
						return fmt.Errorf("Work %v still exists or an unexpected error has occurred", workKey1)
					}

					workKey2 := types.NamespacedName{
						Namespace: namespaceName,
						Name:      fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 1),
					}
					work2 := fleetv1beta1.Work{}
					if err := k8sClient.Get(ctx, workKey2, &work2); !apierrors.IsNotFound(err) {
						return fmt.Errorf("Work %v still exists or an unexpected error has occurred", workKey2)
					}

					return nil
				}, timeout, interval).Should(Succeed(), "Failed to clean out all the works")

				// check the binding is deleted
				Eventually(func() bool {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)
					return apierrors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is deleted in %s", work.Name, work.Namespace))
			})
		})
	})
})

func fetchEnvelopedWork(workList *fleetv1beta1.WorkList, binding *fleetv1beta1.ClusterResourceBinding) {
	// try to locate the work that contains enveloped object
	Eventually(func() error {
		envelopWorkLabelMatcher := client.MatchingLabels{
			fleetv1beta1.ParentBindingLabel:     binding.Name,
			fleetv1beta1.CRPTrackingLabel:       testCRPName,
			fleetv1beta1.EnvelopeTypeLabel:      string(fleetv1beta1.ConfigMapEnvelopeType),
			fleetv1beta1.EnvelopeNameLabel:      "envelop-configmap",
			fleetv1beta1.EnvelopeNamespaceLabel: "app",
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

func generateClusterResourceBinding(state fleetv1beta1.BindingState, resourceSnapshotName, targetCluster string) *fleetv1beta1.ClusterResourceBinding {
	return &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-" + resourceSnapshotName,
			Labels: map[string]string{
				fleetv1beta1.CRPTrackingLabel: testCRPName,
			},
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State:                state,
			ResourceSnapshotName: resourceSnapshotName,
			TargetCluster:        targetCluster,
		},
	}
}

func generateResourceSnapshot(resourceIndex, numberResource, subIndex int, rawContents [][]byte) *fleetv1beta1.ClusterResourceSnapshot {
	var snapshotName string
	clusterResourceSnapshot := &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				fleetv1beta1.ResourceIndexLabel: strconv.Itoa(resourceIndex),
				fleetv1beta1.CRPTrackingLabel:   testCRPName,
			},
			Annotations: map[string]string{
				fleetv1beta1.NumberOfResourceSnapshotsAnnotation: strconv.Itoa(numberResource),
			},
		},
	}
	if subIndex == 0 {
		// master resource snapshot
		snapshotName = fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, resourceIndex)
	} else {
		snapshotName = fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, resourceIndex, subIndex)
		clusterResourceSnapshot.Annotations[fleetv1beta1.SubindexOfResourceSnapshotAnnotation] = strconv.Itoa(subIndex)
	}
	clusterResourceSnapshot.Name = snapshotName
	for _, rawContent := range rawContents {
		clusterResourceSnapshot.Spec.SelectedResources = append(clusterResourceSnapshot.Spec.SelectedResources, fleetv1beta1.ResourceContent{
			RawExtension: runtime.RawExtension{Raw: rawContent},
		})
	}
	return clusterResourceSnapshot
}

func markWorkApplied(work *fleetv1beta1.Work) {
	work.Status.Conditions = []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Reason:             "appliedManifest",
			Message:            "fake apply manifest",
			ObservedGeneration: work.Generation,
			LastTransitionTime: metav1.Now(),
		},
	}
	Expect(k8sClient.Status().Update(ctx, work)).Should(Succeed())
	By(fmt.Sprintf("resource work `%s` is marked as applied", work.Name))
}
