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
	Context("Creating Work", func() {
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
	wns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultWorkNamespace,
		},
	}
	err := HubCluster.KubeClient.Delete(context.Background(), wns)
	if err != nil {
		return err
	}
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
