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
	"strings"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workapplier"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

const (
	envelopeResourceName = "envelope-wrapper"
	cmDataKey            = "foo"
	cmDataVal            = "bar"
)

var (
	// pre loaded test manifests
	testConfigMap               corev1.ConfigMap
	testResourceQuota           corev1.ResourceQuota
	testDeployment              appv1.Deployment
	testClusterRole             rbacv1.ClusterRole
	testResourceEnvelope        placementv1beta1.ResourceEnvelope
	testClusterResourceEnvelope placementv1beta1.ClusterResourceEnvelope
)

// Note that this container will run in parallel with other containers.
var _ = Describe("placing wrapped resources using a CRP", func() {
	Context("Test a CRP place enveloped objects successfully", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespaceName := appNamespace().Name
		var wantSelectedResources []placementv1beta1.ResourceIdentifier

		BeforeAll(func() {
			// Create the test resources.
			By("Create the test resources in the namespace")
			readEnvelopTestManifests()
			createWrappedResourcesForEnvelopTest()
		})

		It("Create the CRP that select the name space that contains wrapper and clusterresourceenvelope", func() {
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
							Group:   "",
							Kind:    "Namespace",
							Version: "v1",
							Name:    workNamespaceName,
						},
						{
							Group:   placementv1beta1.GroupVersion.Group,
							Kind:    "ClusterResourceEnvelope",
							Version: placementv1beta1.GroupVersion.Version,
							Name:    testClusterResourceEnvelope.Name,
						},
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
		})

		It("should update CRP status as expected", func() {
			wantSelectedResources = []placementv1beta1.ResourceIdentifier{
				{
					Kind:    "Namespace",
					Name:    workNamespaceName,
					Version: "v1",
				},
				{
					Kind:      "ConfigMap",
					Name:      testConfigMap.Name,
					Version:   "v1",
					Namespace: workNamespaceName,
				},
				{
					Group:   placementv1beta1.GroupVersion.Group,
					Kind:    "ClusterResourceEnvelope",
					Version: placementv1beta1.GroupVersion.Version,
					Name:    testClusterResourceEnvelope.Name,
				},
				{
					Group:     placementv1beta1.GroupVersion.Group,
					Kind:      placementv1beta1.ResourceEnvelopeKind,
					Version:   placementv1beta1.GroupVersion.Version,
					Name:      testResourceEnvelope.Name,
					Namespace: workNamespaceName,
				},
			}
			crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(types.NamespacedName{Name: crpName}, wantSelectedResources, allMemberClusterNames, nil, "0", true)
			Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := checkAllResourcesPlacement(memberCluster)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("Update the resource envelope with bad configuration", func() {
			// modify the embedded namespaced resource to add a scope but it will be rejected as its immutable
			badEnvelopeResourceQuota := testResourceQuota.DeepCopy()
			badEnvelopeResourceQuota.Spec.Scopes = []corev1.ResourceQuotaScope{
				corev1.ResourceQuotaScopeNotBestEffort, corev1.ResourceQuotaScopeNotTerminating,
			}
			badResourceQuotaByte, err := json.Marshal(badEnvelopeResourceQuota)
			Expect(err).Should(Succeed())
			// Get the resource envelope
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: workNamespaceName, Name: testResourceEnvelope.Name}, &testResourceEnvelope)).To(Succeed(), "Failed to get the resourceEnvelope")
			testResourceEnvelope.Data["resourceQuota.yaml"] = runtime.RawExtension{Raw: badResourceQuotaByte}
			Expect(hubClient.Update(ctx, &testResourceEnvelope)).To(Succeed(), "Failed to update the enveloped resource")
		})

		It("should update CRP status with failed to apply resourceQuota", func() {
			// rolloutStarted is false, but other conditions are true.
			// "The rollout is being blocked by the rollout strategy in 2 cluster(s)",
			crpStatusUpdatedActual := checkForRolloutStuckOnOneFailedClusterStatus(wantSelectedResources)
			Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("Update the envelope configMap back with good configuration", func() {
			// Get the resource envelope
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: workNamespaceName, Name: testResourceEnvelope.Name}, &testResourceEnvelope)).To(Succeed(), "Failed to get the resourceEnvelope")
			// update the resource envelope with a valid resourceQuota
			resourceQuotaByte, err := json.Marshal(testResourceQuota)
			Expect(err).Should(Succeed())
			testResourceEnvelope.Data["resourceQuota.yaml"] = runtime.RawExtension{Raw: resourceQuotaByte}
			Expect(hubClient.Update(ctx, &testResourceEnvelope)).To(Succeed(), "Failed to update the enveloped resource")
		})

		It("should update CRP status as success again", func() {
			crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(types.NamespacedName{Name: crpName}, wantSelectedResources, allMemberClusterNames, nil, "2", true)
			Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters again", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := checkAllResourcesPlacement(memberCluster)
				Eventually(workResourcesPlacedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("can delete the CRP", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
			}
			Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP")
		})

		It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

		It("should remove controller finalizers from CRP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: crpName})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP")
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting envelope %s", testResourceEnvelope.Name))
			Expect(hubClient.Delete(ctx, &testResourceEnvelope)).To(Succeed(), "Failed to delete ResourceEnvelope")
			By(fmt.Sprintf("deleting cluster scoped envelope %s", testClusterResourceEnvelope.Name))
			Expect(hubClient.Delete(ctx, &testClusterResourceEnvelope)).To(Succeed(), "Failed to delete testClusterResourceEnvelope")
			By(fmt.Sprintf("deleting placement %s and related resources", crpName))
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("Test a CRP place workload objects with mixed availability", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespace := appNamespace()
		var wantSelectedResources []placementv1beta1.ResourceIdentifier
		var testDaemonSet appv1.DaemonSet
		var testStatefulSet appv1.StatefulSet

		BeforeAll(func() {
			// read the test resources.
			readDeploymentTestManifest(&testDeployment)
			readDaemonSetTestManifest(&testDaemonSet)
			readStatefulSetTestManifest(&testStatefulSet, StatefulSetInvalidStorage)
			readEnvelopeResourceTestManifest(&testResourceEnvelope)
		})

		It("Create the namespace", func() {
			Expect(hubClient.Create(ctx, &workNamespace)).To(Succeed(), "Failed to create namespace %s", workNamespace.Name)
		})

		It("Create the wrapped resources in the namespace", func() {
			testResourceEnvelope.Data = make(map[string]runtime.RawExtension)
			constructWrappedResources(&testResourceEnvelope, &testDeployment, utils.DeploymentKind, workNamespace)
			constructWrappedResources(&testResourceEnvelope, &testDaemonSet, utils.DaemonSetKind, workNamespace)
			constructWrappedResources(&testResourceEnvelope, &testStatefulSet, utils.StatefulSetKind, workNamespace)
			Expect(hubClient.Create(ctx, &testResourceEnvelope)).To(Succeed(), "Failed to create testEnvelope object %s containing workloads", testResourceEnvelope.Name)
		})

		It("Create the CRP that select the namespace", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe the behavior of the controllers.
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

		It("should update CRP status with only a not available statefulset", func() {
			// the statefulset has an invalid storage class PVC
			failedStatefulSetResourceIdentifier := placementv1beta1.ResourceIdentifier{
				Group:     appv1.SchemeGroupVersion.Group,
				Version:   appv1.SchemeGroupVersion.Version,
				Kind:      utils.StatefulSetKind,
				Name:      testStatefulSet.Name,
				Namespace: testStatefulSet.Namespace,
				Envelope: &placementv1beta1.EnvelopeIdentifier{
					Name:      testResourceEnvelope.Name,
					Namespace: workNamespace.Name,
					Type:      placementv1beta1.ResourceEnvelopeType,
				},
			}
			// We only expect the statefulset to not be available all the clusters
			PlacementStatuses := make([]placementv1beta1.PerClusterPlacementStatus, 0)
			for _, memberClusterName := range allMemberClusterNames {
				unavailableResourcePlacementStatus := placementv1beta1.PerClusterPlacementStatus{
					ClusterName:           memberClusterName,
					ObservedResourceIndex: "0",
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.PerClusterScheduledConditionType),
							Status:             metav1.ConditionTrue,
							Reason:             condition.ScheduleSucceededReason,
							ObservedGeneration: 1,
						},
						{
							Type:               string(placementv1beta1.PerClusterRolloutStartedConditionType),
							Status:             metav1.ConditionTrue,
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: 1,
						},
						{
							Type:               string(placementv1beta1.PerClusterOverriddenConditionType),
							Status:             metav1.ConditionTrue,
							Reason:             condition.OverrideNotSpecifiedReason,
							ObservedGeneration: 1,
						},
						{
							Type:               string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
							Status:             metav1.ConditionTrue,
							Reason:             condition.AllWorkSyncedReason,
							ObservedGeneration: 1,
						},
						{
							Type:               string(placementv1beta1.PerClusterAppliedConditionType),
							Status:             metav1.ConditionTrue,
							Reason:             condition.AllWorkAppliedReason,
							ObservedGeneration: 1,
						},
						{
							Type:               string(placementv1beta1.PerClusterAvailableConditionType),
							Status:             metav1.ConditionFalse,
							Reason:             condition.WorkNotAvailableReason,
							ObservedGeneration: 1,
						},
					},
					FailedPlacements: []placementv1beta1.FailedResourcePlacement{
						{
							ResourceIdentifier: failedStatefulSetResourceIdentifier,
							Condition: metav1.Condition{
								Type:               string(placementv1beta1.PerClusterAvailableConditionType),
								Status:             metav1.ConditionFalse,
								Reason:             string(workapplier.AvailabilityResultTypeNotYetAvailable),
								ObservedGeneration: 1,
							},
						},
					},
				}
				PlacementStatuses = append(PlacementStatuses, unavailableResourcePlacementStatus)
			}
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
					Name:      testResourceEnvelope.Name,
					Namespace: workNamespace.Name,
				},
			}
			wantStatus := placementv1beta1.PlacementStatus{
				Conditions:                  crpNotAvailableConditions(1, false),
				PerClusterPlacementStatuses: PlacementStatuses,
				SelectedResources:           wantSelectedResources,
				ObservedResourceIndex:       "0",
			}

			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				if diff := cmp.Diff(crp.Status, wantStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting envelope %s", testResourceEnvelope.Name))
			Expect(hubClient.Delete(ctx, &testResourceEnvelope)).To(Succeed(), "Failed to delete ResourceEnvelope")
			By(fmt.Sprintf("deleting placement %s and related resources", crpName))
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})
	})

	Context("Block envelopeResource that wrap cluster-scoped resources", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		var envelopWrapper *placementv1beta1.ResourceEnvelope

		BeforeAll(func() {
			// Use an envelope to create duplicate resource entries.
			ns := appNamespace()
			Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

			// Create an envelope config map.
			envelopWrapper = &placementv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      envelopeResourceName,
					Namespace: ns.Name,
				},
				Data: make(map[string]runtime.RawExtension),
			}

			// Create a configMap and a clusterRole as wrapped resources.
			configMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: ns.Name,
					Name:      "config",
				},
				Data: map[string]string{
					cmDataKey: cmDataVal,
				},
			}
			configMapBytes, err := json.Marshal(configMap)
			Expect(err).To(BeNil(), "Failed to marshal configMap %s", configMap.Name)
			envelopWrapper.Data["cm.yaml"] = runtime.RawExtension{Raw: configMapBytes}

			clusterRole := &rbacv1.ClusterRole{
				TypeMeta: metav1.TypeMeta{
					APIVersion: rbacv1.SchemeGroupVersion.String(),
					Kind:       "ClusterRole",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "clusterRole",
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"pods"},
						Verbs:     []string{"get", "list", "watch"},
					},
				},
			}
			clusterRoleBytes, err := json.Marshal(clusterRole)
			Expect(err).To(BeNil(), "Failed to marshal clusterRole %s", clusterRole.Name)
			envelopWrapper.Data["cb.yaml"] = runtime.RawExtension{Raw: clusterRoleBytes}

			Expect(hubClient.Create(ctx, envelopWrapper)).To(Succeed(), "Failed to create wrapper %s", envelopWrapper.Name)

			// Create a CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames: []string{
							memberCluster1EastProdName,
						},
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
		})

		It("should update CRP status as expected", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				wantStatus := placementv1beta1.PlacementStatus{
					Conditions: crpWorkSynchronizedFailedConditions(crp.Generation, false),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterWorkSynchronizedFailedConditions(crp.Generation, false),
						},
					},
					SelectedResources: []placementv1beta1.ResourceIdentifier{
						{
							Kind:    "Namespace",
							Name:    workNamespaceName,
							Version: "v1",
						},
						{
							Group:     placementv1beta1.GroupVersion.Group,
							Kind:      placementv1beta1.ResourceEnvelopeKind,
							Version:   placementv1beta1.GroupVersion.Version,
							Name:      envelopeResourceName,
							Namespace: workNamespaceName,
						},
					},
					ObservedResourceIndex: "0",
				}
				if diff := cmp.Diff(crp.Status, wantStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		// Note that due to the order in which the work generator handles resources, the synchronization error is
		// triggered before the primary work object is applied; that is, the namespace itself will not be created
		// either.

		AfterAll(func() {
			By(fmt.Sprintf("deleting envelope %s", envelopWrapper.Name))
			Expect(hubClient.Delete(ctx, envelopWrapper)).To(Succeed(), "Failed to delete ResourceEnvelope")
			// Remove the CRP and the namespace from the hub cluster.
			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{memberCluster1EastProd})
		})
	})
})

// Note that this container will run in parallel with other containers.
var _ = Describe("Test NamespaceOnly placement through CRP then resource placement through ResourcePlacement", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	workNamespaceName := appNamespace().Name

	BeforeAll(func() {
		// Create the test resources.
		By("Create the test resources in the namespace")
		readEnvelopTestManifests()
		createWrappedResourcesForEnvelopTest()
	})

	It("Create the CRP that selects only the namespace with NamespaceOnly scope", func() {
		createNamespaceOnlyCRP(crpName)
	})

	It("should update CRP status to show namespace placement", func() {
		statusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(statusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place only the namespace on all member clusters", func() {
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]
			// Verify namespace is placed
			workNamespacePlacedActual := func() error {
				return validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: workNamespaceName})
			}
			Eventually(workNamespacePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place namespace on member cluster %s", memberCluster.ClusterName)

			// Verify configMap is NOT placed (NamespaceOnly should not include resources within namespace)
			nameSpaceResourceNotPlacedActual := namespacedResourcesRemovedFromClusterActual(memberCluster, &testConfigMap)
			Consistently(nameSpaceResourceNotPlacedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Namespace resource (ConfigMap) should not be placed with NamespaceOnly selection on member cluster %s", memberCluster.ClusterName)
		}
	})

	It("Create the ResourcePlacement that selects the ResourceEnvelope", func() {
		rp := &placementv1beta1.ResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpName,
				Namespace: workNamespaceName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   placementv1beta1.GroupVersion.Group,
						Kind:    placementv1beta1.ResourceEnvelopeKind,
						Version: placementv1beta1.GroupVersion.Version,
						Name:    testResourceEnvelope.Name,
					},
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create ResourcePlacement")
	})

	It("should update ResourcePlacement status as expected", func() {
		rpSelectedResources := []placementv1beta1.ResourceIdentifier{
			{
				Group:     placementv1beta1.GroupVersion.Group,
				Kind:      placementv1beta1.ResourceEnvelopeKind,
				Version:   placementv1beta1.GroupVersion.Version,
				Name:      testResourceEnvelope.Name,
				Namespace: workNamespaceName,
			},
		}
		Eventually(rpStatusUpdatedActual(rpSelectedResources, allMemberClusterNames, nil, "0"), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ResourcePlacement status as expected")
	})

	It("should place the enveloped resources on all member clusters", func() {
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]
			workResourcesPlacedActual := checkEnvelopedResourcesPlacement(memberCluster)
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place enveloped resources on member cluster %s", memberCluster.ClusterName)
		}
	})

	It("should not placed the configMap on any clusters as its not selected", func() {
		// Verify that all resources placed have been removed from specified member clusters.
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]
			configMapNotPlacedActual := namespacedResourcesRemovedFromClusterActual(memberCluster, &testConfigMap)
			Eventually(configMapNotPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from member cluster %s", memberCluster.ClusterName)
		}
	})

	It("can delete the ResourcePlacement", func() {
		rp := &placementv1beta1.ResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpName,
				Namespace: workNamespaceName,
			},
		}
		Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete ResourcePlacement")
	})

	It("should remove enveloped resources from all clusters", func() {
		// Verify that all resources placed have been removed from specified member clusters.
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]
			workResourcesRemovedActual := namespacedResourcesRemovedFromClusterActual(memberCluster, &testDeployment, &testResourceQuota)
			Eventually(workResourcesRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from member cluster %s", memberCluster.ClusterName)
		}
	})

	It("should remove controller finalizers from ResourcePlacement", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: workNamespaceName})
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from ResourcePlacement")
	})

	It("should not remove namespace on all member clusters", func() {
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]
			// Verify namespace is placed
			workNamespacePlacedActual := func() error {
				return validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: workNamespaceName})
			}
			Eventually(workNamespacePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place namespace on member cluster %s", memberCluster.ClusterName)
		}
	})

	It("can delete the CRP", func() {
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
		}
		Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP")
	})

	It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: crpName})
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP")
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting ResourcePlacement %s and related resources", rpName))
		ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, allMemberClusters)
		By(fmt.Sprintf("deleting envelope %s", testClusterResourceEnvelope.Name))
		Expect(hubClient.Delete(ctx, &testClusterResourceEnvelope)).To(Succeed(), "Failed to delete testClusterResourceEnvelope")
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})
})

var _ = Describe("Process objects with generate name", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	nsGenerateName := "application-"
	wrappedCMGenerateName := "wrapped-foo-"
	var envelope *placementv1beta1.ResourceEnvelope

	BeforeAll(func() {
		// Create the namespace with both name and generate name set.
		ns := appNamespace()
		ns.GenerateName = nsGenerateName
		Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

		// Create an envelope.
		envelope = &placementv1beta1.ResourceEnvelope{
			ObjectMeta: metav1.ObjectMeta{
				Name:      envelopeResourceName,
				Namespace: ns.Name,
			},
			Data: map[string]runtime.RawExtension{},
		}

		wrappedCM := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: wrappedCMGenerateName,
				Namespace:    ns.Name,
			},
			Data: map[string]string{
				cmDataKey: cmDataVal,
			},
		}
		wrappedCMByte, err := json.Marshal(wrappedCM)
		Expect(err).Should(BeNil())
		envelope.Data["wrapped.yaml"] = runtime.RawExtension{Raw: wrappedCMByte}
		Expect(hubClient.Create(ctx, envelope)).To(Succeed(), "Failed to create config map %s", envelope.Name)

		// Create a CRP that selects the namespace.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: workResourceSelector(),
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickFixedPlacementType,
					ClusterNames: []string{
						memberCluster1EastProdName,
					},
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
	})

	It("should update CRP status as expected", func() {
		Eventually(func() error {
			crp := &placementv1beta1.ClusterResourcePlacement{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
				return err
			}

			wantStatus := placementv1beta1.PlacementStatus{
				Conditions: crpAppliedFailedConditions(crp.Generation),
				PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           memberCluster1EastProdName,
						ObservedResourceIndex: "0",
						FailedPlacements: []placementv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: placementv1beta1.ResourceIdentifier{
									Kind:      "ConfigMap",
									Namespace: workNamespaceName,
									Version:   "v1",
									Envelope: &placementv1beta1.EnvelopeIdentifier{
										Name:      envelopeResourceName,
										Namespace: workNamespaceName,
										Type:      placementv1beta1.ResourceEnvelopeType,
									},
								},
								Condition: metav1.Condition{
									Type:               placementv1beta1.WorkConditionTypeApplied,
									Status:             metav1.ConditionFalse,
									Reason:             string(workapplier.ApplyOrReportDiffResTypeFoundGenerateName),
									ObservedGeneration: 0,
								},
							},
						},
						Conditions: perClusterApplyFailedConditions(crp.Generation),
					},
				},
				SelectedResources: []placementv1beta1.ResourceIdentifier{
					{
						Kind:    "Namespace",
						Name:    workNamespaceName,
						Version: "v1",
					},
					{
						Group:     placementv1beta1.GroupVersion.Group,
						Kind:      placementv1beta1.ResourceEnvelopeKind,
						Version:   placementv1beta1.GroupVersion.Version,
						Name:      envelopeResourceName,
						Namespace: workNamespaceName,
					},
				},
				ObservedResourceIndex: "0",
			}
			if diff := cmp.Diff(crp.Status, wantStatus, placementStatusCmpOptions...); diff != "" {
				return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
			}
			return nil
		}, eventuallyDuration*3, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place some manifests on member clusters", func() {
		Eventually(func() error {
			return validateWorkNamespaceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: workNamespaceName})
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")
	})

	It("should not place some manifests on member clusters", func() {
		Consistently(func() error {
			cmList := &corev1.ConfigMapList{}
			if err := memberCluster1EastProdClient.List(ctx, cmList, client.InNamespace(workNamespaceName)); err != nil {
				return fmt.Errorf("failed to list ConfigMap objects: %w", err)
			}

			for _, cm := range cmList.Items {
				if cm.GenerateName == wrappedCMGenerateName {
					return fmt.Errorf("found a ConfigMap object with generate name that should not be applied")
				}
			}
			return nil
		}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Applied the wrapped config map on member cluster")
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting envelope %s", envelope.Name))
		Expect(hubClient.Delete(ctx, envelope)).To(Succeed(), "Failed to delete ResourceEnvelope")
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})
})

func checkForRolloutStuckOnOneFailedClusterStatus(wantSelectedResources []placementv1beta1.ResourceIdentifier) func() error {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	wantFailedResourcePlacement := []placementv1beta1.FailedResourcePlacement{
		{
			ResourceIdentifier: placementv1beta1.ResourceIdentifier{
				Kind:      "ResourceQuota",
				Name:      testResourceQuota.Name,
				Version:   "v1",
				Namespace: testResourceQuota.Namespace,
				Envelope: &placementv1beta1.EnvelopeIdentifier{
					Name:      testResourceEnvelope.Name,
					Namespace: workNamespaceName,
					Type:      placementv1beta1.ResourceEnvelopeType,
				},
			},
			Condition: metav1.Condition{
				Type:   placementv1beta1.WorkConditionTypeApplied,
				Status: metav1.ConditionFalse,
				Reason: string(workapplier.ApplyOrReportDiffResTypeFailedToApply),
			},
		},
	}

	return func() error {
		crp := &placementv1beta1.ClusterResourcePlacement{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return err
		}
		wantCRPConditions := crpRolloutStuckConditions(crp.Generation)
		if diff := cmp.Diff(crp.Status.Conditions, wantCRPConditions, placementStatusCmpOptions...); diff != "" {
			return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
		}
		// check the selected resources is still right
		if diff := cmp.Diff(crp.Status.SelectedResources, wantSelectedResources, placementStatusCmpOptions...); diff != "" {
			return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
		}
		// check the placement status has a failed placement
		applyFailed := false
		for _, placementStatus := range crp.Status.PerClusterPlacementStatuses {
			if len(placementStatus.FailedPlacements) != 0 {
				applyFailed = true
			}
		}
		if !applyFailed {
			return fmt.Errorf("CRP status does not have failed placement")
		}
		for _, placementStatus := range crp.Status.PerClusterPlacementStatuses {
			// this is the cluster that got the new enveloped resource that was malformed
			if len(placementStatus.FailedPlacements) != 0 {
				if diff := cmp.Diff(placementStatus.FailedPlacements, wantFailedResourcePlacement, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
				// check that the applied error message is correct
				if !strings.Contains(placementStatus.FailedPlacements[0].Condition.Message, "field is immutable") {
					return fmt.Errorf("CRP failed resource placement does not have unsupported scope message")
				}
				if diff := cmp.Diff(placementStatus.Conditions, perClusterApplyFailedConditions(crp.Generation), placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
			} else {
				// the cluster is stuck behind a rollout schedule since we now have 1 cluster that is not in applied ready status
				if diff := cmp.Diff(placementStatus.Conditions, perClusterSyncPendingConditions(crp.Generation), placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
				}
			}
		}
		return nil
	}
}

func readEnvelopTestManifests() {
	By("Read the ConfigMap resources which is no longer identified as the envelope")
	testConfigMap = corev1.ConfigMap{}
	err := utils.GetObjectFromManifest("resources/test-configmap.yaml", &testConfigMap)
	Expect(err).Should(Succeed())

	By("Read ResourceQuota to be filled in an envelope")
	testResourceQuota = corev1.ResourceQuota{}
	err = utils.GetObjectFromManifest("resources/resourcequota.yaml", &testResourceQuota)
	Expect(err).Should(Succeed())

	By("Read Deployment to be filled in an envelope")
	testDeployment = appv1.Deployment{}
	err = utils.GetObjectFromManifest("resources/test-deployment.yaml", &testDeployment)
	Expect(err).Should(Succeed())

	By("Read ClusterRole to be filled in an envelope")
	testClusterRole = rbacv1.ClusterRole{}
	err = utils.GetObjectFromManifest("resources/test-clusterrole.yaml", &testClusterRole)
	Expect(err).Should(Succeed())

	By("Create ResourceEnvelope template")
	testResourceEnvelope = placementv1beta1.ResourceEnvelope{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-resource-envelope",
		},
		Data: make(map[string]runtime.RawExtension),
	}

	By("Create ClusterResourceEnvelope template")
	clusterEnvName := fmt.Sprintf("test-cluster-resource-envelope-%d", GinkgoParallelProcess())
	testClusterResourceEnvelope = placementv1beta1.ClusterResourceEnvelope{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterEnvName,
		},
		Data: make(map[string]runtime.RawExtension),
	}
}

// createWrappedResourcesForEnvelopTest creates some enveloped resources on the hub cluster for testing purposes.
func createWrappedResourcesForEnvelopTest() {
	ns := appNamespace()
	Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

	// Update namespaces for namespaced resources
	testConfigMap.Namespace = ns.Name
	Expect(hubClient.Create(ctx, &testConfigMap)).To(Succeed(), "Failed to create ConfigMap")

	testResourceQuota.Namespace = ns.Name
	testDeployment.Namespace = ns.Name
	testResourceEnvelope.Namespace = ns.Name

	// Create ResourceEnvelope with ResourceQuota inside
	quotaBytes, err := json.Marshal(testResourceQuota)
	Expect(err).Should(Succeed())
	testResourceEnvelope.Data["resourceQuota.yaml"] = runtime.RawExtension{Raw: quotaBytes}
	deploymentBytes, err := json.Marshal(testDeployment)
	Expect(err).Should(Succeed())
	testResourceEnvelope.Data["deployment.yaml"] = runtime.RawExtension{Raw: deploymentBytes}
	Expect(hubClient.Create(ctx, &testResourceEnvelope)).To(Succeed(), "Failed to create ResourceEnvelope")

	// Create ClusterResourceEnvelope with ClusterRole inside
	roleBytes, err := json.Marshal(testClusterRole)
	Expect(err).Should(Succeed())
	testClusterResourceEnvelope.Data["clusterRole.yaml"] = runtime.RawExtension{Raw: roleBytes}
	Expect(hubClient.Create(ctx, &testClusterResourceEnvelope)).To(Succeed(), "Failed to create ClusterResourceEnvelope")
}

func cleanupWrappedResourcesForEnvelopTest() {
	By("deleting namespace resources")
	cleanupWorkResources()

	By(fmt.Sprintf("deleting envelope %s", testClusterResourceEnvelope.Name))
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, &testClusterResourceEnvelope))).To(Succeed(), "Failed to delete testClusterResourceEnvelope")

	Eventually(func() error {
		if err := hubClient.Get(ctx, types.NamespacedName{Name: testClusterResourceEnvelope.Name}, &placementv1beta1.ClusterResourceEnvelope{}); !errors.IsNotFound(err) {
			return fmt.Errorf("testClusterResourceEnvelope still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove testClusterResourceEnvelope from hub cluster")
}

// checkEnvelopedResourcesPlacement checks if the enveloped resources (ResourceQuota and Deployment from ResourceEnvelope) are placed correctly
func checkEnvelopedResourcesPlacement(memberCluster *framework.Cluster) func() error {
	workNamespaceName := appNamespace().Name
	return func() error {
		// Check that ResourceQuota from ResourceEnvelope was placed
		By("Check ResourceQuota from ResourceEnvelope")
		placedResourceQuota := &corev1.ResourceQuota{}
		if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{
			Namespace: workNamespaceName,
			Name:      testResourceQuota.Name,
		}, placedResourceQuota); err != nil {
			return fmt.Errorf("failed to find resourceQuota from ResourceEnvelope:  %s: %w", testResourceQuota.Name, err)
		}
		// Verify the ResourceQuota matches expected spec
		if diff := cmp.Diff(placedResourceQuota.Spec, testResourceQuota.Spec); diff != "" {
			return fmt.Errorf("ResourceQuota from ResourceEnvelope diff (-got, +want): %s", diff)
		}

		// Check that Deployment from ResourceEnvelope was placed
		By("Check Deployment from ResourceEnvelope")
		placedDeployment := &appv1.Deployment{}
		if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{
			Namespace: workNamespaceName,
			Name:      testDeployment.Name,
		}, placedDeployment); err != nil {
			return fmt.Errorf("failed to find Deployment from ResourceEnvelope: %w", err)
		}

		// Verify the deployment matches expected spec
		if diff := cmp.Diff(placedDeployment.Spec.Template.Spec.Containers[0].Image, testDeployment.Spec.Template.Spec.Containers[0].Image); diff != "" {
			return fmt.Errorf("deployment from ResourceEnvelope diff (-got, +want): %s", diff)
		}

		return nil
	}
}

func checkAllResourcesPlacement(memberCluster *framework.Cluster) func() error {
	workNamespaceName := appNamespace().Name
	return func() error {
		// Verify namespace exists on target cluster
		if err := validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: workNamespaceName}); err != nil {
			return err
		}

		// Check that ConfigMap was placed
		By("Check ConfigMap")
		placedConfigMap := &corev1.ConfigMap{}
		if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{
			Namespace: workNamespaceName,
			Name:      testConfigMap.Name,
		}, placedConfigMap); err != nil {
			return fmt.Errorf("failed to find configMap %s: %w", testConfigMap.Name, err)
		}
		// Verify the Configmap matches expected spec
		if diff := cmp.Diff(placedConfigMap.Data, testConfigMap.Data); diff != "" {
			return fmt.Errorf("ConfigMap diff (-got, +want): %s", diff)
		}

		// Check enveloped resources (ResourceQuota and Deployment from ResourceEnvelope)
		if err := checkEnvelopedResourcesPlacement(memberCluster)(); err != nil {
			return err
		}

		// Check that ClusterRole from ClusterResourceEnvelope was placed
		By("Check ClusterRole from ClusterResourceEnvelope")
		placedClusterRole := &rbacv1.ClusterRole{}
		if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{
			Name: testClusterRole.Name,
		}, placedClusterRole); err != nil {
			return fmt.Errorf("failed to find ClusterRole from ClusterResourceEnvelope: %w", err)
		}

		// Verify the ClusterRole matches expected rules
		if diff := cmp.Diff(placedClusterRole.Rules, testClusterRole.Rules); diff != "" {
			return fmt.Errorf("clusterRole from ClusterResourceEnvelope diff (-got, +want): %s", diff)
		}

		return nil
	}
}
