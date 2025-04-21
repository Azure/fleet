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

package v1alpha1

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workv1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	testutils "github.com/kubefleet-dev/kubefleet/test/e2e/v1alpha1/utils"
)

var _ = Describe("Work API Controller test", func() {

	const (
		conditionTypeApplied = "Applied"
		specHashAnnotation   = "fleet.azure.com/spec-hash"
	)

	var (
		ctx context.Context

		// Comparison Options
		cmpOptions = []cmp.Option{
			cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime", "ObservedGeneration", "Reason"),
			cmpopts.IgnoreFields(metav1.OwnerReference{}, "BlockOwnerDeletion"),
			cmpopts.IgnoreFields(workapi.ResourceIdentifier{}, "Ordinal"),
			cmpopts.IgnoreFields(metav1.ObjectMeta{},
				"UID",
				"ResourceVersion",
				"Generation",
				"CreationTimestamp",
				"Annotations",
				"OwnerReferences",
				"ManagedFields",
				"Labels"),
		}

		appliedWorkCmpOptions = append(cmpOptions, cmpopts.IgnoreFields(workapi.AppliedResourceMeta{}, "UID"))

		crdCmpOptions = append(cmpOptions,
			cmpopts.IgnoreFields(apiextensionsv1.CustomResourceDefinition{}, "Status"),
			cmpopts.IgnoreFields(apiextensionsv1.CustomResourceDefinitionSpec{}, "Versions", "Conversion"))

		secretCmpOptions = append(cmpOptions,
			cmpopts.IgnoreFields(corev1.Secret{}, "TypeMeta"),
		)

		resourceNamespace *corev1.Namespace
		workName          string
	)

	BeforeEach(func() {
		ctx = context.Background()

		// This namespace in MemberCluster will store specified test resources created from the Work-api.
		resourceNamespaceName := "resource-namespace" + utils.RandStr()
		resourceNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourceNamespaceName,
			},
		}
		Expect(MemberCluster.KubeClient.Create(ctx, resourceNamespace)).Should(Succeed(), "Failed to create namespace %s in %s cluster", resourceNamespace.Name, MemberCluster.ClusterName)
	})

	AfterEach(func() {
		testutils.DeleteNamespace(ctx, *MemberCluster, resourceNamespace)
	})

	Context("Work Creation Test", func() {
		BeforeEach(func() {
			workName = testutils.RandomWorkName(5)
		})

		AfterEach(func() {
			testutils.DeleteWork(ctx, *HubCluster, workapi.Work{ObjectMeta: metav1.ObjectMeta{Name: workName, Namespace: workNamespace.Name}})
		})

		It("Upon successful work creation of a single resource, work manifest is applied and resource is created", func() {
			By(fmt.Sprintf("Here is the work Name %s", workName))

			// Configmap will be included in this work object.
			manifestConfigMapName := "work-configmap"
			manifestConfigMap := corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      manifestConfigMapName,
					Namespace: resourceNamespace.Name,
				},
				Data: map[string]string{
					"test-key": "test-data",
				},
			}

			// Creating types.NamespacedName to use in retrieving objects.
			namespaceType := types.NamespacedName{Name: workName, Namespace: workNamespace.Name}

			//Creating types.NamespacedName for the resource created.
			resourceNamespaceType := types.NamespacedName{Name: manifestConfigMapName, Namespace: resourceNamespace.Name}

			manifests := testutils.AddManifests([]runtime.Object{&manifestConfigMap}, []workapi.Manifest{})
			By(fmt.Sprintf("creating work %s of %s", namespaceType, manifestConfigMapName))
			testutils.CreateWork(ctx, *HubCluster, workName, workNamespace.Name, manifests)

			By(fmt.Sprintf("Applied Condition should be set to True for Work %s", namespaceType))
			work := workapi.Work{}

			Eventually(func() string {
				if err := HubCluster.KubeClient.Get(ctx, namespaceType, &work); err != nil {
					return err.Error()
				}

				want := []metav1.Condition{
					{
						Type:   conditionTypeApplied,
						Status: metav1.ConditionTrue,
						Reason: "appliedWorkComplete",
					},
				}

				return cmp.Diff(want, work.Status.Conditions, cmpOptions...)
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(), "Validate WorkStatus mismatch (-want, +got):")

			By(fmt.Sprintf("Manifest Conditions on Work Objects %s should be applied", namespaceType))
			wantManifestCondition := []workapi.ManifestCondition{
				{
					Conditions: []metav1.Condition{
						{
							Type:   conditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: "appliedManifestUpdated",
						},
					},
					Identifier: workapi.ResourceIdentifier{
						Group:     manifestConfigMap.GroupVersionKind().Group,
						Version:   manifestConfigMap.GroupVersionKind().Version,
						Kind:      manifestConfigMap.GroupVersionKind().Kind,
						Namespace: manifestConfigMap.Namespace,
						Name:      manifestConfigMap.Name,
						Resource:  "configmaps",
					},
				},
			}

			Expect(cmp.Diff(wantManifestCondition, work.Status.ManifestConditions, cmpOptions...)).Should(BeEmpty(),
				"Manifest Condition not matching for work %s (-want, +got):", namespaceType)

			Expect(work.Status.ManifestConditions[0].Conditions[0].Reason == string(workv1alpha1.ManifestCreatedAction)).Should(BeTrue())

			By(fmt.Sprintf("AppliedWorkStatus should contain the meta for the resource %s", manifestConfigMapName))
			appliedWork := workapi.AppliedWork{}
			Expect(MemberCluster.KubeClient.Get(ctx, namespaceType, &appliedWork)).Should(Succeed(),
				"Retrieving AppliedWork %s failed", workName)

			want := workapi.AppliedtWorkStatus{
				AppliedResources: []workapi.AppliedResourceMeta{
					{
						ResourceIdentifier: workapi.ResourceIdentifier{
							Group:     manifestConfigMap.GroupVersionKind().Group,
							Version:   manifestConfigMap.GroupVersionKind().Version,
							Kind:      manifestConfigMap.GroupVersionKind().Kind,
							Namespace: manifestConfigMap.Namespace,
							Name:      manifestConfigMap.Name,
							Resource:  "configmaps",
						},
					},
				},
			}

			Expect(cmp.Diff(want, appliedWork.Status, appliedWorkCmpOptions...)).Should(BeEmpty(),
				"Validate AppliedResourceMeta mismatch (-want, +got):")

			By(fmt.Sprintf("Resource %s should have been created in cluster %s", manifestConfigMapName, MemberCluster.ClusterName))
			gotConfigMap := corev1.ConfigMap{}
			Expect(MemberCluster.KubeClient.Get(ctx, resourceNamespaceType, &gotConfigMap)).Should(Succeed(),
				"Retrieving the resource %s failed", manifestConfigMap.Name)
			//TODO: Fix this to compare the whole structure instead of just the data
			Expect(cmp.Diff(manifestConfigMap.Data, gotConfigMap.Data)).Should(BeEmpty(),
				"ConfigMap %s was not created in the cluster %s, or configMap data mismatch(-want, +got):", manifestConfigMapName, MemberCluster.ClusterName)

			By(fmt.Sprintf("Validating that the resource %s is owned by the work %s", manifestConfigMapName, namespaceType))
			wantOwner := []metav1.OwnerReference{
				{
					APIVersion: workapi.GroupVersion.String(),
					Kind:       workapi.AppliedWorkKind,
					Name:       appliedWork.GetName(),
					UID:        appliedWork.GetUID(),
				},
			}

			Expect(cmp.Diff(wantOwner, gotConfigMap.OwnerReferences, cmpOptions...)).Should(BeEmpty(), "OwnerReference mismatch (-want, +got):")

			By(fmt.Sprintf("Validating that the annotation of resource's spec exists on the resource %s", manifestConfigMapName))
			Expect(gotConfigMap.ObjectMeta.Annotations[specHashAnnotation]).ToNot(BeEmpty(),
				"SpecHash Annotation does not exist for resource %s", gotConfigMap.Name)
		})

		It("Upon successful creation of 2 work resources with same manifest, work manifest is applied, and only 1 resource is created with merged owner references.", func() {
			workNameTwo := testutils.RandomWorkName(5)

			manifestSecretName := "test-secret"

			secret := corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Secret",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      manifestSecretName,
					Namespace: resourceNamespace.Name,
				},
				Data: map[string][]byte{
					"test-secret": []byte("test-data"),
				},
				Type: "Opaque",
			}

			// Creating types.NamespacedName to use in retrieving objects.
			namespaceTypeOne := types.NamespacedName{Name: workName, Namespace: workNamespace.Name}
			namespaceTypeTwo := types.NamespacedName{Name: workNameTwo, Namespace: workNamespace.Name}

			resourceNamespaceType := types.NamespacedName{Name: manifestSecretName, Namespace: resourceNamespace.Name}

			manifests := testutils.AddManifests([]runtime.Object{&secret}, []workapi.Manifest{})

			By(fmt.Sprintf("creating work %s of %s", namespaceTypeOne, manifestSecretName))
			testutils.CreateWork(ctx, *HubCluster, workName, workNamespace.Name, manifests)

			By(fmt.Sprintf("creating work %s of %s", namespaceTypeTwo, manifestSecretName))
			testutils.CreateWork(ctx, *HubCluster, workNameTwo, workNamespace.Name, manifests)

			By(fmt.Sprintf("Applied Condition should be set to True for Work %s and %s", namespaceTypeOne, namespaceTypeTwo))

			want := []metav1.Condition{
				{
					Type:   conditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: "appliedWorkComplete",
				},
			}

			workOne := workapi.Work{}

			Eventually(func() string {
				if err := HubCluster.KubeClient.Get(ctx, namespaceTypeOne, &workOne); err != nil {
					return err.Error()
				}

				return cmp.Diff(want, workOne.Status.Conditions, cmpOptions...)
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(), "Validate WorkStatus mismatch (-want, +got):")

			workTwo := workapi.Work{}

			Eventually(func() string {
				if err := HubCluster.KubeClient.Get(ctx, namespaceTypeTwo, &workTwo); err != nil {
					return err.Error()
				}

				return cmp.Diff(want, workTwo.Status.Conditions, cmpOptions...)
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(), "Validate WorkStatus mismatch (-want, +got):")

			By(fmt.Sprintf("Manifest Conditions on Work Objects %s and %s should be applied", namespaceTypeOne, namespaceTypeTwo))
			wantManifestCondition := []workapi.ManifestCondition{
				{
					Conditions: []metav1.Condition{
						{
							Type:   conditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: "appliedManifestUpdated",
						},
					},
					Identifier: workapi.ResourceIdentifier{
						Group:     secret.GroupVersionKind().Group,
						Version:   secret.GroupVersionKind().Version,
						Kind:      secret.GroupVersionKind().Kind,
						Namespace: secret.Namespace,
						Name:      secret.Name,
						Resource:  "secrets",
					},
				},
			}

			Expect(cmp.Diff(wantManifestCondition, workOne.Status.ManifestConditions, cmpOptions...)).Should(BeEmpty(),
				"Manifest Condition not matching for work %s (-want, +got):", namespaceTypeOne)

			Expect(cmp.Diff(wantManifestCondition, workTwo.Status.ManifestConditions, cmpOptions...)).Should(BeEmpty(),
				"Manifest Condition not matching for work %s (-want, +got):", namespaceTypeTwo)

			// One of them should be a ManifestCreatedAction and one of them should be an ManifestUpdatedAction
			By(fmt.Sprintf("Verify that either works %s and %s condition reason should be updated", namespaceTypeOne, namespaceTypeTwo))
			Expect(workOne.Status.ManifestConditions[0].Conditions[0].Reason == string(workv1alpha1.ManifestCreatedAction) ||
				workTwo.Status.ManifestConditions[0].Conditions[0].Reason == string(workv1alpha1.ManifestCreatedAction)).Should(BeTrue())
			Expect(workOne.Status.ManifestConditions[0].Conditions[0].Reason == string(workv1alpha1.ManifestThreeWayMergePatchAction) ||
				workTwo.Status.ManifestConditions[0].Conditions[0].Reason == string(workv1alpha1.ManifestThreeWayMergePatchAction)).Should(BeTrue())

			By(fmt.Sprintf("AppliedWorkStatus for both works %s and %s should contain the meta for the resource %s", namespaceTypeOne, namespaceTypeTwo, manifestSecretName))
			wantAppliedStatus := workapi.AppliedtWorkStatus{
				AppliedResources: []workapi.AppliedResourceMeta{
					{
						ResourceIdentifier: workapi.ResourceIdentifier{
							Group:     secret.GroupVersionKind().Group,
							Version:   secret.GroupVersionKind().Version,
							Kind:      secret.GroupVersionKind().Kind,
							Namespace: secret.Namespace,
							Name:      secret.Name,
							Resource:  "secrets",
						},
					},
				},
			}

			appliedWorkOne := workapi.AppliedWork{}
			Expect(MemberCluster.KubeClient.Get(ctx, namespaceTypeOne, &appliedWorkOne)).Should(Succeed(),
				"Retrieving AppliedWork %s failed", workName)

			Expect(cmp.Diff(wantAppliedStatus, appliedWorkOne.Status, appliedWorkCmpOptions...)).Should(BeEmpty(),
				"Validate AppliedResourceMeta mismatch (-want, +got):")

			appliedWorkTwo := workapi.AppliedWork{}
			Expect(MemberCluster.KubeClient.Get(ctx, namespaceTypeTwo, &appliedWorkTwo)).Should(Succeed(),
				"Retrieving AppliedWork %s failed", workNameTwo)

			Expect(cmp.Diff(wantAppliedStatus, appliedWorkTwo.Status, appliedWorkCmpOptions...)).Should(BeEmpty(),
				"Validate AppliedResourceMeta mismatch (-want, +got):")

			By(fmt.Sprintf("Resource %s should have been created in cluster %s", manifestSecretName, MemberCluster.ClusterName))
			retrievedSecret := corev1.Secret{}
			err := MemberCluster.KubeClient.Get(ctx, resourceNamespaceType, &retrievedSecret)

			Expect(err).Should(Succeed(), "Secret %s was not created in the cluster %s", manifestSecretName, MemberCluster.ClusterName)

			Expect(cmp.Diff(secret, retrievedSecret, append(cmpOptions, secretCmpOptions...)...)).Should(BeEmpty(), "Secret %s mismatch (-want, +got):")

			By(fmt.Sprintf("Validating that the resource %s is owned by the both works: %s and %s", manifestSecretName, namespaceTypeOne, namespaceTypeTwo))
			wantOwner := []metav1.OwnerReference{
				{
					APIVersion: workapi.GroupVersion.String(),
					Kind:       workapi.AppliedWorkKind,
					Name:       appliedWorkOne.GetName(),
					UID:        appliedWorkOne.GetUID(),
				},
				{
					APIVersion: workapi.GroupVersion.String(),
					Kind:       workapi.AppliedWorkKind,
					Name:       appliedWorkTwo.GetName(),
					UID:        appliedWorkTwo.GetUID(),
				},
			}
			// sort using compare function (sort the array to guarantee the sequence)
			// then the result will be determined
			sortSlicesOption := append(cmpOptions, cmpopts.SortSlices(func(ref1, ref2 metav1.OwnerReference) bool { return ref1.Name < ref2.Name }))
			Expect(cmp.Diff(wantOwner, retrievedSecret.OwnerReferences, sortSlicesOption...)).Should(BeEmpty(), "OwnerReference mismatch (-want, +got):")

			By(fmt.Sprintf("Validating that the annotation of resource's spec exists on the resource %s", manifestSecretName))
			Expect(retrievedSecret.ObjectMeta.Annotations[specHashAnnotation]).ToNot(BeEmpty(),
				"SpecHash Annotation does not exist for resource %s", secret.Name)

			testutils.DeleteWork(ctx, *HubCluster, workTwo)
		})

		It("Upon successful work creation of a CRD resource, manifest is applied, and resources are created", func() {
			// Name of the CRD object from the manifest file
			crdName := "testcrds.multicluster.x-k8s.io"
			crdObjectName := "test-crd-object"

			// Creating CRD manifest from test file
			manifestCRD, crdGVK, crdGVR := testutils.GenerateCRDObjectFromFile(*MemberCluster, TestManifestFiles, "manifests/test-crd.yaml", genericCodec)

			// GVR for the Custom Resource created
			crGVR := schema.GroupVersionResource{
				Group:    "multicluster.x-k8s.io",
				Version:  "v1alpha1",
				Resource: "testcrds",
			}
			customResourceManifestString := "{\"apiVersion\":\"multicluster.x-k8s.io/v1alpha1\",\"kind\":\"TestCRD\",\"metadata\":{\"name\":\"test-crd-object\"}}"

			// Creating types.NamespacedName to use in retrieving objects.
			namespaceType := types.NamespacedName{Name: workName, Namespace: workNamespace.Name}

			// Creating NamespacedName to retrieve CRD object created
			crdNamespaceType := types.NamespacedName{Name: crdName}

			By(fmt.Sprintf("creating work %s of %s", namespaceType, crdGVK.Kind))
			manifests := testutils.AddManifests([]runtime.Object{manifestCRD}, []workapi.Manifest{})
			manifests = testutils.AddByteArrayToManifest([]byte(customResourceManifestString), manifests)
			testutils.CreateWork(ctx, *HubCluster, workName, workNamespace.Name, manifests)

			By(fmt.Sprintf("Applied Condition should be set to True for Work %s", namespaceType))
			work := workapi.Work{}

			Eventually(func() string {
				if err := HubCluster.KubeClient.Get(ctx, namespaceType, &work); err != nil {
					return err.Error()
				}

				want := []metav1.Condition{
					{
						Type:   conditionTypeApplied,
						Status: metav1.ConditionTrue,
						Reason: "appliedWorkComplete",
					},
				}

				return cmp.Diff(want, work.Status.Conditions, cmpOptions...)
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(),
				"Applied Condition mismatch for work %s (-want, +got):", workName)

			By(fmt.Sprintf("Manifest Conditions on Work Objects %s should be applied", namespaceType))
			expectedManifestCondition := []workapi.ManifestCondition{
				{
					Conditions: []metav1.Condition{
						{
							Type:   conditionTypeApplied,
							Status: metav1.ConditionTrue,
							// Will ignore reason for now, there can be 2 different outcomes, Complete and Updated.
							//Reason: "appliedManifestComplete",
						},
					},
					Identifier: workapi.ResourceIdentifier{
						Group:    crdGVK.Group,
						Version:  crdGVK.Version,
						Kind:     crdGVK.Kind,
						Name:     crdName,
						Resource: crdGVR.Resource,
					},
				},
				{
					Conditions: []metav1.Condition{
						{
							Type:   conditionTypeApplied,
							Status: metav1.ConditionTrue,
							//Reason: "appliedManifestUpdated",
						},
					},
					Identifier: workapi.ResourceIdentifier{
						Group:    crGVR.Group,
						Version:  crGVR.Version,
						Kind:     "TestCRD",
						Name:     crdObjectName,
						Resource: "testcrds",
					},
				},
			}

			options := append(cmpOptions, cmpopts.IgnoreFields(metav1.Condition{}, "Reason"))
			Expect(cmp.Diff(expectedManifestCondition, work.Status.ManifestConditions, options...)).Should(BeEmpty(),
				"Manifest Condition not matching for work %s (-want, +got):", namespaceType)

			By(fmt.Sprintf("AppliedWorkStatus should contain the meta for the resource %s", crdGVK.Kind))
			var appliedWork workapi.AppliedWork

			Expect(MemberCluster.KubeClient.Get(ctx, namespaceType, &appliedWork)).Should(Succeed(),
				"Retrieving AppliedWork %s failed", workName)

			wantAppliedWorkStatus := workapi.AppliedtWorkStatus{
				AppliedResources: []workapi.AppliedResourceMeta{
					{
						ResourceIdentifier: workapi.ResourceIdentifier{
							Group:    crdGVK.Group,
							Version:  crdGVK.Version,
							Kind:     crdGVK.Kind,
							Name:     crdName,
							Resource: crdGVR.Resource,
						},
					},
					{
						ResourceIdentifier: workapi.ResourceIdentifier{
							Group:    crGVR.Group,
							Version:  crGVR.Version,
							Kind:     "TestCRD",
							Name:     crdObjectName,
							Resource: "testcrds",
						},
					},
				},
			}

			Expect(cmp.Diff(wantAppliedWorkStatus, appliedWork.Status, appliedWorkCmpOptions...)).Should(BeEmpty(), "Validate AppliedResourceMeta mismatch (-want, +got):")

			By(fmt.Sprintf("CRD %s should have been created in cluster %s", crdName, MemberCluster.ClusterName))

			crd := apiextensionsv1.CustomResourceDefinition{}
			err := MemberCluster.KubeClient.Get(ctx, crdNamespaceType, &crd)
			Expect(err).Should(Succeed(), "Resources %s not created in cluster %s", crdName, MemberCluster.ClusterName)

			wantCRD := apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testcrds.multicluster.x-k8s.io",
				},
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Group: "multicluster.x-k8s.io",
					Names: apiextensionsv1.CustomResourceDefinitionNames{
						Plural:     "testcrds",
						Singular:   "testcrd",
						ShortNames: nil,
						Kind:       "TestCRD",
						ListKind:   "TestCRDList",
						Categories: nil,
					},
					Scope: "Cluster",
				},
			}

			Expect(cmp.Diff(wantCRD, crd, crdCmpOptions...)).Should(BeEmpty(), "Validate CRD object mismatch (-want, got+):")

			By(fmt.Sprintf("CR %s should have been created in cluster %s", crdObjectName, MemberCluster.ClusterName))

			customResource, err := MemberCluster.DynamicClient.Resource(crGVR).Get(ctx, crdObjectName, metav1.GetOptions{})
			Expect(err).Should(Succeed(), "retrieving CR %s failed in cluster %s", crdObjectName, MemberCluster.ClusterName)
			wantCRObject := unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "multicluster.x-k8s.io/v1alpha1",
					"kind":       "TestCRD",
					"metadata": map[string]interface{}{
						"name": "test-crd-object",
					},
				},
			}

			filterMetadataFunc := cmp.FilterPath(isKeyMetadata, cmp.Ignore())
			filterNotNameFunc := cmp.FilterPath(isKeyNotName, cmp.Ignore())

			Expect(cmp.Diff(wantCRObject, *customResource,
				append(cmpOptions, filterMetadataFunc)...)).Should(BeEmpty(), "Validate CR Object Metadata mismatch (-want, +got):")

			Expect(cmp.Diff(wantCRObject.Object["metadata"], customResource.Object["metadata"],
				append(cmpOptions, filterNotNameFunc)...)).Should(BeEmpty(), "Validate CR Object Metadata mismatch (-want, +got):")

			By(fmt.Sprintf("Validating that the resource %s is owned by the work %s", crdName, namespaceType))
			wantOwner := []metav1.OwnerReference{
				{
					APIVersion: workapi.GroupVersion.String(),
					Kind:       workapi.AppliedWorkKind,
					Name:       appliedWork.GetName(),
					UID:        appliedWork.GetUID(),
				},
			}

			Expect(cmp.Diff(wantOwner, crd.OwnerReferences, cmpOptions...)).Should(BeEmpty(),
				"OwnerReference for resource %s mismatch (-want, +got):", crd.Name)
			Expect(cmp.Diff(wantOwner, customResource.GetOwnerReferences(), cmpOptions...)).Should(BeEmpty(),
				"OwnerReference for CR %s mismatch (-want, +got):", customResource.GetName())

			By(fmt.Sprintf("Validating that the annotation of resource's spec exists on the resource %s", crd.Name))

			Expect(crd.GetAnnotations()[specHashAnnotation]).ToNot(BeEmpty(),
				"There is no spec annotation on the resource %s", crd.Name)
			Expect(customResource.GetAnnotations()[specHashAnnotation]).ToNot(BeEmpty(),
				"There is no spec annotation on the custom resource %s", customResource.GetName())
		})

		It("Manifests with dependencies within different work objects should successfully apply", func() {
			workNameForServiceAccount := testutils.RandomWorkName(6)

			testNamespace := corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
				},
			}

			testServiceAccount := corev1.ServiceAccount{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ServiceAccount",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-account",
					Namespace: testNamespace.Name,
				},
			}

			manifestNamespace := testutils.AddManifests([]runtime.Object{&testNamespace}, []workapi.Manifest{})
			manifestServiceAccount := testutils.AddManifests([]runtime.Object{&testServiceAccount}, []workapi.Manifest{})

			workForNamespace := testutils.CreateWork(ctx, *HubCluster, workName, workNamespace.Name, manifestNamespace)
			workForServiceAccount := testutils.CreateWork(ctx, *HubCluster, workNameForServiceAccount, workNamespace.Name, manifestServiceAccount)

			By(fmt.Sprintf("Applied Condition should be set to True for Work %s and %s", workName, workNameForServiceAccount))

			wantAppliedCondition := []metav1.Condition{
				{
					Type:   conditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: "appliedWorkComplete",
				},
			}

			receivedWorkForNamespace := workapi.Work{}
			receivedWorkForServiceAccount := workapi.Work{}

			namespaceTypeForNamespaceWork := types.NamespacedName{Name: workName, Namespace: workNamespace.Name}

			Eventually(func() string {
				if err := HubCluster.KubeClient.Get(ctx, namespaceTypeForNamespaceWork, &receivedWorkForNamespace); err != nil {
					return err.Error()
				}

				return cmp.Diff(wantAppliedCondition, receivedWorkForNamespace.Status.Conditions, cmpOptions...)
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(),
				"Validate WorkStatus for work %s mismatch (-want, +got):", workForNamespace)

			namespaceTypeForServiceAccountWork := types.NamespacedName{Name: workNameForServiceAccount, Namespace: workNamespace.Name}

			Eventually(func() string {
				if err := HubCluster.KubeClient.Get(ctx, namespaceTypeForServiceAccountWork, &receivedWorkForServiceAccount); err != nil {
					return err.Error()
				}

				return cmp.Diff(wantAppliedCondition, receivedWorkForServiceAccount.Status.Conditions, cmpOptions...)
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(),
				"Validate WorkStatus for work %s mismatch (-want, +got):", workForServiceAccount)

			By(fmt.Sprintf("Manifest Conditions on Work Objects %s and %s should be applied", namespaceTypeForNamespaceWork, namespaceTypeForServiceAccountWork))
			wantNamespaceManifestCondition := []workapi.ManifestCondition{
				{
					Conditions: []metav1.Condition{
						{
							Type:   conditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: "appliedManifestUpdated",
						},
					},
					Identifier: workapi.ResourceIdentifier{
						Group:     testNamespace.GroupVersionKind().Group,
						Version:   testNamespace.GroupVersionKind().Version,
						Kind:      testNamespace.GroupVersionKind().Kind,
						Namespace: testNamespace.Namespace,
						Name:      testNamespace.Name,
						Resource:  "namespaces",
					},
				},
			}
			wantServiceAccountManifestCondition := []workapi.ManifestCondition{
				{
					Conditions: []metav1.Condition{
						{
							Type:   conditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: "appliedManifestUpdated",
						},
					},
					Identifier: workapi.ResourceIdentifier{
						Group:     testServiceAccount.GroupVersionKind().Group,
						Version:   testServiceAccount.GroupVersionKind().Version,
						Kind:      testServiceAccount.GroupVersionKind().Kind,
						Namespace: testServiceAccount.Namespace,
						Name:      testServiceAccount.Name,
						Resource:  "serviceaccounts",
					},
				},
			}

			Expect(cmp.Diff(wantNamespaceManifestCondition, receivedWorkForNamespace.Status.ManifestConditions, cmpOptions...)).Should(BeEmpty(),
				"Manifest Condition not matching for work %s (-want, +got):", namespaceTypeForNamespaceWork)

			Expect(cmp.Diff(wantServiceAccountManifestCondition, receivedWorkForServiceAccount.Status.ManifestConditions, cmpOptions...)).Should(BeEmpty(),
				"Manifest Condition not matching for work %s (-want, +got):", namespaceTypeForServiceAccountWork)

			By(fmt.Sprintf("AppliedWorkStatus should contain the meta for the resource %s and %s", testNamespace.Name, testServiceAccount.Name))
			appliedWorkForNamespace := workapi.AppliedWork{}
			Expect(MemberCluster.KubeClient.Get(ctx, namespaceTypeForNamespaceWork, &appliedWorkForNamespace)).Should(Succeed(),
				"Retrieving AppliedWork %s failed", workName)

			wantAppliedWorkConditionNamespace := workapi.AppliedtWorkStatus{
				AppliedResources: []workapi.AppliedResourceMeta{
					{
						ResourceIdentifier: workapi.ResourceIdentifier{
							Group:     testNamespace.GroupVersionKind().Group,
							Version:   testNamespace.GroupVersionKind().Version,
							Kind:      testNamespace.GroupVersionKind().Kind,
							Namespace: testNamespace.Namespace,
							Name:      testNamespace.Name,
							Resource:  "namespaces",
						},
					},
				},
			}

			Expect(cmp.Diff(wantAppliedWorkConditionNamespace, appliedWorkForNamespace.Status, appliedWorkCmpOptions...)).Should(BeEmpty(),
				"Validate AppliedResourceMeta mismatch for appliedWork %s (-want, +got):", appliedWorkForNamespace.Name)

			appliedWorkForServiceAccount := workapi.AppliedWork{}
			Expect(MemberCluster.KubeClient.Get(ctx, namespaceTypeForServiceAccountWork, &appliedWorkForServiceAccount)).Should(Succeed(),
				"Retrieving AppliedWork %s failed", workNameForServiceAccount)

			wantAppliedConditionServiceAccount := workapi.AppliedtWorkStatus{
				AppliedResources: []workapi.AppliedResourceMeta{
					{
						ResourceIdentifier: workapi.ResourceIdentifier{
							Group:     testServiceAccount.GroupVersionKind().Group,
							Version:   testServiceAccount.GroupVersionKind().Version,
							Kind:      testServiceAccount.GroupVersionKind().Kind,
							Namespace: testServiceAccount.Namespace,
							Name:      testServiceAccount.Name,
							Resource:  "serviceaccounts",
						},
					},
				},
			}

			Expect(cmp.Diff(wantAppliedConditionServiceAccount, appliedWorkForServiceAccount.Status, appliedWorkCmpOptions...)).Should(BeEmpty(),
				"Validate AppliedResourceMeta mismatch for appliedWork %s (-want, +got):", appliedWorkForServiceAccount.Name)

			By(fmt.Sprintf("The resources %s and %s should both be created in the member cluster.", testNamespace.Name, testServiceAccount.Name))
			retrievedNamespace := corev1.Namespace{}

			// Ns abbreviated to avoid duplicate wording
			nsNamespaceType := types.NamespacedName{
				Name: testNamespace.Name,
			}

			Expect(MemberCluster.KubeClient.Get(ctx, nsNamespaceType, &retrievedNamespace)).Should(Succeed(),
				"Failed in retrieving resource %s", testNamespace)
			wantNamespace := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
				},
				Status: corev1.NamespaceStatus{
					Phase: corev1.NamespaceActive,
				},
			}

			namespaceCmpOptions := append(cmpOptions,
				cmpopts.IgnoreFields(corev1.Namespace{}, "Spec"))

			Expect(cmp.Diff(wantNamespace, retrievedNamespace, namespaceCmpOptions...)).Should(BeEmpty(),
				"Validate Namespace %s mismatch (-want, +got):", wantNamespace.Name)

			serviceAccountNamespaceType := types.NamespacedName{
				Name:      testServiceAccount.Name,
				Namespace: testNamespace.Name,
			}

			retrievedServiceAccount := corev1.ServiceAccount{}
			Expect(MemberCluster.KubeClient.Get(ctx, serviceAccountNamespaceType, &retrievedServiceAccount)).Should(Succeed(),
				"Failed in retrieving resource %s", testServiceAccount)

			By(fmt.Sprintf("Validating that the resource %s and %s is owned by the respective work", testNamespace.Name, testServiceAccount.Name))
			wantOwnerForNamespace := []metav1.OwnerReference{
				{
					APIVersion: workapi.GroupVersion.String(),
					Kind:       workapi.AppliedWorkKind,
					Name:       appliedWorkForNamespace.GetName(),
					UID:        appliedWorkForNamespace.GetUID(),
				},
			}

			wantOwnerForServiceAccount := []metav1.OwnerReference{
				{
					APIVersion: workapi.GroupVersion.String(),
					Kind:       workapi.AppliedWorkKind,
					Name:       appliedWorkForServiceAccount.GetName(),
					UID:        appliedWorkForServiceAccount.GetUID(),
				},
			}

			Expect(cmp.Diff(wantOwnerForNamespace, retrievedNamespace.OwnerReferences, cmpOptions...)).Should(BeEmpty(),
				"OwnerReference mismatch for resource %s (-want, +got):", testNamespace.Name)
			Expect(cmp.Diff(wantOwnerForServiceAccount, retrievedServiceAccount.OwnerReferences, cmpOptions...)).Should(BeEmpty(),
				"OwnerReference mismatch for resource %s (-want, +got):", testServiceAccount.Name)

			testutils.DeleteWork(ctx, *HubCluster, workForServiceAccount)
		})

	})

	Context("Updating Work", func() {
		configMap := corev1.ConfigMap{}
		work := workapi.Work{}
		namespaceType := types.NamespacedName{}

		BeforeEach(func() {
			workName := testutils.RandomWorkName(5)
			manifestConfigMapName := "work-update-configmap"
			configMap = corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      manifestConfigMapName,
					Namespace: resourceNamespace.Name,
				},
				Data: map[string]string{
					"before-update-key": "before-update-data",
				},
			}

			// Creating types.NamespacedName to use in retrieving objects.
			namespaceType = types.NamespacedName{Name: workName, Namespace: workNamespace.Name}

			manifests := testutils.AddManifests([]runtime.Object{&configMap}, []workapi.Manifest{})
			By(fmt.Sprintf("creating work %s of %s", namespaceType, manifestConfigMapName))
			work = testutils.CreateWork(ctx, *HubCluster, workName, workNamespace.Name, manifests)

			Eventually(func() string {
				if err := HubCluster.KubeClient.Get(ctx, namespaceType, &work); err != nil {
					return err.Error()
				}

				want := []metav1.Condition{
					{
						Type:   conditionTypeApplied,
						Status: metav1.ConditionTrue,
						Reason: "appliedWorkComplete",
					},
				}

				return cmp.Diff(want, work.Status.Conditions, cmpOptions...)
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(), "Validate WorkStatus mismatch (-want, +got):")

		})

		AfterEach(func() {
			testutils.DeleteWork(ctx, *HubCluster, work)
		})

		It("Updating Work object on the Hub Cluster should update the resource on the member cluster.", func() {
			updatedConfigMap := configMap.DeepCopy()
			updatedConfigMap.Data = map[string]string{
				"updated-key": "updated-data",
			}
			testutils.UpdateWork(ctx, HubCluster, &work, []runtime.Object{updatedConfigMap})

			By(fmt.Sprintf("The resource %s should be updated in the member cluster %s", updatedConfigMap.Name, memberClusterName))
			configMapNamespaceType := types.NamespacedName{Name: updatedConfigMap.Name, Namespace: resourceNamespace.Name}
			gotConfigMap := corev1.ConfigMap{}

			wantConfigMap := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      updatedConfigMap.Name,
					Namespace: resourceNamespace.Name,
				},
				Data: map[string]string{
					"updated-key": "updated-data",
				},
			}

			Eventually(func() string {
				if err := MemberCluster.KubeClient.Get(ctx, configMapNamespaceType, &gotConfigMap); err != nil {
					return err.Error()
				}
				return cmp.Diff(wantConfigMap, gotConfigMap, cmpOptions...)
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(), "Resource %s mismatch (-want, +got):", updatedConfigMap.Name)
		})

		It("Deleting a manifest from the Work object should delete the corresponding resource in the member cluster", func() {
			configMapNamespaceType := types.NamespacedName{Name: configMap.Name, Namespace: resourceNamespace.Name}
			testutils.UpdateWork(ctx, HubCluster, &work, []runtime.Object{})

			By(fmt.Sprintf("The resource %s should be deleted in the member cluster %s", configMap.Name, memberClusterName))
			Eventually(func() error {
				retrievedConfigMap := corev1.ConfigMap{}
				return MemberCluster.KubeClient.Get(ctx, configMapNamespaceType, &retrievedConfigMap)
			}, testutils.PollTimeout, testutils.PollInterval).Should(&utils.NotFoundMatcher{}, "Resource %s should have been deleted", configMap.Name)

			By(fmt.Sprintf("Condition for resource %s should be removed from AppliedWork", configMap.Name))
			appliedWork := workapi.AppliedWork{}
			err := MemberCluster.KubeClient.Get(ctx, namespaceType, &appliedWork)
			Expect(err).Should(Succeed(), "AppliedWork for Work %s should still exist", namespaceType)

			wantAppliedWorkStatus := workapi.AppliedtWorkStatus{AppliedResources: nil}
			Expect(cmp.Diff(wantAppliedWorkStatus, appliedWork.Status)).Should(BeEmpty(),
				"Status should be empty for AppliedWork %s (-want, +got):", appliedWork.Name)

			By(fmt.Sprintf("Applied Condition for Work %s should still be true", namespaceType))
			updatedWork := workapi.Work{}
			err = HubCluster.KubeClient.Get(ctx, namespaceType, &updatedWork)
			Expect(err).Should(Succeed(), "Retrieving Work Object should succeed")

			// Since "all" the manifests are applied, the work status is counted as "Applied Correctly".
			// There are no more manifests within this work.
			wantWorkStatus := workapi.WorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:   conditionTypeApplied,
						Status: metav1.ConditionTrue,
					},
				},
			}
			Expect(cmp.Diff(wantWorkStatus, updatedWork.Status, cmpOptions...)).Should(BeEmpty(),
				"Work Condition for Work Object %s should still be applied / mismatch (-want, +got): ")
		})
	})

	Context("Work-api Deletion tests", func() {
		It("Deleting a Work Object on Hub Cluster should also delete the corresponding resource on Member Cluster.", func() {
			// creating work resource to be deleted
			workName := testutils.RandomWorkName(5)
			manifestConfigMapName := "work-update-configmap"
			configMapBeforeDelete := corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      manifestConfigMapName,
					Namespace: resourceNamespace.Name,
				},
				Data: map[string]string{
					"before-delete-key": "before-delete-data",
				},
			}

			// Creating types.NamespacedName to use in retrieving objects.
			namespaceType := types.NamespacedName{Name: workName, Namespace: workNamespace.Name}
			manifests := testutils.AddManifests([]runtime.Object{&configMapBeforeDelete}, []workapi.Manifest{})

			By(fmt.Sprintf("creating work %s of %s", namespaceType, manifestConfigMapName))
			workBeforeDelete := testutils.CreateWork(ctx, *HubCluster, workName, workNamespace.Name, manifests)

			Eventually(func() string {
				if err := HubCluster.KubeClient.Get(ctx, namespaceType, &workBeforeDelete); err != nil {
					return err.Error()
				}

				want := []metav1.Condition{
					{
						Type:   conditionTypeApplied,
						Status: metav1.ConditionTrue,
						Reason: "appliedWorkComplete",
					},
				}

				return cmp.Diff(want, workBeforeDelete.Status.Conditions, cmpOptions...)
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(), "Validate WorkStatus mismatch (-want, +got):")

			testutils.DeleteWork(ctx, *HubCluster, workBeforeDelete)

			By("Deleting the Work Object should also delete the AppliedWork in the member cluster")
			appliedWork := workapi.AppliedWork{}
			Expect(MemberCluster.KubeClient.Get(ctx, namespaceType, &appliedWork)).Should(&utils.NotFoundMatcher{},
				"AppliedWork %s was either not deleted or encountered an error in cluster %s", workName, MemberCluster.ClusterName)

			By("Deleting the Work Object should also delete the resources in the member cluster")
			configMapDeleted := corev1.ConfigMap{}
			resourceNamespaceType := types.NamespacedName{Name: configMapBeforeDelete.Name, Namespace: resourceNamespace.Name}
			Eventually(func() error {
				return MemberCluster.KubeClient.Get(ctx, resourceNamespaceType, &configMapDeleted)
			}, testutils.PollTimeout, testutils.PollInterval).Should(&utils.NotFoundMatcher{}, "resource %s was either not deleted or encountered an error in cluster %s", configMapBeforeDelete.Name, MemberCluster.ClusterName)
		})
	})
})

func isKeyMetadata(p cmp.Path) bool {
	step, ok := p[len(p)-1].(cmp.MapIndex)
	return ok && step.Key().String() == "metadata"
}

func isKeyNotName(p cmp.Path) bool {
	step, ok := p[len(p)-1].(cmp.MapIndex)
	return ok && step.Key().String() != "name"
}
