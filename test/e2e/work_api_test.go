package e2e

import (
	"fmt"

	"github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("Work API test", func() {
	var testMemberClusterName string
	var testWorkName string
	var testWorkNamespace corev1.Namespace
	var testManifestConfigMap corev1.ConfigMap
	var testManifestConfigMapName string
	var testManifestNamespace corev1.Namespace

	ginkgo.BeforeEach(func() {
		testMemberClusterName = fmt.Sprintf("work-%s", rand.String(10))
		testWorkNamespace = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testMemberClusterName,
			},
		}

		framework.CreateNamespace(*HubCluster, &testWorkNamespace)

		testManifestNamespace = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("configMapNs-%s", rand.String(10)),
			},
		}

		testManifestConfigMapName = fmt.Sprintf("configMap-%s", rand.String(10))
		framework.CreateNamespace(*MemberCluster, &testManifestNamespace)

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
		framework.CreateWork(testWorkName, testWorkNamespace.Name, *HubCluster, &testManifestConfigMap)
		framework.WaitAppliedWorkPresent(testWorkName, testWorkNamespace.Name, *MemberCluster)
		DeferCleanup(func() {
			framework.RemoveWork(testWorkName, testWorkNamespace.Name, *HubCluster)
			framework.DeleteNamespace(*HubCluster, &testWorkNamespace)
			framework.DeleteNamespace(*MemberCluster, &testManifestNamespace)
		})
	})

	ginkgo.Context("Work Creation test", func() {

		ginkgo.By("check if manifest was applied to the member cluster")
		_, err := framework.GetConfigMap(MemberCluster, testManifestConfigMapName, testManifestNamespace.Name)
		gomega.Expect(err).To(gomega.BeNil())

		ginkgo.By("check if Work condition of Applied is successful")
		work, err := framework.GetWork(testWorkName, testWorkNamespace.Name, *HubCluster)
		gomega.Expect(err).To(gomega.BeNil())

		framework.CheckIfAppliedConditionIsTrue(work)

		ginkgo.By("check if AppliedWork is created correctly")
		appliedWork, err := framework.GetAppliedWork(testWorkName, testWorkNamespace.Name, *HubCluster)
		gomega.Expect(err).To(gomega.BeNil())
		gomega.Expect(len(appliedWork.Status.AppliedResources)).To(gomega.Equal(1))
		gomega.Expect(appliedWork.Status.AppliedResources[0].Name).To(gomega.Equal(testManifestConfigMapName))
		gomega.Expect(appliedWork.Status.AppliedResources[0].Namespace).To(gomega.Equal(testManifestNamespace))

	})

	ginkgo.Context("Work Update test", func() {
		ginkgo.It("update work by adding new manifest", func() {
			newObjectName := fmt.Sprintf("deployment-%s", rand.String(10))
			newObject := v1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      newObjectName,
					Namespace: testManifestNamespace.Name,
				},
			}
			err := framework.AddManifestToWork(&newObject, *HubCluster, testWorkName, testWorkNamespace.Name)
			gomega.Expect(err).To(gomega.BeNil())

			By("check if Work was updated correctly")
			updatedWork, err := framework.GetWork(testWorkName, testWorkNamespace.Name, *HubCluster)
			gomega.Expect(err).To(gomega.BeNil())
			gomega.Expect(len(updatedWork.Spec.Workload.Manifests)).To(gomega.Equal(2))

			By("check if Condition of Work is set to Applied")
			framework.CheckIfAppliedConditionIsTrue(updatedWork)

			framework.WaitConfigMapCreated(MemberCluster, newObjectName, testManifestNamespace.Name)

			By("check if existing resource still exists")
			existingConfigMap, err := framework.GetConfigMap(MemberCluster, testManifestConfigMapName, testManifestNamespace.Name)
			gomega.Expect(err).To(gomega.BeNil())
			gomega.Expect(existingConfigMap.Data).To(gomega.Equal(testManifestConfigMap.Data))

			By("check if new resource was created correctly")
			framework.WaitDeploymentCreated(MemberCluster, newObjectName, testManifestNamespace.Name)
			deployment, err := framework.GetDeployment(MemberCluster, newObjectName, testManifestNamespace.Name)
			gomega.Expect(err).To(gomega.BeNil())
			gomega.Expect(deployment.Kind).To(gomega.Equal("Deployment"))
			gomega.Expect(deployment.APIVersion).To(gomega.Equal("apps/v1"))

			By("check if AppliedWork is updated correctly")
			appliedWork, err := framework.GetAppliedWork(testWorkName, testWorkNamespace.Name, *HubCluster)
			gomega.Expect(err).To(gomega.BeNil())
			gomega.Expect(len(appliedWork.Status.AppliedResources)).To(gomega.Equal(2))

			for _, appliedResources := range appliedWork.Status.AppliedResources {
				switch appliedResources.Name {
				case testManifestConfigMapName:
					gomega.Expect(appliedResources.Kind).To(gomega.Equal("ConfigMap"))
					gomega.Expect(appliedResources.Version).To(gomega.Equal("v1"))
				case newObjectName:
					gomega.Expect(appliedResources.Kind).To(gomega.Equal("Deployment"))
					gomega.Expect(appliedResources.Version).To(gomega.Equal("apps/v1"))

				}
			}

			gomega.Expect(appliedWork.Status.AppliedResources[0].Name).To(gomega.Equal(testManifestConfigMapName))
			gomega.Expect(appliedWork.Status.AppliedResources[0].Namespace).To(gomega.Equal(testManifestNamespace))
		})

		ginkgo.It("update work by editing existing manifest", func() {
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
			gomega.Expect(err).To(gomega.BeNil())

			framework.WaitConfigMapUpdated(MemberCluster, testManifestConfigMapName, testManifestNamespace.Name, newObject.Data)

			By("check if Work is updated correctly")
			updatedWork, err := framework.GetWork(testWorkName, testWorkNamespace.Name, *HubCluster)
			gomega.Expect(err).To(gomega.BeNil())
			framework.CheckIfAppliedConditionIsTrue(updatedWork)
		})
	})

	ginkgo.Context("Work Delete Test", func() {
		ginkgo.It("deleting the Work", func() {
			framework.RemoveWork(testWorkName, testWorkNamespace.Name, *HubCluster)
			framework.WaitAppliedWorkAbsent(testWorkName, testWorkName, *MemberCluster)

			ginkgo.By("check if resource was garbage collected from the member cluster")
			_, err := framework.GetConfigMap(MemberCluster, testManifestConfigMapName, testManifestNamespace.Name)
			gomega.Expect(err).ToNot(gomega.BeNil())
		})
	})
})
