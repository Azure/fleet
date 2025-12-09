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

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var _ = Describe("placing workloads using a CRP with PickAll policy", Label("resourceplacement"), Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	var testDeployment appsv1.Deployment
	var testDaemonSet appsv1.DaemonSet
	var testJob batchv1.Job
	var testStatefulSet appsv1.StatefulSet

	BeforeAll(func() {
		// Read the test manifests
		readDeploymentTestManifest(&testDeployment)
		readDaemonSetTestManifest(&testDaemonSet)
		readJobTestManifest(&testJob)
		readStatefulSetTestManifest(&testStatefulSet, StatefulSetWithStorage)
		workNamespace := appNamespace()

		// Create namespace and workloads
		By("creating namespace and workloads")
		Expect(hubClient.Create(ctx, &workNamespace)).To(Succeed(), "Failed to create namespace %s", workNamespace.Name)
		testDeployment.Namespace = workNamespace.Name
		testDaemonSet.Namespace = workNamespace.Name
		testJob.Namespace = workNamespace.Name
		testStatefulSet.Namespace = workNamespace.Name
		Expect(hubClient.Create(ctx, &testDeployment)).To(Succeed(), "Failed to create test deployment %s", testDeployment.Name)
		Expect(hubClient.Create(ctx, &testDaemonSet)).To(Succeed(), "Failed to create test daemonset %s", testDaemonSet.Name)
		Expect(hubClient.Create(ctx, &testJob)).To(Succeed(), "Failed to create test job %s", testJob.Name)
		Expect(hubClient.Create(ctx, &testStatefulSet)).To(Succeed(), "Failed to create test statefulset %s", testStatefulSet.Name)

		// Create the CRP that selects the namespace
		By("creating CRP that selects the namespace")
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name:       crpName,
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: workResourceSelector(),
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
		wantSelectedResources := []placementv1beta1.ResourceIdentifier{
			{
				Kind:    "Namespace",
				Name:    workNamespace.Name,
				Version: "v1",
			},
			{
				Group:     "apps",
				Version:   "v1",
				Kind:      "Deployment",
				Name:      testDeployment.Name,
				Namespace: workNamespace.Name,
			},
			{
				Group:     "apps",
				Version:   "v1",
				Kind:      "DaemonSet",
				Name:      testDaemonSet.Name,
				Namespace: workNamespace.Name,
			},
			{
				Group:     "batch",
				Version:   "v1",
				Kind:      "Job",
				Name:      testJob.Name,
				Namespace: workNamespace.Name,
			},
			{
				Group:     "apps",
				Version:   "v1",
				Kind:      "StatefulSet",
				Name:      testStatefulSet.Name,
				Namespace: workNamespace.Name,
			},
		}
		// Use customizedPlacementStatusUpdatedActual with resourceIsTrackable=false
		// because Jobs don't have availability tracking like Deployments/DaemonSets/StatefulSets do
		crpKey := types.NamespacedName{Name: crpName}
		crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(crpKey, wantSelectedResources, allMemberClusterNames, nil, "0", false)
		Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterAll(func() {
		By("cleaning up resources")
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("with PickAll placement type", Ordered, func() {
		It("should verify hub deployment is ready", func() {
			By("checking hub deployment status")
			Eventually(func() error {
				var hubDeployment appsv1.Deployment
				if err := hubClient.Get(ctx, types.NamespacedName{
					Name:      testDeployment.Name,
					Namespace: testDeployment.Namespace,
				}, &hubDeployment); err != nil {
					return err
				}
				// Verify deployment is ready in hub cluster
				if hubDeployment.Status.ReadyReplicas != *hubDeployment.Spec.Replicas {
					return fmt.Errorf("hub deployment not ready: %d/%d replicas ready", hubDeployment.Status.ReadyReplicas, *hubDeployment.Spec.Replicas)
				}
				if hubDeployment.Status.UpdatedReplicas != *hubDeployment.Spec.Replicas {
					return fmt.Errorf("hub deployment not updated: %d/%d replicas updated", hubDeployment.Status.UpdatedReplicas, *hubDeployment.Spec.Replicas)
				}
				return nil
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(),
				"Hub deployment should be ready before placement")
		})

		It("should verify hub daemonset is ready", func() {
			By("checking hub daemonset status")
			Eventually(func() error {
				var hubDaemonSet appsv1.DaemonSet
				if err := hubClient.Get(ctx, types.NamespacedName{
					Name:      testDaemonSet.Name,
					Namespace: testDaemonSet.Namespace,
				}, &hubDaemonSet); err != nil {
					return err
				}
				// Verify daemonset is ready in hub cluster
				if hubDaemonSet.Status.NumberReady == 0 {
					return fmt.Errorf("hub daemonset has no ready pods")
				}
				if hubDaemonSet.Status.NumberReady != hubDaemonSet.Status.DesiredNumberScheduled {
					return fmt.Errorf("hub daemonset not ready: %d/%d pods ready", hubDaemonSet.Status.NumberReady, hubDaemonSet.Status.DesiredNumberScheduled)
				}
				return nil
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(),
				"Hub daemonset should be ready before placement")
		})

		It("should verify hub job completes successfully", func() {
			By("checking hub job completion status")
			jobCompletedActual := waitForJobToComplete(hubClient, &testJob)
			Eventually(jobCompletedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(),
				"Hub job should complete successfully")
		})

		It("should verify hub statefulset is ready", func() {
			By("checking hub statefulset status")
			statefulSetReadyActual := waitForStatefulSetToBeReady(hubClient, &testStatefulSet)
			Eventually(statefulSetReadyActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(),
				"Hub statefulset should be ready before placement")
		})

		It("should place the deployment on all member clusters", func() {
			By("verifying deployment is placed and ready on all member clusters")
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				deploymentPlacedActual := waitForDeploymentPlacementToReady(memberCluster, &testDeployment)
				Eventually(deploymentPlacedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place deployment on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("should place the daemonset on all member clusters", func() {
			By("verifying daemonset is placed and ready on all member clusters")
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				daemonsetPlacedActual := waitForDaemonSetPlacementToReady(memberCluster, &testDaemonSet)
				Eventually(daemonsetPlacedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place daemonset on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("should place the job on all member clusters", func() {
			By("verifying job is placed on all member clusters")
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				jobPlacedActual := waitForJobToBePlaced(memberCluster, &testJob)
				Eventually(jobPlacedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place job on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("should verify job completes successfully on all clusters", func() {
			By("checking job completion status on each cluster")
			for _, cluster := range allMemberClusters {
				jobCompletedActual := waitForJobToComplete(cluster.KubeClient, &testJob)
				Eventually(jobCompletedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(),
					"Job should complete successfully on cluster %s", cluster.ClusterName)
			}
		})

		It("should place the statefulset on all member clusters", func() {
			By("verifying statefulset is placed and ready on all member clusters")
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				statefulsetPlacedActual := waitForStatefulSetPlacementToReady(memberCluster, &testStatefulSet)
				Eventually(statefulsetPlacedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place statefulset on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("should verify statefulset replicas are ready on all clusters", func() {
			By("checking statefulset status on each cluster")
			for _, cluster := range allMemberClusters {
				statefulSetReadyActual := waitForStatefulSetToBeReady(cluster.KubeClient, &testStatefulSet)
				Eventually(statefulSetReadyActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(),
					"StatefulSet should be ready on cluster %s", cluster.ClusterName)
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

func waitForStatefulSetToBeReady(kubeClient client.Client, testStatefulSet *appsv1.StatefulSet) func() error {
	return func() error {
		var statefulSet appsv1.StatefulSet
		if err := kubeClient.Get(ctx, types.NamespacedName{
			Name:      testStatefulSet.Name,
			Namespace: testStatefulSet.Namespace,
		}, &statefulSet); err != nil {
			return err
		}

		// Verify statefulset is ready
		requiredReplicas := int32(1)
		if statefulSet.Spec.Replicas != nil {
			requiredReplicas = *statefulSet.Spec.Replicas
		}

		wantStatus := appsv1.StatefulSetStatus{
			ObservedGeneration: statefulSet.Generation,
			CurrentReplicas:    requiredReplicas,
			UpdatedReplicas:    requiredReplicas,
		}

		gotStatus := appsv1.StatefulSetStatus{
			ObservedGeneration: statefulSet.Status.ObservedGeneration,
			CurrentReplicas:    statefulSet.Status.CurrentReplicas,
			UpdatedReplicas:    statefulSet.Status.UpdatedReplicas,
		}

		if diff := cmp.Diff(wantStatus, gotStatus); diff != "" {
			return fmt.Errorf("statefulset not ready (-want +got):\n%s", diff)
		}

		return nil
	}
}

func waitForJobToComplete(kubeClient client.Client, testJob *batchv1.Job) func() error {
	return func() error {
		var job batchv1.Job
		if err := kubeClient.Get(ctx, types.NamespacedName{
			Name:      testJob.Name,
			Namespace: testJob.Namespace,
		}, &job); err != nil {
			return err
		}

		// Check if job has completed successfully
		if job.Status.Succeeded == 0 {
			return fmt.Errorf("job not completed: %d succeeded", job.Status.Succeeded)
		}

		// Verify all job pods completed successfully
		podList := &corev1.PodList{}
		if err := kubeClient.List(ctx, podList, client.InNamespace(testJob.Namespace),
			client.MatchingLabels{"job-name": testJob.Name}); err != nil {
			return fmt.Errorf("failed to list job pods: %w", err)
		}

		if len(podList.Items) == 0 {
			return fmt.Errorf("no pods found for job %s", testJob.Name)
		}

		for _, pod := range podList.Items {
			if pod.Status.Phase != corev1.PodSucceeded {
				return fmt.Errorf("pod %s not succeeded: phase=%s", pod.Name, pod.Status.Phase)
			}
		}

		return nil
	}
}
