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
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	"github.com/kubefleet-dev/kubefleet/test/utils/controller"
)

const (
	valFoo1 = "foo1"
	valBar1 = "bar1"
)

var (
	testDaemonSet      appv1.DaemonSet
	testStatefulSet    appv1.StatefulSet
	testService        corev1.Service
	testJob            batchv1.Job
	testCustomResource testv1alpha1.TestResource
)

var _ = Describe("placing namespaced scoped resources using a RP with rollout", Label("resourceplacement"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	rpKey := types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}

	BeforeEach(OncePerOrdered, func() {
		testDeployment = appv1.Deployment{}
		readDeploymentTestManifest(&testDeployment)
		testDaemonSet = appv1.DaemonSet{}
		readDaemonSetTestManifest(&testDaemonSet)
		testStatefulSet = appv1.StatefulSet{}
		readStatefulSetTestManifest(&testStatefulSet, StatefulSetBasic)
		testService = corev1.Service{}
		readServiceTestManifest(&testService)
		testJob = batchv1.Job{}
		readJobTestManifest(&testJob)

		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: namespaceOnlySelector(),
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

		crpStatusUpdatedActual := crpStatusUpdatedActual(nil, allMemberClusterNames, nil, "0") // nil as no resources created yet
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		// Remove the custom deletion blocker finalizer from the RP and CRP.
		ensureRPAndRelatedResourcesDeleted(rpKey, allMemberClusters, &testDeployment, &testDaemonSet, &testStatefulSet, &testService, &testJob)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("Test an RP place enveloped objects successfully", Ordered, func() {
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testDeploymentEnvelope placementv1beta1.ResourceEnvelope

		BeforeAll(func() {
			readEnvelopeResourceTestManifest(&testDeploymentEnvelope)
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
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

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("Create the RP that select the enveloped objects", func() {
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  workNamespace.Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   placementv1beta1.GroupVersion.Group,
							Kind:    placementv1beta1.ResourceEnvelopeKind,
							Version: placementv1beta1.GroupVersion.Version,
							Name:    testDeploymentEnvelope.Name,
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(wantSelectedResources, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
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
						placementv1beta1.PlacementTrackingLabel: rpName,
						placementv1beta1.ParentNamespaceLabel:   workNamespace.Name,
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
	})

	Context("Test an RP place workload objects successfully, block rollout based on deployment availability", Ordered, func() {
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier

		BeforeAll(func() {
			// Create the test resources.
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
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

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("create the RP that select the deployment", func() {
			rp := buildRPForSafeRollout(workNamespace.Name)
			rp.Spec.ResourceSelectors = []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   appv1.SchemeGroupVersion.Group,
					Kind:    utils.DeploymentKind,
					Version: appv1.SchemeGroupVersion.Version,
					Name:    testDeployment.Name,
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(wantSelectedResources, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
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

		It("should update RP status as expected", func() {
			failedDeploymentResourceIdentifier := placementv1beta1.ResourceIdentifier{
				Group:     appv1.SchemeGroupVersion.Group,
				Version:   appv1.SchemeGroupVersion.Version,
				Kind:      utils.DeploymentKind,
				Name:      testDeployment.Name,
				Namespace: testDeployment.Namespace,
			}
			rpStatusActual := safeRolloutWorkloadRPStatusUpdatedActual(wantSelectedResources, failedDeploymentResourceIdentifier, allMemberClusterNames, "1", 2)
			Eventually(rpStatusActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})
	})

	Context("Test an RP place workload objects successfully, block rollout based on daemonset availability", Ordered, func() {
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testDaemonSetEnvelope placementv1beta1.ResourceEnvelope

		BeforeAll(func() {
			// Create the test resources.
			readEnvelopeResourceTestManifest(&testDaemonSetEnvelope)
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
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

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("create the RP that select the enveloped daemonset", func() {
			rp := buildRPForSafeRollout(workNamespace.Name)
			rp.Spec.ResourceSelectors = []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   placementv1beta1.GroupVersion.Group,
					Kind:    placementv1beta1.ResourceEnvelopeKind,
					Version: placementv1beta1.GroupVersion.Version,
					Name:    testDaemonSetEnvelope.Name,
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(wantSelectedResources, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := waitForDaemonSetPlacementToReady(memberCluster, &testDaemonSet)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
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
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to change the image name of daemonset in envelope object")
		})

		It("should update RP status as expected", func() {
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
			rpStatusActual := safeRolloutWorkloadRPStatusUpdatedActual(wantSelectedResources, failedDaemonSetResourceIdentifier, allMemberClusterNames, "1", 2)
			Eventually(rpStatusActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})
	})

	Context("Test an RP place workload objects successfully, block rollout based on statefulset availability", Ordered, func() {
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testStatefulSetEnvelope placementv1beta1.ResourceEnvelope

		BeforeAll(func() {
			// Create the test resources.
			readEnvelopeResourceTestManifest(&testStatefulSetEnvelope)
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
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

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("create the RP that select the enveloped statefulset", func() {
			rp := buildRPForSafeRollout(workNamespace.Name)
			rp.Spec.ResourceSelectors = []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   placementv1beta1.GroupVersion.Group,
					Kind:    placementv1beta1.ResourceEnvelopeKind,
					Version: placementv1beta1.GroupVersion.Version,
					Name:    testStatefulSetEnvelope.Name,
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(wantSelectedResources, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, 2*workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := waitForStatefulSetPlacementToReady(memberCluster, &testStatefulSet)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("change the image name in statefulset, to make it unavailable", func() {
			Eventually(func() error {
				testStatefulSet.Spec.Template.Spec.Containers[0].Image = randomImageName
				statefulSetByte, err := json.Marshal(testStatefulSet)
				if err != nil {
					return nil
				}
				testStatefulSetEnvelope.Data["statefulset.yaml"] = runtime.RawExtension{Raw: statefulSetByte}
				return hubClient.Update(ctx, &testStatefulSetEnvelope)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to change the image name in statefulset")
		})

		It("should update RP status as expected", func() {
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
			rpStatusActual := safeRolloutWorkloadRPStatusUpdatedActual(wantSelectedResources, failedStatefulSetResourceIdentifier, allMemberClusterNames, "1", 2)
			Eventually(rpStatusActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})
	})

	Context("Test an RP place workload objects successfully, block rollout based on service availability", Ordered, func() {
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier

		BeforeAll(func() {
			// Create the test resources.
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
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

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("create the RP that select the service", func() {
			rp := buildRPForSafeRollout(workNamespace.Name)
			rp.Spec.ResourceSelectors = []placementv1beta1.ResourceSelectorTerm{
				{
					Kind:    utils.ServiceKind,
					Version: corev1.SchemeGroupVersion.Version,
					Name:    testService.Name,
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(wantSelectedResources, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
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

		It("should update RP status as expected", func() {
			failedServiceResourceIdentifier := placementv1beta1.ResourceIdentifier{
				Group:     corev1.SchemeGroupVersion.Group,
				Version:   corev1.SchemeGroupVersion.Version,
				Kind:      utils.ServiceKind,
				Name:      testService.Name,
				Namespace: testService.Namespace,
			}
			// failedResourceObservedGeneration is set to 0 because generation is not populated for service.
			rpStatusActual := safeRolloutWorkloadRPStatusUpdatedActual(wantSelectedResources, failedServiceResourceIdentifier, allMemberClusterNames, "1", 0)
			Eventually(rpStatusActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})
	})

	Context("Test an RP place workload successful and update it to be failed and then delete the resource snapshot,"+
		"rollout should eventually be successful after we correct the image", Ordered, func() {
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier

		BeforeAll(func() {
			// Create the test resources.
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
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

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("create the RP that select the deployment", func() {
			rp := buildRPForSafeRollout(workNamespace.Name)
			rp.Spec.RevisionHistoryLimit = ptr.To(int32(1))
			rp.Spec.ResourceSelectors = []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   appv1.SchemeGroupVersion.Group,
					Kind:    utils.DeploymentKind,
					Version: appv1.SchemeGroupVersion.Version,
					Name:    testDeployment.Name,
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(wantSelectedResources, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
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

		It("should update RP status on deployment failed as expected", func() {
			failedDeploymentResourceIdentifier := placementv1beta1.ResourceIdentifier{
				Group:     appv1.SchemeGroupVersion.Group,
				Version:   appv1.SchemeGroupVersion.Version,
				Kind:      utils.DeploymentKind,
				Name:      testDeployment.Name,
				Namespace: testDeployment.Namespace,
			}
			rpStatusActual := safeRolloutWorkloadRPStatusUpdatedActual(wantSelectedResources, failedDeploymentResourceIdentifier, allMemberClusterNames, "1", 2)
			Eventually(rpStatusActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("update work to trigger a work generator reconcile", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx].ClusterName
				namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster)
				workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, fmt.Sprintf(placementv1beta1.WorkNameBaseFmt, workNamespace.Name, rpName))
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
			rsList := &placementv1beta1.ResourceSnapshotList{}
			listOptions := &client.ListOptions{
				LabelSelector: labels.SelectorFromSet(labels.Set{placementv1beta1.PlacementTrackingLabel: rpName}),
				Namespace:     workNamespace.Name,
			}
			Expect(hubClient.List(ctx, rsList, listOptions)).Should(Succeed(), "Failed to list the resourcesnapshot")
			Expect(len(rsList.Items) == 1).Should(BeTrue())
			oldRS := rsList.Items[0].Name
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: testDeployment.Name, Namespace: testDeployment.Namespace}, &testDeployment)).Should(Succeed(), "Failed to get deployment")
			testDeployment.Spec.Template.Spec.Containers[0].Image = "extra-snapshot"
			Expect(hubClient.Update(ctx, &testDeployment)).Should(Succeed(), "Failed to change the image name in deployment")
			// wait for the new resourcesnapshot to be created
			Eventually(func() bool {
				Expect(hubClient.List(ctx, rsList, listOptions)).Should(Succeed(), "Failed to list the resourcesnapshot")
				Expect(len(rsList.Items) == 1).Should(BeTrue())
				return rsList.Items[0].Name != oldRS
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove the old resourcesnapshot")
		})

		It("update work to trigger a work generator reconcile", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx].ClusterName
				namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster)
				workName := fmt.Sprintf(placementv1beta1.FirstWorkNameFmt, fmt.Sprintf(placementv1beta1.WorkNameBaseFmt, workNamespace.Name, rpName))
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
	})

	Context("Test an RP place workload objects successfully, don't block rollout based on job availability", Ordered, func() {
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		unAvailablePeriodSeconds := 15

		BeforeAll(func() {
			// Create the test resources.
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
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

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("create the RP that select the job", func() {
			rp := buildRPForSafeRollout(workNamespace.Name)
			// the job we are trying to propagate takes 10s to complete. MaxUnavailable is set to 1. So setting UnavailablePeriodSeconds to 15s
			// so that after each rollout phase we only wait for 15s before proceeding to the next since Job is not trackable,
			// we want rollout to finish in a reasonable time.
			rp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds = ptr.To(unAvailablePeriodSeconds)
			rp.Spec.ResourceSelectors = []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   batchv1.SchemeGroupVersion.Group,
					Kind:    utils.JobKind,
					Version: batchv1.SchemeGroupVersion.Version,
					Name:    testJob.Name,
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(rpKey, wantSelectedResources, allMemberClusterNames, nil, "0", false)
			Eventually(rpStatusUpdatedActual, 2*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
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
		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(rpKey, wantSelectedResources, allMemberClusterNames, nil, "1", false)
			Eventually(rpStatusUpdatedActual, 5*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})
	})
})

// These 2 testcases need to run in ordered because they are going to place the same CRD,
// and if they in parallel, a resource conflict may occur.
var _ = Describe("placing namespaced custom resources using a RP with rollout", Label("resourceplacement"), Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	rpKey := types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}
	testCustomResourceKind := ""

	BeforeEach(OncePerOrdered, func() {
		testCustomResource = testv1alpha1.TestResource{}
		readTestCustomResource(&testCustomResource)
		// I need to initialize the kind here because the returned obj after creating has Kind field emptied.
		testCustomResourceKind = testCustomResource.Kind

		// Create the test resources, the CRD is already installed in BeforeSuite.
		workNamespace := appNamespace()
		Expect(hubClient.Create(ctx, &workNamespace)).To(Succeed(), "Failed to create namespace %s", workNamespace.Name)
		testCustomResource.Namespace = workNamespace.Name
		// Create the custom resource at the very beginning because our resource detect runs every 30s to detect new resources,
		// thus giving it some grace period.
		Expect(hubClient.Create(ctx, &testCustomResource)).To(Succeed(), "Failed to create test custom resource %s", testCustomResource.GetName())

		// Create a namespace-only CRP that selects both namespace and CRD for custom resource placement
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Group:          "",
						Kind:           utils.NamespaceKind,
						Version:        corev1.SchemeGroupVersion.Version,
						Name:           appNamespace().Name,
						SelectionScope: placementv1beta1.NamespaceOnly,
					},
					{
						Group:   utils.CRDMetaGVK.Group,
						Kind:    utils.CRDMetaGVK.Kind,
						Version: utils.CRDMetaGVK.Version,
						Name:    testResourceCRDName,
					},
				},
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

		crpStatusUpdatedActual := crpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{
			{
				Kind:    utils.NamespaceKind,
				Name:    appNamespace().Name,
				Version: corev1.SchemeGroupVersion.Version,
			},
			{
				Group:   utils.CRDMetaGVK.Group,
				Kind:    utils.CRDMetaGVK.Kind,
				Name:    testResourceCRDName,
				Version: utils.CRDMetaGVK.Version,
			},
		}, allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		// Remove the custom deletion blocker finalizer from the RP and CRP.
		ensureRPAndRelatedResourcesDeleted(rpKey, allMemberClusters, &testCustomResource)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("Test an RP place custom resource successfully, should wait to update resource", Ordered, func() {
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var rp *placementv1beta1.ResourcePlacement
		var observedResourceIdx string
		unAvailablePeriodSeconds := 30
		workNamespace := appNamespace()

		BeforeAll(func() {
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Group:     testv1alpha1.GroupVersion.Group,
					Kind:      testCustomResourceKind,
					Name:      testCustomResource.Name,
					Version:   testv1alpha1.GroupVersion.Version,
					Namespace: workNamespace.Name,
				},
			}
		})

		It("create the RP that select the custom resource", func() {
			rp = &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  workNamespace.Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   testv1alpha1.GroupVersion.Group,
							Kind:    testCustomResourceKind,
							Version: testv1alpha1.GroupVersion.Version,
							Name:    testCustomResource.Name,
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames: []string{
							memberCluster1EastProdName,
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							MaxUnavailable: &intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 1,
							},
							UnavailablePeriodSeconds: ptr.To(unAvailablePeriodSeconds),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			// Wait until all the expected resources have been selected.
			//
			// This is to address a flakiness situation where it might take a while for Fleet
			// to recognize the custom resource (even if it is created before the RP).
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, rpKey, rp); err != nil {
					return fmt.Errorf("failed to get RP: %w", err)
				}

				if diff := cmp.Diff(rp.Status.SelectedResources, wantSelectedResources, cmpopts.SortSlices(utils.LessFuncResourceIdentifier)); diff != "" {
					return fmt.Errorf("selected resources mismatched (-got, +want): %s", diff)
				}
				// Use the fresh observed resource index to verify the RP status later.
				observedResourceIdx = rp.Status.ObservedResourceIndex
				return nil
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to select all the expected resources")

			rpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(rpKey, wantSelectedResources, []string{memberCluster1EastProdName}, nil, observedResourceIdx, false)
			Eventually(rpStatusUpdatedActual, 2*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
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
				cr.Spec.Foo = valBar1 // Previously was "foo1"
				return hubClient.Update(ctx, &cr)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update custom resource")
		})

		It("should not update the resource on member cluster before the unavailable second", func() {
			// subtracting 5 seconds because transition between IT takes ~1 second
			unavailablePeriod := time.Duration(*rp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds)*time.Second - (5 * time.Second)
			Consistently(func() bool {
				var cr testv1alpha1.TestResource
				err := memberCluster1EastProd.KubeClient.Get(ctx, types.NamespacedName{Name: testCustomResource.Name, Namespace: workNamespace.Name}, &cr)
				if err != nil {
					klog.Errorf("Failed to get custom resource %s/%s: %v", workNamespace.Name, testCustomResource.Name, err)
					return false
				}
				if cr.Spec.Foo == valFoo1 { // Previously was "foo1"
					return true
				}
				return false
			}, unavailablePeriod, consistentlyInterval).Should(BeTrue(), "Test resource was updated when it shouldn't be")
		})

		It("should update RP status as expected", func() {
			// Refresh the observed resource index.
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, rpKey, rp); err != nil {
					return fmt.Errorf("failed to get RP: %w", err)
				}

				if rp.Status.ObservedResourceIndex == observedResourceIdx {
					// It is expected that the observed resource index has been bumped by 1
					// due to the resource change.
					return fmt.Errorf("observed resource index is not updated")
				}
				// Use the fresh observed resource index to verify the RP status later.
				observedResourceIdx = rp.Status.ObservedResourceIndex
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to select all the expected resources")

			rpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(rpKey, wantSelectedResources, []string{memberCluster1EastProdName}, nil, observedResourceIdx, false)
			Eventually(rpStatusUpdatedActual, 4*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("delete the RP and related resources", func() {
		})
	})

	Context("Test an RP place custom resource successfully, should wait to update resource on multiple member clusters", Ordered, func() {
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var rp *placementv1beta1.ResourcePlacement
		unAvailablePeriodSeconds := 30
		var observedResourceIdx string

		BeforeAll(func() {
			// Create the test resources.
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Group:     testv1alpha1.GroupVersion.Group,
					Kind:      testCustomResourceKind,
					Name:      testCustomResource.Name,
					Version:   testv1alpha1.GroupVersion.Version,
					Namespace: workNamespace.Name,
				},
			}
		})

		It("create the RP that select the custom resource", func() {
			rp = &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  workNamespace.Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   testv1alpha1.GroupVersion.Group,
							Kind:    testCustomResourceKind,
							Version: testv1alpha1.GroupVersion.Version,
							Name:    testCustomResource.Name,
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							MaxUnavailable: &intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 1,
							},
							UnavailablePeriodSeconds: ptr.To(unAvailablePeriodSeconds),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, rpKey, rp); err != nil {
					return fmt.Errorf("failed to get RP: %w", err)
				}

				if diff := cmp.Diff(rp.Status.SelectedResources, wantSelectedResources, cmpopts.SortSlices(utils.LessFuncResourceIdentifier)); diff != "" {
					return fmt.Errorf("selected resources mismatched (-got, +want): %s", diff)
				}
				// Use the fresh observed resource index to verify the RP status later.
				observedResourceIdx = rp.Status.ObservedResourceIndex
				return nil
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to select all the expected resources")

			rpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(rpKey, wantSelectedResources, allMemberClusterNames, nil, observedResourceIdx, false)
			Eventually(rpStatusUpdatedActual, 2*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
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
				cr.Spec.Foo = valBar1 // Previously was "foo1"
				return hubClient.Update(ctx, &cr)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update custom resource")
		})

		It("should update one member cluster", func() {
			// adding a buffer of 5 seconds
			unavailablePeriod := time.Duration(*rp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds)*time.Second + (5 * time.Second)
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
			unavailablePeriod := time.Duration(*rp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds)*time.Second - (5 * time.Second)
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

		It("should update RP status as expected", func() {
			// Refresh the observed resource index.
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, rpKey, rp); err != nil {
					return fmt.Errorf("failed to get RP: %w", err)
				}

				if rp.Status.ObservedResourceIndex == observedResourceIdx {
					// It is expected that the observed resource index has been bumped by 1
					// due to the resource change.
					return fmt.Errorf("observed resource index is not updated")
				}
				// Use the fresh observed resource index to verify the RP status later.
				observedResourceIdx = rp.Status.ObservedResourceIndex
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to select all the expected resources")

			rpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(rpKey, wantSelectedResources, allMemberClusterNames, nil, observedResourceIdx, false)
			Eventually(rpStatusUpdatedActual, 4*time.Duration(unAvailablePeriodSeconds)*time.Second, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})
	})
})

func buildRPForSafeRollout(namespace string) *placementv1beta1.ResourcePlacement {
	return &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess()),
			Namespace:  namespace,
			Finalizers: []string{customDeletionBlockerFinalizer},
		},
		Spec: placementv1beta1.PlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
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
