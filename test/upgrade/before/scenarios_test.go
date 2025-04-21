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

package before

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

var _ = Describe("CRP with trackable resources, all available (before upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-trackable-available"
	workNamespaceName := "work-trackable-available"
	appConfigMapName := "app-configmap-trackable-available"

	BeforeAll(func() {
		// Create the resources.
		createWorkResources(workNamespaceName, appConfigMapName, crpName)

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(workNamespaceName),
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(crpName, workResourceIdentifiers(workNamespaceName, appConfigMapName), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place the resources on all member clusters", func() {
		checkIfPlacedWorkResourcesOnAllMemberClusters(workNamespaceName, appConfigMapName)
	})
})

var _ = Describe("CRP with non-trackable resources, all available (before upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-non-trackable-available"
	workNamespaceName := "work-non-trackable-available"
	jobName := "job-non-trackable-available"

	BeforeAll(func() {
		// Create the resources.
		ns := appNamespace(workNamespaceName, crpName)
		Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

		// Job is currently untrackable in Fleet.
		job := batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: workNamespaceName,
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Image:           "busybox",
								ImagePullPolicy: corev1.PullIfNotPresent,
								Name:            "busybox",
							},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, &job)).To(Succeed(), "Failed to create job %s", job.Name)

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(workNamespaceName),
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
		wantResourceIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Kind:    "Namespace",
				Name:    workNamespaceName,
				Version: "v1",
			},
			{
				Group:     "batch",
				Kind:      "Job",
				Name:      jobName,
				Version:   "v1",
				Namespace: workNamespaceName,
			},
		}
		crpStatusUpdatedActual := customizedCRPStatusUpdatedActual(crpName, wantResourceIdentifiers, allMemberClusterNames, nil, "0", false)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place the resources on all member clusters", func() {
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]
			// Give the system a bit more breathing room when process resource placement.
			Eventually(func() error {
				if err := validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: workNamespaceName}); err != nil {
					return err
				}

				return validateJobOnCluster(memberCluster, types.NamespacedName{Name: jobName, Namespace: workNamespaceName})
			}, eventuallyDuration*2, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
		}
	})
})

var _ = Describe("CRP with availability failure (before upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-availability-failure"
	workNamespaceName := "work-availability-failure"
	svcName := "svc-availability-failure"

	BeforeAll(func() {
		// Create the resources.
		ns := appNamespace(workNamespaceName, crpName)
		Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

		// Use a Service of the LoadBalancer type as by default KinD environment does not support
		// this service type and such services will always be unavailable.
		//
		// Fleet support a few other API types for availability checks (e.g., Deployment, DaemonSet.
		// etc.); however, they cannot be used in this test spec as they might spawn derived
		// resources (ReplicaSets, ClusterRevisions, etc.) and may cause ownership conflicts if
		// placed.
		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: workNamespaceName,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
				Selector: map[string]string{
					"app": "nginx",
				},
				Ports: []corev1.ServicePort{
					{
						Port:       80,
						TargetPort: intstr.FromInt(80),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, &svc)).To(Succeed(), "Failed to create daement set")

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(workNamespaceName),
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickFixedPlacementType,
					ClusterNames:  []string{memberCluster1EastProdName},
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
		wantResourceIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Kind:    "Namespace",
				Name:    workNamespaceName,
				Version: "v1",
			},
			{
				Kind:      "Service",
				Name:      svcName,
				Version:   "v1",
				Namespace: workNamespaceName,
			},
		}
		wantFailedWorkloadResourceIdentifier := placementv1beta1.ResourceIdentifier{
			Kind:      "Service",
			Name:      svcName,
			Version:   "v1",
			Namespace: workNamespaceName,
		}
		crpStatusUpdatedActual := crpWithOneFailedAvailabilityCheckStatusUpdatedActual(
			crpName,
			wantResourceIdentifiers,
			[]string{memberCluster1EastProdName},
			wantFailedWorkloadResourceIdentifier, 0,
			[]string{},
			"0",
		)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place the resources on the member cluster", func() {
		// Give the system a bit more breathing room when process resource placement.
		Eventually(func() error {
			if err := validateWorkNamespaceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: workNamespaceName}); err != nil {
				return err
			}

			return validateServiceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: svcName, Namespace: workNamespaceName})
		}, eventuallyDuration*2, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProd.ClusterName)
	})
})

var _ = Describe("CRP with apply op failure (before upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-apply-failure"
	workNamespaceName := "work-apply-failure"
	appConfigMapName := "app-configmap-apply-failure"

	BeforeAll(func() {
		// Create the resources on the hub cluster.
		createWorkResources(workNamespaceName, appConfigMapName, crpName)

		// Create the resources on the member cluster with a custom manager
		ns := appNamespace(workNamespaceName, crpName)
		Expect(memberCluster1EastProdClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

		configMap := appConfigMap(workNamespaceName, appConfigMapName)
		configMap.Data = map[string]string{
			"data": "foo",
		}
		Expect(memberCluster1EastProdClient.Create(ctx, &configMap, &client.CreateOptions{FieldManager: "custom"})).To(Succeed(), "Failed to create configMap")

		// Create the CRP.
		//
		// Apply would fail due to SSA owner (manager) conflicts.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(workNamespaceName),
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickFixedPlacementType,
					ClusterNames:  []string{memberCluster1EastProdName},
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type: placementv1beta1.ApplyStrategyTypeServerSideApply,
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
	})

	It("should update CRP status as expected", func() {
		wantFailedWorkloadResourceIdentifier := placementv1beta1.ResourceIdentifier{
			Kind:      "ConfigMap",
			Name:      appConfigMapName,
			Version:   "v1",
			Namespace: workNamespaceName,
		}
		crpStatusUpdatedActual := crpWithOneFailedApplyOpStatusUpdatedActual(
			crpName,
			workResourceIdentifiers(workNamespaceName, appConfigMapName),
			[]string{memberCluster1EastProdName},
			wantFailedWorkloadResourceIdentifier, 0,
			[]string{},
			"0",
		)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place some resources on the member cluster", func() {
		// Give the system a bit more breathing room when process resource placement.
		Eventually(func() error {
			return validateWorkNamespaceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: workNamespaceName})
		}, eventuallyDuration*2, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProd.ClusterName)
	})

	It("should not place some resources on the member cluster", func() {
		// Give the system a bit more breathing room when process resource placement.
		Eventually(func() error {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      appConfigMapName,
					Namespace: workNamespaceName,
				},
				Data: map[string]string{
					"data": "foo",
				},
			}

			wantConfigMap := &corev1.ConfigMap{}
			if err := memberCluster1EastProdClient.Get(ctx, types.NamespacedName{Name: appConfigMapName, Namespace: workNamespaceName}, wantConfigMap); err != nil {
				return err
			}

			if diff := cmp.Diff(
				configMap, wantConfigMap,
				ignoreObjectMetaAutoGeneratedFields,
				ignoreObjectMetaAnnotationField,
			); diff != "" {
				return fmt.Errorf("app config map diff (-got, +want): %s", diff)
			}

			return nil
		}, eventuallyDuration*2, eventuallyInterval).Should(Succeed(), "Failed to skip resource placement")
	})
})

var _ = Describe("CRP stuck in the rollout process (blocked by availability failure)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-availability-failure-stuck"
	workNamespaceName := "work-availability-failure-stuck"
	svcName := "svc-availability-failure-stuck"

	var originalSvc *corev1.Service
	BeforeAll(func() {
		// Create the resources.
		ns := appNamespace(workNamespaceName, crpName)
		Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

		// Use a Service of the ClusterIP type. KinD supports it and it will become available
		// once an IP has been assigned.
		//
		// Fleet support a few other API types for availability checks (e.g., Deployment, DaemonSet.
		// etc.); however, they cannot be used in this test spec as they might spawn derived
		// resources (ReplicaSets, ClusterRevisions, etc.) and may cause ownership conflicts if
		// placed.
		originalSvc = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: workNamespaceName,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Selector: map[string]string{
					"app": "nginx",
				},
				Ports: []corev1.ServicePort{
					{
						Port:       80,
						TargetPort: intstr.FromInt(80),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, originalSvc.DeepCopy())).To(Succeed(), "Failed to create daement set")

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(workNamespaceName),
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
						MaxUnavailable:           ptr.To(intstr.FromInt(1)),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
	})

	It("should update CRP status as expected", func() {
		wantResourceIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Kind:    "Namespace",
				Name:    workNamespaceName,
				Version: "v1",
			},
			{
				Kind:      "Service",
				Name:      svcName,
				Version:   "v1",
				Namespace: workNamespaceName,
			},
		}
		crpStatusUpdatedActual := customizedCRPStatusUpdatedActual(
			crpName,
			wantResourceIdentifiers,
			allMemberClusterNames,
			[]string{},
			"0",
			true,
		)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place the resources on the member cluster", func() {
		// Give the system a bit more breathing room when process resource placement.
		Eventually(func() error {
			if err := validateWorkNamespaceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: workNamespaceName}); err != nil {
				return err
			}

			return validateServiceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: svcName, Namespace: workNamespaceName})
		}, eventuallyDuration*2, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProd.ClusterName)
	})

	It("can update the service objects", func() {
		// Update the service object.
		svc := &corev1.Service{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: svcName, Namespace: workNamespaceName}, svc)).To(Succeed(), "Failed to get service %s", svcName)

		// KinD does not support LoadBalancer typed Services; no LB can be provisioned and Fleet
		// will consider the service to be of an unavailable state.
		svc.Spec.Type = corev1.ServiceTypeLoadBalancer
		Expect(hubClient.Update(ctx, svc)).To(Succeed(), "Failed to update service %s", svcName)
	})

	It("should update CRP status as expected (rollout blocked due to unavailable objects)", func() {
		wantSelectedResourceIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Kind:    "Namespace",
				Name:    workNamespaceName,
				Version: "v1",
			},
			{
				Kind:      "Service",
				Name:      svcName,
				Version:   "v1",
				Namespace: workNamespaceName,
			},
		}
		failedWorkloadResourceIdentifier := placementv1beta1.ResourceIdentifier{
			Kind:      "Service",
			Name:      svcName,
			Version:   "v1",
			Namespace: workNamespaceName,
		}
		crpStatusUpdatedActual := crpWithStuckRolloutDueToOneFailedAvailabilityCheckStatusUpdatedActual(
			crpName,
			wantSelectedResourceIdentifiers,
			failedWorkloadResourceIdentifier, 0,
			allMemberClusterNames,
			"1",
		)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should update objects and leave objects alone on respective clusters", func() {
		// Retrieve the CRP for its status.
		crp := &placementv1beta1.ClusterResourcePlacement{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp)).To(Succeed(), "Failed to get CRP")

		// Find the clusters that have the updated Service object and those that do not.
		clustersWithUpdatedService := map[string]struct{}{}
		clustersWithOldService := map[string]struct{}{}
		for idx := range crp.Status.PlacementStatuses {
			rps := crp.Status.PlacementStatuses[idx]
			availableCond := meta.FindStatusCondition(rps.Conditions, string(placementv1beta1.ResourcesAvailableConditionType))
			switch {
			case availableCond == nil:
				clustersWithOldService[rps.ClusterName] = struct{}{}
			case availableCond.Status == metav1.ConditionFalse:
				clustersWithUpdatedService[rps.ClusterName] = struct{}{}
			default:
				Fail(fmt.Sprintf("Found an unexpected availability reporting \n(%v)", rps))
			}
		}

		// Validate things on the clusters with updated resources.
		Eventually(func() error {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				if _, ok := clustersWithUpdatedService[memberCluster.ClusterName]; ok {
					// No need to validate the NS as it is unchanged.
					if err := validateServiceOnCluster(memberCluster, types.NamespacedName{Name: svcName, Namespace: workNamespaceName}); err != nil {
						return err
					}
				}
			}
			return nil
		}, eventuallyDuration*2, eventuallyInterval).Should(Succeed(), "Failed to update objects on clusters")

		// Validate things on the clusters with old resources.
		Eventually(func() error {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				if _, ok := clustersWithOldService[memberCluster.ClusterName]; ok {
					svc := &corev1.Service{}
					if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: svcName, Namespace: workNamespaceName}, svc); err != nil {
						return fmt.Errorf("failed to retrieve svc on cluster %s: %w", memberCluster.ClusterName, err)
					}

					if diff := cmp.Diff(
						svc, originalSvc,
						ignoreObjectMetaAutoGeneratedFields,
						ignoreObjectMetaAnnotationField,
						ignoreServiceStatusField,
						ignoreServiceSpecIPAndPolicyFields,
						ignoreServicePortNodePortProtocolField,
					); diff != "" {
						return fmt.Errorf("service diff (-got, +want): %s", diff)
					}
				}
			}
			return nil
		}, eventuallyDuration*2, eventuallyInterval).Should(Succeed(), "Failed to leave objects alone on clusters")
	})
})

var _ = Describe("CRP stuck in the rollout process (blocked by apply op failure)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-apply-failure-stuck"
	workNamespaceName := "work-apply-failure-stuck"
	appConfigMapName := "app-configmap-apply-failure-stuck"

	BeforeAll(func() {
		// Create the resources.
		createWorkResources(workNamespaceName, appConfigMapName, crpName)

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(workNamespaceName),
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
						MaxUnavailable:           ptr.To(intstr.FromInt(1)),
					},
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type: placementv1beta1.ApplyStrategyTypeServerSideApply,
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(crpName, workResourceIdentifiers(workNamespaceName, appConfigMapName), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place the resources on all member clusters", func() {
		checkIfPlacedWorkResourcesOnAllMemberClusters(workNamespaceName, appConfigMapName)
	})

	It("assume ownership", func() {
		// Update the ConfigMap object on all member clusters.
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]

			configMap := &corev1.ConfigMap{}
			Expect(memberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: appConfigMapName, Namespace: workNamespaceName}, configMap)).To(Succeed(), "Failed to get config map %s", appConfigMapName)

			configMap.TypeMeta = metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			}
			configMap.Data["custom"] = "foo"
			// Unset this field as required by the server.
			configMap.ObjectMeta.ManagedFields = nil
			Expect(memberCluster.KubeClient.Patch(ctx, configMap, client.Apply, &client.PatchOptions{FieldManager: "handover", Force: ptr.To(true)})).To(Succeed(), "Failed to update config map %s", appConfigMapName)
		}
	})

	It("can update the config map", func() {
		// Update the ConfigMap object on the hub cluster.
		configMap := &corev1.ConfigMap{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: appConfigMapName, Namespace: workNamespaceName}, configMap)).To(Succeed(), "Failed to get config map %s", appConfigMapName)

		// This change cannot be applied as the added field is not currently managed by Fleet.
		configMap.Data["custom"] = "baz"
		Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update config map %s", appConfigMapName)
	})

	It("should update CRP status as expected (rollout blocked due to apply op failures)", func() {
		wantSelectedResourceIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Kind:    "Namespace",
				Name:    workNamespaceName,
				Version: "v1",
			},
			{
				Kind:      "ConfigMap",
				Name:      appConfigMapName,
				Version:   "v1",
				Namespace: workNamespaceName,
			},
		}
		failedWorkloadResourceIdentifier := placementv1beta1.ResourceIdentifier{
			Kind:      "ConfigMap",
			Name:      appConfigMapName,
			Version:   "v1",
			Namespace: workNamespaceName,
		}
		crpStatusUpdatedActual := crpWithStuckRolloutDueToOneFailedApplyOpStatusUpdatedActual(
			crpName,
			wantSelectedResourceIdentifiers,
			failedWorkloadResourceIdentifier, 0,
			allMemberClusterNames,
			"1",
		)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should leave objects alone on all clusters", func() {
		// Validate things on the clusters with old resources.
		Eventually(func() error {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]

				configMap := &corev1.ConfigMap{}
				if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: appConfigMapName, Namespace: workNamespaceName}, configMap); err != nil {
					return fmt.Errorf("failed to retrieve config map on cluster %s: %w", memberCluster.ClusterName, err)
				}

				wantConfigMap := configMap.DeepCopy()
				wantConfigMap.Data["custom"] = "foo"
				if diff := cmp.Diff(
					configMap, wantConfigMap,
					ignoreObjectMetaAutoGeneratedFields,
					ignoreObjectMetaAnnotationField,
				); diff != "" {
					return fmt.Errorf("service diff (-got, +want): %s", diff)
				}
			}

			return nil
		}, eventuallyDuration*2, eventuallyInterval).Should(Succeed(), "Failed to leave objects alone on clusters")
	})
})

var _ = Describe("CRP stuck in the rollout process (long wait time)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-non-trackable-stuck"
	workNamespaceName := "work-non-trackable-stuck"
	jobName := "job-non-trackable-stuck"

	var originalJob *batchv1.Job
	BeforeAll(func() {
		// Create the resources.
		ns := appNamespace(workNamespaceName, crpName)
		Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Name)

		// Job is currently untrackable in Fleet.
		originalJob = &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: workNamespaceName,
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Image:           "busybox",
								ImagePullPolicy: corev1.PullIfNotPresent,
								Name:            "busybox",
							},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				},
				BackoffLimit: ptr.To(int32(6)),
			},
		}
		Expect(hubClient.Create(ctx, originalJob.DeepCopy())).To(Succeed(), "Failed to create job")

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(workNamespaceName),
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						// Block rollout for a long time if there are untrackable resources.
						UnavailablePeriodSeconds: ptr.To(3600),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
	})

	It("should update CRP status as expected", func() {
		wantResourceIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Kind:    "Namespace",
				Name:    workNamespaceName,
				Version: "v1",
			},
			{
				Group:     "batch",
				Kind:      "Job",
				Name:      jobName,
				Version:   "v1",
				Namespace: workNamespaceName,
			},
		}
		crpStatusUpdatedActual := customizedCRPStatusUpdatedActual(crpName, wantResourceIdentifiers, allMemberClusterNames, nil, "0", false)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place the resources on all member clusters", func() {
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]
			// Give the system a bit more breathing room when process resource placement.
			Eventually(func() error {
				if err := validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: workNamespaceName}); err != nil {
					return err
				}

				return validateJobOnCluster(memberCluster, types.NamespacedName{Name: jobName, Namespace: workNamespaceName})
			}, eventuallyDuration*2, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
		}
	})

	It("can update the job object", func() {
		// Update the service object.
		job := &batchv1.Job{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: workNamespaceName}, job)).To(Succeed(), "Failed to get job %s", jobName)

		job.Spec.BackoffLimit = ptr.To(int32(10))
		Expect(hubClient.Update(ctx, job)).To(Succeed(), "Failed to update job %s", jobName)
	})

	It("should update CRP status as expected (rollout blocked due to long wait time)", func() {
		wantResourceIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Kind:    "Namespace",
				Name:    workNamespaceName,
				Version: "v1",
			},
			{
				Group:     "batch",
				Kind:      "Job",
				Name:      jobName,
				Version:   "v1",
				Namespace: workNamespaceName,
			},
		}
		crpStatusUpdatedActual := crpWithStuckRolloutDueToUntrackableResourcesStatusUpdatedActual(crpName, wantResourceIdentifiers, allMemberClusterNames, "1")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should leave objects alone on all clusters", func() {
		// Validate things on the clusters with old resources.
		Eventually(func() error {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]

				job := &batchv1.Job{}
				if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: workNamespaceName}, job); err != nil {
					return fmt.Errorf("failed to retrieve job on cluster %s: %w", memberCluster.ClusterName, err)
				}

				// For simplicity, we only check the BackoffLimit field.
				if !cmp.Equal(job.Spec.BackoffLimit, originalJob.Spec.BackoffLimit) {
					return fmt.Errorf("job backoff limit mismatches on cluster %s", memberCluster.ClusterName)
				}
			}
			return nil
		}, eventuallyDuration*2, eventuallyInterval).Should(Succeed(), "Failed to leave objects alone on clusters")
	})
})
