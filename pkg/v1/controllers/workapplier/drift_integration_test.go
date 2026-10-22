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

package workapplier

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var _ = Describe("drift detection", func() {
	Context("apply anyway (default), partial comparison", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularDeploy *appsv1.Deployment

		BeforeAll(func() {
			// Prepare the manifests.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a new work object with no sync strategy; by default, the work applier overwrites drifts and
			// uses partial comparison.
			createWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, regularDeployJSON)
		})

		It("should apply the manifests", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the deployment object")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
		})

		It("should update the work object status", func() {
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    1,
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Resource:   "deployments",
						Name:       deployName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 1,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 1,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("can introduce a drift on the deployment and add a label that the manifest does not specify", func() {
			updateDeploymentInMemberCluster(memberClient1, nsName, deployName, func(deploy *appsv1.Deployment) {
				deploy.Spec.Replicas = ptr.To(int32(2))
				deploy.Labels = map[string]string{
					memberOnlyLabelKey: memberOnlyLabelValue,
				}
			})
		})

		It("should overwrite the drift", func() {
			// The change in the member cluster triggers no event for the work object; the drift is overwritten at the
			// next requeue of the work object, with no change on the work object itself.
			wantDeploy := regularDeploy.DeepCopy()
			wantDeploy.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			deploymentObjectActual := deploymentObjectActual(memberClient1, wantDeploy)
			Eventually(deploymentObjectActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to overwrite the drift on the deployment")
		})

		It("should keep the label that the manifest does not specify", func() {
			objectLabelsActual := objectLabelsActual(memberClient1, regularDeploy, map[string]string{
				memberOnlyLabelKey: memberOnlyLabelValue,
			})
			Consistently(objectLabelsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The label that the manifest does not specify has been changed")
		})

		It("can mark the deployment as available", func() {
			// The deployment has been updated twice (by the drift, and by the work applier) since it was last marked
			// as available.
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
		})

		It("should update the work object status (no diff reported)", func() {
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    1,
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Resource:   "deployments",
						Name:       deployName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 3,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 3,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the deployment manually, as there is no GC controller in the test environment.
			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("report error, partial comparison (drift appears, changes, and is reverted)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		// The deployment as it is after the drift has been introduced.
		var driftedDeploy *appsv1.Deployment
		// The timestamp when the drift was first observed.
		var firstDiffedTimestamp metav1.Time

		// The status of the namespace manifest stays the same throughout the test case.
		nsManifestStatus := func() placementv1alpha1.PerManifestStatus {
			return placementv1alpha1.PerManifestStatus{
				Identifier: placementv1alpha1.ManifestIdentifier{
					Ordinal:    0,
					APIGroup:   "",
					APIVersion: "v1",
					Kind:       "Namespace",
					Resource:   "namespaces",
					Name:       nsName,
				},
				Conditions: []metav1.Condition{
					{
						Type:               placementv1alpha1.ManifestCondTypeApplied,
						Status:             metav1.ConditionTrue,
						Reason:             string(ApplyResTypeApplied),
						ObservedGeneration: 0,
					},
					{
						Type:               placementv1alpha1.ManifestCondTypeAvailable,
						Status:             metav1.ConditionTrue,
						Reason:             string(AvailabilityResultTypeAvailable),
						ObservedGeneration: 0,
					},
				},
			}
		}
		deployManifestIdentifier := func() placementv1alpha1.ManifestIdentifier {
			return placementv1alpha1.ManifestIdentifier{
				Ordinal:    1,
				APIGroup:   "apps",
				APIVersion: "v1",
				Kind:       "Deployment",
				Resource:   "deployments",
				Name:       deployName,
				Namespace:  nsName,
			}
		}

		BeforeAll(func() {
			// Prepare the manifests.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a new work object that reports drifts instead of overwriting them; the API server fills in the
			// defaults for the other fields in the sync strategy (partial comparison).
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenDrifted: placementv1alpha1.WhenDriftedOptionReportError,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularNSJSON, regularDeployJSON)
		})

		It("should apply the manifests", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the deployment object")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
		})

		It("should update the work object status", func() {
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				nsManifestStatus(),
				{
					Identifier: deployManifestIdentifier(),
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 1,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 1,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("can introduce a drift on the deployment", func() {
			driftedDeploy = updateDeploymentInMemberCluster(memberClient1, nsName, deployName, func(deploy *appsv1.Deployment) {
				deploy.Spec.Replicas = ptr.To(int32(2))
			})
		})

		It("should report the drift", func() {
			// The change in the member cluster triggers no event for the work object; the drift is found at the next
			// requeue of the work object. The manifest is no longer applied, and its Available condition is removed.
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAppliedCondNotAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAvailableCondNotAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				nsManifestStatus(),
				{
					Identifier: deployManifestIdentifier(),
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeFoundDrifts),
							ObservedGeneration: 2,
						},
					},
					DiffDetails: &placementv1alpha1.DiffDetails{
						ObservedInMemberClusterGeneration: ptr.To(int64(2)),
						ObservedDiffs: []placementv1alpha1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "2",
								ValueInHub:    "1",
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the drift")

			firstDiffedTimestamp = retrieveManifestFirstDiffedTimestamp(memberReservedNSName1, workName, 1)
		})

		It("should not overwrite the drift", func() {
			// The check spans at least one periodic requeue of the work object.
			deployUntouchedActual := objectUntouchedActual(memberClient1, driftedDeploy)
			Consistently(deployUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The drifted deployment has been modified")
		})

		It("can change the drift on the deployment", func() {
			updateDeploymentInMemberCluster(memberClient1, nsName, deployName, func(deploy *appsv1.Deployment) {
				deploy.Spec.Replicas = ptr.To(int32(3))
			})
		})

		It("should report the changed drift", func() {
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAppliedCondNotAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAvailableCondNotAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				nsManifestStatus(),
				{
					Identifier: deployManifestIdentifier(),
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeFoundDrifts),
							ObservedGeneration: 3,
						},
					},
					DiffDetails: &placementv1alpha1.DiffDetails{
						ObservedInMemberClusterGeneration: ptr.To(int64(3)),
						ObservedDiffs: []placementv1alpha1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "3",
								ValueInHub:    "1",
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the changed drift")
		})

		It("should keep the timestamp when the drift was first observed", func() {
			// The diff details have been rewritten since the timestamp was captured, but the object has stayed
			// drifted all along.
			gotTimestamp := retrieveManifestFirstDiffedTimestamp(memberReservedNSName1, workName, 1)
			Expect(gotTimestamp.Equal(&firstDiffedTimestamp)).To(BeTrue(), "first diffed timestamp = %v, want %v", gotTimestamp, firstDiffedTimestamp)
		})

		It("can revert the drift on the deployment", func() {
			updateDeploymentInMemberCluster(memberClient1, nsName, deployName, func(deploy *appsv1.Deployment) {
				deploy.Spec.Replicas = ptr.To(int32(1))
			})
		})

		It("should report the manifest as applied again (deployment not yet available)", func() {
			// The deployment has been updated three times since it was last marked as available.
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAvailableCondNotAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				nsManifestStatus(),
				{
					Identifier: deployManifestIdentifier(),
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 4,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionFalse,
							Reason:             string(AvailabilityResultTypeNotYetAvailable),
							ObservedGeneration: 4,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the manifest as applied again")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
		})

		It("should update the work object status (all available)", func() {
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				nsManifestStatus(),
				{
					Identifier: deployManifestIdentifier(),
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 4,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 4,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the deployment manually, as there is no GC controller in the test environment.
			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("report error, full comparison", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularConfigMap *corev1.ConfigMap
		// The config map as it is after a label has been added in the member cluster.
		var labeledConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Create the namespace directly in the member cluster; the work object does not feature the namespace
			// manifest, as full comparison always finds diffs on the fields that the API server populates on
			// namespaces (e.g., the finalizers), which the work applier would then keep reporting as drifts.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifest.
			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create a new work object that reports drifts instead of overwriting them, and that compares all the
			// fields, including those that the manifest does not specify.
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenDrifted:      placementv1alpha1.WhenDriftedOptionReportError,
				ComparisonOption: placementv1alpha1.ComparisonOptionFullComparison,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON)
		})

		It("should apply the manifest", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")
		})

		It("should update the work object status", func() {
			// No diff is reported.
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       configMapName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("can add a label that the manifest does not specify to the config map", func() {
			labeledConfigMap = updateConfigMapInMemberCluster(memberClient1, nsName, configMapName, func(configMap *corev1.ConfigMap) {
				configMap.Labels = map[string]string{
					memberOnlyLabelKey: memberOnlyLabelValue,
				}
			})
		})

		It("should report the label as a drift", func() {
			// The change in the member cluster triggers no event for the work object; the drift is found at the next
			// requeue of the work object. The manifest is no longer applied, and its Available condition is removed.
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAppliedCondNotAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAvailableCondNotAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       configMapName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeFoundDrifts),
							ObservedGeneration: 0,
						},
					},
					DiffDetails: &placementv1alpha1.DiffDetails{
						ObservedInMemberClusterGeneration: ptr.To(int64(0)),
						ObservedDiffs: []placementv1alpha1.PatchDetail{
							{
								Path:          fmt.Sprintf("/metadata/labels/%s", memberOnlyLabelKey),
								ValueInMember: memberOnlyLabelValue,
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the drift")
		})

		It("should not overwrite the drift", func() {
			// The check spans at least one periodic requeue of the work object.
			configMapUntouchedActual := objectUntouchedActual(memberClient1, labeledConfigMap)
			Consistently(configMapUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The drifted config map has been modified")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the config map manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("report error, partial comparison (manifest updated after a drift)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		// The image that the updated manifest specifies.
		updatedImage := "nginx:1.27"

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNSJSON []byte
		var updatedDeploy *appsv1.Deployment
		var syncStrategy *placementv1alpha1.SyncStrategy

		// The status of the namespace manifest stays the same throughout the test case.
		nsManifestStatus := func() placementv1alpha1.PerManifestStatus {
			return placementv1alpha1.PerManifestStatus{
				Identifier: placementv1alpha1.ManifestIdentifier{
					Ordinal:    0,
					APIGroup:   "",
					APIVersion: "v1",
					Kind:       "Namespace",
					Resource:   "namespaces",
					Name:       nsName,
				},
				Conditions: []metav1.Condition{
					{
						Type:               placementv1alpha1.ManifestCondTypeApplied,
						Status:             metav1.ConditionTrue,
						Reason:             string(ApplyResTypeApplied),
						ObservedGeneration: 0,
					},
					{
						Type:               placementv1alpha1.ManifestCondTypeAvailable,
						Status:             metav1.ConditionTrue,
						Reason:             string(AvailabilityResultTypeAvailable),
						ObservedGeneration: 0,
					},
				},
			}
		}
		deployManifestIdentifier := func() placementv1alpha1.ManifestIdentifier {
			return placementv1alpha1.ManifestIdentifier{
				Ordinal:    1,
				APIGroup:   "apps",
				APIVersion: "v1",
				Kind:       "Deployment",
				Resource:   "deployments",
				Name:       deployName,
				Namespace:  nsName,
			}
		}

		BeforeAll(func() {
			// Prepare the manifests.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON = marshalK8sObjJSON(regularNS)

			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Prepare the updated deployment manifest; it changes the image, but keeps the replica count.
			updatedDeploy = regularDeploy.DeepCopy()
			updatedDeploy.Spec.Template.Spec.Containers[0].Image = updatedImage

			// Create a new work object that reports drifts instead of overwriting them.
			syncStrategy = &placementv1alpha1.SyncStrategy{
				WhenDrifted: placementv1alpha1.WhenDriftedOptionReportError,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularNSJSON, regularDeployJSON)
		})

		It("should apply the manifests", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the deployment object")
		})

		It("can introduce a drift on the deployment", func() {
			updateDeploymentInMemberCluster(memberClient1, nsName, deployName, func(deploy *appsv1.Deployment) {
				deploy.Spec.Replicas = ptr.To(int32(2))
			})
		})

		It("should report the drift", func() {
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAppliedCondNotAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAvailableCondNotAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				nsManifestStatus(),
				{
					Identifier: deployManifestIdentifier(),
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeFoundDrifts),
							ObservedGeneration: 2,
						},
					},
					DiffDetails: &placementv1alpha1.DiffDetails{
						ObservedInMemberClusterGeneration: ptr.To(int64(2)),
						ObservedDiffs: []placementv1alpha1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "2",
								ValueInHub:    "1",
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the drift")
		})

		It("can update the deployment manifest", func() {
			updateWorkObject(workName, memberReservedNSName1, syncStrategy, regularNSJSON, marshalK8sObjJSON(updatedDeploy))
		})

		It("should apply the updated manifest and overwrite the drift", func() {
			// Drift detection runs only if the manifest has not changed since it was last applied; the updated
			// manifest is applied regardless of the drift, which brings the replica count back to the value that
			// the manifest specifies.
			wantDeploy := updatedDeploy.DeepCopy()
			wantDeploy.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			deploymentObjectActual := deploymentObjectActual(memberClient1, wantDeploy)
			Eventually(deploymentObjectActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the updated manifest")
		})

		It("should update the work object status (no drift reported)", func() {
			// The deployment has been updated twice (by the drift, and by the work applier) since it was created, and
			// has never been marked as available.
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAvailableCondNotAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				nsManifestStatus(),
				{
					Identifier: deployManifestIdentifier(),
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 3,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionFalse,
							Reason:             string(AvailabilityResultTypeNotYetAvailable),
							ObservedGeneration: 3,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the deployment manually, as there is no GC controller in the test environment.
			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("apply anyway, full comparison (diffs reported after apply ops)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularConfigMap *corev1.ConfigMap

		// The status of the namespace manifest stays the same throughout the test case: the namespace manifest does
		// not specify any finalizer, while the API server adds the finalizer for the namespace on its own; full
		// comparison reports this as a diff, which the apply ops cannot resolve.
		nsManifestStatus := func() placementv1alpha1.PerManifestStatus {
			return placementv1alpha1.PerManifestStatus{
				Identifier: placementv1alpha1.ManifestIdentifier{
					Ordinal:    0,
					APIGroup:   "",
					APIVersion: "v1",
					Kind:       "Namespace",
					Resource:   "namespaces",
					Name:       nsName,
				},
				Conditions: []metav1.Condition{
					{
						Type:               placementv1alpha1.ManifestCondTypeApplied,
						Status:             metav1.ConditionTrue,
						Reason:             string(ApplyResTypeApplied),
						ObservedGeneration: 0,
					},
					{
						Type:               placementv1alpha1.ManifestCondTypeAvailable,
						Status:             metav1.ConditionTrue,
						Reason:             string(AvailabilityResultTypeAvailable),
						ObservedGeneration: 0,
					},
				},
				DiffDetails: &placementv1alpha1.DiffDetails{
					ObservedInMemberClusterGeneration: ptr.To(int64(0)),
					ObservedDiffs: []placementv1alpha1.PatchDetail{
						{
							Path:          "/spec/finalizers",
							ValueInMember: "[kubernetes]",
						},
					},
				},
			}
		}
		configMapManifestIdentifier := func() placementv1alpha1.ManifestIdentifier {
			return placementv1alpha1.ManifestIdentifier{
				Ordinal:    1,
				APIGroup:   "",
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Resource:   "configmaps",
				Name:       configMapName,
				Namespace:  nsName,
			}
		}

		BeforeAll(func() {
			// Prepare the manifests.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create a new work object that overwrites drifts (the default), and that compares all the fields,
			// including those that the manifest does not specify.
			syncStrategy := &placementv1alpha1.SyncStrategy{
				ComparisonOption: placementv1alpha1.ComparisonOptionFullComparison,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularNSJSON, regularConfigMapJSON)
		})

		It("should apply the manifests", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")
		})

		It("should update the work object status", func() {
			// The manifests are applied; only the namespace manifest has diffs.
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				nsManifestStatus(),
				{
					Identifier: configMapManifestIdentifier(),
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("can add a label that the manifest does not specify to the config map", func() {
			updateConfigMapInMemberCluster(memberClient1, nsName, configMapName, func(configMap *corev1.ConfigMap) {
				configMap.Labels = map[string]string{
					memberOnlyLabelKey: memberOnlyLabelValue,
				}
			})
		})

		It("should report the label as a diff while keeping the manifest applied", func() {
			// The change in the member cluster triggers no event for the work object; the diff is found at the next
			// requeue of the work object.
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				nsManifestStatus(),
				{
					Identifier: configMapManifestIdentifier(),
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
					DiffDetails: &placementv1alpha1.DiffDetails{
						ObservedInMemberClusterGeneration: ptr.To(int64(0)),
						ObservedDiffs: []placementv1alpha1.PatchDetail{
							{
								Path:          fmt.Sprintf("/metadata/labels/%s", memberOnlyLabelKey),
								ValueInMember: memberOnlyLabelValue,
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the diff")
		})

		It("should keep the label that the manifest does not specify", func() {
			// The apply ops (client-side apply) do not remove fields that the manifest has never specified.
			wantConfigMap := regularConfigMap.DeepCopy()
			wantConfigMap.Labels = map[string]string{
				memberOnlyLabelKey: memberOnlyLabelValue,
			}
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Consistently(configMapObjectActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The label that the manifest does not specify has been changed")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the config map manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})
})
