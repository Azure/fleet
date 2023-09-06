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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	"go.goms.io/fleet/pkg/utils"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

const (
	testRole        = "test-role"
	testRoleBinding = "test-role-binding"
	testPod         = "test-pod"
	testDeployment  = "test-deployment"
	testJob         = "test-job"
)

var _ = Describe("Fleet's Reserved Namespaced Resources Handler webhook tests", func() {
	Context("fleet guard rail e2e for role in rbac/v1 api group", func() {
		var roleName string
		BeforeEach(func() {
			roleName = testRole + "-" + utils.RandStr()
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleName,
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
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleName,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &r)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: r.Name, Namespace: r.Namespace}, &r))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE operation on role for user not in system:masters group", func() {
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRole + "-" + utils.RandStr(),
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
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: memberNamespace.Name}, &r)).Should(Succeed())
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
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleName,
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
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: memberNamespace.Name}, &r)).Should(Succeed())
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

	Context("fleet guard rail e2e for role binding in rbac/v1 api group", func() {
		var roleBindingName string
		BeforeEach(func() {
			roleBindingName = testRoleBinding + "-" + utils.RandStr()
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleBindingName,
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
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleBindingName,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &rb)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace}, &rb))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE operation on role binding for user not in system:masters group", func() {
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRoleBinding + "-" + utils.RandStr(),
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
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: roleBindingName, Namespace: kubeSystemNS}, &rb)).Should(Succeed())
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
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleBindingName,
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
				var rb rbacv1.RoleBinding
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: roleBindingName, Namespace: kubeSystemNS}, &rb)).Should(Succeed())
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

	Context("fleet guard rail e2e for pod in core/v1 api group", func() {
		var podName string
		BeforeEach(func() {
			podName = testPod + "-" + utils.RandStr()
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
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPod + "-" + utils.RandStr(),
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

	Context("fleet guard rail e2e for deployment in apps/v1 api group", func() {
		var deploymentName string
		BeforeEach(func() {
			deploymentName = testDeployment + "-" + utils.RandStr()
			d := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
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
			d := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &d)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: d.Name, Namespace: d.Namespace}, &d))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE deployment operation for user not in system:masters group", func() {
			d := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testDeployment + "-" + utils.RandStr(),
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
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: kubeSystemNS}, &d)).Should(Succeed())
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
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: kubeSystemNS}, &d)).Should(Succeed())
			By("expecting denial of operation DELETE of deployment")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &d)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete deployment call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Deployment", types.NamespacedName{Name: d.Name, Namespace: d.Namespace})))
		})

		It("should allow update operation on deployment for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var d appsv1.Deployment
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: kubeSystemNS}, &d)).Should(Succeed())
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

	Context("fleet guard rail e2e for job in batch/v1 api group", func() {
		var jobName string
		BeforeEach(func() {
			jobName = testJob + "-" + utils.RandStr()
			j := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
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
			j := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &j)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: j.Name, Namespace: j.Namespace}, &j))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE job operation for user not in system:masters group", func() {
			j := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testJob + "-" + utils.RandStr(),
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
			Eventually(func(g Gomega) error {
				var j batchv1.Job
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: memberNamespace.Name}, &j)).Should(Succeed())
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
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: memberNamespace.Name}, &j)).Should(Succeed())
			By("expecting denial of operation DELETE of job")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &j)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete job call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Job", types.NamespacedName{Name: j.Name, Namespace: j.Namespace})))
		})

		It("should allow update operation on job for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var j batchv1.Job
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: memberNamespace.Name}, &j)).Should(Succeed())
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
