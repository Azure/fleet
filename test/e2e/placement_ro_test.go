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
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	scheduler "go.goms.io/fleet/pkg/scheduler/framework"
	"go.goms.io/fleet/pkg/utils/condition"
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
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				Placement: &placementv1beta1.PlacementRef{
					Name:  crpName, // assigned CRP name
					Scope: placementv1beta1.ClusterScoped,
				},
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)},
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

	It("update ro and change annotation value", func() {
		Eventually(func() error {
			ro := &placementv1beta1.ResourceOverride{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, ro); err != nil {
				return err
			}
			ro.Spec = placementv1beta1.ResourceOverrideSpec{
				Placement: &placementv1beta1.PlacementRef{
					Name: crpName,
				},
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 1)},
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

	It("update ro and no update on the configmap itself", func() {
		Eventually(func() error {
			ro := &placementv1beta1.ResourceOverride{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, ro); err != nil {
				return err
			}
			ro.Spec.Policy.OverrideRules = append(ro.Spec.Policy.OverrideRules, placementv1beta1.OverrideRule{
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
				OverrideType: placementv1beta1.DeleteOverrideType,
			})
			return hubClient.Update(ctx, ro)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", crpName)
	})

	It("should refresh the CRP status even as there is no change on the resources", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 2)},
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
	roSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/annotations",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s"}`, roTestAnnotationKey, roTestAnnotationValue))},
								},
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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
		// wait until the snapshot is created so that the observed resource index is predictable.
		Eventually(func() error {
			roSnap := &placementv1beta1.ResourceOverrideSnapshot{}
			return hubClient.Get(ctx, types.NamespacedName{Name: roSnapShotName, Namespace: roNamespace}, roSnap)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", crpName)

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
			{Namespace: roNamespace, Name: roSnapShotName},
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
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				Placement: &placementv1beta1.PlacementRef{
					Name: crpName, // assigned CRP name
				},
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
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
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should have override annotations on the configmap", func() {
		for i, cluster := range allMemberClusters {
			wantAnnotations := map[string]string{roTestAnnotationKey: fmt.Sprintf("%s-%d", roTestAnnotationValue, i)}
			Expect(validateAnnotationOfConfigMapOnCluster(cluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of configmap on %s", cluster.ClusterName)
		}
	})
})

var _ = Context("creating resourceOverride and clusterResourceOverride, resourceOverride should win to override configMap", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	roSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)
	croSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, croName, 0)

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()
		cro := &placementv1beta1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: croName,
			},
			Spec: placementv1beta1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: workResourceSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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
		// wait until the snapshot is created so that the observed resource index is predictable.
		Eventually(func() error {
			roSnap := &placementv1beta1.ResourceOverrideSnapshot{}
			return hubClient.Get(ctx, types.NamespacedName{Name: roSnapShotName, Namespace: roNamespace}, roSnap)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", crpName)
		Eventually(func() error {
			croSnap := &placementv1beta1.ClusterResourceOverrideSnapshot{}
			return hubClient.Get(ctx, types.NamespacedName{Name: croSnapShotName}, croSnap)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", crpName)

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
			{Namespace: roNamespace, Name: roSnapShotName},
		}
		wantCRONames := []string{croSnapShotName}
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
			Expect(validateAnnotationOfConfigMapOnCluster(cluster, want)).ShouldNot(Succeed(), "ResourceOverride Should win, ClusterResourceOverride annotated on $s", cluster.ClusterName)
		}
	})
})

var _ = Context("creating resourceOverride with incorrect path", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	roSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()
		// Create the bad ro.
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				Placement: &placementv1beta1.PlacementRef{
					Name: crpName, // assigned CRP name
				},
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
									Path:     fmt.Sprintf("/metadata/annotations/%s", roTestAnnotationKey),
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%s"`, roTestAnnotationValue))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating the bad resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)
		// wait until the snapshot is created so that failed override won't block the rollout
		Eventually(func() error {
			roSnap := &placementv1beta1.ResourceOverrideSnapshot{}
			return hubClient.Get(ctx, types.NamespacedName{Name: roSnapShotName, Namespace: roNamespace}, roSnap)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", crpName)

		// Create the CRP later
		createCRP(crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)
	})

	It("should update CRP status as failed to override", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: roSnapShotName},
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
						MaxUnavailable:           ptr.To(intstr.FromString("100%")),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)
	})

	// Verify the status before creating the overrides, so that we can be certain about the resource index
	// to check for when the override is actually being picked up by Fleet agents.
	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("can create a override that breaks the resource", func() {
		// Create the ro.
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				Placement: &placementv1beta1.PlacementRef{
					Name: crpName, // assigned CRP name
				},
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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

	It("should update CRP status as expected", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)},
		}
		crpStatusUpdatedActual := crpStatusWithWorkSynchronizedUpdatedFailedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// For simplicity reasons, this test spec will only check if the annotation hasn't been added.
	It("should not place the selected resources on member clusters", func() {
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]
			Eventually(validateConfigMapNoAnnotationKeyOnCluster(memberCluster, roTestAnnotationKey)).Should(Succeed(), "Failed to find the annotation of config map on %s", memberCluster.ClusterName)
		}
	})
})

var _ = Context("creating resourceOverride with a templated rules with cluster name to override configMap", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	roSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the ro before crp so that the observed resource index is predictable.
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
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
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpReplace,
									Path:     "/data/data",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%s"`, placementv1beta1.OverrideClusterNameVariable))},
								},
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
									Path:     "/data/newField",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"new-%s"`, placementv1beta1.OverrideClusterNameVariable))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)
		Eventually(func() error {
			roSnap := &placementv1beta1.ResourceOverrideSnapshot{}
			return hubClient.Get(ctx, types.NamespacedName{Name: roSnapShotName, Namespace: roNamespace}, roSnap)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", crpName)

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
			{Namespace: roNamespace, Name: roSnapShotName},
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
	roSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the ro before crp so that the observed resource index is predictable.
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
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
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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
							OverrideType: placementv1beta1.DeleteOverrideType,
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)
		Eventually(func() error {
			roSnap := &placementv1beta1.ResourceOverrideSnapshot{}
			return hubClient.Get(ctx, types.NamespacedName{Name: roSnapShotName, Namespace: roNamespace}, roSnap)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", crpName)

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
			{Namespace: roNamespace, Name: roSnapShotName},
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
			Expect(validateAnnotationOfConfigMapOnCluster(cluster, wantAnnotations)).Should(Succeed(), "Failed to override the annotation of configmap on %s", cluster.ClusterName)
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

var _ = Context("creating resourceOverride with a templated rules with cluster label key replacement", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the ro before crp so that the observed resource index is predictable.
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				Placement: &placementv1beta1.PlacementRef{
					Name: crpName, // assigned CRP name
				},
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
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
												{
													Key:      envLabelName,
													Operator: metav1.LabelSelectorOpExists,
												},
											},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
									Path:     "/data/region",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%s%s}"`, placementv1beta1.OverrideClusterLabelKeyVariablePrefix, regionLabelName))},
								},
								{
									Operator: placementv1beta1.JSONPatchOverrideOpReplace,
									Path:     "/data/data",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"newdata-%s%s}"`, placementv1beta1.OverrideClusterLabelKeyVariablePrefix, envLabelName))},
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
			{Namespace: roNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should replace the cluster label key in the configMap", func() {
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
		cmNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		for _, cluster := range allMemberClusters {
			wantConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: cmNamespace,
				},
				Data: map[string]string{
					"data":   fmt.Sprintf("newdata-%s", labelsByClusterName[cluster.ClusterName][envLabelName]),
					"region": labelsByClusterName[cluster.ClusterName][regionLabelName],
				},
			}
			configMapActual := configMapPlacedOnClusterActual(cluster, wantConfigMap)
			Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update configmap %s data as expected", cmName)
		}
	})

	It("should handle non-existent cluster label key gracefully", func() {
		By("Update the ResourceOverride to use a non-existent label key")
		Eventually(func() error {
			ro := &placementv1beta1.ResourceOverride{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, ro); err != nil {
				return err
			}
			ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides[0].Value = apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%snon-existent-label}"`, placementv1beta1.OverrideClusterLabelKeyVariablePrefix))}
			return hubClient.Update(ctx, ro)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update resourceOverride %s with non-existent label key", roName)

		By("Verify the CRP status should have one cluster failed to override while the rest stuck in rollout")
		Eventually(func() error {
			crp := &placementv1beta1.ClusterResourcePlacement{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
				return err
			}
			wantCondition := []metav1.Condition{
				{
					Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             scheduler.FullyScheduledReason,
					ObservedGeneration: crp.Generation,
				},
				{
					Type:               string(placementv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
					Status:             metav1.ConditionFalse,
					Reason:             condition.RolloutNotStartedYetReason,
					ObservedGeneration: crp.Generation,
				},
			}
			if diff := cmp.Diff(crp.Status.Conditions, wantCondition, placementStatusCmpOptions...); diff != "" {
				return fmt.Errorf("CRP condition diff (-got, +want): %s", diff)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "CRP %s failed to show the override failed and stuck in rollout", crpName)

		By("Verify the configMap remains unchanged")
		cmName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
		cmNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		for _, cluster := range allMemberClusters {
			wantConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: cmNamespace,
				},
				Data: map[string]string{
					"data":   fmt.Sprintf("newdata-%s", labelsByClusterName[cluster.ClusterName][envLabelName]),
					"region": labelsByClusterName[cluster.ClusterName][regionLabelName],
				},
			}
			configMapActual := configMapPlacedOnClusterActual(cluster, wantConfigMap)
			Consistently(configMapActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "ConfigMap %s should remain unchanged", cmName)
		}
	})
})

var _ = Context("creating resourceOverride with non-exist label", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	roSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()
		// Create the bad ro.
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				Placement: &placementv1beta1.PlacementRef{
					Name: crpName, // assigned CRP name
				},
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
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
												{
													Key:      envLabelName,
													Operator: metav1.LabelSelectorOpExists,
												},
											},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
									Path:     "/data/region",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%s%s}"`, placementv1beta1.OverrideClusterLabelKeyVariablePrefix, "non-existent-label"))},
								},
								{
									Operator: placementv1beta1.JSONPatchOverrideOpReplace,
									Path:     "/data/data",
									Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"newdata-%s%s}"`, placementv1beta1.OverrideClusterLabelKeyVariablePrefix, envLabelName))},
								},
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating the bad resourceOverride %s", roName))
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)
		Eventually(func() error {
			roSnap := &placementv1beta1.ResourceOverrideSnapshot{}
			return hubClient.Get(ctx, types.NamespacedName{Name: roSnapShotName, Namespace: roNamespace}, roSnap)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", crpName)

		// Create the CRP later so that failed override won't block the rollout
		createCRP(crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)
	})

	It("should update CRP status as failed to override", func() {
		wantRONames := []placementv1beta1.NamespacedName{
			{Namespace: roNamespace, Name: roSnapShotName},
		}
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedFailedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should not place the selected resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)
})

var _ = Context("creating resourceOverride with namespace scope should not apply override", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()
		// Create the CRP.
		createCRP(crpName)
		// Create the ro with namespace scope.
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				Placement: &placementv1beta1.PlacementRef{
					Name:  crpName, // assigned CRP name
					Scope: placementv1beta1.NamespaceScoped,
				},
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)
	})

	It("should update CRP status as expected without override", func() {
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, "0", nil, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will ignore the annotation of resources.
	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should not have override annotations on the configmap", func() {
		for _, memberCluster := range allMemberClusters {
			Expect(validateConfigMapNoAnnotationKeyOnCluster(memberCluster, roTestAnnotationKey)).Should(Succeed(), "Failed to validate no override annotation on config map on %s", memberCluster.ClusterName)
		}
	})
})

var _ = Context("creating resourceOverride but namespace-only CRP should not apply override", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()
		// Create the namespace-only CRP.
		createNamespaceOnlyCRP(crpName)
		// Create the ro with cluster scope referring to the namespace-only CRP.
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				Placement: &placementv1beta1.PlacementRef{
					Name:  crpName, // assigned CRP name
					Scope: placementv1beta1.ClusterScoped,
				},
				ResourceSelectors: configMapOverrideSelector(),
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
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
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		By(fmt.Sprintf("deleting resourceOverride %s", roName))
		cleanupResourceOverride(roName, roNamespace)
	})

	It("should update CRP status as expected without override", func() {
		// Since the CRP is namespace-only, configMap is not placed, so no override should be applied.
		crpStatusUpdatedActual := crpStatusWithOverrideUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, "0", nil, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	// This check will verify that only namespace is placed, not the configmap.
	It("should place only the namespace on member clusters", checkIfPlacedNamespaceResourceOnAllMemberClusters)

	It("should not place the configmap on member clusters since CRP is namespace-only", func() {
		for _, memberCluster := range allMemberClusters {
			namespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
			configMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
			configMap := corev1.ConfigMap{}
			err := memberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: namespaceName}, &configMap)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "ConfigMap should not be placed on member cluster %s since CRP is namespace-only", memberCluster.ClusterName)
		}
	})
})
