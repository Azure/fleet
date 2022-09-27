package e2e

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

	"go.goms.io/fleet/pkg/utils"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

// TODO: enable this when join/leave logic is connected to work-api, join the Hub and Member for this test.
var _ = Describe("Work API Controller test", func() {

	const (
		conditionTypeApplied = "Applied"
		specHashAnnotation   = "fleet.azure.com/spec-hash"
	)

	var (
		ctx context.Context

		// Comparison Options
		cmpOptions = []cmp.Option{
			cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime", "ObservedGeneration"),
			cmpopts.IgnoreFields(metav1.OwnerReference{}, "BlockOwnerDeletion"),
			cmpopts.IgnoreFields(workapi.ResourceIdentifier{}, "Ordinal"),
			cmpopts.IgnoreFields(metav1.ObjectMeta{},
				"UID",
				"ResourceVersion",
				"Generation",
				"CreationTimestamp",
				"Annotations",
				"OwnerReferences",
				"ManagedFields"),
		}

		appliedWorkCmpOptions = append(cmpOptions, cmpopts.IgnoreFields(workapi.AppliedResourceMeta{}, "UID"))

		crdCmpOptions = append(cmpOptions,
			cmpopts.IgnoreFields(apiextensionsv1.CustomResourceDefinition{}, "Status"),
			cmpopts.IgnoreFields(apiextensionsv1.CustomResourceDefinitionSpec{}, "Versions", "Conversion"))

		secretCmpOptions = append(cmpOptions,
			cmpopts.IgnoreFields(corev1.Secret{}, "TypeMeta"),
		)

		resourceNamespace *corev1.Namespace
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
		testutils.CreateNamespace(*MemberCluster, resourceNamespace)
	})

	AfterEach(func() {
		testutils.DeleteNamespace(*MemberCluster, resourceNamespace)
	})

	It("Upon successful work creation of a single resource, work manifest is applied and resource is created", func() {
		workName := testutils.RandomWorkName(5)

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

		By(fmt.Sprintf("Manifest Condiitons on Work Objects %s should be applied", namespaceType))
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
		retrievedConfigMap := corev1.ConfigMap{}
		Expect(MemberCluster.KubeClient.Get(ctx, resourceNamespaceType, &retrievedConfigMap)).Should(Succeed(),
			"Retrieving the resource %s failed", manifestConfigMap.Name)
		//TODO: Fix this to compare the whole structure instead of just the data
		Expect(cmp.Diff(manifestConfigMap.Data, retrievedConfigMap.Data)).Should(BeEmpty(),
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

		Expect(cmp.Diff(wantOwner, retrievedConfigMap.OwnerReferences, cmpOptions...)).Should(BeEmpty(), "OwnerReference mismatch (-want, +got):")

		By(fmt.Sprintf("Validating that the annotation of resource's spec exists on the resource %s", manifestConfigMapName))
		Expect(retrievedConfigMap.ObjectMeta.Annotations[specHashAnnotation]).ToNot(BeEmpty(),
			"SpecHash Annotation does not exist for resource %s", retrievedConfigMap.Name)
	})

	It("Upon successful creation of 2 work resources with same manifest, work manifest is applied, and only 1 resource is created with merged owner references.", func() {
		workNameOne := testutils.RandomWorkName(5)
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
		namespaceTypeOne := types.NamespacedName{Name: workNameOne, Namespace: workNamespace.Name}
		namespaceTypeTwo := types.NamespacedName{Name: workNameTwo, Namespace: workNamespace.Name}

		resourceNamespaceType := types.NamespacedName{Name: manifestSecretName, Namespace: resourceNamespace.Name}

		manifests := testutils.AddManifests([]runtime.Object{&secret}, []workapi.Manifest{})

		By(fmt.Sprintf("creating work %s of %s", namespaceTypeOne, manifestSecretName))
		testutils.CreateWork(ctx, *HubCluster, workNameOne, workNamespace.Name, manifests)

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

		By(fmt.Sprintf("Manifest Condiitons on Work Objects %s and %s should be applied", namespaceTypeOne, namespaceTypeTwo))
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
			"Retrieving AppliedWork %s failed", workNameOne)

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
	})
	It("Upon successful work creation of a CRD resource, manifest is applied, and resources are created", func() {
		workName := testutils.RandomWorkName(5)

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

		By(fmt.Sprintf("Manifest Condiitons on Work Objects %s should be applied", namespaceType))
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

		want := workapi.AppliedtWorkStatus{
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

		Expect(cmp.Diff(want, appliedWork.Status, appliedWorkCmpOptions...)).Should(BeEmpty(), "Validate AppliedResourceMeta mismatch (-want, +got):")

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

		Expect(cmp.Diff(wantCRD, crd, crdCmpOptions...)).Should(BeEmpty(), "Valdate CRD object mismatch (-want, got+):")

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

		filterMetadataFunc := cmp.FilterPath(IsKeyMetadata, cmp.Ignore())
		filterNotNameFunc := cmp.FilterPath(IsKeyNotName, cmp.Ignore())

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

	Context("Work-api Deletion tests", func() {
		It("Deleting a Work Object on Hub Cluster should also delete the corresponding resource on Member Cluster.", func() {

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
			resourceNamespaceType := types.NamespacedName{Name: configMapBeforeDelete.Name, Namespace: resourceNamespace.Name}
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
			Eventually(func() error {
				appliedWork := workapi.AppliedWork{}
				return MemberCluster.KubeClient.Get(ctx, namespaceType, &appliedWork)
			}, testutils.PollTimeout, testutils.PollInterval).ShouldNot(Succeed(),
				"AppliedWork was not deleted from the cluster %s", workName, MemberCluster.ClusterName)

			By("Deleting the Work Object should also delete the resources in the member cluster")
			configMapDeleted := corev1.ConfigMap{}
			Expect(MemberCluster.KubeClient.Get(ctx, resourceNamespaceType, &configMapDeleted)).ShouldNot(Succeed(),
				"resource %s was not deleted from the cluster %s", configMapBeforeDelete.Name, MemberCluster.ClusterName)

			By(fmt.Sprintf("The Work Object %s was deleted from the Hub Cluster %s", workName, HubCluster.ClusterName))
			deletedWork := workapi.Work{}
			Expect(HubCluster.KubeClient.Get(ctx, namespaceType, &deletedWork)).ShouldNot(Succeed(),
				"The Work resource %s was not deleted on the hub cluster %s", workName, HubCluster.ClusterName)
		})
	})
})

func IsKeyMetadata(p cmp.Path) bool {
	step, ok := p[len(p)-1].(cmp.MapIndex)
	return ok && step.Key().String() == "metadata"
}

func IsKeyNotName(p cmp.Path) bool {
	step, ok := p[len(p)-1].(cmp.MapIndex)
	return ok && step.Key().String() != "name"
}
