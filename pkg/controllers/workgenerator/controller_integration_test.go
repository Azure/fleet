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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

var (
	memberClusterName string
	namespaceName     string
	testCRPName       string
)

var _ = Describe("Test clusterSchedulingPolicySnapshot Controller", func() {
	const (
		timeout  = time.Second * 10
		duration = time.Second * 20
		interval = time.Millisecond * 250
	)

	Context("Test Bound ClusterResourceBinding", func() {
		var binding *fleetv1beta1.ClusterResourceBinding
		ignoreOption := cmpopts.IgnoreFields(metav1.ObjectMeta{},
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
			Expect(k8sClient.Delete(ctx, binding)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		})

		It("Should wait after all the resource snapshot are created", func() {
			// create master resource snapshot with 2 number of resources
			masterSnapshot := generateResourceSnapshot(1, 2, 0, [][]byte{
				testClonesetCRD, testNameSpace, testCloneset,
			})
			Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
			By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
			// create binding
			binding = generateClusterResourceBinding(fleetv1beta1.BindingStateBound, masterSnapshot.Name, memberClusterName)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			// check the work is not created since we have more resource snapshot to create
			work := v1alpha1.Work{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				return apierrors.IsNotFound(err)
			}, duration, interval).Should(BeTrue(), "controller should not create work in hub cluster until all resources are created")
		})

		It("Should not handle the binding with state scheduled", func() {
			// create master resource snapshot with 1 number of resources
			masterSnapshot := generateResourceSnapshot(1, 1, 0, [][]byte{
				testClonesetCRD, testNameSpace, testCloneset,
			})
			Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
			By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
			// create a scheduled binding
			binding = generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, memberClusterName)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			// check the work is not created since the binding state is not bound
			work := v1alpha1.Work{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				return apierrors.IsNotFound(err)
			}, duration, interval).Should(BeTrue(), "controller should not create work in hub cluster until all resources are created")
			// binding should not have any finalizers
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
			Expect(len(binding.Finalizers)).Should(Equal(0))
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
				// check the work is created
				work := v1alpha1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("work %s is created in %s", work.Name, work.Namespace))
				//inspect the work
				wantWork := v1alpha1.Work{
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
							fleetv1beta1.CRPTrackingLabel:   testCRPName,
							fleetv1beta1.ParentBindingLabel: binding.Name,
						},
					},
					Spec: v1alpha1.WorkSpec{
						Workload: v1alpha1.WorkloadTemplate{
							Manifests: []v1alpha1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
								{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
								{RawExtension: runtime.RawExtension{Raw: testCloneset}},
							},
						},
					},
				}
				diff := cmp.Diff(wantWork, work, ignoreOption)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work(%s) mismatch (-want +got):\n%s", work.Name, diff))
			})

			It("Should treat the unscheduled binding as bound", func() {
				// check the work is created
				work := v1alpha1.Work{}
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
				expectedManifest := []v1alpha1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testCloneset}},
				}
				diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff))
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
				work := v1alpha1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is created in %s", work.Name, work.Namespace))
				//inspect the work manifest
				expectedManifest := []v1alpha1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: testClonesetCRD}},
					{RawExtension: runtime.RawExtension{Raw: testNameSpace}},
					{RawExtension: runtime.RawExtension{Raw: testCloneset}},
				}
				diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work manifest(%s) mismatch (-want +got):\n%s", work.Name, diff))
				// check the work for the secondary resource snapshot is created, it's name is crp-subindex
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("second work %s is created in %s", work.Name, work.Namespace))
				//inspect the work
				wantWork := v1alpha1.Work{
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
							fleetv1beta1.CRPTrackingLabel:   testCRPName,
							fleetv1beta1.ParentBindingLabel: binding.Name,
						},
					},
					Spec: v1alpha1.WorkSpec{
						Workload: v1alpha1.WorkloadTemplate{
							Manifests: []v1alpha1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: testConfigMap}},
								{RawExtension: runtime.RawExtension{Raw: testPdb}},
							},
						},
					},
				}
				diff = cmp.Diff(wantWork, work, ignoreOption)
				Expect(diff).Should(BeEmpty(), fmt.Sprintf("work(%s) mismatch (-want +got):\n%s", work.Name, diff))
			})

			It("Should update existing work and create more work in the target namespace when resource snapshots change", func() {
				// check the work for the master resource snapshot is created
				work := v1alpha1.Work{}
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
				expectedManifest := []v1alpha1.Manifest{
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
				expectedManifest = []v1alpha1.Manifest{
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
				expectedManifest = []v1alpha1.Manifest{
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
				work := v1alpha1.Work{}
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
				expectedManifest := []v1alpha1.Manifest{
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
				work := v1alpha1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, testCRPName), Namespace: namespaceName}, &work)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("first work %s is created in %s", work.Name, work.Namespace))
				// check the work for the secondary resource snapshot is created, it's name is crp-subindex
				work2 := v1alpha1.Work{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, testCRPName, 1), Namespace: namespaceName}, &work2)
				}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
				By(fmt.Sprintf("second work %s is created in %s", work2.Name, work2.Namespace))
				// delete the binding
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, binding)).Should(Succeed())
				By(fmt.Sprintf("resource binding  %s is deleted", binding.Name))
				// Check the binding is not removed
				Consistently(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)
				}, duration, interval).Should(Succeed(), "the binding should not be removed until all the work is removed")
				// delete the work (as the testEnv doesn't run GC controller)
				Expect(k8sClient.Delete(ctx, &work)).Should(Succeed())
				By(fmt.Sprintf("work  %s is deleted", work.Name))
				// Check the binding is not still removed
				Consistently(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)
				}, duration, interval).Should(Succeed(), "the binding should not be removed until all the work is removed")
				// delete the work2 (as the testEnv doesn't run GC controller)
				Expect(k8sClient.Delete(ctx, &work2)).Should(Succeed())
				By(fmt.Sprintf("work2  %s is deleted", work2.Name))
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
