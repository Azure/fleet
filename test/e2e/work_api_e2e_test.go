package e2e

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/api/meta"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.goms.io/fleet/pkg/utils"
	testutils "go.goms.io/fleet/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// TODO: when join/leave logic is connected to work-api, join the Hub and Member for this test.
var _ = Describe("Work API Controller test", func() {

	const (
		conditionTypeApplied = "Applied"
	)

	var (
		ctx      context.Context
		workList []workapi.Work
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	AfterEach(func() {
		if len(workList) > 0 {
			Expect(testutils.DeleteWork(*HubCluster, workList, ctx)).Should(Succeed(), "Deletion of work failed.")
		}
	})

	It("Upon successful work creation of a single resource, work manifest is applied and resource is created", func() {
		workName := utils.RandStr()

		// Configmap will be included in this work object.
		manifestConfigMapName := "work-configmap"
		manifestConfigMap := corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      manifestConfigMapName,
				Namespace: workResourceNs.Name,
			},
			Data: map[string]string{
				"test-key": "test-data",
			},
		}
		By(fmt.Sprintf("creating work %s/%s of %s", workName, workNs.Name, manifestConfigMapName))
		testutils.CreateWork(*HubCluster, workName, workNs.Name, ctx, workList, []workapi.Manifest{
			{
				RawExtension: runtime.RawExtension{Object: &manifestConfigMap},
			},
		})

		By(fmt.Sprintf("Waiting for AppliedWork %s to be created", workName))
		Eventually(func() error {
			return MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName}, &workapi.AppliedWork{})
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Failed to create AppliedWork %s", workName)

		By(fmt.Sprintf("Applied Condition should be set to True for Work %s/%s", workName, workNs.Name))
		Eventually(func() bool {
			work := workapi.Work{}
			err := HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: workNs.Name}, &work)
			if err != nil {
				return false
			}

			return meta.IsStatusConditionTrue(work.Status.Conditions, conditionTypeApplied)
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())

		By(fmt.Sprintf("AppliedWorkStatus should contain the meta for the resource %s", manifestConfigMapName))
		Eventually(func() bool {
			appliedWork := workapi.AppliedWork{}
			err := MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName}, &appliedWork)
			if err != nil {
				return false
			}
			for _, status := range appliedWork.Status.AppliedResources {
				if testutils.AppliedWorkContainsResource(status, manifestConfigMap.Name, manifestConfigMap.APIVersion, manifestConfigMap.Kind) {
					return true
				}
			}
			return false
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())

		By(fmt.Sprintf("Resource %s should have been created in cluster %s", manifestConfigMapName, MemberCluster.ClusterName))
		Eventually(func() bool {
			cm, err := MemberCluster.KubeClientSet.CoreV1().ConfigMaps(manifestConfigMap.Namespace).
				Get(ctx, manifestConfigMap.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}

			return reflect.DeepEqual(cm.Data, manifestConfigMap.Data)
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
	})

	It("Upon successful work creation of multiple resources, all manifests are applied and resources are created", func() {
		workName := utils.RandStr()

		// Secret will be included in this work object.
		manifestSecretName := "work-secret"
		manifestSecret := corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      manifestSecretName,
				Namespace: workResourceNs.Name,
			},
			Data: map[string][]byte{"secretData": []byte("testByte")},
		}

		manifestServiceName := "work-service"
		manifestService := corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      manifestServiceName,
				Namespace: workResourceNs.Name,
				Labels:    map[string]string{},
			},
			Spec: corev1.ServiceSpec{
				Ports:    []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
				Selector: map[string]string{"run": "test-nginx"},
			},
		}

		By(fmt.Sprintf("creating work %s/%s of %s and %s", workName, workNs.Name, manifestSecretName, manifestServiceName))
		testutils.CreateWork(*HubCluster, workName, workNs.Name, ctx, workList, []workapi.Manifest{
			{
				RawExtension: runtime.RawExtension{Object: &manifestSecret},
			},
			{
				RawExtension: runtime.RawExtension{Object: &manifestService},
			},
		})

		// Wait for the applied works to be created on the member cluster
		Eventually(func() error {
			return MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName}, &workapi.AppliedWork{})
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeNil())

		By(fmt.Sprintf("Applied Condition should be set to True for Work %s/%s", workName, workNs.Name))
		Eventually(func() bool {
			work := workapi.Work{}
			err := HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: workNs.Name}, &work)
			if err != nil {
				return false
			}

			return meta.IsStatusConditionTrue(work.Status.Conditions, conditionTypeApplied)
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())

		By(fmt.Sprintf("AppliedWorkStatus should contain the meta for the resource %s and %s", manifestSecretName, manifestServiceName))
		Eventually(func() bool {
			appliedWork := workapi.AppliedWork{}
			err := MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName}, &appliedWork)
			if err != nil {
				return false
			}
			secretExists := false
			podExists := false
			for _, appliedResources := range appliedWork.Status.AppliedResources {
				if testutils.AppliedWorkContainsResource(appliedResources, manifestSecret.Name, manifestSecret.APIVersion, manifestSecret.Kind) {
					secretExists = true
				}
				if testutils.AppliedWorkContainsResource(appliedResources, manifestService.Name, manifestService.APIVersion, manifestService.Kind) {
					podExists = true
				}
			}

			return secretExists && podExists
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())

		By(fmt.Sprintf("Resource %s and %s should have been created in cluster %s", manifestSecretName, manifestServiceName, MemberCluster.ClusterName))
		Eventually(func() bool {
			_, secretErr := MemberCluster.KubeClientSet.CoreV1().Secrets(workResourceNs.Name).Get(ctx, manifestSecret.Name, metav1.GetOptions{})

			_, podErr := MemberCluster.KubeClientSet.CoreV1().Services(workResourceNs.Name).Get(ctx, manifestService.Name, metav1.GetOptions{})

			return secretErr == nil && podErr == nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
	})

	It("Upon successful work creation of a CRD resource, manifest is applied, and resources are created", func() {
		workName := utils.RandStr()
		gvk, manifestCRD := testutils.GenerateCRDObjectFromFile(*MemberCluster, "manifests/test-crd.yaml", genericCodec)
		_, err := MemberCluster.RestMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		Expect(err).Should(Succeed(), "The Test CRD was not included in the RestMapper for Cluster %s", MemberCluster.ClusterName)

		By(fmt.Sprintf("creating work %s/%s of %s", workName, workNs.Name, gvk.Kind))
		testutils.CreateWork(*HubCluster, workName, workNs.Name, ctx, workList, []workapi.Manifest{
			{
				RawExtension: manifestCRD,
			},
		})

		// Wait for the applied works to be created on the member cluster
		Eventually(func() error {
			return MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName}, &workapi.AppliedWork{})
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Waiting for AppliedWork %s failed", workName)

		By(fmt.Sprintf("Applied Condition should be set to True for Work %s/%s", workName, workNs.Name))
		Eventually(func() bool {
			work := workapi.Work{}
			err := HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: workNs.Name}, &work)
			if err != nil {
				return false
			}

			return meta.IsStatusConditionTrue(work.Status.Conditions, conditionTypeApplied)
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue(), "Applied Condition not True for work %s", workName)

		By(fmt.Sprintf("AppliedWorkStatus should contain the meta for the resource %s", gvk.Kind))
		Eventually(func() bool {
			appliedWork := workapi.AppliedWork{}
			err := MemberCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName}, &appliedWork)
			if err != nil {
				return false
			}
			for _, status := range appliedWork.Status.AppliedResources {
				if testutils.AppliedWorkContainsResource(status, "testcrds.multicluster.x-k8s.io", gvk.Version, gvk.Kind) {
					return true
				}
			}

			return false
		}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue(), "AppliedWork %s does not portray correct resources created", workName)

		By(fmt.Sprintf("Resource %s should have been created in cluster %s", "testcrds.multicluster.x-k8s.io", MemberCluster.ClusterName))
		Eventually(func() error {
			_, err := MemberCluster.APIExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, "testcrds.multicluster.x-k8s.io", metav1.GetOptions{})

			return err
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Resources %s not created in cluster %s", "testcrds.multicluster.x-k8s.io", MemberCluster.ClusterName)
	})
})
