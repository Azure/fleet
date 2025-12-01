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

package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	testv1alpha1 "github.com/kubefleet-dev/kubefleet/test/apis/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
	"github.com/kubefleet-dev/kubefleet/test/utils/controller"
)

const (
	randomImageName     = "random-image-name"
	testResourceCRDName = "testresources.test.kubernetes-fleet.io"
)

// Note that this container will run in parallel with other containers.
var _ = Describe("placing wrapped resources using a CRP", Ordered, func() {
	Context("Test a CRP place enveloped objects successfully", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testDeploymentEnvelope placementv1beta1.ResourceEnvelope

		BeforeAll(func() {
			readDeploymentTestManifest(&testDeployment)
			readEnvelopeResourceTestManifest(&testDeploymentEnvelope)
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Kind:    utils.NamespaceKind,
					Name:    workNamespace.Name,
					Version: corev1.SchemeGroupVersion.Version,
				},
				{
					Group:     placementv1beta1.GroupVersion.Group,
					Kind:      placementv1beta1.ResourceEnvelopeKind,
					Version:   placementv1beta1.GroupVersion.Version,
					Name:      testDeploymentEnvelope.Name,
					Namespace: workNamespace.Name,
				},
			}
		})

		It("Create the wrapped deployment resources in the namespace", func() {
			createWrappedResourcesForRollout(&testDeploymentEnvelope, &testDeployment, utils.DeploymentKind, workNamespace)
		})

		It("Create the CRP that select the namespace", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(wantSelectedResources, allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := waitForDeploymentPlacementToReady(memberCluster, &testDeployment)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("should mark the work as available", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				var works placementv1beta1.WorkList
				listOpts := []client.ListOption{
					client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, memberCluster.ClusterName)),
					// This test spec runs in parallel with other suites; there might be unrelated
					// Work objects in the namespace.
					client.MatchingLabels{
						placementv1beta1.PlacementTrackingLabel: crpName,
					},
				}
				Eventually(func() string {
					if err := hubClient.List(ctx, &works, listOpts...); err != nil {
						return err.Error()
					}
					for i := range works.Items {
						work := works.Items[i]
						wantConditions := []metav1.Condition{
							{
								Type:               placementv1beta1.WorkConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								Reason:             condition.WorkAllManifestsAppliedReason,
								ObservedGeneration: 1,
							},
							{
								Type:               placementv1beta1.WorkConditionTypeAvailable,
								Status:             metav1.ConditionTrue,
								Reason:             condition.WorkAllManifestsAvailableReason,
								ObservedGeneration: 1,
							},
						}
						diff := controller.CompareConditions(wantConditions, work.Status.Conditions)
						if len(diff) != 0 {
							return diff
						}
					}
					if len(works.Items) == 0 {
						return "no available work found"
					}
					return ""
				}, eventuallyDuration, eventuallyInterval).Should(BeEmpty(),
					"work condition mismatch for work %s (-want, +got):", memberCluster.ClusterName)
			}
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting envelope %s", testDeploymentEnvelope.Name))
			Expect(hubClient.Delete(ctx, &testDeploymentEnvelope)).To(Succeed(), "Failed to delete ResourceEnvelope")
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("Test a CRP place workload objects successfully, block rollout based on deployment availability", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier

		BeforeAll(func() {
			// Create the test resources.
			readDeploymentTestManifest(&testDeployment)
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Kind:    utils.NamespaceKind,
					Name:    workNamespace.Name,
					Version: corev1.SchemeGroupVersion.Version,
				},
				{
					Group:     appv1.SchemeGroupVersion.Group,
					Version:   appv1.SchemeGroupVersion.Version,
					Kind:      utils.DeploymentKind,
					Name:      testDeployment.Name,
					Namespace: workNamespace.Name,
				},
			}
		})

		It("create the deployment resource in the namespace", func() {
			Expect(hubClient.Create(ctx, &workNamespace)).To(Succeed(), "Failed to create namespace %s", workNamespace.Name)
			testDeployment.Namespace = workNamespace.Name
			Expect(hubClient.Create(ctx, &testDeployment)).To(Succeed(), "Failed to create test deployment %s", testDeployment.Name)
		})

		It("create the CRP that select the namespace", func() {
			crp := buildCRPForSafeRollout()
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(wantSelectedResources, allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := waitForDeploymentPlacementToReady(memberCluster, &testDeployment)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("change the image name in deployment, to make it unavailable", func() {
			Eventually(func() error {
				var dep appv1.Deployment
				err := hubClient.Get(ctx, types.NamespacedName{Name: testDeployment.Name, Namespace: testDeployment.Namespace}, &dep)
				if err != nil {
					return err
				}
				dep.Spec.Template.Spec.Containers[0].Image = randomImageName
				return hubClient.Update(ctx, &dep)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to change the image name in deployment")
		})

		It("should update CRP status as expected", func() {
			failedDeploymentResourceIdentifier := placementv1beta1.ResourceIdentifier{
				Group:     appv1.SchemeGroupVersion.Group,
				Version:   appv1.SchemeGroupVersion.Version,
				Kind:      utils.DeploymentKind,
				Name:      testDeployment.Name,
				Namespace: testDeployment.Namespace,
			}
			crpStatusActual := safeRolloutWorkloadCRPStatusUpdatedActual(wantSelectedResources, failedDeploymentResourceIdentifier, allMemberClusterNames, "1", 2)
			Eventually(crpStatusActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("Test a CRP place workload objects successfully, block rollout based on daemonset availability", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testDaemonSet appv1.DaemonSet
		var testDaemonSetEnvelope placementv1beta1.ResourceEnvelope

		BeforeAll(func() {
			// Create the test resources.
			readDaemonSetTestManifest(&testDaemonSet)
			readEnvelopeResourceTestManifest(&testDaemonSetEnvelope)
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Kind:    utils.NamespaceKind,
					Name:    workNamespace.Name,
					Version: corev1.SchemeGroupVersion.Version,
				},
				{
					Group:     placementv1beta1.GroupVersion.Group,
					Kind:      placementv1beta1.ResourceEnvelopeKind,
					Version:   placementv1beta1.GroupVersion.Version,
					Name:      testDaemonSetEnvelope.Name,
					Namespace: workNamespace.Name,
				},
			}
		})

		It("create the daemonset resource in the namespace", func() {
			createWrappedResourcesForRollout(&testDaemonSetEnvelope, &testDaemonSet, utils.DaemonSetKind, workNamespace)
		})

		It("create the CRP that select the namespace", func() {
			crp := buildCRPForSafeRollout()
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(types.NamespacedName{Name: crpName}, wantSelectedResources, allMemberClusterNames, nil, "0", true)
			Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := waitForDaemonSetPlacementToReady(memberCluster, &testDaemonSet)
				Eventually(workResourcesPlacedActual, eventuallyInterval, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("change the image name in daemonset, to make it unavailable", func() {
			Eventually(func() error {
				testDaemonSet.Spec.Template.Spec.Containers[0].Image = randomImageName
				daemonSetByte, err := json.Marshal(testDaemonSet)
				if err != nil {
					return nil
				}
				testDaemonSetEnvelope.Data["daemonset.yaml"] = runtime.RawExtension{Raw: daemonSetByte}
				return hubClient.Update(ctx, &testDaemonSetEnvelope)
			}, eventuallyInterval, eventuallyInterval).Should(Succeed(), "Failed to change the image name of daemonset in envelope object")
		})

		It("should update CRP status as expected", func() {
			failedDaemonSetResourceIdentifier := placementv1beta1.ResourceIdentifier{
				Group:     appv1.SchemeGroupVersion.Group,
				Version:   appv1.SchemeGroupVersion.Version,
				Kind:      utils.DaemonSetKind,
				Name:      testDaemonSet.Name,
				Namespace: testDaemonSet.Namespace,
				Envelope: &placementv1beta1.EnvelopeIdentifier{
					Name:      testDaemonSetEnvelope.Name,
					Namespace: testDaemonSetEnvelope.Namespace,
					Type:      placementv1beta1.ResourceEnvelopeType,
				},
			}
			crpStatusActual := safeRolloutWorkloadCRPStatusUpdatedActual(wantSelectedResources, failedDaemonSetResourceIdentifier, allMemberClusterNames, "1", 2)
			Eventually(crpStatusActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("Test a CRP place workload objects successfully, block rollout based on statefulset availability", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testStatefulSet appv1.StatefulSet
		var testStatefulSetEnvelope placementv1beta1.ResourceEnvelope

		BeforeAll(func() {
			// Create the test resources.
			readStatefulSetTestManifest(&testStatefulSet, StatefulSetBasic)
			readEnvelopeResourceTestManifest(&testStatefulSetEnvelope)
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Kind:    utils.NamespaceKind,
					Name:    workNamespace.Name,
					Version: corev1.SchemeGroupVersion.Version,
				},
				{
					Group:     placementv1beta1.GroupVersion.Group,
					Kind:      placementv1beta1.ResourceEnvelopeKind,
					Version:   placementv1beta1.GroupVersion.Version,
					Name:      testStatefulSetEnvelope.Name,
					Namespace: workNamespace.Name,
				},
			}
		})

		It("create the statefulset resource in the namespace", func() {
			createWrappedResourcesForRollout(&testStatefulSetEnvelope, &testStatefulSet, utils.StatefulSetKind, workNamespace)
		})

		It("create the CRP that select the namespace", func() {
			crp := buildCRPForSafeRollout()
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(types.NamespacedName{Name: crpName}, wantSelectedResources, allMemberClusterNames, nil, "0", true)
			Eventually(crpStatusUpdatedActual, 2*workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := waitForStatefulSetPlacementToReady(memberCluster, &testStatefulSet)
				Eventually(workResourcesPlacedActual, eventuallyInterval, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("change the image name in statefulset, to make it unavailable", func() {
			Eventually(func() error {
				testStatefulSet.Spec.Template.Spec.Containers[0].Image = randomImageName
				daemonSetByte, err := json.Marshal(testStatefulSet)
				if err != nil {
					return nil
				}
				testStatefulSetEnvelope.Data["statefulset.yaml"] = runtime.RawExtension{Raw: daemonSetByte}
				return hubClient.Update(ctx, &testStatefulSetEnvelope)
			}, eventuallyInterval, eventuallyInterval).Should(Succeed(), "Failed to change the image name in statefulset")
		})

		It("should update CRP status as expected", func() {
			failedStatefulSetResourceIdentifier := placementv1beta1.ResourceIdentifier{
				Group:     appv1.SchemeGroupVersion.Group,
				Version:   appv1.SchemeGroupVersion.Version,
				Kind:      utils.StatefulSetKind,
				Name:      testStatefulSet.Name,
				Namespace: testStatefulSet.Namespace,
				Envelope: &placementv1beta1.EnvelopeIdentifier{
					Name:      testStatefulSetEnvelope.Name,
					Namespace: testStatefulSetEnvelope.Namespace,
					Type:      placementv1beta1.ResourceEnvelopeType,
				},
			}
			crpStatusActual := safeRolloutWorkloadCRPStatusUpdatedActual(wantSelectedResources, failedStatefulSetResourceIdentifier, allMemberClusterNames, "1", 2)
			Eventually(crpStatusActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("Test a CRP place workload objects successfully, block rollout based on service availability", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testService corev1.Service

		BeforeAll(func() {
			// Create the test resources.
			readServiceTestManifest(&testService)
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Kind:    utils.NamespaceKind,
					Name:    workNamespace.Name,
					Version: corev1.SchemeGroupVersion.Version,
				},
				{
					Kind:      utils.ServiceKind,
					Name:      testService.Name,
					Version:   corev1.SchemeGroupVersion.Version,
					Namespace: workNamespace.Name,
				},
			}
		})

		It("create the service resource in the namespace", func() {
			Expect(hubClient.Create(ctx, &workNamespace)).To(Succeed(), "Failed to create namespace %s", workNamespace.Name)
			testService.Namespace = workNamespace.Name
			Expect(hubClient.Create(ctx, &testService)).To(Succeed(), "Failed to create test service %s", testService.Name)
		})

		It("create the CRP that select the namespace", func() {
			crp := buildCRPForSafeRollout()
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(wantSelectedResources, allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := waitForServiceToReady(memberCluster, &testService)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("change service to LoadBalancer, to make it unavailable", func() {
			Eventually(func() error {
				var service corev1.Service
				err := hubClient.Get(ctx, types.NamespacedName{Name: testService.Name, Namespace: testService.Namespace}, &service)
				if err != nil {
					return err
				}
				service.Spec.Type = corev1.ServiceTypeLoadBalancer
				return hubClient.Update(ctx, &service)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to change the service type to LoadBalancer")
		})

		It("should update CRP status as expected", func() {
			failedDeploymentResourceIdentifier := placementv1beta1.ResourceIdentifier{
				Group:     corev1.SchemeGroupVersion.Group,
				Version:   corev1.SchemeGroupVersion.Version,
				Kind:      utils.ServiceKind,
				Name:      testService.Name,
				Namespace: testService.Namespace,
			}
			// failedResourceObservedGeneration is set to 0 because generation is not populated for service.
			crpStatusActual := safeRolloutWorkloadCRPStatusUpdatedActual(wantSelectedResources, failedDeploymentResourceIdentifier, allMemberClusterNames, "1", 0)
			Eventually(crpStatusActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("Test a CRP place workload successful and update it to be failed and then delete the resource snapshot,"+
		"rollout should eventually be successful after we correct the image", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testDeployment appv1.Deployment

		BeforeAll(func() {
			// Create the test resources.
			readDeploymentTestManifest(&testDeployment)
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Kind:    utils.NamespaceKind,
					Name:    workNamespace.Name,
					Version: corev1.SchemeGroupVersion.Version,
				},
				{
					Group:     appv1.SchemeGroupVersion.Group,
					Version:   appv1.SchemeGroupVersion.Version,
					Kind:      utils.DeploymentKind,
					Name:      testDeployment.Name,
					Namespace: workNamespace.Name,
				},
			}
		})

		It("create the deployment resource in the namespace", func() {
			Expect(hubClient.Create(ctx, &workNamespace)).To(Succeed(), "Failed to create namespace %s", workNamespace.Name)
			testDeployment.Namespace = workNamespace.Name
			Expect(hubClient.Create(ctx, &testDeployment)).To(Succeed(), "Failed to create test deployment %s", testDeployment.Name)
		})

		It("create the CRP that select the namespace", func() {
			crp := buildCRPForSafeRollout()
			crp.Spec.RevisionHistoryLimit = ptr.To(int32(1))
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(wantSelectedResources, allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := waitForDeploymentPlacementToReady(memberCluster, &testDeployment)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("change the image name in deployment, to make it unavailable", func() {
			Eventually(func() error {
				var dep appv1.Deployment
				err := hubClient.Get(ctx, types.NamespacedName{Name: testDeployment.Name, Namespace: testDeployment.Namespace}, &dep)
				if err != nil {
					return err
				}
				dep.Spec.Template.Spec.Containers[0].Image = randomImageName
				return hubClient.Update(ctx, &dep)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to change the image name in deployment")
		})

		It("should update CRP status on deployment failed as expected", func() {
			failedDeploymentResourceIdentifier := placementv1beta1.ResourceIdentifier{
				Group:     appv1.SchemeGroupVersion.Group,
				Version:   appv1.SchemeGroupVersion.Version,
				Kind:      utils.DeploymentKind,
				Name:      testDeployment.Name,
				Namespace: testDeployment.Namespace,
			}
			crpStatusActual := safeRolloutWorkloadCRPStatusUpdatedActual(wantSelectedResources, failedDeploymentResourceIdentifier, allMemberClusterNames, "1", 2)
			Eventually(crpStatusActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("update work to trigger a work generator reconcile", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx].ClusterName
				namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster)
				workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, crpName)
				work := placementv1beta1.Work{}
				Expect(hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: namespaceName}, &work)).Should(Succeed(), "Failed to get the work")
				if work.Status.ManifestConditions != nil {
					work.Status.ManifestConditions = nil
				} else {
					meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{
						Type:   placementv1beta1.WorkConditionTypeAvailable,
						Status: metav1.ConditionFalse,
						Reason: "WorkNotAvailable",
					})
				}
				Expect(hubClient.Status().Update(ctx, &work)).Should(Succeed(), "Failed to update the work")
			}
		})

		It("change the image name in deployment, to roll over the resourcesnapshot", func() {
			crsList := &placementv1beta1.ClusterResourceSnapshotList{}
			Expect(hubClient.List(ctx, crsList, client.MatchingLabels{placementv1beta1.PlacementTrackingLabel: crpName})).Should(Succeed(), "Failed to list the resourcesnapshot")
			Expect(len(crsList.Items) == 1).Should(BeTrue())
			oldCRS := crsList.Items[0].Name
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: testDeployment.Name, Namespace: testDeployment.Namespace}, &testDeployment)).Should(Succeed(), "Failed to get deployment")
			testDeployment.Spec.Template.Spec.Containers[0].Image = "extra-snapshot"
			Expect(hubClient.Update(ctx, &testDeployment)).Should(Succeed(), "Failed to change the image name in deployment")
			// wait for the new resourcesnapshot to be created
			Eventually(func() bool {
				Expect(hubClient.List(ctx, crsList, client.MatchingLabels{placementv1beta1.PlacementTrackingLabel: crpName})).Should(Succeed(), "Failed to list the resourcesnapshot")
				Expect(len(crsList.Items) == 1).Should(BeTrue())
				return crsList.Items[0].Name != oldCRS
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove the old resourcensnapshot")
		})

		It("update work to trigger a work generator reconcile", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx].ClusterName
				namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster)
				workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, crpName)
				Eventually(func() error {
					work := placementv1beta1.Work{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: namespaceName}, &work); err != nil {
						return err
					}
					if work.Status.ManifestConditions != nil {
						work.Status.ManifestConditions = nil
					} else {
						meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{
							Type:   placementv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionFalse,
							Reason: "WorkNotAvailable",
						})
					}
					return hubClient.Status().Update(ctx, &work)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work")
			}
		})

		It("change the image name in deployment, to make it available again", func() {
			Eventually(func() error {
				err := hubClient.Get(ctx, types.NamespacedName{Name: testDeployment.Name, Namespace: testDeployment.Namespace}, &testDeployment)
				if err != nil {
					return err
				}
				testDeployment.Spec.Template.Spec.Containers[0].Image = "nginx:1.26.2"
				return hubClient.Update(ctx, &testDeployment)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to change the image name in deployment")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := waitForDeploymentPlacementToReady(memberCluster, &testDeployment)
				Eventually(workResourcesPlacedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("Test a CRP place workload objects successfully, don't block rollout based on job availability", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testJob batchv1.Job
		unAvailablePeriodSeconds := 15
		BeforeAll(func() {
			// Create the test resources.
			readJobTestManifest(&testJob)
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Kind:    utils.NamespaceKind,
					Name:    workNamespace.Name,
					Version: corev1.SchemeGroupVersion.Version,
				},
				{
					Group:     batchv1.SchemeGroupVersion.Group,
					Version:   batchv1.SchemeGroupVersion.Version,
					Kind:      utils.JobKind,
					Name:      testJob.Name,
					Namespace: workNamespace.Name,
				},
			}
		})

		It("create the job resource in the namespace", func() {
			Expect(hubClient.Create(ctx, &workNamespace)).To(Succeed(), "Failed to create namespace %s", workNamespace.Name)
			testJob.Namespace = workNamespace.Name
			Expect(hubClient.Create(ctx, &testJob)).To(Succeed(), "Failed to create test job %s", testJob.Name)
		})

		It("create the CRP that select the namespace", func() {
			crp := buildCRPForSafeRollout()
			// the job we are trying to propagate takes 10s to complete. MaxUnavailable is set to 1. So setting UnavailablePeriodSeconds to 15s
			// so that after each rollout phase we only wait for 15s before proceeding to the next since Job is not trackable,
			// we want rollout to finish in a reasonable time.
			crp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds = ptr.To(unAvailablePeriodSeconds)
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(types.NamespacedName{Name: crpName}, wantSelectedResources, allMemberClusterNames, nil, "0", false)
			Eventually(crpStatusUpdatedActual, 2*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := waitForJobToBePlaced(memberCluster, &testJob)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("suspend job", func() {
			Eventually(func() error {
				var job batchv1.Job
				err := hubClient.Get(ctx, types.NamespacedName{Name: testJob.Name, Namespace: testJob.Namespace}, &job)
				if err != nil {
					return err
				}
				job.Spec.Suspend = ptr.To(true)
				return hubClient.Update(ctx, &job)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to suspend job")
		})
		// job is not trackable, so we need to wait for a bit longer for each roll out
		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(types.NamespacedName{Name: crpName}, wantSelectedResources, allMemberClusterNames, nil, "1", false)
			Eventually(crpStatusUpdatedActual, 5*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("Test a CRP place custom resource successfully, should wait to update resource", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testCustomResource testv1alpha1.TestResource
		var crp *placementv1beta1.ClusterResourcePlacement

		var observedResourceIdx string

		unAvailablePeriodSeconds := 30
		BeforeAll(func() {
			// Create the test resources.
			readTestCustomResource(&testCustomResource)
			testCustomResource.Namespace = workNamespace.Name
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Kind:    utils.NamespaceKind,
					Name:    workNamespace.Name,
					Version: corev1.SchemeGroupVersion.Version,
				},
				{
					Group:     testv1alpha1.GroupVersion.Group,
					Kind:      testCustomResource.Kind,
					Name:      testCustomResource.Name,
					Version:   testv1alpha1.GroupVersion.Version,
					Namespace: workNamespace.Name,
				},
				{
					Group:   utils.CRDMetaGVK.Group,
					Kind:    utils.CRDMetaGVK.Kind,
					Name:    testResourceCRDName,
					Version: utils.CRDMetaGVK.Version,
				},
			}
		})

		It("create the resources", func() {
			Expect(hubClient.Create(ctx, &workNamespace)).To(Succeed(), "Failed to create namespace %s", workNamespace.Name)
			Expect(hubClient.Create(ctx, &testCustomResource)).To(Succeed(), "Failed to create test custom resource %s", testCustomResource.GetName())
		})

		It("create the CRP that select the namespace and CRD", func() {
			crp = buildCRPForSafeRollout()
			crdClusterResourceSelector := placementv1beta1.ResourceSelectorTerm{
				Group:   utils.CRDMetaGVK.Group,
				Kind:    utils.CRDMetaGVK.Kind,
				Version: utils.CRDMetaGVK.Version,
				Name:    testResourceCRDName,
			}
			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors, crdClusterResourceSelector)
			crp.Spec.Policy = &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				ClusterNames: []string{
					memberCluster1EastProdName,
				},
			}
			crp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds = ptr.To(unAvailablePeriodSeconds)
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			// Wait until all the expected resources have been selected.
			//
			// This is to address a flakiness situation where it might take a while for Fleet
			// to recognize the custom resource (even if it is created before the CRP).
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return fmt.Errorf("failed to get CRP: %w", err)
				}

				if diff := cmp.Diff(crp.Status.SelectedResources, wantSelectedResources, cmpopts.SortSlices(utils.LessFuncResourceIdentifier)); diff != "" {
					return fmt.Errorf("selected resources mismatched (-got, +want): %s", diff)
				}
				// Use the fresh observed resource index to verify the CRP status later.
				observedResourceIdx = crp.Status.ObservedResourceIndex
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to select all the expected resources")

			crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(types.NamespacedName{Name: crpName}, wantSelectedResources, []string{memberCluster1EastProdName}, nil, observedResourceIdx, false)
			Eventually(crpStatusUpdatedActual, 2*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on member cluster", func() {
			workResourcesPlacedActual := waitForTestResourceToBePlaced(memberCluster1EastProd, &testCustomResource)
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProd.ClusterName)
		})

		It("update the custom resource", func() {
			Eventually(func() error {
				var cr testv1alpha1.TestResource
				err := hubClient.Get(ctx, types.NamespacedName{Name: testCustomResource.Name, Namespace: workNamespace.Name}, &cr)
				if err != nil {
					return err
				}
				cr.Spec.Foo = "bar1" // Previously was "foo1"
				return hubClient.Update(ctx, &cr)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update custom resource")
		})

		It("should not update the resource on member cluster before the unavailable second", func() {
			// subtracting 5 seconds because transition between IT takes ~1 second
			unavailablePeriod := time.Duration(*crp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds)*time.Second - (5 * time.Second)
			Consistently(func() bool {
				var cr testv1alpha1.TestResource
				err := memberCluster1EastProd.KubeClient.Get(ctx, types.NamespacedName{Name: testCustomResource.Name, Namespace: workNamespace.Name}, &cr)
				if err != nil {
					klog.Errorf("Failed to get custom resource %s/%s: %v", workNamespace.Name, testCustomResource.Name, err)
					return false
				}
				if cr.Spec.Foo == "foo1" { // Previously was "foo1"
					return true
				}
				return false
			}, unavailablePeriod, consistentlyInterval).Should(BeTrue(), "Test resource was updated when it shouldn't be")
		})

		It("should update CRP status as expected", func() {
			// Refresh the observed resource index.
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return fmt.Errorf("failed to get CRP: %w", err)
				}

				if crp.Status.ObservedResourceIndex == observedResourceIdx {
					// It is expected that the observed resource index has been bumped by 1
					// due to the resource change.
					return fmt.Errorf("observed resource index is not updated")
				}
				// Use the fresh observed resource index to verify the CRP status later.
				observedResourceIdx = crp.Status.ObservedResourceIndex
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to select all the expected resources")

			crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(types.NamespacedName{Name: crpName}, wantSelectedResources, []string{memberCluster1EastProdName}, nil, observedResourceIdx, false)
			Eventually(crpStatusUpdatedActual, 4*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{memberCluster1EastProd})
		})
	})

	Context("Test a CRP place custom resource successfully, should wait to update resource on multiple member clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testCustomResource testv1alpha1.TestResource
		var crp *placementv1beta1.ClusterResourcePlacement
		unAvailablePeriodSeconds := 30

		BeforeAll(func() {
			// Create the test resources.
			readTestCustomResource(&testCustomResource)
			testCustomResource.Namespace = workNamespace.Name
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Kind:    utils.NamespaceKind,
					Name:    workNamespace.Name,
					Version: corev1.SchemeGroupVersion.Version,
				},
				{
					Group:     testv1alpha1.GroupVersion.Group,
					Kind:      testCustomResource.Kind,
					Name:      testCustomResource.Name,
					Version:   testv1alpha1.GroupVersion.Version,
					Namespace: workNamespace.Name,
				},
				{
					Group:   utils.CRDMetaGVK.Group,
					Kind:    utils.CRDMetaGVK.Kind,
					Name:    testResourceCRDName,
					Version: utils.CRDMetaGVK.Version,
				},
			}
		})

		It("create the resources", func() {
			Expect(hubClient.Create(ctx, &workNamespace)).To(Succeed(), "Failed to create namespace %s", workNamespace.Name)
			Expect(hubClient.Create(ctx, &testCustomResource)).To(Succeed(), "Failed to create test custom resource %s", testCustomResource.GetName())
		})

		It("create the CRP that select the namespace and CRD", func() {
			crp = buildCRPForSafeRollout()
			crdClusterResourceSelector := placementv1beta1.ResourceSelectorTerm{
				Group:   utils.CRDMetaGVK.Group,
				Kind:    utils.CRDMetaGVK.Kind,
				Version: utils.CRDMetaGVK.Version,
				Name:    testResourceCRDName,
			}
			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors, crdClusterResourceSelector)
			crp.Spec.Policy = &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			}
			crp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds = ptr.To(unAvailablePeriodSeconds)
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(types.NamespacedName{Name: crpName}, wantSelectedResources, allMemberClusterNames, nil, "0", false)
			Eventually(crpStatusUpdatedActual, 2*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := waitForTestResourceToBePlaced(memberCluster, &testCustomResource)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("update the custom resource", func() {
			Eventually(func() error {
				var cr testv1alpha1.TestResource
				err := hubClient.Get(ctx, types.NamespacedName{Name: testCustomResource.Name, Namespace: workNamespace.Name}, &cr)
				if err != nil {
					return err
				}
				cr.Spec.Foo = "bar1" // Previously was "foo1"
				return hubClient.Update(ctx, &cr)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update custom resource")
		})

		It("should update one member cluster", func() {
			// adding a buffer of 5 seconds
			unavailablePeriod := time.Duration(*crp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds)*time.Second + (5 * time.Second)
			Eventually(func() bool {
				// Check the number of clusters meeting the condition
				countClustersMeetingCondition := func() int {
					count := 0
					for _, cluster := range allMemberClusters {
						if !checkCluster(cluster, testCustomResource.Name, workNamespace.Name) {
							// resource field updated to "bar1"
							count++
						}
					}
					return count
				}
				return countClustersMeetingCondition() == 1
			}, unavailablePeriod, eventuallyInterval).Should(BeTrue(), "Test resource was updated when it shouldn't be")
		})

		It("should not rollout update to the next member cluster before unavailable second", func() {
			// subtracting a buffer of 5 seconds
			unavailablePeriod := time.Duration(*crp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds)*time.Second - (5 * time.Second)
			Consistently(func() bool {
				// Check the number of clusters meeting the condition
				countClustersMeetingCondition := func() int {
					count := 0
					for _, cluster := range allMemberClusters {
						if !checkCluster(cluster, testCustomResource.Name, workNamespace.Name) {
							// resource field updated to "bar1"
							count++
						}
					}
					return count
				}
				return countClustersMeetingCondition() == 1
			}, unavailablePeriod, consistentlyInterval).Should(BeTrue(), "Test resource was updated when it shouldn't be")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(types.NamespacedName{Name: crpName}, wantSelectedResources, allMemberClusterNames, nil, "1", false)
			Eventually(crpStatusUpdatedActual, 4*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})
})

// createWrappedResourcesForRollout creates an enveloped resource on the hub cluster with a workload object for testing purposes.
func createWrappedResourcesForRollout(testEnvelopeObj *placementv1beta1.ResourceEnvelope, obj metav1.Object, kind string, namespace corev1.Namespace) {
	Expect(hubClient.Create(ctx, &namespace)).To(Succeed(), "Failed to create namespace %s", namespace.Name)
	testEnvelopeObj.Data = make(map[string]runtime.RawExtension)
	constructWrappedResources(testEnvelopeObj, obj, kind, namespace)
	Expect(hubClient.Create(ctx, testEnvelopeObj)).To(Succeed(), "Failed to create testEnvelope object %s containing %s", testEnvelopeObj.Name, kind)
}

func checkCluster(cluster *framework.Cluster, name, namespace string) bool {
	var cr testv1alpha1.TestResource
	err := cluster.KubeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &cr)
	if err != nil {
		klog.Errorf("Failed to get custom resource %s/%s: %v", namespace, name, err)
		return false
	}
	return cr.Spec.Foo == "foo1" // Check that the resource's field is as expected
}

func waitForDeploymentPlacementToReady(memberCluster *framework.Cluster, testDeployment *appv1.Deployment) func() error {
	return func() error {
		if err := validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: testDeployment.Namespace}); err != nil {
			return err
		}
		By("check the placedDeployment")
		placedDeployment := &appv1.Deployment{}
		if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{Namespace: testDeployment.Namespace, Name: testDeployment.Name}, placedDeployment); err != nil {
			return err
		}
		By("check the placedDeployment is ready")
		var depCond *appv1.DeploymentCondition
		for i := range placedDeployment.Status.Conditions {
			if placedDeployment.Status.Conditions[i].Type == appv1.DeploymentAvailable {
				depCond = &placedDeployment.Status.Conditions[i]
				break
			}
		}
		if placedDeployment.Status.ObservedGeneration == placedDeployment.Generation && depCond != nil && depCond.Status == corev1.ConditionTrue {
			if placedDeployment.Spec.Template.Spec.Containers[0].Image != testDeployment.Spec.Template.Spec.Containers[0].Image {
				return fmt.Errorf("deployment spec`%s` is not updated, placedDeployment = %+v, testDeployment = %+v", testDeployment.Name, placedDeployment.Spec, testDeployment.Spec)
			}
			return nil
		}
		return fmt.Errorf("deployment `%s` is not updated", testDeployment.Name)
	}
}

func waitForDaemonSetPlacementToReady(memberCluster *framework.Cluster, testDaemonSet *appv1.DaemonSet) func() error {
	return func() error {
		if err := validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: testDaemonSet.Namespace}); err != nil {
			return err
		}
		By("check the placedDaemonSet")
		placedDaemonSet := &appv1.DaemonSet{}
		if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{Namespace: testDaemonSet.Namespace, Name: testDaemonSet.Name}, placedDaemonSet); err != nil {
			return err
		}
		By("check the placedDaemonSet is ready")
		if placedDaemonSet.Status.ObservedGeneration == placedDaemonSet.Generation &&
			placedDaemonSet.Status.NumberAvailable == placedDaemonSet.Status.DesiredNumberScheduled &&
			placedDaemonSet.Status.CurrentNumberScheduled == placedDaemonSet.Status.UpdatedNumberScheduled {
			if placedDaemonSet.Spec.Template.Spec.Containers[0].Image != testDaemonSet.Spec.Template.Spec.Containers[0].Image {
				return fmt.Errorf("daemonSet spec`%s` is not updated", testDaemonSet.Name)
			}
			return nil
		}
		return errors.New("daemonset is not ready")
	}
}

func waitForStatefulSetPlacementToReady(memberCluster *framework.Cluster, testStatefulSet *appv1.StatefulSet) func() error {
	return func() error {
		if err := validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: testStatefulSet.Namespace}); err != nil {
			return err
		}
		By("check the placedStatefulSet")
		placedStatefulSet := &appv1.StatefulSet{}
		if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{Namespace: testStatefulSet.Namespace, Name: testStatefulSet.Name}, placedStatefulSet); err != nil {
			return err
		}
		By("check the placedStatefulSet is ready")
		if placedStatefulSet.Status.ObservedGeneration == placedStatefulSet.Generation &&
			placedStatefulSet.Status.CurrentReplicas == *placedStatefulSet.Spec.Replicas &&
			placedStatefulSet.Status.CurrentReplicas == placedStatefulSet.Status.UpdatedReplicas {
			if placedStatefulSet.Spec.Template.Spec.Containers[0].Image != testStatefulSet.Spec.Template.Spec.Containers[0].Image {
				return fmt.Errorf("statefulSet spec`%s` is not updated", placedStatefulSet.Name)
			}
			return nil
		}
		return errors.New("statefulset is not ready")
	}
}

func waitForServiceToReady(memberCluster *framework.Cluster, testService *corev1.Service) func() error {
	return func() error {
		if err := validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: testService.Namespace}); err != nil {
			return err
		}
		By("check the placedService")
		placedService := &corev1.Service{}
		if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{Namespace: testService.Namespace, Name: testService.Name}, placedService); err != nil {
			return err
		}
		By("check the placedService is ready")
		if placedService.Spec.ClusterIP != "" {
			return nil
		}
		return errors.New("service is not ready")
	}
}

func waitForJobToBePlaced(memberCluster *framework.Cluster, testJob *batchv1.Job) func() error {
	return func() error {
		if err := validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: testJob.Namespace}); err != nil {
			return err
		}
		By("check the placedJob")
		return memberCluster.KubeClient.Get(ctx, types.NamespacedName{Namespace: testJob.Namespace, Name: testJob.Name}, &batchv1.Job{})
	}
}

func waitForTestResourceToBePlaced(memberCluster *framework.Cluster, testResource *testv1alpha1.TestResource) func() error {
	return func() error {
		if err := validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: testResource.Namespace}); err != nil {
			return err
		}
		By("check the placedTestResource")
		return memberCluster.KubeClient.Get(ctx, types.NamespacedName{Namespace: testResource.Namespace, Name: testResource.Name}, &testv1alpha1.TestResource{})
	}
}

func waitForCRDToBeReady(crdName string) {
	Eventually(func() error { // wait for CRD to be created
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crdName}, crd); err != nil {
			return err
		}
		if crd.Status.Conditions == nil {
			return fmt.Errorf("CRD status conditions are nil for %s", crdName)
		}

		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status != apiextensionsv1.ConditionTrue {
				return fmt.Errorf("CRD is not established: %s", crdName)
			}
			if cond.Type == apiextensionsv1.NamesAccepted && cond.Status != apiextensionsv1.ConditionTrue {
				return fmt.Errorf("CRD names are not accepted: %s", crdName)
			}
		}

		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "CRD failed to be ready %s", crdName)
}

func buildCRPForSafeRollout() *placementv1beta1.ClusterResourcePlacement {
	return &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess()),
			// Add a custom finalizer; this would allow us to better observe
			// the behavior of the controllers.
			Finalizers: []string{customDeletionBlockerFinalizer},
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: workResourceSelector(),
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
		},
	}
}
