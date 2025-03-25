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

// Note that this container will run in parallel with other containers.
var _ = Context("creating resourceOverride (selecting all clusters) to override configMap", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()
		// Create the CRP.
		createCRP(crpName)
		// Create the ro.
		ro := &placementv1alpha1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1alpha1.ResourceOverrideSpec{
				Placement: &placementv1alpha1.PlacementRef{
					Name: crpName, // assigned CRP name
				},
				ResourceSelectors: configMapSelector(),
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
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s"}`, roTestAnnotationKey, roTestAnnotationValue))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)

		By("should update CRP status to not select any override")
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

		By("should not have annotations on the configmap")
		for _, memberCluster := range allMemberClusters {
			Expect(validateConfigMapNoAnnotationKeyOnCluster(memberCluster, roTestAnnotationKey)).Should(Succeed(), "Failed to remove the annotation of config map on %s", memberCluster.ClusterName)
		}

		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update CRP status as expected", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, roName, 0)},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have override annotations on the configmap", func() {
		want := map[string]string{roTestAnnotationKey: roTestAnnotationValue}
		checkIfOverrideAnnotationsOnAllMemberClusters(false, want)
	})

	It("update ro attached to this CRP only and change annotation value", func() {
		Eventually(func() error {
			ro := &placementv1alpha1.ResourceOverride{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, ro); err != nil {
				return err
			}
			ro.Spec = placementv1alpha1.ResourceOverrideSpec{
				Placement: &placementv1alpha1.PlacementRef{
					Name: crpName,
				},
				ResourceSelectors: configMapSelector(),
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
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s"}`, roTestAnnotationKey, roTestAnnotationValue1))},
								},
							},
						},
					},
				},
			}
			return hubClient.Update(ctx, ro)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", crpName)
	})

	It("should update CRP status as expected", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, roName, 1)},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have override annotations on the configmap", func() {
		want := map[string]string{roTestAnnotationKey: roTestAnnotationValue1}
		checkIfOverrideAnnotationsOnAllMemberClusters(false, want)
	})

	It("update ro attached to this CRP only and no update on the configmap itself", func() {
		Eventually(func() error {
			ro := &placementv1alpha1.ResourceOverride{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, ro); err != nil {
				return err
			}
			ro.Spec.Policy.OverrideRules = append(ro.Spec.Policy.OverrideRules, placementv1alpha1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"invalid-key": "invalid-value",
								},
							},
						},
					},
				},
				OverrideType: placementv1alpha1.DeleteOverrideType,
			})
			return hubClient.Update(ctx, ro)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", crpName)
	})

	It("should update CRP status as expected", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, roName, 2)},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have override annotations on the configmap", func() {
		want := map[string]string{roTestAnnotationKey: roTestAnnotationValue1}
		checkIfOverrideAnnotationsOnAllMemberClusters(false, want)
	})
})

var _ = Context("creating resourceOverride with multiple jsonPatchOverrides to override configMap", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the ro before crp so that the observed resource index is predictable.
		ro := &placementv1alpha1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1alpha1.ResourceOverrideSpec{
				ResourceSelectors: configMapSelector(),
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
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s"}`, roTestAnnotationKey, roTestAnnotationValue))},
								},
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     fmt.Sprintf("/metadata/annotations/%s", roTestAnnotationKey1),
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%s"`, roTestAnnotationValue1))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)
	})

	It("should update CRP status as expected", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, roName, 0)},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have override annotations on the configmap", func() {
		wantAnnotations := map[string]string{roTestAnnotationKey: roTestAnnotationValue, roTestAnnotationKey1: roTestAnnotationValue1}
		checkIfOverrideAnnotationsOnAllMemberClusters(false, wantAnnotations)
	})

	It("update ro attached to an invalid CRP", func() {
		Eventually(func() error {
			ro := &placementv1alpha1.ResourceOverride{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, ro); err != nil {
				return err
			}
			ro.Spec.Placement = &placementv1alpha1.PlacementRef{
				Name: "invalid-crp", // assigned CRP name
			}
			return hubClient.Update(ctx, ro)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", crpName)
	})

	It("CRP status should not be changed", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, roName, 0)},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "CRP %s status has been changed", crpName)
	})

})

var _ = Context("creating resourceOverride with different rules for each cluster to override configMap", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()
		// Create the CRP.
		createCRP(crpName)
		// Create the ro.
		ro := &placementv1alpha1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1alpha1.ResourceOverrideSpec{
				Placement: &placementv1alpha1.PlacementRef{
					Name: crpName, // assigned CRP name
				},
				ResourceSelectors: configMapSelector(),
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
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s-0"}`, roTestAnnotationKey, roTestAnnotationValue))},
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
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s-1"}`, roTestAnnotationKey, roTestAnnotationValue))},
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
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s-2"}`, roTestAnnotationKey, roTestAnnotationValue))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)
	})

	It("should update CRP status as expected", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, roName, 0)},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have override annotations on the configmap", func() {
		for i, cluster := range allMemberClusters {
			wantAnnotations := map[string]string{roTestAnnotationKey: fmt.Sprintf("%s-%d", roTestAnnotationValue, i)}
			Expect(validateOverrideAnnotationOfConfigMapOnCluster(cluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of configmap on %s", cluster.ClusterName)
		}
	})
})

var _ = Context("creating resourceOverride and clusterResourceOverride, resourceOverride should win to override configMap", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()
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
		// Create the ro before crp so that the observed resource index is predictable.
		ro := &placementv1alpha1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1alpha1.ResourceOverrideSpec{
				ResourceSelectors: configMapSelector(),
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
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s"}`, roTestAnnotationKey, roTestAnnotationValue))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)

		By(fmt.Sprintf("deleting clusterResourceOverride %s", croName))
		cleanupClusterResourceOverride(croName)
	})

	It("should update CRP status as expected", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, roName, 0)},
		}
		wantCRONames := []string{fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, croName, 0)}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", wantCRONames, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have resource override annotations on the configmap", func() {
		want := map[string]string{roTestAnnotationKey: roTestAnnotationValue}
		checkIfOverrideAnnotationsOnAllMemberClusters(false, want)
	})

	It("should not have cluster resource override annotations on the configmap, but present on configmap", func() {
		want := map[string]string{croTestAnnotationKey: croTestAnnotationValue}
		for _, cluster := range allMemberClusters {
			Expect(validateAnnotationOfWorkNamespaceOnCluster(cluster, want)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", cluster.ClusterName)
			Expect(validateOverrideAnnotationOfConfigMapOnCluster(cluster, want)).ShouldNot(Succeed(), "ResourceOverride Should win, ClusterResourceOverride annotated on $s", cluster.ClusterName)
		}
	})
})

var _ = Context("creating resourceOverride with incorrect path", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()
		// Create the ro.
		ro := &placementv1alpha1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1alpha1.ResourceOverrideSpec{
				Placement: &placementv1alpha1.PlacementRef{
					Name: crpName, // assigned CRP name
				},
				ResourceSelectors: configMapSelector(),
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     fmt.Sprintf("/metadata/annotations/%s", roTestAnnotationKey),
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%s"`, roTestAnnotationValue))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)

		// Create the CRP later so that failed override won't block the rollout
		createCRP(crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)
	})

	It("should update CRP status as expected", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, roName, 0)},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedFailedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should not place the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)
})

var _ = Context("creating resourceOverride and resource becomes invalid after override", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()
		// Create the CRP.
		createCRP(crpName)
		// Create the ro.
		ro := &placementv1alpha1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1alpha1.ResourceOverrideSpec{
				Placement: &placementv1alpha1.PlacementRef{
					Name: crpName, // assigned CRP name
				},
				ResourceSelectors: configMapSelector(),
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
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%s"`, roTestAnnotationValue))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)
	})

	It("should update CRP status as expected", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, roName, 0)},
		}
		crpStatusUpdatedActual := crpStatusWithWorkSynchronizedUpdatedFailedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should not place the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)
})

var _ = Context("creating resourceOverride with a templated rules with cluster name to override configMap", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the ro before crp so that the observed resource index is predictable.
		ro := &placementv1alpha1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1alpha1.ResourceOverrideSpec{
				ResourceSelectors: configMapSelector(),
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
									Operator: placementv1alpha1.JSONPatchOverrideOpReplace,
									Path:     "/data/data",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%s"`, placementv1alpha1.OverrideClusterNameVariable))},
								},
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
									Path:     "/data/newField",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"new-%s"`, placementv1alpha1.OverrideClusterNameVariable))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)
	})

	It("should update CRP status as expected", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, roName, 0)},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should have override configMap on the member clusters", func() {
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
		cmNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		for _, cluster := range allMemberClusters {
			wantConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: cmNamespace,
				},
				Data: map[string]string{
					"data":     cluster.ClusterName,
					"newField": fmt.Sprintf("new-%s", cluster.ClusterName),
				},
			}
			configMapActual := configMapPlacedOnClusterActual(cluster, wantConfigMap)
			Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update configmap %s data as expected", cmName)
		}
	})
})

var _ = Context("creating resourceOverride with delete configMap", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the ro before crp so that the observed resource index is predictable.
		ro := &placementv1alpha1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1alpha1.ResourceOverrideSpec{
				ResourceSelectors: configMapSelector(),
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
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s"}`, roTestAnnotationKey, roTestAnnotationValue))},
								},
							},
						},
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
		By(fmt.Sprintf("creating resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)
	})

	It("should update CRP status as expected", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, roName, 0)},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the namespaces on all member clusters", func() {
		for idx := 0; idx < 3; idx++ {
			memberCluster := allMemberClusters[idx]
			workResourcesPlacedActual := workNamespacePlacedOnClusterActual(memberCluster)
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
		}
	})

	// This check will ignore the annotation of resources.
	It("should place the configmap on member clusters that are patched", func() {
		for idx := 0; idx < 2; idx++ {
			memberCluster := allMemberClusters[idx]
			workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
		}
	})

	It("should have override annotations on the configmap on the member clusters that are patched", func() {
		for idx := 0; idx < 2; idx++ {
			cluster := allMemberClusters[idx]
			wantAnnotations := map[string]string{roTestAnnotationKey: roTestAnnotationValue}
			Expect(validateOverrideAnnotationOfConfigMapOnCluster(cluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of configmap on %s", cluster.ClusterName)
		}
	})

	It("should not place the configmap on the member clusters that are deleted", func() {
		memberCluster := allMemberClusters[2]
		Consistently(func() bool {
			namespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
			configMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
			configMap := corev1.ConfigMap{}
			err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: namespaceName}, &configMap)
			return errors.IsNotFound(err)
		}, consistentlyDuration, consistentlyInterval).Should(BeTrue(), "Failed to delete work resources on member cluster %s", memberCluster.ClusterName)
	})
})
