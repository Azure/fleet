package e2e

import (
	"errors"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	testUser = "test-user"
)

var (
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
		Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceDeniedFormat, testUser, testGroups, admissionv1.Create, &mcGVK, "", types.NamespacedName{Name: mc.Name})))
	})
})

var _ = Describe("fleet guard rail tests for allow/deny MC UPDATE, DELETE operations", Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		createMemberClusterResource(mcName)
	})

	AfterAll(func() {
		deleteMemberClusterResource(mcName)
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
			g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceDeniedFormat, testUser, testGroups, admissionv1.Update, &mcGVK, "", types.NamespacedName{Name: mc.Name})))
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
		Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceDeniedFormat, testUser, testGroups, admissionv1.Delete, &mcGVK, "", types.NamespacedName{Name: mc.Name})))
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
