/*
Copyright 2025 The KubeFleet Authors.

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

package workgenerator

import (
	"crypto/sha256"
	"encoding/json"
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

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workapplier"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
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

	bindingStatusCmpOpts = cmp.Options{
		cmpopts.SortSlices(utils.LessFuncConditionByType),
		cmpopts.SortSlices(utils.LessFuncFailedResourcePlacements),
		cmpopts.SortSlices(utils.LessFuncDriftedResourcePlacements),
		cmpopts.SortSlices(utils.LessFuncDiffedResourcePlacements),
		utils.IgnoreConditionLTTAndMessageFields,
		cmpopts.EquateEmpty(),
	}
	cmpConditionOption        = cmp.Options{cmpopts.SortSlices(utils.LessFuncFailedResourcePlacements), utils.IgnoreConditionLTTAndMessageFields, cmpopts.EquateEmpty()}
	cmpConditionOptionWithLTT = cmp.Options{cmpopts.SortSlices(utils.LessFuncFailedResourcePlacements), cmpopts.EquateEmpty()}

	fakeFailedAppliedReason  = "fakeApplyFailureReason"
	fakeFailedAppliedMessage = "fake apply failure message"

	fakeFailedAvailableReason  = "fakeNotAvailableReason"
	fakeFailedAvailableMessage = "fake not available message"

	// Define a specific time
	specificTime = time.Date(2024, time.November, 19, 15, 30, 0, 0, time.UTC)

	// Define Drift Details
	configmapPatchDetail = placementv1beta1.PatchDetail{
		Path:          "/data",
		ValueInMember: "k=1",
		ValueInHub:    "k=2",
	}
	servicePatchDetail = placementv1beta1.PatchDetail{
		Path:          "/spec/ports/1/containerPort",
		ValueInHub:    "80",
		ValueInMember: "90",
	}

	emptyArray   []string
	jsonBytes, _ = json.Marshal(emptyArray)
	emptyHash    = fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
)

var (
	ignoreTypeMeta   = cmpopts.IgnoreFields(metav1.TypeMeta{}, "Kind", "APIVersion")
	ignoreWorkOption = cmpopts.IgnoreFields(metav1.ObjectMeta{},
		"UID", "ResourceVersion", "ManagedFields", "CreationTimestamp", "Generation")
)

const (
	timeout  = time.Second * 20
	duration = time.Second * 10
	interval = time.Millisecond * 250
)

var _ = Describe("Test Work Generator Controller", func() {
	Context("Test Bound ClusterResourceBinding", func() {
		var binding *placementv1beta1.ClusterResourceBinding

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
				testResourceCRD, testNameSpace, testResource,
			})
			Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
			// create a scheduled binding
			spec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateScheduled,
				ResourceSnapshotName: masterSnapshot.Name,
				TargetCluster:        memberClusterName,
			}
			createClusterResourceBinding(&binding, spec)
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
			updateRolloutStartedGeneration(&binding)
			// check the work is created
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
			}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
			By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
			// check the binding status
			verifyBindingStatusSyncedNotApplied(binding, false, true)
		})

		It("Should only create work after all the resource snapshots are created", func() {
			// create master resource snapshot with 1 number of resources
			masterSnapshot := generateResourceSnapshot(1, 2, 0, [][]byte{
				testResourceCRD, testNameSpace, testResource,
			})
			Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
			By(fmt.Sprintf("master resource snapshot `%s` created", masterSnapshot.Name))
			// create binding
			spec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateBound,
				ResourceSnapshotName: masterSnapshot.Name,
				TargetCluster:        memberClusterName,
			}
			createClusterResourceBinding(&binding, spec)
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
						Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
						Status:             metav1.ConditionTrue,
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: binding.GetGeneration(),
					},
					{
						Type:               string(placementv1beta1.ResourceBindingOverridden),
						Status:             metav1.ConditionFalse,
						Reason:             condition.OverriddenFailedReason,
						ObservedGeneration: binding.GetGeneration(),
					},
				},
			}
			diff := cmp.Diff(wantStatus, binding.Status, cmpConditionOption)
			Expect(diff).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n%s", binding.Name, diff))
			Expect(binding.GetCondition(string(placementv1beta1.ResourceBindingOverridden)).Message).Should(ContainSubstring("resource snapshots are still being created for the masterResourceSnapshot"))
			// create the second resource snapshot
			secondSnapshot := generateResourceSnapshot(1, 2, 1, [][]byte{
				testResourceCRD, testNameSpace, testResource,
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

		It("Should handle the case that the binding is deleted", func() {
			// generate master resource snapshot
			masterSnapshot := generateResourceSnapshot(1, 1, 0, [][]byte{
				testResourceCRD, testNameSpace, testResource,
			})
			Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
			By(fmt.Sprintf("master resource snapshot `%s` created", masterSnapshot.Name))
			// create a scheduled binding pointing to the master resource snapshot
			spec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateBound,
				ResourceSnapshotName: masterSnapshot.Name,
				TargetCluster:        memberClusterName,
			}
			createClusterResourceBinding(&binding, spec)
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
					testResourceCRD, testNameSpace, testResource,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				spec := placementv1beta1.ResourceBindingSpec{
					State:                placementv1beta1.BindingStateBound,
					ResourceSnapshotName: masterSnapshot.Name,
					TargetCluster:        memberClusterName,
				}
				createClusterResourceBinding(&binding, spec)
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
						meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingWorkSynchronized)), binding.GetGeneration())
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
						Annotations: map[string]string{
							placementv1beta1.ParentResourceSnapshotNameAnnotation:                binding.Spec.ResourceSnapshotName,
							placementv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: emptyHash,
							placementv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        emptyHash,
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
								{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
								{RawExtension: runtime.RawExtension{Raw: testResource}},
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
				verifyBindStatusAvail(binding, false, false)
			})

			It("Should treat the unscheduled binding as bound and not remove work", func() {
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
				updateRolloutStartedGeneration(&binding)
				Consistently(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, duration, interval).Should(Succeed(), "controller should not remove work in hub cluster for unscheduled binding")
				//inspect the work manifest to make sure it still has the same content
				expectedManifest := []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testResource}},
				}
				diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the binding status
				verifyBindingStatusSyncedNotApplied(binding, false, false)
			})

			Context("Test Bound ClusterResourceBinding with manifests go from not applied to available", func() {
				work := placementv1beta1.Work{}
				BeforeEach(func() {
					// check the binding status till the bound condition is true
					Eventually(func() bool {
						if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding); err != nil {
							return false
						}
						// only check the work created status as the applied status reason changes depends on where the reconcile logic is
						return condition.IsConditionStatusTrue(
							meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingWorkSynchronized)), binding.GetGeneration())
					}, timeout, interval).Should(BeTrue(), fmt.Sprintf("binding(%s) condition should be true", binding.Name))
					// check the work is created by now
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
							Annotations: map[string]string{
								placementv1beta1.ParentResourceSnapshotNameAnnotation:                binding.Spec.ResourceSnapshotName,
								placementv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: emptyHash,
								placementv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        emptyHash,
							},
						},
						Spec: placementv1beta1.WorkSpec{
							Workload: placementv1beta1.WorkloadTemplate{
								Manifests: []placementv1beta1.Manifest{
									{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
									{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
									{RawExtension: runtime.RawExtension{Raw: testResource}},
								},
							},
						},
					}
					diff := cmp.Diff(wantWork, work, ignoreWorkOption, ignoreTypeMeta)
					Expect(diff).Should(BeEmpty(), fmt.Sprintf("work(%s) mismatch (-want +got):\n%s", work.Name, diff))
					// check the binding status that it should be marked as work not applied eventually
					verifyBindingStatusSyncedNotApplied(binding, false, true)
				})

				It("Should update the binding when the work overall condition changed", func() {
					// mark the work as not applied with failed manifests, and diffed manifests
					markWorkWithFailedToApplyAndNotAvailable(&work, true, true)
					// check the binding status that it should have failed placement
					verifyBindStatusNotAppliedWithTwoPlacements(binding, false, true, true, true) // mark the work applied but still not available with failed manifests
					// mark the work available directly
					markWorkAvailable(&work)
					// check the binding status that it should be marked as available true eventually with 2 drift placements
					verifyBindStatusAvail(binding, false, true)
				})

				It("Should update the binding when the failed placement list has changed but over all condition didn't change", func() {
					// mark the work as not applied with failed manifests
					markWorkWithFailedToApplyAndNotAvailable(&work, false, false)
					// check the binding status that it should have failed placement
					verifyBindStatusNotAppliedWithTwoPlacements(binding, false, true, false, false) // mark the work applied but still not available with failed manifests
					// change one of the failed manifests to be applied
					markWorkAsAppliedButNotAvailableWithFailedManifest(&work, false)
					// check the binding status that it should be applied but not available and with two failed placement
					verifyBindStatusNotAvailableWithTwoPlacements(binding, false, true, false)
					// mark one of the failed manifests as available but no change in the overall condition
					markOneManifestAvailable(&work, false)
					// check the binding status that it should be applied but not available and with one failed placement
					verifyBindStatusNotAvailableWithOnePlacement(binding, false, true, false)
					// mark the work available directly
					markWorkAvailable(&work)
					// check the binding status that it should be marked as available true eventually
					verifyBindStatusAvail(binding, false, false)
				})

				It("Should update the binding when the diffed placement list has changed but over all condition didn't change", func() {
					// mark the work as not applied with failed manifests and diffed manifests
					markWorkWithFailedToApplyAndNotAvailable(&work, true, false)
					// check the binding status that it have failed placements and diffed placements
					verifyBindStatusNotAppliedWithTwoPlacements(binding, false, true, true, false)
					// change one of the failed manifests to be applied and diff manifests to be aligned (applied is now true so no diff placements)
					markWorkAsAppliedButNotAvailableWithFailedManifest(&work, false)
					// check the binding status that it should be applied but not available and with two failed placement
					// placement list should be changed
					verifyBindStatusNotAvailableWithTwoPlacements(binding, false, true, false)
					// mark one of the failed manifests as available but no change in the overall condition
					markOneManifestAvailable(&work, false)
					// check the binding status that it should be applied but not available and with one failed placement
					verifyBindStatusNotAvailableWithOnePlacement(binding, false, true, false)
					// mark the work available directly
					markWorkAvailable(&work)
					// check the binding status that it should be marked as available true eventually
					verifyBindStatusAvail(binding, false, false)
				})

				It("Should update the binding when the drifted placement list has changed but over all condition didn't change", func() {
					// mark the work as not applied with failed manifests and drifted manifests
					markWorkWithFailedToApplyAndNotAvailable(&work, false, true)
					// check the binding status that it have failed placements and drifted placements
					verifyBindStatusNotAppliedWithTwoPlacements(binding, false, true, false, true)
					// change one of the failed manifests to be applied and drift manifest to be aligned
					markWorkAsAppliedButNotAvailableWithFailedManifest(&work, true)
					// check the binding status that it should be applied but not available and with two failed placement and 2 drifted placement
					verifyBindStatusNotAvailableWithTwoPlacements(binding, false, true, true)
					// mark one of the failed manifests as available and  no drift placements but no change in the overall condition
					markOneManifestAvailable(&work, true)
					// check the binding status that it should be applied but not available and with one failed placement and one drifted placement
					// placement list should be changed
					verifyBindStatusNotAvailableWithOnePlacement(binding, false, true, true)
					// mark the work available directly
					markWorkAvailable(&work)
					// check the binding status that it should be marked as available true eventually with one drift placement
					verifyBindStatusAvailableWithOnePlacement(binding, false)
					// mark the work with no drift placements
					markWorkWithNoDrift(&work)
					// check the binding status that it should be marked as available true eventually with no drift placement
					// placement list should be changed
					verifyBindStatusAvail(binding, false, false)

				})

				It("Should continue to update the binding status even if the master resource snapshot is deleted after the work is synced", func() {
					// delete the snapshot after the work is synced with binding
					Expect(k8sClient.Delete(ctx, masterSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
					// mark the work applied which should trigger a reconcile loop and copy the status from the work to the binding
					markWorkApplied(&work)
					// check the binding status that it should be marked as applied true eventually
					verifyBindStatusAppliedNotAvailable(binding, false)
					// delete the ParentResourceSnapshotNameAnnotation after the work is synced with binding
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)).Should(Succeed())
					delete(work.Annotations, placementv1beta1.ParentResourceSnapshotNameAnnotation)
					Expect(k8sClient.Update(ctx, &work)).Should(Succeed())
					// mark the work available which should trigger a reconcile loop and copy the status from the work to the binding even if the work has no annotation
					markWorkAvailable(&work)
					// check the binding status that it should be marked as available true eventually
					verifyBindStatusAvail(binding, false, false)
				})

				It("Should mark the binding as failed to sync if the master resource snapshot does not exist and the work do not sync ", func() {
					// delete the snapshot after the work is synced with binding
					Expect(k8sClient.Delete(ctx, masterSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
					// mark the work applied which should trigger a reconcile loop and copy the status from the work to the binding
					markWorkApplied(&work)
					// check the binding status that it should be marked as applied true eventually
					verifyBindStatusAppliedNotAvailable(binding, false)
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
					// update the resource snapshot to the next version that doesn't exist
					binding.Spec.ResourceSnapshotName = "next"
					Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
					updateRolloutStartedGeneration(&binding)
					// check the binding status that it should be marked as override succeed but not synced
					Eventually(func() string {
						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
						wantStatus := placementv1beta1.ResourceBindingStatus{
							Conditions: []metav1.Condition{
								{
									Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
									Status:             metav1.ConditionTrue,
									Reason:             condition.RolloutStartedReason,
									ObservedGeneration: binding.GetGeneration(),
								},
								{
									Type:               string(placementv1beta1.ResourceBindingOverridden),
									Status:             metav1.ConditionFalse,
									Reason:             condition.OverriddenFailedReason,
									ObservedGeneration: binding.GetGeneration(),
								},
							},
							FailedPlacements: nil,
						}
						return cmp.Diff(wantStatus, binding.Status, cmpConditionOption)
					}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
				})
			})
		})

		Context("Test Bound ClusterResourceBinding with a single resource snapshot with envelop objects", func() {
			var masterSnapshot *placementv1beta1.ClusterResourceSnapshot

			BeforeEach(func() {
				masterSnapshot = generateResourceSnapshot(1, 1, 0, [][]byte{
					testConfigMap, testEnvelopConfigMap, testResourceCRD, testNameSpace,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				spec := placementv1beta1.ResourceBindingSpec{
					State:                placementv1beta1.BindingStateBound,
					ResourceSnapshotName: masterSnapshot.Name,
					TargetCluster:        memberClusterName,
				}
				createClusterResourceBinding(&binding, spec)
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
						Annotations: map[string]string{
							placementv1beta1.ParentResourceSnapshotNameAnnotation:                binding.Spec.ResourceSnapshotName,
							placementv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: emptyHash,
							placementv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        emptyHash,
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testConfigMap}},
								{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
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
						Annotations: map[string]string{
							placementv1beta1.ParentResourceSnapshotNameAnnotation:                binding.Spec.ResourceSnapshotName,
							placementv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: emptyHash,
							placementv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        emptyHash,
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
				verifyBindStatusAvail(binding, false, false)
			})

			It("Should modify the enveloped work object with the same name", func() {
				// make sure the enveloped work is created
				var workList placementv1beta1.WorkList
				fetchEnvelopedWork(&workList, binding)
				// create a second snapshot with a modified enveloped object
				masterSnapshot = generateResourceSnapshot(2, 1, 0, [][]byte{
					testEnvelopConfigMap2, testResourceCRD, testNameSpace,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("another master resource snapshot  %s created", masterSnapshot.Name))
				// update binding
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				binding.Spec.ResourceSnapshotName = masterSnapshot.Name
				Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s updated", binding.Name))
				updateRolloutStartedGeneration(&binding)
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
						meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingWorkSynchronized)), binding.GetGeneration())
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
						Annotations: map[string]string{
							placementv1beta1.ParentResourceSnapshotNameAnnotation:                binding.Spec.ResourceSnapshotName,
							placementv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: emptyHash,
							placementv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        emptyHash,
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
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
						Annotations: map[string]string{
							placementv1beta1.ParentResourceSnapshotNameAnnotation:                binding.Spec.ResourceSnapshotName,
							placementv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: emptyHash,
							placementv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        emptyHash,
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
					testResourceCRD, testNameSpace,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("another master resource snapshot  %s created", masterSnapshot.Name))
				// update binding
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				binding.Spec.ResourceSnapshotName = masterSnapshot.Name
				Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s updated", binding.Name))
				updateRolloutStartedGeneration(&binding)
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
						meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingWorkSynchronized)), binding.GetGeneration())
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
					testResourceCRD, testNameSpace, testResource,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				// create binding
				spec := placementv1beta1.ResourceBindingSpec{
					State:                placementv1beta1.BindingStateBound,
					ResourceSnapshotName: masterSnapshot.Name,
					TargetCluster:        memberClusterName,
				}
				createClusterResourceBinding(&binding, spec)
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
					{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testResource}},
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
						Annotations: map[string]string{
							placementv1beta1.ParentResourceSnapshotNameAnnotation:                binding.Spec.ResourceSnapshotName,
							placementv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: emptyHash,
							placementv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        emptyHash,
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
				verifyBindStatusAvail(binding, false, false)
			})

			It("Should create all the work in the target namespace after some failed to apply but eventually succeeded", func() {
				// check the work for the master resource snapshot is created
				work := placementv1beta1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is created in %s", work.Name, work.Namespace))
				//inspect the work manifest
				expectedManifest := []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testResource}},
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
						Annotations: map[string]string{
							placementv1beta1.ParentResourceSnapshotNameAnnotation:                binding.Spec.ResourceSnapshotName,
							placementv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: emptyHash,
							placementv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        emptyHash,
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
				// mark one work applied while the other failed
				markWorkApplied(&work)
				markWorkWithFailedToApplyAndNotAvailable(&secondWork, false, false)
				// check the binding status that it should be marked as applied true eventually
				verifyBindStatusNotAppliedWithTwoPlacements(binding, false, true, false, false)
				// mark failed the work available
				markWorkAvailable(&secondWork)
				// only one work available is still just applied
				verifyBindStatusAppliedNotAvailable(binding, false)
				markWorkAvailable(&work)
				// check the binding status that it should be marked as applied true eventually
				verifyBindStatusAvail(binding, false, false)
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
					testResourceCRD, testNameSpace,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("new master resource snapshot  %s created", masterSnapshot.Name))
				// update binding
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				binding.Spec.ResourceSnapshotName = masterSnapshot.Name
				Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
				updateRolloutStartedGeneration(&binding)
				By(fmt.Sprintf("resource binding  %s updated", binding.Name))
				// Now create the second resource snapshot
				secondSnapshot = generateResourceSnapshot(3, 3, 1, [][]byte{
					testResource, testConfigMap,
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
					{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
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
					{RawExtension: runtime.RawExtension{Raw: testResource}},
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
					testResourceCRD, testNameSpace, testResource, testConfigMap, testPdb,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("new master resource snapshot  %s created", masterSnapshot.Name))
				// update binding
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				binding.Spec.ResourceSnapshotName = masterSnapshot.Name
				Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
				updateRolloutStartedGeneration(&binding)
				By(fmt.Sprintf("resource binding  %s updated", binding.Name))
				//inspect the work manifest that should have been updated to contain all
				expectedManifest := []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testResource}},
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
			var roHash, croHash string

			BeforeEach(func() {
				masterSnapshot = generateResourceSnapshot(1, 1, 0, [][]byte{
					testResourceCRD, testNameSpace, testResource,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				crolist := []string{validClusterResourceOverrideSnapshotName}
				roList := []placementv1beta1.NamespacedName{
					{
						Name:      validResourceOverrideSnapshotName,
						Namespace: appNamespaceName,
					},
				}
				spec := placementv1beta1.ResourceBindingSpec{
					State:                            placementv1beta1.BindingStateBound,
					ResourceSnapshotName:             masterSnapshot.Name,
					TargetCluster:                    memberClusterName,
					ClusterResourceOverrideSnapshots: crolist,
					ResourceOverrideSnapshots:        roList,
				}
				createClusterResourceBinding(&binding, spec)
				jsonBytes, _ := json.Marshal(roList)
				roHash = fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
				jsonBytes, _ = json.Marshal(crolist)
				croHash = fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
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
						meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingWorkSynchronized)), binding.GetGeneration())
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
						Annotations: map[string]string{
							placementv1beta1.ParentResourceSnapshotNameAnnotation:                binding.Spec.ResourceSnapshotName,
							placementv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: croHash,
							placementv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        roHash,
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
								{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
								{RawExtension: runtime.RawExtension{Raw: wantOverriddenTestResource}},
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
				verifyBindStatusAvail(binding, true, false)
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
				updateRolloutStartedGeneration(&binding)
				Consistently(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, duration, interval).Should(Succeed(), "controller should not remove work in hub cluster for unscheduled binding")
				//inspect the work manifest to make sure it still has the same content
				expectedManifest := []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: wantOverriddenTestResource}},
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
					testResourceCRD, testNameSpace, testResource,
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
				createClusterResourceBinding(&binding, spec)
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
								Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: binding.GetGeneration(),
							},
							{
								Type:               string(placementv1beta1.ResourceBindingOverridden),
								Status:             metav1.ConditionFalse,
								Reason:             condition.OverriddenFailedReason,
								ObservedGeneration: binding.GetGeneration(),
							},
						},
					}
					return cmp.Diff(wantStatus, binding.Status, cmpConditionOption)
				}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
				Expect(binding.GetCondition(string(placementv1beta1.ResourceBindingOverridden)).Message).Should(ContainSubstring("Failed to apply the override rules on the resources: add operation does not apply"))
			})
		})

		Context("Test Bound ClusterResourceBinding with a single resource snapshot and not found override", func() {
			var masterSnapshot *placementv1beta1.ClusterResourceSnapshot

			BeforeEach(func() {
				masterSnapshot = generateResourceSnapshot(1, 1, 0, [][]byte{
					testResourceCRD, testNameSpace, testResource,
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
				createClusterResourceBinding(&binding, spec)
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
								Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: binding.GetGeneration(),
							},
							{
								Type:               string(placementv1beta1.ResourceBindingOverridden),
								Status:             metav1.ConditionFalse,
								Reason:             condition.OverriddenFailedReason,
								ObservedGeneration: binding.GetGeneration(),
							},
						},
					}
					return cmp.Diff(wantStatus, binding.Status, cmpConditionOption)
				}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
			})
		})

		Context("Should not touch/reset RolloutStarted condition when the binding is updated", func() {
			var masterSnapshot *placementv1beta1.ClusterResourceSnapshot

			BeforeEach(func() {
				masterSnapshot = generateResourceSnapshot(1, 1, 0, [][]byte{
					testResourceCRD, testNameSpace, testResource,
				})
				Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
				By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
				spec := placementv1beta1.ResourceBindingSpec{
					State:                placementv1beta1.BindingStateBound,
					ResourceSnapshotName: masterSnapshot.Name,
					TargetCluster:        memberClusterName,
				}
				createClusterResourceBinding(&binding, spec)
			})

			AfterEach(func() {
				By("Deleting master clusterResourceSnapshot")
				Expect(k8sClient.Delete(ctx, masterSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			})

			It("Should create all the work in the target namespace after the resource snapshot is created", func() {
				// check the binding status till the bound condition is true
				Eventually(func() bool {
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding); err != nil {
						return false
					}
					// only check the work created status as the applied status reason changes depends on where the reconcile logic is
					return condition.IsConditionStatusTrue(
						meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingWorkSynchronized)), binding.GetGeneration())
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
						Annotations: map[string]string{
							placementv1beta1.ParentResourceSnapshotNameAnnotation:                binding.Spec.ResourceSnapshotName,
							placementv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: emptyHash,
							placementv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        emptyHash,
						},
					},
					Spec: placementv1beta1.WorkSpec{
						Workload: placementv1beta1.WorkloadTemplate{
							Manifests: []placementv1beta1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
								{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
								{RawExtension: runtime.RawExtension{Raw: testResource}},
							},
						},
					},
				}
				diff := cmp.Diff(wantWork, work, ignoreWorkOption, ignoreTypeMeta)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the binding status that it should be marked as work not applied eventually
				verifyBindingStatusSyncedNotApplied(binding, false, true)
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				rolloutCond := binding.GetCondition(string(placementv1beta1.ResourceBindingRolloutStarted))
				// mark the work applied
				markWorkApplied(&work)
				// check the binding status that it should be marked as applied true eventually
				verifyBindStatusAppliedNotAvailable(binding, false)
				checkRolloutStartedNotUpdated(rolloutCond, binding)
				// mark the work available
				markWorkAvailable(&work)
				// check the binding status that it should be marked as available true eventually
				verifyBindStatusAvail(binding, false, false)
				checkRolloutStartedNotUpdated(rolloutCond, binding)
			})

			It("Should treat the unscheduled binding as bound and not remove work", func() {
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
				updateRolloutStartedGeneration(&binding)
				rolloutCond := binding.GetCondition(string(placementv1beta1.ResourceBindingRolloutStarted))
				Consistently(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName), Namespace: memberClusterNamespaceName}, &work)
				}, duration, interval).Should(Succeed(), "controller should not remove work in hub cluster for unscheduled binding")
				//inspect the work manifest to make sure it still has the same content
				expectedManifest := []placementv1beta1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testResourceCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testResource}},
				}
				diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the binding status
				verifyBindingStatusSyncedNotApplied(binding, false, false)
				checkRolloutStartedNotUpdated(rolloutCond, binding)
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
			createClusterResourceBinding(&binding, spec)
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

	Context("Sync apply strategy changes (apply -> apply, works with valid resource snapshots)", Ordered, func() {
		var masterResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
		var clusterResourceBinding *placementv1beta1.ClusterResourceBinding

		BeforeAll(func() {
			memberClusterName = "cluster-" + utils.RandStr()
			memberCluster := clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberCluster)).Should(Succeed(), "Failed to create member cluster")

			memberClusterNamespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
			memberClusterReservedNS := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterNamespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberClusterReservedNS)).Should(Succeed(), "Failed to create namespace")

			testCRPName = "crp-" + utils.RandStr()
		})

		It("can create master resource snapshot and resource binding", func() {
			// Create the master resource snapshot.
			masterResourceSnapshot = generateResourceSnapshot(
				1, 1, 0,
				[][]byte{
					testResourceCRD, testNameSpace, testResource,
				})
			Expect(k8sClient.Create(ctx, masterResourceSnapshot)).Should(Succeed(), "Failed to create master resource snapshot")

			resourceBindingSpec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateBound,
				ResourceSnapshotName: masterResourceSnapshot.Name,
				TargetCluster:        memberClusterName,
			}
			createClusterResourceBinding(&clusterResourceBinding, resourceBindingSpec)
		})

		It("should create work object with no apply strategy", func() {
			workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName)
			applyStrategyUpdatedActual := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName, nil)
			Eventually(applyStrategyUpdatedActual, timeout, interval).Should(Succeed(), "Failed to create expected work object")
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkApplyInProcess,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, nil, nil,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update apply strategy on the resource binding", func() {
			// Update the apply strategy on the resource binding; use an Eventually
			// block to avoid conflict induced errors.
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterResourceBinding.Name}, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to get resource binding: %w", err)
				}

				clusterResourceBinding.Spec.ApplyStrategy = &placementv1beta1.ApplyStrategy{
					ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
					Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeNever,
				}

				if err := k8sClient.Update(ctx, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to update resource binding: %w", err)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to update apply strategy on the resource binding")
		})

		It("can add the rollout started condition to the resource binding", func() {
			updateRolloutStartedGeneration(&clusterResourceBinding)
		})

		It("should update apply strategy on the work object", func() {
			workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName)
			wantApplyStrategy := &placementv1beta1.ApplyStrategy{
				ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
				WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
				Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
				WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeNever,
			}
			applyStrategyUpdatedActual := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName, wantApplyStrategy)
			Eventually(applyStrategyUpdatedActual, timeout, interval).Should(Succeed(), "Failed to create expected work object")
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkApplyInProcess,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, nil, nil,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		AfterAll(func() {
			cleanupResources(memberClusterNamespaceName, clusterResourceBinding, masterResourceSnapshot, nil, memberClusterName)
		})
	})

	Context("Sync apply strategy changes (apply -> apply, works with missing resource snapshots)", Ordered, func() {
		var masterResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
		var clusterResourceBinding *placementv1beta1.ClusterResourceBinding

		BeforeAll(func() {
			memberClusterName = "cluster-" + utils.RandStr()
			memberCluster := clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberCluster)).Should(Succeed(), "Failed to create member cluster")

			memberClusterNamespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
			memberClusterReservedNS := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterNamespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberClusterReservedNS)).Should(Succeed(), "Failed to create namespace")

			testCRPName = "crp-" + utils.RandStr()
		})

		It("can create master resource snapshot and resource binding", func() {
			// Create the master resource snapshot.
			masterResourceSnapshot = generateResourceSnapshot(
				1, 1, 0,
				[][]byte{
					testResourceCRD, testNameSpace, testResource,
				})
			Expect(k8sClient.Create(ctx, masterResourceSnapshot)).Should(Succeed(), "Failed to create master resource snapshot")

			resourceBindingSpec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateBound,
				ResourceSnapshotName: masterResourceSnapshot.Name,
				TargetCluster:        memberClusterName,
			}
			createClusterResourceBinding(&clusterResourceBinding, resourceBindingSpec)
		})

		It("should create work object with no apply strategy", func() {
			workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName)
			applyStrategyUpdatedActual := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName, nil)
			Eventually(applyStrategyUpdatedActual, timeout, interval).Should(Succeed(), "Failed to create expected work object")
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkApplyInProcess,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, nil, nil,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can delete the master resource snapshot", func() {
			Expect(k8sClient.Delete(ctx, masterResourceSnapshot)).Should(Succeed(), "Failed to delete master resource snapshot")
		})

		It("can update apply strategy on the resource binding", func() {
			// Update the apply strategy on the resource binding; use an Eventually
			// block to avoid conflict induced errors.
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterResourceBinding.Name}, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to get resource binding: %w", err)
				}

				clusterResourceBinding.Spec.ApplyStrategy = &placementv1beta1.ApplyStrategy{
					ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
					Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeNever,
				}

				if err := k8sClient.Update(ctx, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to update resource binding: %w", err)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to update apply strategy on the resource binding")
		})

		It("can add the rollout started condition to the resource binding", func() {
			updateRolloutStartedGeneration(&clusterResourceBinding)
		})

		It("should update apply strategy on the work object", func() {
			workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName)
			wantApplyStrategy := &placementv1beta1.ApplyStrategy{
				ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
				WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
				Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
				WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeNever,
			}
			applyStrategyUpdatedActual := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName, wantApplyStrategy)
			Eventually(applyStrategyUpdatedActual, timeout, interval).Should(Succeed(), "Failed to create expected work object")
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkApplyInProcess,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, nil, nil,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		AfterAll(func() {
			cleanupResources(memberClusterNamespaceName, clusterResourceBinding, masterResourceSnapshot, nil, memberClusterName)
		})
	})

	Context("Sync apply strategy changes (apply -> reportDiff, works with valid resource snapshots)", Ordered, func() {
		var masterResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
		var clusterResourceBinding *placementv1beta1.ClusterResourceBinding

		BeforeAll(func() {
			memberClusterName = "cluster-" + utils.RandStr()
			memberCluster := clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberCluster)).Should(Succeed(), "Failed to create member cluster")

			memberClusterNamespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
			memberClusterReservedNS := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterNamespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberClusterReservedNS)).Should(Succeed(), "Failed to create namespace")

			testCRPName = "crp-" + utils.RandStr()
		})

		It("can create master resource snapshot and resource binding", func() {
			// Create the master resource snapshot.
			masterResourceSnapshot = generateResourceSnapshot(
				1, 1, 0,
				[][]byte{
					testResourceCRD, testNameSpace, testResource,
				})
			Expect(k8sClient.Create(ctx, masterResourceSnapshot)).Should(Succeed(), "Failed to create master resource snapshot")

			resourceBindingSpec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateBound,
				ResourceSnapshotName: masterResourceSnapshot.Name,
				TargetCluster:        memberClusterName,
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type: placementv1beta1.ApplyStrategyTypeServerSideApply,
				},
			}
			createClusterResourceBinding(&clusterResourceBinding, resourceBindingSpec)
		})

		It("should create work object with server side apply apply strategy", func() {
			workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName)
			applyStrategy := &placementv1beta1.ApplyStrategy{
				ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
				Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
				WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
				WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
			}
			applyStrategyUpdatedActual := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName, applyStrategy)
			Eventually(applyStrategyUpdatedActual, timeout, interval).Should(Succeed(), "Failed to create expected work object")
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkApplyInProcess,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, nil, nil,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update apply strategy on the resource binding", func() {
			// Update the apply strategy on the resource binding; use an Eventually
			// block to avoid conflict induced errors.
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterResourceBinding.Name}, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to get resource binding: %w", err)
				}

				clusterResourceBinding.Spec.ApplyStrategy = &placementv1beta1.ApplyStrategy{
					Type: placementv1beta1.ApplyStrategyTypeReportDiff,
				}

				if err := k8sClient.Update(ctx, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to update resource binding: %w", err)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to update apply strategy on the resource binding")
		})

		It("can add the rollout started condition to the resource binding", func() {
			updateRolloutStartedGeneration(&clusterResourceBinding)
		})

		It("should update apply strategy on the work object", func() {
			workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName)
			wantApplyStrategy := &placementv1beta1.ApplyStrategy{
				ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
				Type:             placementv1beta1.ApplyStrategyTypeReportDiff,
				WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
				WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
			}
			applyStrategyUpdatedActual := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName, wantApplyStrategy)
			Eventually(applyStrategyUpdatedActual, timeout, interval).Should(Succeed(), "Failed to create expected work object")
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingDiffReported),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkDiffReportInProcess,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, nil, nil,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		AfterAll(func() {
			cleanupResources(memberClusterNamespaceName, clusterResourceBinding, masterResourceSnapshot, nil, memberClusterName)
		})
	})

	Context("Sync apply strategy changes (reportDiff -> apply, works with valid resource snapshots)", Ordered, func() {
		var masterResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
		var clusterResourceBinding *placementv1beta1.ClusterResourceBinding

		BeforeAll(func() {
			memberClusterName = "cluster-" + utils.RandStr()
			memberCluster := clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberCluster)).Should(Succeed(), "Failed to create member cluster")

			memberClusterNamespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
			memberClusterReservedNS := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterNamespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberClusterReservedNS)).Should(Succeed(), "Failed to create namespace")

			testCRPName = "crp-" + utils.RandStr()
		})

		It("can create master resource snapshot and resource binding", func() {
			// Create the master resource snapshot.
			masterResourceSnapshot = generateResourceSnapshot(
				1, 1, 0,
				[][]byte{
					testResourceCRD, testNameSpace, testResource,
				})
			Expect(k8sClient.Create(ctx, masterResourceSnapshot)).Should(Succeed(), "Failed to create master resource snapshot")

			resourceBindingSpec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateBound,
				ResourceSnapshotName: masterResourceSnapshot.Name,
				TargetCluster:        memberClusterName,
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type: placementv1beta1.ApplyStrategyTypeReportDiff,
				},
			}
			createClusterResourceBinding(&clusterResourceBinding, resourceBindingSpec)
		})

		It("should create work object with no apply strategy", func() {
			workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName)
			applyStrategy := &placementv1beta1.ApplyStrategy{
				ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
				Type:             placementv1beta1.ApplyStrategyTypeReportDiff,
				WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
				WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
			}
			applyStrategyUpdatedActual := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName, applyStrategy)
			Eventually(applyStrategyUpdatedActual, timeout, interval).Should(Succeed(), "Failed to create expected work object")
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingDiffReported),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkDiffReportInProcess,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, nil, nil,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update apply strategy on the resource binding", func() {
			// Update the apply strategy on the resource binding; use an Eventually
			// block to avoid conflict induced errors.
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterResourceBinding.Name}, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to get resource binding: %w", err)
				}

				clusterResourceBinding.Spec.ApplyStrategy = &placementv1beta1.ApplyStrategy{
					Type: placementv1beta1.ApplyStrategyTypeClientSideApply,
				}

				if err := k8sClient.Update(ctx, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to update resource binding: %w", err)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to update apply strategy on the resource binding")
		})

		It("can add the rollout started condition to the resource binding", func() {
			updateRolloutStartedGeneration(&clusterResourceBinding)
		})

		It("should update apply strategy on the work object", func() {
			workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName)
			wantApplyStrategy := &placementv1beta1.ApplyStrategy{
				ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
				Type:             placementv1beta1.ApplyStrategyTypeReportDiff,
				WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
				WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
			}
			applyStrategyUpdatedActual := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName, wantApplyStrategy)
			Eventually(applyStrategyUpdatedActual, timeout, interval).Should(Succeed(), "Failed to create expected work object")
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkApplyInProcess,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, nil, nil,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		AfterAll(func() {
			cleanupResources(memberClusterNamespaceName, clusterResourceBinding, masterResourceSnapshot, nil, memberClusterName)
		})
	})

	Context("Refresh status given different apply strategies (ReportDiff -> ClientSideApply)", Ordered, func() {
		var masterResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
		var secondaryResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
		var clusterResourceBinding *placementv1beta1.ClusterResourceBinding

		now := metav1.Now().Rfc3339Copy()

		BeforeAll(func() {
			memberClusterName = "cluster-" + utils.RandStr()
			memberCluster := clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberCluster)).Should(Succeed(), "Failed to create member cluster")

			memberClusterNamespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
			memberClusterReservedNS := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterNamespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberClusterReservedNS)).Should(Succeed(), "Failed to create namespace")

			testCRPName = "crp-" + utils.RandStr()
		})

		It("can create multiple resource snapshots and resource binding", func() {
			// Create the master resource snapshot.
			masterResourceSnapshot = generateResourceSnapshot(
				1, 2, 0,
				[][]byte{
					testResourceCRD, testNameSpace,
				},
			)
			Expect(k8sClient.Create(ctx, masterResourceSnapshot)).Should(Succeed(), "Failed to create master resource snapshot")

			secondaryResourceSnapshot = generateResourceSnapshot(
				1, 2, 1,
				[][]byte{
					testResource,
				},
			)
			Expect(k8sClient.Create(ctx, secondaryResourceSnapshot)).Should(Succeed(), "Failed to create secondary resource snapshot")

			resourceBindingSpec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateBound,
				ResourceSnapshotName: masterResourceSnapshot.Name,
				TargetCluster:        memberClusterName,
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type: placementv1beta1.ApplyStrategyTypeReportDiff,
				},
			}
			createClusterResourceBinding(&clusterResourceBinding, resourceBindingSpec)
		})

		It("can update work status (only one work reported diff)", func() {
			work := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName))

			diffReportedCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeDiffReported),
				Status:             metav1.ConditionTrue,
				Reason:             "DiffReportedOnAllManifests",
				ObservedGeneration: work.GetGeneration(),
			}

			manifestConds := []placementv1beta1.ManifestCondition{
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:  1,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingReportDiffResultTypeNoDiffFound),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
				},
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:   2,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      "app",
						Namespace: "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingReportDiffResultTypeFoundDiff),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
					DiffDetails: &placementv1beta1.DiffDetails{
						ObservationTime:                   now,
						FirstDiffedObservedTime:           now,
						ObservedInMemberClusterGeneration: ptr.To(int64(1)),
						ObservedDiffs: []placementv1beta1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "1",
								ValueInHub:    "2",
							},
						},
					},
				},
			}
			refreshWorkStatus(work, nil, nil, diffReportedCond, manifestConds)
		})

		It("should update binding status as expected", func() {
			diffReportedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingDiffReported),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkNotDiffReportedReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			// Diff information is not populated as one Work object has not reported diff.
			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				nil, nil, &diffReportedCond,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update work status (one work has reported diff; the other work has failed to report diff)", func() {
			work := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1))

			diffReportedCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeDiffReported),
				Status:             metav1.ConditionFalse,
				Reason:             "DiffReportingFailedOnSomeManifests",
				ObservedGeneration: work.GetGeneration(),
			}

			manifestConds := []placementv1beta1.ManifestCondition{
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Resource:  "configmaps",
						Name:      "app",
						Namespace: "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionFalse,
							Reason:             string(workapplier.ManifestProcessingReportDiffResultTypeFailed),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
				},
			}
			refreshWorkStatus(work, nil, nil, diffReportedCond, manifestConds)
		})

		It("should update binding status as expected", func() {
			diffReportedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingDiffReported),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkNotDiffReportedReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			// Diff information is not populated as one Work object has failed to report diff.
			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				nil, nil, &diffReportedCond,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update work status (all works have diff reported)", func() {
			work := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1))

			diffReportedCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeDiffReported),
				Status:             metav1.ConditionTrue,
				Reason:             "DiffReportedOnAllManifests",
				ObservedGeneration: work.GetGeneration(),
			}

			manifestConds := []placementv1beta1.ManifestCondition{
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Resource:  "configmaps",
						Name:      "app",
						Namespace: "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingReportDiffResultTypeNoDiffFound),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
				},
			}
			refreshWorkStatus(work, nil, nil, diffReportedCond, manifestConds)
		})

		It("should update binding status as expected", func() {
			diffReportedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingDiffReported),
				Status:             metav1.ConditionTrue,
				Reason:             condition.AllWorkDiffReportedReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}
			diffedPlacements := []placementv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "app",
						Namespace: "work",
					},
					ObservationTime:                 now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					FirstDiffedObservedTime:         now,
					ObservedDiffs: []placementv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInMember: "1",
							ValueInHub:    "2",
						},
					},
				},
			}

			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				nil, nil, &diffReportedCond,
				nil, nil, diffedPlacements)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update apply strategy on the resource binding", func() {
			// Update the apply strategy on the resource binding; use an Eventually
			// block to avoid conflict induced errors.
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterResourceBinding.Name}, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to get resource binding: %w", err)
				}

				clusterResourceBinding.Spec.ApplyStrategy = &placementv1beta1.ApplyStrategy{
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
					Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
				}

				if err := k8sClient.Update(ctx, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to update resource binding: %w", err)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to update apply strategy on the resource binding")

			updateRolloutStartedGeneration(&clusterResourceBinding)
		})

		It("should update apply strategy on the work object", func() {
			workName0 := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName)
			workName1 := fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1)
			wantApplyStrategy := &placementv1beta1.ApplyStrategy{
				ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
				WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
				Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
				WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
			}

			applyStrategyUpdatedActual1 := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName0, wantApplyStrategy)
			Eventually(applyStrategyUpdatedActual1, timeout, interval).Should(Succeed(), "Failed to update apply strategy on work object")
			applyStrategyUpdatedActual2 := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName1, wantApplyStrategy)
			Eventually(applyStrategyUpdatedActual2, timeout, interval).Should(Succeed(), "Failed to update apply strategy on work object")
		})

		It("can update work status (all works have completed apply op and become available)", func() {
			// Update the status of the first work object.
			work0 := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName))

			appliedCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeApplied),
				Status:             metav1.ConditionTrue,
				Reason:             "AppliedAllManifests",
				ObservedGeneration: work0.GetGeneration(),
			}
			availableCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeAvailable),
				Status:             metav1.ConditionTrue,
				Reason:             "AllManifestsAreAvailable",
				ObservedGeneration: work0.GetGeneration(),
			}
			// The DiffReported condition added before has become stale.
			refreshWorkStatus(work0, appliedCond, availableCond, nil, nil)

			// Update the status of the second work object.
			work1 := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1))

			appliedCond = &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeApplied),
				Status:             metav1.ConditionTrue,
				Reason:             "AppliedAllManifests",
				ObservedGeneration: work1.GetGeneration(),
			}
			availableCond = &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeAvailable),
				Status:             metav1.ConditionTrue,
				Reason:             "AllManifestsAreAvailable",
				ObservedGeneration: work1.GetGeneration(),
			}
			// The DiffReported condition added before has become stale.
			refreshWorkStatus(work1, appliedCond, availableCond, nil, nil)
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionTrue,
				Reason:             condition.AllWorkAppliedReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}
			availableCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingAvailable),
				Status:             metav1.ConditionTrue,
				Reason:             condition.AllWorkAvailableReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			// Not all work objects have been applied; the diff information left behind
			// on the first Work object should not be populated in the binding status.
			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, &availableCond, nil,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		AfterAll(func() {
			cleanupResources(memberClusterNamespaceName, clusterResourceBinding, masterResourceSnapshot, []*placementv1beta1.ClusterResourceSnapshot{secondaryResourceSnapshot}, memberClusterName)
		})
	})

	Context("Refresh status given different apply strategies (ServerSideApply -> ReportDiff)", Ordered, func() {
		var masterResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
		var secondaryResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
		var clusterResourceBinding *placementv1beta1.ClusterResourceBinding

		now := metav1.Now().Rfc3339Copy()

		BeforeAll(func() {
			memberClusterName = "cluster-" + utils.RandStr()
			memberCluster := clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberCluster)).Should(Succeed(), "Failed to create member cluster")

			memberClusterNamespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
			memberClusterReservedNS := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterNamespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, &memberClusterReservedNS)).Should(Succeed(), "Failed to create namespace")

			testCRPName = "crp-" + utils.RandStr()
		})

		It("can create multiple resource snapshots and resource binding", func() {
			// Create the master resource snapshot.
			masterResourceSnapshot = generateResourceSnapshot(
				1, 2, 0,
				[][]byte{
					testResourceCRD, testNameSpace,
				},
			)
			Expect(k8sClient.Create(ctx, masterResourceSnapshot)).Should(Succeed(), "Failed to create master resource snapshot")

			secondaryResourceSnapshot = generateResourceSnapshot(
				1, 2, 1,
				[][]byte{
					testResource,
				},
			)
			Expect(k8sClient.Create(ctx, secondaryResourceSnapshot)).Should(Succeed(), "Failed to create secondary resource snapshot")

			resourceBindingSpec := placementv1beta1.ResourceBindingSpec{
				State:                placementv1beta1.BindingStateBound,
				ResourceSnapshotName: masterResourceSnapshot.Name,
				TargetCluster:        memberClusterName,
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
					Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
					WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeIfNoDiff,
				},
			}
			createClusterResourceBinding(&clusterResourceBinding, resourceBindingSpec)
		})

		It("can update work status (one work has apply op failures with diffs + successful apply op with drifts; the other work has not been applied yet)", func() {
			work := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName))

			appliedCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeApplied),
				Status:             metav1.ConditionFalse,
				Reason:             "FailedToApplySomeManifests",
				ObservedGeneration: work.GetGeneration(),
			}

			manifestConds := []placementv1beta1.ManifestCondition{
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:  1,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
					DiffDetails: &placementv1beta1.DiffDetails{
						ObservationTime:                   now,
						FirstDiffedObservedTime:           now,
						ObservedInMemberClusterGeneration: ptr.To(int64(1)),
						ObservedDiffs: []placementv1beta1.PatchDetail{
							{
								Path:       "/metadata/labels/foo",
								ValueInHub: "bar",
							},
						},
					},
				},
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:   2,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      "app",
						Namespace: "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
						{
							Type:               placementv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingAvailabilityResultTypeAvailable),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
					DriftDetails: &placementv1beta1.DriftDetails{
						ObservationTime:                   now,
						FirstDriftedObservedTime:          now,
						ObservedInMemberClusterGeneration: 1,
						ObservedDrifts: []placementv1beta1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "1",
								ValueInHub:    "2",
							},
						},
					},
				},
			}
			refreshWorkStatus(work, appliedCond, nil, nil, manifestConds)
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkNotAppliedReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			// Apply op/availability check failure, diff and drift information is not
			// populated as one Work object has not been applied.
			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, nil, nil,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update work status (one work has apply op failures with diffs and drifts; the other work also has apply op failures, but with no diffs/drifts)", func() {
			work := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1))

			appliedCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeApplied),
				Status:             metav1.ConditionFalse,
				Reason:             "FailedToApplySomeManifests",
				ObservedGeneration: work.GetGeneration(),
			}

			manifestConds := []placementv1beta1.ManifestCondition{
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Resource:  "configmaps",
						Name:      "app",
						Namespace: "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToApply),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
				},
			}
			refreshWorkStatus(work, appliedCond, nil, nil, manifestConds)
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkNotAppliedReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			failedPlacements := []placementv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					Condition: metav1.Condition{
						Type:               string(placementv1beta1.WorkConditionTypeApplied),
						Status:             metav1.ConditionFalse,
						Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
						ObservedGeneration: 1,
						Message:            "",
						LastTransitionTime: now,
					},
				},
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "app",
						Namespace: "work",
					},
					Condition: metav1.Condition{
						Type:               string(placementv1beta1.WorkConditionTypeApplied),
						Status:             metav1.ConditionFalse,
						Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToApply),
						ObservedGeneration: 1,
						Message:            "",
						LastTransitionTime: now,
					},
				},
			}
			diffedPlacements := []placementv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []placementv1beta1.PatchDetail{
						{
							Path:       "/metadata/labels/foo",
							ValueInHub: "bar",
						},
					},
				},
			}
			driftedPlacements := []placementv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "app",
						Namespace: "work",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []placementv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInMember: "1",
							ValueInHub:    "2",
						},
					},
				},
			}

			// Apply op/availability check failure, diff, and drift information is
			// now populated as all Work objects have been applied.
			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, nil, nil,
				failedPlacements, driftedPlacements, diffedPlacements)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update work status (one work has apply op failures with diffs and drifts; the other work has become available)", func() {
			work := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1))

			appliedCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeApplied),
				Status:             metav1.ConditionTrue,
				Reason:             "AppliedAllManifests",
				ObservedGeneration: work.GetGeneration(),
			}
			availableCond := &metav1.Condition{
				Type:   string(placementv1beta1.WorkConditionTypeAvailable),
				Status: metav1.ConditionTrue,
				// This is inconsistent with how Fleet (specificall the work applier)
				// actually handles resources; ConfigMap objects are considered to be
				// trackable (available immediately after placement). Here it is set
				// to NotTrackable for testing purposes.
				Reason:             workapplier.WorkNotAllManifestsTrackableReason,
				ObservedGeneration: work.GetGeneration(),
			}

			manifestConds := []placementv1beta1.ManifestCondition{
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Resource:  "configmaps",
						Name:      "app",
						Namespace: "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingApplyResultTypeApplied),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
						{
							Type:   placementv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							// As explained earlier, for this spec the ConfigMap object is
							// considered to be untrackable in availability check.
							Reason:             string(workapplier.ManifestProcessingAvailabilityResultTypeNotTrackable),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
				},
			}
			refreshWorkStatus(work, appliedCond, availableCond, nil, manifestConds)
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkNotAppliedReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			failedPlacements := []placementv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					Condition: metav1.Condition{
						Type:               string(placementv1beta1.WorkConditionTypeApplied),
						Status:             metav1.ConditionFalse,
						Reason:             string(workapplier.ManifestProcessingApplyResultTypeFailedToTakeOver),
						ObservedGeneration: 1,
						Message:            "",
						LastTransitionTime: now,
					},
				},
			}
			diffedPlacements := []placementv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []placementv1beta1.PatchDetail{
						{
							Path:       "/metadata/labels/foo",
							ValueInHub: "bar",
						},
					},
				},
			}
			driftedPlacements := []placementv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "app",
						Namespace: "work",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []placementv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInMember: "1",
							ValueInHub:    "2",
						},
					},
				},
			}

			// Apply op/availability check failure, diff, and drift information is
			// now populated as all Work objects have been applied.
			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, nil, nil,
				failedPlacements, driftedPlacements, diffedPlacements)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update work status (one work has availability check failure; the other work has become available)", func() {
			work := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName))

			appliedCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeApplied),
				Status:             metav1.ConditionTrue,
				Reason:             "AppliedAllManifests",
				ObservedGeneration: work.GetGeneration(),
			}
			availableCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeAvailable),
				Status:             metav1.ConditionFalse,
				Reason:             "NotAllManifestsAreAvailable",
				ObservedGeneration: work.GetGeneration(),
			}

			manifestConds := []placementv1beta1.ManifestCondition{
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:  1,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingApplyResultTypeApplied),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
						{
							Type:               placementv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingAvailabilityResultTypeAvailable),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
				},
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:   2,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      "app",
						Namespace: "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
						{
							Type:               placementv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionFalse,
							Reason:             string(workapplier.ManifestProcessingAvailabilityResultTypeNotYetAvailable),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
					DriftDetails: &placementv1beta1.DriftDetails{
						ObservationTime:                   now,
						FirstDriftedObservedTime:          now,
						ObservedInMemberClusterGeneration: 1,
						ObservedDrifts: []placementv1beta1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "1",
								ValueInHub:    "2",
							},
						},
					},
				},
			}
			refreshWorkStatus(work, appliedCond, availableCond, nil, manifestConds)
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionTrue,
				Reason:             condition.AllWorkAppliedReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}
			availableCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingAvailable),
				Status:             metav1.ConditionFalse,
				Reason:             condition.WorkNotAvailableReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			failedPlacements := []placementv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "app",
						Namespace: "work",
					},
					Condition: metav1.Condition{
						Type:               string(placementv1beta1.WorkConditionTypeAvailable),
						Status:             metav1.ConditionFalse,
						Reason:             string(workapplier.ManifestProcessingAvailabilityResultTypeNotYetAvailable),
						ObservedGeneration: 1,
						Message:            "",
						LastTransitionTime: now,
					},
				},
			}
			driftedPlacements := []placementv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "app",
						Namespace: "work",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []placementv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInMember: "1",
							ValueInHub:    "2",
						},
					},
				},
			}

			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, &availableCond, nil,
				failedPlacements, driftedPlacements, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update work status (both works are available)", func() {
			work := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName))

			appliedCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeApplied),
				Status:             metav1.ConditionTrue,
				Reason:             "AppliedAllManifests",
				ObservedGeneration: work.GetGeneration(),
			}
			availableCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeAvailable),
				Status:             metav1.ConditionTrue,
				Reason:             "AllManifestsAreAvailable",
				ObservedGeneration: work.GetGeneration(),
			}

			manifestConds := []placementv1beta1.ManifestCondition{
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:  1,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingApplyResultTypeApplied),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
						{
							Type:               placementv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingAvailabilityResultTypeAvailable),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
				},
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:   2,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      "app",
						Namespace: "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
						{
							Type:               placementv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingAvailabilityResultTypeAvailable),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
					DriftDetails: &placementv1beta1.DriftDetails{
						ObservationTime:                   now,
						FirstDriftedObservedTime:          now,
						ObservedInMemberClusterGeneration: 1,
						ObservedDrifts: []placementv1beta1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "1",
								ValueInHub:    "2",
							},
						},
					},
				},
			}
			refreshWorkStatus(work, appliedCond, availableCond, nil, manifestConds)
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionTrue,
				Reason:             condition.AllWorkAppliedReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}
			availableCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingAvailable),
				Status:             metav1.ConditionTrue,
				Reason:             condition.WorkNotAvailabilityTrackableReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			driftedPlacements := []placementv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "app",
						Namespace: "work",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []placementv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInMember: "1",
							ValueInHub:    "2",
						},
					},
				},
			}

			// Dirfts are reported even when all Work objects are available.
			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, &availableCond, nil,
				nil, driftedPlacements, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update work status (both works are available, no untrackable resource)", func() {
			work := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1))

			appliedCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeApplied),
				Status:             metav1.ConditionTrue,
				Reason:             "AppliedAllManifests",
				ObservedGeneration: work.GetGeneration(),
			}
			availableCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeAvailable),
				Status:             metav1.ConditionTrue,
				Reason:             "AllManifestsAreAvailable",
				ObservedGeneration: work.GetGeneration(),
			}

			manifestConds := []placementv1beta1.ManifestCondition{
				{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Resource:  "configmaps",
						Name:      "app",
						Namespace: "work",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingApplyResultTypeApplied),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
						{
							Type:               placementv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(workapplier.ManifestProcessingAvailabilityResultTypeAvailable),
							ObservedGeneration: 1,
							Message:            "",
							LastTransitionTime: now,
						},
					},
				},
			}
			refreshWorkStatus(work, appliedCond, availableCond, nil, manifestConds)
		})

		It("should update binding status as expected", func() {
			appliedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionTrue,
				Reason:             condition.AllWorkAppliedReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}
			availableCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingAvailable),
				Status:             metav1.ConditionTrue,
				Reason:             condition.AllWorkAvailableReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			driftedPlacements := []placementv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "app",
						Namespace: "work",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []placementv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInMember: "1",
							ValueInHub:    "2",
						},
					},
				},
			}

			// Dirfts are reported even when all Work objects are available.
			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				&appliedCond, &availableCond, nil,
				nil, driftedPlacements, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		It("can update apply strategy on the resource binding", func() {
			// Update the apply strategy on the resource binding; use an Eventually
			// block to avoid conflict induced errors.
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterResourceBinding.Name}, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to get resource binding: %w", err)
				}

				clusterResourceBinding.Spec.ApplyStrategy = &placementv1beta1.ApplyStrategy{
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
					Type:             placementv1beta1.ApplyStrategyTypeReportDiff,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
				}

				if err := k8sClient.Update(ctx, clusterResourceBinding); err != nil {
					return fmt.Errorf("failed to update resource binding: %w", err)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to update apply strategy on the resource binding")

			updateRolloutStartedGeneration(&clusterResourceBinding)
		})

		It("should update apply strategy on the work object", func() {
			workName0 := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName)
			workName1 := fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1)
			wantApplyStrategy := &placementv1beta1.ApplyStrategy{
				ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
				WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
				Type:             placementv1beta1.ApplyStrategyTypeReportDiff,
				WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
			}

			applyStrategyUpdatedActual1 := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName0, wantApplyStrategy)
			Eventually(applyStrategyUpdatedActual1, timeout, interval).Should(Succeed(), "Failed to update apply strategy on work object")
			applyStrategyUpdatedActual2 := workApplyStrategyUpdatedActual(memberClusterNamespaceName, workName1, wantApplyStrategy)
			Eventually(applyStrategyUpdatedActual2, timeout, interval).Should(Succeed(), "Failed to update apply strategy on work object")
		})

		It("can update work status (all works have reported diff)", func() {
			// Update the status of the first work object.
			work0 := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, testCRPName))

			diffReportedCond := &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeDiffReported),
				Status:             metav1.ConditionTrue,
				Reason:             "DiffReportedOnAllManifests",
				ObservedGeneration: work0.GetGeneration(),
			}
			// The Applied and Available conditions added earlier have become stale.
			refreshWorkStatus(work0, nil, nil, diffReportedCond, nil)

			// Update the status of the second work object.
			work1 := retrieveWork(memberClusterNamespaceName, fmt.Sprintf(placementv1beta1.WorkNameWithSubindexFmt, testCRPName, 1))

			diffReportedCond = &metav1.Condition{
				Type:               string(placementv1beta1.WorkConditionTypeDiffReported),
				Status:             metav1.ConditionTrue,
				Reason:             "DiffReportedOnAllManifests",
				ObservedGeneration: work1.GetGeneration(),
			}
			// The Applied and Available conditions added earlier have become stale.
			refreshWorkStatus(work1, nil, nil, diffReportedCond, nil)
		})

		It("should update binding status as expected", func() {
			diffReportedCond := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingDiffReported),
				Status:             metav1.ConditionTrue,
				Reason:             condition.AllWorkDiffReportedReason,
				ObservedGeneration: clusterResourceBinding.GetGeneration(),
			}

			// Not all work objects have been applied; the diff information left behind
			// on the first Work object should not be populated in the binding status.
			statusUpdatedActual := bindingStatusUpdatedActual(
				clusterResourceBinding,
				false,
				nil, nil, &diffReportedCond,
				nil, nil, nil)
			Eventually(statusUpdatedActual, timeout, interval).Should(Succeed(), "Failed to update binding status")
		})

		AfterAll(func() {
			cleanupResources(memberClusterNamespaceName, clusterResourceBinding, masterResourceSnapshot, []*placementv1beta1.ClusterResourceSnapshot{secondaryResourceSnapshot}, memberClusterName)
		})
	})
})

func verifyBindingStatusSyncedNotApplied(binding *placementv1beta1.ClusterResourceBinding, hasOverride, workSync bool) {
	Eventually(func() string {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
		appliedReason := condition.WorkNotAppliedReason
		if workSync {
			appliedReason = condition.WorkApplyInProcess
		}
		overrideReason := condition.OverrideNotSpecifiedReason
		if hasOverride {
			overrideReason = condition.OverriddenSucceededReason
		}

		wantStatus := placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingOverridden),
					Status:             metav1.ConditionTrue,
					Reason:             overrideReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Status:             metav1.ConditionFalse,
					Type:               string(placementv1beta1.ResourceBindingApplied),
					Reason:             appliedReason,
					ObservedGeneration: binding.Generation,
				},
			},
			FailedPlacements: nil,
		}
		return cmp.Diff(wantStatus, binding.Status, cmpConditionOption)
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
					Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingOverridden),
					Status:             metav1.ConditionTrue,
					Reason:             overrideReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingApplied),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkAppliedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingAvailable),
					Status:             metav1.ConditionFalse,
					Reason:             condition.WorkNotAvailableReason,
					ObservedGeneration: binding.GetGeneration(),
				},
			},
			FailedPlacements: nil,
		}
		return cmp.Diff(wantStatus, binding.Status, cmpConditionOption)
	}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
}

func verifyBindStatusAvail(binding *placementv1beta1.ClusterResourceBinding, hasOverride, hasDriftPlacements bool) {
	Eventually(func() string {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
		overrideReason := condition.OverrideNotSpecifiedReason
		if hasOverride {
			overrideReason = condition.OverriddenSucceededReason
		}
		wantStatus := placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingOverridden),
					Status:             metav1.ConditionTrue,
					Reason:             overrideReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingApplied),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkAppliedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingAvailable),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkAvailableReason,
					ObservedGeneration: binding.GetGeneration(),
				},
			},
			FailedPlacements: nil,
			DiffedPlacements: nil,
		}
		if hasDriftPlacements {
			wantStatus.DriftedPlacements = []placementv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					ObservationTime:                 metav1.Time{Time: specificTime},
					FirstDriftedObservedTime:        metav1.Time{Time: specificTime},
					TargetClusterObservedGeneration: 2,
					ObservedDrifts: []placementv1beta1.PatchDetail{
						configmapPatchDetail,
					},
				},
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					ObservationTime:                 metav1.Time{Time: specificTime},
					TargetClusterObservedGeneration: 1,
					FirstDriftedObservedTime:        metav1.Time{Time: specificTime},
					ObservedDrifts: []placementv1beta1.PatchDetail{
						servicePatchDetail,
					},
				},
			}
		}
		return cmp.Diff(wantStatus, binding.Status, cmpConditionOption)
	}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got):\n", binding.Name))
}

func verifyBindStatusNotAppliedWithTwoPlacements(binding *placementv1beta1.ClusterResourceBinding, hasOverride, hasFailedPlacements, hasDiffedPlacements, hasDriftedPlacements bool) {
	Eventually(func() string {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
		overrideReason := condition.OverrideNotSpecifiedReason
		if hasOverride {
			overrideReason = condition.OverriddenSucceededReason
		}
		wantStatus := placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingOverridden),
					Status:             metav1.ConditionTrue,
					Reason:             overrideReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingApplied),
					Status:             metav1.ConditionFalse,
					Reason:             condition.WorkNotAppliedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
			},
		}
		if hasFailedPlacements {
			wantStatus.FailedPlacements = []placementv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:    placementv1beta1.WorkConditionTypeAvailable,
						Status:  metav1.ConditionFalse,
						Reason:  fakeFailedAvailableReason,
						Message: fakeFailedAvailableMessage,
					},
				},
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					Condition: metav1.Condition{
						Type:    placementv1beta1.WorkConditionTypeApplied,
						Status:  metav1.ConditionFalse,
						Reason:  fakeFailedAppliedReason,
						Message: fakeFailedAppliedMessage,
					},
				},
			}
		}
		if hasDiffedPlacements {
			wantStatus.DiffedPlacements = []placementv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					ObservationTime:                 metav1.Time{Time: specificTime},
					FirstDiffedObservedTime:         metav1.Time{Time: specificTime},
					TargetClusterObservedGeneration: ptr.To(int64(2)),
					ObservedDiffs: []placementv1beta1.PatchDetail{
						configmapPatchDetail,
					},
				},
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					ObservationTime:                 metav1.Time{Time: specificTime},
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					FirstDiffedObservedTime:         metav1.Time{Time: specificTime},
					ObservedDiffs: []placementv1beta1.PatchDetail{
						servicePatchDetail,
					},
				},
			}
		}
		if hasDriftedPlacements {
			wantStatus.DriftedPlacements = []placementv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					ObservationTime:                 metav1.Time{Time: specificTime},
					FirstDriftedObservedTime:        metav1.Time{Time: specificTime},
					TargetClusterObservedGeneration: 2,
					ObservedDrifts: []placementv1beta1.PatchDetail{
						configmapPatchDetail,
					},
				},
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					ObservationTime:                 metav1.Time{Time: specificTime},
					TargetClusterObservedGeneration: 1,
					FirstDriftedObservedTime:        metav1.Time{Time: specificTime},
					ObservedDrifts: []placementv1beta1.PatchDetail{
						servicePatchDetail,
					},
				},
			}
		}
		return cmp.Diff(wantStatus, binding.Status, cmpConditionOption)
	}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
}

func verifyBindStatusNotAvailableWithTwoPlacements(binding *placementv1beta1.ClusterResourceBinding, hasOverride, hasFailedPlacements, hasDriftedPlacements bool) {
	Eventually(func() string {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
		overrideReason := condition.OverrideNotSpecifiedReason
		if hasOverride {
			overrideReason = condition.OverriddenSucceededReason
		}
		wantStatus := placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingOverridden),
					Status:             metav1.ConditionTrue,
					Reason:             overrideReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingApplied),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkAppliedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingAvailable),
					Status:             metav1.ConditionFalse,
					Reason:             condition.WorkNotAvailableReason,
					ObservedGeneration: binding.GetGeneration(),
				},
			},
		}
		if hasFailedPlacements {
			wantStatus.FailedPlacements = []placementv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:    placementv1beta1.WorkConditionTypeAvailable,
						Status:  metav1.ConditionFalse,
						Reason:  fakeFailedAvailableReason,
						Message: fakeFailedAvailableMessage,
					},
				},
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					Condition: metav1.Condition{
						Type:    placementv1beta1.WorkConditionTypeAvailable,
						Status:  metav1.ConditionFalse,
						Reason:  fakeFailedAvailableReason,
						Message: fakeFailedAvailableMessage,
					},
				},
			}
		}
		if hasDriftedPlacements {
			wantStatus.DriftedPlacements = []placementv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					ObservationTime:                 metav1.Time{Time: specificTime},
					FirstDriftedObservedTime:        metav1.Time{Time: specificTime},
					TargetClusterObservedGeneration: 2,
					ObservedDrifts: []placementv1beta1.PatchDetail{
						configmapPatchDetail,
					},
				},
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					ObservationTime:                 metav1.Time{Time: specificTime},
					TargetClusterObservedGeneration: 1,
					FirstDriftedObservedTime:        metav1.Time{Time: specificTime},
					ObservedDrifts: []placementv1beta1.PatchDetail{
						servicePatchDetail,
					},
				},
			}
		}
		return cmp.Diff(wantStatus, binding.Status, cmpConditionOption)
	}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
}

func verifyBindStatusNotAvailableWithOnePlacement(binding *placementv1beta1.ClusterResourceBinding, hasOverride, hasFailedPlacement, hasDriftedPlacement bool) {
	Eventually(func() string {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
		overrideReason := condition.OverrideNotSpecifiedReason
		if hasOverride {
			overrideReason = condition.OverriddenSucceededReason
		}
		wantStatus := placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingOverridden),
					Status:             metav1.ConditionTrue,
					Reason:             overrideReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingApplied),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkAppliedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingAvailable),
					Status:             metav1.ConditionFalse,
					Reason:             condition.WorkNotAvailableReason,
					ObservedGeneration: binding.GetGeneration(),
				},
			},
		}
		if hasFailedPlacement {
			wantStatus.FailedPlacements = []placementv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:    placementv1beta1.WorkConditionTypeAvailable,
						Status:  metav1.ConditionFalse,
						Reason:  fakeFailedAvailableReason,
						Message: fakeFailedAvailableMessage,
					},
				},
			}
		}

		if hasDriftedPlacement {
			wantStatus.DriftedPlacements = []placementv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					ObservationTime:                 metav1.Time{Time: specificTime},
					FirstDriftedObservedTime:        metav1.Time{Time: specificTime},
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []placementv1beta1.PatchDetail{
						servicePatchDetail,
					},
				},
			}
		}
		return cmp.Diff(wantStatus, binding.Status, cmpConditionOption)
	}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
}

func verifyBindStatusAvailableWithOnePlacement(binding *placementv1beta1.ClusterResourceBinding, hasOverride bool) {
	Eventually(func() string {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
		overrideReason := condition.OverrideNotSpecifiedReason
		if hasOverride {
			overrideReason = condition.OverriddenSucceededReason
		}
		wantStatus := placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingOverridden),
					Status:             metav1.ConditionTrue,
					Reason:             overrideReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingApplied),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkAppliedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingAvailable),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkAvailableReason,
					ObservedGeneration: binding.GetGeneration(),
				},
			},
			DriftedPlacements: []placementv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: placementv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					ObservationTime:                 metav1.Time{Time: specificTime},
					FirstDriftedObservedTime:        metav1.Time{Time: specificTime},
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []placementv1beta1.PatchDetail{
						servicePatchDetail,
					},
				},
			},
		}
		return cmp.Diff(wantStatus, binding.Status, cmpConditionOption)
	}, timeout, interval).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name))
}

// TO-DO (chenyu1): consider refactoring the related code to reduce duplication.
func workApplyStrategyUpdatedActual(workNamespace, workName string, wantApplyStrategy *placementv1beta1.ApplyStrategy) func() error {
	return func() error {
		work := &placementv1beta1.Work{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: workNamespace, Name: workName}, work); err != nil {
			return fmt.Errorf("failed to retrieve work %s: %w", workName, err)
		}

		gotApplyStrategy := work.Spec.ApplyStrategy
		if diff := cmp.Diff(gotApplyStrategy, wantApplyStrategy); diff != "" {
			return fmt.Errorf("work apply strategy mismatches (-got, +want):\n%s", diff)
		}
		return nil
	}
}

// TO-DO (chenyu1): consider refactoring the related code to reduce duplication.
func bindingStatusUpdatedActual(
	binding *placementv1beta1.ClusterResourceBinding,
	hasOverrides bool,
	appliedCond, availableCond, diffReportedCond *metav1.Condition,
	failedPlacements []placementv1beta1.FailedResourcePlacement,
	driftedPlacements []placementv1beta1.DriftedResourcePlacement,
	diffedPlacements []placementv1beta1.DiffedResourcePlacement,
) func() error {
	return func() error {
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding); err != nil {
			return fmt.Errorf("failed to retrieve binding %s: %w", binding.Name, err)
		}

		wantBindingStatus := placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
					Status:             metav1.ConditionTrue,
					Reason:             condition.RolloutStartedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingOverridden),
					Status:             metav1.ConditionTrue,
					Reason:             condition.OverrideNotSpecifiedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
				{
					Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
					Status:             metav1.ConditionTrue,
					Reason:             condition.AllWorkSyncedReason,
					ObservedGeneration: binding.GetGeneration(),
				},
			},
			FailedPlacements:  failedPlacements,
			DiffedPlacements:  diffedPlacements,
			DriftedPlacements: driftedPlacements,
		}
		if hasOverrides {
			meta.SetStatusCondition(&wantBindingStatus.Conditions, metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingOverridden),
				Status:             metav1.ConditionTrue,
				Reason:             condition.OverriddenSucceededReason,
				ObservedGeneration: binding.GetGeneration(),
			})
		}
		if appliedCond != nil {
			meta.SetStatusCondition(&wantBindingStatus.Conditions, *appliedCond)
		}
		if availableCond != nil {
			meta.SetStatusCondition(&wantBindingStatus.Conditions, *availableCond)
		}
		if diffReportedCond != nil {
			meta.SetStatusCondition(&wantBindingStatus.Conditions, *diffReportedCond)
		}

		if diff := cmp.Diff(binding.Status, wantBindingStatus, bindingStatusCmpOpts); diff != "" {
			return fmt.Errorf("binding status mismatches (-got, +want):\n%s", diff)
		}
		return nil
	}
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
		Type:               placementv1beta1.WorkConditionTypeApplied,
		Reason:             "appliedManifest",
		Message:            "fake apply manifest",
		ObservedGeneration: work.Generation,
		LastTransitionTime: metav1.Now(),
	})
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

func markWorkWithNoDrift(work *placementv1beta1.Work) {
	work.Status.ManifestConditions = []placementv1beta1.ManifestCondition{
		{
			Identifier: placementv1beta1.WorkResourceIdentifier{
				Ordinal:   0,
				Group:     "",
				Version:   "v1",
				Kind:      "ConfigMap",
				Name:      "config-name",
				Namespace: "config-namespace",
			},
			Conditions: []metav1.Condition{
				{
					Type:               placementv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             "fakeAppliedManifest",
					Message:            "fake apply manifest",
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               placementv1beta1.WorkConditionTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             "fakeAvailableManifest",
					Message:            "fake available manifest",
					LastTransitionTime: metav1.Now(),
				},
			},
			DriftDetails: nil,
		},
		{
			Identifier: placementv1beta1.WorkResourceIdentifier{
				Ordinal:   1,
				Group:     "",
				Version:   "v1",
				Kind:      "Service",
				Name:      "svc-name",
				Namespace: "svc-namespace",
			},
			Conditions: []metav1.Condition{
				{
					Type:               placementv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             "fakeAppliedManifest",
					Message:            "fake apply manifest",
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               placementv1beta1.WorkConditionTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             "fakeAvailableManifest",
					Message:            "fake available manifest",
					LastTransitionTime: metav1.Now(),
				},
			},
			DriftDetails: nil,
		},
	}
	Expect(k8sClient.Status().Update(ctx, work)).Should(Succeed())
	By(fmt.Sprintf("resource work `%s` is marked with no drift", work.Name))
}

// TO-DO (chenyu1): consider refactoring related code to reduce duplication.
func retrieveWork(namespace, name string) *placementv1beta1.Work {
	work := &placementv1beta1.Work{}
	Eventually(func() error {
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, work); err != nil {
			return fmt.Errorf("failed to retrieve work %s: %w", name, err)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "Failed to retrieve work")

	return work
}

// TO-DO (chenyu1): consider refactoring related code to reduce duplication.
func refreshWorkStatus(
	work *placementv1beta1.Work,
	appliedCond, availableCond, diffReportedCond *metav1.Condition,
	manifestConds []placementv1beta1.ManifestCondition,
) {
	Eventually(func() error {
		namespacedName := types.NamespacedName{Name: work.Name, Namespace: work.Namespace}
		if err := k8sClient.Get(ctx, namespacedName, work); err != nil {
			return fmt.Errorf("failed to retrieve work %s: %w", work.Name, err)
		}

		if appliedCond != nil {
			meta.SetStatusCondition(&work.Status.Conditions, *appliedCond)
		}
		if availableCond != nil {
			meta.SetStatusCondition(&work.Status.Conditions, *availableCond)
		}
		if diffReportedCond != nil {
			meta.SetStatusCondition(&work.Status.Conditions, *diffReportedCond)
		}
		work.Status.ManifestConditions = manifestConds
		if err := k8sClient.Status().Update(ctx, work); err != nil {
			return fmt.Errorf("failed to update work status: %w", err)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "Failed to update work status")
}

// TO-DO (chenyu1): consider refactoring related code to reduce duplication.
func cleanupResources(
	memberClusterNamespaceName string,
	clusterResourceBinding *placementv1beta1.ClusterResourceBinding,
	masterResourceSnapshot *placementv1beta1.ClusterResourceSnapshot,
	additionalResourceSnapshots []*placementv1beta1.ClusterResourceSnapshot,
	memberClusterName string,
) {
	// Note that the cleanup logic here will not check if the process has been
	// fully completed. This is OK as the suite is never run in parallel
	// and a random suffix is used for each created resource.

	if len(memberClusterNamespaceName) > 0 {
		memberClusterNamespace := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberClusterNamespaceName,
			},
		}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &memberClusterNamespace))).Should(Succeed(), "Failed to delete namespace")
	}

	if clusterResourceBinding != nil {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, clusterResourceBinding))).Should(Succeed(), "Failed to delete cluster resource binding")
	}

	if masterResourceSnapshot != nil {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, masterResourceSnapshot))).Should(Succeed(), "Failed to delete master resource snapshot")
	}

	for idx := range additionalResourceSnapshots {
		snapshot := additionalResourceSnapshots[idx]
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, snapshot))).Should(Succeed(), "Failed to delete additional resource snapshot")
	}

	if len(memberClusterName) > 0 {
		memberCluster := clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberClusterName,
			},
		}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &memberCluster))).Should(Succeed(), "Failed to delete member cluster")
	}
}

// markWorkWithFailedToApplyAndNotAvailable marks the work as not applied with failedPlacement
func markWorkWithFailedToApplyAndNotAvailable(work *placementv1beta1.Work, hasDiffedDetails, hasDriftedDetails bool) {
	meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{
		Status:             metav1.ConditionFalse,
		Type:               placementv1beta1.WorkConditionTypeApplied,
		Reason:             "failedToApplyManifest",
		Message:            "fake available manifest",
		ObservedGeneration: work.Generation,
		LastTransitionTime: metav1.Now(),
	})
	meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{
		Status:             metav1.ConditionFalse,
		Type:               placementv1beta1.WorkConditionTypeAvailable,
		Reason:             "availableManifest",
		Message:            "fake available manifest",
		ObservedGeneration: work.Generation,
		LastTransitionTime: metav1.Now(),
	})
	work.Status.ManifestConditions = []placementv1beta1.ManifestCondition{
		{
			Identifier: placementv1beta1.WorkResourceIdentifier{
				Ordinal:   0,
				Group:     "",
				Version:   "v1",
				Kind:      "ConfigMap",
				Name:      "config-name",
				Namespace: "config-namespace",
			},
			Conditions: []metav1.Condition{
				{
					Type:               placementv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionFalse,
					Reason:             fakeFailedAppliedReason,
					Message:            fakeFailedAppliedMessage,
					LastTransitionTime: metav1.Now(),
				},
			},
		},
		{
			Identifier: placementv1beta1.WorkResourceIdentifier{
				Ordinal:   1,
				Group:     "",
				Version:   "v1",
				Kind:      "Service",
				Name:      "svc-name",
				Namespace: "svc-namespace",
			},
			Conditions: []metav1.Condition{
				{
					Type:               placementv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             "fakeAppliedManifest",
					Message:            "fake apply manifest",
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               placementv1beta1.WorkConditionTypeAvailable,
					Status:             metav1.ConditionFalse,
					Reason:             fakeFailedAvailableReason,
					Message:            fakeFailedAvailableMessage,
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}
	if hasDiffedDetails {
		work.Status.ManifestConditions[0].DiffDetails = &placementv1beta1.DiffDetails{
			ObservationTime:                   metav1.Time{Time: specificTime},
			FirstDiffedObservedTime:           metav1.Time{Time: specificTime},
			ObservedInMemberClusterGeneration: ptr.To(int64(2)),
			ObservedDiffs: []placementv1beta1.PatchDetail{
				configmapPatchDetail,
			},
		}
		work.Status.ManifestConditions[1].DiffDetails = &placementv1beta1.DiffDetails{
			ObservationTime:                   metav1.Time{Time: specificTime},
			FirstDiffedObservedTime:           metav1.Time{Time: specificTime},
			ObservedInMemberClusterGeneration: ptr.To(int64(1)),
			ObservedDiffs: []placementv1beta1.PatchDetail{
				servicePatchDetail,
			},
		}
	}

	if hasDriftedDetails {
		work.Status.ManifestConditions[0].DriftDetails = &placementv1beta1.DriftDetails{
			ObservationTime:                   metav1.Time{Time: specificTime},
			FirstDriftedObservedTime:          metav1.Time{Time: specificTime},
			ObservedInMemberClusterGeneration: 2,
			ObservedDrifts: []placementv1beta1.PatchDetail{
				configmapPatchDetail,
			},
		}
		work.Status.ManifestConditions[1].DriftDetails = &placementv1beta1.DriftDetails{
			ObservationTime:                   metav1.Time{Time: specificTime},
			FirstDriftedObservedTime:          metav1.Time{Time: specificTime},
			ObservedInMemberClusterGeneration: 1,
			ObservedDrifts: []placementv1beta1.PatchDetail{
				servicePatchDetail,
			},
		}
	}
	Expect(k8sClient.Status().Update(ctx, work)).Should(Succeed())
	By(fmt.Sprintf("resource work `%s` is marked as available", work.Name))
}

// markWorkAsAppliedButNotAvailableWithFailedManifest marks the work as not available with failedPlacement
func markWorkAsAppliedButNotAvailableWithFailedManifest(work *placementv1beta1.Work, hasDriftedDetails bool) {
	meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               placementv1beta1.WorkConditionTypeApplied,
		Reason:             "appliedManifest",
		Message:            "fake apply manifest",
		ObservedGeneration: work.Generation,
		LastTransitionTime: metav1.Now(),
	})
	work.Status.ManifestConditions = []placementv1beta1.ManifestCondition{
		{
			Identifier: placementv1beta1.WorkResourceIdentifier{
				Ordinal:   0,
				Group:     "",
				Version:   "v1",
				Kind:      "ConfigMap",
				Name:      "config-name",
				Namespace: "config-namespace",
			},
			Conditions: []metav1.Condition{
				{
					Type:               placementv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             "fakeAppliedManifest",
					Message:            "fake apply manifest",
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               placementv1beta1.WorkConditionTypeAvailable,
					Status:             metav1.ConditionFalse,
					Reason:             fakeFailedAvailableReason,
					Message:            fakeFailedAvailableMessage,
					LastTransitionTime: metav1.Now(),
				},
			},
		},
		{
			Identifier: placementv1beta1.WorkResourceIdentifier{
				Ordinal:   1,
				Group:     "",
				Version:   "v1",
				Kind:      "Service",
				Name:      "svc-name",
				Namespace: "svc-namespace",
			},
			Conditions: []metav1.Condition{
				{
					Type:               placementv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             "fakeAppliedManifest",
					Message:            "fake apply manifest",
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               placementv1beta1.WorkConditionTypeAvailable,
					Status:             metav1.ConditionFalse,
					Reason:             fakeFailedAvailableReason,
					Message:            fakeFailedAvailableMessage,
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}
	if hasDriftedDetails {
		work.Status.ManifestConditions[0].DriftDetails = &placementv1beta1.DriftDetails{
			ObservationTime:                   metav1.Time{Time: specificTime},
			FirstDriftedObservedTime:          metav1.Time{Time: specificTime},
			ObservedInMemberClusterGeneration: 2,
			ObservedDrifts: []placementv1beta1.PatchDetail{
				configmapPatchDetail,
			},
		}
		work.Status.ManifestConditions[1].DriftDetails = &placementv1beta1.DriftDetails{
			ObservationTime:                   metav1.Time{Time: specificTime},
			FirstDriftedObservedTime:          metav1.Time{Time: specificTime},
			ObservedInMemberClusterGeneration: 1,
			ObservedDrifts: []placementv1beta1.PatchDetail{
				servicePatchDetail,
			},
		}
	}
	Expect(k8sClient.Status().Update(ctx, work)).Should(Succeed())
	By(fmt.Sprintf("resource work `%s` is marked as available", work.Name))
}

func markOneManifestAvailable(work *placementv1beta1.Work, hasDriftedManifest bool) {
	work.Status.ManifestConditions = []placementv1beta1.ManifestCondition{
		{
			Identifier: placementv1beta1.WorkResourceIdentifier{
				Ordinal:   0,
				Group:     "",
				Version:   "v1",
				Kind:      "ConfigMap",
				Name:      "config-name",
				Namespace: "config-namespace",
			},
			Conditions: []metav1.Condition{
				{
					Type:               placementv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             "fakeAppliedManifest",
					Message:            "fake apply manifest",
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               placementv1beta1.WorkConditionTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             "fakeAvailableManifest",
					Message:            "fake available manifest",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
		{
			Identifier: placementv1beta1.WorkResourceIdentifier{
				Ordinal:   1,
				Group:     "",
				Version:   "v1",
				Kind:      "Service",
				Name:      "svc-name",
				Namespace: "svc-namespace",
			},
			Conditions: []metav1.Condition{
				{
					Type:               placementv1beta1.WorkConditionTypeApplied,
					Status:             metav1.ConditionTrue,
					Reason:             "fakeAppliedManifest",
					Message:            "fake apply manifest",
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               placementv1beta1.WorkConditionTypeAvailable,
					Status:             metav1.ConditionFalse,
					Reason:             fakeFailedAvailableReason,
					Message:            fakeFailedAvailableMessage,
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}
	if hasDriftedManifest {
		work.Status.ManifestConditions[1].DriftDetails = &placementv1beta1.DriftDetails{
			ObservationTime:                   metav1.Time{Time: specificTime},
			FirstDriftedObservedTime:          metav1.Time{Time: specificTime},
			ObservedInMemberClusterGeneration: 1,
			ObservedDrifts: []placementv1beta1.PatchDetail{
				servicePatchDetail,
			},
		}
	}
	Expect(k8sClient.Status().Update(ctx, work)).Should(Succeed())
	By(fmt.Sprintf("resource work `%s` is marked as available", work.Name))
}

func checkRolloutStartedNotUpdated(rolloutCond *metav1.Condition, binding *placementv1beta1.ClusterResourceBinding) {
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
	diff := cmp.Diff(rolloutCond, binding.GetCondition(string(placementv1beta1.ResourceBindingRolloutStarted)), cmpConditionOptionWithLTT)
	Expect(diff).Should(BeEmpty(), fmt.Sprintf("binding(%s) mismatch (-want +got)", binding.Name), diff)
}

func createClusterResourceBinding(binding **placementv1beta1.ClusterResourceBinding, spec placementv1beta1.ResourceBindingSpec) {
	*binding = generateClusterResourceBinding(spec)
	Expect(k8sClient.Create(ctx, *binding)).Should(Succeed())
	Eventually(func() error {
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: (*binding).Name}, *binding); err != nil {
			return err
		}
		(*binding).Status.Conditions = []metav1.Condition{
			{
				Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
				Status:             metav1.ConditionTrue,
				Reason:             condition.RolloutStartedReason,
				ObservedGeneration: (*binding).GetGeneration(),
				LastTransitionTime: metav1.Now(),
			},
		}
		return k8sClient.Status().Update(ctx, *binding)
	}, timeout, interval).Should(Succeed(), "Failed to update the binding with RolloutStarted condition")
	By(fmt.Sprintf("resource binding  %s created", (*binding).Name))
}

func updateRolloutStartedGeneration(binding **placementv1beta1.ClusterResourceBinding) {
	Eventually(func() error {
		// Eventually update the binding with the new generation for RolloutStarted condition in case it runs into a conflict error
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: (*binding).Name}, *binding); err != nil {
			return err
		}
		// RolloutStarted condition has to be updated to reflect the new generation
		for i := range (*binding).Status.Conditions {
			if (*binding).Status.Conditions[i].Type == string(placementv1beta1.ResourceBindingRolloutStarted) {
				(*binding).Status.Conditions[0].ObservedGeneration = (*binding).GetGeneration()
			}
		}
		return k8sClient.Status().Update(ctx, *binding)
	}, timeout, interval).Should(Succeed(), "Failed to update the binding with new generation for RolloutStarted condition")
}
