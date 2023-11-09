/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	imcv1beta1 "go.goms.io/fleet/pkg/controllers/internalmembercluster/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/test/e2e/framework"
)

// setAllMemberClustersToJoin creates a MemberCluster object for each member cluster.
func setAllMemberClustersToJoin() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]
		//TODO: clean up the member cluster object if it already exists
		mcObj := &clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   memberCluster.ClusterName,
				Labels: labelsByClusterName[memberCluster.ClusterName],
			},
			Spec: clusterv1beta1.MemberClusterSpec{
				Identity: rbacv1.Subject{
					Name:      hubClusterSAName,
					Kind:      "ServiceAccount",
					Namespace: fleetSystemNS,
				},
			},
		}
		Expect(hubClient.Create(ctx, mcObj)).To(Succeed(), "Failed to create member cluster object")
	}
}

// checkIfAllMemberClustersHaveJoined verifies if all member clusters have connected to the hub
// cluster, i.e., updated the MemberCluster object status as expected.
func checkIfAllMemberClustersHaveJoined() {
	wantAgentStatus := []clusterv1beta1.AgentStatus{
		{
			Type: clusterv1beta1.MemberAgent,
			Conditions: []metav1.Condition{
				{
					Status: metav1.ConditionTrue,
					Type:   string(clusterv1beta1.AgentHealthy),
					Reason: imcv1beta1.EventReasonInternalMemberClusterHealthy,
				},
				{
					Status: metav1.ConditionTrue,
					Type:   string(clusterv1beta1.AgentJoined),
					Reason: imcv1beta1.EventReasonInternalMemberClusterJoined,
				},
			},
		},
	}

	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		Eventually(func() error {
			mcObj := &clusterv1beta1.MemberCluster{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: memberCluster.ClusterName}, mcObj); err != nil {
				By(fmt.Sprintf("Failed to get member cluster object %s", memberCluster.ClusterName))
				return err
			}

			if diff := cmp.Diff(
				mcObj.Status.AgentStatus,
				wantAgentStatus,
				cmpopts.SortSlices(lessFuncCondition),
				ignoreConditionObservedGenerationField,
				ignoreConditionLTTAndMessageFields,
				ignoreAgentStatusHeartbeatField,
			); diff != "" {
				return fmt.Errorf("agent status diff (-got, +want): %s", diff)
			}

			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Member cluster has not joined yet")
	}
}

// setupInvalidClusters simulates the case where some clusters in the fleet becomes unhealthy or
// have left the fleet.
func setupInvalidClusters() {
	// Create a member cluster object that represents the unhealthy cluster.
	mcObj := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberCluster4UnhealthyName,
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Name:      hubClusterSAName,
				Kind:      "ServiceAccount",
				Namespace: fleetSystemNS,
			},
		},
	}
	Expect(hubClient.Create(ctx, mcObj)).To(Succeed(), "Failed to create member cluster object")

	// Mark the member cluster as unhealthy.

	// Use an Eventually block to avoid flakiness and conflicts; as the hub agent will attempt
	// to reconcile this object at the same time.
	Eventually(func() error {
		memberCluster := clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: memberCluster4UnhealthyName}, &memberCluster); err != nil {
			return err
		}

		memberCluster.Status = clusterv1beta1.MemberClusterStatus{
			AgentStatus: []clusterv1beta1.AgentStatus{
				{
					Type: clusterv1beta1.MemberAgent,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							LastTransitionTime: metav1.Now(),
							ObservedGeneration: 0,
							Status:             metav1.ConditionTrue,
							Reason:             "UnhealthyCluster",
							Message:            "set to be unhealthy",
						},
					},
					LastReceivedHeartbeat: metav1.NewTime(time.Now().Add(time.Minute * (-20))),
				},
			},
		}

		return hubClient.Status().Update(ctx, &memberCluster)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update member cluster status")

	// Create a member cluster object that represents the cluster that has left the fleet.
	//
	// Note that we use a custom finalizer to block the member cluster's deletion.
	mcObj = &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       memberCluster5LeftName,
			Finalizers: []string{customDeletionBlockerFinalizer},
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Name:      hubClusterSAName,
				Kind:      "ServiceAccount",
				Namespace: fleetSystemNS,
			},
		},
	}
	Expect(hubClient.Create(ctx, mcObj)).To(Succeed(), "Failed to create member cluster object")
	Expect(hubClient.Delete(ctx, mcObj)).To(Succeed(), "Failed to delete member cluster object")
}

func cleanupInvalidClusters() {
	invalidClusterNames := []string{memberCluster4UnhealthyName, memberCluster5LeftName}
	for _, name := range invalidClusterNames {
		mcObj := &clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
		Expect(hubClient.Delete(ctx, mcObj)).To(Succeed(), "Failed to delete member cluster object")

		Expect(hubClient.Get(ctx, types.NamespacedName{Name: name}, mcObj)).To(Succeed(), "Failed to get member cluster object")
		mcObj.Finalizers = []string{}
		Expect(hubClient.Update(ctx, mcObj)).To(Succeed(), "Failed to update member cluster object")

		Eventually(func() error {
			mcObj := &clusterv1beta1.MemberCluster{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, mcObj); !k8serrors.IsNotFound(err) {
				return fmt.Errorf("member cluster still exists or an unexpected error occurred: %w", err)
			}

			return nil
		})
	}
}

// createResourcesForFleetGuardRail create resources required for guard rail E2Es.
func createResourcesForFleetGuardRail() {
	cr := rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster-role",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"*"},
				Verbs:     []string{"*"},
				Resources: []string{"*"},
			},
		},
	}
	Eventually(func() error {
		return hubClient.Create(ctx, &cr)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "failed to create cluster role %s for fleet guard rail E2E", cr.Name)

	crb := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster-role-binding",
		},
		Subjects: []rbacv1.Subject{
			{
				APIGroup: rbacv1.GroupName,
				Kind:     "User",
				Name:     testUser,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "test-cluster-role",
		},
	}

	Eventually(func() error {
		return hubClient.Create(ctx, &crb)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "failed to create cluster role binding %s for fleet guard rail E2E", crb.Name)
}

// deleteResourcesForFleetGuardRail deletes resources created for guard rail E2Es.
func deleteResourcesForFleetGuardRail() {
	crb := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster-role-binding",
		},
	}
	Expect(hubClient.Delete(ctx, &crb)).Should(Succeed())

	cr := rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster-role",
		},
	}
	Expect(hubClient.Delete(ctx, &cr)).Should(Succeed())
}

func createMemberClusterResource(name, user string) {
	// Create the MC.
	mc := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Name:      user,
				Kind:      "ServiceAccount",
				Namespace: utils.FleetSystemNamespace,
			},
			HeartbeatPeriodSeconds: 60,
		},
	}
	Expect(hubClient.Create(ctx, mc)).To(Succeed(), "Failed to create MC %s", mc)
}

func deleteMemberClusterResource(name string) {
	Eventually(func(g Gomega) error {
		var mc clusterv1beta1.MemberCluster
		err := hubClient.Get(ctx, types.NamespacedName{Name: name}, &mc)
		if k8serrors.IsNotFound(err) {
			return nil
		}
		g.Expect(err).Should(Succeed(), "Failed to get MC %s", name)
		controllerutil.RemoveFinalizer(&mc, placementv1beta1.MemberClusterFinalizer)
		err = hubClient.Update(ctx, &mc)
		if k8serrors.IsConflict(err) {
			return err
		}
		g.Expect(hubClient.Delete(ctx, &mc)).Should(Succeed())
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed())

	Eventually(func(g Gomega) error {
		var mc clusterv1beta1.MemberCluster
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, &mc); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("MC still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed())
}

func checkInternalMemberClusterExists(name, namespace string) {
	imc := &clusterv1beta1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	Eventually(func() error {
		return hubClient.Get(ctx, types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed())
}

func checkMemberClusterNamespaceIsDeleted(name string) {
	Eventually(func(g Gomega) error {
		var ns corev1.Namespace
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, &ns); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("member cluster namespace %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed())
}

func setupNetworkingCRDs() {
	var internalServiceExportCRD, serviceImportCRD apiextensionsv1.CustomResourceDefinition
	Expect(utils.GetObjectFromManifest("./manifests/internalserviceexport-crd.yaml", &internalServiceExportCRD)).Should(Succeed())
	Expect(utils.GetObjectFromManifest("./manifests/serviceimport-crd.yaml", &serviceImportCRD)).Should(Succeed())
	Expect(hubClient.Create(ctx, &internalServiceExportCRD)).Should(Succeed())
	Expect(hubClient.Create(ctx, &serviceImportCRD)).Should(Succeed())
	By("Networking CRDs are created")

	Eventually(func(g Gomega) error {
		return hubClient.Get(ctx, types.NamespacedName{Name: "internalserviceexports.networking.fleet.azure.com"}, &internalServiceExportCRD)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	Eventually(func(g Gomega) error {
		return hubClient.Get(ctx, types.NamespacedName{Name: "serviceimports.networking.fleet.azure.com"}, &serviceImportCRD)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed())
}

func cleanupNetworkingCRDs() {
	internalServiceExportCRD := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "internalserviceexports.networking.fleet.azure.com",
		},
	}
	serviceImportCRD := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "serviceimports.networking.fleet.azure.com",
		},
	}
	Expect(hubClient.Delete(ctx, &internalServiceExportCRD)).Should(Succeed())
	Expect(hubClient.Delete(ctx, &serviceImportCRD)).Should(Succeed())
	By("Networking CRDs are deleted")
}

func createInternalServiceExport(name, namespace string) {
	ise := fleetnetworkingv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: fleetnetworkingv1alpha1.InternalServiceExportSpec{
			Ports: []fleetnetworkingv1alpha1.ServicePort{
				{
					Protocol: corev1.ProtocolTCP,
					Port:     4848,
				},
			},
			ServiceReference: fleetnetworkingv1alpha1.ExportedObjectReference{
				NamespacedName:  "test-svc",
				ResourceVersion: "test-resource-version",
				ClusterID:       "member-1",
				ExportedSince:   metav1.NewTime(time.Now().Round(time.Second)),
			},
		},
	}
	// can return no kind match error.
	Eventually(func(g Gomega) error {
		return hubClient.Create(ctx, &ise)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed())
}

func createWorkResource(name, namespace string) {
	testDeployment := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Deployment",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: utilrand.String(10),
					Kind:       utilrand.String(10),
					Name:       utilrand.String(10),
					UID:        types.UID(utilrand.String(10)),
				},
			},
		},
	}
	deploymentBytes, err := json.Marshal(testDeployment)
	Expect(err).Should(Succeed())
	w := placementv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: placementv1beta1.WorkSpec{
			Workload: placementv1beta1.WorkloadTemplate{
				Manifests: []placementv1beta1.Manifest{
					{
						RawExtension: runtime.RawExtension{
							Raw: deploymentBytes,
						},
					},
				},
			},
		},
	}
	Expect(hubClient.Create(ctx, &w)).Should(Succeed())
}

func deleteWorkResource(name, namespace string) {
	w := placementv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	Expect(hubClient.Delete(ctx, &w)).Should(Succeed())

	Eventually(func(g Gomega) error {
		var w placementv1beta1.Work
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &w); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("work still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed())
}

// createWorkResources creates some resources on the hub cluster for testing purposes.
func createWorkResources() {
	ns := workNamespace()
	Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Namespace)

	configMap := appConfigMap()
	Expect(hubClient.Create(ctx, &configMap)).To(Succeed(), "Failed to create config map %s", configMap.Name)
}

// cleanupWorkResources deletes the resources created by createWorkResources and waits until the resources are not found.
func cleanupWorkResources() {
	cleanWorkResourcesOnCluster(hubCluster)
}

func cleanWorkResourcesOnCluster(cluster *framework.Cluster) {
	ns := workNamespace()
	Expect(client.IgnoreNotFound(cluster.KubeClient.Delete(ctx, &ns))).To(Succeed(), "Failed to delete namespace %s", ns.Namespace)

	workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(cluster)
	Eventually(workResourcesRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from %s cluster", cluster.ClusterName)
}

// setAllMemberClustersToLeave sets all member clusters to leave the fleet.
func setAllMemberClustersToLeave() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		mcObj := &clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberCluster.ClusterName,
			},
		}
		Expect(client.IgnoreNotFound(hubClient.Delete(ctx, mcObj))).To(Succeed(), "Failed to set member cluster to leave state")
	}
}

func checkIfAllMemberClustersHaveLeft() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		Eventually(func() error {
			mcObj := &clusterv1beta1.MemberCluster{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: memberCluster.ClusterName}, mcObj); !k8serrors.IsNotFound(err) {
				return fmt.Errorf("member cluster still exists or an unexpected error occurred: %w", err)
			}

			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete member cluster")
	}
}

func checkIfPlacedWorkResourcesOnAllMemberClusters() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
		Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
	}
}

func checkIfRemovedWorkResourcesFromAllMemberClusters() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster)
		Eventually(workResourcesRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from member cluster %s", memberCluster.ClusterName)
	}
}

// cleanupCRP deletes the CRP and waits until the resources are not found.
func cleanupCRP(name string) {
	// TODO(Arvindthiru): There is a conflict which requires the Eventually block, not sure of series of operations that leads to it yet.
	Eventually(func() error {
		crp := &placementv1beta1.ClusterResourcePlacement{}
		err := hubClient.Get(ctx, types.NamespacedName{Name: name}, crp)
		if k8serrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}

		// Delete the CRP (again, if applicable).
		//
		// This helps the After All node to run successfully even if the steps above fail early.
		if err := hubClient.Delete(ctx, crp); err != nil {
			return err
		}

		crp.Finalizers = []string{}
		return hubClient.Update(ctx, crp)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete CRP %s", name)

	// Wait until the CRP is removed.
	removedActual := crpRemovedActual()
	Eventually(removedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove CRP %s", name)
}

func ensureCRPAndRelatedResourcesDeletion(crpName string, memberClusters []*framework.Cluster) {
	// Delete the CRP.
	crp := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpName,
		},
	}
	Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP")

	// Verify that all resources placed have been removed from specified member clusters.
	for idx := range memberClusters {
		memberCluster := memberClusters[idx]

		workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster)
		Eventually(workResourcesRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from member cluster %s", memberCluster.ClusterName)
	}

	// Verify that related finalizers have been removed from the CRP.
	finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
	Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP")

	// Remove the custom deletion blocker finalizer from the CRP.
	cleanupCRP(crpName)

	// Delete the created resources.
	cleanupWorkResources()
}
