/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package integration

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	omegatypes "github.com/onsi/gomega/types"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	testv1alpha1 "go.goms.io/fleet/test/apis/v1alpha1"
)

const (
	timeout  = time.Second * 10
	interval = time.Millisecond * 250
)

var (
	cfg          *rest.Config
	k8sClient    client.Client
	testEnv      *envtest.Environment
	ctx          context.Context
	cancel       context.CancelFunc
	genericCodec runtime.Decoder

	// pre loaded test manifests
	testClusterRole rbacv1.ClusterRole
	testNameSpace   corev1.Namespace
	testResourceCRD apiextensionsv1.CustomResourceDefinition
	testResource    testv1alpha1.TestResource
	testConfigMap   corev1.ConfigMap
	testSecret      corev1.Secret
	testService     corev1.Service
	testPdb         policyv1.PodDisruptionBudget
)

// applyTestManifests creates the test manifests in the hub cluster.
// Here is the list, please do NOT change this list unless you know what you are doing.
// ClusterScoped resource:
// TestResource CRD, ClusterRole, Namespace
// Namespaced resources:
// TestResource CR, Pdb, Configmap, Secret, Service.
func applyTestManifests() {
	By("Create testResource CRD")
	err := utils.GetObjectFromManifest("../manifests/test_testresources_crd.yaml", &testResourceCRD)
	Expect(err).Should(Succeed())
	Expect(k8sClient.Create(ctx, &testResourceCRD)).Should(Succeed())

	// TODO: replace the rest objects with programmatic definition
	By("Create testClusterRole resource")
	err = utils.GetObjectFromManifest("manifests/resources/test_clusterrole.yaml", &testClusterRole)
	Expect(err).Should(Succeed())
	Expect(k8sClient.Create(ctx, &testClusterRole)).Should(Succeed())

	By("Create namespace")
	err = utils.GetObjectFromManifest("manifests/resources/test_namespace.yaml", &testNameSpace)
	Expect(err).Should(Succeed())
	Expect(k8sClient.Create(ctx, &testNameSpace)).Should(Succeed())

	By("Create PodDisruptionBudget")
	err = utils.GetObjectFromManifest("manifests/resources/test_pdb.yaml", &testPdb)
	Expect(err).Should(Succeed())
	Expect(k8sClient.Create(ctx, &testPdb)).Should(Succeed())

	By("Create the testConfigMap resources")
	err = utils.GetObjectFromManifest("manifests/resources/test-configmap.yaml", &testConfigMap)
	Expect(err).Should(Succeed())
	Expect(k8sClient.Create(ctx, &testConfigMap)).Should(Succeed())

	By("Create testSecret resource")
	err = utils.GetObjectFromManifest("manifests/resources/test-secret.yaml", &testSecret)
	Expect(err).Should(Succeed())
	Expect(k8sClient.Create(ctx, &testSecret)).Should(Succeed())

	By("Create testService resource")
	err = utils.GetObjectFromManifest("manifests/resources/test-service.yaml", &testService)
	Expect(err).Should(Succeed())
	Expect(k8sClient.Create(ctx, &testService)).Should(Succeed())

	By("Create testResource resource")
	err = utils.GetObjectFromManifest("../manifests/test-resource.yaml", &testResource)
	Expect(err).Should(Succeed())
	Expect(k8sClient.Create(ctx, &testResource)).Should(Succeed())
}

func deleteTestManifests() {
	// check that the manifest is clean
	By("Delete testClusterRole resource")
	Expect(k8sClient.Delete(ctx, &testClusterRole)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))

	By("Delete PodDisruptionBudget")
	Expect(k8sClient.Delete(ctx, &testPdb)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))

	By("Delete the testConfigMap resources")
	Expect(k8sClient.Delete(ctx, &testConfigMap)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))

	By("Delete testSecret resource")
	Expect(k8sClient.Delete(ctx, &testSecret)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))

	By("Delete testService resource")
	Expect(k8sClient.Delete(ctx, &testService)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))

	By("Delete testResource resource")
	Expect(k8sClient.Delete(ctx, &testResource)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))

	By("Delete testResource CRD")
	Expect(k8sClient.Delete(ctx, &testResourceCRD)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))

	// delete the namespace the last as there is no GC
	By("Delete namespace")
	Expect(k8sClient.Delete(ctx, &testNameSpace)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
}

// verifyManifest verify the manifest we get from the work is the same as one of the test manifest
func verifyManifest(manifest unstructured.Unstructured) {
	// check that the manifest is clean
	Expect(manifest.GetGeneration()).Should(BeEquivalentTo(0))
	Expect(manifest.GetOwnerReferences()).Should(BeNil())
	Expect(manifest.GetUID()).Should(BeEmpty())
	Expect(manifest.GetFinalizers()).Should(BeNil())
	Expect(manifest.GetResourceVersion()).Should(BeEmpty())
	// compare with the original
	switch manifest.GetObjectKind().GroupVersionKind().Kind {
	case "CustomResourceDefinition":
		var workTestResource apiextensionsv1.CustomResourceDefinition
		Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(manifest.Object, &workTestResource)).Should(Succeed())
		Expect(workTestResource.GetName()).Should(Equal(testResourceCRD.Name))
		Expect(workTestResource.Spec.Versions[0]).Should(Equal(testResourceCRD.Spec.Versions[0]))

	case "ClusterRole":
		var workClusterRole rbacv1.ClusterRole
		Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(manifest.Object, &workClusterRole)).Should(Succeed())
		Expect(workClusterRole.GetName()).Should(Equal(testClusterRole.GetName()))
		Expect(workClusterRole.Rules).Should(Equal(testClusterRole.Rules))

	case "Namespace":
		var workNameSpace corev1.Namespace
		Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(manifest.Object, &workNameSpace)).Should(Succeed())
		Expect(workNameSpace.GetName()).Should(Equal(testNameSpace.GetName()))

	case "TestResource":
		var workTestResource testv1alpha1.TestResource
		Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(manifest.Object, &workTestResource)).Should(Succeed())
		Expect(workTestResource.GetName()).Should(Equal(workTestResource.GetName()))
		Expect(workTestResource.Spec).Should(Equal(workTestResource.Spec))

	case "ConfigMap":
		var workConfigMap corev1.ConfigMap
		Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(manifest.Object, &workConfigMap)).Should(Succeed())
		Expect(workConfigMap.GetName()).Should(Equal(testConfigMap.GetName()))
		Expect(workConfigMap.Data).Should(Equal(testConfigMap.Data))

	case "Secret":
		var workSecret corev1.Secret
		Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(manifest.Object, &workSecret)).Should(Succeed())
		Expect(workSecret.GetName()).Should(Equal(testSecret.GetName()))
		Expect(workSecret.Data).Should(Equal(testSecret.Data))
		Expect(workSecret.Type).Should(Equal(testSecret.Type))

	case "PodDisruptionBudget":
		var workPdb policyv1.PodDisruptionBudget
		Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(manifest.Object, &workPdb)).Should(Succeed())
		Expect(workPdb.GetName()).Should(Equal(testPdb.GetName()))
		Expect(workPdb.Spec).Should(Equal(testPdb.Spec))

	case "Service":
		var workService corev1.Service
		Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(manifest.Object, &workService)).Should(Succeed())
		Expect(workService.GetName()).Should(Equal(testService.GetName()))
		Expect(workService.Spec.Type).Should(Equal(testService.Spec.Type))
		Expect(workService.Spec.Selector).Should(Equal(testService.Spec.Selector))
		Expect(workService.Spec.Ports).Should(Equal(testService.Spec.Ports))

	default:
		// always fail
		Expect(manifest.Object).Should(BeNil())
	}
}

func waitForPlacementScheduled(crpName string) (lastTransitionTime metav1.Time) {
	var crp fleetv1alpha1.ClusterResourcePlacement
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp)
		if err != nil {
			return false
		}
		scheduledCondition := crp.GetCondition(string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled))
		if scheduledCondition == nil {
			return false
		}
		if scheduledCondition.Status == metav1.ConditionUnknown {
			return false
		}
		lastTransitionTime = scheduledCondition.LastTransitionTime
		return true
	}, timeout, interval).Should(BeTrue())
	return
}

func waitForPlacementScheduleStopped(crpName string) {
	Eventually(func() bool {
		firstTransitionTime := waitForPlacementScheduled(crpName)
		// TODO: find a better way to make sure the reconcile loop has stopped
		time.Sleep(time.Second)
		secondTransitionTime := waitForPlacementScheduled(crpName)
		return secondTransitionTime.Equal(&firstTransitionTime)
	}, timeout, interval).Should(BeTrue())
}

// verifyWorkObjects verifies that we have created work objects that contain the resource listed in the expectedKind
// on the clusters
func verifyWorkObjects(crp *fleetv1alpha1.ClusterResourcePlacement, expectedKinds []string, clusters []*fleetv1alpha1.MemberCluster) time.Time {
	return verifyPartialWorkObjects(crp, expectedKinds, len(expectedKinds), clusters)
}

func verifyPartialWorkObjects(crp *fleetv1alpha1.ClusterResourcePlacement, expectedKinds []string, expectedLength int, clusters []*fleetv1alpha1.MemberCluster) time.Time {
	waitForPlacementScheduleStopped(crp.Name)
	By("ClusterResourcePlacement finished the resource scheduling")

	// build the array of expected kinds
	expectedKindMatchers := make([]omegatypes.GomegaMatcher, len(expectedKinds))
	for i, kind := range expectedKinds {
		expectedKindMatchers[i] = Equal(kind)
	}
	preciseMatch := len(expectedKinds) == expectedLength

	var clusterWork workv1alpha1.Work
	// check each work object in the cluster namespace
	for _, cluster := range clusters {
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      crp.Name,
			Namespace: fmt.Sprintf(utils.NamespaceNameFormat, cluster.Name),
		}, &clusterWork)).Should(Succeed())
		Expect(clusterWork.GetLabels()[utils.LabelWorkPlacementName]).Should(Equal(crp.Name))
		By(fmt.Sprintf("validate work resource for cluster %s. It should contain %d manifests", cluster.Name, expectedLength))
		Expect(len(clusterWork.Spec.Workload.Manifests)).Should(BeIdenticalTo(expectedLength))
		for i, manifest := range clusterWork.Spec.Workload.Manifests {
			By(fmt.Sprintf("validate the %d uObj in the work resource in cluster %s", i, cluster.Name))
			var uObj unstructured.Unstructured
			err := utils.GetObjectFromRawExtension(manifest.Raw, &uObj)
			Expect(err).Should(Succeed())
			kind := uObj.GroupVersionKind().Kind
			if preciseMatch {
				Expect(kind).Should(SatisfyAny(expectedKindMatchers...))
				verifyManifest(uObj)
			} else if slices.Contains(expectedKinds, kind) {
				verifyManifest(uObj)
			}
		}
	}
	lastUpdateTime, err := time.Parse(time.RFC3339, clusterWork.GetAnnotations()[utils.LastWorkUpdateTimeAnnotationKey])
	Expect(err).Should(Succeed())
	return lastUpdateTime
}

func markInternalMCLeft(mc fleetv1alpha1.MemberCluster) {
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mc.Name}, &mc)).Should(Succeed())
	mc.Spec.State = fleetv1alpha1.ClusterStateLeave
	Expect(k8sClient.Update(ctx, &mc)).Should(Succeed())
	var imc fleetv1alpha1.InternalMemberCluster
	nsName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	By("Mark internal member cluster as Left")
	Eventually(func() error {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: mc.Name, Namespace: nsName}, &imc)
		if err != nil {
			return err
		}
		imc.SetConditionsWithType(fleetv1alpha1.MemberAgent, metav1.Condition{
			Type:               string(fleetv1alpha1.AgentJoined),
			Status:             metav1.ConditionFalse,
			Reason:             "FakeLeave",
			ObservedGeneration: imc.GetGeneration(),
		})
		return k8sClient.Status().Update(ctx, &imc)
	}, timeout, interval).Should(Succeed())
	By("Marked internal member cluster as Left")
	Eventually(func() bool {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mc.Name}, &mc)).Should(Succeed())
		joinCond := mc.GetCondition(string(fleetv1alpha1.ConditionTypeMemberClusterJoined))
		return joinCond.Status == metav1.ConditionFalse
	}, timeout, interval).Should(BeTrue())
	By("Member cluster is marked as Left")
}

func markInternalMCJoined(mc fleetv1alpha1.MemberCluster) {
	var imc fleetv1alpha1.InternalMemberCluster
	nsName := fmt.Sprintf(utils.NamespaceNameFormat, mc.Name)
	By("Wait for internal member cluster to be created")
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: mc.Name, Namespace: nsName}, &imc)
	}, timeout, interval).Should(Succeed())
	imc.SetConditionsWithType(fleetv1alpha1.MemberAgent, metav1.Condition{
		Type:               string(fleetv1alpha1.AgentJoined),
		Status:             metav1.ConditionTrue,
		Reason:             "FakeJoin",
		ObservedGeneration: imc.GetGeneration(),
	})
	Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())
	By("Marked internal member cluster as joined")
	Eventually(func() bool {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mc.Name}, &mc)).Should(Succeed())
		joinCond := mc.GetCondition(string(fleetv1alpha1.ConditionTypeMemberClusterJoined))
		if joinCond == nil {
			return false
		}
		return joinCond.Status == metav1.ConditionTrue
	}, timeout, interval).Should(BeTrue())
	By("Member cluster is marked as Join")
}

func markWorkAppliedStatusSuccess(crp *fleetv1alpha1.ClusterResourcePlacement, cluster *fleetv1alpha1.MemberCluster) {
	var clusterWork workv1alpha1.Work
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Name:      crp.Name,
		Namespace: fmt.Sprintf(utils.NamespaceNameFormat, cluster.Name),
	}, &clusterWork)).Should(Succeed())
	clusterWork.Status.Conditions = []metav1.Condition{
		{
			Type:               "Applied",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "appliedWorkComplete",
			Message:            "Apply work complete",
			ObservedGeneration: crp.Generation,
		},
	}
	controllerutil.AddFinalizer(&clusterWork, "fleet.azure.com/work-cleanup")
	Expect(k8sClient.Status().Update(ctx, &clusterWork)).Should(Succeed())
}

func verifyPlacementScheduleStatus(crp *fleetv1alpha1.ClusterResourcePlacement, selectedResourceCount, targetClusterCount int, scheduleStatus metav1.ConditionStatus) {
	status := crp.Status
	Expect(len(status.SelectedResources)).Should(Equal(selectedResourceCount))
	Expect(len(status.TargetClusters)).Should(Equal(targetClusterCount))
	Expect(len(status.FailedResourcePlacements)).Should(Equal(0))
	schedCond := crp.GetCondition(string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled))
	Expect(schedCond).ShouldNot(BeNil())
	Expect(schedCond.Status).Should(Equal(scheduleStatus))
}

func verifyPlacementApplyStatus(crp *fleetv1alpha1.ClusterResourcePlacement, applyStatus metav1.ConditionStatus, applyReason string) {
	applyCond := crp.GetCondition(string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied))
	Expect(applyCond).ShouldNot(BeNil())
	Expect(applyCond.Status == applyStatus).Should(BeTrue())
	Expect(applyCond.Reason == applyReason).Should(BeTrue())
}
