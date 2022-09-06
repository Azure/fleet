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
	"k8s.io/utils/pointer"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	testutils "go.goms.io/fleet/test/e2e/utils"
)

// TODO: when join/leave logic is connected to work-api, join the Hub and Member for this test.
var _ = Describe("Work API Controller test", func() {

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
			cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration", "Message"),
			cmpopts.IgnoreFields(metav1.OwnerReference{}, "BlockOwnerDeletion"),
		}
	)

	BeforeEach(func() {
		ctx = context.Background()

		//Empties the works since they were garbage collected earlier.
		works = []workapi.Work{}
	})

	AfterEach(func() {
		testutils.DeleteWork(ctx, *HubCluster, works)
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
				Namespace: workResourceNamespace.Name,
			},
			Data: map[string]string{
				"test-key": "test-data",
			},
		}

		manifests := testutils.AddManifests([]runtime.Object{&manifestConfigMap}, []workapi.Manifest{})
		By(fmt.Sprintf("creating work %s/%s of %s", workName, workNamespace.Name, manifestConfigMapName))
		testutils.CreateWork(ctx, *HubCluster, workName, workNamespace.Name, manifests)

		testutils.WaitWork(ctx, *HubCluster, workName, memberNamespace.Name)

		By(fmt.Sprintf("Applied Condition should be set to True for Work %s/%s", workName, workNamespace.Name))
		work := workapi.Work{}
		Eventually(func() string {
			if err := HubCluster.KubeClient.Get(ctx,
				types.NamespacedName{Name: workName, Namespace: workNamespace.Name}, &work); err != nil {
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

		By(fmt.Sprintf("Work %s should contain every manifest conditions for corresponding manifests with correct condition", workName))
		Expect(len(work.Status.ManifestConditions)).To(Equal(1),
			"Invalid manifest conditions for work %s", workName)

		expectedManifestCondition := []workapi.ManifestCondition{
			{
				Conditions: []metav1.Condition{
					{
						Type:   conditionTypeApplied,
						Status: metav1.ConditionTrue,
						Reason: "appliedManifestComplete",
					},
				},
				Identifier: workapi.ResourceIdentifier{
					Ordinal:   0,
					Group:     manifestConfigMap.GroupVersionKind().Group,
					Version:   manifestConfigMap.GroupVersionKind().Version,
					Kind:      manifestConfigMap.GroupVersionKind().Kind,
					Namespace: manifestConfigMap.Namespace,
					Name:      manifestConfigMap.Name,
					Resource:  "configmaps",
				},
			},
		}
		//Expecting the reason of the condition seperately, since it could be either Complete or Updated.
		Expect(work.Status.ManifestConditions[0].Conditions[0].Reason).Should(
			SatisfyAny(Equal("appliedManifestComplete"), Equal("appliedManifestUpdated")))
		// Will leave out the reason for this check, since the manifest condition's reason was checked above.
		Expect(cmp.Diff(expectedManifestCondition, work.Status.ManifestConditions,
			append(cmpOptions, cmpopts.IgnoreFields(metav1.Condition{}, "Reason"))...)).Should(BeEmpty(),
			"Manifest Condition not matching for work %s (-want, +got):", workName)

		By(fmt.Sprintf("AppliedWorkStatus should contain the meta for the resource %s", manifestConfigMapName))
		appliedWork := workapi.AppliedWork{}
		Expect(MemberCluster.KubeClient.Get(ctx,
			types.NamespacedName{Name: workName, Namespace: workNamespace.Name}, &appliedWork)).Should(Succeed())

		want := workapi.AppliedtWorkStatus{
			AppliedResources: []workapi.AppliedResourceMeta{
				{
					ResourceIdentifier: workapi.ResourceIdentifier{
						Ordinal:   0,
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
		Eventually(func() string {
			cm, err := testutils.GetConfigMap(ctx, *MemberCluster, manifestConfigMap.Name, manifestConfigMap.Namespace)
			if err != nil {
				return err.Error()
			}
			return cmp.Diff(manifestConfigMap.Data, cm.Data)
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(),
			"ConfigMap %s was not created in the cluster %s, or configMap data mismatch(-want, +got):", manifestConfigMapName, MemberCluster.ClusterName)

		By(fmt.Sprintf("Validating that the resource %s is owned by the work %s", manifestConfigMapName, workName))
		configMap, err := testutils.GetConfigMap(ctx, *MemberCluster, manifestConfigMap.Name, manifestConfigMap.Namespace)
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
		// Owner Reference is created when manifests are being applied.
		ownerRef := []metav1.OwnerReference{
			{
				APIVersion:         workapi.GroupVersion.String(),
				Kind:               workapi.AppliedWorkKind,
				Name:               appliedWork.GetName(),
				UID:                appliedWork.GetUID(),
				BlockOwnerDeletion: pointer.Bool(false),
			},
		}
		validateConfigMap := manifestConfigMap.DeepCopy()
		validateConfigMap.SetOwnerReferences(ownerRef)

		//Generating SpecHash for work object to compare with Annotation in the resource.
		newManifests := testutils.AddManifests([]runtime.Object{validateConfigMap}, []workapi.Manifest{})
		specHashes := testutils.GenerateSpecHash(newManifests)

		Expect(cmp.Diff(specHashes[0], configMap.ObjectMeta.Annotations[specHashAnnotation])).Should(BeEmpty(),
			"Validating SpecHash Annotation failed for resource %s in work %s(-want, +got):", configMap.Name, workName)
	})
})
