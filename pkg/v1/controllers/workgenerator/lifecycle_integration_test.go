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
	"strconv"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

// scope describes the scope (namespace-scoped or cluster-scoped) of the placement bindings (and their placement
// resource snapshots) that a test runs against.
type scope struct {
	name                string
	bindingNS           string
	bindingNameTemplate string
	policyNameTemplate  string
}

var scopes = []scope{
	{
		name:                "namespace-scoped",
		bindingNS:           appNamespaceName,
		bindingNameTemplate: placementBindingNameTemplate,
		policyNameTemplate:  placementPolicyNameTemplate,
	},
	{
		name:                "cluster-scoped",
		bindingNS:           clusterScopedNS,
		bindingNameTemplate: clusterPlacementBindingNameTemplate,
		policyNameTemplate:  clusterPlacementPolicyNameTemplate,
	},
}

// Every placement resource snapshot in this file carries a NS, a Deployment, and a ConfigMap.
const resourcesPerSnapshot = 3

// snapshotSet is a set of placement resource snapshots that share the same index; the first one is the
// primary snapshot (sub-index 0).
type snapshotSet struct {
	names       []string
	snapshots   []placementv1alpha1.PlacementResourceSnapshotAccessor
	identifiers [][]placementv1alpha1.ManifestIdentifier
}

func (s *snapshotSet) primaryName() string {
	return s.names[0]
}

func (s *snapshotSet) resourceCount() int32 {
	return int32(len(s.names) * resourcesPerSnapshot) //nolint:gosec // safe: the counts are tiny.
}

// bindingFixture bundles the objects and the expectations of a placement binding bound to a member cluster.
// Its methods that touch the API server must be called at spec run time (not at tree construction time).
type bindingFixture struct {
	scope       scope
	clusterName string
	bindingName string
	policyName  string
	workNS      string

	// The snapshot sets created so far, keyed by snapshot index.
	sets map[int]*snapshotSet
	// The UIDs of the work objects, keyed by work name; recorded by tests that check if work objects are
	// updated in place or re-created.
	workUIDs map[string]types.UID
}

func newBindingFixture(sc scope, clusterName string) *bindingFixture {
	return &bindingFixture{
		scope:       sc,
		clusterName: clusterName,
		bindingName: fmt.Sprintf(sc.bindingNameTemplate, utils.RandStr()),
		policyName:  fmt.Sprintf(sc.policyNameTemplate, utils.RandStr()),
		workNS:      fmt.Sprintf(utils.NamespaceNameFormat, clusterName),
		sets:        map[int]*snapshotSet{},
	}
}

// createSnapshots creates `count` placement resource snapshots of the given index, each with its own NS, Deployment,
// and ConfigMap.
func (f *bindingFixture) createSnapshots(index, count int) *snapshotSet {
	set := &snapshotSet{
		names:       make([]string, count),
		snapshots:   make([]placementv1alpha1.PlacementResourceSnapshotAccessor, count),
		identifiers: make([][]placementv1alpha1.ManifestIdentifier, count),
	}
	suffix := utils.RandStr()
	resources := make([][]placementv1alpha1.SnapshottedResource, count)
	for subIdx := 0; subIdx < count; subIdx++ {
		if subIdx == 0 {
			set.names[subIdx] = fmt.Sprintf(placementResourceSnapshotNameTemplate, f.policyName, index)
		} else {
			set.names[subIdx] = fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, f.policyName, index, subIdx)
		}

		nsName := fmt.Sprintf("ns-%s-%d", suffix, subIdx)
		deployName := fmt.Sprintf(deployNameTemplate, subIdx)
		configMapName := fmt.Sprintf(configMapNameTemplate, subIdx)
		resources[subIdx] = []placementv1alpha1.SnapshottedResource{
			nsSnapshottedResource(nsName),
			deploySnapshottedResource(nsName, deployName),
			configMapSnapshottedResource(nsName, configMapName),
		}
		set.identifiers[subIdx] = []placementv1alpha1.ManifestIdentifier{
			nsManifestIdentifier(0, nsName),
			deployManifestIdentifier(1, nsName, deployName),
			configMapManifestIdentifier(2, nsName, configMapName),
		}
	}

	// Create the secondary snapshots first, as the placement resource snapshot manager would do.
	for subIdx := count - 1; subIdx >= 0; subIdx-- {
		set.snapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(f.scope.bindingNS, set.names[subIdx], f.policyName,
			index, subIdx, count, resources[subIdx]...)
	}
	f.sets[index] = set
	return set
}

// createBinding creates the placement binding, bound to the primary snapshot of the given snapshot set.
func (f *bindingFixture) createBinding(setIdx int) {
	createPlacementBinding(f.scope.bindingNS, f.bindingName, f.policyName, f.clusterName, f.sets[setIdx].primaryName())
}

// workName returns the name of the work object derived from the snapshot with the given sub-index.
func (f *bindingFixture) workName(subIdx int) string {
	binding := newPlacementBindingObj(f.scope.bindingNS, f.bindingName)
	binding.SetSpec(placementv1alpha1.PlacementBindingSpec{PlacementPolicyName: f.policyName})
	return uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
		&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
}

// worksMatchActual verifies that the work objects of the binding are exactly those derived from the given snapshot
// set, with the given sync strategy.
func (f *bindingFixture) worksMatchActual(setIdx int, syncStrategy *placementv1alpha1.SyncStrategy) func() error {
	return func() error {
		set := f.sets[setIdx]

		// The secondary work objects should be owned by the primary work object; retrieve its current UID as it
		// changes whenever the primary work object is re-created.
		primaryWork := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: f.workNS, Name: f.workName(0)}, primaryWork); err != nil {
			return fmt.Errorf("failed to retrieve the primary work object: %w", err)
		}
		ownerRef := &metav1.OwnerReference{
			APIVersion:         placementv1alpha1.GroupVersion.String(),
			Kind:               workKind,
			Name:               primaryWork.Name,
			UID:                primaryWork.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		}

		wantWorks := make([]placementv1alpha1.Work, len(set.names))
		for subIdx := range set.names {
			wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(f.workNS, f.workName(subIdx), f.scope.bindingNS, f.bindingName, f.policyName,
				set.primaryName(), subIdx, len(set.names), ownerRef, set.snapshots[subIdx])
			wantWorks[subIdx].Spec.SyncStrategy = syncStrategy.DeepCopy()
		}
		return worksGeneratedActual(f.clusterName, f.scope.bindingNS, f.bindingName, wantWorks)()
	}
}

// noWorksActual verifies that there is no work object for the binding.
func (f *bindingFixture) noWorksActual() func() error {
	return worksGeneratedActual(f.clusterName, f.scope.bindingNS, f.bindingName, nil)
}

func (f *bindingFixture) waitingStatusActual(setIdx int) func() error {
	set := f.sets[setIdx]
	return placementBindingStatusUpdatedActual(f.scope.bindingNS, f.bindingName,
		wantPlacementBindingStatusWaitingForSync(set.resourceCount(), set.primaryName()))
}

func (f *bindingFixture) syncedStatusActual(setIdx int) func() error {
	set := f.sets[setIdx]
	return placementBindingStatusUpdatedActual(f.scope.bindingNS, f.bindingName,
		wantPlacementBindingStatusSyncedAndAvailable(set.resourceCount(), set.primaryName()))
}

// markWorks marks the work objects with the given sub-indices (all of them if none is given) as applied and
// available.
func (f *bindingFixture) markWorks(setIdx int, subIdxs ...int) {
	set := f.sets[setIdx]
	if len(subIdxs) == 0 {
		for subIdx := range set.names {
			subIdxs = append(subIdxs, subIdx)
		}
	}
	for _, subIdx := range subIdxs {
		markWorkAsAppliedAndAvailable(f.workNS, f.workName(subIdx), set.identifiers[subIdx])
	}
}

// listWorkUIDs returns the UIDs of the work objects of the binding, keyed by work name.
func (f *bindingFixture) listWorkUIDs() (map[string]types.UID, error) {
	workList := &placementv1alpha1.WorkList{}
	if err := hubClient.List(ctx, workList,
		client.InNamespace(f.workNS),
		client.MatchingLabels{
			placementv1alpha1.WorkOwnerNamespaceLabelKey:          f.scope.bindingNS,
			placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: f.bindingName,
		},
	); err != nil {
		return nil, fmt.Errorf("failed to list work objects: %w", err)
	}
	uids := make(map[string]types.UID, len(workList.Items))
	for idx := range workList.Items {
		uids[workList.Items[idx].Name] = workList.Items[idx].UID
	}
	return uids, nil
}

// recordWorkUIDs records the current UIDs of the work objects of the binding.
func (f *bindingFixture) recordWorkUIDs() {
	uids, err := f.listWorkUIDs()
	Expect(err).To(Succeed(), "Failed to list the work objects")
	f.workUIDs = uids
}

// registerBaselineSpecs registers the specs that generate the work objects from the given snapshot set and bring the
// binding to the synchronized and available state.
func (f *bindingFixture) registerBaselineSpecs(setIdx int) {
	It("should add cleanup finalizer to the placement binding", func() {
		finalizerAddedActual := placementBindingFinalizerAddedActual(f.scope.bindingNS, f.bindingName)
		Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
	})

	It("should generate the work objects", func() {
		Eventually(f.worksMatchActual(setIdx, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
	})

	It("should refresh the placement binding status", func() {
		Eventually(f.waitingStatusActual(setIdx), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
	})

	It("can mark the work objects as applied and available", func() {
		f.markWorks(setIdx)
	})

	It("should refresh the placement binding status", func() {
		Eventually(f.syncedStatusActual(setIdx), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
	})
}

// syncWorks generates the work objects from the given snapshot set and brings the binding to the synchronized and
// available state in one go; it is for bindings that are not the focus of a test.
func (f *bindingFixture) syncWorks(setIdx int) {
	Eventually(f.worksMatchActual(setIdx, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
	Eventually(f.waitingStatusActual(setIdx), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
	f.markWorks(setIdx)
	Eventually(f.syncedStatusActual(setIdx), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
}

// teardown removes the binding, its leftover work objects, and the snapshots.
func (f *bindingFixture) teardown() {
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, newPlacementBindingObj(f.scope.bindingNS, f.bindingName)))).To(Succeed(), "Failed to delete the placement binding")
	Eventually(placementBindingRemovedActual(f.scope.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

	// The work generator deletes only the primary work object upon cleanup, and the secondary work objects are left to
	// K8s' garbage collection, which does not run in the test environment; delete any leftover explicitly.
	uids, err := f.listWorkUIDs()
	Expect(err).To(Succeed(), "Failed to list the work objects")
	for workName := range uids {
		deleteWork(f.workNS, workName)
		Eventually(workRemovedActual(f.workNS, workName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")
	}

	for _, set := range f.sets {
		for _, snapshotName := range set.names {
			Eventually(placementResourceSnapshotRemovedActual(f.scope.bindingNS, snapshotName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement resource snapshot")
		}
	}
}

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

				It("should refresh the placement binding status", func() {
					Eventually(f.waitingStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
				})

				It("can mark the work objects as applied and available", func() {
					f.markWorks(0)
				})

				It("should refresh the placement binding status", func() {
					Eventually(f.syncedStatusActual(0), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
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
