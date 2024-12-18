/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	imcv1beta1 "go.goms.io/fleet/pkg/controllers/internalmembercluster/v1beta1"
	"go.goms.io/fleet/pkg/controllers/work"
	"go.goms.io/fleet/pkg/propertyprovider"
	"go.goms.io/fleet/pkg/propertyprovider/azure"
	"go.goms.io/fleet/pkg/propertyprovider/azure/trackers"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	testv1alpha1 "go.goms.io/fleet/test/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

var (
	croTestAnnotationKey    = "cro-test-annotation"
	croTestAnnotationValue  = "cro-test-annotation-val"
	croTestAnnotationKey1   = "cro-test-annotation1"
	croTestAnnotationValue1 = "cro-test-annotation-val1"
	roTestAnnotationKey     = "ro-test-annotation"
	roTestAnnotationValue   = "ro-test-annotation-val"
	roTestAnnotationKey1    = "ro-test-annotation1"
	roTestAnnotationValue1  = "ro-test-annotation-val1"
)

// createMemberCluster creates a MemberCluster object.
func createMemberCluster(name, svcAccountName string, labels, annotations map[string]string) {
	mcObj := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Name:      svcAccountName,
				Kind:      "ServiceAccount",
				Namespace: fleetSystemNS,
			},
			HeartbeatPeriodSeconds: 60,
		},
	}
	Expect(hubClient.Create(ctx, mcObj)).To(Succeed(), "Failed to create member cluster object %s", name)
}

// markMemberClusterAsHealthy marks the specified member cluster as healthy.
func markMemberClusterAsHealthy(name string) {
	Eventually(func() error {
		imcObj := &clusterv1beta1.InternalMemberCluster{}
		mcReservedNS := fmt.Sprintf(utils.NamespaceNameFormat, name)
		if err := hubClient.Get(ctx, types.NamespacedName{Namespace: mcReservedNS, Name: name}, imcObj); err != nil {
			return err
		}

		imcObj.Status = clusterv1beta1.InternalMemberClusterStatus{
			AgentStatus: []clusterv1beta1.AgentStatus{
				{
					Type: clusterv1beta1.MemberAgent,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							LastTransitionTime: metav1.Now(),
							ObservedGeneration: 0,
							Status:             metav1.ConditionTrue,
							Reason:             "JoinedCluster",
							Message:            "set to be joined",
						},
						{
							Type:               string(clusterv1beta1.AgentHealthy),
							LastTransitionTime: metav1.Now(),
							ObservedGeneration: 0,
							Status:             metav1.ConditionTrue,
							Reason:             "HealthyCluster",
							Message:            "set to be healthy",
						},
					},
					LastReceivedHeartbeat: metav1.Now(),
				},
			},
		}

		return hubClient.Status().Update(ctx, imcObj)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark member cluster as healthy")
}

// markMemberClusterAsUnhealthy marks the specified member cluster as unhealthy (last
// received heartbeat expired).
func markMemberClusterAsUnhealthy(name string) {
	Eventually(func() error {
		mcObj := &clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, mcObj); err != nil {
			return err
		}

		mcObj.Status = clusterv1beta1.MemberClusterStatus{
			AgentStatus: []clusterv1beta1.AgentStatus{
				{
					Type: clusterv1beta1.MemberAgent,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour * 24)),
							ObservedGeneration: 0,
							Status:             metav1.ConditionTrue,
							Reason:             "JoinedCluster",
							Message:            "set to be joined",
						},
						{
							Type:               string(clusterv1beta1.AgentHealthy),
							LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour * 24)),
							ObservedGeneration: 0,
							Status:             metav1.ConditionTrue,
							Reason:             "HealthyCluster",
							Message:            "set to be healthy",
						},
					},
					LastReceivedHeartbeat: metav1.NewTime(time.Now().Add(-time.Hour * 24)),
				},
			},
		}
		return hubClient.Status().Update(ctx, mcObj)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark member cluster as unhealthy")
}

// markMemberClusterAsLeft marks the specified member cluster as left.
func markMemberClusterAsLeft(name string) {
	mcObj := &clusterv1beta1.MemberCluster{}
	Eventually(func() error {
		// Add a custom deletion blocker finalizer to the member cluster.
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, mcObj); err != nil {
			return err
		}

		mcObj.Finalizers = append(mcObj.Finalizers, customDeletionBlockerFinalizer)
		return hubClient.Update(ctx, mcObj)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark member cluster as left")

	Expect(hubClient.Delete(ctx, mcObj)).To(Succeed(), "Failed to delete member cluster")
}

// setAllMemberClustersToJoin creates a MemberCluster object for each member cluster.
func setAllMemberClustersToJoin() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]
		createMemberCluster(memberCluster.ClusterName, memberCluster.PresentingServiceAccountInHubClusterName, labelsByClusterName[memberCluster.ClusterName], annotationsByClusterName[memberCluster.ClusterName])
	}
}

// checkIfAllMemberClustersHaveJoined verifies if all member clusters have connected to the hub
// cluster, i.e., updated the MemberCluster object status as expected.
func checkIfAllMemberClustersHaveJoined() {
	for idx := range allMemberClusters {
		checkIfMemberClusterHasJoined(allMemberClusters[idx])
	}
}

// checkIfMemberClusterHasJoined verifies if the specified member cluster has connected to the hub
// cluster, i.e., updated the MemberCluster object status as expected.
func checkIfMemberClusterHasJoined(memberCluster *framework.Cluster) {
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
			utils.IgnoreConditionLTTAndMessageFields,
			ignoreAgentStatusHeartbeatField,
		); diff != "" {
			return fmt.Errorf("agent status diff (-got, +want): %s", diff)
		}

		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Member cluster has not joined yet")
}

// checkIfAzurePropertyProviderIsWorking verifies if all member clusters have the Azure property
// provider set up as expected along with the Fleet member agent. This setup uses it
// to verify the behavior of property-based scheduling.
//
// Note that this check applies only to the test environment that features the Azure property
// provider, as indicated by the PROPERTY_PROVIDER environment variable; for setup that
// does not use it, the check will always pass.
func checkIfAzurePropertyProviderIsWorking() {
	if !isAzurePropertyProviderEnabled {
		return
	}

	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]
		Eventually(func() error {
			mcObj := &clusterv1beta1.MemberCluster{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: memberCluster.ClusterName}, mcObj); err != nil {
				return fmt.Errorf("failed to get member cluster %s: %w", memberCluster.ClusterName, err)
			}

			// Summarize the AKS cluster properties.
			wantStatus, err := summarizeAKSClusterProperties(memberCluster, mcObj)
			if err != nil {
				return fmt.Errorf("failed to summarize AKS cluster properties for member cluster %s: %w", memberCluster.ClusterName, err)
			}

			// Diff different sections separately as the status object is large and might lead
			// the diff output (if any) to omit certain fields.

			// Diff the non-resource properties.
			if diff := cmp.Diff(
				mcObj.Status.Properties, wantStatus.Properties,
				ignoreTimeTypeFields,
			); diff != "" {
				return fmt.Errorf("member cluster status properties diff (-got, +want):\n%s", diff)
			}

			// Diff the resource usage.
			if diff := cmp.Diff(
				mcObj.Status.ResourceUsage, wantStatus.ResourceUsage,
				ignoreTimeTypeFields,
			); diff != "" {
				return fmt.Errorf("member cluster status resource usage diff (-got, +want):\n%s", diff)
			}

			// Diff the conditions.
			if diff := cmp.Diff(
				mcObj.Status.Conditions, wantStatus.Conditions,
				ignoreMemberClusterJoinAndPropertyProviderStartedConditions,
				utils.IgnoreConditionLTTAndMessageFields, ignoreConditionReasonField,
				ignoreTimeTypeFields,
			); diff != "" {
				return fmt.Errorf("member cluster status conditions diff (-got, +want):\n%s", diff)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to confirm that Azure property provider is up and running")
	}
}

// summarizeAKSClusterProperties returns the current cluster state, specifically its node count,
// total/allocatable/available capacity, and the average per CPU and per GB of memory costs, in
// the form of the a member cluster status object; the E2E test suite uses this information
// to verify if the Azure property provider is working correctly.
func summarizeAKSClusterProperties(memberCluster *framework.Cluster, mcObj *clusterv1beta1.MemberCluster) (*clusterv1beta1.MemberClusterStatus, error) {
	c := memberCluster.KubeClient
	nodeList := &corev1.NodeList{}
	if err := c.List(ctx, nodeList); err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	nodeCount := len(nodeList.Items)
	if nodeCount == 0 {
		// No nodes are found; terminate the summarization early.
		return nil, fmt.Errorf("no nodes are found")
	}

	totalCPUCapacity := resource.Quantity{}
	totalMemoryCapacity := resource.Quantity{}
	allocatableCPUCapacity := resource.Quantity{}
	allocatableMemoryCapacity := resource.Quantity{}
	totalHourlyRate := 0.0
	for idx := range nodeList.Items {
		node := nodeList.Items[idx]

		totalCPUCapacity.Add(node.Status.Capacity[corev1.ResourceCPU])
		totalMemoryCapacity.Add(node.Status.Capacity[corev1.ResourceMemory])
		allocatableCPUCapacity.Add(node.Status.Allocatable[corev1.ResourceCPU])
		allocatableMemoryCapacity.Add(node.Status.Allocatable[corev1.ResourceMemory])

		nodeSKU := node.Labels[trackers.AKSClusterNodeSKULabelName]
		hourlyRate, found := memberCluster.PricingProvider.OnDemandPrice(nodeSKU)
		if found {
			totalHourlyRate += hourlyRate
		}
	}

	cpuCores := totalCPUCapacity.AsApproximateFloat64()
	if cpuCores < 0.001 {
		// The total CPU capacity is zero or too small; terminate the summarization early.
		return nil, fmt.Errorf("total CPU capacity is zero or too small")
	}
	memoryBytes := totalMemoryCapacity.AsApproximateFloat64()
	if memoryBytes < 0.001 {
		// The total CPU capacity is zero or too small; terminate the summarization early.
		return nil, fmt.Errorf("total memory capacity is zero or too small")
	}
	perCPUCoreCost := totalHourlyRate / cpuCores
	perGBMemoryCost := totalHourlyRate / (memoryBytes / (1024 * 1024 * 1024))

	availableCPUCapacity := allocatableCPUCapacity.DeepCopy()
	availableMemoryCapacity := allocatableMemoryCapacity.DeepCopy()
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList); err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}
	for idx := range podList.Items {
		pod := podList.Items[idx]

		requestedCPUCapacity := resource.Quantity{}
		requestedMemoryCapacity := resource.Quantity{}
		for cidx := range pod.Spec.Containers {
			container := pod.Spec.Containers[cidx]
			requestedCPUCapacity.Add(container.Resources.Requests[corev1.ResourceCPU])
			requestedMemoryCapacity.Add(container.Resources.Requests[corev1.ResourceMemory])
		}

		availableCPUCapacity.Sub(requestedCPUCapacity)
		availableMemoryCapacity.Sub(requestedMemoryCapacity)
	}

	status := clusterv1beta1.MemberClusterStatus{
		Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
			propertyprovider.NodeCountProperty: {
				Value: fmt.Sprintf("%d", nodeCount),
			},
			azure.PerCPUCoreCostProperty: {
				Value: fmt.Sprintf(azure.CostPrecisionTemplate, perCPUCoreCost),
			},
			azure.PerGBMemoryCostProperty: {
				Value: fmt.Sprintf(azure.CostPrecisionTemplate, perGBMemoryCost),
			},
		},
		ResourceUsage: clusterv1beta1.ResourceUsage{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    totalCPUCapacity,
				corev1.ResourceMemory: totalMemoryCapacity,
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    allocatableCPUCapacity,
				corev1.ResourceMemory: allocatableMemoryCapacity,
			},
			Available: corev1.ResourceList{
				corev1.ResourceCPU:    availableCPUCapacity,
				corev1.ResourceMemory: availableMemoryCapacity,
			},
		},
		Conditions: []metav1.Condition{
			{
				Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: mcObj.Generation,
			},
			{
				Type:               azure.PropertyCollectionSucceededConditionType,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: mcObj.Generation,
			},
		},
	}

	return &status, nil
}

// setupInvalidClusters simulates the case where some clusters in the fleet becomes unhealthy or
// have left the fleet.
func setupInvalidClusters() {
	// Create a member cluster object that represents the unhealthy cluster.
	createMemberCluster(memberCluster4UnhealthyName, hubClusterSAName, nil, nil)

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
	mcObj := &clusterv1beta1.MemberCluster{
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
		Eventually(func() error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: name}, mcObj)
			if err != nil {
				return err
			}
			mcObj.Finalizers = []string{}
			return hubClient.Update(ctx, mcObj)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update member cluster object")
		Expect(hubClient.Delete(ctx, mcObj)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}), "Failed to delete member cluster object")
		Eventually(func() error {
			mcObj := &clusterv1beta1.MemberCluster{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, mcObj); !k8serrors.IsNotFound(err) {
				return fmt.Errorf("member cluster still exists or an unexpected error occurred: %w", err)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to check if member cluster is deleted, member cluster still exists")
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
				Name:     "test-user",
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
	Expect(hubClient.Delete(ctx, &crb)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))

	cr := rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster-role",
		},
	}
	Expect(hubClient.Delete(ctx, &cr)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
}

func deleteTestResourceCRD() {
	crd := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testresources.test.kubernetes-fleet.io",
		},
	}
	Expect(hubClient.Delete(ctx, &crd)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
}

func createTestResourceCRD() {
	var crd apiextensionsv1.CustomResourceDefinition
	readTestCustomResourceDefinition(&crd)
	Expect(hubClient.Create(ctx, &crd)).To(Succeed(), "Failed to create test custom resource definition %s", crd.Name)
	waitForCRDToBeReady(crd.Name)
}

// cleanupMemberCluster removes finalizers (if any) from the member cluster, and
// wait until its final removal.
func cleanupMemberCluster(memberClusterName string) {
	// Remove the custom deletion blocker finalizer from the member cluster.
	Eventually(func() error {
		mcObj := &clusterv1beta1.MemberCluster{}
		err := hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName}, mcObj)
		if k8serrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}

		mcObj.Finalizers = []string{}
		return hubClient.Update(ctx, mcObj)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove custom deletion blocker finalizer from member cluster")

	// Wait until the member cluster is fully removed.
	Eventually(func() error {
		mcObj := &clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName}, mcObj); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("member cluster still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to fully delete member cluster")
}

func ensureMemberClusterAndRelatedResourcesDeletion(memberClusterName string) {
	// Delete the member cluster.
	mcObj := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberClusterName,
		},
	}
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, mcObj))).To(Succeed(), "Failed to delete member cluster")

	// Remove the finalizers on the member cluster, and wait until the member cluster is fully removed.
	cleanupMemberCluster(memberClusterName)

	// Verify that the member cluster and the namespace reserved for the member cluster has been removed.
	reservedNSName := fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
	Eventually(func() error {
		ns := corev1.Namespace{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: reservedNSName}, &ns); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("namespace still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove reserved namespace")
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

// createWorkResources creates some resources on the hub cluster for testing purposes.
func createWorkResources() {
	ns := appNamespace()
	Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Namespace)

	configMap := appConfigMap()
	Expect(hubClient.Create(ctx, &configMap)).To(Succeed(), "Failed to create config map %s", configMap.Name)
}

// cleanupWorkResources deletes the resources created by createWorkResources and waits until the resources are not found.
func cleanupWorkResources() {
	cleanWorkResourcesOnCluster(hubCluster)
}

func cleanWorkResourcesOnCluster(cluster *framework.Cluster) {
	ns := appNamespace()
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

func checkIfPlacedWorkResourcesOnAllMemberClustersConsistently() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
		Consistently(workResourcesPlacedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
	}
}

func checkIfPlacedNamespaceResourceOnAllMemberClusters() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		namespaceResourcePlacedActual := workNamespacePlacedOnClusterActual(memberCluster)
		Eventually(namespaceResourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work namespace on member cluster %s", memberCluster.ClusterName)
	}
}

func checkIfRemovedWorkResourcesFromAllMemberClusters() {
	checkIfRemovedWorkResourcesFromMemberClusters(allMemberClusters)
}

func checkIfRemovedWorkResourcesFromMemberClusters(clusters []*framework.Cluster) {
	for idx := range clusters {
		memberCluster := clusters[idx]

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
	removedActual := crpRemovedActual(name)
	Eventually(removedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove CRP %s", name)
}

// createResourceOverrides creates a number of resource overrides.
func createResourceOverrides(namespace string, number int) {
	for i := 0; i < number; i++ {
		ro := &placementv1alpha1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf(roNameTemplate, i),
				Namespace: namespace,
			},
			Spec: placementv1alpha1.ResourceOverrideSpec{
				ResourceSelectors: []placementv1alpha1.ResourceSelector{
					{
						Group:   "apps",
						Kind:    "Deployment",
						Version: "v1",
						Name:    fmt.Sprintf("test-deployment-%d", i),
					},
				},
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"key": "value",
											},
										},
									},
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"key1": "value1",
											},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
									Path:     "/meta/labels/test-key",
								},
							},
						},
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, ro)).Should(Succeed(), "Failed to create ResourceOverride %s", ro.Name)
	}
}

// createClusterResourceOverrides creates a number of cluster resource overrides.
func createClusterResourceOverrides(number int) {
	for i := 0; i < number; i++ {
		cro := &placementv1alpha1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf(croNameTemplate, i),
			},
			Spec: placementv1alpha1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
					{
						Group:   "rbac.authorization.k8s.io/v1",
						Kind:    "ClusterRole",
						Version: "v1",
						Name:    fmt.Sprintf("test-cluster-role-%d", i),
					},
				},
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"key": "value",
											},
										},
									},
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"key1": "value1",
											},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
									Path:     "/meta/labels/test-key",
								},
							},
						},
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, cro)).Should(Succeed(), "Failed to create ClusterResourceOverride %s", cro.Name)
	}
}

func ensureCRPAndRelatedResourcesDeletion(crpName string, memberClusters []*framework.Cluster) {
	// Delete the CRP.
	crp := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpName,
		},
	}
	Expect(hubClient.Delete(ctx, crp)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}), "Failed to delete CRP")

	// Verify that all resources placed have been removed from specified member clusters.
	for idx := range memberClusters {
		memberCluster := memberClusters[idx]

		workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster)
		Eventually(workResourcesRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from member cluster %s", memberCluster.ClusterName)
	}

	// Verify that related finalizers have been removed from the CRP.
	finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual(crpName)
	Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP")

	// Remove the custom deletion blocker finalizer from the CRP.
	cleanupCRP(crpName)

	// Delete the created resources.
	cleanupWorkResources()
}

func ensureCRPEvictionDeletion(crpEvictionName string) {
	crpe := &placementv1alpha1.ClusterResourcePlacementEviction{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpEvictionName,
		},
	}
	Expect(hubClient.Delete(ctx, crpe)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}), "Failed to delete CRP eviction")
	removedActual := crpEvictionRemovedActual(crpEvictionName)
	Eventually(removedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "CRP eviction still exists")
}

// verifyWorkPropagationAndMarkAsAvailable verifies that works derived from a specific CPR have been created
// for a specific cluster, and marks these works in the specific member cluster's
// reserved namespace as applied and available.
//
// This is mostly used for simulating member agents for virtual clusters.
//
// Note that this utility function currently assumes that there is only one work object.
func verifyWorkPropagationAndMarkAsAvailable(memberClusterName, crpName string, resourceIdentifiers []placementv1beta1.ResourceIdentifier) {
	memberClusterReservedNS := fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
	// Wait until the works are created.
	workList := placementv1beta1.WorkList{}
	Eventually(func() error {
		workList = placementv1beta1.WorkList{}
		matchLabelOptions := client.MatchingLabels{
			placementv1beta1.CRPTrackingLabel: crpName,
		}
		if err := hubClient.List(ctx, &workList, client.InNamespace(memberClusterReservedNS), matchLabelOptions); err != nil {
			return err
		}

		if len(workList.Items) == 0 {
			return fmt.Errorf("no works found in namespace %s", memberClusterReservedNS)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to list works")

	for _, item := range workList.Items {
		workName := item.Name
		// To be on the safer set, update the status with retries.
		Eventually(func() error {
			w := placementv1beta1.Work{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: memberClusterReservedNS}, &w); err != nil {
				return err
			}

			// Set the resource applied condition to the item object.
			meta.SetStatusCondition(&w.Status.Conditions, metav1.Condition{
				Type:               placementv1beta1.WorkConditionTypeApplied,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             condition.AllWorkAvailableReason,
				Message:            "Set to be applied",
				ObservedGeneration: w.Generation,
			})

			meta.SetStatusCondition(&w.Status.Conditions, metav1.Condition{
				Type:               placementv1beta1.WorkConditionTypeAvailable,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             work.WorkAvailableReason,
				Message:            "Set to be available",
				ObservedGeneration: w.Generation,
			})

			// Set the manifest conditions.
			//
			// Currently the CRP controller ignores this setup if the applied condition has been
			// set as expected (i.e., resources applied). Here it adds the manifest conditions
			// just in case the CRP controller changes its behavior in the future.
			for idx := range resourceIdentifiers {
				resourceIdentifier := resourceIdentifiers[idx]
				w.Status.ManifestConditions = append(w.Status.ManifestConditions, placementv1beta1.ManifestCondition{
					Identifier: placementv1beta1.WorkResourceIdentifier{
						Group:     resourceIdentifier.Group,
						Kind:      resourceIdentifier.Kind,
						Version:   resourceIdentifier.Version,
						Name:      resourceIdentifier.Name,
						Namespace: resourceIdentifier.Namespace,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							// Typically, this field is set to be the generation number of the
							// applied object; here a dummy value is used as there is no object
							// actually being applied in the case.
							ObservedGeneration: 0,
							Reason:             "ManifestApplied",
							Message:            "Set to be applied",
						},
						{
							Type:               placementv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							// Typically, this field is set to be the generation number of the
							// applied object; here a dummy value is used as there is no object
							// actually being applied in the case.
							ObservedGeneration: 0,
							Reason:             "ManifestAvailable",
							Message:            "Set to be available",
						},
					},
				})
			}

			return hubClient.Status().Update(ctx, &w)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	}
}

func buildTaints(memberClusterNames []string) []clusterv1beta1.Taint {
	var taint map[string]string
	taints := make([]clusterv1beta1.Taint, len(memberClusterNames))
	for i, name := range memberClusterNames {
		taint = taintTolerationMap[name]
		taints[i].Key = regionLabelName
		taints[i].Value = taint[regionLabelName]
		taints[i].Effect = corev1.TaintEffectNoSchedule
	}
	return taints
}

func buildTolerations(memberClusterNames []string) []placementv1beta1.Toleration {
	var toleration map[string]string
	tolerations := make([]placementv1beta1.Toleration, len(memberClusterNames))
	for i, name := range memberClusterNames {
		toleration = taintTolerationMap[name]
		tolerations[i].Key = regionLabelName
		tolerations[i].Operator = corev1.TolerationOpEqual
		tolerations[i].Value = toleration[regionLabelName]
		tolerations[i].Effect = corev1.TaintEffectNoSchedule
	}
	return tolerations
}

func addTaintsToMemberClusters(memberClusterNames []string, taints []clusterv1beta1.Taint) {
	for i, clusterName := range memberClusterNames {
		Eventually(func() error {
			var mc clusterv1beta1.MemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: clusterName}, &mc)
			if err != nil {
				return err
			}
			mc.Spec.Taints = []clusterv1beta1.Taint{taints[i]}
			return hubClient.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add taints to member cluster %s", clusterName)
	}
}

func removeTaintsFromMemberClusters(memberClusterNames []string) {
	for _, clusterName := range memberClusterNames {
		Eventually(func() error {
			var mc clusterv1beta1.MemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: clusterName}, &mc)
			if err != nil {
				return err
			}
			mc.Spec.Taints = nil
			return hubClient.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove taints from member cluster %s", clusterName)
	}
}

func updateCRPWithTolerations(tolerations []placementv1beta1.Toleration) {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	Eventually(func() error {
		var crp placementv1beta1.ClusterResourcePlacement
		err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp)
		if err != nil {
			return err
		}
		if crp.Spec.Policy == nil {
			crp.Spec.Policy = &placementv1beta1.PlacementPolicy{
				Tolerations: tolerations,
			}
		} else {
			crp.Spec.Policy.Tolerations = tolerations
		}
		return hubClient.Update(ctx, &crp)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement with tolerations %s", crpName)
}

func cleanupClusterResourceOverride(name string) {
	cro := &placementv1alpha1.ClusterResourceOverride{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, cro))).To(Succeed(), "Failed to delete clusterResourceOverride %s", name)
	Eventually(func() error {
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, &placementv1alpha1.ClusterResourceOverride{}); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("clusterResourceOverride %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove clusterResourceOverride %s from hub cluster", name)
}

func cleanupResourceOverride(name string, namespace string) {
	ro := &placementv1alpha1.ResourceOverride{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, ro))).To(Succeed(), "Failed to delete resourceOverride %s", name)
	Eventually(func() error {
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &placementv1alpha1.ResourceOverride{}); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("resourceOverride %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove resourceOverride %s from hub cluster", name)
}

func checkIfOverrideAnnotationsOnAllMemberClusters(includeNamespace bool, wantAnnotations map[string]string) {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]
		if includeNamespace {
			Expect(validateAnnotationOfWorkNamespaceOnCluster(memberCluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", memberCluster.ClusterName)
		}
		Expect(validateOverrideAnnotationOfConfigMapOnCluster(memberCluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of config map on %s", memberCluster.ClusterName)
	}
}

func readTestCustomResource(customResource *testv1alpha1.TestResource) {
	By("Read the custom resource")
	err := utils.GetObjectFromManifest("../manifests/test-resource.yaml", customResource)
	customResource.Name = fmt.Sprintf("%s-%d", customResource.Name, GinkgoParallelProcess())
	Expect(err).Should(Succeed())
}

func readTestCustomResourceDefinition(crd *apiextensionsv1.CustomResourceDefinition) {
	By("Read the custom resource definition")
	err := utils.GetObjectFromManifest("../manifests/test_testresources_crd.yaml", crd)
	Expect(err).Should(Succeed())
}

func readDeploymentTestManifest(testDeployment *appsv1.Deployment) {
	By("Read the deployment resource")
	err := utils.GetObjectFromManifest("resources/test-deployment.yaml", testDeployment)
	Expect(err).Should(Succeed())
}

func readDaemonSetTestManifest(testDaemonSet *appsv1.DaemonSet) {
	By("Read the daemonSet resource")
	err := utils.GetObjectFromManifest("resources/test-daemonset.yaml", testDaemonSet)
	Expect(err).Should(Succeed())
}

func readStatefulSetTestManifest(testStatefulSet *appsv1.StatefulSet, withVolume bool) {
	By("Read the statefulSet resource")
	if withVolume {
		Expect(utils.GetObjectFromManifest("resources/statefulset-with-volume.yaml", testStatefulSet)).Should(Succeed())
	} else {
		Expect(utils.GetObjectFromManifest("resources/test-statefulset.yaml", testStatefulSet)).Should(Succeed())
	}
}

func readServiceTestManifest(testService *corev1.Service) {
	By("Read the service resource")
	err := utils.GetObjectFromManifest("resources/test-service.yaml", testService)
	Expect(err).Should(Succeed())
}

func readJobTestManifest(testManifest *batchv1.Job) {
	By("Read the job resource")
	err := utils.GetObjectFromManifest("resources/test-job.yaml", testManifest)
	Expect(err).Should(Succeed())
}

func readEnvelopeConfigMapTestManifest(testEnvelopeObj *corev1.ConfigMap) {
	By("Read testEnvelopConfigMap resource")
	err := utils.GetObjectFromManifest("resources/test-envelope-object.yaml", testEnvelopeObj)
	Expect(err).Should(Succeed())
}

// constructWrappedResources fill the enveloped resource with the workload object
func constructWrappedResources(testEnvelopeObj *corev1.ConfigMap, workloadObj metav1.Object, kind string, namespace corev1.Namespace) {
	// modify the enveloped configMap according to the namespace
	testEnvelopeObj.Namespace = namespace.Name

	// modify the embedded namespaced resource according to the namespace
	workloadObj.SetNamespace(namespace.Name)
	workloadObjectByte, err := json.Marshal(workloadObj)
	Expect(err).Should(Succeed())
	switch kind {
	case utils.DeploymentKind:
		testEnvelopeObj.Data["deployment.yaml"] = string(workloadObjectByte)
	case utils.DaemonSetKind:
		testEnvelopeObj.Data["daemonset.yaml"] = string(workloadObjectByte)
	case utils.StatefulSetKind:
		testEnvelopeObj.Data["statefulset.yaml"] = string(workloadObjectByte)
	}
}

// checkIfStatusErrorWithMessage checks if the error is a status error and if error contains the error message.
func checkIfStatusErrorWithMessage(err error, errorMsg string) error {
	var statusErr *k8serrors.StatusError
	if errors.As(err, &statusErr) {
		if strings.Contains(statusErr.ErrStatus.Message, errorMsg) {
			return nil
		}
	}
	return fmt.Errorf("error message %s not found in error %w", errorMsg, err)
}
