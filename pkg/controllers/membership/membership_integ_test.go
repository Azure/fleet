package membership

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("Test Membership Controller", func() {
	var (
		ctx                       context.Context
		memberClusterName         string
		memberClusterNamespace    string
		membershipChan            chan v1alpha1.ClusterState
		internalMemberClusterChan chan v1alpha1.ClusterState
		r                         *Reconciler
	)

	BeforeEach(func() {
		memberClusterName = "fleet-rand-cluster-name"
		memberClusterNamespace = "fleet-rand-cluster-namespace"
		internalMemberClusterChan = make(chan v1alpha1.ClusterState)
		membershipChan = make(chan v1alpha1.ClusterState)

		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberClusterNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, &ns)).Should(SatisfyAny(Succeed(), &utils.AlreadyExistMatcher{}))
		if r == nil {
			r = NewReconciler(k8sClient, internalMemberClusterChan, membershipChan)
			err := r.SetupWithManager(mgr)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	AfterEach(func() {
		By("delete the member cluster namespace")
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberClusterNamespace,
			},
		}
		Expect(k8sClient.Delete(ctx, &ns)).Should(SatisfyAny(Succeed(), &utils.NotFoundMatcher{}))
		close(internalMemberClusterChan)
		close(membershipChan)
	})

	Context("Test the membership join flow", func() {
		BeforeEach(func() {
			internalMemberClusterChan <- v1alpha1.ClusterStateJoin
		})

		AfterEach(func() {
			close(internalMemberClusterChan)
		})

		It("test membership joined after internal member cluster CR has been created", func() {
			membership := v1alpha1.Membership{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: v1alpha1.MembershipSpec{State: v1alpha1.ClusterStateJoin},
			}
			Expect(k8sClient.Create(ctx, &membership)).Should(SatisfyAny(Succeed(), &utils.AlreadyExistMatcher{}))

			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
			})

			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())
		})
	})

	It("test membership joined after internal member cluster CR has been created", func() {
		membership := v1alpha1.Membership{
			ObjectMeta: metav1.ObjectMeta{
				Name:      memberClusterName,
				Namespace: memberClusterNamespace,
			},
			Spec: v1alpha1.MembershipSpec{State: v1alpha1.ClusterStateJoin},
		}
		Expect(k8sClient.Create(ctx, &membership)).Should(SatisfyAny(Succeed(), &utils.AlreadyExistMatcher{}))

		result, err := r.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      memberClusterName,
				Namespace: memberClusterNamespace,
			},
		})

		Expect(result).Should(Equal(ctrl.Result{RequeueAfter: time.Minute}))
		Expect(err).Should(Succeed())
	})
})
