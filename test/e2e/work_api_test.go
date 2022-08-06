package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/work-api/pkg/utils"

	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

const (
	eventuallyTimeout  = 60 // seconds
	eventuallyInterval = 1  // seconds
)

var defaultWorkNamespace = "fleet-member-" + MemberCluster.ClusterName

type manifestDetails struct {
	Manifest workapi.Manifest
	GVK      *schema.GroupVersionKind
	GVR      *schema.GroupVersionResource
	ObjMeta  metav1.ObjectMeta
}

var _ = Describe("work-api testing", func() {
	Context("with a Work resource that has two manifests: Deployment & Service", func() {
		var createdWork *workapi.Work
		var err error
		var mDetails []manifestDetails

		BeforeEach(func() {
			mDetails = generateManifestDetails([]string{
				"manifests/test-deployment.yaml",
				"manifests/test-service.yaml",
			})
			wns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: defaultWorkNamespace,
				},
			}

			_, err = HubCluster.KubeClientSet.CoreV1().Namespaces().Create(context.Background(), wns, metav1.CreateOptions{})
			Expect(err).Should(SatisfyAny(Succeed(), &utils.AlreadyExistMatcher{}))

			workObj := createWorkObj(
				getWorkName(5),
				defaultWorkNamespace,
				mDetails,
			)

			err = createWork(workObj)
			createdWork, err = retrieveWork(workObj.Namespace, workObj.Name)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err = deleteWorkResource(createdWork)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should have created: a respective AppliedWork, and the resources specified in the Work's manifests", func() {
			By("verifying an AppliedWork was created")
			Eventually(func() error {
				_, err := retrieveAppliedWork(createdWork.Name)

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("verifying a deployment was created")
			Eventually(func() error {
				_, err := MemberCluster.KubeClientSet.AppsV1().Deployments(mDetails[0].ObjMeta.Namespace).
					Get(context.Background(), mDetails[0].ObjMeta.Name, metav1.GetOptions{})

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("verifying a service was created")
			Eventually(func() error {
				_, err := MemberCluster.KubeClientSet.CoreV1().Services(mDetails[1].ObjMeta.Namespace).
					Get(context.Background(), mDetails[1].ObjMeta.Name, metav1.GetOptions{})

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("verifying that corresponding conditions were created")
			Eventually(func() bool {
				work, err := retrieveWork(createdWork.Namespace, createdWork.Name)
				if err != nil {
					return false
				}
				appliedCondition := meta.IsStatusConditionTrue(work.Status.Conditions, "Applied")
				availableCondition := meta.IsStatusConditionTrue(work.Status.Conditions, "Available")
				return appliedCondition && availableCondition
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("with two resource for the same resource: Deployment", func() {
		var workOne *workapi.Work
		var workTwo *workapi.Work
		var err error
		var manifestDetailsOne []manifestDetails
		var manifestDetailsTwo []manifestDetails

		BeforeEach(func() {
			manifestDetailsOne = generateManifestDetails([]string{
				"manifests/test-deployment.yaml",
			})
			manifestDetailsTwo = generateManifestDetails([]string{
				"manifests/test-deployment.yaml",
			})

			workOne = createWorkObj(
				getWorkName(5),
				defaultWorkNamespace,
				manifestDetailsOne,
			)

			workTwo = createWorkObj(
				getWorkName(5),
				defaultWorkNamespace,
				manifestDetailsTwo)

		})

		AfterEach(func() {
			err = deleteWorkResource(workOne)
			Expect(err).ToNot(HaveOccurred())

			err = deleteWorkResource(workTwo)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should ignore the duplicate manifest", func() {

			By("creating the work resources")
			err = createWork(workOne)
			Expect(err).ToNot(HaveOccurred())

			err = createWork(workTwo)
			Expect(err).ToNot(HaveOccurred())

			By("Checking the Applied Work status of each to see if one of the manifest is abandoned.")
			Eventually(func() bool {
				appliedWorkOne, err := retrieveAppliedWork(workOne.Name)
				if err != nil {
					return false
				}

				appliedWorkTwo, err := retrieveAppliedWork(workTwo.Name)
				if err != nil {
					return false
				}

				return len(appliedWorkOne.Status.AppliedResources)+len(appliedWorkTwo.Status.AppliedResources) == 1
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("Checking the work status of each works for verification")
			Eventually(func() bool {
				workOne, err := retrieveWork(workOne.Namespace, workOne.Name)
				if err != nil {
					return false
				}
				workTwo, err := retrieveWork(workTwo.Namespace, workTwo.Name)
				if err != nil {
					return false
				}
				workOneCondition := meta.IsStatusConditionTrue(workOne.Status.ManifestConditions[0].Conditions, "Applied")
				workTwoCondition := meta.IsStatusConditionTrue(workTwo.Status.ManifestConditions[0].Conditions, "Applied")
				return (workOneCondition || workTwoCondition) && !(workOneCondition && workTwoCondition)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("verifying the resource was garbage collected")
			Eventually(func() error {

				return MemberCluster.KubeClientSet.AppsV1().Deployments(manifestDetailsOne[0].ObjMeta.Namespace).Delete(context.Background(), manifestDetailsOne[0].ObjMeta.Name, metav1.DeleteOptions{})
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
			Eventually(func() error {

				return MemberCluster.KubeClientSet.AppsV1().Deployments(manifestDetailsTwo[0].ObjMeta.Namespace).Delete(context.Background(), manifestDetailsTwo[0].ObjMeta.Name, metav1.DeleteOptions{})
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})
	})

	Context("updating work with two newly added manifests: configmap & namespace", func() {
		var createdWork *workapi.Work
		var err error
		var initialManifestDetails []manifestDetails
		var addedManifestDetails []manifestDetails

		BeforeEach(func() {
			initialManifestDetails = generateManifestDetails([]string{
				"manifests/test-secret.yaml",
			})
			addedManifestDetails = generateManifestDetails([]string{
				"manifests/test-configmap.ns.yaml",
				"manifests/test-namespace.yaml",
			})

			workObj := createWorkObj(
				getWorkName(5),
				defaultWorkNamespace,
				initialManifestDetails,
			)

			err = createWork(workObj)
			createdWork, err = retrieveWork(workObj.Namespace, workObj.Name)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err = deleteWorkResource(createdWork)
			Expect(err).ToNot(HaveOccurred())

			err = MemberCluster.KubeClientSet.CoreV1().ConfigMaps(addedManifestDetails[0].ObjMeta.Namespace).Delete(context.Background(), addedManifestDetails[0].ObjMeta.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should have created the ConfigMap in the new namespace", func() {
			By("retrieving the existing work and updating it by adding new manifests")
			Eventually(func() error {
				createdWork, err = retrieveWork(createdWork.Namespace, createdWork.Name)
				Expect(err).ToNot(HaveOccurred())

				createdWork.Spec.Workload.Manifests = append(createdWork.Spec.Workload.Manifests, addedManifestDetails[0].Manifest, addedManifestDetails[1].Manifest)
				createdWork, err = updateWork(createdWork)

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("checking if the ConfigMap was created in the new namespace")
			Eventually(func() error {
				_, err := MemberCluster.KubeClientSet.CoreV1().ConfigMaps(addedManifestDetails[0].ObjMeta.Namespace).Get(context.Background(), addedManifestDetails[0].ObjMeta.Name, metav1.GetOptions{})

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("checking if the new Namespace was created ")
			Eventually(func() error {
				_, err := MemberCluster.KubeClientSet.CoreV1().Namespaces().Get(context.Background(), addedManifestDetails[1].ObjMeta.Name, metav1.GetOptions{})

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})
	})

	Context("work update with a modified Manifest", func() {
		var configMap corev1.ConfigMap
		var createdWork *workapi.Work
		var err error
		var manifestDetails []manifestDetails
		var newDataKey string
		var newDataValue string

		BeforeEach(func() {
			manifestDetails = generateManifestDetails([]string{
				"manifests/test-configmap.yaml",
			})
			newDataKey = getWorkName(5)
			newDataValue = getWorkName(5)

			workObj := createWorkObj(
				getWorkName(5),
				defaultWorkNamespace,
				manifestDetails,
			)

			err = createWork(workObj)
			createdWork, err = retrieveWork(workObj.Namespace, workObj.Name)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err = deleteWorkResource(createdWork)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reapply the manifest's updated spec on the spoke cluster", func() {
			By("retrieving the existing work and modifying the manifest")
			Eventually(func() error {
				createdWork, err = retrieveWork(createdWork.Namespace, createdWork.Name)

				// Extract and modify the ConfigMap by adding a new key value pair.
				err = json.Unmarshal(createdWork.Spec.Workload.Manifests[0].Raw, &configMap)
				configMap.Data[newDataKey] = newDataValue

				rawUpdatedManifest, _ := json.Marshal(configMap)

				obj, _, _ := genericCodec.Decode(rawUpdatedManifest, nil, nil)

				createdWork.Spec.Workload.Manifests[0].Object = obj
				createdWork.Spec.Workload.Manifests[0].Raw = rawUpdatedManifest

				createdWork, err = updateWork(createdWork)

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("verifying if the manifest was reapplied")
			Eventually(func() bool {
				configMap, _ := MemberCluster.KubeClientSet.CoreV1().ConfigMaps(manifestDetails[0].ObjMeta.Namespace).Get(context.Background(), manifestDetails[0].ObjMeta.Name, metav1.GetOptions{})

				return configMap.Data[newDataKey] == newDataValue
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("with all manifests replaced", func() {
		var appliedWork *workapi.AppliedWork
		var createdWork *workapi.Work
		var err error
		var originalManifestDetails []manifestDetails
		var replacedManifestDetails []manifestDetails
		resourcesStillExist := true

		BeforeEach(func() {
			originalManifestDetails = generateManifestDetails([]string{
				"manifests/test-secret.yaml",
			})
			replacedManifestDetails = generateManifestDetails([]string{
				"manifests/test-configmap.yaml",
			})

			workObj := createWorkObj(
				getWorkName(5),
				defaultWorkNamespace,
				originalManifestDetails,
			)

			err = createWork(workObj)
			createdWork, err = retrieveWork(workObj.Namespace, workObj.Name)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err = deleteWorkResource(createdWork)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should have deleted the original Work's resources, and created new resources with the replaced manifests", func() {
			By("getting the respective AppliedWork")
			Eventually(func() int {
				appliedWork, _ = retrieveAppliedWork(createdWork.Name)

				return len(appliedWork.Status.AppliedResources)
			}, eventuallyTimeout, eventuallyInterval).Should(Equal(len(originalManifestDetails)))

			By("updating the Work resource with replaced manifests")
			Eventually(func() error {
				createdWork, err = retrieveWork(createdWork.Namespace, createdWork.Name)
				createdWork.Spec.Workload.Manifests = nil
				for _, mD := range replacedManifestDetails {
					createdWork.Spec.Workload.Manifests = append(createdWork.Spec.Workload.Manifests, mD.Manifest)
				}

				createdWork, err = updateWork(createdWork)

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("verifying all the initial Work owned resources were deleted")
			Eventually(func() bool {
				for resourcesStillExist == true {
					for _, ar := range appliedWork.Status.AppliedResources {
						gvr := schema.GroupVersionResource{
							Group:    ar.Group,
							Version:  ar.Version,
							Resource: ar.Resource,
						}

						_, err = MemberCluster.DynamicClient.Resource(gvr).Namespace(ar.Namespace).Get(context.Background(), ar.Name, metav1.GetOptions{})
						if err != nil {
							resourcesStillExist = false
						} else {
							resourcesStillExist = true
						}
					}
				}

				return resourcesStillExist
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(BeTrue())

			By("verifying the new manifest was applied")
			Eventually(func() error {
				_, err = MemberCluster.KubeClientSet.CoreV1().ConfigMaps(replacedManifestDetails[0].ObjMeta.Namespace).Get(context.Background(), replacedManifestDetails[0].ObjMeta.Name, metav1.GetOptions{})

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})
	})

	Context("Work deletion", func() {
		var createdWork *workapi.Work
		var err error
		var manifestDetails []manifestDetails

		BeforeEach(func() {
			manifestDetails = generateManifestDetails([]string{
				"manifests/test-secret.yaml",
			})

			workObj := createWorkObj(
				getWorkName(5),
				defaultWorkNamespace,
				manifestDetails,
			)

			err = createWork(workObj)
			createdWork, err = retrieveWork(workObj.Namespace, workObj.Name)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should delete the Work and verify the resource has been garbage collected", func() {
			By("verifying the manifest was applied")
			Eventually(func() error {
				_, err = MemberCluster.KubeClientSet.CoreV1().Secrets(manifestDetails[0].ObjMeta.Namespace).Get(context.Background(), manifestDetails[0].ObjMeta.Name, metav1.GetOptions{})

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("deleting the Work resource")
			err = deleteWorkResource(createdWork)
			Expect(err).ToNot(HaveOccurred())

			By("verifying the resource was garbage collected")
			Eventually(func() error {
				err = MemberCluster.KubeClientSet.CoreV1().Secrets(manifestDetails[0].ObjMeta.Namespace).Delete(context.Background(), manifestDetails[0].ObjMeta.Name, metav1.DeleteOptions{})

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})

	})
})

func createWorkObj(workName string, workNamespace string, manifestDetails []manifestDetails) *workapi.Work {
	work := &workapi.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: workNamespace,
		},
	}

	for _, detail := range manifestDetails {
		work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, detail.Manifest)
	}

	return work
}
func createWork(work *workapi.Work) error {
	return HubCluster.KubeClient.Create(context.Background(), work)
}
func decodeUnstructured(manifest workapi.Manifest) (*unstructured.Unstructured, error) {
	unstructuredObj := &unstructured.Unstructured{}
	err := unstructuredObj.UnmarshalJSON(manifest.Raw)

	return unstructuredObj, err
}
func deleteWorkResource(work *workapi.Work) error {
	return HubCluster.KubeClient.Delete(context.Background(), work)
}
func generateManifestDetails(manifestFiles []string) []manifestDetails {
	var details []manifestDetails

	for _, file := range manifestFiles {
		detail := manifestDetails{}

		// Read files, create manifest
		fileRaw, err := testManifestFiles.ReadFile(file)
		Expect(err).ToNot(HaveOccurred())

		obj, gvk, err := genericCodec.Decode(fileRaw, nil, nil)
		Expect(err).ToNot(HaveOccurred())

		jsonObj, err := json.Marshal(obj)
		Expect(err).ToNot(HaveOccurred())

		detail.Manifest = workapi.Manifest{
			RawExtension: runtime.RawExtension{
				Object: obj,
				Raw:    jsonObj},
		}

		unstructuredObj, err := decodeUnstructured(detail.Manifest)
		Expect(err).ShouldNot(HaveOccurred())

		mapping, err := MemberCluster.RestMapper.RESTMapping(unstructuredObj.GroupVersionKind().GroupKind(), unstructuredObj.GroupVersionKind().Version)
		Expect(err).ShouldNot(HaveOccurred())

		detail.GVK = gvk
		detail.GVR = &mapping.Resource
		detail.ObjMeta = metav1.ObjectMeta{
			Name:      unstructuredObj.GetName(),
			Namespace: unstructuredObj.GetNamespace(),
		}

		details = append(details, detail)
	}

	return details
}
func retrieveAppliedWork(appliedWorkName string) (*workapi.AppliedWork, error) {
	retrievedAppliedWork := workapi.AppliedWork{}
	err := MemberCluster.KubeClient.Get(context.Background(), types.NamespacedName{Name: appliedWorkName}, &retrievedAppliedWork)
	if err != nil {
		return &retrievedAppliedWork, err
	}

	return &retrievedAppliedWork, nil
}
func retrieveWork(workNamespace string, workName string) (*workapi.Work, error) {
	workRetrieved := workapi.Work{}
	err := HubCluster.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: workNamespace, Name: workName}, &workRetrieved)
	if err != nil {
		println("err still exists")
		println(err.Error())
		return nil, err
	}
	return &workRetrieved, nil
}
func updateWork(work *workapi.Work) (*workapi.Work, error) {
	err := HubCluster.KubeClient.Update(context.Background(), work)
	if err != nil {
		return nil, err
	}

	updatedWork, err := retrieveWork(work.Namespace, work.Name)
	if err != nil {
		return nil, err
	}
	return updatedWork, err
}

func getWorkName(length int) string {
	return "work" + rand.String(length)
}
