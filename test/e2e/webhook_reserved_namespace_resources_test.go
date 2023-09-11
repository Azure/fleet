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
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	"go.goms.io/fleet/pkg/utils"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

const (
	testRole                    = "test-role"
	testRoleBinding             = "test-role-binding"
	testPod                     = "test-pod"
	testDeployment              = "test-deployment"
	testJob                     = "test-job"
	testHorizontalPodAutoScaler = "test-horizontal-pod-auto-scaler"
	testLease                   = "test-lease"
	testEndPointSlice           = "test-endpoint-slice"
	testIngress                 = "test-ingress"
	testPodDisruptionBudget     = "test-pod-disruption-budget"
	testCSICapacity             = "test-csi-capacity"
)

// TODO(Arvindthiru): Refactor file to use table driven tests.
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

	Context("fleet guard rail e2e for horizontal pod auto scaler", func() {
		var hpaName string
		BeforeEach(func() {
			hpaName = testHorizontalPodAutoScaler + "-" + utils.RandStr()
			hpa := autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hpaName,
					Namespace: fleetSystemNS,
				},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "test-deployment",
					},
					MaxReplicas: 1,
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &hpa)).Should(Succeed())
		})
		AfterEach(func() {
			hpa := autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hpaName,
					Namespace: fleetSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &hpa)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: hpa.Name, Namespace: hpa.Namespace}, &hpa))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE horizontal pod auto scaler operation for user not in system:masters group", func() {
			hpa := autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testHorizontalPodAutoScaler + "-" + utils.RandStr(),
					Namespace: fleetSystemNS,
				},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "test-deployment",
					},
					MaxReplicas: 1,
				},
			}
			By("expecting denial of operation CREATE of horizontal pod auto scaler")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &hpa)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create horizontal pod auto scaler call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "HorizontalPodAutoscaler", types.NamespacedName{Name: hpa.Name, Namespace: hpa.Namespace})))
		})

		It("should deny UPDATE horizontal pod auto scaler operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var hpa autoscalingv2.HorizontalPodAutoscaler
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: hpaName, Namespace: fleetSystemNS}, &hpa)).Should(Succeed())
				hpa.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of horizontal pod auto scaler")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &hpa)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update horizontal pod auto scaler call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "HorizontalPodAutoscaler", types.NamespacedName{Name: hpa.Name, Namespace: hpa.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE horizontal pod auto scaler operation for user not in system:masters group", func() {
			var hpa autoscalingv2.HorizontalPodAutoscaler
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: hpaName, Namespace: fleetSystemNS}, &hpa)).Should(Succeed())
			By("expecting denial of operation DELETE of horizontal pod auto scaler")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &hpa)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete horizontal pod auto scaler call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "HorizontalPodAutoscaler", types.NamespacedName{Name: hpa.Name, Namespace: hpa.Namespace})))
		})

		It("should allow update operation on horizontal pod auto scaler for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var hpa autoscalingv2.HorizontalPodAutoscaler
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: hpaName, Namespace: fleetSystemNS}, &hpa)).Should(Succeed())
				By("update labels in horizontal pod auto scaler")
				labels := make(map[string]string)
				labels[testKey] = testValue
				hpa.SetLabels(labels)

				By("expecting successful UPDATE of horizontal pod auto scaler")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &hpa)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for lease", func() {
		var leaseName string
		BeforeEach(func() {
			leaseName = testLease + "-" + utils.RandStr()
			l := coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      leaseName,
					Namespace: kubeSystemNS,
				},
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity:       pointer.String("test-holder-identity"),
					LeaseDurationSeconds: pointer.Int32(3600),
					RenewTime:            &metav1.MicroTime{Time: metav1.Now().Time},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &l)).Should(Succeed())
		})
		AfterEach(func() {
			l := coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      leaseName,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &l)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: l.Name, Namespace: l.Namespace}, &l))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE horizontal lease operation for user not in system:masters group", func() {
			l := coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testLease + "-" + utils.RandStr(),
					Namespace: kubeSystemNS,
				},
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity:       pointer.String("test-holder-identity"),
					LeaseDurationSeconds: pointer.Int32(3600),
					RenewTime:            &metav1.MicroTime{Time: metav1.Now().Time},
				},
			}
			By("expecting denial of operation CREATE of lease")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &l)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create lease call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Lease", types.NamespacedName{Name: l.Name, Namespace: l.Namespace})))
		})

		It("should deny UPDATE lease operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var l coordinationv1.Lease
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: kubeSystemNS}, &l)).Should(Succeed())
				l.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of lease")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &l)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update lease call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Lease", types.NamespacedName{Name: l.Name, Namespace: l.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE lease operation for user not in system:masters group", func() {
			var l coordinationv1.Lease
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: kubeSystemNS}, &l)).Should(Succeed())
			By("expecting denial of operation DELETE of lease")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &l)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete lease call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Lease", types.NamespacedName{Name: l.Name, Namespace: l.Namespace})))
		})

		It("should allow update operation on lease for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var l coordinationv1.Lease
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: kubeSystemNS}, &l)).Should(Succeed())
				By("update labels in lease")
				labels := make(map[string]string)
				labels[testKey] = testValue
				l.SetLabels(labels)

				By("expecting successful UPDATE of lease")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &l)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for end point slices", func() {
		var endPointName string
		BeforeEach(func() {
			endPointName = testEndPointSlice + "-" + utils.RandStr()
			protocol := corev1.ProtocolTCP
			e := discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      endPointName,
					Namespace: fleetSystemNS,
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Ports: []discoveryv1.EndpointPort{
					{
						Name:     pointer.String("http"),
						Protocol: &protocol,
						Port:     pointer.Int32(80),
					},
				},
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{"10.1.2.3"},
						Conditions: discoveryv1.EndpointConditions{
							Ready: pointer.Bool(true),
						},
						Hostname: pointer.String("pod-1"),
						NodeName: pointer.String("node-1"),
						Zone:     pointer.String("us-west2-a"),
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &e)).Should(Succeed())
		})
		AfterEach(func() {
			e := discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      endPointName,
					Namespace: fleetSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &e)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: e.Name, Namespace: e.Namespace}, &e))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE endpoint slice for user not in system:masters group", func() {
			protocol := corev1.ProtocolTCP
			e := discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      endPointName + "-" + utils.RandStr(),
					Namespace: fleetSystemNS,
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Ports: []discoveryv1.EndpointPort{
					{
						Name:     pointer.String("http"),
						Protocol: &protocol,
						Port:     pointer.Int32(80),
					},
				},
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{"10.1.2.3"},
						Conditions: discoveryv1.EndpointConditions{
							Ready: pointer.Bool(true),
						},
						Hostname: pointer.String("pod-1"),
						NodeName: pointer.String("node-1"),
						Zone:     pointer.String("us-west2-a"),
					},
				},
			}
			By("expecting denial of operation CREATE of endpoint slice")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &e)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create endpoint slice call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "EndpointSlice", types.NamespacedName{Name: e.Name, Namespace: e.Namespace})))
		})

		It("should deny UPDATE end point slice operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var e discoveryv1.EndpointSlice
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: endPointName, Namespace: fleetSystemNS}, &e)).Should(Succeed())
				e.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of endpoint slice")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &e)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update endpoint slice call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "EndpointSlice", types.NamespacedName{Name: e.Name, Namespace: e.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE endpoint slice operation for user not in system:masters group", func() {
			var e discoveryv1.EndpointSlice
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: endPointName, Namespace: fleetSystemNS}, &e)).Should(Succeed())
			By("expecting denial of operation DELETE of endpoint slice")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &e)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete endpoint slice call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "EndpointSlice", types.NamespacedName{Name: e.Name, Namespace: e.Namespace})))
		})

		It("should allow update operation on endpoint slice for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var e discoveryv1.EndpointSlice
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: endPointName, Namespace: fleetSystemNS}, &e)).Should(Succeed())
				By("update labels in endpoint slice")
				labels := make(map[string]string)
				labels[testKey] = testValue
				e.SetLabels(labels)

				By("expecting successful UPDATE of endpoint slice")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &e)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for ingress", func() {
		var ingressName string
		BeforeEach(func() {
			ingressName = testIngress + "-" + utils.RandStr()
			pathType := networkingv1.PathTypePrefix
			i := networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: memberNamespace.Name,
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: pointer.String("test-ingress-class"),
					Rules: []networkingv1.IngressRule{
						{
							Host: "http",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/testpath",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "test-service-backend",
													Port: networkingv1.ServiceBackendPort{
														Number: 80,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &i)).Should(Succeed())
		})
		AfterEach(func() {
			i := networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &i)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: i.Name, Namespace: i.Namespace}, &i))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE ingress for user not in system:masters group", func() {
			pathType := networkingv1.PathTypePrefix
			i := networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testIngress + "-" + utils.RandStr(),
					Namespace: memberNamespace.Name,
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: pointer.String("test-ingress-class"),
					Rules: []networkingv1.IngressRule{
						{
							Host: "http",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/testpath",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "test-service-backend",
													Port: networkingv1.ServiceBackendPort{
														Number: 80,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			By("expecting denial of operation CREATE of ingress")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &i)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ingress call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Ingress", types.NamespacedName{Name: i.Name, Namespace: i.Namespace})))
		})

		It("should deny UPDATE ingress operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var i networkingv1.Ingress
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: ingressName, Namespace: memberNamespace.Name}, &i)).Should(Succeed())
				i.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of ingress")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &i)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ingress call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Ingress", types.NamespacedName{Name: i.Name, Namespace: i.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE ingress operation for user not in system:masters group", func() {
			var i networkingv1.Ingress
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: ingressName, Namespace: memberNamespace.Name}, &i)).Should(Succeed())
			By("expecting denial of operation DELETE of ingress")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &i)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete ingress call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Ingress", types.NamespacedName{Name: i.Name, Namespace: i.Namespace})))
		})

		It("should allow update operation on ingress for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var i networkingv1.Ingress
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: ingressName, Namespace: memberNamespace.Name}, &i)).Should(Succeed())
				By("update labels in ingress")
				labels := make(map[string]string)
				labels[testKey] = testValue
				i.SetLabels(labels)

				By("expecting successful UPDATE of ingress")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &i)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for pod disruption budgets", func() {
		var pdbName string
		BeforeEach(func() {
			pdbName = testPodDisruptionBudget + "-" + utils.RandStr()
			pdb := policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pdbName,
					Namespace: kubeSystemNS,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{IntVal: 1},
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"test-key": "test-label"},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &pdb)).Should(Succeed())
		})
		AfterEach(func() {
			pdb := policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pdbName,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &pdb)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}, &pdb))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE pod disruption budget for user not in system:masters group", func() {
			pdb := policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodDisruptionBudget + "-" + utils.RandStr(),
					Namespace: kubeSystemNS,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{IntVal: 1},
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"test-key": "test-label"},
					},
				},
			}
			By("expecting denial of operation CREATE of pod disruption budget")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &pdb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create pod disruption budget call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "PodDisruptionBudget", types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace})))
		})

		It("should deny UPDATE pod disruption budget operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var pdb policyv1.PodDisruptionBudget
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: pdbName, Namespace: kubeSystemNS}, &pdb)).Should(Succeed())
				pdb.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of pod disruption budget")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &pdb)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update pod disruption budget call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "PodDisruptionBudget", types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE pod disruption budget operation for user not in system:masters group", func() {
			var pdb policyv1.PodDisruptionBudget
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: pdbName, Namespace: kubeSystemNS}, &pdb)).Should(Succeed())
			By("expecting denial of operation DELETE of pod disruption budget")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &pdb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete pod disruption budget call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "PodDisruptionBudget", types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace})))
		})

		It("should allow update operation on pod disruption budget for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var pdb policyv1.PodDisruptionBudget
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: pdbName, Namespace: kubeSystemNS}, &pdb)).Should(Succeed())
				By("update labels in pod disruption budget")
				labels := make(map[string]string)
				labels[testKey] = testValue
				pdb.SetLabels(labels)

				By("expecting successful UPDATE of pod disruption budget")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &pdb)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for CSI storage capacity", func() {
		var cscName string
		BeforeEach(func() {
			cscName = testCSICapacity + "-" + utils.RandStr()
			csc := storagev1.CSIStorageCapacity{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cscName,
					Namespace: fleetSystemNS,
				},
				StorageClassName: "test-storage",
			}
			Expect(HubCluster.KubeClient.Create(ctx, &csc)).Should(Succeed())
		})
		AfterEach(func() {
			csc := storagev1.CSIStorageCapacity{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cscName,
					Namespace: fleetSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &csc)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: csc.Name, Namespace: csc.Namespace}, &csc))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE CSI storage capacity for user not in system:masters group", func() {
			csc := storagev1.CSIStorageCapacity{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testCSICapacity + "-" + utils.RandStr(),
					Namespace: fleetSystemNS,
				},
				StorageClassName: "test-storage",
			}
			By("expecting denial of operation CREATE of CSI storage capacity")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &csc)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CSI storage capacity call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "CSIStorageCapacity", types.NamespacedName{Name: csc.Name, Namespace: csc.Namespace})))
		})

		It("should deny UPDATE CSI storage capacity operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var csc storagev1.CSIStorageCapacity
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cscName, Namespace: fleetSystemNS}, &csc)).Should(Succeed())
				csc.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of CSI storage capacity")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &csc)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CSI storage capacity call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "CSIStorageCapacity", types.NamespacedName{Name: csc.Name, Namespace: csc.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE CSI storage capacity operation for user not in system:masters group", func() {
			var csc storagev1.CSIStorageCapacity
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cscName, Namespace: fleetSystemNS}, &csc)).Should(Succeed())
			By("expecting denial of operation DELETE of CSI storage capacity")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &csc)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete CSI storage capacity call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "CSIStorageCapacity", types.NamespacedName{Name: csc.Name, Namespace: csc.Namespace})))
		})

		It("should allow update operation on CSI storage capacity for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var csc storagev1.CSIStorageCapacity
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cscName, Namespace: fleetSystemNS}, &csc)).Should(Succeed())
				By("update labels in CSI storage capacity")
				labels := make(map[string]string)
				labels[testKey] = testValue
				csc.SetLabels(labels)

				By("expecting successful UPDATE of CSI storage capacity")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &csc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})
})
