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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

// The work generator does not watch placement resource snapshots; it picks up snapshot changes only when it retries
// a failed reconciliation, whose backoff is capped at a second in this suite (see requeueMaxDelay).
var _ = Describe("handling invalid placement resource snapshots", func() {
	serverSideApplyStrategy := &placementv1alpha1.SyncStrategy{
		ApplyMethod: placementv1alpha1.ApplyMethodServerSideApply,
		ServerSideApplyOptions: &placementv1alpha1.ServerSideApplyOptions{
			ForceConflicts: true,
		},
	}

	for _, sc := range scopes {
		Context(fmt.Sprintf("roll out to a missing snapshot (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)
			var set1 *snapshotSet

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can record the UIDs of the work objects", func() {
				f.recordWorkUIDs()
			})

			It("can roll out a snapshot that does not exist yet", func() {
				set1 = f.newSnapshotSet(1, 1)
				updatePlacementBindingResourceSnapshot(sc.bindingNS, f.bindingName, set1.primaryName())
			})

			It("should mark the placement binding as failed to synchronize due to invalid snapshots", func() {
				set0 := f.sets[0]
				wantStatus := wantPlacementBindingStatusInvalidSnapshots(set0.resourceCount(), set0.primaryName(), f.status)
				Eventually(f.invalidSnapshotsStatusActual(wantStatus, fmt.Sprintf(snapshotNotFoundErrMsgFmt, set1.primaryName())),
					eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the placement binding as failed to synchronize")
				f.recordStatus()
			})

			It("should not write to the placement binding or update the work objects while retrying", func() {
				Consistently(func() error {
					if err := f.worksMatchActual(0, nil)(); err != nil {
						return err
					}
					return f.resourceVersionUnchangedActual()()
				}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding or the work objects have been updated unexpectedly")
			})

			It("can mark the deployment in the placed work object as unavailable", func() {
				markWorkManifestAsUnavailable(f.workNS, f.workName(0), 1)
			})

			It("should recount the resources from the placed work objects", func() {
				set0 := f.sets[0]
				wantStatus := wantPlacementBindingStatusInvalidSnapshots(set0.resourceCount(), set0.primaryName(), f.status)
				wantStatus.AvailableResources = ptr.To(set0.resourceCount() - 1)
				wantStatus.FailedResources = []placementv1alpha1.FailedResource{
					{
						ObjectRef: objectRefFromManifestIdentifier(set0.identifiers[0][1]),
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeAvailable,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsUnavailableReason,
							},
						},
					},
				}
				Eventually(f.invalidSnapshotsStatusActual(wantStatus, fmt.Sprintf(snapshotNotFoundErrMsgFmt, set1.primaryName())),
					eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to recount the resources")
			})

			It("can create the missing snapshot", func() {
				f.createSnapshotsInSet(set1)
			})

			It("should update the work object in place", func() {
				Eventually(f.worksMatchActual(1, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work object")
				f.expectWorksNotRecreated()
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

		for _, tc := range []struct {
			name      string
			strayHash string
		}{
			{name: "with the same contents hash", strayHash: dummyContentsHash},
			{name: "with a different contents hash", strayHash: "stray-contents-hash"},
		} {
			Context(fmt.Sprintf("stray snapshot duplicating the last counted sub-index, %s (%s)", tc.name, sc.name), Ordered, func() {
				f := newBindingFixture(sc, memberCluster1Name)
				var set1 *snapshotSet
				var strayName string

				BeforeAll(func() {
					f.createSnapshots(0, 2)
					f.createBinding(0)
				})

				f.registerBaselineSpecs(0)

				It("can record the UIDs of the work objects", func() {
					f.recordWorkUIDs()
				})

				It("can roll out a snapshot set that does not exist yet", func() {
					set1 = f.newSnapshotSet(1, 2)
					updatePlacementBindingResourceSnapshot(sc.bindingNS, f.bindingName, set1.primaryName())
				})

				It("should mark the placement binding as failed to synchronize as the snapshot is not found", func() {
					set0 := f.sets[0]
					wantStatus := wantPlacementBindingStatusInvalidSnapshots(set0.resourceCount(), set0.primaryName(), f.status)
					Eventually(f.invalidSnapshotsStatusActual(wantStatus, fmt.Sprintf(snapshotNotFoundErrMsgFmt, set1.primaryName())),
						eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the placement binding as failed to synchronize")
				})

				It("can create the snapshot set along with a stray snapshot duplicating its last sub-index", func() {
					// The stray snapshot is created first, so that the work generator never sees the snapshot set
					// without it.
					strayName = f.createStraySnapshot(1, 1, 2, tc.strayHash)
					f.createSnapshotsInSet(set1)
				})

				It("should report the duplicate sub-indices", func() {
					set0 := f.sets[0]
					wantStatus := wantPlacementBindingStatusInvalidSnapshots(set0.resourceCount(), set0.primaryName(), f.status)
					Eventually(f.invalidSnapshotsStatusActual(wantStatus, duplicateSubIdxErrMsg),
						eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the duplicate sub-indices")
					f.recordStatus()
				})

				It("should not write to the placement binding or update the work objects while retrying", func() {
					Consistently(func() error {
						if err := f.worksMatchActual(0, nil)(); err != nil {
							return err
						}
						return f.resourceVersionUnchangedActual()()
					}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding or the work objects have been updated unexpectedly")
				})

				It("can delete the stray snapshot", func() {
					f.deleteStraySnapshot(strayName)
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

				AfterAll(func() {
					f.teardown()
				})
			})
		}

		Context(fmt.Sprintf("stray snapshots further down the listing (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)
			var set1 *snapshotSet

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can create a snapshot set along with two stray snapshots beyond its count", func() {
				// Both stray snapshots have the sub-index right after the counted ones; only the first of them in the
				// sorted listing is checked (and found not to be a duplicate of the last counted snapshot), and the
				// second, a duplicate of the first, is ignored.
				set1 = f.newSnapshotSet(1, 2)
				f.createStraySnapshot(1, 2, 2, dummyContentsHash)
				f.createStraySnapshot(1, 2, 2, dummyContentsHash)
				f.createSnapshotsInSet(set1)
			})

			It("can roll out the snapshot set", func() {
				updatePlacementBindingResourceSnapshot(sc.bindingNS, f.bindingName, set1.primaryName())
			})

			It("should update the work objects (and not create any for the stray snapshots)", func() {
				Eventually(f.worksMatchActual(1, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
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

		Context(fmt.Sprintf("stray primary snapshot at the same index (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)
			var set1 *snapshotSet
			var strayName string

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can create a snapshot set along with a stray primary snapshot at the same index", func() {
				set1 = f.newSnapshotSet(1, 2)
				strayName = f.createStraySnapshot(1, 0, 2, dummyContentsHash)
				f.createSnapshotsInSet(set1)
			})

			It("can roll out the snapshot set", func() {
				updatePlacementBindingResourceSnapshot(sc.bindingNS, f.bindingName, set1.primaryName())
			})

			It("should report the duplicate sub-indices", func() {
				set0 := f.sets[0]
				wantStatus := wantPlacementBindingStatusInvalidSnapshots(set0.resourceCount(), set0.primaryName(), f.status)
				Eventually(f.invalidSnapshotsStatusActual(wantStatus, duplicateSubIdxErrMsg),
					eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the duplicate sub-indices")
			})

			It("should not update the work objects", func() {
				Consistently(f.worksMatchActual(0, nil), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The work objects have been updated unexpectedly")
			})

			It("can delete the stray snapshot", func() {
				f.deleteStraySnapshot(strayName)
			})

			It("should update the work objects", func() {
				Eventually(f.worksMatchActual(1, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
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

		Context(fmt.Sprintf("gap in the counted sub-indices (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)
			var set1 *snapshotSet

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can create a snapshot set with a missing sub-index and an extra snapshot beyond its count", func() {
				// Sub-indices 0 and 2 (of 3), plus a stray snapshot of sub-index 3.
				set1 = f.newSnapshotSet(1, 3)
				f.createStraySnapshot(1, 3, 3, dummyContentsHash)
				f.createSnapshotsInSet(set1, 2, 0)
			})

			It("can roll out the snapshot set", func() {
				updatePlacementBindingResourceSnapshot(sc.bindingNS, f.bindingName, set1.primaryName())
			})

			It("should report the inconsistent sub-indices", func() {
				set0 := f.sets[0]
				wantStatus := wantPlacementBindingStatusInvalidSnapshots(set0.resourceCount(), set0.primaryName(), f.status)
				Eventually(f.invalidSnapshotsStatusActual(wantStatus, inconsistentSubIdxErrMsg),
					eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the inconsistent sub-indices")
			})

			It("should not update the work objects", func() {
				Consistently(f.worksMatchActual(0, nil), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The work objects have been updated unexpectedly")
			})

			It("can create the missing snapshot", func() {
				f.createSnapshotsInSet(set1, 1)
			})

			It("should update and create the work objects (and not create any for the extra snapshot)", func() {
				Eventually(f.worksMatchActual(1, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update and create the work objects")
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

		Context(fmt.Sprintf("new binding with a missing snapshot (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)
			var set0 *snapshotSet

			BeforeAll(func() {
				set0 = f.newSnapshotSet(0, 1)
				f.createBinding(0)
			})

			It("should add cleanup finalizer to the placement binding", func() {
				Eventually(placementBindingFinalizerAddedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
			})

			It("should mark the placement binding as failed to synchronize due to invalid snapshots", func() {
				// Nothing has been processed (or placed) yet.
				wantStatus := wantPlacementBindingStatusInvalidSnapshots(0, "", nil)
				wantStatus.LastProcessedResourceSnapshotName = nil
				Eventually(f.invalidSnapshotsStatusActual(wantStatus, fmt.Sprintf(snapshotNotFoundErrMsgFmt, set0.primaryName())),
					eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the placement binding as failed to synchronize")
			})

			It("should not generate any work object", func() {
				Consistently(f.noWorksActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Work objects have been generated unexpectedly")
			})

			It("can create the missing snapshot", func() {
				f.createSnapshotsInSet(set0)
			})

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

		Context(fmt.Sprintf("sync strategy change while the processed snapshot is missing (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)

			BeforeAll(func() {
				f.createSnapshots(0, 2)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can record the UIDs of the work objects and the placement binding status", func() {
				f.recordWorkUIDs()
				f.recordStatus()
			})

			It("can delete the primary snapshot in use", func() {
				f.deleteSnapshot(0, 0)
			})

			It("can set a sync strategy on the placement binding", func() {
				updatePlacementBindingSyncStrategy(sc.bindingNS, f.bindingName, serverSideApplyStrategy)
			})

			It("should update the sync strategy of the work objects in place, without the snapshots", func() {
				Eventually(f.worksMatchBindingStrategyActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
				f.expectWorksNotRecreated()
			})

			// A sync strategy change alone is applied to the work objects without resetting the placement binding
			// status (and without reporting the missing snapshot).
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

		Context(fmt.Sprintf("sync strategy change along with a rollout to a missing snapshot (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)
			var set1 *snapshotSet

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can record the UIDs of the work objects", func() {
				f.recordWorkUIDs()
			})

			It("can set a sync strategy and roll out a snapshot that does not exist yet in one update", func() {
				set1 = f.newSnapshotSet(1, 1)
				f.updateSpec(func(spec *placementv1alpha1.PlacementBindingSpec) {
					spec.SyncStrategy = serverSideApplyStrategy.DeepCopy()
					spec.ResourceSnapshotName = set1.primaryName()
				})
			})

			It("should mark the placement binding as failed to synchronize due to invalid snapshots", func() {
				set0 := f.sets[0]
				wantStatus := wantPlacementBindingStatusInvalidSnapshots(set0.resourceCount(), set0.primaryName(), f.status)
				Eventually(f.invalidSnapshotsStatusActual(wantStatus, fmt.Sprintf(snapshotNotFoundErrMsgFmt, set1.primaryName())),
					eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the placement binding as failed to synchronize")
			})

			It("should not update the work objects (including their sync strategy)", func() {
				Consistently(f.worksMatchActual(0, nil), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The work objects have been updated unexpectedly")
			})

			It("can create the missing snapshot", func() {
				f.createSnapshotsInSet(set1)
			})

			It("should update the work object in place, with the new snapshot and the sync strategy", func() {
				Eventually(f.worksMatchBindingStrategyActual(1), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work object")
				f.expectWorksNotRecreated()
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

		Context(fmt.Sprintf("work object deleted out of band while the processed snapshot is missing (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)

			BeforeAll(func() {
				f.createSnapshots(0, 2)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can record the UIDs of the work objects", func() {
				f.recordWorkUIDs()
			})

			It("can delete the primary snapshot in use", func() {
				f.deleteSnapshot(0, 0)
			})

			It("can delete the secondary work object", func() {
				deleteWork(f.workNS, f.workName(1))
			})

			It("should mark the placement binding as failed to synchronize, at the same generation", func() {
				// The placement binding still points to the processed snapshot, so availability is invalidated;
				// the resource counters are tallied from the remaining (primary) work object.
				set0 := f.sets[0]
				placedResources := int32(resourcesPerSnapshot)
				wantStatus := &placementv1alpha1.PlacementBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
							Status: metav1.ConditionFalse,
							Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonInvalidSnapshots,
						},
						{
							Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
							Status: metav1.ConditionFalse,
							Reason: placementv1alpha1.PlacementBindingAvailableCondReasonIncompleteSetOfResources,
						},
					},
					SelectedResources:                 ptr.To(placedResources),
					SynchronizedResources:             ptr.To(placedResources),
					AvailableResources:                ptr.To(placedResources),
					LastProcessedResourceSnapshotName: ptr.To(set0.primaryName()),
				}
				Eventually(f.invalidSnapshotsStatusActual(wantStatus, fmt.Sprintf(snapshotNotFoundErrMsgFmt, set0.primaryName())),
					eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the placement binding as failed to synchronize")
			})

			It("should not re-create the secondary work object", func() {
				Consistently(func() error {
					uids, err := f.listWorkUIDs()
					if err != nil {
						return err
					}
					if _, found := uids[f.workName(1)]; found {
						return fmt.Errorf("the secondary work object has been re-created")
					}
					if uids[f.workName(0)] != f.workUIDs[f.workName(0)] {
						return fmt.Errorf("the primary work object has been re-created or removed")
					}
					return nil
				}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The work objects have been changed unexpectedly")
			})

			It("can re-create the primary snapshot", func() {
				f.createSnapshotsInSet(f.sets[0], 0)
			})

			It("should re-create the secondary work object", func() {
				Eventually(f.worksMatchActual(0, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-create the secondary work object")
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

		Context(fmt.Sprintf("suspend, resume, and delete a binding with a missing snapshot (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)
			var set1 *snapshotSet

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can roll out a snapshot that does not exist yet", func() {
				set1 = f.newSnapshotSet(1, 1)
				updatePlacementBindingResourceSnapshot(sc.bindingNS, f.bindingName, set1.primaryName())
			})

			It("should mark the placement binding as failed to synchronize due to invalid snapshots", func() {
				set0 := f.sets[0]
				wantStatus := wantPlacementBindingStatusInvalidSnapshots(set0.resourceCount(), set0.primaryName(), f.status)
				Eventually(f.invalidSnapshotsStatusActual(wantStatus, fmt.Sprintf(snapshotNotFoundErrMsgFmt, set1.primaryName())),
					eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the placement binding as failed to synchronize")
				f.recordStatus()
			})

			It("can suspend the placement binding", func() {
				updatePlacementBindingSuspended(sc.bindingNS, f.bindingName, true)
			})

			It("should remove the work object", func() {
				Eventually(workRemovedActual(f.workNS, f.workName(0)), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")
			})

			It("should unset the last processed snapshot and remove the cleanup finalizer, leaving the rest of the status as it is", func() {
				wantStatus := f.status.DeepCopy()
				wantStatus.LastProcessedResourceSnapshotName = nil
				Eventually(placementBindingStatusMatchesActual(sc.bindingNS, f.bindingName, wantStatus), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to unset the last processed snapshot")
				Eventually(placementBindingFinalizerRemovedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
			})

			It("can resume the placement binding", func() {
				updatePlacementBindingSuspended(sc.bindingNS, f.bindingName, false)
			})

			It("should add cleanup finalizer to the placement binding", func() {
				Eventually(placementBindingFinalizerAddedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
			})

			It("should mark the placement binding as failed to synchronize again, at the new generation", func() {
				// No snapshot has been processed (or resource placed) since the suspension.
				wantStatus := wantPlacementBindingStatusInvalidSnapshots(0, "", nil)
				wantStatus.LastProcessedResourceSnapshotName = nil
				Eventually(f.invalidSnapshotsStatusActual(wantStatus, fmt.Sprintf(snapshotNotFoundErrMsgFmt, set1.primaryName())),
					eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the placement binding as failed to synchronize")
			})

			It("should not generate any work object", func() {
				Consistently(f.noWorksActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Work objects have been generated unexpectedly")
			})

			It("can delete the placement binding", func() {
				removePlacementBinding(sc.bindingNS, f.bindingName)
			})

			It("should remove the placement binding", func() {
				Eventually(placementBindingRemovedActual(sc.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")
			})

			AfterAll(func() {
				f.teardown()
			})
		})
	}
})
