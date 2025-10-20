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
	rbacv1 "k8s.io/api/rbac/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/validation"

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
		Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Create(ctx, mc), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &mcGVK, "", types.NamespacedName{Name: mc.Name}))).Should(Succeed())
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
			err = impersonateHubClient.Update(ctx, &mc)
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
			err = impersonateHubClient.Update(ctx, &mc)
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
			err = impersonateHubClient.Update(ctx, &mc)
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
			err = impersonateHubClient.Status().Update(ctx, &mc)
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
		Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Delete(ctx, &mc), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &mcGVK, "", types.NamespacedName{Name: mc.Name}))).Should(Succeed())
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
			return impersonateHubClient.Update(ctx, &mc)
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
			return impersonateHubClient.Update(ctx, &mc)
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
			return impersonateHubClient.Update(ctx, &mc)
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
		Expect(impersonateHubClient.Create(ctx, mc)).Should(Succeed())
		Expect(impersonateHubClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)).Should(Succeed())
		Expect(impersonateHubClient.Delete(ctx, mc)).Should(Succeed())
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
			err = impersonateHubClient.Update(ctx, &mc)
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
			err = impersonateHubClient.Status().Update(ctx, &mc)
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
			return impersonateHubClient.Update(ctx, &mc)
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
			return impersonateHubClient.Update(ctx, &mc)
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
			return impersonateHubClient.Update(ctx, &mc)
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
			return impersonateHubClient.Update(ctx, &mc)
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
		Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Create(ctx, &imc), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}))).Should(Succeed())
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
			err = impersonateHubClient.Update(ctx, &imc)
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
		Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Delete(ctx, &imc), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}))).Should(Succeed())
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
			err = impersonateHubClient.Status().Update(ctx, &imc)
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
			return impersonateHubClient.Status().Update(ctx, &imc)
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
			return impersonateHubClient.Update(ctx, &imc)
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
		Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Create(ctx, &w), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &workGVK, "", types.NamespacedName{Name: w.Name, Namespace: w.Namespace}))).Should(Succeed())
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
			err = impersonateHubClient.Status().Update(ctx, &w)
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
		Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Delete(ctx, &w), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &workGVK, "", types.NamespacedName{Name: w.Name, Namespace: w.Namespace}))).Should(Succeed())
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
			return impersonateHubClient.Update(ctx, &w)
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
			return impersonateHubClient.Status().Update(ctx, &w)
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
				err = impersonateHubClient.Update(ctx, &ise)
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
			Expect(impersonateHubClient.Create(ctx, &ise)).Should(Succeed())

			By("deleting Internal Service Export")
			// Cleanup the internalServiceExport so that it won't block the member deletion.
			Expect(impersonateHubClient.Delete(ctx, &ise)).Should(Succeed(), "failed to delete Internal Service Export")
		})

		It("should allow CREATE operation on Internal service import resource in fleet-member namespace for user in member cluster identity", func() {
			isi := internalServiceImport(isiName, imcNamespace)
			By("expecting successful CREATE of Internal Service Import")
			Expect(impersonateHubClient.Create(ctx, &isi)).Should(Succeed())

			By("deleting Internal Service Import")
			Expect(impersonateHubClient.Delete(ctx, &isi)).Should(Succeed(), "failed to delete Internal Service Import")
		})

		It("should allow CREATE operation on Endpoint slice export resource in fleet-member namespace for user in member cluster identity", func() {
			esx := endpointSliceExport(epName, imcNamespace)
			By("expecting successful CREATE of Endpoint slice export")
			Expect(impersonateHubClient.Create(ctx, &esx)).Should(Succeed())

			By("deleting Endpoint Slice Export")
			Expect(impersonateHubClient.Delete(ctx, &esx)).Should(Succeed(), "failed to delete EndpointSlice Export")
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
				return impersonateHubClient.Update(ctx, &ise)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
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
			Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Create(ctx, &imc), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}))).Should(Succeed())
		})
	})

	It("should deny CREATE operation on internal service export resource in kube-system namespace for invalid user", func() {
		ise := internalServiceExport("test-ise", "kube-system")
		Expect(checkIfStatusErrorWithMessage(impersonateHubClient.Create(ctx, &ise), fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &iseGVK, "", types.NamespacedName{Name: ise.Name, Namespace: ise.Namespace}))).Should(Succeed())
	})
})
