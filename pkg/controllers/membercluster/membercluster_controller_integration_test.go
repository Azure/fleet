/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package membercluster

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

var _ = Describe("Test MemberCluster Controller", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Second * 1
	)

	var (
		ctx                         context.Context
		memberClusterName           string
		namespaceName               string
		memberClusterNamespacedName types.NamespacedName
		r                           *Reconciler
		ignoreOption                = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
	)

	Context("Test membercluster controller without networking agents", func() {
		BeforeEach(func() {
			ctx = context.Background()
			memberClusterName = utils.RandStr()
			namespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
			memberClusterNamespacedName = types.NamespacedName{
				Name: memberClusterName,
			}

			By("create the member cluster reconciler")
			r = &Reconciler{
				Client: k8sClient,
			}
			err := r.SetupWithManager(mgr)
			Expect(err).Should(Succeed())

			By("create member cluster for join")
			mc := &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       "MemberCluster",
					APIVersion: fleetv1alpha1.GroupVersion.Version,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateJoin,
					Identity: rbacv1.Subject{
						Kind: rbacv1.ServiceAccountKind,
						Name: "hub-access",
					},
				},
			}
			Expect(k8sClient.Create(ctx, mc)).Should(Succeed())

			By("trigger reconcile to initiate the join workflow")
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			var ns corev1.Namespace
			var role rbacv1.Role
			var roleBinding rbacv1.RoleBinding
			var imc fleetv1alpha1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(utils.RoleNameFormat, memberClusterName), Namespace: namespaceName}, &role)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(utils.RoleBindingNameFormat, memberClusterName), Namespace: namespaceName}, &roleBinding)).Should(Succeed())
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, mc)).Should(Succeed())

			wantMC := fleetv1alpha1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())

			By("simulate member agent updating internal member cluster status")
			imc.Status.ResourceUsage.Capacity = utils.NewResourceList()
			imc.Status.ResourceUsage.Allocatable = utils.NewResourceList()
			imc.Status.ResourceUsage.ObservationTime = metav1.Now()
			joinedCondition := metav1.Condition{
				Type:               string(fleetv1alpha1.AgentJoined),
				Status:             metav1.ConditionTrue,
				Reason:             reasonMemberClusterJoined,
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(fleetv1alpha1.MemberAgent, joinedCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to update member cluster status to joined")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())
		})

		AfterEach(func() {
			var ns corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)).Should(Succeed())

			By("Deleting the namespace")
			Eventually(func() error {
				return k8sClient.Delete(ctx, &ns)
			}, timeout, interval).Should(SatisfyAny(Succeed(), &utils.NotFoundMatcher{}))
		})

		It("should create namespace, role, role binding and internal member cluster & mark member cluster as joined", func() {
			var mc fleetv1alpha1.MemberCluster
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			joinCondition := mc.GetCondition(string(fleetv1alpha1.ConditionTypeMemberClusterJoined))
			Expect(joinCondition).NotTo(BeNil())
			Expect(joinCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(joinCondition.Reason).To(Equal(reasonMemberClusterJoined))
		})

		It("member cluster is marked as left after leave workflow is completed", func() {
			By("Update member cluster's spec to leave")
			var mc fleetv1alpha1.MemberCluster
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())
			mc.Spec.State = fleetv1alpha1.ClusterStateLeave
			Expect(k8sClient.Update(ctx, &mc))

			By("trigger reconcile again to initiate leave workflow")
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			var imc fleetv1alpha1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())
			Expect(imc.Spec.State).To(Equal(fleetv1alpha1.ClusterStateLeave))

			By("mark Internal Member Cluster as left")
			imcLeftCondition := metav1.Condition{
				Type:               string(fleetv1alpha1.AgentJoined),
				Status:             metav1.ConditionFalse,
				Reason:             "InternalMemberClusterLeft",
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(fleetv1alpha1.MemberAgent, imcLeftCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to mark member cluster as left")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			wantMC := fleetv1alpha1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionFalse,
						Reason:             reasonMemberClusterNotReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionFalse,
						Reason:             reasonMemberClusterLeft,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())
		})

		It("remove label from namespace and trigger reconcile to patch the namespace with the new label", func() {
			By("remove label from namespace")
			var mcNamespace corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &mcNamespace)).Should(Succeed())
			delete(mcNamespace.Labels, fleetv1beta1.FleetResourceLabelKey)
			Expect(k8sClient.Update(ctx, &mcNamespace)).Should(Succeed())

			By("trigger reconcile again to patch member cluster namespace with new label")
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &mcNamespace)).Should(Succeed())
			Expect(mcNamespace.Labels[fleetv1beta1.FleetResourceLabelKey]).Should(Equal("true"))
		})
	})

	Context("Test membercluster controller with enabling networking agents", func() {
		BeforeEach(func() {
			ctx = context.Background()
			memberClusterName = utils.RandStr()
			namespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
			memberClusterNamespacedName = types.NamespacedName{
				Name: memberClusterName,
			}

			By("create the member cluster reconciler")
			r = &Reconciler{
				Client:                  k8sClient,
				NetworkingAgentsEnabled: true,
			}
			err := r.SetupWithManager(mgr)
			Expect(err).Should(Succeed())

			By("create member cluster for join")
			mc := &fleetv1alpha1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       "MemberCluster",
					APIVersion: fleetv1alpha1.GroupVersion.Version,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateJoin,
					Identity: rbacv1.Subject{
						Kind: rbacv1.ServiceAccountKind,
						Name: "hub-access",
					},
				},
			}
			Expect(k8sClient.Create(ctx, mc)).Should(Succeed())

			By("trigger reconcile to initiate the join workflow")
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			var ns corev1.Namespace
			var role rbacv1.Role
			var roleBinding rbacv1.RoleBinding
			var imc fleetv1alpha1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(utils.RoleNameFormat, memberClusterName), Namespace: namespaceName}, &role)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(utils.RoleBindingNameFormat, memberClusterName), Namespace: namespaceName}, &roleBinding)).Should(Succeed())

			By("simulate member agent updating internal member cluster status")
			imc.Status.ResourceUsage.Capacity = corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("100m"),
			}
			imc.Status.ResourceUsage.Allocatable = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			}
			imc.Status.ResourceUsage.ObservationTime = metav1.Now()
			joinedCondition := metav1.Condition{
				Type:               string(fleetv1alpha1.AgentJoined),
				Status:             metav1.ConditionTrue,
				Reason:             reasonMemberClusterJoined,
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(fleetv1alpha1.MemberAgent, joinedCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to update member cluster status to joined")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())
		})

		AfterEach(func() {
			var ns corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)).Should(Succeed())

			By("Deleting the namespace")
			Eventually(func() error {
				return k8sClient.Delete(ctx, &ns)
			}, timeout, interval).Should(SatisfyAny(Succeed(), &utils.NotFoundMatcher{}))
		})

		It("should create namespace, role, role binding and internal member cluster & mark member cluster as joined", func() {
			By("getting imc status")
			var imc fleetv1alpha1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())

			var mc fleetv1alpha1.MemberCluster
			By("checking mc status")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			wantMC := fleetv1alpha1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionUnknown,
						Reason:             reasonMemberClusterUnknown,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())

			By("simulate multiClusterService agent updating internal member cluster status as joined")
			joinedCondition := metav1.Condition{
				Type:               string(fleetv1alpha1.AgentJoined),
				Status:             metav1.ConditionTrue,
				Reason:             reasonMemberClusterJoined,
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(fleetv1alpha1.MultiClusterServiceAgent, joinedCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to update member cluster status")
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			By("getting imc status")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())

			By("checking mc status")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			wantMC = fleetv1alpha1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionUnknown,
						Reason:             reasonMemberClusterUnknown,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())

			By("simulate serviceExportImport agent updating internal member cluster status as unknown")
			joinedCondition = metav1.Condition{
				Type:               string(fleetv1alpha1.AgentJoined),
				Status:             metav1.ConditionUnknown,
				Reason:             reasonMemberClusterUnknown,
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(fleetv1alpha1.ServiceExportImportAgent, joinedCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to update member cluster status")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			By("getting imc status")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())

			By("checking mc status")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			wantMC = fleetv1alpha1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionUnknown,
						Reason:             reasonMemberClusterUnknown,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())

			By("simulate serviceExportImport agent updating internal member cluster status as joined")
			joinedCondition = metav1.Condition{
				Type:               string(fleetv1alpha1.AgentJoined),
				Status:             metav1.ConditionTrue,
				Reason:             reasonMemberClusterJoined,
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(fleetv1alpha1.ServiceExportImportAgent, joinedCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to update member cluster status")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			By("getting imc status")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())

			By("checking mc status")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			wantMC = fleetv1alpha1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterJoined,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())
		})

		It("member cluster is marked as left after leave workflow is completed", func() {
			By("Update member cluster's spec to leave")
			var mc fleetv1alpha1.MemberCluster
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())
			mc.Spec.State = fleetv1alpha1.ClusterStateLeave
			Expect(k8sClient.Update(ctx, &mc))

			By("trigger reconcile again to initiate leave workflow")
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			var imc fleetv1alpha1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())
			Expect(imc.Spec.State).To(Equal(fleetv1alpha1.ClusterStateLeave))

			By("getting imc status")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())

			By("member agent marks Internal Member Cluster as left")
			imcLeftCondition := metav1.Condition{
				Type:               string(fleetv1alpha1.AgentJoined),
				Status:             metav1.ConditionFalse,
				Reason:             "InternalMemberClusterLeft",
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(fleetv1alpha1.MemberAgent, imcLeftCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to initiate leave workflow")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			By("checking mc status")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			wantMC := fleetv1alpha1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(), // should be old observedGeneration
					},
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionUnknown,
						Reason:             reasonMemberClusterUnknown,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			options := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration")
			// ignore the ObservedGeneration here cause controller won't update the ReadyToJoin condition.
			Expect(cmp.Diff(wantMC, mc.Status, options)).Should(BeEmpty())

			By("multiClusterService agent marks Internal Member Cluster as joined")
			imcLeftCondition = metav1.Condition{
				Type:               string(fleetv1alpha1.AgentJoined),
				Status:             metav1.ConditionTrue,
				Reason:             "InternalMemberClusterJoined",
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(fleetv1alpha1.MultiClusterServiceAgent, imcLeftCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to initiate leave workflow")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			By("checking mc status")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			wantMC = fleetv1alpha1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(), // should be old observedGeneration
					},
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionUnknown,
						Reason:             reasonMemberClusterUnknown,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			// ignore the ObservedGeneration here cause controller won't update the ReadyToJoin condition.
			Expect(cmp.Diff(wantMC, mc.Status, options)).Should(BeEmpty())

			By("multiClusterService and serviceExportImport agent mark Internal Member Cluster as left")
			imcLeftCondition = metav1.Condition{
				Type:               string(fleetv1alpha1.AgentJoined),
				Status:             metav1.ConditionFalse,
				Reason:             "InternalMemberClusterLeft",
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(fleetv1alpha1.MultiClusterServiceAgent, imcLeftCondition)

			imcLeftCondition = metav1.Condition{
				Type:               string(fleetv1alpha1.AgentJoined),
				Status:             metav1.ConditionFalse,
				Reason:             "InternalMemberClusterLeft",
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(fleetv1alpha1.ServiceExportImportAgent, imcLeftCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to initiate leave workflow")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			By("checking mc status")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			wantMC = fleetv1alpha1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionFalse,
						Reason:             reasonMemberClusterNotReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionFalse,
						Reason:             reasonMemberClusterLeft,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())
		})
	})

})
