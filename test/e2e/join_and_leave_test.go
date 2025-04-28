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
	"errors"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	memberAgentName = "member-agent"
)

// Note that this container cannot run in parallel with other containers.
var _ = Describe("Test member cluster join and leave flow", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	internalServiceExportName := fmt.Sprintf("internal-service-export-%d", GinkgoParallelProcess())
	var wantSelectedResources []placementv1beta1.ResourceIdentifier
	BeforeAll(func() {
		// Create the test resources.
		readEnvelopTestManifests()
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
				Kind:      "ConfigMap",
				Name:      testEnvelopConfigMap.Name,
				Version:   "v1",
				Namespace: workNamespaceName,
			},
		}
	})

	Context("Test cluster join and leave flow with CRP not deleted", Ordered, Serial, func() {
		It("Create the test resources in the namespace", createWrappedResourcesForEnvelopTest)

		It("Create the CRP that select the name space and place it to all clusters", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
						ApplyStrategy: &placementv1beta1.ApplyStrategy{AllowCoOwnership: true},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			// resourceQuota is not trackable yet
			crpStatusUpdatedActual := customizedCRPStatusUpdatedActual(crpName, wantSelectedResources, allMemberClusterNames, nil, "0", true)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := checkEnvelopQuotaPlacement(memberCluster)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("create a dummy internalServiceExport in the reserved member namespace", func() {
			for idx := range allMemberClusterNames {
				memberCluster := allMemberClusters[idx]
				namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster.ClusterName)
				internalServiceExport := &fleetnetworkingv1alpha1.InternalServiceExport{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespaceName,
						Name:      internalServiceExportName,
						// Add a custom finalizer; this would allow us to better observe
						// the behavior of the controllers.
						Finalizers: []string{customDeletionBlockerFinalizer},
					},
					Spec: fleetnetworkingv1alpha1.InternalServiceExportSpec{
						ServiceReference: fleetnetworkingv1alpha1.ExportedObjectReference{
							NamespacedName:  "test-namespace/test-svc",
							ClusterID:       memberCluster.ClusterName,
							Kind:            "Service",
							Namespace:       "test-namespace",
							Name:            "test-svc",
							ResourceVersion: "0",
							UID:             "00000000-0000-0000-0000-000000000000",
						},
						Ports: []fleetnetworkingv1alpha1.ServicePort{
							{
								Protocol: corev1.ProtocolTCP,
								Port:     4848,
							},
						},
					},
				}
				Expect(hubClient.Create(ctx, internalServiceExport)).To(Succeed(), "Failed to create internalServiceExport")
			}
		})

		It("Should fail the unjoin requests", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				mcObj := &clusterv1beta1.MemberCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: memberCluster.ClusterName,
					},
				}
				err := hubClient.Delete(ctx, mcObj)
				Expect(err).ShouldNot(Succeed(), "Want the deletion to be denied")
				var statusErr *apierrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete memberCluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&apierrors.StatusError{})))
				Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("Please delete serviceExport test-namespace/test-svc in the member cluster before leaving, request is denied"))
			}
		})

		It("Deleting the internalServiceExports", func() {
			for idx := range allMemberClusterNames {
				memberCluster := allMemberClusters[idx]
				namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster.ClusterName)

				internalSvcExportKey := types.NamespacedName{Namespace: namespaceName, Name: internalServiceExportName}
				var export fleetnetworkingv1alpha1.InternalServiceExport
				Expect(hubClient.Get(ctx, internalSvcExportKey, &export)).Should(Succeed(), "Failed to get internalServiceExport")
				Expect(hubClient.Delete(ctx, &export)).To(Succeed(), "Failed to delete internalServiceExport")
			}
		})

		It("Should be able to trigger the member cluster DELETE", func() {
			setAllMemberClustersToLeave()
		})

		It("Removing the finalizer from the internalServiceExport", func() {
			for idx := range allMemberClusterNames {
				memberCluster := allMemberClusters[idx]
				namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster.ClusterName)

				internalSvcExportKey := types.NamespacedName{Namespace: namespaceName, Name: internalServiceExportName}
				var export fleetnetworkingv1alpha1.InternalServiceExport
				Expect(hubClient.Get(ctx, internalSvcExportKey, &export)).Should(Succeed(), "Failed to get internalServiceExport")
				export.Finalizers = nil
				Expect(hubClient.Update(ctx, &export)).To(Succeed(), "Failed to update internalServiceExport")
			}
		})

		It("Should be able to unjoin a cluster with crp still running", func() {
			checkIfAllMemberClustersHaveLeft()
		})

		It("should update CRP status to not placing any resources since all clusters are left", func() {
			// resourceQuota is enveloped so it's not trackable yet
			crpStatusUpdatedActual := customizedCRPStatusUpdatedActual(crpName, wantSelectedResources, nil, nil, "0", false)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("Should be able to rejoin the cluster", func() {
			By("rejoin all the clusters without deleting the CRP")
			setAllMemberClustersToJoin()
			checkIfAllMemberClustersHaveJoined()
			checkIfAzurePropertyProviderIsWorking()
		})

		It("should update CRP status to applied to all clusters again automatically after rejoining", func() {
			crpStatusUpdatedActual := customizedCRPStatusUpdatedActual(crpName, wantSelectedResources, allMemberClusterNames, nil, "0", true)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})
})

var _ = Describe("Test member cluster force delete flow", Ordered, Serial, func() {
	Context("Test cluster join and leave flow with member agent down and force delete member cluster", Ordered, Serial, func() {
		It("Simulate the member agent going down in member cluster", func() {
			updateMemberAgentDeploymentReplicas(memberCluster3WestProdClient, 0)
		})

		It("Delete member cluster CR associated to the member cluster with member agent down", func() {
			var mc clusterv1beta1.MemberCluster
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: memberCluster3WestProdName}, &mc)).To(Succeed(), "Failed to get member cluster")
			Expect(hubClient.Delete(ctx, &mc)).Should(Succeed())
		})
		// we set force delete time at 1 minute in the test env
		It("Should garbage collect resources owned by member cluster and force delete member cluster CR after force delete wait time", func() {
			memberClusterNamespace := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster3WestProdName)
			Eventually(func() bool {
				var ns corev1.Namespace
				return apierrors.IsNotFound(hubClient.Get(ctx, types.NamespacedName{Name: memberClusterNamespace}, &ns))
			}, longEventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to garbage collect resources owned by member cluster")

			Eventually(func() bool {
				return apierrors.IsNotFound(hubClient.Get(ctx, types.NamespacedName{Name: memberCluster3WestProdName}, &clusterv1beta1.MemberCluster{}))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to force delete member cluster")
		})
	})

	AfterAll(func() {
		By("Simulate the member agent coming back up")
		updateMemberAgentDeploymentReplicas(memberCluster3WestProdClient, 1)

		createMemberCluster(memberCluster3WestProd.ClusterName, memberCluster3WestProd.PresentingServiceAccountInHubClusterName, labelsByClusterName[memberCluster3WestProd.ClusterName], annotationsByClusterName[memberCluster3WestProd.ClusterName])
		checkIfMemberClusterHasJoined(memberCluster3WestProd)
	})
})

func updateMemberAgentDeploymentReplicas(clusterClient client.Client, replicas int32) {
	Eventually(func() error {
		var d appsv1.Deployment
		err := clusterClient.Get(ctx, types.NamespacedName{Name: memberAgentName, Namespace: fleetSystemNS}, &d)
		if err != nil {
			return err
		}
		d.Spec.Replicas = ptr.To(replicas)
		return clusterClient.Update(ctx, &d)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed())

	Eventually(func() error {
		var podList corev1.PodList
		listOpts := []client.ListOption{
			client.InNamespace(fleetSystemNS),
			client.MatchingLabels(map[string]string{"app.kubernetes.io/name": memberAgentName}),
		}
		err := clusterClient.List(ctx, &podList, listOpts...)
		if err != nil {
			return err
		}
		if len(podList.Items) != int(replicas) {
			return fmt.Errorf("member agent pods %d doesn't match replicas specified %d", len(podList.Items), replicas)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed())
}
