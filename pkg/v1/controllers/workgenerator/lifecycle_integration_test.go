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

package workgenerator

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var _ = Describe("deleting placement bindings", func() {
	for _, sc := range scopes {
		Context(fmt.Sprintf("delete a binding with a single placement resource snapshot (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)
			// A binding on another member cluster, which should not be affected.
			other := newBindingFixture(sc, memberCluster2Name)

			BeforeAll(func() {
				for _, fixture := range []*bindingFixture{f, other} {
					fixture.createSnapshots(0, 1)
					fixture.createBinding(0)
				}
			})

			f.registerBaselineSpecs(0)

			It("should generate the work objects and sync the status of the other placement binding", func() {
				other.syncWorks(0)
			})

			It("can delete the placement binding", func() {
				removePlacementBinding(sc.bindingNS, f.bindingName)
			})

			It("should remove the work object and the placement binding", func() {
				Eventually(workRemovedActual(f.workNS, f.workName(0)), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")
				Eventually(placementBindingRemovedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")
			})

			It("should not affect the other placement binding", func() {
				Consistently(other.worksMatchActual(0, nil), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The work objects of the other placement binding have changed")
				Consistently(other.syncedStatusActual(0), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The status of the other placement binding has changed")
			})

			AfterAll(func() {
				f.teardown()
				other.teardown()
			})
		})

		Context(fmt.Sprintf("delete a binding with multiple placement resource snapshots (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)

			BeforeAll(func() {
				f.createSnapshots(0, 2)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can delete the placement binding", func() {
				removePlacementBinding(sc.bindingNS, f.bindingName)
			})

			// The secondary work object is owned by the primary one, and is left for K8s' garbage collection.
			It("should remove the primary work object and the placement binding", func() {
				Eventually(workRemovedActual(f.workNS, f.workName(0)), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")
				Eventually(placementBindingRemovedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")
			})

			AfterAll(func() {
				f.teardown()
			})
		})
	}
})

var _ = Describe("suspending placement bindings", func() {
	for _, sc := range scopes {
		Context(fmt.Sprintf("suspend and resume a binding (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can suspend the placement binding", func() {
				updatePlacementBindingSuspended(sc.bindingNS, f.bindingName, true)
			})

			It("should remove the work object", func() {
				Eventually(workRemovedActual(f.workNS, f.workName(0)), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")
			})

			It("should unset the last processed placement resource snapshot name", func() {
				unsetActual := placementBindingLastProcessedSnapshotUnsetActual(sc.bindingNS, f.bindingName)
				Eventually(unsetActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to unset the last processed placement resource snapshot name")
			})

			It("should remove the cleanup finalizer from the placement binding", func() {
				finalizerRemovedActual := placementBindingFinalizerRemovedActual(sc.bindingNS, f.bindingName)
				Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
			})

			It("can roll out a new placement resource snapshot while the placement binding is suspended", func() {
				set := f.createSnapshots(1, 1)
				updatePlacementBindingResourceSnapshot(sc.bindingNS, f.bindingName, set.primaryName())
			})

			It("should not generate any work object while the placement binding is suspended", func() {
				Consistently(f.noWorksActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Work objects have been generated for a suspended placement binding")
				finalizerRemovedActual := placementBindingFinalizerRemovedActual(sc.bindingNS, f.bindingName)
				Consistently(finalizerRemovedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The cleanup finalizer has been added back to a suspended placement binding")
			})

			It("can resume the placement binding", func() {
				updatePlacementBindingSuspended(sc.bindingNS, f.bindingName, false)
			})

			It("should add cleanup finalizer to the placement binding", func() {
				finalizerAddedActual := placementBindingFinalizerAddedActual(sc.bindingNS, f.bindingName)
				Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
			})

			It("should generate the work object from the new placement resource snapshot", func() {
				Eventually(f.worksMatchActual(1, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work object")
			})

			It("should refresh the placement binding status", func() {
				Eventually(f.waitingStatusActual(1), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			It("can mark the work object as applied and available", func() {
				f.markWorks(1)
			})

			It("should refresh the placement binding status", func() {
				Eventually(f.syncedStatusActual(1), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			AfterAll(func() {
				f.teardown()
			})
		})

		Context(fmt.Sprintf("suspend and resume a binding whose work objects have not been processed (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			It("should generate the work object", func() {
				Eventually(f.worksMatchActual(0, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work object")
			})

			It("should refresh the placement binding status", func() {
				Eventually(f.waitingStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			It("can suspend the placement binding", func() {
				updatePlacementBindingSuspended(sc.bindingNS, f.bindingName, true)
			})

			It("should remove the work object and unset the last processed placement resource snapshot name", func() {
				Eventually(workRemovedActual(f.workNS, f.workName(0)), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")
				unsetActual := placementBindingLastProcessedSnapshotUnsetActual(sc.bindingNS, f.bindingName)
				Eventually(unsetActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to unset the last processed placement resource snapshot name")
			})

			It("can resume the placement binding", func() {
				updatePlacementBindingSuspended(sc.bindingNS, f.bindingName, false)
			})

			It("should re-generate the work object", func() {
				Eventually(f.worksMatchActual(0, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-generate the work object")
			})

			It("should refresh the placement binding status", func() {
				Eventually(f.waitingStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			It("can mark the work object as applied and available", func() {
				f.markWorks(0)
			})

			It("should refresh the placement binding status", func() {
				Eventually(f.syncedStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			AfterAll(func() {
				f.teardown()
			})
		})

		Context(fmt.Sprintf("delete a suspended binding (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can suspend the placement binding", func() {
				updatePlacementBindingSuspended(sc.bindingNS, f.bindingName, true)
			})

			It("should remove the work object and the cleanup finalizer", func() {
				Eventually(workRemovedActual(f.workNS, f.workName(0)), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")
				finalizerRemovedActual := placementBindingFinalizerRemovedActual(sc.bindingNS, f.bindingName)
				Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
			})

			It("can delete the placement binding", func() {
				removePlacementBinding(sc.bindingNS, f.bindingName)
			})

			It("should remove the placement binding without generating any work object", func() {
				Eventually(placementBindingRemovedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")
				Consistently(f.noWorksActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Work objects have been generated for a deleted placement binding")
			})

			AfterAll(func() {
				f.teardown()
			})
		})
	}
})

var _ = Describe("changing sync strategies of placement bindings", func() {
	serverSideApplyStrategy := &placementv1alpha1.SyncStrategy{
		ApplyMethod: placementv1alpha1.ApplyMethodServerSideApply,
		ServerSideApplyOptions: &placementv1alpha1.ServerSideApplyOptions{
			ForceConflicts: true,
		},
	}

	for _, sc := range scopes {
		for _, snapshotCount := range []int{1, 2} {
			Context(fmt.Sprintf("update the sync strategy with %d placement resource snapshot(s) (%s)", snapshotCount, sc.name), Ordered, func() {
				f := newBindingFixture(sc, memberCluster1Name)

				BeforeAll(func() {
					f.createSnapshots(0, snapshotCount)
					f.createBinding(0)
				})

				f.registerBaselineSpecs(0)

				It("can record the UIDs of the work objects", func() {
					f.recordWorkUIDs()
					Expect(f.workUIDs).To(HaveLen(snapshotCount), "Unexpected number of work objects")
				})

				It("can record the placement binding status", func() {
					f.recordStatus()
				})

				It("can set a sync strategy on the placement binding", func() {
					updatePlacementBindingSyncStrategy(sc.bindingNS, f.bindingName, serverSideApplyStrategy)
				})

				It("should update the work objects in place", func() {
					// The sync strategy on the work objects should match the one on the placement binding as written
					// to the API server (which might carry defaulted fields).
					Eventually(func() error {
						binding, err := getPlacementBinding(sc.bindingNS, f.bindingName)
						if err != nil {
							return err
						}
						syncStrategy := binding.GetSpec().SyncStrategy
						if syncStrategy == nil {
							return fmt.Errorf("the sync strategy has not been set on the placement binding")
						}
						return f.worksMatchActual(0, syncStrategy)()
					}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")

					uids, err := f.listWorkUIDs()
					Expect(err).To(Succeed(), "Failed to list the work objects")
					if diff := cmp.Diff(uids, f.workUIDs); diff != "" {
						Fail(fmt.Sprintf("work objects have been re-created (-got +want):\n%s", diff))
					}
				})

				// A sync strategy change alone is applied to the work objects without resetting the placement binding
				// status; the status is refreshed only after the updated work objects have been processed.
				It("should not refresh the placement binding status before the work objects are processed", func() {
					Consistently(f.statusUnchangedActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding status has been refreshed unexpectedly")
				})

				It("can mark the work objects as applied and available", func() {
					f.markWorks(0)
				})

				It("should refresh the placement binding status", func() {
					Eventually(f.syncedStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
				})

				It("can record the placement binding status again", func() {
					f.recordStatus()
				})

				It("can unset the sync strategy on the placement binding", func() {
					updatePlacementBindingSyncStrategy(sc.bindingNS, f.bindingName, nil)
				})

				It("should update the work objects in place", func() {
					Eventually(f.worksMatchActual(0, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")

					uids, err := f.listWorkUIDs()
					Expect(err).To(Succeed(), "Failed to list the work objects")
					if diff := cmp.Diff(uids, f.workUIDs); diff != "" {
						Fail(fmt.Sprintf("work objects have been re-created (-got +want):\n%s", diff))
					}
				})

				It("should not refresh the placement binding status before the work objects are processed", func() {
					Consistently(f.statusUnchangedActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding status has been refreshed unexpectedly")
				})

				It("can mark the work objects as applied and available", func() {
					f.markWorks(0)
				})

				It("should refresh the placement binding status", func() {
					Eventually(f.syncedStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
				})

				AfterAll(func() {
					f.teardown()
				})
			})
		}
	}
})

var _ = Describe("handling work object events", func() {
	for _, sc := range scopes {
		Context(fmt.Sprintf("work objects that the work generator does not own (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)

			foreignWorkNames := []string{
				fmt.Sprintf("foreign-work-%s", utils.RandStr()),
				fmt.Sprintf("foreign-work-%s", utils.RandStr()),
				fmt.Sprintf("foreign-work-%s", utils.RandStr()),
			}

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can create work objects that are not owned by the placement binding", func() {
				ownerNSLabel := map[string]string{placementv1alpha1.WorkOwnerNamespaceLabelKey: sc.bindingNS}

				// A work object without any KubeFleet owner metadata.
				createForeignWork(f.workNS, foreignWorkNames[0], nil, nil)
				// A work object owned by a placement binding that does not exist.
				createForeignWork(f.workNS, foreignWorkNames[1], ownerNSLabel, map[string]string{
					placementv1alpha1.WorkOwnedByPlacementBindingAnnotationKey: fmt.Sprintf("non-existent-binding-%s", utils.RandStr()),
				})
				// A work object that points to the placement binding, but is not labeled as being owned by it.
				createForeignWork(f.workNS, foreignWorkNames[2], ownerNSLabel, map[string]string{
					placementv1alpha1.WorkOwnedByPlacementBindingAnnotationKey: f.bindingName,
				})
			})

			It("can mark the work objects as applied and available", func() {
				for _, workName := range foreignWorkNames {
					markWorkAsAppliedAndAvailable(f.workNS, workName, nil)
				}
			})

			It("should not affect the placement binding", func() {
				Consistently(f.worksMatchActual(0, nil), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The work objects have changed")
				Consistently(f.syncedStatusActual(0), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding status has changed")
			})

			It("can delete the work objects", func() {
				for _, workName := range foreignWorkNames {
					deleteWork(f.workNS, workName)
					Eventually(workRemovedActual(f.workNS, workName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")
				}
			})

			It("should not affect the placement binding", func() {
				Consistently(f.worksMatchActual(0, nil), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The work objects have changed")
				Consistently(f.syncedStatusActual(0), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding status has changed")
			})

			It("can roll out a new placement resource snapshot", func() {
				set := f.createSnapshots(1, 1)
				updatePlacementBindingResourceSnapshot(sc.bindingNS, f.bindingName, set.primaryName())
			})

			It("should update the work object", func() {
				Eventually(f.worksMatchActual(1, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work object")
			})

			It("should refresh the placement binding status", func() {
				Eventually(f.waitingStatusActual(1), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			It("can mark the work object as applied and available", func() {
				f.markWorks(1)
			})

			It("should refresh the placement binding status", func() {
				Eventually(f.syncedStatusActual(1), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			AfterAll(func() {
				f.teardown()
			})
		})

		// The work generator re-creates work objects that have been deleted out of band, without waiting for a rollout.
		for _, tc := range []struct {
			name          string
			snapshotCount int
			// The sub-indices of the work objects to delete.
			deleteSubIdxs []int
		}{
			{name: "the secondary work object is deleted", snapshotCount: 2, deleteSubIdxs: []int{1}},
			{name: "the primary work object is deleted", snapshotCount: 2, deleteSubIdxs: []int{0}},
			{name: "the only work object is deleted", snapshotCount: 1, deleteSubIdxs: []int{0}},
		} {
			Context(fmt.Sprintf("work objects deleted out of band: %s (%s)", tc.name, sc.name), Ordered, func() {
				f := newBindingFixture(sc, memberCluster1Name)

				BeforeAll(func() {
					f.createSnapshots(0, tc.snapshotCount)
					f.createBinding(0)
				})

				f.registerBaselineSpecs(0)

				It("can record the UIDs of the work objects", func() {
					f.recordWorkUIDs()
					Expect(f.workUIDs).To(HaveLen(tc.snapshotCount), "Unexpected number of work objects")
				})

				It("can delete the work objects", func() {
					for _, subIdx := range tc.deleteSubIdxs {
						deleteWork(f.workNS, f.workName(subIdx))
					}
				})

				It("should re-create the deleted work objects", func() {
					Eventually(func() error {
						uids, err := f.listWorkUIDs()
						if err != nil {
							return err
						}
						// The cache might still serve the deleted work objects.
						for _, subIdx := range tc.deleteSubIdxs {
							workName := f.workName(subIdx)
							if uid, found := uids[workName]; found && uid == f.workUIDs[workName] {
								return fmt.Errorf("work object %s has not been re-created", workName)
							}
						}
						return f.worksMatchActual(0, nil)()
					}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-create the deleted work objects")
				})

				It("should leave the other work objects untouched", func() {
					uids, err := f.listWorkUIDs()
					Expect(err).To(Succeed(), "Failed to list the work objects")
					for subIdx := 0; subIdx < tc.snapshotCount; subIdx++ {
						deleted := false
						for _, deletedSubIdx := range tc.deleteSubIdxs {
							deleted = deleted || deletedSubIdx == subIdx
						}
						workName := f.workName(subIdx)
						if !deleted && uids[workName] != f.workUIDs[workName] {
							Fail(fmt.Sprintf("work object %s has been re-created unexpectedly", workName))
						}
					}
				})

				It("should refresh the placement binding status", func() {
					Eventually(f.waitingStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
				})

				It("can mark the re-created work objects as applied and available", func() {
					f.markWorks(0, tc.deleteSubIdxs...)
				})

				It("should refresh the placement binding status", func() {
					Eventually(f.syncedStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
				})

				AfterAll(func() {
					f.teardown()
				})
			})
		}
	}
})

var _ = Describe("binding a placement policy to multiple clusters", func() {
	for _, sc := range scopes {
		Context(fmt.Sprintf("two bindings of the same placement policy (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)
			// Another binding of the same placement policy (sharing the same snapshots), on another member cluster.
			other := f.newFixtureOfSamePolicy(memberCluster2Name)

			BeforeAll(func() {
				f.createSnapshots(0, 2)
				f.createBinding(0)
				other.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("should generate the work objects of the other placement binding, with the same names", func() {
				Eventually(other.worksMatchActual(0, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects of the other placement binding")
				for subIdx := 0; subIdx < 2; subIdx++ {
					Expect(other.workName(subIdx)).To(Equal(f.workName(subIdx)), "The work objects of the two placement bindings should have the same names")
				}
			})

			It("should keep the other placement binding waiting, as its work objects have not been processed", func() {
				Eventually(other.waitingStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the status of the other placement binding")
				Consistently(other.waitingStatusActual(0), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The status of the other placement binding has changed unexpectedly")
			})

			It("can record the UIDs of the work objects and the status of the other placement binding", func() {
				other.recordWorkUIDs()
				other.recordStatus()
			})

			It("can roll out a new snapshot set to the first placement binding only", func() {
				set := f.createSnapshots(1, 2)
				updatePlacementBindingResourceSnapshot(sc.bindingNS, f.bindingName, set.primaryName())
			})

			It("should update the work objects of the first placement binding", func() {
				Eventually(f.worksMatchActual(1, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
			})

			It("should refresh the status of the first placement binding", func() {
				Eventually(f.waitingStatusActual(1), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			It("can mark the work objects of the first placement binding as applied and available", func() {
				f.markWorks(1)
			})

			It("should refresh the status of the first placement binding", func() {
				Eventually(f.syncedStatusActual(1), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			It("should leave the other placement binding untouched", func() {
				Consistently(func() error {
					if err := other.worksMatchActual(0, nil)(); err != nil {
						return err
					}
					return other.resourceVersionUnchangedActual()()
				}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The other placement binding or its work objects have changed")
				other.expectWorksNotRecreated()
			})

			It("can delete the first placement binding", func() {
				removePlacementBinding(sc.bindingNS, f.bindingName)
			})

			It("should remove the primary work object of the first placement binding and the placement binding itself", func() {
				Eventually(workRemovedActual(f.workNS, f.workName(0)), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")
				Eventually(placementBindingRemovedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")
			})

			It("should leave the other placement binding untouched", func() {
				Consistently(func() error {
					if err := other.worksMatchActual(0, nil)(); err != nil {
						return err
					}
					return other.resourceVersionUnchangedActual()()
				}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The other placement binding or its work objects have changed")
				other.expectWorksNotRecreated()
			})

			AfterAll(func() {
				f.teardown()
				other.teardown()
			})
		})
	}
})

var _ = Describe("placement bindings with long and dotted names", func() {
	for _, sc := range scopes {
		Context(fmt.Sprintf("names over the label value length limit, with dots (%s)", sc.name), Ordered, func() {
			// The placement policy name is long enough for the work names to be truncated, while keeping the
			// snapshot names (`[POLICY-NAME]-snapshot-[INDEX]-[SUB-INDEX]`) within the 253-character limit; both names
			// are too long (and dotted) to be used as label values as they are.
			f := newBindingFixtureWithNames(sc, memberCluster1Name, longDottedName("binding", 100), longDottedName("policy", 240))

			BeforeAll(func() {
				f.createSnapshots(0, 2)
				f.createBinding(0)
			})

			// The work objects are listed through the owner binding field index (keyed on the hashed owner label
			// value), and their events are mapped to the placement binding through the owner annotation (which
			// carries the full name); the baseline specs exercise both.
			f.registerBaselineSpecs(0)

			It("should use names long enough to exercise label value hashing and work name truncation", func() {
				Expect(workOwnerLabelValue(f.bindingName)).NotTo(Equal(f.bindingName), "The owner label value of the placement binding should be hashed")
				Expect(workOwnerLabelValue(f.policyName)).NotTo(Equal(f.policyName), "The owner label value of the placement policy should be hashed")
				Expect(f.workName(0)).To(HaveLen(nameLenLimit), "The name of the primary work object should be truncated")
			})

			It("can record the UIDs of the work objects", func() {
				f.recordWorkUIDs()
			})

			It("can roll out a new snapshot set", func() {
				set := f.createSnapshots(1, 2)
				updatePlacementBindingResourceSnapshot(sc.bindingNS, f.bindingName, set.primaryName())
			})

			It("should update the work objects in place", func() {
				Eventually(f.worksMatchActual(1, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
				f.expectWorksNotRecreated()
			})

			It("should refresh the placement binding status", func() {
				Eventually(f.waitingStatusActual(1), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			It("can mark the work objects as applied and available", func() {
				f.markWorks(1)
			})

			It("should refresh the placement binding status", func() {
				Eventually(f.syncedStatusActual(1), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			It("can delete the placement binding", func() {
				removePlacementBinding(sc.bindingNS, f.bindingName)
			})

			It("should remove the primary work object and the placement binding", func() {
				Eventually(workRemovedActual(f.workNS, f.workName(0)), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")
				Eventually(placementBindingRemovedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")
			})

			AfterAll(func() {
				f.teardown()
			})
		})
	}
})

var _ = Describe("re-creating placement bindings", func() {
	for _, sc := range scopes {
		Context(fmt.Sprintf("re-create a binding while the secondary work objects of the previous one linger (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)

			BeforeAll(func() {
				f.createSnapshots(0, 3)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can record the UIDs of the work objects", func() {
				f.recordWorkUIDs()
			})

			It("can delete the placement binding", func() {
				removePlacementBinding(sc.bindingNS, f.bindingName)
			})

			It("should remove the primary work object and the placement binding", func() {
				Eventually(workRemovedActual(f.workNS, f.workName(0)), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")
				Eventually(placementBindingRemovedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")
			})

			// The secondary work objects are owned by the primary work object and are left to K8s' garbage collection,
			// which does not run in the test environment; this simulates a garbage collector that has yet to catch up.
			It("should leave the secondary work objects behind", func() {
				uids, err := f.listWorkUIDs()
				Expect(err).To(Succeed(), "Failed to list the work objects")
				wantUIDs := map[string]types.UID{
					f.workName(1): f.workUIDs[f.workName(1)],
					f.workName(2): f.workUIDs[f.workName(2)],
				}
				Expect(cmp.Diff(uids, wantUIDs)).To(BeEmpty(), "Unexpected work objects (-got +want)")
			})

			It("can re-create the placement binding with a new snapshot set of fewer snapshots", func() {
				f.createSnapshots(1, 2)
				f.createBinding(1)
			})

			It("should add cleanup finalizer to the placement binding", func() {
				Eventually(placementBindingFinalizerAddedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
			})

			It("should adopt the lingering secondary work object still needed, and delete the other one", func() {
				// The work objects should match the new snapshot set exactly, i.e., the secondary work object of
				// sub-index 1 now points to the new primary work object, and the one of sub-index 2 is gone.
				Eventually(f.worksMatchActual(1, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")

				uids, err := f.listWorkUIDs()
				Expect(err).To(Succeed(), "Failed to list the work objects")
				Expect(uids[f.workName(1)]).To(Equal(f.workUIDs[f.workName(1)]), "The lingering secondary work object has been re-created")
			})

			It("should refresh the placement binding status", func() {
				Eventually(f.waitingStatusActual(1), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			It("can mark the work objects as applied and available", func() {
				f.markWorks(1)
			})

			It("should refresh the placement binding status", func() {
				Eventually(f.syncedStatusActual(1), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			AfterAll(func() {
				f.teardown()
			})
		})
	}
})

var _ = Describe("missing reserved namespaces", func() {
	// The handling of reserved namespaces does not depend on the scope of the placement binding; the namespace-scoped
	// one is used.
	sc := scopes[0]

	Context("the reserved namespace of the target cluster does not exist yet", Ordered, func() {
		// A member cluster whose reserved namespace has not been created (namespaces cannot be deleted in the test
		// environment, so a new one is used each time).
		clusterName := fmt.Sprintf("member-%s", utils.RandStr())
		f := newBindingFixture(sc, clusterName)

		BeforeAll(func() {
			f.createSnapshots(0, 1)
			f.createBinding(0)
		})

		It("should add cleanup finalizer to the placement binding", func() {
			Eventually(placementBindingFinalizerAddedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
		})

		It("should not generate any work object or report any status", func() {
			Consistently(func() error {
				if err := f.noWorksActual()(); err != nil {
					return err
				}
				return placementBindingStatusMatchesActual(sc.bindingNS, f.bindingName, &placementv1alpha1.PlacementBindingStatus{})()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Work objects have been generated or the status has been reported unexpectedly")
		})

		It("can create the reserved namespace", func() {
			createReservedNamespace(clusterName)
		})

		// The work generator does not watch namespaces; it picks up the new namespace when it retries the failed
		// reconciliation, whose backoff is capped at a second in this suite (see requeueMaxDelay).
		It("should generate the work objects", func() {
			Eventually(f.worksMatchActual(0, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the placement binding status", func() {
			Eventually(f.waitingStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work objects as applied and available", func() {
			f.markWorks(0)
		})

		It("should refresh the placement binding status", func() {
			Eventually(f.syncedStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		AfterAll(func() {
			f.teardown()
		})
	})
})
