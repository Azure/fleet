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

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	scheduler "github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
)

var _ = Describe("placing namespaced scoped resources using a RP with ResourceOverride", Label("resourceplacement"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeEach(OncePerOrdered, func() {
		By("creating namespace")
		createNamespace()

		// Create the CRP with Namespace-only selector.
		createNamespaceOnlyCRP(crpName)

		By("should update CRP status as expected")
		crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("creating resourceOverride (selecting all clusters) to override configMap for ResourcePlacement", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
		workNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			createConfigMap()

			// Create the RP in the same namespace selecting namespaced resources.
			createRP(workNamespace, rpName)

			// Create the ro.
			ro := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: workNamespace,
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name:  rpName, // assigned RP name
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
			By(fmt.Sprintf("deleting resourceOverride %s", roName))
			cleanupResourceOverride(roName, workNamespace)

			By("should update RP status to not select any override")
			rpStatusUpdatedActual := rpStatusWithOverrideUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", workNamespace, rpName)

			By("should not have annotations on the configmap")
			for _, memberCluster := range allMemberClusters {
				Expect(validateConfigMapNoAnnotationKeyOnCluster(memberCluster, roTestAnnotationKey)).Should(Succeed(), "Failed to remove the annotation of config map on %s", memberCluster.ClusterName)
			}

			By(fmt.Sprintf("deleting resource placement %s/%s and related resources", workNamespace, rpName))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters)
		})

		It("should update RP status as expected", func() {
			wantRONames := []placementv1beta1.NamespacedName{
				{Namespace: workNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)},
			}
			rpStatusUpdatedActual := rpStatusWithOverrideUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		// This check will ignore the annotation of resources.
		It("should place the resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should have override annotations on the configmap", func() {
			want := map[string]string{roTestAnnotationKey: roTestAnnotationValue}
			checkIfOverrideAnnotationsOnAllMemberClusters(false, want)
		})

		It("update ro and change annotation value", func() {
			Eventually(func() error {
				ro := &placementv1beta1.ResourceOverride{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: workNamespace}, ro); err != nil {
					return err
				}
				ro.Spec = placementv1beta1.ResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name:  rpName, // assigned RP name
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
										Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s"}`, roTestAnnotationKey, roTestAnnotationValue1))},
									},
								},
							},
						},
					},
				}
				return hubClient.Update(ctx, ro)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", rpName)
		})

		It("should update RP status as expected after RO update", func() {
			wantRONames := []placementv1beta1.NamespacedName{
				{Namespace: workNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 1)},
			}
			rpStatusUpdatedActual := rpStatusWithOverrideUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		// This check will ignore the annotation of resources.
		It("should place the selected resources on member clusters after RO update", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should have updated override annotations on the configmap", func() {
			want := map[string]string{roTestAnnotationKey: roTestAnnotationValue1}
			checkIfOverrideAnnotationsOnAllMemberClusters(false, want)
		})

		It("update ro and no update on the configmap itself", func() {
			Eventually(func() error {
				ro := &placementv1beta1.ResourceOverride{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: workNamespace}, ro); err != nil {
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

		It("should refresh the RP status even as there is no change on the resources", func() {
			wantRONames := []placementv1beta1.NamespacedName{
				{Namespace: workNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 2)},
			}
			rpStatusUpdatedActual := rpStatusWithOverrideUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		// This check will ignore the annotation of resources.
		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should have override annotations on the configmap", func() {
			want := map[string]string{roTestAnnotationKey: roTestAnnotationValue1}
			checkIfOverrideAnnotationsOnAllMemberClusters(false, want)
		})
	})

	Context("creating resourceOverride with multiple jsonPatchOverrides to override configMap for ResourcePlacement", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
		workNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		roSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)

		BeforeAll(func() {
			createConfigMap()

			ro := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: workNamespace,
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
				return hubClient.Get(ctx, types.NamespacedName{Name: roSnapShotName, Namespace: workNamespace}, roSnap)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", rpName)

			// Create the RP.
			createRP(workNamespace, rpName)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting resource placement %s/%s and related resources", workNamespace, rpName))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters)

			By(fmt.Sprintf("deleting resourceOverride %s", roName))
			cleanupResourceOverride(roName, workNamespace)
		})

		It("should update RP status as expected", func() {
			wantRONames := []placementv1beta1.NamespacedName{
				{Namespace: workNamespace, Name: roSnapShotName},
			}
			rpStatusUpdatedActual := rpStatusWithOverrideUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		// This check will ignore the annotation of resources.
		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should have override annotations on the configmap", func() {
			wantAnnotations := map[string]string{roTestAnnotationKey: roTestAnnotationValue, roTestAnnotationKey1: roTestAnnotationValue1}
			checkIfOverrideAnnotationsOnAllMemberClusters(false, wantAnnotations)
		})
	})

	Context("creating resourceOverride with different rules for each cluster to override configMap for ResourcePlacement", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
		workNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			createConfigMap()

			// Create the RP.
			createRP(workNamespace, rpName)

			// Create the ro.
			ro := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: workNamespace,
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name:  rpName, // assigned RP name
						Scope: placementv1beta1.NamespaceScoped,
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
			By(fmt.Sprintf("deleting resource placement %s/%s and related resources", workNamespace, rpName))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters)

			By(fmt.Sprintf("deleting resourceOverride %s", roName))
			cleanupResourceOverride(roName, workNamespace)
		})

		It("should update RP status as expected", func() {
			wantRONames := []placementv1beta1.NamespacedName{
				{Namespace: workNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)},
			}
			rpStatusUpdatedActual := rpStatusWithOverrideUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
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

	Context("creating resourceOverride with incorrect path for ResourcePlacement", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
		workNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		roSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)

		BeforeAll(func() {
			createConfigMap()

			// Create the bad ro.
			ro := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: workNamespace,
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name:  rpName, // assigned RP name
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
				return hubClient.Get(ctx, types.NamespacedName{Name: roSnapShotName, Namespace: workNamespace}, roSnap)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", rpName)

			// Create the RP later
			createRP(workNamespace, rpName)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting resource placement %s/%s and related resources", workNamespace, rpName))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters)

			By(fmt.Sprintf("deleting resourceOverride %s", roName))
			cleanupResourceOverride(roName, workNamespace)
		})

		It("should update RP status as failed to override", func() {
			wantRONames := []placementv1beta1.NamespacedName{
				{Namespace: workNamespace, Name: roSnapShotName},
			}
			rpStatusUpdatedActual := rpStatusWithOverrideUpdatedFailedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		// This check will ignore the annotation of resources.
		It("should not place the selected resources on member clusters", checkIfRemovedConfigMapFromAllMemberClusters)
	})

	Context("creating resourceOverride and resource becomes invalid after override for ResourcePlacement", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
		workNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			createConfigMap()

			// Create the RP.
			createRP(workNamespace, rpName)

			// Create the ro.
			ro := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: workNamespace,
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name:  rpName, // assigned RP name
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
			By(fmt.Sprintf("deleting resource placement %s/%s and related resources", workNamespace, rpName))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters)

			By(fmt.Sprintf("deleting resourceOverride %s", roName))
			cleanupResourceOverride(roName, workNamespace)
		})

		It("should update RP status as expected", func() {
			wantRONames := []placementv1beta1.NamespacedName{
				{Namespace: workNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)},
			}
			rpStatusUpdatedActual := rpStatusWithWorkSynchronizedUpdatedFailedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		// This check will ignore the annotation of resources.
		It("should not place the selected resources on member clusters", checkIfRemovedConfigMapFromAllMemberClusters)
	})

	Context("creating resourceOverride with templated rules with cluster name to override configMap for ResourcePlacement", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
		workNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		roSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)

		BeforeAll(func() {
			createConfigMap()

			// Create the ro before rp so that the observed resource index is predictable.
			ro := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: workNamespace,
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
				return hubClient.Get(ctx, types.NamespacedName{Name: roSnapShotName, Namespace: workNamespace}, roSnap)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", rpName)

			// Create the RP.
			createRP(workNamespace, rpName)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting resource placement %s/%s and related resources", workNamespace, rpName))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters)

			By(fmt.Sprintf("deleting resourceOverride %s", roName))
			cleanupResourceOverride(roName, workNamespace)
		})

		It("should update RP status as expected", func() {
			wantRONames := []placementv1beta1.NamespacedName{
				{Namespace: workNamespace, Name: roSnapShotName},
			}
			rpStatusUpdatedActual := rpStatusWithOverrideUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
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

	Context("creating resourceOverride with delete configMap for ResourcePlacement", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
		workNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		roSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)

		BeforeAll(func() {
			createConfigMap()

			// Create the ro before rp so that the observed resource index is predictable.
			ro := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: workNamespace,
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
				return hubClient.Get(ctx, types.NamespacedName{Name: roSnapShotName, Namespace: workNamespace}, roSnap)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", rpName)

			// Create the RP.
			createRP(workNamespace, rpName)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting resource placement %s/%s and related resources", workNamespace, rpName))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters)

			By(fmt.Sprintf("deleting resourceOverride %s", roName))
			cleanupResourceOverride(roName, workNamespace)
		})

		It("should update RP status as expected", func() {
			wantRONames := []placementv1beta1.NamespacedName{
				{Namespace: workNamespace, Name: roSnapShotName},
			}
			rpStatusUpdatedActual := rpStatusWithOverrideUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

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

	Context("creating resourceOverride with templated rules with cluster label key replacement for ResourcePlacement", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
		workNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			createConfigMap()

			// Create the ro before rp so that the observed resource index is predictable.
			ro := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: workNamespace,
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name:  rpName, // assigned RP name
						Scope: placementv1beta1.NamespaceScoped,
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

			// Create the RP.
			createRP(workNamespace, rpName)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting resource placement %s/%s and related resources", workNamespace, rpName))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters)

			By(fmt.Sprintf("deleting resourceOverride %s", roName))
			cleanupResourceOverride(roName, workNamespace)
		})

		It("should update RP status as expected", func() {
			wantRONames := []placementv1beta1.NamespacedName{
				{Namespace: workNamespace, Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)},
			}
			rpStatusUpdatedActual := rpStatusWithOverrideUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
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
				if err := hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: workNamespace}, ro); err != nil {
					return err
				}
				ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides[0].Value = apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`"%snon-existent-label}"`, placementv1beta1.OverrideClusterLabelKeyVariablePrefix))}
				return hubClient.Update(ctx, ro)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update resourceOverride %s with non-existent label key", roName)

			By("Verify the RP status should have one cluster failed to override while the rest stuck in rollout")
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespace}, rp); err != nil {
					return err
				}
				wantCondition := []metav1.Condition{
					{
						Type:               string(placementv1beta1.ResourcePlacementScheduledConditionType),
						Status:             metav1.ConditionTrue,
						Reason:             scheduler.FullyScheduledReason,
						ObservedGeneration: rp.Generation,
					},
					{
						Type:               string(placementv1beta1.ResourcePlacementRolloutStartedConditionType),
						Status:             metav1.ConditionFalse,
						Reason:             condition.RolloutNotStartedYetReason,
						ObservedGeneration: rp.Generation,
					},
				}
				if diff := cmp.Diff(rp.Status.Conditions, wantCondition, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("RP condition diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "RP %s failed to show the override failed and stuck in rollout", rpName)

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

	Context("creating resourceOverride with non-exist label for ResourcePlacement", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
		workNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		roSnapShotName := fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, roName, 0)

		BeforeAll(func() {
			createConfigMap()

			// Create the bad ro.
			ro := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: workNamespace,
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name:  rpName, // assigned RP name
						Scope: placementv1beta1.NamespaceScoped,
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
				return hubClient.Get(ctx, types.NamespacedName{Name: roSnapShotName, Namespace: workNamespace}, roSnap)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update ro as expected", rpName)

			// Create the RP later so that failed override won't block the rollout
			createRP(workNamespace, rpName)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting resource placement %s/%s and related resources", workNamespace, rpName))
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespace}, allMemberClusters)

			By(fmt.Sprintf("deleting resourceOverride %s", roName))
			cleanupResourceOverride(roName, workNamespace)
		})

		It("should update RP status as failed to override", func() {
			wantRONames := []placementv1beta1.NamespacedName{
				{Namespace: workNamespace, Name: roSnapShotName},
			}
			rpStatusUpdatedActual := rpStatusWithOverrideUpdatedFailedActual(appConfigMapIdentifiers(), allMemberClusterNames, "0", nil, wantRONames)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		// This check will ignore the annotation of resources.
		It("should not place the selected resources on member clusters", checkIfRemovedConfigMapFromAllMemberClusters)
	})
})
