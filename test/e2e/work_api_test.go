package e2e

import (
	"fmt"
	"go.goms.io/fleet/pkg/utils"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.goms.io/fleet/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Work API test", Ordered, func() {
	var testMemberClusterName string
	var testWorkName string
	var testWorkNamespace corev1.Namespace
	var testManifestConfigMap corev1.ConfigMap
	var testManifestConfigMapName string
	var testManifestNamespace corev1.Namespace

	BeforeAll(func() {
		testMemberClusterName = fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName)
		testWorkNamespace = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testMemberClusterName,
			},
		}

		framework.CreateNamespace(*HubCluster, &testWorkNamespace)
	})

	BeforeEach(func() {

		testManifestNamespace = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("test-namespace" + rand.String(10)),
			},
		}

		testManifestConfigMapName = fmt.Sprintf("test-configmap")
		framework.CreateNamespace(*MemberCluster, &testManifestNamespace)

		testWorkName = fmt.Sprintf("test-work-" + rand.String(10))

		testManifestConfigMap = corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      testManifestConfigMapName,
				Namespace: testManifestNamespace.Name,
			},
			Data: map[string]string{
				"test": "test",
			},
		}
		framework.CreateWork(testWorkName, testWorkNamespace.Name, *HubCluster, []workapi.Manifest{{RawExtension: runtime.RawExtension{Object: &testManifestConfigMap}}})
		framework.WaitWorkPresent(testWorkName, testWorkNamespace.Name, *HubCluster)
		framework.WaitAppliedWorkPresent(testWorkName, testWorkNamespace.Name, *MemberCluster)

	})

	AfterEach(func() {
		framework.RemoveWork(testWorkName, testWorkNamespace.Name, *HubCluster)
	})

	AfterAll(func() {
		framework.DeleteNamespace(*HubCluster, &testWorkNamespace)
		framework.DeleteNamespace(*MemberCluster, &testManifestNamespace)
	})

	It("Work Creation test", func() {
		By("check if manifest was applied to the member cluster")
		framework.WaitConfigMapCreated(MemberCluster, testManifestConfigMapName, testManifestNamespace.Name)
		_, err := framework.GetConfigMap(MemberCluster, testManifestConfigMapName, testManifestNamespace.Name)
		Expect(err).To(BeNil())

		By("check if Work condition of Applied is successful")
		work, err := framework.GetWork(testWorkName, testWorkNamespace.Name, *HubCluster)
		Expect(err).To(BeNil())

		framework.CheckIfAppliedConditionIsTrue(work)

		By("check if AppliedWork is created correctly")
		appliedWork, err := framework.GetAppliedWork(testWorkName, testWorkNamespace.Name, *MemberCluster)
		Expect(err).To(BeNil())
		Expect(len(appliedWork.Status.AppliedResources)).To(Equal(1))
		Expect(appliedWork.Status.AppliedResources[0].Name).To(Equal(testManifestConfigMapName))
		Expect(appliedWork.Status.AppliedResources[0].Namespace).To(Equal(testManifestNamespace.Name))

	})

	It("update work by adding new manifest", func() {
		newObjectName := fmt.Sprintf("test-pod")
		newPodObject := corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      newObjectName,
				Namespace: testManifestNamespace.Name,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "testing-image",
					},
				},
			},
		}
		err := framework.AddManifestToWork(&newPodObject, *HubCluster, testWorkName, testWorkNamespace.Name)
		Expect(err).To(BeNil())

		By("check if Work was updated correctly")
		updatedWork, err := framework.GetWork(testWorkName, testWorkNamespace.Name, *HubCluster)
		Expect(err).To(BeNil())
		Expect(len(updatedWork.Spec.Workload.Manifests)).To(Equal(2))

		By("check if Condition of Work is set to Applied")
		framework.CheckIfAppliedConditionIsTrue(updatedWork)

		By("check if existing resource still exists")
		existingConfigMap, err := framework.GetConfigMap(MemberCluster, testManifestConfigMapName, testManifestNamespace.Name)
		Expect(err).To(BeNil())
		Expect(existingConfigMap.Data).To(Equal(testManifestConfigMap.Data))

		By("check if new resource was created correctly")
		framework.WaitPodCreated(MemberCluster, newObjectName, testManifestNamespace.Name)
		pod, err := framework.GetPod(MemberCluster, newObjectName, testManifestNamespace.Name)
		Expect(err).To(BeNil())
		Expect(pod.Spec.Containers[0].Name).To(Equal("test-container"))
		Expect(pod.Spec.Containers[0].Image).To(Equal("testing-image"))

		By("check if AppliedWork is updated correctly")
		appliedWork, err := framework.GetAppliedWork(testWorkName, testWorkNamespace.Name, *MemberCluster)
		Expect(err).To(BeNil())
		Expect(len(appliedWork.Status.AppliedResources)).To(Equal(2))

		for _, appliedResources := range appliedWork.Status.AppliedResources {
			switch appliedResources.Name {
			case testManifestConfigMapName:
				Expect(appliedResources.Kind).To(Equal("ConfigMap"))
				Expect(appliedResources.Version).To(Equal("v1"))
			case newObjectName:
				Expect(appliedResources.Kind).To(Equal("Pod"))
				Expect(appliedResources.Version).To(Equal("v1"))

			}
		}

		Expect(appliedWork.Status.AppliedResources[0].Name).To(Equal(testManifestConfigMapName))
		Expect(appliedWork.Status.AppliedResources[0].Namespace).To(Equal(testManifestNamespace.Name))
	})

	It("update work by editing existing manifest", func() {
		newObject := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      testManifestConfigMapName,
				Namespace: testManifestNamespace.Name,
			},
			Data: map[string]string{
				"data": "newData",
			},
		}
		err := framework.ReplaceWorkManifest(newObject, *HubCluster, testWorkName, testWorkNamespace.Name)
		Expect(err).To(BeNil())

		framework.WaitConfigMapUpdated(MemberCluster, testManifestConfigMapName, testManifestNamespace.Name, newObject.Data)

		By("check if Work is updated correctly")
		updatedWork, err := framework.GetWork(testWorkName, testWorkNamespace.Name, *HubCluster)
		Expect(err).To(BeNil())
		framework.CheckIfAppliedConditionIsTrue(updatedWork)
	})

	It("Work Delete Test", func() {
		framework.RemoveWork(testWorkName, testWorkNamespace.Name, *HubCluster)
		framework.WaitAppliedWorkAbsent(testWorkName, testWorkName, *MemberCluster)

		By("check if resource was garbage collected from the member cluster")
		_, err := framework.GetConfigMap(MemberCluster, testManifestConfigMapName, testManifestNamespace.Name)
		Expect(err).ToNot(BeNil())
	})
})
