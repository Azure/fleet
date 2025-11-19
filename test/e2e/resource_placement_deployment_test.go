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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

var _ = Describe("placing a Deployment using a RP with PickAll policy", Label("resourceplacement"), Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	var testDeployment appsv1.Deployment

	BeforeAll(func() {
		// Read the test deployment manifest
		readDeploymentTestManifest(&testDeployment)
		workNamespace := appNamespace()

		// Create namespace and deployment
		By("creating namespace and deployment")
		Expect(hubClient.Create(ctx, &workNamespace)).To(Succeed(), "Failed to create namespace %s", workNamespace.Name)
		testDeployment.Namespace = workNamespace.Name
		Expect(hubClient.Create(ctx, &testDeployment)).To(Succeed(), "Failed to create test deployment %s", testDeployment.Name)

		// Create the CRP with namespace-only selector
		By("creating CRP with namespace selector")
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name:       crpName,
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

		By("waiting for CRP status to update")
		crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterAll(func() {
		By("cleaning up resources")
		ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: testDeployment.Namespace}, allMemberClusters, &testDeployment)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("with PickAll placement type", Ordered, func() {
		It("creating the RP should succeed", func() {
			By("creating RP that selects the deployment")
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  testDeployment.Namespace,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   appsv1.SchemeGroupVersion.Group,
							Version: appsv1.SchemeGroupVersion.Version,
							Kind:    utils.DeploymentKind,
							Name:    testDeployment.Name,
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
			By("verifying RP status update")
			wantSelectedResources := []placementv1beta1.ResourceIdentifier{
				{
					Group:     appsv1.SchemeGroupVersion.Group,
					Version:   appsv1.SchemeGroupVersion.Version,
					Kind:      utils.DeploymentKind,
					Name:      testDeployment.Name,
					Namespace: testDeployment.Namespace,
				},
			}
			rpStatusUpdatedActual := rpStatusUpdatedActual(wantSelectedResources, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place the deployment on all member clusters", func() {
			By("verifying deployment is placed and ready on all member clusters")
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				deploymentPlacedActual := waitForDeploymentPlacementToReady(memberCluster, &testDeployment)
				Eventually(deploymentPlacedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place deployment on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("should verify deployment replicas are ready on all clusters", func() {
			By("checking deployment status on each cluster")
			for _, cluster := range allMemberClusters {
				Eventually(func() error {
					var deployed appsv1.Deployment
					if err := cluster.KubeClient.Get(ctx, types.NamespacedName{
						Name:      testDeployment.Name,
						Namespace: testDeployment.Namespace,
					}, &deployed); err != nil {
						return err
					}
					// Verify deployment is ready
					if deployed.Status.ReadyReplicas != *deployed.Spec.Replicas {
						return fmt.Errorf("deployment not ready: %d/%d replicas ready", deployed.Status.ReadyReplicas, *deployed.Spec.Replicas)
					}
					if deployed.Status.UpdatedReplicas != *deployed.Spec.Replicas {
						return fmt.Errorf("deployment not updated: %d/%d replicas updated", deployed.Status.UpdatedReplicas, *deployed.Spec.Replicas)
					}
					return nil
				}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(),
					"Deployment should be ready on cluster %s", cluster.ClusterName)
			}
		})
	})
})
