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
	"math"
	"strconv"
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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	imcv1beta1 "github.com/kubefleet-dev/kubefleet/pkg/controllers/internalmembercluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workapplier"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider/azure"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider/azure/trackers"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	testv1alpha1 "github.com/kubefleet-dev/kubefleet/test/apis/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

// StatefulSetVariant represents different StatefulSet configurations for testing
type StatefulSetVariant int

const (
	// StatefulSetBasic is a StatefulSet without any persistent volume claims
	StatefulSetBasic StatefulSetVariant = iota
	// StatefulSetInvalidStorage is a StatefulSet with a non-existent storage class
	StatefulSetInvalidStorage
	// StatefulSetWithStorage is a StatefulSet with a valid standard storage class
	StatefulSetWithStorage
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
			HeartbeatPeriodSeconds: memberClusterHeartbeatPeriodSeconds,
		},
	}
	Expect(hubClient.Create(ctx, mcObj)).To(SatisfyAny(&utils.AlreadyExistMatcher{}, Succeed()), "Failed to create member cluster object %s", name)
}

func updateMemberClusterDeleteOptions(name string, deleteOptions *clusterv1beta1.DeleteOptions) {
	Eventually(func() error {
		mcObj := &clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, mcObj); err != nil {
			return err
		}

		mcObj.Spec.DeleteOptions = deleteOptions
		return hubClient.Update(ctx, mcObj)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update member cluster delete options")
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

// markMemberClusterAsLeaving marks the specified member cluster as leaving.
func markMemberClusterAsLeaving(name string) {
	mcObj := &clusterv1beta1.MemberCluster{}
	Eventually(func() error {
		// Add a custom deletion blocker finalizer to the member cluster.
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, mcObj); err != nil {
			return err
		}

		mcObj.Finalizers = append(mcObj.Finalizers, customDeletionBlockerFinalizer)
		return hubClient.Update(ctx, mcObj)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add finalizer")

	Expect(hubClient.Delete(ctx, mcObj)).To(Succeed(), "Failed to mark member cluster as leaving")
}

// markMemberClusterAsLeft deletes the specified member cluster.
func markMemberClusterAsLeft(name string) {
	mcObj := &clusterv1beta1.MemberCluster{}
	Eventually(func() error {
		// remove finalizer to the member cluster.
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, mcObj); err != nil {
			return err
		}
		if len(mcObj.Finalizers) > 0 {
			mcObj.Finalizers = []string{}
			return hubClient.Update(ctx, mcObj)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove finalizer")

	Expect(hubClient.Delete(ctx, mcObj)).To(SatisfyAny(Succeed(), utils.NotFoundMatcher{}), "Failed to delete member cluster")
}

// setMemberClusterToJoin creates a MemberCluster object for a specific member cluster.
func setMemberClusterToJoin(memberCluster *framework.Cluster) {
	createMemberCluster(memberCluster.ClusterName, memberCluster.PresentingServiceAccountInHubClusterName, labelsByClusterName[memberCluster.ClusterName], annotationsByClusterName[memberCluster.ClusterName])
}

// setAllMemberClustersToJoin creates a MemberCluster object for each member cluster.
func setAllMemberClustersToJoin() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]
		setMemberClusterToJoin(memberCluster)
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
	}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Member cluster has not joined yet")
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

			// Cost properties are checked separately to account for approximation margins.
			ignoreCostProperties := cmpopts.IgnoreMapEntries(func(k clusterv1beta1.PropertyName, v clusterv1beta1.PropertyValue) bool {
				return k == azure.PerCPUCoreCostProperty || k == azure.PerGBMemoryCostProperty
			})
			// we don't know the exact value of k8s version and cluster entry point
			ignoreClusterProperties := cmpopts.IgnoreMapEntries(func(k clusterv1beta1.PropertyName, v clusterv1beta1.PropertyValue) bool {
				return k == propertyprovider.K8sVersionProperty || k == propertyprovider.ClusterCertificateAuthorityProperty
			})
			if diff := cmp.Diff(
				mcObj.Status.Properties, wantStatus.Properties,
				ignoreTimeTypeFields,
				ignoreCostProperties,
				ignoreClusterProperties,
			); diff != "" {
				return fmt.Errorf("member cluster status properties diff (-got, +want):\n%s", diff)
			}

			// Check the cost properties separately.
			//
			// The test suite consider cost outputs with a margin of no more than 0.002 to be acceptable.
			perCPUCoreCostProperty, found := mcObj.Status.Properties[azure.PerCPUCoreCostProperty]
			wantPerCPUCoreCostProperty, wantFound := wantStatus.Properties[azure.PerCPUCoreCostProperty]
			if found != wantFound {
				return fmt.Errorf("member cluster per CPU core cost property diff: found=%v, wantFound=%v", found, wantFound)
			}
			perCPUCoreCost, err := strconv.ParseFloat(perCPUCoreCostProperty.Value, 64)
			wantPerCPUCoreCost, wantErr := strconv.ParseFloat(wantPerCPUCoreCostProperty.Value, 64)
			if err != nil || wantErr != nil {
				return fmt.Errorf("failed to parse per CPU core cost property: val=%s, err=%w, wantVal=%s, wantErr=%w", perCPUCoreCostProperty.Value, err, wantPerCPUCoreCostProperty.Value, wantErr)
			}
			if diff := math.Abs(perCPUCoreCost - wantPerCPUCoreCost); diff > 0.002 {
				return fmt.Errorf("member cluster per CPU core cost property diff: got=%f, want=%f, diff=%f", perCPUCoreCost, wantPerCPUCoreCost, diff)
			}

			perGBMemoryCostProperty, found := mcObj.Status.Properties[azure.PerGBMemoryCostProperty]
			wantPerGBMemoryCostProperty, wantFound := wantStatus.Properties[azure.PerGBMemoryCostProperty]
			if found != wantFound {
				return fmt.Errorf("member cluster per GB memory cost property diff: found=%v, wantFound=%v", found, wantFound)
			}
			perGBMemoryCost, err := strconv.ParseFloat(perGBMemoryCostProperty.Value, 64)
			wantPerGBMemoryCost, wantErr := strconv.ParseFloat(wantPerGBMemoryCostProperty.Value, 64)
			if err != nil || wantErr != nil {
				return fmt.Errorf("failed to parse per GB memory cost property: val=%s, err=%w, wantVal=%s, wantErr=%w", perGBMemoryCostProperty.Value, err, wantPerGBMemoryCostProperty.Value, wantErr)
			}
			if diff := math.Abs(perGBMemoryCost - wantPerGBMemoryCost); diff > 0.002 {
				return fmt.Errorf("member cluster per GB memory cost property diff: got=%f, want=%f, diff=%f", perGBMemoryCost, wantPerGBMemoryCost, diff)
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
		}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to confirm that Azure property provider is up and running on cluster", memberCluster.ClusterName)
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

	nodeCountBySKU := map[string]int{}
	for idx := range nodeList.Items {
		node := nodeList.Items[idx]

		nodeSKU := node.Labels[trackers.AKSClusterNodeSKULabelName]
		if len(nodeSKU) == 0 {
			nodeSKU = trackers.ReservedNameForUndefinedSKU
		}
		nodeCountBySKU[nodeSKU]++
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
	if totalHourlyRate <= 0.002 {
		return nil, fmt.Errorf("total hourly rate is zero or too small; there might be unrecognized SKUs or incorrect pricing data")
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

	properties := map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
		propertyprovider.NodeCountProperty: {
			Value: fmt.Sprintf("%d", nodeCount),
		},
		azure.PerCPUCoreCostProperty: {
			Value: fmt.Sprintf(azure.CostPrecisionTemplate, perCPUCoreCost),
		},
		azure.PerGBMemoryCostProperty: {
			Value: fmt.Sprintf(azure.CostPrecisionTemplate, perGBMemoryCost),
		},
	}
	for sku, count := range nodeCountBySKU {
		pName := clusterv1beta1.PropertyName(fmt.Sprintf(azure.NodeCountPerSKUPropertyTmpl, sku))
		properties[pName] = clusterv1beta1.PropertyValue{
			Value: fmt.Sprintf("%d", count),
		}
	}
	status := clusterv1beta1.MemberClusterStatus{
		Properties: properties,
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
				Type:               azure.CostPropertiesCollectionSucceededCondType,
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
			if err != nil && !k8serrors.IsNotFound(err) {
				return err
			}
			mcObj.Finalizers = []string{}
			return hubClient.Update(ctx, mcObj)
		}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update member cluster object")
		Expect(hubClient.Delete(ctx, mcObj)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}), "Failed to delete member cluster object")
		Eventually(func() error {
			mcObj := &clusterv1beta1.MemberCluster{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, mcObj); !k8serrors.IsNotFound(err) {
				return fmt.Errorf("member cluster still exists or an unexpected error occurred: %w", err)
			}
			return nil
		}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to check if member cluster is deleted, member cluster still exists")
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
			if err == nil {
				return fmt.Errorf("work namespace %s still exists on cluster %s: deletion timestamp: %v, current timestamp: %v",
					ns.Name, memberClusterName, ns.GetDeletionTimestamp(), time.Now())
			}
			return fmt.Errorf("an unexpected error occurred: %w", err)
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
	createNamespace()
	createConfigMap()
}

func createNamespace() {
	var ns corev1.Namespace
	Eventually(func() error {
		ns = appNamespace()
		err := hubClient.Create(ctx, &ns)
		if k8serrors.IsAlreadyExists(err) {
			err = hubClient.Get(ctx, types.NamespacedName{Name: ns.Name}, &ns)
			if err != nil {
				return fmt.Errorf("failed to get the namespace %s, err is %+w", ns.Name, err)
			}
			return fmt.Errorf("namespace %s already exists, delete time is %v", ns.Name, ns.GetDeletionTimestamp())
		}
		return err
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create namespace %s", ns.Name)
}

func createConfigMap() {
	configMap := appConfigMap()
	Expect(hubClient.Create(ctx, &configMap)).To(Succeed(), "Failed to create config map %s", configMap.Name)
}

// cleanupWorkResources deletes the resources created by createWorkResources and waits until the resources are not found.
func cleanupWorkResources() {
	cleanWorkResourcesOnCluster(hubCluster)
}

func cleanWorkResourcesOnCluster(cluster *framework.Cluster) {
	ns := appNamespace()
	Expect(client.IgnoreNotFound(cluster.KubeClient.Delete(ctx, &ns))).To(Succeed(), "Failed to delete namespace %s", ns.Name)

	workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(cluster)
	Eventually(workResourcesRemovedActual, workloadEventuallyDuration, time.Second*5).Should(Succeed(), "Failed to remove work resources from %s cluster", cluster.ClusterName)
}

// cleanupConfigMap deletes the ConfigMap created by createWorkResources and waits until the resource is not found.
func cleanupConfigMap() {
	cleanupConfigMapOnCluster(hubCluster)
}

func cleanupConfigMapOnCluster(cluster *framework.Cluster) {
	configMap := appConfigMap()
	Expect(client.IgnoreNotFound(cluster.KubeClient.Delete(ctx, &configMap))).To(Succeed(), "Failed to delete config map %s", configMap.Name)

	configMapRemovedActual := namespacedResourcesRemovedFromClusterActual(cluster)
	Eventually(configMapRemovedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove config map from %s cluster", cluster.ClusterName)
}

// setMemberClusterToLeave sets a specific member cluster to leave the fleet.
func setMemberClusterToLeave(memberCluster *framework.Cluster) {
	mcObj := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberCluster.ClusterName,
		},
	}
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, mcObj))).To(Succeed(), "Failed to set member cluster %s to leave state", memberCluster.ClusterName)
}

// setAllMemberClustersToLeave sets all member clusters to leave the fleet.
func setAllMemberClustersToLeave() {
	for idx := range allMemberClusters {
		setMemberClusterToLeave(allMemberClusters[idx])
	}
}

func createAnotherValidOwnerReference(nsName string) metav1.OwnerReference {
	// Create a namespace to be owner.
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	Expect(allMemberClusters[0].KubeClient.Create(ctx, ns)).Should(Succeed(), "Failed to create namespace %s", nsName)

	// Get the namespace to ensure to create a valid owner reference.
	Expect(allMemberClusters[0].KubeClient.Get(ctx, types.NamespacedName{Name: nsName}, ns)).Should(Succeed(), "Failed to get namespace %s", nsName)

	return metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "Namespace",
		Name:       nsName,
		UID:        ns.UID,
	}
}

func createAnotherValidOwnerReferenceForConfigMap(namespace, configMapName string) metav1.OwnerReference {
	// Create a configmap to be owner.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			"key": "value",
		},
	}
	Expect(allMemberClusters[0].KubeClient.Create(ctx, cm)).Should(Succeed(), "Failed to create configmap %s/%s", namespace, configMapName)

	// Get the configmap to ensure to create a valid owner reference.
	Expect(allMemberClusters[0].KubeClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: namespace}, cm)).Should(Succeed(), "Failed to get configmap %s/%s", namespace, configMapName)

	return metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       configMapName,
		UID:        cm.UID,
	}
}

func cleanupAnotherValidOwnerReferenceForConfigMap(namespace, configMapName string) {
	// Cleanup the configmap created for the owner reference.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
	}
	Expect(allMemberClusters[0].KubeClient.Delete(ctx, cm)).Should(Succeed(), "Failed to delete configmap %s/%s", namespace, configMapName)
}

func cleanupAnotherValidOwnerReference(nsName string) {
	// Cleanup the namespace created for the owner reference.
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	Expect(allMemberClusters[0].KubeClient.Delete(ctx, ns)).Should(Succeed(), "Failed to create namespace %s", nsName)
}

// checkIfMemberClusterHasLeft verifies if the specified member cluster has left.
func checkIfMemberClusterHasLeft(memberCluster *framework.Cluster) {
	Eventually(func() error {
		mcObj := &clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: memberCluster.ClusterName}, mcObj); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("member cluster %s still exists or an unexpected error occurred: %w", memberCluster.ClusterName, err)
		}
		return nil
	}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete member cluster %s", memberCluster.ClusterName)
}

func checkIfAllMemberClustersHaveLeft() {
	for idx := range allMemberClusters {
		checkIfMemberClusterHasLeft(allMemberClusters[idx])
	}
}

func checkIfPlacedWorkResourcesOnMemberClustersConsistently(clusters []*framework.Cluster) {
	for idx := range clusters {
		memberCluster := clusters[idx]

		workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
		Consistently(workResourcesPlacedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s consistently", memberCluster.ClusterName)
	}
}

func checkIfPlacedWorkResourcesOnAllMemberClusters() {
	checkIfPlacedWorkResourcesOnMemberClusters(allMemberClusters)
}

func checkIfPlacedWorkResourcesOnMemberClusters(clusters []*framework.Cluster) {
	for idx := range clusters {
		memberCluster := clusters[idx]
		workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
		Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
	}
}

func checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(clusters []*framework.Cluster) {
	for idx := range clusters {
		memberCluster := clusters[idx]
		workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
		Eventually(workResourcesPlacedActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
	}
}

func checkIfPlacedNamespaceResourceOnAllMemberClusters() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		namespaceResourcePlacedActual := workNamespacePlacedOnClusterActual(memberCluster)
		Eventually(namespaceResourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work namespace on member cluster %s", memberCluster.ClusterName)
	}
}

// checkIfRemovedConfigMapFromMemberCluster verifies that the ConfigMap has been removed from the specified member cluster.
func checkIfRemovedConfigMapFromMemberClusters(clusters []*framework.Cluster) {
	for idx := range clusters {
		memberCluster := clusters[idx]

		configMapRemovedActual := namespacedResourcesRemovedFromClusterActual(memberCluster)
		Eventually(configMapRemovedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove config map from member cluster %s", memberCluster.ClusterName)
	}
}

func checkIfRemovedWorkResourcesFromAllMemberClusters() {
	checkIfRemovedWorkResourcesFromMemberClusters(allMemberClusters)
}

func checkIfRemovedWorkResourcesFromMemberClusters(clusters []*framework.Cluster) {
	for idx := range clusters {
		memberCluster := clusters[idx]

		workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster)
		Eventually(workResourcesRemovedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from member cluster %s", memberCluster.ClusterName)
	}
}

func checkIfRemovedConfigMapFromAllMemberClusters() {
	checkIfRemovedConfigMapFromMemberClusters(allMemberClusters)
}

func checkIfRemovedWorkResourcesFromAllMemberClustersConsistently() {
	checkIfRemovedWorkResourcesFromMemberClustersConsistently(allMemberClusters)
}

func checkIfRemovedWorkResourcesFromMemberClustersConsistently(clusters []*framework.Cluster) {
	for idx := range clusters {
		memberCluster := clusters[idx]

		workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster)
		Consistently(workResourcesRemovedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to remove work resources from member cluster %s consistently", memberCluster.ClusterName)
	}
}

func checkIfRemovedConfigMapFromAllMemberClustersConsistently() {
	checkIfRemovedConfigMapFromMemberClustersConsistently(allMemberClusters)
}

func checkIfRemovedConfigMapFromMemberClustersConsistently(clusters []*framework.Cluster) {
	for idx := range clusters {
		memberCluster := clusters[idx]

		configMapRemovedActual := namespacedResourcesRemovedFromClusterActual(memberCluster)
		Consistently(configMapRemovedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to remove config map from member cluster %s consistently", memberCluster.ClusterName)
	}
}

func checkNamespaceExistsWithOwnerRefOnMemberCluster(nsName, crpName string) {
	Consistently(func() error {
		ns := &corev1.Namespace{}
		if err := allMemberClusters[0].KubeClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err != nil {
			return fmt.Errorf("failed to get namespace %s: %w", nsName, err)
		}

		if len(ns.OwnerReferences) > 0 {
			for _, ownerRef := range ns.OwnerReferences {
				if ownerRef.APIVersion == placementv1beta1.GroupVersion.String() &&
					ownerRef.Kind == placementv1beta1.AppliedWorkKind &&
					ownerRef.Name == fmt.Sprintf("%s-work", crpName) {
					if *ownerRef.BlockOwnerDeletion {
						return fmt.Errorf("namespace %s owner reference for AppliedWork should have been updated to have BlockOwnerDeletion set to false", nsName)
					}
				}
			}
		}
		return nil
	}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Namespace which is not owned by the CRP should not be deleted")
}

func checkConfigMapExistsWithOwnerRefOnMemberCluster(namespace, cmName, rpName string) {
	cmHasNoWorkOwnerRefActual := func() error {
		cm := &corev1.ConfigMap{}
		if err := allMemberClusters[0].KubeClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: namespace}, cm); err != nil {
			return fmt.Errorf("failed to get configmap %s/%s: %w", namespace, cmName, err)
		}

		if len(cm.OwnerReferences) > 0 {
			for _, ownerRef := range cm.OwnerReferences {
				if ownerRef.APIVersion == placementv1beta1.GroupVersion.String() &&
					ownerRef.Kind == placementv1beta1.AppliedWorkKind &&
					ownerRef.Name == fmt.Sprintf("%s.%s-work", namespace, rpName) {
					if *ownerRef.BlockOwnerDeletion {
						return fmt.Errorf("configmap %s/%s owner reference for AppliedWork should have been updated to have BlockOwnerDeletion set to false", namespace, cmName)
					}
				}
			}
		}
		return nil
	}

	// Must use Eventually checks first, as Fleet agents might not act fast enough in the test environment.
	Eventually(cmHasNoWorkOwnerRefActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "ConfigMap still has AppliedWork owner reference")
	Consistently(cmHasNoWorkOwnerRefActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "ConfigMap which is not owned by the RP should not be deleted")
}

// cleanupPlacement deletes the placement and waits until the resources are not found.
func cleanupPlacement(placementKey types.NamespacedName) {
	// TODO(Arvindthiru): There is a conflict which requires the Eventually block, not sure of series of operations that leads to it yet.
	Eventually(func() error {
		placement, err := retrievePlacement(placementKey)
		if k8serrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}

		// Delete the placement (again, if applicable).
		// This helps the After All node to run successfully even if the steps above fail early.
		if err = hubClient.Delete(ctx, placement); err != nil {
			return err
		}

		placement.SetFinalizers([]string{})
		return hubClient.Update(ctx, placement)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete placement %s", placementKey)

	// Wait until the placement is removed.
	removedActual := placementRemovedActual(placementKey)
	Eventually(removedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove placement %s", placementKey)

	// Check if work is deleted. Needed to ensure that the Work resource is cleaned up before the next CRP is created.
	// This is because the Work resource is created with a finalizer that blocks deletion until the all applied work
	// and applied work itself is successfully deleted. If the Work resource is not deleted, it can cause resource overlap
	// and flakiness in subsequent tests.
	By("Check if work is deleted")
	var workNS string
	workName := fmt.Sprintf("%s-work", placementKey.Name)
	if placementKey.Namespace != "" {
		workName = fmt.Sprintf("%s.%s", placementKey.Namespace, workName)
	}
	work := &placementv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name: workName,
		},
	}
	Eventually(func() bool {
		for i := range allMemberClusters {
			workNS = fmt.Sprintf("fleet-member-%s", allMemberClusterNames[i])
			if err := hubClient.Get(ctx, types.NamespacedName{Name: work.Name, Namespace: workNS}, work); err != nil && k8serrors.IsNotFound(err) {
				// Work resource is not found, which is expected.
				continue
			}
			// Work object still exists, or some other error occurred, return false to retry.
			return false
		}
		return true
	}, workloadEventuallyDuration, eventuallyInterval).Should(BeTrue(), fmt.Sprintf("Work resource %s from namespace %s should be deleted from hub", work.Name, workNS))
}

// createResourceOverrides creates a number of resource overrides.
func createResourceOverrides(namespace string, number int) {
	for i := 0; i < number; i++ {
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf(roNameTemplate, i),
				Namespace: namespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				ResourceSelectors: []placementv1beta1.ResourceSelector{
					{
						Group:   "apps",
						Kind:    "Deployment",
						Version: "v1",
						Name:    fmt.Sprintf("test-deployment-%d", i),
					},
				},
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
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
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpRemove,
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
		cro := &placementv1beta1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf(croNameTemplate, i),
			},
			Spec: placementv1beta1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   "rbac.authorization.k8s.io/v1",
						Kind:    "ClusterRole",
						Version: "v1",
						Name:    fmt.Sprintf("test-cluster-role-%d", i),
					},
				},
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
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
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpRemove,
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

func ensureCRPAndRelatedResourcesDeleted(crpName string, memberClusters []*framework.Cluster) {
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
		Eventually(workResourcesRemovedActual, workloadEventuallyDuration, time.Second*5).Should(Succeed(), "Failed to remove work resources from member cluster %s", memberCluster.ClusterName)
	}

	// Verify that related finalizers have been removed from the CRP.
	finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: crpName})
	Eventually(finalizerRemovedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP")

	// Remove the custom deletion blocker finalizer from the CRP.
	cleanupPlacement(types.NamespacedName{Name: crpName})

	// Delete the created resources.
	cleanupWorkResources()
}

func ensureCRPEvictionDeleted(crpEvictionName string) {
	crpe := &placementv1beta1.ClusterResourcePlacementEviction{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpEvictionName,
		},
	}
	Expect(hubClient.Delete(ctx, crpe)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}), "Failed to delete CRP eviction")
	removedActual := crpEvictionRemovedActual(crpEvictionName)
	Eventually(removedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "CRP eviction still exists")
}

func ensureCRPDisruptionBudgetDeleted(crpDisruptionBudgetName string) {
	crpdb := &placementv1beta1.ClusterResourcePlacementDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpDisruptionBudgetName,
		},
	}
	Expect(hubClient.Delete(ctx, crpdb)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}), "Failed to delete CRP disruption budget")
	removedActual := crpDisruptionBudgetRemovedActual(crpDisruptionBudgetName)
	Eventually(removedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "CRP disruption budget still exists")
}

// verifyWorkPropagationAndMarkAsAvailable verifies that works derived from a specific CPR have been created
// for a specific cluster, and marks these works in the specific member cluster's
// reserved namespace as applied and available.
//
// This is mostly used for simulating member agents for virtual clusters.
//
// Note that this utility function currently assumes that there is only one work object.
func verifyWorkPropagationAndMarkAsAvailable(memberClusterName, placementName string, resourceIdentifiers []placementv1beta1.ResourceIdentifier) {
	memberClusterReservedNS := fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
	// Wait until the works are created.
	workList := placementv1beta1.WorkList{}
	Eventually(func() error {
		workList = placementv1beta1.WorkList{}
		matchLabelOptions := client.MatchingLabels{
			placementv1beta1.PlacementTrackingLabel: placementName,
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
				Reason:             string(workapplier.AvailabilityResultTypeAvailable),
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

func updateRPWithTolerations(rpKey types.NamespacedName, tolerations []placementv1beta1.Toleration) {
	Eventually(func() error {
		var rp placementv1beta1.ResourcePlacement
		err := hubClient.Get(ctx, rpKey, &rp)
		if err != nil {
			return err
		}
		if rp.Spec.Policy == nil {
			rp.Spec.Policy = &placementv1beta1.PlacementPolicy{
				Tolerations: tolerations,
			}
		} else {
			rp.Spec.Policy.Tolerations = tolerations
		}
		return hubClient.Update(ctx, &rp)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update resource placement with tolerations %s", rpKey)
}

func cleanupClusterResourceOverride(name string) {
	cro := &placementv1beta1.ClusterResourceOverride{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, cro))).To(Succeed(), "Failed to delete clusterResourceOverride %s", name)
	Eventually(func() error {
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name}, &placementv1beta1.ClusterResourceOverride{}); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("clusterResourceOverride %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove clusterResourceOverride %s from hub cluster", name)
}

func cleanupResourceOverride(name string, namespace string) {
	ro := &placementv1beta1.ResourceOverride{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, ro))).To(Succeed(), "Failed to delete resourceOverride %s", name)
	Eventually(func() error {
		if err := hubClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &placementv1beta1.ResourceOverride{}); !k8serrors.IsNotFound(err) {
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
		Expect(validateAnnotationOfConfigMapOnCluster(memberCluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of config map on %s", memberCluster.ClusterName)
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

func readStatefulSetTestManifest(testStatefulSet *appsv1.StatefulSet, variant StatefulSetVariant) {
	By("Read the statefulSet resource")
	var manifestPath string
	switch variant {
	case StatefulSetBasic:
		manifestPath = "resources/statefulset-basic.yaml"
	case StatefulSetInvalidStorage:
		manifestPath = "resources/statefulset-invalid-storage.yaml"
	case StatefulSetWithStorage:
		manifestPath = "resources/statefulset-with-storage.yaml"
	}
	Expect(utils.GetObjectFromManifest(manifestPath, testStatefulSet)).Should(Succeed())
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

func readEnvelopeResourceTestManifest(testEnvelopeObj *placementv1beta1.ResourceEnvelope) {
	By("Read testEnvelopConfigMap resource")
	testEnvelopeObj.ResourceVersion = ""
	err := utils.GetObjectFromManifest("resources/test-envelope-object.yaml", testEnvelopeObj)
	Expect(err).Should(Succeed())
}

// constructWrappedResources fill the enveloped resource with the workload object
func constructWrappedResources(testEnvelopeObj *placementv1beta1.ResourceEnvelope, workloadObj metav1.Object, kind string, namespace corev1.Namespace) {
	// modify the enveloped configMap according to the namespace
	testEnvelopeObj.Namespace = namespace.Name

	// modify the embedded namespaced resource according to the namespace
	workloadObj.SetNamespace(namespace.Name)
	workloadObjectByte, err := json.Marshal(workloadObj)
	Expect(err).Should(Succeed())
	switch kind {
	case utils.DeploymentKind:
		testEnvelopeObj.Data["deployment.yaml"] = runtime.RawExtension{Raw: workloadObjectByte}
	case utils.DaemonSetKind:
		testEnvelopeObj.Data["daemonset.yaml"] = runtime.RawExtension{Raw: workloadObjectByte}
	case utils.StatefulSetKind:
		testEnvelopeObj.Data["statefulset.yaml"] = runtime.RawExtension{Raw: workloadObjectByte}
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

// buildOwnerReference builds an owner reference given a cluster and a placement name.
//
// This function assumes that the placement has only one associated Work object (no resource snapshot
// sub-index, no envelope object used).
func buildOwnerReference(cluster *framework.Cluster, placementName, placementNamespace string) *metav1.OwnerReference {
	var workName string
	if placementNamespace == "" {
		workName = fmt.Sprintf("%s-work", placementName)
	} else {
		workName = fmt.Sprintf("%s.%s-work", placementNamespace, placementName)
	}

	appliedWork := placementv1beta1.AppliedWork{}
	Expect(cluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName}, &appliedWork)).Should(Succeed(), "Failed to get applied work object")

	return &metav1.OwnerReference{
		APIVersion:         placementv1beta1.GroupVersion.String(),
		Kind:               "AppliedWork",
		Name:               workName,
		UID:                appliedWork.UID,
		BlockOwnerDeletion: ptr.To(true),
	}
}

// createRPWithApplyStrategy creates a ResourcePlacement with the given name and apply strategy.
func createRPWithApplyStrategy(rpNamespace, rpName string, applyStrategy *placementv1beta1.ApplyStrategy, resourceSelectors []placementv1beta1.ResourceSelectorTerm) {
	rp := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rpName,
			Namespace: rpNamespace,
			// Add a custom finalizer; this would allow us to better observe
			// the behavior of the controllers.
			Finalizers: []string{customDeletionBlockerFinalizer},
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: configMapSelector(),
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					UnavailablePeriodSeconds: ptr.To(2),
				},
			},
		},
	}
	if applyStrategy != nil {
		rp.Spec.Strategy.ApplyStrategy = applyStrategy
	}
	if resourceSelectors != nil {
		rp.Spec.ResourceSelectors = resourceSelectors
	}
	By(fmt.Sprintf("creating placement %s", rpName))
	Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP %s", rpName)
}

// createCRPWithApplyStrategy creates a ClusterResourcePlacement with the given name and apply strategy.
func createCRPWithApplyStrategy(crpName string, applyStrategy *placementv1beta1.ApplyStrategy, resourceSelectors []placementv1beta1.ResourceSelectorTerm) {
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
	if applyStrategy != nil {
		crp.Spec.Strategy.ApplyStrategy = applyStrategy
	}
	if resourceSelectors != nil {
		crp.Spec.ResourceSelectors = resourceSelectors
	}
	By(fmt.Sprintf("creating placement %s", crpName))
	Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
}

// createRP creates a ResourcePlacement with the given name.
func createRP(rpNamespace, rpName string) {
	createRPWithApplyStrategy(rpNamespace, rpName, nil, nil)
}

// createCRP creates a ClusterResourcePlacement with the given name.
func createCRP(crpName string) {
	createCRPWithApplyStrategy(crpName, nil, nil)
}

// createNamespaceOnlyCRP creates a ClusterResourcePlacement with namespace-only selector.
func createNamespaceOnlyCRP(crpName string) {
	createCRPWithApplyStrategy(crpName, nil, namespaceOnlySelector())
}

// ensureClusterStagedUpdateRunDeletion deletes the cluster staged update run with the given name and checks all related cluster approval requests are also deleted.
func ensureClusterStagedUpdateRunDeletion(updateRunName string) {
	updateRun := &placementv1beta1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: updateRunName,
		},
	}
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, updateRun))).Should(Succeed(), "Failed to delete ClusterStagedUpdateRun %s", updateRunName)

	removedActual := clusterStagedUpdateRunAndClusterApprovalRequestsRemovedActual(updateRunName)
	Eventually(removedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "ClusterStagedUpdateRun or ClusterApprovalRequests still exists")
}

// ensureStagedUpdateRunDeletion deletes the staged update run with the given name and checks all related approval requests are also deleted.
func ensureStagedUpdateRunDeletion(updateRunName, namespace string) {
	updateRun := &placementv1beta1.StagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      updateRunName,
			Namespace: namespace,
		},
	}
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, updateRun))).Should(Succeed(), "Failed to delete StagedUpdateRun %s", updateRunName)

	removedActual := stagedUpdateRunAndApprovalRequestsRemovedActual(updateRunName, namespace)
	Eventually(removedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "StagedUpdateRun or ApprovalRequests still exists")
}

// ensureClusterUpdateRunStrategyDeletion deletes the cluster update run strategy with the given name.
func ensureClusterUpdateRunStrategyDeletion(strategyName string) {
	strategy := &placementv1beta1.ClusterStagedUpdateStrategy{
		ObjectMeta: metav1.ObjectMeta{
			Name: strategyName,
		},
	}
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, strategy))).Should(Succeed(), "Failed to delete ClusterStagedUpdateStrategy %s", strategyName)
	removedActual := clusterUpdateRunStrategyRemovedActual(strategyName)
	Eventually(removedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "ClusterStagedUpdateStrategy still exists")
}

func ensureStagedUpdateRunStrategyDeletion(strategyName, namespace string) {
	Eventually(func() error {
		strategy := &placementv1beta1.StagedUpdateStrategy{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: strategyName, Namespace: namespace}, strategy); err != nil {
			return client.IgnoreNotFound(err)
		}
		return hubClient.Delete(ctx, strategy)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete StagedUpdateStrategy %s", strategyName)

	// Wait for the staged update strategy to be deleted.
	Eventually(func() bool {
		strategy := &placementv1beta1.StagedUpdateStrategy{}
		return hubClient.Get(ctx, client.ObjectKey{Name: strategyName, Namespace: namespace}, strategy) != nil
	}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to delete StagedUpdateStrategy %s", strategyName)
}

// ensureRPAndRelatedResourcesDeleted deletes rp and verifies resources in the specified namespace placed by the rp are removed from the cluster.
// It checks if the placed configMap is removed by default, as this is tested in most of the test cases.
// For tests with additional resources placed, e.g. deployments, daemonSets, add those to placedResources.
func ensureRPAndRelatedResourcesDeleted(rpKey types.NamespacedName, memberClusters []*framework.Cluster, placedResources ...client.Object) {
	// Delete the ResourcePlacement.
	rp := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rpKey.Name,
			Namespace: rpKey.Namespace,
		},
	}
	Expect(hubClient.Delete(ctx, rp)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}), "Failed to delete ResourcePlacement")

	// Verify that all resources placed have been removed from specified member clusters.
	for idx := range memberClusters {
		memberCluster := memberClusters[idx]

		workResourcesRemovedActual := namespacedResourcesRemovedFromClusterActual(memberCluster, placedResources...)
		Eventually(workResourcesRemovedActual, workloadEventuallyDuration, time.Second*5).Should(Succeed(), "Failed to remove work resources from member cluster %s", memberCluster.ClusterName)
	}

	// Verify that related finalizers have been removed from the ResourcePlacement.
	finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(rpKey)
	Eventually(finalizerRemovedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from ResourcePlacement")

	// Remove the custom deletion blocker finalizer from the ResourcePlacement.
	cleanupPlacement(rpKey)

	// Delete the created resources.
	cleanupConfigMap()
}

func retrievePlacement(placementKey types.NamespacedName) (placementv1beta1.PlacementObj, error) {
	var placement placementv1beta1.PlacementObj
	if placementKey.Namespace == "" {
		placement = &placementv1beta1.ClusterResourcePlacement{}
	} else {
		placement = &placementv1beta1.ResourcePlacement{}
	}
	if err := hubClient.Get(ctx, placementKey, placement); err != nil {
		return nil, err
	}
	return placement, nil
}
