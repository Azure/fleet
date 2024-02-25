/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

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

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/webhook/validation"
	testutils "go.goms.io/fleet/test/e2e/v1alpha1/utils"
)

const (
	testUser     = "test-user"
	testIdentity = "test-identity"
)

var (
	mcGVK      = metav1.GroupVersionKind{Group: clusterv1beta1.GroupVersion.Group, Version: clusterv1beta1.GroupVersion.Version, Kind: "MemberCluster"}
	imcGVK     = metav1.GroupVersionKind{Group: clusterv1beta1.GroupVersion.Group, Version: clusterv1beta1.GroupVersion.Version, Kind: "InternalMemberCluster"}
	workGVK    = metav1.GroupVersionKind{Group: placementv1beta1.GroupVersion.Group, Version: placementv1beta1.GroupVersion.Version, Kind: "Work"}
	iseGVK     = metav1.GroupVersionKind{Group: fleetnetworkingv1alpha1.GroupVersion.Group, Version: fleetnetworkingv1alpha1.GroupVersion.Version, Kind: "InternalServiceExport"}
	testGroups = []string{"system:authenticated"}
)

var _ = Describe("fleet guard rail tests for deny MC CREATE operations", func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())

	It("should deny CREATE operation on member cluster CR for user not in system:masters group", func() {
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

		By(fmt.Sprintf("expecting denial of operation CREATE of member cluster %s", mc.Name))
		err := impersonateHubClient.Create(ctx, mc)
		var statusErr *k8sErrors.StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
		Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &mcGVK, "", types.NamespacedName{Name: mc.Name})))
	})
})

var _ = Describe("fleet guard rail tests for allow/deny MC UPDATE, DELETE operations", Serial, Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		createMemberCluster(mcName, testUser, nil)
	})

	AfterAll(func() {
		ensureMemberClusterAndRelatedResourcesDeletion(mcName)
	})

	It("should deny UPDATE operation on member cluster CR for user not in MC identity", func() {
		Eventually(func(g Gomega) error {
			var mc clusterv1beta1.MemberCluster
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())

			By(fmt.Sprintf("update member cluster spec for MC %s", mc.Name))
			mc.Spec.HeartbeatPeriodSeconds = 30

			By(fmt.Sprintf("expecting denial of operation UPDATE of member cluster %s", mc.Name))
			err := impersonateHubClient.Update(ctx, &mc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &mcGVK, "", types.NamespacedName{Name: mc.Name})))
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny DELETE operation on member cluster CR for user not in system:masters group", func() {
		mc := clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcName,
			},
		}

		By(fmt.Sprintf("expecting denial of operation DELETE of member cluster %s", mc.Name))
		err := impersonateHubClient.Delete(ctx, &mc)
		var statusErr *k8sErrors.StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
		Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &mcGVK, "", types.NamespacedName{Name: mc.Name})))
	})

	It("should allow update operation on member cluster CR labels for any user", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update labels in member cluster %s, expecting successful UPDATE of member cluster", mcName))
		Eventually(func(g Gomega) error {
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())
			labels := make(map[string]string)
			labels["test-key"] = "test-value"
			mc.SetLabels(labels)
			return impersonateHubClient.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on member cluster CR annotations for any user", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update annotations in member cluster %s, expecting successful UPDATE of member cluster", mcName))
		Eventually(func(g Gomega) error {
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())
			annotations := make(map[string]string)
			annotations["test-key"] = "test-value"
			mc.SetLabels(annotations)
			return impersonateHubClient.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on member cluster CR taints for any user", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update taints in member cluster %s, expecting successful UPDATE of member cluster", mcName))
		Eventually(func(g Gomega) error {
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())
			taint := clusterv1beta1.Taint{
				Key:    "key1",
				Value:  "value1",
				Effect: corev1.TaintEffectNoSchedule,
			}
			mc.Spec.Taints = append(mc.Spec.Taints, taint)
			return impersonateHubClient.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on member cluster CR spec for user in system:masters group", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update spec of member cluster %s, expecting successful UPDATE of member cluster", mcName))
		Eventually(func(g Gomega) error {
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())
			mc.Spec.HeartbeatPeriodSeconds = 31
			return hubClient.Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update operation on member cluster CR status for user in system:masters group", func() {
		var mc clusterv1beta1.MemberCluster
		By(fmt.Sprintf("update status of member cluster %s, expecting successful UPDATE of member cluster", mcName))
		Eventually(func(g Gomega) error {
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())
			g.Expect(mc.Status.Conditions).ToNot(BeEmpty())
			mc.Status.Conditions[0].Reason = "update"
			return hubClient.Status().Update(ctx, &mc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})
})

var _ = Describe("fleet guard rail tests for IMC UPDATE operation, in fleet-member prefixed namespace with user not in MC identity", Serial, Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
	imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)

	BeforeAll(func() {
		createMemberCluster(mcName, testIdentity, nil)
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
		err := impersonateHubClient.Create(ctx, &imc)
		var statusErr *k8sErrors.StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create internal member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
		Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
	})

	It("should deny UPDATE operation on internal member cluster CR for user not in MC identity in fleet member namespace", func() {
		Eventually(func(g Gomega) error {
			var imc clusterv1beta1.InternalMemberCluster
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())
			imc.Spec.HeartbeatPeriodSeconds = 25

			By("expecting denial of operation UPDATE of Internal Member Cluster")
			err := impersonateHubClient.Update(ctx, &imc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update internal member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny DELETE operation on internal member cluster CR for user not in MC identity in fleet member namespace", func() {
		var imc clusterv1beta1.InternalMemberCluster
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())

		By("expecting denial of operation DELETE of Internal Member Cluster")
		err := impersonateHubClient.Delete(ctx, &imc)
		var statusErr *k8sErrors.StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete internal member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
		Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
	})

	It("should deny UPDATE operation on internal member cluster CR status for user not in MC identity in fleet member namespace", func() {
		Eventually(func(g Gomega) error {
			var imc clusterv1beta1.InternalMemberCluster
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())
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
			err := impersonateHubClient.Status().Update(ctx, &imc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update internal member cluster status call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString(testGroups), admissionv1.Update, &imcGVK, "status", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})
})

var _ = Describe("fleet guard rail tests for IMC UPDATE operation, in fleet-member prefixed namespace with user in MC identity", Serial, Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
	imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)

	BeforeAll(func() {
		createMemberCluster(mcName, testUser, nil)
		checkInternalMemberClusterExists(mcName, imcNamespace)
	})

	AfterAll(func() {
		ensureMemberClusterAndRelatedResourcesDeletion(mcName)
	})

	It("should allow UPDATE operation on internal member cluster CR status for user in MC identity", func() {
		Eventually(func(g Gomega) error {
			var imc clusterv1beta1.InternalMemberCluster
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())
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
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())
			imc.Labels = map[string]string{"test-key": "test-value"}
			By("expecting successful UPDATE of Internal Member Cluster Status")
			return impersonateHubClient.Update(ctx, &imc)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow UPDATE operation on internal member cluster CR spec for user in system:masters group", func() {
		Eventually(func(g Gomega) error {
			var imc clusterv1beta1.InternalMemberCluster
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())
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
		createMemberCluster(mcName, testIdentity, nil)
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
		err = impersonateHubClient.Create(ctx, &w)
		var statusErr *k8sErrors.StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create work call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
		Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &workGVK, "", types.NamespacedName{Name: w.Name, Namespace: w.Namespace})))
	})

	It("should deny UPDATE operation on work CR status for user not in MC identity", func() {
		Eventually(func(g Gomega) error {
			var w placementv1beta1.Work
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: imcNamespace}, &w)).Should(Succeed())
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
			err := impersonateHubClient.Status().Update(ctx, &w)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update work status call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &workGVK, "status", types.NamespacedName{Name: w.Name, Namespace: w.Namespace})))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny DELETE work operation for user not in MC identity", func() {
		var w placementv1beta1.Work
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: imcNamespace}, &w)).Should(Succeed())
		By("expecting denial of operation DELETE of work")
		err := impersonateHubClient.Delete(ctx, &w)
		var statusErr *k8sErrors.StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete work call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
		Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &workGVK, "", types.NamespacedName{Name: w.Name, Namespace: w.Namespace})))
	})
})

var _ = Describe("fleet guard rail for UPDATE work operations, in fleet prefixed namespace with user in MC identity", Serial, Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
	imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
	workName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		createMemberCluster(mcName, testUser, nil)
		checkInternalMemberClusterExists(mcName, imcNamespace)
		createWorkResource(workName, imcNamespace)
	})

	AfterAll(func() {
		ensureMemberClusterAndRelatedResourcesDeletion(mcName)
	})

	It("should allow UPDATE operation on work CR for user in MC identity", func() {
		Eventually(func(g Gomega) error {
			var w placementv1beta1.Work
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: imcNamespace}, &w)).Should(Succeed())
			w.SetLabels(map[string]string{"test-key": "test-value"})
			By("expecting successful UPDATE of work")
			return impersonateHubClient.Update(ctx, &w)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow UPDATE operation on work CR status for user in MC identity", func() {
		Eventually(func(g Gomega) error {
			var w placementv1beta1.Work
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: imcNamespace}, &w)).Should(Succeed())
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
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: imcNamespace}, &w)).Should(Succeed())
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

		BeforeEach(func() {
			createMemberCluster(mcName, "random-user", nil)
			checkInternalMemberClusterExists(mcName, imcNamespace)
			ise := internalServiceExport(iseName, imcNamespace)
			// can return no kind match error.
			Eventually(func(g Gomega) error {
				return hubClient.Create(ctx, &ise)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		AfterEach(func() {
			ensureMemberClusterAndRelatedResourcesDeletion(mcName)
		})

		It("should deny update operation on Internal service export resource in fleet-member namespace for user not in member cluster identity", func() {
			Eventually(func(g Gomega) error {
				var ise fleetnetworkingv1alpha1.InternalServiceExport
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: iseName, Namespace: imcNamespace}, &ise)).Should(Succeed())
				ise.SetLabels(map[string]string{"test-key": "test-value"})
				By("expecting denial of operation UPDATE of Internal Service Export")
				err := impersonateHubClient.Update(ctx, &ise)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update Internal Serivce Export call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &iseGVK, "", types.NamespacedName{Name: ise.Name, Namespace: ise.Namespace})))
				return nil
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
			createMemberCluster(mcName, "test-user", nil)
			checkInternalMemberClusterExists(mcName, imcNamespace)
		})

		AfterEach(func() {
			ensureMemberClusterAndRelatedResourcesDeletion(mcName)
		})

		It("should allow CREATE operation on Internal service export resource in fleet-member namespace for user in member cluster identity", func() {
			ise := internalServiceExport(iseName, imcNamespace)
			By("expecting successful CREATE of Internal Service Export")
			Expect(impersonateHubClient.Create(ctx, &ise)).Should(Succeed())
		})

		It("should allow CREATE operation on Internal service import resource in fleet-member namespace for user in member cluster identity", func() {
			ise := internalServiceImport(isiName, imcNamespace)
			By("expecting successful CREATE of Internal Service Import")
			Expect(impersonateHubClient.Create(ctx, &ise)).Should(Succeed())
		})

		It("should allow CREATE operation on Endpoint slice export resource in fleet-member namespace for user in member cluster identity", func() {
			ise := endpointSliceExport(epName, imcNamespace)
			By("expecting successful CREATE of Endpoint slice export")
			Expect(impersonateHubClient.Create(ctx, &ise)).Should(Succeed())
		})
	})

	Context("allow request to modify network resources in fleet member namespaces, for user in member cluster identity", func() {
		mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())
		iseName := fmt.Sprintf(internalServiceExportNameTemplate, GinkgoParallelProcess())
		imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)

		BeforeEach(func() {
			createMemberCluster(mcName, testUser, nil)
			checkInternalMemberClusterExists(mcName, imcNamespace)
			ise := internalServiceExport(iseName, imcNamespace)
			// can return no kind match error.
			Eventually(func(g Gomega) error {
				return hubClient.Create(ctx, &ise)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		AfterEach(func() {
			ensureMemberClusterAndRelatedResourcesDeletion(mcName)
		})

		It("should allow update operation on Internal service export resource in fleet-member namespace for user in member cluster identity", func() {
			Eventually(func(g Gomega) error {
				var ise fleetnetworkingv1alpha1.InternalServiceExport
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: iseName, Namespace: imcNamespace}, &ise)).Should(Succeed())
				ise.SetLabels(map[string]string{"test-key": "test-value"})
				By("expecting denial of operation UPDATE of Internal Service Export")
				Expect(impersonateHubClient.Update(ctx, &ise)).Should(Succeed())
				return nil
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
			err := impersonateHubClient.Create(ctx, &imc)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create internal member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
		})
	})

	It("should deny CREATE operation on internal service export resource in kube-system namespace for invalid user", func() {
		ise := internalServiceExport("test-ise", "kube-system")
		err := impersonateHubClient.Create(ctx, &ise)
		var statusErr *k8sErrors.StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create internal service export call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
		Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &iseGVK, "", types.NamespacedName{Name: ise.Name, Namespace: ise.Namespace})))
	})
})
