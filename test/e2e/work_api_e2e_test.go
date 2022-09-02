package e2e

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	testutils "go.goms.io/fleet/test/e2e/utils"
)

// TODO: when join/leave logic is connected to work-api, join the Hub and Member for this test.
var _ = Describe("Work API Controller test", func() {

	const (
		conditionTypeApplied = "Applied"
	)

	var (
		ctx context.Context

		// Includes all works applied to the hub cluster. Used for garbage collection.
		works []workapi.Work

		// Includes all manifests to be within a Work object.
		manifests []workapi.Manifest

		// Comparison Options
		cmpOptions = []cmp.Option{
			cmpopts.IgnoreFields(workapi.AppliedResourceMeta{}, "UID"),
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
		workName := testutils.GetWorkName(5)
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

		manifests = testutils.AddManifests([]runtime.Object{&manifestConfigMap}, manifests)
		By(fmt.Sprintf("creating work %s/%s of %s", workName, workNamespace.Name, manifestConfigMapName))
		testutils.CreateWork(ctx, *HubCluster, workName, workNamespace.Name, manifests)

		testutils.WaitWork(ctx, *HubCluster, workName, memberNamespace.Name)

		By(fmt.Sprintf("Waiting for AppliedWork %s to be created", workName))
		Eventually(func() error {
			return MemberCluster.KubeClient.Get(ctx,
				types.NamespacedName{Name: workName}, &workapi.AppliedWork{})
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Failed to create AppliedWork %s", workName)

		By(fmt.Sprintf("Applied Condition should be set to True for Work %s/%s", workName, workNamespace.Name))
		Eventually(func() bool {
			work := workapi.Work{}
			if err := HubCluster.KubeClient.Get(ctx,
				types.NamespacedName{Name: workName, Namespace: workNamespace.Name}, &work); err != nil {
				return false
			}

			return meta.IsStatusConditionTrue(work.Status.Conditions, conditionTypeApplied)
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())

		By(fmt.Sprintf("AppliedWorkStatus should contain the meta for the resource %s", manifestConfigMapName))
		Eventually(func() string {
			appliedWork := workapi.AppliedWork{}
			if err := MemberCluster.KubeClient.Get(ctx,
				types.NamespacedName{Name: workName, Namespace: workNamespace.Name}, &appliedWork); err != nil {
				return err.Error()
			}

			if len(appliedWork.Status.AppliedResources) == 0 {
				return fmt.Sprintf("Applied Work Meta not created for resource %s", manifestConfigMapName)
			}
			want := workapi.AppliedResourceMeta{
				ResourceIdentifier: workapi.ResourceIdentifier{
					Ordinal:   0,
					Group:     manifestConfigMap.GroupVersionKind().Group,
					Version:   manifestConfigMap.GroupVersionKind().Version,
					Kind:      manifestConfigMap.GroupVersionKind().Kind,
					Namespace: manifestConfigMap.Namespace,
					Name:      manifestConfigMap.Name,
					Resource:  "configmaps",
				},
			}
			return cmp.Diff(want, appliedWork.Status.AppliedResources[0], cmpOptions...)
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(), "Validate AppliedResourceMeta mismatch (-want, +got)")

		By(fmt.Sprintf("Resource %s should have been created in cluster %s", manifestConfigMapName, MemberCluster.ClusterName))
		Eventually(func() string {
			cm, err := MemberCluster.KubeClientSet.CoreV1().ConfigMaps(manifestConfigMap.Namespace).Get(ctx, manifestConfigMap.Name, metav1.GetOptions{})
			if err != nil {
				return err.Error()
			}
			return cmp.Diff(cm.Data, manifestConfigMap.Data)
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeEmpty(),
			"ConfigMap %s was not created in the cluster %s", manifestConfigMapName, MemberCluster.ClusterName)
	})
})
