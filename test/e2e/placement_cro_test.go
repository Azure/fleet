/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// TODO: Add more tests to cover the negative cases that override failed, need to make sure
// the CRP status has all the information to debug the issue.

// Note that this container will run in parallel with other containers.
var _ = Describe("creating clusterResourceOverride (selecting all clusters) to override all resources under the namespace", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the cro before crp so that the observed resource index is predictable.
		cro := &placementv1alpha1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: croName,
			},
			Spec: placementv1alpha1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: workResourceSelector(),
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/annotations",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s"}`, croTestAnnotationKey, croTestAnnotationValue))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating clusterResourceOverride %s", croName))
		Expect(hubClient.Create(ctx, cro)).To(Succeed(), "Failed to create clusterResourceOverride %s", croName)

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(),
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting clusterResourceOverride %s", croName))
		cleanupClusterResourceOverride(croName)
	})

	It("should update CRP status as expected", func() {
		wantCRONames := []string{fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, croName, 0)}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", wantCRONames, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have override annotations on the placed resources", func() {
		want := map[string]string{croTestAnnotationKey: croTestAnnotationValue}
		checkIfOverrideAnnotationsOnAllMemberClusters(true, want)
	})

	It("update cro attached to this CRP only and change annotation value", func() {
		Eventually(func() error {
			cro := &placementv1alpha1.ClusterResourceOverride{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: croName}, cro); err != nil {
				return err
			}
			cro.Spec = placementv1alpha1.ClusterResourceOverrideSpec{
				Placement: &placementv1alpha1.PlacementRef{
					Name: crpName, // assigned CRP name
				},
				ClusterResourceSelectors: workResourceSelector(),
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/annotations",
									// changed the annotation value to croTestAnnotationValue1
									Value: apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s"}`, croTestAnnotationKey, croTestAnnotationValue1))},
								},
							},
						},
					},
				},
			}
			return hubClient.Update(ctx, cro)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cro as expected", crpName)
	})

	It("should update CRP status as expected", func() {
		wantCRONames := []string{fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, croName, 1)}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", wantCRONames, nil)
		// use the long duration to wait until the rollout is finished.
		Eventually(crpStatusUpdatedActual, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have new override annotation value on the placed resources", func() {
		want := map[string]string{croTestAnnotationKey: croTestAnnotationValue1}
		checkIfOverrideAnnotationsOnAllMemberClusters(true, want)
	})
})

var _ = Describe("creating clusterResourceOverride with multiple jsonPatchOverrides", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the cro before crp so that the observed resource index is predictable.
		cro := &placementv1alpha1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: croName,
			},
			Spec: placementv1alpha1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: workResourceSelector(),
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/annotations",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s"}`, croTestAnnotationKey, croTestAnnotationValue))},
								},
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     fmt.Sprintf("/metadata/annotations/%s", croTestAnnotationKey1),
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%s"`, croTestAnnotationValue1))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating clusterResourceOverride %s", croName))
		Expect(hubClient.Create(ctx, cro)).To(Succeed(), "Failed to create clusterResourceOverride %s", croName)

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(),
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting clusterResourceOverride %s", croName))
		cleanupClusterResourceOverride(croName)
	})

	It("should update CRP status as expected", func() {
		wantCRONames := []string{fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, croName, 0)}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", wantCRONames, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have annotations on all member clusters", func() {
		wantAnnotations := map[string]string{croTestAnnotationKey: croTestAnnotationValue, croTestAnnotationKey1: croTestAnnotationValue1}
		checkIfOverrideAnnotationsOnAllMemberClusters(true, wantAnnotations)
	})

	It("update cro attached to an invalid CRP", func() {
		Eventually(func() error {
			cro := &placementv1alpha1.ClusterResourceOverride{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: croName}, cro); err != nil {
				return err
			}
			cro.Spec.Placement = &placementv1alpha1.PlacementRef{
				Name: "invalid-crp", // assigned CRP name
			}
			return hubClient.Update(ctx, cro)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cro as expected", crpName)
	})

	It("CRP status should not be changed", func() {
		wantCRONames := []string{fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, croName, 0)}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", wantCRONames, nil)
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "CRP %s status has been changed", crpName)
	})
})

var _ = Describe("creating clusterResourceOverride with different rules for each cluster", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the cro before crp so that the observed resource index is predictable.
		cro := &placementv1alpha1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: croName,
			},
			Spec: placementv1alpha1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: workResourceSelector(),
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{regionLabelName: regionEast, envLabelName: envProd},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/annotations",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s-0"}`, croTestAnnotationKey, croTestAnnotationValue))},
								},
							},
						},
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{regionLabelName: regionEast, envLabelName: envCanary},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/annotations",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s-1"}`, croTestAnnotationKey, croTestAnnotationValue))},
								},
							},
						},
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{regionLabelName: regionWest, envLabelName: envProd},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/annotations",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s-2"}`, croTestAnnotationKey, croTestAnnotationValue))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating clusterResourceOverride %s", croName))
		Expect(hubClient.Create(ctx, cro)).To(Succeed(), "Failed to create clusterResourceOverride %s", croName)

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(),
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting clusterResourceOverride %s", croName))
		cleanupClusterResourceOverride(croName)
	})

	It("should update CRP status as expected", func() {
		wantCRONames := []string{fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, croName, 0)}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", wantCRONames, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have override annotations on the member clusters", func() {
		for i, cluster := range allMemberClusters {
			wantAnnotations := map[string]string{croTestAnnotationKey: fmt.Sprintf("%s-%d", croTestAnnotationValue, i)}
			Expect(validateAnnotationOfWorkNamespaceOnCluster(cluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", cluster.ClusterName)
			Expect(validateOverrideAnnotationOfConfigMapOnCluster(cluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of configmap on %s", cluster.ClusterName)
		}
	})
})

var _ = Describe("creating clusterResourceOverride with different rules for each cluster", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()
		createCRP(crpName)
		cro := &placementv1alpha1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: croName,
			},
			Spec: placementv1alpha1.ClusterResourceOverrideSpec{
				Placement: &placementv1alpha1.PlacementRef{
					Name: crpName, // assigned CRP name
				},
				ClusterResourceSelectors: workResourceSelector(),
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchExpressions: []metav1.LabelSelectorRequirement{
												{
													Key:      regionLabelName,
													Operator: metav1.LabelSelectorOpExists,
												},
											},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/annotations",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "test-%s"}`, croTestAnnotationKey, placementv1alpha1.OverrideClusterNameVariable))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating clusterResourceOverride %s", croName))
		Expect(hubClient.Create(ctx, cro)).To(Succeed(), "Failed to create clusterResourceOverride %s", croName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting clusterResourceOverride %s", croName))
		cleanupClusterResourceOverride(croName)
	})

	It("should update CRP status as expected", func() {
		wantCRONames := []string{fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, croName, 0)}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", wantCRONames, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have override annotations on the member clusters", func() {
		for _, cluster := range allMemberClusters {
			wantAnnotations := map[string]string{croTestAnnotationKey: fmt.Sprintf("test-%s", cluster.ClusterName)}
			Expect(validateAnnotationOfWorkNamespaceOnCluster(cluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", cluster.ClusterName)
			Expect(validateOverrideAnnotationOfConfigMapOnCluster(cluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of configmap on %s", cluster.ClusterName)
		}
	})
})

var _ = Describe("creating clusterResourceOverride with incorrect path", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the cro before crp so that the observed resource index is predictable.
		cro := &placementv1alpha1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: croName,
			},
			Spec: placementv1alpha1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: workResourceSelector(),
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     fmt.Sprintf("/metadata/annotations/%s", croTestAnnotationKey),
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%s"`, croTestAnnotationValue))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating clusterResourceOverride %s", croName))
		Expect(hubClient.Create(ctx, cro)).To(Succeed(), "Failed to create clusterResourceOverride %s", croName)

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(),
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting clusterResourceOverride %s", croName))
		cleanupClusterResourceOverride(croName)
	})

	It("should update CRP status as expected", func() {
		wantCRONames := []string{fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, croName, 0)}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedFailedActual(workResourceIdentifiers(), allMemberClusterNames, "0", wantCRONames, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should not place the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)
})

var _ = Describe("creating clusterResourceOverride with and resource becomes invalid after override", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the cro before crp so that the observed resource index is predictable.
		cro := &placementv1alpha1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: croName,
			},
			Spec: placementv1alpha1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: workResourceSelector(),
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/annotations",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%s"`, croTestAnnotationValue))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating clusterResourceOverride %s", croName))
		Expect(hubClient.Create(ctx, cro)).To(Succeed(), "Failed to create clusterResourceOverride %s", croName)

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(),
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting clusterResourceOverride %s", croName))
		cleanupClusterResourceOverride(croName)
	})

	It("should update CRP status as expected", func() {
		wantCRONames := []string{fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, croName, 0)}
		crpStatusUpdatedActual := crpStatusWithWorkSynchronizedUpdatedFailedActual(workResourceIdentifiers(), allMemberClusterNames, "0", wantCRONames, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should not place the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)
})

var _ = Describe("creating clusterResourceOverride with delete rules for one cluster", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the cro before crp so that the observed resource index is predictable.
		cro := &placementv1alpha1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: croName,
			},
			Spec: placementv1alpha1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: workResourceSelector(),
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{regionLabelName: regionEast},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/annotations",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s"}`, croTestAnnotationKey1, croTestAnnotationValue1))},
								},
							},
						},
						//  Delete the resources on the region2 cluster.
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{regionLabelName: regionWest},
										},
									},
								},
							},
							OverrideType: placementv1alpha1.DeleteOverrideType,
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating clusterResourceOverride %s", croName))
		Expect(hubClient.Create(ctx, cro)).To(Succeed(), "Failed to create clusterResourceOverride %s", croName)

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(),
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting clusterResourceOverride %s", croName))
		cleanupClusterResourceOverride(croName)
	})

	It("should update CRP status as expected", func() {
		wantCRONames := []string{fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, croName, 0)}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", wantCRONames, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on the member clusters that are patched", func() {
		for idx := 0; idx < 2; idx++ {
			memberCluster := allMemberClusters[idx]
			workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
		}
	})

	It("should have override annotations on the member clusters that are patched", func() {
		for idx := 0; idx < 2; idx++ {
			cluster := allMemberClusters[idx]
			wantAnnotations := map[string]string{croTestAnnotationKey1: croTestAnnotationValue1}
			Expect(validateAnnotationOfWorkNamespaceOnCluster(cluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", cluster.ClusterName)
			Expect(validateOverrideAnnotationOfConfigMapOnCluster(cluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of configmap on %s", cluster.ClusterName)
		}
	})

	It("should not place the selected resources on the member clusters that are deleted", func() {
		memberCluster := allMemberClusters[2]
		Consistently(func() bool {
			ns := &corev1.Namespace{}
			workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
			if err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns); err != nil {
				return errors.IsNotFound(err)
			}
			return false
		}, consistentlyDuration, eventuallyInterval).Should(BeTrue(), "Failed to delete work resources on member cluster %s", memberCluster.ClusterName)
	})
})
