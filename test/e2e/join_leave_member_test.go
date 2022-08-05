/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/pkg/utils"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

type manifestDetails struct {
	Manifest workapi.Manifest
	GVK      *schema.GroupVersionKind
	GVR      *schema.GroupVersionResource
	ObjMeta  v1.ObjectMeta
}

const (
	eventuallyTimeout    = 60 // seconds
	eventuallyInterval   = 1  // seconds
	defaultWorkNamespace = "default"
)

var _ = Describe("Fleet testing", func() {
	var mc *v1alpha1.MemberCluster
	var sa *corev1.ServiceAccount
	var memberIdentity rbacv1.Subject
	var memberNS *corev1.Namespace
	var imc *v1alpha1.InternalMemberCluster

	memberNS = NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName))

	Context("Join/Leave member clusters don't share member identity", func() {
		BeforeEach(func() {
			memberIdentity = rbacv1.Subject{
				Name:      MemberCluster.ClusterName,
				Kind:      "ServiceAccount",
				Namespace: "fleet-system",
			}
		})

		It("Join flow is successful ", func() {
			By("Prepare resources in member cluster", func() {
				// create testing NS in member cluster
				framework.CreateNamespace(*MemberCluster, memberNS)
				framework.WaitNamespace(*MemberCluster, memberNS)

				sa = NewServiceAccount(memberIdentity.Name, memberNS.Name)
				framework.CreateServiceAccount(*MemberCluster, sa)
			})

			By("deploy memberCluster in the hub cluster", func() {
				mc = NewMemberCluster(MemberCluster.ClusterName, 60, memberIdentity, v1alpha1.ClusterStateJoin)

				framework.CreateMemberCluster(*HubCluster, mc)
				framework.WaitMemberCluster(*HubCluster, mc)

				By("check if internalmembercluster created in the hub cluster", func() {
					imc = NewInternalMemberCluster(MemberCluster.ClusterName, memberNS.Name)
					framework.WaitInternalMemberCluster(*HubCluster, imc)
				})
			})

			By("check if membercluster condition is updated to Joined", func() {
				framework.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionTrue, 3*framework.PollTimeout)
			})

			By("check if internalMemberCluster condition is updated to Joined", func() {
				framework.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.ConditionTypeInternalMemberClusterJoin, v1.ConditionTrue, 3*framework.PollTimeout)
			})

		})
		It("leave flow is successful ", func() {

			By("update membercluster in the hub cluster", func() {

				framework.UpdateMemberClusterState(*HubCluster, mc, v1alpha1.ClusterStateLeave)
				framework.WaitMemberCluster(*HubCluster, mc)
			})

			By("check if membercluster condition is updated to Left", func() {
				framework.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionFalse, 3*framework.PollTimeout)
			})

			By("check if internalMemberCluster condition is updated to Left", func() {
				framework.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.ConditionTypeInternalMemberClusterJoin, v1.ConditionFalse, 3*framework.PollTimeout)
			})

			By("member namespace is deleted from hub cluster", func() {
				framework.DeleteMemberCluster(*HubCluster, mc)
				Eventually(func() bool {
					err := HubCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: memberNS.Name, Namespace: ""}, memberNS)
					return apierrors.IsNotFound(err)
				}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
			})
		})
	})

	Context("work-api testing", func() {
		var createdWork *workapi.Work
		var err error
		var manifestDetails []manifestDetails

		BeforeEach(func() {
			manifestDetails = generateManifestDetails(
				[]string{
					"manifests/test-deployment.yaml",
					"manifests/test-service.yaml",
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

		AfterEach(func() {
			err = deleteWorkResource(createdWork)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should have created the CRD within the spoke", func() {
			Eventually(func() error {
				By("verifying the CRD exists within the spoke")
				_, err = MemberCluster.ApiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.Background(), manifestDetails[0].ObjMeta.Name, v1.GetOptions{})

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})
	})
})

func createWorkObj(workName string, workNamespace string, manifestDetails []manifestDetails) *workapi.Work {
	work := &workapi.Work{
		ObjectMeta: v1.ObjectMeta{
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

		mapping, err := HubCluster.RestMapper.RESTMapping(unstructuredObj.GroupVersionKind().GroupKind(), unstructuredObj.GroupVersionKind().Version)
		Expect(err).ShouldNot(HaveOccurred())

		detail.GVK = gvk
		detail.GVR = &mapping.Resource
		detail.ObjMeta = v1.ObjectMeta{
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
	return "work" + utilrand.String(length)
}
