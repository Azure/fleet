/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"errors"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	"go.goms.io/fleet/pkg/utils"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

const (
	testRole        = "test-role"
	testRoleBinding = "test-role-binding"
	testPod         = "test-pod"
	testService     = "test-service"
	testSecret      = "test-secret"
	testDaemonSet   = "test-daemon-set"
	testDeployment  = "test-deployment"
	testReplicaSet  = "test-replica-set"
	testConfigMap   = "test-config-map"
	testCronJob     = "test-cron-job"
	testJob         = "test-job"
)

var _ = Describe("Fleet's Reserved Namespaced Resources Handler webhook tests", func() {
	Context("fleet guard rail e2e for role", func() {
		BeforeEach(func() {
			rName := testRole + fmt.Sprint(GinkgoParallelProcess())
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rName,
					Namespace: memberNamespace.Name,
				},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"*"},
						APIGroups: []string{"*"},
						Resources: []string{"*"},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &r)).Should(Succeed())
		})

		AfterEach(func() {
			rName := testRole + fmt.Sprint(GinkgoParallelProcess())
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rName,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &r)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: r.Name, Namespace: r.Namespace}, &r))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE operation on role for user not in system:masters group", func() {
			rName := testRole + fmt.Sprint(GinkgoParallelProcess())
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rName + utils.RandStr(),
					Namespace: memberNamespace.Name,
				},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"*"},
						APIGroups: []string{rbacv1.SchemeGroupVersion.Group},
						Resources: []string{"*"},
					},
				},
			}

			By("expecting denial of operation CREATE of role")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &r)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create role call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Role", types.NamespacedName{Name: r.Name, Namespace: r.Namespace})))
		})

		It("should deny UPDATE operation on role for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var r rbacv1.Role
				rName := testRole + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rName, Namespace: memberNamespace.Name}, &r)).Should(Succeed())
				By("update role")
				labels := make(map[string]string)
				labels[testKey] = testValue
				r.SetLabels(labels)
				By("expecting denial of operation UPDATE of role")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &r)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update role call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Role", types.NamespacedName{Name: r.Name, Namespace: r.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE operation on role for user not in system:masters group", func() {
			rName := testRole + fmt.Sprint(GinkgoParallelProcess())
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rName,
					Namespace: memberNamespace.Name,
				},
			}

			By("expecting denial of operation DELETE of role")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &r)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete role call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Role", types.NamespacedName{Name: r.Name, Namespace: r.Namespace})))
		})

		It("should allow update operation on role for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var r rbacv1.Role
				rName := testRole + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rName, Namespace: memberNamespace.Name}, &r)).Should(Succeed())
				By("update labels in Role")
				labels := make(map[string]string)
				labels[testKey] = testValue
				r.SetLabels(labels)

				By("expecting successful UPDATE of role")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &r)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for role binding", func() {
		BeforeEach(func() {
			rbName := testRoleBinding + fmt.Sprint(GinkgoParallelProcess())
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rbName,
					Namespace: kubeSystemNS,
				},
				Subjects: []rbacv1.Subject{
					{
						APIGroup: rbacv1.GroupName,
						Kind:     "User",
						Name:     "test-user",
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "Role",
					Name:     testRole,
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &rb)).Should(Succeed())
		})

		AfterEach(func() {
			rbName := testRoleBinding + fmt.Sprint(GinkgoParallelProcess())
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rbName,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &rb)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace}, &rb))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE operation on role binding for user not in system:masters group", func() {
			rbName := testRoleBinding + fmt.Sprint(GinkgoParallelProcess())
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rbName + utils.RandStr(),
					Namespace: kubeSystemNS,
				},
				Subjects: []rbacv1.Subject{
					{
						APIGroup: rbacv1.GroupName,
						Kind:     "User",
						Name:     testUser,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "Role",
					Name:     testRole,
				},
			}

			By("expecting denial of operation CREATE of role binding")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &rb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create role binding call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "RoleBinding", types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace})))
		})

		It("should deny UPDATE operation on role binding for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var rb rbacv1.RoleBinding
				rbName := testRoleBinding + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rbName, Namespace: kubeSystemNS}, &rb)).Should(Succeed())
				By("update role")
				labels := make(map[string]string)
				labels[testKey] = testValue
				rb.SetLabels(labels)
				By("expecting denial of operation UPDATE of role binding")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &rb)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update role binding call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "RoleBinding", types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE operation on role binding for user not in system:masters group", func() {
			rbName := testRoleBinding + fmt.Sprint(GinkgoParallelProcess())
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rbName,
					Namespace: kubeSystemNS,
				},
			}

			By("expecting denial of operation DELETE of role binding")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &rb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete role binding call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "RoleBinding", types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace})))
		})

		It("should allow update operation on role binding for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				rbName := testRoleBinding + fmt.Sprint(GinkgoParallelProcess())
				var rb rbacv1.RoleBinding
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rbName, Namespace: kubeSystemNS}, &rb)).Should(Succeed())
				By("update labels in role binding")
				labels := make(map[string]string)
				labels[testKey] = testValue
				rb.SetLabels(labels)

				By("expecting successful UPDATE of role binding")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &rb)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for pod", func() {
		BeforeEach(func() {
			podName := testPod + fmt.Sprint(GinkgoParallelProcess())
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: kubeSystemNS,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image:           "busybox",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Name:            "busybox",
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &pod)).Should(Succeed())
		})
		AfterEach(func() {
			podName := testPod + fmt.Sprint(GinkgoParallelProcess())
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &pod)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, &pod))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE pod operation for user not in system:masters group", func() {
			podName := testPod + fmt.Sprint(GinkgoParallelProcess())
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName + utils.RandStr(),
					Namespace: kubeSystemNS,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image:           "busybox",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Name:            "busybox",
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			}

			By("expecting denial of operation CREATE of pod")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &pod)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create pod call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Pod", types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace})))
		})

		It("should deny UPDATE pod operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var pod corev1.Pod
				podName := testPod + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: kubeSystemNS}, &pod)).Should(Succeed())
				pod.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of pod")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &pod)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update pod call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Pod", types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE pod operation for user not in system:masters group", func() {
			var pod corev1.Pod
			podName := testPod + fmt.Sprint(GinkgoParallelProcess())
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: kubeSystemNS}, &pod)).Should(Succeed())
			By("expecting denial of operation DELETE of pod")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &pod)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete pod call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Pod", types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace})))
		})

		It("should allow update operation on pod for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var pod corev1.Pod
				podName := testPod + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: kubeSystemNS}, &pod)).Should(Succeed())
				By("update labels in pod")
				labels := make(map[string]string)
				labels[testKey] = testValue
				pod.SetLabels(labels)

				By("expecting successful UPDATE of pod")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &pod)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for service", func() {
		BeforeEach(func() {
			serviceName := testService + fmt.Sprint(GinkgoParallelProcess())
			service := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: fleetSystemNS,
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Protocol: corev1.ProtocolTCP,
							Port:     80,
							TargetPort: intstr.IntOrString{
								IntVal: 8080,
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &service)).Should(Succeed())
		})
		AfterEach(func() {
			serviceName := testService + fmt.Sprint(GinkgoParallelProcess())
			service := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: fleetSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &service)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, &service))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())

		})

		It("should deny CREATE service operation for user not in system:masters group", func() {
			serviceName := testService + fmt.Sprint(GinkgoParallelProcess())
			service := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName + utils.RandStr(),
					Namespace: fleetSystemNS,
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Protocol: corev1.ProtocolTCP,
							Port:     80,
							TargetPort: intstr.IntOrString{
								IntVal: 8080,
							},
						},
					},
				},
			}

			By("expecting denial of operation CREATE of service")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &service)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create service call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Service", types.NamespacedName{Name: service.Name, Namespace: service.Namespace})))
		})

		It("should deny UPDATE service operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var service corev1.Service
				serviceName := testService + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: fleetSystemNS}, &service)).Should(Succeed())
				service.Spec.Ports[0].Port = 81
				By("expecting denial of operation UPDATE of service")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &service)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update service call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Service", types.NamespacedName{Name: service.Name, Namespace: service.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE service operation for user not in system:masters group", func() {
			var service corev1.Service
			serviceName := testService + fmt.Sprint(GinkgoParallelProcess())
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: fleetSystemNS}, &service)).Should(Succeed())
			By("expecting denial of operation DELETE of service")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &service)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete service call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Service", types.NamespacedName{Name: service.Name, Namespace: service.Namespace})))
		})

		It("should allow update operation on service for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var s corev1.Service
				serviceName := testService + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: fleetSystemNS}, &s)).Should(Succeed())
				By("update labels in service")
				labels := make(map[string]string)
				labels[testKey] = testValue
				s.SetLabels(labels)

				By("expecting successful UPDATE of service")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &s)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for config map", func() {
		BeforeEach(func() {
			cmName := testConfigMap + fmt.Sprint(GinkgoParallelProcess())
			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: memberNamespace.Name,
				},
				Data: map[string]string{"test-key": "test-value"},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &cm)).Should(Succeed())
		})
		AfterEach(func() {
			cmName := testConfigMap + fmt.Sprint(GinkgoParallelProcess())
			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &cm)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, &cm))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE config map operation for user not in system:masters group", func() {
			cmName := testConfigMap + fmt.Sprint(GinkgoParallelProcess())
			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName + utils.RandStr(),
					Namespace: memberNamespace.Name,
				},
				Data: map[string]string{"test-key": "test-value"},
			}

			By("expecting denial of operation CREATE of config map")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &cm)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create config map call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ConfigMap", types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace})))
		})

		It("should deny UPDATE config map operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var cm corev1.ConfigMap
				cmName := testConfigMap + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: memberNamespace.Name}, &cm)).Should(Succeed())
				cm.Data["test-key"] = "test-value1"
				By("expecting denial of operation UPDATE of config map")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &cm)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update config map call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ConfigMap", types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE config map operation for user not in system:masters group", func() {
			var cm corev1.ConfigMap
			cmName := testConfigMap + fmt.Sprint(GinkgoParallelProcess())
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: memberNamespace.Name}, &cm)).Should(Succeed())
			By("expecting denial of operation DELETE of config map")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &cm)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete config map call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ConfigMap", types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace})))
		})

		It("should allow update operation on config map for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var cm corev1.ConfigMap
				cmName := testConfigMap + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: memberNamespace.Name}, &cm)).Should(Succeed())
				By("update labels in config map")
				labels := make(map[string]string)
				labels[testKey] = testValue
				cm.SetLabels(labels)

				By("expecting successful UPDATE of config map")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &cm)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for secret", func() {
		BeforeEach(func() {
			sName := testSecret + fmt.Sprint(GinkgoParallelProcess())
			s := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sName,
					Namespace: memberNamespace.Name,
				},
				Data: map[string][]byte{"test-key": []byte("dGVzdA==")},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &s)).Should(Succeed())
		})
		AfterEach(func() {
			sName := testSecret + fmt.Sprint(GinkgoParallelProcess())
			s := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sName,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &s)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: s.Namespace}, &s))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE secret operation for user not in system:masters group", func() {
			sName := testSecret + fmt.Sprint(GinkgoParallelProcess())
			s := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sName + utils.RandStr(),
					Namespace: memberNamespace.Name,
				},
				Data: map[string][]byte{"test-key": []byte("dGVzdA==")},
			}

			By("expecting denial of operation CREATE of secret")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &s)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create secret call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Secret", types.NamespacedName{Name: s.Name, Namespace: s.Namespace})))
		})

		It("should deny UPDATE secret operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var s corev1.Secret
				sName := testSecret + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: sName, Namespace: memberNamespace.Name}, &s)).Should(Succeed())
				s.Data["test-key"] = []byte("dmFsdWUtMg0KDQo=")
				By("expecting denial of operation UPDATE of secret")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &s)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update secret call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Secret", types.NamespacedName{Name: s.Name, Namespace: s.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE secret operation for user not in system:masters group", func() {
			var s corev1.Secret
			sName := testSecret + fmt.Sprint(GinkgoParallelProcess())
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: sName, Namespace: memberNamespace.Name}, &s)).Should(Succeed())
			By("expecting denial of operation DELETE of secret")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &s)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete secret call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Secret", types.NamespacedName{Name: s.Name, Namespace: s.Namespace})))
		})

		It("should allow update operation on secret for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var s corev1.Secret
				sName := testSecret + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: sName, Namespace: memberNamespace.Name}, &s)).Should(Succeed())
				By("update labels in secret")
				labels := make(map[string]string)
				labels[testKey] = testValue
				s.SetLabels(labels)
				By("expecting successful UPDATE of secret")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &s)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for deployment", func() {
		BeforeEach(func() {
			dName := testDeployment + fmt.Sprint(GinkgoParallelProcess())
			d := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dName,
					Namespace: kubeSystemNS,
					Labels:    map[string]string{"app": "busybox"},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: pointer.Int32(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "busybox"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "busybox"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "busybox",
								},
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &d)).Should(Succeed())
		})
		AfterEach(func() {
			dName := testDeployment + fmt.Sprint(GinkgoParallelProcess())
			d := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dName,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &d)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: d.Name, Namespace: d.Namespace}, &d))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE deployment operation for user not in system:masters group", func() {
			dName := testDeployment + fmt.Sprint(GinkgoParallelProcess())
			d := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dName + utils.RandStr(),
					Namespace: kubeSystemNS,
					Labels:    map[string]string{"app": "busybox"},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: pointer.Int32(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "busybox"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "busybox"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "busybox",
								},
							},
						},
					},
				},
			}

			By("expecting denial of operation CREATE of deployment")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &d)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create deployment call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Deployment", types.NamespacedName{Name: d.Name, Namespace: d.Namespace})))
		})

		It("should deny UPDATE deployment operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var d appsv1.Deployment
				dName := testDeployment + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: dName, Namespace: kubeSystemNS}, &d)).Should(Succeed())
				d.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of deployment")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &d)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update deployment call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Deployment", types.NamespacedName{Name: d.Name, Namespace: d.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE deployment operation for user not in system:masters group", func() {
			var d appsv1.Deployment
			dName := testDeployment + fmt.Sprint(GinkgoParallelProcess())
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: dName, Namespace: kubeSystemNS}, &d)).Should(Succeed())
			By("expecting denial of operation DELETE of deployment")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &d)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete deployment call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Deployment", types.NamespacedName{Name: d.Name, Namespace: d.Namespace})))
		})

		It("should allow update operation on deployment for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var d appsv1.Deployment
				dName := testDeployment + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: dName, Namespace: kubeSystemNS}, &d)).Should(Succeed())
				By("update labels in deployment")
				labels := make(map[string]string)
				labels[testKey] = testValue
				d.SetLabels(labels)

				By("expecting successful UPDATE of deployment")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &d)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for replica set", func() {
		BeforeEach(func() {
			rsName := testReplicaSet + fmt.Sprint(GinkgoParallelProcess())
			rs := appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: memberNamespace.Name,
					Labels:    map[string]string{"tier": "frontend"},
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: pointer.Int32(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"tier": "frontend"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"tier": "frontend"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "busybox",
								},
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &rs)).Should(Succeed())
		})
		AfterEach(func() {
			rsName := testReplicaSet + fmt.Sprint(GinkgoParallelProcess())
			rs := appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &rs)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, &rs))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE replica set operation for user not in system:masters group", func() {
			rsName := testReplicaSet + fmt.Sprint(GinkgoParallelProcess())
			rs := appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName + utils.RandStr(),
					Namespace: memberNamespace.Name,
					Labels:    map[string]string{"tier": "frontend"},
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: pointer.Int32(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"tier": "frontend"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"tier": "frontend"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "busybox",
								},
							},
						},
					},
				},
			}

			By("expecting denial of operation CREATE of replica set")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &rs)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create replica set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ReplicaSet", types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace})))
		})

		It("should deny UPDATE replica set operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var rs appsv1.ReplicaSet
				rsName := testReplicaSet + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: memberNamespace.Name}, &rs)).Should(Succeed())
				rs.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of replica set")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &rs)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update replica set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ReplicaSet", types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE replica set operation for user not in system:masters group", func() {
			var rs appsv1.ReplicaSet
			rsName := testReplicaSet + fmt.Sprint(GinkgoParallelProcess())
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: memberNamespace.Name}, &rs)).Should(Succeed())
			By("expecting denial of operation DELETE of replica set")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &rs)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete replica set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ReplicaSet", types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace})))
		})

		It("should allow update operation on replica set for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var rs appsv1.ReplicaSet
				rsName := testReplicaSet + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: memberNamespace.Name}, &rs)).Should(Succeed())
				By("update labels in replica set")
				labels := make(map[string]string)
				labels[testKey] = testValue
				rs.SetLabels(labels)

				By("expecting successful UPDATE of replica set")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &rs)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for daemon set", func() {
		dsName := testDaemonSet + fmt.Sprint(GinkgoParallelProcess())
		BeforeEach(func() {
			hostPath := &corev1.HostPathVolumeSource{
				Path: "/var/log",
			}
			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName,
					Namespace: fleetSystemNS,
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"name": "fluentd-elasticsearch"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"name": "fluentd-elasticsearch"},
						},
						Spec: corev1.PodSpec{
							Tolerations: []corev1.Toleration{
								{
									Key:      "node-role.kubernetes.io/control-plane",
									Operator: corev1.TolerationOpExists,
									Effect:   corev1.TaintEffectNoSchedule,
								},
								{
									Key:      "node-role.kubernetes.io/master",
									Operator: corev1.TolerationOpExists,
									Effect:   corev1.TaintEffectNoSchedule,
								},
							},
							Containers: []corev1.Container{
								{
									Name:  "fluentd-elasticsearch",
									Image: "quay.io/fluentd_elasticsearch/fluentd:v2.5.2",
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("200Mi"),
										},
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("100m"),
											corev1.ResourceMemory: resource.MustParse("200Mi"),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "varlog",
									VolumeSource: corev1.VolumeSource{
										HostPath: hostPath,
									},
								},
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &ds)).Should(Succeed())
		})
		AfterEach(func() {
			dsName := testDaemonSet + fmt.Sprint(GinkgoParallelProcess())
			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName,
					Namespace: fleetSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &ds)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace}, &ds))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE daemon set operation for user not in system:masters group", func() {
			dsName := testDaemonSet + fmt.Sprint(GinkgoParallelProcess())
			hostPath := &corev1.HostPathVolumeSource{
				Path: "/var/log",
			}
			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName + utils.RandStr(),
					Namespace: fleetSystemNS,
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"name": "fluentd-elasticsearch"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"name": "fluentd-elasticsearch"},
						},
						Spec: corev1.PodSpec{
							Tolerations: []corev1.Toleration{
								{
									Key:      "node-role.kubernetes.io/control-plane",
									Operator: corev1.TolerationOpExists,
									Effect:   corev1.TaintEffectNoSchedule,
								},
								{
									Key:      "node-role.kubernetes.io/master",
									Operator: corev1.TolerationOpExists,
									Effect:   corev1.TaintEffectNoSchedule,
								},
							},
							Containers: []corev1.Container{
								{
									Name:  "fluentd-elasticsearch",
									Image: "quay.io/fluentd_elasticsearch/fluentd:v2.5.2",
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("200Mi"),
										},
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("100m"),
											corev1.ResourceMemory: resource.MustParse("200Mi"),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "varlog",
									VolumeSource: corev1.VolumeSource{
										HostPath: hostPath,
									},
								},
							},
						},
					},
				},
			}
			By("expecting denial of operation CREATE of daemon set")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &ds)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create daemon set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "DaemonSet", types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace})))
		})

		It("should deny UPDATE daemon set operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var ds appsv1.DaemonSet
				dsName := testDaemonSet + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: dsName, Namespace: fleetSystemNS}, &ds)).Should(Succeed())
				ds.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of daemon set")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &ds)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update dameon set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "DaemonSet", types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE daemon set operation for user not in system:masters group", func() {
			var ds appsv1.DaemonSet
			dsName := testDaemonSet + fmt.Sprint(GinkgoParallelProcess())
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: dsName, Namespace: fleetSystemNS}, &ds)).Should(Succeed())
			By("expecting denial of operation DELETE of daemon set")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &ds)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete daemon set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "DaemonSet", types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace})))
		})

		It("should allow update operation on daemon set for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var ds appsv1.DaemonSet
				dsName := testDaemonSet + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: dsName, Namespace: fleetSystemNS}, &ds)).Should(Succeed())
				By("update labels in daemon set")
				labels := make(map[string]string)
				labels[testKey] = testValue
				ds.SetLabels(labels)

				By("expecting successful UPDATE of daemon set")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &ds)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for cronjob", func() {
		BeforeEach(func() {
			cjName := testCronJob + fmt.Sprint(GinkgoParallelProcess())
			cj := batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cjName,
					Namespace: kubeSystemNS,
				},
				Spec: batchv1.CronJobSpec{
					Schedule: "* * * * *",
					JobTemplate: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Image:           "busybox",
											ImagePullPolicy: corev1.PullIfNotPresent,
											Name:            "cronjob-busybox",
										},
									},
									RestartPolicy: corev1.RestartPolicyOnFailure,
								},
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &cj)).Should(Succeed())
		})
		AfterEach(func() {
			cjName := testCronJob + fmt.Sprint(GinkgoParallelProcess())
			cj := batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cjName,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &cj)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cj.Name, Namespace: cj.Namespace}, &cj))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE cronjob operation for user not in system:masters group", func() {
			cjName := testCronJob + fmt.Sprint(GinkgoParallelProcess())
			cj := batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cjName + utils.RandStr(),
					Namespace: kubeSystemNS,
				},
				Spec: batchv1.CronJobSpec{
					Schedule: "* * * * *",
					JobTemplate: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Image:           "busybox",
											ImagePullPolicy: corev1.PullIfNotPresent,
											Name:            "cronjob-busybox",
										},
									},
									RestartPolicy: corev1.RestartPolicyOnFailure,
								},
							},
						},
					},
				},
			}
			By("expecting denial of operation CREATE of cronjob")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &cj)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create cronjob call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "CronJob", types.NamespacedName{Name: cj.Name, Namespace: cj.Namespace})))
		})

		It("should deny UPDATE cronjob operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var cj batchv1.CronJob
				cjName := testCronJob + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cjName, Namespace: kubeSystemNS}, &cj)).Should(Succeed())
				cj.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of cronjob")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &cj)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update cronjob call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "CronJob", types.NamespacedName{Name: cj.Name, Namespace: cj.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE cronjob operation for user not in system:masters group", func() {
			var cj batchv1.CronJob
			cjName := testCronJob + fmt.Sprint(GinkgoParallelProcess())
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cjName, Namespace: kubeSystemNS}, &cj)).Should(Succeed())
			By("expecting denial of operation DELETE of cronjob")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &cj)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete cronjob call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "CronJob", types.NamespacedName{Name: cj.Name, Namespace: cj.Namespace})))
		})

		It("should allow update operation on cronjob for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var cj batchv1.CronJob
				cjName := testCronJob + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cjName, Namespace: kubeSystemNS}, &cj)).Should(Succeed())
				By("update labels in cronjob")
				labels := make(map[string]string)
				labels[testKey] = testValue
				cj.SetLabels(labels)

				By("expecting successful UPDATE of cronjob")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &cj)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for job", func() {
		BeforeEach(func() {
			jName := testJob + fmt.Sprint(GinkgoParallelProcess())
			j := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jName,
					Namespace: memberNamespace.Name,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "job-busybox",
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &j)).Should(Succeed())
		})
		AfterEach(func() {
			jName := testJob + fmt.Sprint(GinkgoParallelProcess())
			j := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jName,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &j)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: j.Name, Namespace: j.Namespace}, &j))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE job operation for user not in system:masters group", func() {
			jName := testJob + fmt.Sprint(GinkgoParallelProcess())
			j := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jName + utils.RandStr(),
					Namespace: memberNamespace.Name,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "job-busybox",
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}
			By("expecting denial of operation CREATE of job")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &j)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create job call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Job", types.NamespacedName{Name: j.Name, Namespace: j.Namespace})))
		})

		It("should deny UPDATE job operation for user not in system:masters group", func() {
			jName := testJob + fmt.Sprint(GinkgoParallelProcess())
			Eventually(func(g Gomega) error {
				var j batchv1.Job
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: jName, Namespace: memberNamespace.Name}, &j)).Should(Succeed())
				j.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of job")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &j)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update job call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Job", types.NamespacedName{Name: j.Name, Namespace: j.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE job operation for user not in system:masters group", func() {
			var j batchv1.Job
			jName := testJob + fmt.Sprint(GinkgoParallelProcess())
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: jName, Namespace: memberNamespace.Name}, &j)).Should(Succeed())
			By("expecting denial of operation DELETE of job")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &j)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete job call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Job", types.NamespacedName{Name: j.Name, Namespace: j.Namespace})))
		})

		It("should allow update operation on job for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var j batchv1.Job
				jName := testJob + fmt.Sprint(GinkgoParallelProcess())
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: jName, Namespace: memberNamespace.Name}, &j)).Should(Succeed())
				By("update labels in job")
				labels := make(map[string]string)
				labels[testKey] = testValue
				j.SetLabels(labels)

				By("expecting successful UPDATE of job")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &j)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})
})
