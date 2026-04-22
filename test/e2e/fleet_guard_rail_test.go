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
package e2e

import (
	"encoding/json"
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/webhook/validation"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	testUser     = "test-user"
	testIdentity = "test-identity"
	testKey      = "test-key"
	testValue    = "test-value"
	testReason   = "testReason"
)

var (
	mcGVK      = metav1.GroupVersionKind{Group: clusterv1beta1.GroupVersion.Group, Version: clusterv1beta1.GroupVersion.Version, Kind: "MemberCluster"}
	imcGVK     = metav1.GroupVersionKind{Group: clusterv1beta1.GroupVersion.Group, Version: clusterv1beta1.GroupVersion.Version, Kind: "InternalMemberCluster"}
	workGVK    = metav1.GroupVersionKind{Group: placementv1beta1.GroupVersion.Group, Version: placementv1beta1.GroupVersion.Version, Kind: "Work"}
	iseGVK     = metav1.GroupVersionKind{Group: fleetnetworkingv1alpha1.GroupVersion.Group, Version: fleetnetworkingv1alpha1.GroupVersion.Version, Kind: "InternalServiceExport"}
	testGroups = []string{"system:authenticated"}
)

var _ = Describe("fleet guard rail tests for deny fleet MC CREATE operations", func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())

	It("should deny CREATE operation on fleet member cluster CR for user not in system:masters group", func() {
		mc := &clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        mcName,
				Annotations: map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1},
			},
			Spec: clusterv1beta1.MemberClusterSpec{
				Identity: rbacv1.Subject{
					Name:      testUser,
					Kind:      "ServiceAccount",
					Namespace: utils.FleetSystemNamespace,
				},
				HeartbeatPeriodSeconds: 60,
			},
		}

		By(fmt.Sprintf("expecting denial of operation CREATE of fleet member cluster %s", mc.Name))
		Expect(checkIfStatusErrorWithMessage(notMasterUser.Create(ctx, mc), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &mcGVK, "", types.NamespacedName{Name: mc.Name}))).Should(Succeed())
	})
})

var _ = Describe("fleet guard rail tests for allow/deny fleet MC UPDATE, DELETE operations", Serial, Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		createMemberCluster(mcName, testUser, nil, map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
	})

	AfterAll(func() {
		ensureMemberClusterAndRelatedResourcesDeletion(mcName)
	})

	It("should deny update operation on fleet member cluster CR, fleet prefixed annotation for user not in system:masters group", func() {
		Eventually(func(g Gomega) error {
			var mc clusterv1beta1.MemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			By(fmt.Sprintf("update fleet member cluster, cluster id annotation %s", mc.Name))
			if len(mc.Annotations) == 0 {
				return errors.New("annotations are empty")
			}
			mc.Annotations[fleetClusterResourceIDAnnotationKey] = clusterID2
			err = notMasterUser.Update(ctx, &mc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &mcGVK, "", types.NamespacedName{Name: mc.Name}))
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())

		Eventually(func(g Gomega) error {
			var mc clusterv1beta1.MemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			By(fmt.Sprintf("remove fleet member cluster, cluster id annotation %s", mc.Name))
			mc.SetAnnotations(nil)
			err = notMasterUser.Update(ctx, &mc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			return checkIfStatusErrorWithMessage(err, "no user is allowed to remove all fleet pre-fixed annotations from a fleet member cluster")
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny update operation on fleet member cluster CR remove fleet prefixed annotation for user in system:masters group", func() {
		Eventually(func(g Gomega) error {
			var mc clusterv1beta1.MemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			By(fmt.Sprintf("remove fleet member cluster, cluster id annotation %s", mc.Name))
			mc.SetAnnotations(nil)
			err = hubClient.Update(ctx, &mc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			return checkIfStatusErrorWithMessage(err, "no user is allowed to remove all fleet pre-fixed annotations from a fleet member cluster")
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny UPDATE operation on fleet member cluster CR spec for user not in systems:masters group", func() {
		Eventually(func(g Gomega) error {
			var mc clusterv1beta1.MemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			By(fmt.Sprintf("update fleet member cluster spec for MC %s", mc.Name))
			mc.Spec.HeartbeatPeriodSeconds = 30
			By(fmt.Sprintf("expecting denial of operation UPDATE of fleet member cluster %s", mc.Name))
			err = notMasterUser.Update(ctx, &mc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &mcGVK, "", types.NamespacedName{Name: mc.Name}))
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny update operation on fleet member cluster CR status for user not in system:masters group", func() {
		Eventually(func(g Gomega) error {
			var mc clusterv1beta1.MemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			By(fmt.Sprintf("update fleet member cluster status for MC %s", mc.Name))
			if len(mc.Status.Conditions) == 0 {
				return errors.New("status conditions are empty")
			}
			mc.Status.Conditions[0].Reason = testReason
			err = notMasterUser.Status().Update(ctx, &mc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &mcGVK, "status", types.NamespacedName{Name: mc.Name}))
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny DELETE operation on fleet member cluster CR for user not in system:masters group", func() {
		mc := clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcName,
			},
		}
		By(fmt.Sprintf("expecting denial of operation DELETE of fleet member cluster %s", mc.Name))
		Expect(checkIfStatusErrorWithMessage(notMasterUser.Delete(ctx, &mc), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &mcGVK, "", types.NamespacedName{Name: mc.Name}))).Should(Succeed())
	})

	It("should allow update operation on fleet member cluster CR labels for non system user", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update labels in fleet member cluster %s, expecting successful UPDATE of fleet member cluster", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			mc.SetLabels(map[string]string{testKey: testValue})
			return notMasterUser.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on fleet member cluster CR annotations except fleet pre-fixed annotation for non system user", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update annotations in fleet member cluster %s, expecting successful UPDATE of fleet member cluster", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			annotations := mc.GetAnnotations()
			if len(annotations) == 0 {
				return errors.New("annotations are empty")
			}
			annotations[testKey] = testValue
			mc.SetAnnotations(annotations)
			return notMasterUser.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on fleet member cluster CR to modify fleet pre-fixed annotation value for user in system:masters group", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update fleet member cluster, remove cluster id annotation %s", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			annotations := mc.GetAnnotations()
			_, exists := annotations[fleetClusterResourceIDAnnotationKey]
			if !exists {
				return errors.New("fleet-prefixed cluster resource id annotation not found")
			}
			annotations[fleetClusterResourceIDAnnotationKey] = clusterID2
			mc.SetAnnotations(annotations)
			return hubClient.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on fleet member cluster CR to add a new fleet pre-fixed annotation and remove it for user in system:masters group", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update fleet member cluster, remove cluster id annotation %s", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			annotations := mc.GetAnnotations()
			if len(annotations) == 0 {
				return errors.New("annotations are empty")
			}
			annotations[fleetLocationAnnotationKey] = "test-location"
			mc.SetAnnotations(annotations)
			return hubClient.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())

		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			annotations := mc.GetAnnotations()
			if len(annotations) == 0 {
				return errors.New("annotations are empty")
			}
			mc.SetAnnotations(map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
			return hubClient.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on fleet member cluster CR taints for non system user", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update taints in fleet member cluster %s, expecting successful UPDATE of fleet member cluster", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			taint := clusterv1beta1.Taint{
				Key:    "key1",
				Value:  "value1",
				Effect: corev1.TaintEffectNoSchedule,
			}
			mc.Spec.Taints = append(mc.Spec.Taints, taint)
			return notMasterUser.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on fleet member cluster CR spec for user in system:masters group", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update spec of fleet member cluster %s, expecting successful UPDATE of fleet member cluster", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			mc.Spec.HeartbeatPeriodSeconds = 31
			return hubClient.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on fleet member cluster CR status for user in system:masters group", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update status of fleet member cluster %s, expecting successful UPDATE of fleet member cluster", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			if len(mc.Status.Conditions) == 0 {
				return errors.New("status conditions are empty")
			}
			mc.Status.Conditions[0].Reason = testReason
			return hubClient.Status().Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})
})

var _ = Describe("fleet guard rail tests for allow upstream MC CREATE, DELETE operations", func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())

	It("should allow CREATE, DELETE operation on upstream member cluster CR for user not in system:masters group", func() {
		mc := &clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcName,
			},
			Spec: clusterv1beta1.MemberClusterSpec{
				Identity: rbacv1.Subject{
					Name:      testUser,
					Kind:      "ServiceAccount",
					Namespace: utils.FleetSystemNamespace,
				},
				HeartbeatPeriodSeconds: 60,
			},
		}
		Expect(notMasterUser.Create(ctx, mc)).Should(Succeed())
		Expect(notMasterUser.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)).Should(Succeed())
		Expect(notMasterUser.Delete(ctx, mc)).Should(Succeed())
		ensureMemberClusterAndRelatedResourcesDeletion(mcName)
	})
})

var _ = Describe("fleet guard rail tests for allow/deny upstream MC UPDATE operations", Serial, Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		createMemberCluster(mcName, testUser, nil, nil)
	})

	AfterAll(func() {
		ensureMemberClusterAndRelatedResourcesDeletion(mcName)
	})

	It("should deny update operation on upstream member cluster CR add fleet pre-fixed annotation for user not in system:masters group", func() {
		Eventually(func(g Gomega) error {
			var mc clusterv1beta1.MemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			By(fmt.Sprintf("add fleet member cluster, cluster id annotation %s", mc.Name))
			mc.SetAnnotations(map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
			err = notMasterUser.Update(ctx, &mc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			return checkIfStatusErrorWithMessage(err, "no user is allowed to add a fleet pre-fixed annotation to an upstream member cluster")
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny update operation on upstream member cluster CR add fleet pre-fixed annotation for user in system:masters group", func() {
		Eventually(func(g Gomega) error {
			var mc clusterv1beta1.MemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			By(fmt.Sprintf("add fleet member cluster, cluster id annotation %s", mc.Name))
			mc.SetAnnotations(map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
			err = hubClient.Update(ctx, &mc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			return checkIfStatusErrorWithMessage(err, "no user is allowed to add a fleet pre-fixed annotation to an upstream member cluster")
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny update operation on upstream member cluster CR status for user not in system:masters group", func() {
		Eventually(func(g Gomega) error {
			var mc clusterv1beta1.MemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			By(fmt.Sprintf("update upstream member cluster status for MC %s", mc.Name))
			if len(mc.Status.Conditions) == 0 {
				return errors.New("status conditions are empty")
			}
			mc.Status.Conditions[0].Reason = testReason
			err = notMasterUser.Status().Update(ctx, &mc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &mcGVK, "status", types.NamespacedName{Name: mc.Name}))
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on upstream member cluster CR labels for non system user", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update labels in upstream member cluster %s, expecting successful UPDATE of upstream member cluster", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			mc.SetLabels(map[string]string{testKey: testValue})
			return notMasterUser.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on upstream member cluster CR annotations without add fleet-prefixed annotation for non system user", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update annotations in upstream member cluster %s, expecting successful UPDATE of upstream member cluster", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			mc.SetAnnotations(map[string]string{testKey: testValue})
			return notMasterUser.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on upstream member cluster CR taints for non system user", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update taints in upstream member cluster %s, expecting successful UPDATE of upstream member cluster", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			taint := clusterv1beta1.Taint{
				Key:    "key1",
				Value:  "value1",
				Effect: corev1.TaintEffectNoSchedule,
			}
			mc.Spec.Taints = append(mc.Spec.Taints, taint)
			return notMasterUser.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on upstream member cluster CR spec for user in system:masters group", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update spec of upstream member cluster %s, expecting successful UPDATE of upstream member cluster", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			mc.Spec.HeartbeatPeriodSeconds = 31
			return hubClient.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow UPDATE operation on upstream member cluster CR spec for user not in systems:masters group", func() {
		Eventually(func(g Gomega) error {
			var mc clusterv1beta1.MemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			By(fmt.Sprintf("update upstream member cluster spec for MC %s", mc.Name))
			mc.Spec.HeartbeatPeriodSeconds = 32
			By(fmt.Sprintf("expecting denial of operation UPDATE of upstream member cluster %s", mc.Name))
			return notMasterUser.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on upstream member cluster CR status for user in system:masters group", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update status of upstream member cluster %s, expecting successful UPDATE of upstream member cluster", mcName))
		Eventually(func(g Gomega) error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)
			if err != nil {
				return err
			}
			if len(mc.Status.Conditions) == 0 {
				return errors.New("status conditions are empty")
			}
			mc.Status.Conditions[0].Reason = testReason
			return hubClient.Status().Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})
})

var _ = Describe("fleet guard rail tests for IMC UPDATE operation, in fleet-member prefixed namespace with user not in MC identity", Serial, Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
	imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)

	BeforeAll(func() {
		createMemberCluster(mcName, testIdentity, nil, map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
		checkInternalMemberClusterExists(mcName, imcNamespace)
	})

	AfterAll(func() {
		ensureMemberClusterAndRelatedResourcesDeletion(mcName)
	})

	It("should deny CREATE operation on internal member cluster CR for user not in MC identity in fleet member namespace", func() {
		imc := clusterv1beta1.InternalMemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcName,
				Namespace: imcNamespace,
			},
			Spec: clusterv1beta1.InternalMemberClusterSpec{
				State:                  clusterv1beta1.ClusterStateJoin,
				HeartbeatPeriodSeconds: 30,
			},
		}
		By(fmt.Sprintf("expecting denial of operation CREATE of Internal Member Cluster %s/%s", mcName, imcNamespace))
		Expect(checkIfStatusErrorWithMessage(notMasterUser.Create(ctx, &imc), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}))).Should(Succeed())
	})

	It("should deny UPDATE operation on internal member cluster CR for user not in MC identity in fleet member namespace", func() {
		Eventually(func(g Gomega) error {
			var imc clusterv1beta1.InternalMemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)
			if err != nil {
				return err
			}
			imc.Spec.HeartbeatPeriodSeconds = 25
			By("expecting denial of operation UPDATE of Internal Member Cluster")
			err = notMasterUser.Update(ctx, &imc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}))
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny DELETE operation on internal member cluster CR for user not in MC identity in fleet member namespace", func() {
		var imc clusterv1beta1.InternalMemberCluster
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())
		By("expecting denial of operation DELETE of Internal Member Cluster")
		Expect(checkIfStatusErrorWithMessage(notMasterUser.Delete(ctx, &imc), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}))).Should(Succeed())
	})

	It("should deny UPDATE operation on internal member cluster CR status for user not in MC identity in fleet member namespace", func() {
		Eventually(func(g Gomega) error {
			var imc clusterv1beta1.InternalMemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)
			if err != nil {
				return err
			}
			imc.Status = clusterv1beta1.InternalMemberClusterStatus{
				ResourceUsage: clusterv1beta1.ResourceUsage{
					Capacity: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceCPU: {
							Format: "testFormat",
						},
					},
				},
				AgentStatus: nil,
			}
			By("expecting denial of operation UPDATE of internal member cluster CR status")
			err = notMasterUser.Status().Update(ctx, &imc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString(testGroups), admissionv1.Update, &imcGVK, "status", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}))
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})
})

var _ = Describe("fleet guard rail tests for IMC UPDATE operation, in fleet-member prefixed namespace with user in MC identity", Serial, Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
	imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)

	BeforeAll(func() {
		createMemberCluster(mcName, testUser, nil, map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
		checkInternalMemberClusterExists(mcName, imcNamespace)
	})

	AfterAll(func() {
		ensureMemberClusterAndRelatedResourcesDeletion(mcName)
	})

	It("should allow UPDATE operation on internal member cluster CR status for user in MC identity", func() {
		Eventually(func(g Gomega) error {
			var imc clusterv1beta1.InternalMemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)
			if err != nil {
				return err
			}
			imc.Status = clusterv1beta1.InternalMemberClusterStatus{
				ResourceUsage: clusterv1beta1.ResourceUsage{
					Capacity: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceCPU: {
							Format: "testFormat",
						},
					},
				},
				AgentStatus: nil,
			}
			By("expecting successful UPDATE of Internal Member Cluster Status")
			return notMasterUser.Status().Update(ctx, &imc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow UPDATE operation on internal member cluster CR for user in MC identity", func() {
		Eventually(func(g Gomega) error {
			var imc clusterv1beta1.InternalMemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)
			if err != nil {
				return err
			}
			imc.Labels = map[string]string{testKey: testValue}
			By("expecting successful UPDATE of Internal Member Cluster Status")
			return notMasterUser.Update(ctx, &imc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow UPDATE operation on internal member cluster CR spec for user in system:masters group", func() {
		Eventually(func(g Gomega) error {
			var imc clusterv1beta1.InternalMemberCluster
			err := hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)
			if err != nil {
				return err
			}
			imc.Spec.HeartbeatPeriodSeconds = 25
			By("expecting successful UPDATE of Internal Member Cluster Spec")
			return hubClient.Update(ctx, &imc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})
})

var _ = Describe("fleet guard rail for UPDATE work operations, in fleet prefixed namespace with user not in MC identity", Serial, Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
	imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
	workName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		createMemberCluster(mcName, testIdentity, nil, map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
		checkInternalMemberClusterExists(mcName, imcNamespace)
		createWorkResource(workName, imcNamespace)
	})

	AfterAll(func() {
		ensureMemberClusterAndRelatedResourcesDeletion(mcName)
	})

	It("should deny CREATE operation on work CR for user not in MC identity in fleet member namespace", func() {
		testDeployment := appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "Deployment",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: utilrand.String(10),
						Kind:       utilrand.String(10),
						Name:       utilrand.String(10),
						UID:        types.UID(utilrand.String(10)),
					},
				},
			},
		}
		deploymentBytes, err := json.Marshal(testDeployment)
		Expect(err).Should(Succeed())
		w := placementv1beta1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Name:      workName,
				Namespace: imcNamespace,
			},
			Spec: placementv1beta1.WorkSpec{
				Workload: placementv1beta1.WorkloadTemplate{
					Manifests: []placementv1beta1.Manifest{
						{
							RawExtension: runtime.RawExtension{
								Raw: deploymentBytes,
							},
						},
					},
				},
			},
		}
		By(fmt.Sprintf("expecting denial of operation CREATE of Work %s/%s", workName, imcNamespace))
		Expect(checkIfStatusErrorWithMessage(notMasterUser.Create(ctx, &w), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &workGVK, "", types.NamespacedName{Name: w.Name, Namespace: w.Namespace}))).Should(Succeed())
	})

	It("should deny UPDATE operation on work CR status for user not in MC identity", func() {
		Eventually(func(g Gomega) error {
			var w placementv1beta1.Work
			err := hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: imcNamespace}, &w)
			if err != nil {
				return err
			}
			w.Status = placementv1beta1.WorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Applied",
						Status:             metav1.ConditionTrue,
						Reason:             "appliedWorkComplete",
						Message:            "Apply work complete",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			By("expecting denial of operation UPDATE of work CR status")
			err = notMasterUser.Status().Update(ctx, &w)
			if k8sErrors.IsConflict(err) {
				return err
			}
			return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &workGVK, "status", types.NamespacedName{Name: w.Name, Namespace: w.Namespace}))
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny DELETE work operation for user not in MC identity", func() {
		var w placementv1beta1.Work
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: imcNamespace}, &w)).Should(Succeed())
		By("expecting denial of operation DELETE of work")
		Expect(checkIfStatusErrorWithMessage(notMasterUser.Delete(ctx, &w), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &workGVK, "", types.NamespacedName{Name: w.Name, Namespace: w.Namespace}))).Should(Succeed())
	})
})

var _ = Describe("fleet guard rail for UPDATE work operations, in fleet prefixed namespace with user in MC identity", Serial, Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
	imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
	workName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		createMemberCluster(mcName, testUser, nil, map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
		checkInternalMemberClusterExists(mcName, imcNamespace)
		createWorkResource(workName, imcNamespace)
	})

	AfterAll(func() {
		ensureMemberClusterAndRelatedResourcesDeletion(mcName)
	})

	It("should allow UPDATE operation on work CR for user in MC identity", func() {
		Eventually(func(g Gomega) error {
			var w placementv1beta1.Work
			err := hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: imcNamespace}, &w)
			if err != nil {
				return err
			}
			w.SetLabels(map[string]string{testKey: testValue})
			By("expecting successful UPDATE of work")
			return notMasterUser.Update(ctx, &w)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow UPDATE operation on work CR status for user in MC identity", func() {
		Eventually(func(g Gomega) error {
			var w placementv1beta1.Work
			err := hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: imcNamespace}, &w)
			if err != nil {
				return err
			}
			w.Status = placementv1beta1.WorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Applied",
						Status:             metav1.ConditionTrue,
						Reason:             "appliedWorkComplete",
						Message:            "Apply work complete",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			By("expecting successful UPDATE of work Status")
			return notMasterUser.Status().Update(ctx, &w)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow UPDATE operation on work CR spec for user in system:masters group", func() {
		Eventually(func(g Gomega) error {
			var w placementv1beta1.Work
			err := hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: imcNamespace}, &w)
			if err != nil {
				return err
			}
			w.Spec.Workload.Manifests = []placementv1beta1.Manifest{}
			By("expecting successful UPDATE of work Spec")
			return hubClient.Update(ctx, &w)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})
})

var _ = Describe("fleet guard rail networking E2Es", Serial, Ordered, func() {
	Context("deny request to modify fleet networking resources in fleet member namespaces, for user not in member cluster identity", func() {
		mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
		iseName := fmt.Sprintf(internalServiceExportNameTemplate, GinkgoParallelProcess())
		imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
		var ise fleetnetworkingv1alpha1.InternalServiceExport

		BeforeEach(func() {
			createMemberCluster(mcName, "random-user", nil, map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
			checkInternalMemberClusterExists(mcName, imcNamespace)
			ise = internalServiceExport(iseName, imcNamespace)
			// can return no kind match error.
			Eventually(func(g Gomega) error {
				return hubClient.Create(ctx, &ise)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, &ise)).Should(Succeed(), "failed to delete Internal Service Export")

			ensureMemberClusterAndRelatedResourcesDeletion(mcName)
		})

		It("should deny update operation on Internal service export resource in fleet-member namespace for user not in member cluster identity", func() {
			Eventually(func(g Gomega) error {
				err := hubClient.Get(ctx, types.NamespacedName{Name: iseName, Namespace: imcNamespace}, &ise)
				if err != nil {
					return err
				}
				ise.SetLabels(map[string]string{testKey: testValue})
				By("expecting denial of operation UPDATE of Internal Service Export")
				err = notMasterUser.Update(ctx, &ise)
				if k8sErrors.IsConflict(err) {
					return err
				}
				return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &iseGVK, "", types.NamespacedName{Name: ise.Name, Namespace: ise.Namespace}))
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})

	Context("allow request to CREATE fleet networking resources in fleet member namespace, for user in member cluster identity", func() {
		mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
		iseName := fmt.Sprintf(internalServiceExportNameTemplate, GinkgoParallelProcess())
		isiName := fmt.Sprintf(internalServiceImportNameTemplate, GinkgoParallelProcess())
		epName := fmt.Sprintf(endpointSliceExportNameTemplate, GinkgoParallelProcess())
		imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
		BeforeEach(func() {
			createMemberCluster(mcName, "test-user", nil, map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
			checkInternalMemberClusterExists(mcName, imcNamespace)
		})

		AfterEach(func() {
			ensureMemberClusterAndRelatedResourcesDeletion(mcName)
		})

		It("should allow CREATE operation on Internal service export resource in fleet-member namespace for user in member cluster identity", func() {
			ise := internalServiceExport(iseName, imcNamespace)
			By("expecting successful CREATE of Internal Service Export")
			Expect(notMasterUser.Create(ctx, &ise)).Should(Succeed())

			By("deleting Internal Service Export")
			// Cleanup the internalServiceExport so that it won't block the member deletion.
			Expect(notMasterUser.Delete(ctx, &ise)).Should(Succeed(), "failed to delete Internal Service Export")
		})

		It("should allow CREATE operation on Internal service import resource in fleet-member namespace for user in member cluster identity", func() {
			isi := internalServiceImport(isiName, imcNamespace)
			By("expecting successful CREATE of Internal Service Import")
			Expect(notMasterUser.Create(ctx, &isi)).Should(Succeed())

			By("deleting Internal Service Import")
			Expect(notMasterUser.Delete(ctx, &isi)).Should(Succeed(), "failed to delete Internal Service Import")
		})

		It("should allow CREATE operation on Endpoint slice export resource in fleet-member namespace for user in member cluster identity", func() {
			esx := endpointSliceExport(epName, imcNamespace)
			By("expecting successful CREATE of Endpoint slice export")
			Expect(notMasterUser.Create(ctx, &esx)).Should(Succeed())

			By("deleting Endpoint Slice Export")
			Expect(notMasterUser.Delete(ctx, &esx)).Should(Succeed(), "failed to delete EndpointSlice Export")
		})
	})

	Context("allow request to modify network resources in fleet member namespaces, for user in member cluster identity", func() {
		mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
		iseName := fmt.Sprintf(internalServiceExportNameTemplate, GinkgoParallelProcess())
		imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
		var ise fleetnetworkingv1alpha1.InternalServiceExport

		BeforeEach(func() {
			createMemberCluster(mcName, testUser, nil, map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
			checkInternalMemberClusterExists(mcName, imcNamespace)
			ise = internalServiceExport(iseName, imcNamespace)
			// can return no kind match error.
			Eventually(func(g Gomega) error {
				return hubClient.Create(ctx, &ise)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, &ise)).Should(Succeed(), "failed to delete Internal Service Export")
			ensureMemberClusterAndRelatedResourcesDeletion(mcName)
		})

		It("should allow update operation on Internal service export resource in fleet-member namespace for user in member cluster identity", func() {
			Eventually(func(g Gomega) error {
				var ise fleetnetworkingv1alpha1.InternalServiceExport
				err := hubClient.Get(ctx, types.NamespacedName{Name: iseName, Namespace: imcNamespace}, &ise)
				if err != nil {
					return err
				}
				ise.SetLabels(map[string]string{testKey: testValue})
				By("expecting denial of operation UPDATE of Internal Service Export")
				return notMasterUser.Update(ctx, &ise)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})
})

var _ = Describe("fleet guard rail for pods and replicasets in fleet/kube namespaces", Serial, Ordered, func() {
	var (
		podGVK        = metav1.GroupVersionKind{Group: corev1.SchemeGroupVersion.Group, Version: corev1.SchemeGroupVersion.Version, Kind: "Pod"}
		replicaSetGVK = metav1.GroupVersionKind{Group: appsv1.SchemeGroupVersion.Group, Version: appsv1.SchemeGroupVersion.Version, Kind: "ReplicaSet"}
	)

	Context("deny pod operations in fleet-system namespace", func() {
		It("should deny CREATE operation on pod in fleet-system namespace for user not in system:masters", func() {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "fleet-system",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
						},
					},
				},
			}
			Expect(checkIfStatusErrorWithMessage(notMasterUser.Create(ctx, &pod), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &podGVK, "", types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}))).Should(Succeed())
		})

		It("should deny UPDATE operation on pod in fleet-system namespace for user not in system:masters", func() {
			// First create a pod as admin
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-update",
					Namespace: "fleet-system",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &pod)).Should(Succeed())

			// Try to update as non-admin
			Eventually(func(g Gomega) error {
				var p corev1.Pod
				err := hubClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, &p)
				if err != nil {
					return err
				}
				p.Labels = map[string]string{testKey: testValue}
				err = notMasterUser.Update(ctx, &p)
				if k8sErrors.IsConflict(err) {
					return err
				}
				return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &podGVK, "", types.NamespacedName{Name: p.Name, Namespace: p.Namespace}))
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())

			// Cleanup
			Expect(hubClient.Delete(ctx, &pod)).Should(Succeed())
		})
	})

	Context("deny replicaset operations in fleet-member namespace", func() {
		var (
			mcName       string
			imcNamespace string
		)

		BeforeAll(func() {
			mcName = fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
			imcNamespace = fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			createMemberCluster(mcName, testIdentity, nil, map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
			checkInternalMemberClusterExists(mcName, imcNamespace)
		})

		AfterAll(func() {
			ensureMemberClusterAndRelatedResourcesDeletion(mcName)
		})

		It("should deny CREATE operation on replicaset in fleet-member namespace for user not in MC identity", func() {
			rs := appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-replicaset",
					Namespace: imcNamespace,
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
			Expect(checkIfStatusErrorWithMessage(notMasterUser.Create(ctx, &rs), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &replicaSetGVK, "", types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}))).Should(Succeed())
		})

		It("should deny UPDATE operation on replicaset in fleet-member namespace for user not in MC identity", func() {
			// First create a replicaset as admin
			rs := appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-replicaset-update",
					Namespace: imcNamespace,
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &rs)).Should(Succeed())

			// Try to update as non-admin
			Eventually(func(g Gomega) error {
				var r appsv1.ReplicaSet
				err := hubClient.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, &r)
				if err != nil {
					return err
				}
				r.Labels = map[string]string{testKey: testValue}
				err = notMasterUser.Update(ctx, &r)
				if k8sErrors.IsConflict(err) {
					return err
				}
				return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &replicaSetGVK, "", types.NamespacedName{Name: r.Name, Namespace: r.Namespace}))
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())

			// Cleanup
			Expect(hubClient.Delete(ctx, &rs)).Should(Succeed())
		})

		It("should deny DELETE operation on pod in fleet-member namespace for user not in MC identity", func() {
			// First create a pod as admin
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-delete",
					Namespace: imcNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &pod)).Should(Succeed())

			// Try to delete as non-admin
			Eventually(func() error {
				var p corev1.Pod
				err := hubClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, &p)
				if err != nil {
					return err
				}
				err = notMasterUser.Delete(ctx, &p)
				return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &podGVK, "", types.NamespacedName{Name: p.Name, Namespace: p.Namespace}))
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())

			// Cleanup by admin
			Expect(hubClient.Delete(ctx, &pod)).Should(Succeed())
		})
	})

	Context("deny pod/replicaset operations in kube-system namespace", func() {
		It("should deny CREATE operation on pod in kube-system namespace for user not in system:masters", func() {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-kube",
					Namespace: "kube-system",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
						},
					},
				},
			}
			Expect(checkIfStatusErrorWithMessage(notMasterUser.Create(ctx, &pod), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &podGVK, "", types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}))).Should(Succeed())
		})

		It("should deny CREATE operation on replicaset in kube-system namespace for user not in system:masters", func() {
			rs := appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-replicaset-kube",
					Namespace: "kube-system",
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
			Expect(checkIfStatusErrorWithMessage(notMasterUser.Create(ctx, &rs), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &replicaSetGVK, "", types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}))).Should(Succeed())
		})
	})
})

// Note: even though the PDB webhook allows creation of PDBs in reserved namespaces, the guard rail webhook would still block
// such creation requests if they do not come from whitelisted users.
var _ = Describe("fleet guard rail webhook tests for PodDisruptionBudgets", Serial, Ordered, func() {
	var (
		pdbGVK = metav1.GroupVersionKind{Group: policyv1.SchemeGroupVersion.Group, Version: policyv1.SchemeGroupVersion.Version, Kind: "PodDisruptionBudget"}
	)

	Context("deny PDB operations in fleet-system namespace", func() {
		It("should deny CREATE operation on PDB in fleet-system namespace for non-whitelisted users", func() {
			Skip("PDB webhook is temporarily disabled.")

			pdb := policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pdb-fleet-system",
					Namespace: "fleet-system",
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: ptr.To(intstr.FromInt32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
				},
			}
			Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Create(ctx, &pdb), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &pdbGVK, "", types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}))).Should(Succeed())
		})
	})

	Context("deny PDB operations in fleet-member namespace", func() {
		var (
			mcName       string
			imcNamespace string
		)

		BeforeAll(func() {
			Skip("PDB webhook is temporarily disabled.")

			mcName = fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
			imcNamespace = fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			createMemberCluster(mcName, testIdentity, nil, map[string]string{fleetClusterResourceIDAnnotationKey: clusterID1})
			checkInternalMemberClusterExists(mcName, imcNamespace)
		})

		AfterAll(func() {
			ensureMemberClusterAndRelatedResourcesDeletion(mcName)
		})

		It("should deny CREATE operation on PDB in fleet-member namespace for user not in MC identity", func() {
			Skip("PDB webhook is temporarily disabled.")

			pdb := policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pdb-member",
					Namespace: imcNamespace,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: ptr.To(intstr.FromInt32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
				},
			}
			Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Create(ctx, &pdb), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &pdbGVK, "", types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}))).Should(Succeed())
		})

		It("should deny UPDATE operation on PDB in fleet-member namespace for user not in MC identity", func() {
			Skip("PDB webhook is temporarily disabled.")

			// First create a PDB as admin.
			pdb := policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pdb-member-update",
					Namespace: imcNamespace,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: ptr.To(intstr.FromInt32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
				},
			}
			Expect(hubClient.Create(ctx, &pdb)).Should(Succeed())

			// Try to update as non-admin.
			Eventually(func(g Gomega) error {
				var p policyv1.PodDisruptionBudget
				err := hubClient.Get(ctx, types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}, &p)
				if err != nil {
					return err
				}
				p.Labels = map[string]string{testKey: testValue}
				err = impersonateHubClient.Update(ctx, &p)
				if k8sErrors.IsConflict(err) {
					return err
				}
				return checkIfStatusErrorWithMessage(err, fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &pdbGVK, "", types.NamespacedName{Name: p.Name, Namespace: p.Namespace}))
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())

			// Cleanup.
			Expect(hubClient.Delete(ctx, &pdb)).Should(Succeed())
		})
	})

	Context("deny PDB operations in kube-system namespace", func() {
		It("should deny CREATE operation on PDB in kube-system namespace for non-whitelisted users", func() {
			Skip("PDB webhook is temporarily disabled.")

			pdb := policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pdb-kube",
					Namespace: "kube-system",
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: ptr.To(intstr.FromInt32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
				},
			}
			Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Create(ctx, &pdb), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &pdbGVK, "", types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}))).Should(Succeed())
		})
	})
})

var _ = Describe("fleet guard rail restrict internal fleet resources from being created in fleet/kube pre-fixed namespaces", Serial, Ordered, func() {
	Context("deny request to CREATE IMC in fleet-system namespace", func() {
		It("should deny CREATE operation on internal member cluster resource in fleet-system namespace for invalid user", func() {
			imc := clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "fleet-system",
				},
				Spec: clusterv1beta1.InternalMemberClusterSpec{
					State:                  clusterv1beta1.ClusterStateJoin,
					HeartbeatPeriodSeconds: 30,
				},
			}
			Expect(checkIfStatusErrorWithMessage(notMasterUser.Create(ctx, &imc), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}))).Should(Succeed())
		})
	})

	It("should deny CREATE operation on internal service export resource in kube-system namespace for invalid user", func() {
		ise := internalServiceExport("test-ise", "kube-system")
		Expect(checkIfStatusErrorWithMessage(notMasterUser.Create(ctx, &ise), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &iseGVK, "", types.NamespacedName{Name: ise.Name, Namespace: ise.Namespace}))).Should(Succeed())
	})
})

var _ = Describe("fleet deployment webhook tests for validating and mutating deployments", Serial, Ordered, func() {
	const (
		testNS = "default"
	)

	newDeployment := func(name, namespace string, deployLabels, podTemplateLabels map[string]string) *appsv1.Deployment {
		mergeMaps := func(ms ...map[string]string) map[string]string {
			result := make(map[string]string)
			for _, m := range ms {
				for k, v := range m {
					result[k] = v
				}
			}
			return result
		}
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    deployLabels,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(1)),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": name},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: mergeMaps(map[string]string{"app": name}, podTemplateLabels),
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "nginx:latest",
							},
						},
					},
				},
			},
		}
	}

	Context("validating webhook - deny regular user from setting reconcile label", func() {
		It("should deny regular user from creating a deployment with reconcile label on deployment metadata", func() {
			deploy := newDeployment("test-deploy-val-1", testNS,
				map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
				nil,
			)
			// Deployment creation should be denied; no cleanup needed.
			err := notMasterUser.Create(ctx, deploy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create deployment call produced error %s, want StatusError", fmt.Sprintf("%T", err)))
			Expect(statusErr.ErrStatus.Message).Should(ContainSubstring(utils.ReconcileLabelKey))
		})

		It("should deny regular user from creating a deployment with reconcile label on pod template", func() {
			deploy := newDeployment("test-deploy-val-2", testNS,
				nil,
				map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
			)
			// Deployment creation should be denied; no cleanup needed.
			err := notMasterUser.Create(ctx, deploy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create deployment call produced error %s, want StatusError", fmt.Sprintf("%T", err)))
			Expect(statusErr.ErrStatus.Message).Should(ContainSubstring(utils.ReconcileLabelKey))
		})

		It("should deny regular user from updating a deployment to add reconcile label", func() {
			// First create a deployment without reconcile label as admin.
			deploy := newDeployment("test-deploy-val-3", testNS, nil, nil)
			Expect(hubClient.Create(ctx, deploy)).Should(Succeed())
			DeferCleanup(func() {
				_ = hubClient.Delete(ctx, deploy)
			})

			// Try to update as non-admin to add reconcile label.
			Eventually(func(g Gomega) error {
				var d appsv1.Deployment
				err := hubClient.Get(ctx, types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}, &d)
				if err != nil {
					return err
				}
				if d.Labels == nil {
					d.Labels = map[string]string{}
				}
				d.Labels[utils.ReconcileLabelKey] = utils.ReconcileLabelValue
				err = notMasterUser.Update(ctx, &d)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update deployment call produced error %s, want StatusError", fmt.Sprintf("%T", err)))
				g.Expect(statusErr.ErrStatus.Message).Should(ContainSubstring(utils.ReconcileLabelKey))
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})

	Context("validating webhook - allow aksService user to set reconcile label", func() {
		It("should allow aksService user to create a deployment with reconcile label", func() {
			deploy := newDeployment("test-deploy-val-aks-1", testNS,
				map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
				map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
			)
			DeferCleanup(func() {
				_ = hubClient.Delete(ctx, deploy)
			})
			Expect(sysMastersClient.Create(ctx, deploy)).Should(Succeed())
		})

		It("should allow aksService user to create a deployment with reconcile label in kube-system", func() {
			deploy := newDeployment("test-deploy-val-aks-2", "kube-system",
				map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
				map[string]string{utils.ReconcileLabelKey: utils.ReconcileLabelValue},
			)
			DeferCleanup(func() {
				_ = hubClient.Delete(ctx, deploy)
			})
			Expect(sysMastersClient.Create(ctx, deploy)).Should(Succeed())
		})
	})

	Context("validating webhook - allow deployment without reconcile label", func() {
		It("should allow regular user to create a deployment without reconcile label", func() {
			deploy := newDeployment("test-deploy-val-norc", testNS, nil, nil)
			DeferCleanup(func() {
				_ = hubClient.Delete(ctx, deploy)
			})
			Expect(notMasterUser.Create(ctx, deploy)).Should(Succeed())
		})

		It("should allow aksService user to create a deployment without reconcile label", func() {
			deploy := newDeployment("test-deploy-val-norc", testNS, nil, nil)
			DeferCleanup(func() {
				_ = hubClient.Delete(ctx, deploy)
			})
			Expect(sysMastersClient.Create(ctx, deploy)).Should(Succeed())
		})
	})

	Context("mutating webhook - inject reconcile label for aksService user", func() {
		It("should inject reconcile label when aksService creates a deployment", func() {
			deploy := newDeployment("test-deploy-mut-1", testNS, nil, nil)
			DeferCleanup(func() {
				_ = hubClient.Delete(ctx, deploy)
			})
			Expect(sysMastersClient.Create(ctx, deploy)).Should(Succeed())

			// Verify the reconcile label was injected. The mutating webhook runs
			// synchronously during admission, but Eventually is used defensively
			// to handle brief etcd/cache propagation lag.
			Eventually(func(g Gomega) {
				var d appsv1.Deployment
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}, &d)).Should(Succeed())
				g.Expect(d.Labels).Should(HaveKeyWithValue(utils.ReconcileLabelKey, utils.ReconcileLabelValue),
					"deployment metadata should have reconcile label injected by mutating webhook")
				g.Expect(d.Spec.Template.Labels).Should(HaveKeyWithValue(utils.ReconcileLabelKey, utils.ReconcileLabelValue),
					"pod template should have reconcile label injected by mutating webhook")
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})

	Context("mutating webhook - no injection for regular user", func() {
		It("should not inject reconcile label when regular user creates a deployment", func() {
			deploy := newDeployment("test-deploy-mut-2", testNS, nil, nil)
			DeferCleanup(func() {
				_ = hubClient.Delete(ctx, deploy)
			})
			Expect(notMasterUser.Create(ctx, deploy)).Should(Succeed())

			// Verify the reconcile label was NOT injected.
			Eventually(func(g Gomega) {
				var d appsv1.Deployment
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}, &d)).Should(Succeed())
				g.Expect(d.Labels).ShouldNot(HaveKey(utils.ReconcileLabelKey),
					"deployment metadata should NOT have reconcile label for regular user")
				g.Expect(d.Spec.Template.Labels).ShouldNot(HaveKey(utils.ReconcileLabelKey),
					"pod template should NOT have reconcile label for regular user")
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})

	Context("mutating webhook - no injection in reserved namespaces", func() {
		It("should not inject reconcile label for deployment in kube-system even for aksService", func() {
			deploy := newDeployment("test-deploy-mut-3", "kube-system", nil, nil)
			DeferCleanup(func() {
				_ = hubClient.Delete(ctx, deploy)
			})
			Expect(sysMastersClient.Create(ctx, deploy)).Should(Succeed())

			// Verify the reconcile label was NOT injected for reserved namespace.
			Eventually(func(g Gomega) {
				var d appsv1.Deployment
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}, &d)).Should(Succeed())
				g.Expect(d.Labels).ShouldNot(HaveKey(utils.ReconcileLabelKey),
					"deployment in kube-system should NOT have reconcile label auto-injected")
				g.Expect(d.Spec.Template.Labels).ShouldNot(HaveKey(utils.ReconcileLabelKey),
					"pod template in kube-system should NOT have reconcile label auto-injected")
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})

	Context("end-to-end deployment lifecycle with webhook enforcement", func() {
		It("should block RS and Pod creation when non-aksService user creates a deployment", func() {
			deployName := "test-deploy-e2e-noaks"
			deploy := newDeployment(deployName, testNS, nil, nil)
			DeferCleanup(func() {
				_ = hubClient.Delete(ctx, deploy)
			})

			By("creating deployment as non-aksService user")
			Expect(notMasterUser.Create(ctx, deploy)).Should(Succeed())

			By("waiting for deployment to appear in the cache")
			Eventually(func(g Gomega) {
				var d appsv1.Deployment
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: testNS}, &d)).Should(Succeed())
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())

			By("verifying deployment exists but has no available replicas because RS creation is blocked")
			Consistently(func(g Gomega) {
				var d appsv1.Deployment
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: testNS}, &d)).Should(Succeed())
				g.Expect(d.Status.AvailableReplicas).Should(Equal(int32(0)),
					"deployment should have 0 available replicas because RS/Pod creation is blocked by validating webhooks")
				g.Expect(d.Status.ReadyReplicas).Should(Equal(int32(0)),
					"deployment should have 0 ready replicas")

				// Verify no ReplicaSets exist for this deployment.
				var rsList appsv1.ReplicaSetList
				g.Expect(hubClient.List(ctx, &rsList, client.InNamespace(testNS), client.MatchingLabels{"app": deployName})).Should(Succeed())
				g.Expect(rsList.Items).Should(BeEmpty(),
					"no ReplicaSets should exist because the RS validating webhook blocks creation without the reconcile label")

				// Verify no Pods exist for this deployment.
				var podList corev1.PodList
				g.Expect(hubClient.List(ctx, &podList, client.InNamespace(testNS), client.MatchingLabels{"app": deployName})).Should(Succeed())
				g.Expect(podList.Items).Should(BeEmpty(),
					"no Pods should exist because the Pod validating webhook blocks creation without the reconcile label")
			}, consistentlyDuration, consistentlyInterval).Should(Succeed())
		})

		It("should allow RS and Pod creation when aksService user creates a deployment", func() {
			deployName := "test-deploy-e2e-aks"
			deploy := newDeployment(deployName, testNS, nil, nil)
			DeferCleanup(func() {
				_ = hubClient.Delete(ctx, deploy)
			})

			By("creating deployment as aksService user (mutating webhook injects reconcile label)")
			Expect(sysMastersClient.Create(ctx, deploy)).Should(Succeed())

			By("verifying the mutating webhook injected the reconcile label")
			Eventually(func(g Gomega) {
				var d appsv1.Deployment
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: testNS}, &d)).Should(Succeed())
				g.Expect(d.Labels).Should(HaveKeyWithValue(utils.ReconcileLabelKey, utils.ReconcileLabelValue),
					"deployment metadata should have reconcile label injected by mutating webhook")
				g.Expect(d.Spec.Template.Labels).Should(HaveKeyWithValue(utils.ReconcileLabelKey, utils.ReconcileLabelValue),
					"pod template should have reconcile label injected by mutating webhook")
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())

			By("verifying ReplicaSet is created with the reconcile label")
			Eventually(func(g Gomega) {
				var rsList appsv1.ReplicaSetList
				g.Expect(hubClient.List(ctx, &rsList, client.InNamespace(testNS), client.MatchingLabels{"app": deployName})).Should(Succeed())
				g.Expect(rsList.Items).ShouldNot(BeEmpty(),
					"at least one ReplicaSet should be created for the deployment")

				rs := rsList.Items[0]
				g.Expect(rs.Labels).Should(HaveKeyWithValue(utils.ReconcileLabelKey, utils.ReconcileLabelValue),
					"ReplicaSet should inherit the reconcile label from the pod template")
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed())

			By("verifying Pods are running with the reconcile label")
			Eventually(func(g Gomega) {
				var podList corev1.PodList
				g.Expect(hubClient.List(ctx, &podList, client.InNamespace(testNS), client.MatchingLabels{"app": deployName})).Should(Succeed())
				g.Expect(podList.Items).ShouldNot(BeEmpty(),
					"at least one Pod should be created for the deployment")

				pod := podList.Items[0]
				g.Expect(pod.Labels).Should(HaveKeyWithValue(utils.ReconcileLabelKey, utils.ReconcileLabelValue),
					"Pod should inherit the reconcile label from the pod template")
				g.Expect(pod.Status.Phase).Should(Equal(corev1.PodRunning),
					"Pod should be in Running phase")
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed())

			By("verifying deployment reports available replicas")
			Eventually(func(g Gomega) {
				var d appsv1.Deployment
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: testNS}, &d)).Should(Succeed())
				g.Expect(d.Status.AvailableReplicas).Should(Equal(int32(1)),
					"deployment should have 1 available replica")
				g.Expect(d.Status.ReadyReplicas).Should(Equal(int32(1)),
					"deployment should have 1 ready replica")
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})
})
