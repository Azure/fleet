/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
	"sigs.k8s.io/work-api/pkg/utils"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	fleetutil "go.goms.io/fleet/pkg/utils"
)

const (
	eventuallyTimeout  = 10 * time.Second
	eventuallyInterval = 500 * time.Millisecond
)

var defaultWorkNamespace = fmt.Sprintf(fleetutil.NamespaceNameFormat, MemberCluster.ClusterName)

var _ = Describe("work-api testing", Ordered, func() {

	wns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultWorkNamespace,
		},
	}

	BeforeAll(func() {
		_, err := HubCluster.KubeClientSet.CoreV1().Namespaces().Create(context.Background(), wns, metav1.CreateOptions{})
		Expect(err).Should(SatisfyAny(Succeed(), &utils.AlreadyExistMatcher{}))
	})

	Context("with a Work resource that has two manifests: Deployment & Service", func() {
		var createdWork *workapi.Work
		var err error
		var mDetails []manifestDetails

		BeforeEach(func() {
			mDetails = generateManifestDetails([]string{
				"manifests/test-deployment.yaml",
				"manifests/test-service.yaml",
			})

			workObj := createWorkObj(
				getWorkName(5),
				defaultWorkNamespace,
				mDetails,
			)

			err = createWork(workObj, HubCluster)
			Expect(err).ToNot(HaveOccurred())

			createdWork, err = retrieveWork(workObj.Namespace, workObj.Name, HubCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err = deleteWorkResource(createdWork, HubCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should have created: a respective AppliedWork, and the resources specified in the Work's manifests", func() {
			By("verifying an AppliedWork was created")
			Eventually(func() error {
				_, err := retrieveAppliedWork(createdWork.Name, MemberCluster)

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
				work, err := retrieveWork(createdWork.Namespace, createdWork.Name, HubCluster)
				if err != nil {
					return false
				}
				appliedCondition := meta.IsStatusConditionTrue(work.Status.Conditions, "Applied")
				return appliedCondition
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

		It("should apply both the works with duplicated manifest", func() {
			By("creating the work resources")
			err = createWork(workOne, HubCluster)
			Expect(err).ToNot(HaveOccurred())

			err = createWork(workTwo, HubCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Checking the Applied Work status of each to see both are applied.")
			Eventually(func() bool {
				appliedWorkOne, err := retrieveAppliedWork(workOne.Name, MemberCluster)
				if err != nil {
					return false
				}

				appliedWorkTwo, err := retrieveAppliedWork(workTwo.Name, MemberCluster)
				if err != nil {
					return false
				}

				return len(appliedWorkOne.Status.AppliedResources) == 1 && len(appliedWorkTwo.Status.AppliedResources) == 1
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("Checking the work status of each works for verification")
			Eventually(func() bool {
				workOne, err := retrieveWork(workOne.Namespace, workOne.Name, HubCluster)
				if err != nil {
					return false
				}
				workTwo, err := retrieveWork(workTwo.Namespace, workTwo.Name, HubCluster)
				if err != nil {
					return false
				}
				workOneCondition := meta.IsStatusConditionTrue(workOne.Status.ManifestConditions[0].Conditions, string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied))
				workTwoCondition := meta.IsStatusConditionTrue(workTwo.Status.ManifestConditions[0].Conditions, string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied))
				return workOneCondition && workTwoCondition
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("verifying the one resource on the spoke are owned by both appliedWork")
			var deploy appsv1.Deployment
			Eventually(func() int {
				err := MemberCluster.KubeClient.Get(context.Background(), types.NamespacedName{
					Name:      manifestDetailsOne[0].ObjMeta.Name,
					Namespace: manifestDetailsOne[0].ObjMeta.Namespace}, &deploy)
				Expect(err).Should(Succeed())
				return len(deploy.OwnerReferences)
			}, eventuallyTimeout, eventuallyInterval).Should(Equal(2))

			By("delete the work two resources")
			Expect(deleteWorkResource(workTwo, HubCluster)).To(Succeed())

			By("Delete one work wont' delete the manifest")
			Eventually(func() int {
				err := MemberCluster.KubeClient.Get(context.Background(), types.NamespacedName{
					Name:      manifestDetailsOne[0].ObjMeta.Name,
					Namespace: manifestDetailsOne[0].ObjMeta.Namespace}, &deploy)
				Expect(err).Should(Succeed())
				return len(deploy.OwnerReferences)
			}, eventuallyTimeout, eventuallyInterval).Should(Equal(1))

			By("delete the work one resources")
			err = deleteWorkResource(workOne, HubCluster)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool {
				err := MemberCluster.KubeClient.Get(context.Background(), types.NamespacedName{
					Name:      manifestDetailsOne[0].ObjMeta.Name,
					Namespace: manifestDetailsOne[0].ObjMeta.Namespace}, &deploy)
				return apierrors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
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

			err = createWork(workObj, HubCluster)
			Expect(err).ToNot(HaveOccurred())

			createdWork, err = retrieveWork(workObj.Namespace, workObj.Name, HubCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err = deleteWorkResource(createdWork, HubCluster)
			Expect(err).ToNot(HaveOccurred())

			err = MemberCluster.KubeClientSet.CoreV1().ConfigMaps(addedManifestDetails[0].ObjMeta.Namespace).Delete(context.Background(), addedManifestDetails[0].ObjMeta.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should have created the ConfigMap in the new namespace", func() {
			By("retrieving the existing work and updating it by adding new manifests")
			Eventually(func() error {
				createdWork, err = retrieveWork(createdWork.Namespace, createdWork.Name, HubCluster)
				Expect(err).ToNot(HaveOccurred())

				createdWork.Spec.Workload.Manifests = append(createdWork.Spec.Workload.Manifests, addedManifestDetails[0].Manifest, addedManifestDetails[1].Manifest)
				createdWork, err = updateWork(createdWork, HubCluster)

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

			err = createWork(workObj, HubCluster)
			Expect(err).ToNot(HaveOccurred())

			createdWork, err = retrieveWork(workObj.Namespace, workObj.Name, HubCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err = deleteWorkResource(createdWork, HubCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reapply the manifest's updated spec on the spoke cluster", func() {
			By("retrieving the existing work and modifying the manifest")
			Eventually(func() error {
				createdWork, err = retrieveWork(createdWork.Namespace, createdWork.Name, HubCluster)

				// Extract and modify the ConfigMap by adding a new key value pair.
				err = json.Unmarshal(createdWork.Spec.Workload.Manifests[0].Raw, &configMap)
				configMap.Data[newDataKey] = newDataValue
				rawUpdatedManifest, _ := json.Marshal(configMap)
				obj, _, _ := genericCodec.Decode(rawUpdatedManifest, nil, nil)
				createdWork.Spec.Workload.Manifests[0].Object = obj
				createdWork.Spec.Workload.Manifests[0].Raw = rawUpdatedManifest
				_, err = updateWork(createdWork, HubCluster)
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

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

			err = createWork(workObj, HubCluster)
			Expect(err).ToNot(HaveOccurred())

			createdWork, err = retrieveWork(workObj.Namespace, workObj.Name, HubCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err = deleteWorkResource(createdWork, HubCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should have deleted the original Work's resources, and created new resources with the replaced manifests", func() {
			By("getting the respective AppliedWork")
			Eventually(func() int {
				appliedWork, _ = retrieveAppliedWork(createdWork.Name, MemberCluster)

				return len(appliedWork.Status.AppliedResources)
			}, eventuallyTimeout, eventuallyInterval).Should(Equal(len(originalManifestDetails)))

			By("updating the Work resource with replaced manifests")
			Eventually(func() error {
				createdWork, err = retrieveWork(createdWork.Namespace, createdWork.Name, HubCluster)
				createdWork.Spec.Workload.Manifests = nil
				for _, mD := range replacedManifestDetails {
					createdWork.Spec.Workload.Manifests = append(createdWork.Spec.Workload.Manifests, mD.Manifest)
				}

				createdWork, err = updateWork(createdWork, HubCluster)

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

			err = createWork(workObj, HubCluster)
			Expect(err).ToNot(HaveOccurred())

			createdWork, err = retrieveWork(workObj.Namespace, workObj.Name, HubCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should delete the Work and verify the resource has been garbage collected", func() {
			By("verifying the manifest was applied")
			Eventually(func() error {
				_, err = MemberCluster.KubeClientSet.CoreV1().Secrets(manifestDetails[0].ObjMeta.Namespace).Get(context.Background(), manifestDetails[0].ObjMeta.Name, metav1.GetOptions{})

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("deleting the Work resource")
			err = deleteWorkResource(createdWork, HubCluster)
			Expect(err).ToNot(HaveOccurred())

			By("verifying the resource was garbage collected")
			Eventually(func() error {
				err = MemberCluster.KubeClientSet.CoreV1().Secrets(manifestDetails[0].ObjMeta.Namespace).Delete(context.Background(), manifestDetails[0].ObjMeta.Name, metav1.DeleteOptions{})

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})
	})

	AfterAll(func() {
		err := HubCluster.KubeClient.Delete(context.Background(), wns)
		Expect(err).ToNot(HaveOccurred())
	})
})

func generateManifestDetails(manifestFiles []string) []manifestDetails {
	details := make([]manifestDetails, 0, len(manifestFiles))

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
