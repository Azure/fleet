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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var _ = Describe("CRP with trackable resources, all available (after upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the before upgrade
	// test stage.
	crpName := "crp-trackable-available"
	workNamespaceName := "work-trackable-available"
	appConfigMapName := "app-configmap-trackable-available"

	// Setup is done in the previous step.

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(crpName, workResourceIdentifiers(workNamespaceName, appConfigMapName), allMemberClusterNames, nil, "0")
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place the resources on all member clusters", func() {
		checkIfPlacedWorkResourcesOnAllMemberClustersConsistently(workNamespaceName, appConfigMapName)
	})
})

var _ = Describe("CRP with untrackable resources, all available (after upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-non-trackable-available"
	workNamespaceName := "work-non-trackable-available"
	jobName := "job-non-trackable-available"

	// Setup is done in the previous step.

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
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place the resources on all member clusters", func() {
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

var _ = Describe("CRP with availability failure (before upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-availability-failure"
	workNamespaceName := "work-availability-failure"
	svcName := "svc-availability-failure"

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
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place the resources on the member cluster", func() {
		// Give the system a bit more breathing room when process resource placement.
		Consistently(func() error {
			if err := validateWorkNamespaceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: workNamespaceName}); err != nil {
				return err
			}

			return validateServiceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: svcName, Namespace: workNamespaceName})
		}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProd.ClusterName)
	})
})

var _ = Describe("CRP with apply op failure (before upgrade)", Ordered, func() {
	// The names specified must match with those in the corresponding node from the after upgrade
	// test stage.
	crpName := "crp-apply-failure"
	workNamespaceName := "work-apply-failure"
	appConfigMapName := "app-configmap-apply-failure"

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
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place some resources on the member cluster", func() {
		// Give the system a bit more breathing room when process resource placement.
		Consistently(func() error {
			return validateWorkNamespaceOnCluster(memberCluster1EastProd, types.NamespacedName{Name: workNamespaceName})
		}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster1EastProd.ClusterName)
	})

	It("should not place some resources on the member cluster", func() {
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

var _ = Describe("CRP stuck in the rollout process (blocked by availability failure)", Ordered, func() {})

var _ = Describe("CRP stuck in the rollout process (blocked by apply op failure)", Ordered, func() {})

var _ = Describe("CRP stuck in the rollout process (long wait time)", Ordered, func() {})
