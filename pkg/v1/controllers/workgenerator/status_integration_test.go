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
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var _ = Describe("refreshing placement binding status", func() {
	for _, sc := range scopes {
		Context(fmt.Sprintf("work objects applied, but not yet checked for availability (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)

			BeforeAll(func() {
				f.createSnapshots(0, 2)
				f.createBinding(0)
			})

			f.registerWorkGenerationSpecs(0)

			It("can record the placement binding status", func() {
				f.recordStatus()
			})

			It("can mark the work objects as applied", func() {
				f.markWorksAsApplied(0)
			})

			It("should not refresh the placement binding status until the availability checks complete", func() {
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

		Context(fmt.Sprintf("work status changes that do not change the processing results (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can record the placement binding status", func() {
				f.recordStatus()
			})

			It("can update the status of the work object without changing its processing results", func() {
				touchWorkStatus(f.workNS, f.workName(0))
			})

			It("should not write to the placement binding", func() {
				Consistently(f.resourceVersionUnchangedActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding has been written to unexpectedly")
			})

			It("can mark the deployment (and consequently the work object) as unavailable", func() {
				markWorkManifestAsUnavailable(f.workNS, f.workName(0), 1)
			})

			It("should refresh the placement binding status", func() {
				set := f.sets[0]
				wantStatus := &placementv1alpha1.PlacementBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
							Status: metav1.ConditionTrue,
							Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonAllResourcesSynchronized,
						},
						{
							Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
							Status: metav1.ConditionFalse,
							Reason: placementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable,
						},
					},
					SelectedResources:     ptr.To(set.resourceCount()),
					SynchronizedResources: ptr.To(set.resourceCount()),
					AvailableResources:    ptr.To(set.resourceCount() - 1),
					FailedResources: []placementv1alpha1.FailedResource{
						{
							ObjectRef: objectRefFromManifestIdentifier(set.identifiers[0][1]),
							Conditions: []metav1.Condition{
								{
									Type:               placementv1alpha1.ManifestCondTypeAvailable,
									Status:             metav1.ConditionFalse,
									ObservedGeneration: 1,
									Reason:             markedAsUnavailableReason,
								},
							},
						},
					},
					LastProcessedResourceSnapshotName: ptr.To(set.primaryName()),
				}
				Eventually(placementBindingStatusUpdatedActual(sc.bindingNS, f.bindingName, wantStatus), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			AfterAll(func() {
				f.teardown()
			})
		})

		Context(fmt.Sprintf("work object metadata tampered with (%s)", sc.name), Ordered, func() {
			f := newBindingFixture(sc, memberCluster1Name)
			tamperedSnapshotName := fmt.Sprintf("tampered-snapshot-%s", utils.RandStr())
			var primaryWorkGeneration int64

			BeforeAll(func() {
				f.createSnapshots(0, 1)
				f.createBinding(0)
			})

			f.registerBaselineSpecs(0)

			It("can record the UIDs and generation of the work objects, and the placement binding status", func() {
				f.recordWorkUIDs()
				primaryWorkGeneration = f.workGeneration(0)
				f.recordStatus()
			})

			It("can link the work object to another snapshot", func() {
				setWorkAnnotation(f.workNS, f.workName(0), placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey, tamperedSnapshotName)
			})

			// Metadata-only changes to work objects are not events that the work generator reacts to.
			It("should not reconcile the placement binding", func() {
				tamperedActual := workAnnotationActual(f.workNS, f.workName(0), placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey, tamperedSnapshotName)
				Eventually(tamperedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the tampered work object")
				Consistently(func() error {
					if err := tamperedActual(); err != nil {
						return err
					}
					return f.statusUnchangedActual()()
				}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding has been reconciled unexpectedly")
			})

			It("can update the status of the work object", func() {
				touchWorkStatus(f.workNS, f.workName(0))
			})

			It("should re-link the work object in place, without changing its spec", func() {
				Eventually(f.worksMatchActual(0, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-link the work object")
				f.expectWorksNotRecreated()
				Expect(f.workGeneration(0)).To(Equal(primaryWorkGeneration), "The spec of the work object has been changed")
			})

			// The work generator resets the placement binding status upon re-linking, then refreshes it after a
			// requeue, as the work object does not need to be re-processed.
			It("should refresh the placement binding status", func() {
				Eventually(func() error {
					if err := f.syncedStatusActual(0)(); err != nil {
						return err
					}
					return f.syncCondTransitionedSinceRecordActual()()
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
			})

			AfterAll(func() {
				f.teardown()
			})
		})
	}

	Context("more failed resources than the reporting limit", Ordered, func() {
		// The handling of failed resources does not depend on the scope of the placement binding; the namespace-scoped
		// one is used.
		sc := scopes[0]
		f := newBindingFixture(sc, memberCluster1Name)
		configMapCount := maxFailedResourcesToReport + 1
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		identifiers := make([]placementv1alpha1.ManifestIdentifier, configMapCount)

		BeforeAll(func() {
			// The ConfigMaps are placed in descending order of their names, so that the failed resources reported
			// (sorted, then truncated) differ from the first ones in the work object.
			resources := make([]placementv1alpha1.SnapshottedResource, configMapCount)
			for idx := 0; idx < configMapCount; idx++ {
				configMapName := fmt.Sprintf(paddedConfigMapNameTemplate, configMapCount-1-idx)
				resources[idx] = configMapSnapshottedResource(nsName, configMapName)
				identifiers[idx] = configMapManifestIdentifier(idx, nsName, configMapName)
			}
			f.createSnapshotWithResources(0, resources, identifiers)
			f.createBinding(0)
		})

		f.registerWorkGenerationSpecs(0)

		It("can mark all the manifests as failed to be applied", func() {
			failedToApplyOrdinals := make(map[int]*placementv1alpha1.DiffDetails, configMapCount)
			for idx := range identifiers {
				failedToApplyOrdinals[identifiers[idx].Ordinal] = nil
			}
			markWorkManifestsAsFailed(f.workNS, f.workName(0), identifiers, failedToApplyOrdinals, nil)
		})

		It("should report the failed resources in order, up to the limit", func() {
			wantFailedResources := make([]placementv1alpha1.FailedResource, 0, maxFailedResourcesToReport)
			for num := 0; num < maxFailedResourcesToReport; num++ {
				configMapName := fmt.Sprintf(paddedConfigMapNameTemplate, num)
				wantFailedResources = append(wantFailedResources, placementv1alpha1.FailedResource{
					ObjectRef: objectRefFromManifestIdentifier(configMapManifestIdentifier(0, nsName, configMapName)),
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
							Reason:             markedAsFailedToApplyReason,
						},
					},
				})
			}
			wantStatus := &placementv1alpha1.PlacementBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonFailedToSynchronizeSomeResources,
					},
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable,
					},
				},
				SelectedResources:                 ptr.To(int32(configMapCount)),
				SynchronizedResources:             ptr.To(int32(0)),
				AvailableResources:                ptr.To(int32(0)),
				FailedResources:                   wantFailedResources,
				LastProcessedResourceSnapshotName: ptr.To(f.sets[0].primaryName()),
			}
			Eventually(placementBindingStatusInOrderActual(sc.bindingNS, f.bindingName, wantStatus), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the failed resources")
		})

		AfterAll(func() {
			f.teardown()
		})
	})
})
