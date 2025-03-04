/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package after

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

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// TO-DO (chenyu1): expand the test specs to check agent liveness after upgrade.

var _ = Describe("CRP with trackable resources, all available (after upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the before upgrade
	// test stage.
	crpName := "crp-trackable-available"
	workNamespaceName := "work-trackable-available"
	appConfigMapName := "app-configmap-trackable-available"

	// Setup is done in the previous step.

	It("should keep CRP status", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(crpName, workResourceIdentifiers(workNamespaceName, appConfigMapName), allMemberClusterNames, nil, "0")
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should keep the state of objects", func() {
		checkIfPlacedWorkResourcesOnAllMemberClustersConsistently(workNamespaceName, appConfigMapName)
	})
})

var _ = Describe("CRP with untrackable resources, all available (after upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the before upgrade
	// test stage.
	crpName := "crp-non-trackable-available"
	workNamespaceName := "work-non-trackable-available"
	jobName := "job-non-trackable-available"

	// Setup is done in the previous step.

	It("should keep CRP status unchanged", func() {
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
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should keep the state of objects", func() {
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]
			// Give the system a bit more breathing room when process resource placement.
			Consistently(func() error {
				if err := validateWorkNamespaceOnCluster(memberCluster, types.NamespacedName{Name: workNamespaceName}); err != nil {
					return err
				}

				return validateJobOnCluster(memberCluster, types.NamespacedName{Name: jobName, Namespace: workNamespaceName})
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
		}
	})
})

var _ = Describe("CRP with availability failure (after upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the before upgrade
	// test stage.
	crpName := "crp-availability-failure"
	workNamespaceName := "work-availability-failure"
	svcName := "svc-availability-failure"

	It("should keep CRP status unchanged", func() {
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
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should keep the state of objects", func() {
		// Give the system a bit more breathing room when process resource placement.
		Consistently(func() error {
			if err := validateWorkNamespaceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: workNamespaceName}); err != nil {
				return err
			}

			return validateServiceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: svcName, Namespace: workNamespaceName})
		}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProd.ClusterName)
	})
})

var _ = Describe("CRP with apply op failure (after upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the before upgrade
	// test stage.
	crpName := "crp-apply-failure"
	workNamespaceName := "work-apply-failure"
	appConfigMapName := "app-configmap-apply-failure"

	It("should keep CRP status unchanged", func() {
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
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should keep the state of objects (no apply op failure)", func() {
		// Give the system a bit more breathing room when process resource placement.
		Consistently(func() error {
			return validateWorkNamespaceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: workNamespaceName})
		}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProd.ClusterName)
	})

	It("should keep the state of objects (apply op failure)", func() {
		// Give the system a bit more breathing room when process resource placement.
		Consistently(func() error {
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
		}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to skip resource placement")
	})
})

var _ = Describe("CRP stuck in the rollout process (blocked by availability failure)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the before upgrade
	// test stage.
	crpName := "crp-availability-failure-stuck"
	workNamespaceName := "work-availability-failure-stuck"
	svcName := "svc-availability-failure-stuck"

	originalSvc := &corev1.Service{
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

	It("should keep CRP status unchanged (rollout blocked due to unavailable objects)", func() {
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
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should keeps the state of objects", func() {
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
		Consistently(func() error {
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
		}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update objects on clusters")

		// Validate things on the clusters with old resources.
		Consistently(func() error {
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
		}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to leave objects alone on clusters")
	})
})

var _ = Describe("CRP stuck in the rollout process (blocked by apply op failure)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-apply-failure-stuck"
	workNamespaceName := "work-apply-failure-stuck"
	appConfigMapName := "app-configmap-apply-failure-stuck"

	It("should keep CRP status unchanged (rollout blocked due to apply op failures)", func() {
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
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should keep the state of objects", func() {
		// Validate things on the clusters with old resources.
		Consistently(func() error {
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
		}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to leave objects alone on clusters")
	})
})

var _ = Describe("CRP stuck in the rollout process (long wait time)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-non-trackable-stuck"
	workNamespaceName := "work-non-trackable-stuck"
	jobName := "job-non-trackable-stuck"

	originalJob := &batchv1.Job{
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

	It("should keep the CRP status unchanged (rollout blocked due to long wait time)", func() {
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
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should keep the state of objects", func() {
		// Validate things on the clusters with old resources.
		Consistently(func() error {
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
		}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to leave objects alone on clusters")
	})
})
