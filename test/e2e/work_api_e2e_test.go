package e2e

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/pkg/utils"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

// TODO: enable this when join/leave logic is connected to work-api, join the Hub and Member for this test.
var _ = XDescribe("Work API Controller test", func() {

	const (
		conditionTypeApplied = "Applied"
		specHashAnnotation   = "fleet.azure.com/spec-hash"
	)

	var (
		ctx context.Context

		// Includes all works applied to the hub cluster. Used for garbage collection.
		works []workapi.Work

		// Comparison Options
		cmpOptions = []cmp.Option{
			cmpopts.IgnoreFields(workapi.AppliedResourceMeta{}, "UID"),
			cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime", "ObservedGeneration"),
			cmpopts.IgnoreFields(metav1.OwnerReference{}, "BlockOwnerDeletion"),
			cmpopts.IgnoreFields(workapi.ResourceIdentifier{}, "Ordinal"),
		}

		resourceNamespace *corev1.Namespace
	)

	BeforeEach(func() {
		ctx = context.Background()

		// This namespace in MemberCluster will store specified test resources created from the Work-api.
		resourceNamespaceName := "resource-namespace" + utils.RandStr()
		resourceNamespace = testutils.NewNamespace(resourceNamespaceName)
		testutils.CreateNamespace(*MemberCluster, resourceNamespace)

		//Empties the works since they were garbage collected earlier.
		works = []workapi.Work{}
	})

	AfterEach(func() {
		testutils.DeleteWork(ctx, *HubCluster, works)
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
		expectedManifestCondition := []workapi.ManifestCondition{
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

		Expect(cmp.Diff(expectedManifestCondition, work.Status.ManifestConditions, cmpOptions...)).Should(BeEmpty(),
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

		Expect(cmp.Diff(want, appliedWork.Status, cmpOptions...)).Should(BeEmpty(),
			"Validate AppliedResourceMeta mismatch (-want, +got):")

		By(fmt.Sprintf("Resource %s should have been created in cluster %s", manifestConfigMapName, MemberCluster.ClusterName))
		cm, err := MemberCluster.KubeClientSet.CoreV1().ConfigMaps(manifestConfigMap.Namespace).Get(ctx, manifestConfigMapName, metav1.GetOptions{})
		Expect(err).Should(Succeed())
		Expect(cmp.Diff(manifestConfigMap.Data, cm.Data)).Should(BeEmpty(),
			"ConfigMap %s was not created in the cluster %s, or configMap data mismatch(-want, +got):", manifestConfigMapName, MemberCluster.ClusterName)

		By(fmt.Sprintf("Validating that the resource %s is owned by the work %s", manifestConfigMapName, namespaceType))
		configMap, err := MemberCluster.KubeClientSet.CoreV1().ConfigMaps(manifestConfigMap.Namespace).Get(ctx, manifestConfigMapName, metav1.GetOptions{})
		Expect(err).Should(Succeed(), "Retrieving resource %s failed", manifestConfigMap.Name)
		wantOwner := []metav1.OwnerReference{
			{
				APIVersion: workapi.GroupVersion.String(),
				Kind:       workapi.AppliedWorkKind,
				Name:       appliedWork.GetName(),
				UID:        appliedWork.GetUID(),
			},
		}

		Expect(cmp.Diff(wantOwner, configMap.OwnerReferences, cmpOptions...)).Should(BeEmpty(), "OwnerReference mismatch (-want, +got):")

		By(fmt.Sprintf("Validating that the annotation of resource's spec exists on the resource %s", manifestConfigMapName))
		Expect(configMap.ObjectMeta.Annotations[specHashAnnotation]).ToNot(BeEmpty(),
			"SpecHash Annotation does not exist for resource %s", configMap.Name)
	})
})
