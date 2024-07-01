/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package v1beta1

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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
)

var _ = Describe("Test MemberCluster Controller", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Second * 1
	)

	var (
		ctx                         context.Context
		mc                          *clusterv1beta1.MemberCluster
		ns                          corev1.Namespace
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
			mc = &clusterv1beta1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       "MemberCluster",
					APIVersion: clusterv1beta1.GroupVersion.Version,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
				Spec: clusterv1beta1.MemberClusterSpec{
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

			var role rbacv1.Role
			var roleBinding rbacv1.RoleBinding
			var imc clusterv1beta1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(utils.RoleNameFormat, memberClusterName), Namespace: namespaceName}, &role)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(utils.RoleBindingNameFormat, memberClusterName), Namespace: namespaceName}, &roleBinding)).Should(Succeed())
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, mc)).Should(Succeed())

			wantMC := clusterv1beta1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())

			By("simulate member agent updating internal member cluster status")
			now := metav1.Now()
			// Update the resource usage.
			imc.Status.ResourceUsage = clusterv1beta1.ResourceUsage{
				Capacity:        utils.NewResourceList(),
				Allocatable:     utils.NewResourceList(),
				Available:       utils.NewResourceList(),
				ObservationTime: now,
			}
			joinedCondition := metav1.Condition{
				Type:               string(clusterv1beta1.AgentJoined),
				Status:             metav1.ConditionTrue,
				Reason:             reasonMemberClusterJoined,
				ObservedGeneration: imc.GetGeneration(),
			}
			// Update the agent status.
			imc.SetConditionsWithType(clusterv1beta1.MemberAgent, joinedCondition)
			// Update the cluster properties.
			imc.Status.Properties = map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				clusterPropertyName1: {
					Value:           clusterPropertyValue1,
					ObservationTime: now,
				},
				clusterPropertyName2: {
					Value:           clusterPropertyValue2,
					ObservationTime: now,
				},
			}
			// Add conditions reported by the property provider.
			meta.SetStatusCondition(&imc.Status.Conditions, metav1.Condition{
				Type:    propertyProviderConditionType1,
				Status:  propertyProviderConditionStatus1,
				Reason:  propertyProviderConditionReason1,
				Message: propertyProviderConditionMessage1,
			})
			meta.SetStatusCondition(&imc.Status.Conditions, metav1.Condition{
				Type:    propertyProviderConditionType2,
				Status:  propertyProviderConditionStatus2,
				Reason:  propertyProviderConditionReason2,
				Message: propertyProviderConditionMessage2,
			})
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
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, mc)).Should(Succeed())
			joinCondition := mc.GetCondition(string(clusterv1beta1.ConditionTypeMemberClusterJoined))
			Expect(joinCondition).NotTo(BeNil())
			Expect(joinCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(joinCondition.Reason).To(Equal(reasonMemberClusterJoined))
		})

		It("should relay cluster resource usage + properties, and property provider conditions", func() {
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, mc)).Should(Succeed())

			// Compare the properties (if present).
			wantProperties := map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				clusterPropertyName1: {
					Value: clusterPropertyValue1,
				},
				clusterPropertyName2: {
					Value: clusterPropertyValue2,
				},
			}
			Expect(cmp.Diff(mc.Status.Properties, wantProperties, cmpopts.IgnoreTypes(time.Time{}))).To(BeEmpty())

			// Compare the resource usage.
			wantResourceUsage := clusterv1beta1.ResourceUsage{
				Capacity:    utils.NewResourceList(),
				Allocatable: utils.NewResourceList(),
				Available:   utils.NewResourceList(),
			}
			Expect(cmp.Diff(mc.Status.ResourceUsage, wantResourceUsage, cmpopts.IgnoreTypes(time.Time{}))).To(BeEmpty())

			// Compare the property provider conditions.
			wantConditions := []metav1.Condition{
				{
					Type:               propertyProviderConditionType1,
					Status:             propertyProviderConditionStatus1,
					Reason:             propertyProviderConditionReason1,
					Message:            propertyProviderConditionMessage1,
					ObservedGeneration: mc.GetGeneration(),
				},
				{
					Type:               propertyProviderConditionType2,
					Status:             propertyProviderConditionStatus2,
					Reason:             propertyProviderConditionReason2,
					Message:            propertyProviderConditionMessage2,
					ObservedGeneration: mc.GetGeneration(),
				},
			}
			conditions := []metav1.Condition{
				*meta.FindStatusCondition(mc.Status.Conditions, propertyProviderConditionType1),
				*meta.FindStatusCondition(mc.Status.Conditions, propertyProviderConditionType2),
			}
			Expect(cmp.Diff(conditions, wantConditions, cmpopts.IgnoreTypes(time.Time{}))).To(BeEmpty())
		})

		It("member cluster is marked as left after leave workflow is completed", func() {
			By("Delete member cluster to initiate leave workflow")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, mc)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, mc))

			By("trigger reconcile again to initiate leave workflow")
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			var imc clusterv1beta1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())
			Expect(imc.Spec.State).To(Equal(clusterv1beta1.ClusterStateLeave))

			By("verify mc joined status still be true")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, mc)).Should(Succeed())
			Expect(condition.IsConditionStatusTrue(mc.GetCondition(string(clusterv1beta1.ConditionTypeMemberClusterJoined)), mc.Generation)).Should(BeTrue())

			By("mark Internal Member Cluster as left")
			imcLeftCondition := metav1.Condition{
				Type:               string(clusterv1beta1.AgentJoined),
				Status:             metav1.ConditionFalse,
				Reason:             "InternalMemberClusterLeft",
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(clusterv1beta1.MemberAgent, imcLeftCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to mark member cluster as left")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{Requeue: true}))
			Expect(err).Should(Succeed())

			By("verify mc joined status is set to be false")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, mc)).Should(Succeed())
			Expect(condition.IsConditionStatusFalse(mc.GetCondition(string(clusterv1beta1.ConditionTypeMemberClusterJoined)), mc.Generation)).Should(BeTrue())

			// check the cluster namespace is being deleted. There is no namespace controller so it won't be removed
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)).Should(Succeed())
			Expect(ns.DeletionTimestamp != nil).Should(BeTrue())
		})

		It("remove label from namespace and trigger reconcile to patch the namespace with the new label", func() {
			By("remove fleet resource label from namespace")
			var mcNamespace corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &mcNamespace)).Should(Succeed())
			delete(mcNamespace.Labels, placementv1beta1.FleetResourceLabelKey)
			Expect(k8sClient.Update(ctx, &mcNamespace)).Should(Succeed())

			By("trigger reconcile again to patch member cluster namespace with fleet resource label")
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &mcNamespace)).Should(Succeed())
			Expect(mcNamespace.Labels[placementv1beta1.FleetResourceLabelKey]).Should(Equal("true"))
		})

		It("member cluster is deleting even with work objects after the leave workflow is completed", func() {
			By("Delete member cluster to initiate leave workflow")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, mc)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, mc))

			By("trigger reconcile again to initiate leave workflow")
			result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: memberClusterNamespacedName})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			var imc clusterv1beta1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())
			Expect(imc.Spec.State).To(Equal(clusterv1beta1.ClusterStateLeave))
			// check mc still exist
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, mc)).Should(Succeed())

			By("Create works in the cluster namespace")
			for i := 0; i < 10; i++ {
				work := placementv1beta1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("work%d", i),
						Namespace: namespaceName,
						Labels: map[string]string{
							placementv1beta1.ParentBindingLabel:               "resourceBindingName",
							placementv1beta1.CRPTrackingLabel:                 "parentCRP",
							placementv1beta1.ParentResourceSnapshotIndexLabel: "resourceIndexLabel",
						},
						Finalizers: []string{placementv1beta1.WorkFinalizer},
					},
				}
				Expect(k8sClient.Create(ctx, &work)).Should(Succeed())
			}

			By("mark Internal Member Cluster as left")
			imcLeftCondition := metav1.Condition{
				Type:               string(clusterv1beta1.AgentJoined),
				Status:             metav1.ConditionFalse,
				Reason:             "InternalMemberClusterLeft",
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(clusterv1beta1.MemberAgent, imcLeftCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to mark member cluster as left")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{Requeue: true}))
			Expect(err).Should(Succeed())

			By("check the workers objects don't have finalizer")
			for i := 0; i < 10; i++ {
				work := placementv1beta1.Work{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("work%d", i), Namespace: namespaceName}, &work)).Should(Succeed())
				Expect(work.Finalizers).Should(BeEmpty())
			}

			By("verify mc joined status is set to be false")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, mc)).Should(Succeed())
			Expect(condition.IsConditionStatusFalse(mc.GetCondition(string(clusterv1beta1.ConditionTypeMemberClusterJoined)), mc.Generation)).Should(BeTrue())

			// check the cluster namespace is being deleted. There is no namespace controller so it won't be removed
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)).Should(Succeed())
			Expect(ns.DeletionTimestamp != nil).Should(BeTrue())
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
			mc := &clusterv1beta1.MemberCluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       "MemberCluster",
					APIVersion: clusterv1beta1.GroupVersion.Version,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
				Spec: clusterv1beta1.MemberClusterSpec{
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
			var imc clusterv1beta1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(utils.RoleNameFormat, memberClusterName), Namespace: namespaceName}, &role)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(utils.RoleBindingNameFormat, memberClusterName), Namespace: namespaceName}, &roleBinding)).Should(Succeed())

			By("simulate member agent updating internal member cluster status")
			now := metav1.Now()
			// Update the resource usage.
			imc.Status.ResourceUsage = clusterv1beta1.ResourceUsage{
				Capacity:        utils.NewResourceList(),
				Allocatable:     utils.NewResourceList(),
				Available:       utils.NewResourceList(),
				ObservationTime: now,
			}
			// Update the agent status.
			joinedCondition := metav1.Condition{
				Type:               string(clusterv1beta1.AgentJoined),
				Status:             metav1.ConditionTrue,
				Reason:             reasonMemberClusterJoined,
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(clusterv1beta1.MemberAgent, joinedCondition)
			// Update the cluster properties.
			imc.Status.Properties = map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				clusterPropertyName1: {
					Value:           clusterPropertyValue1,
					ObservationTime: now,
				},
				clusterPropertyName2: {
					Value:           clusterPropertyValue2,
					ObservationTime: now,
				},
			}
			// Add conditions reported by the property provider.
			meta.SetStatusCondition(&imc.Status.Conditions, metav1.Condition{
				Type:    propertyProviderConditionType1,
				Status:  propertyProviderConditionStatus1,
				Reason:  propertyProviderConditionReason1,
				Message: propertyProviderConditionMessage1,
			})
			meta.SetStatusCondition(&imc.Status.Conditions, metav1.Condition{
				Type:    propertyProviderConditionType2,
				Status:  propertyProviderConditionStatus2,
				Reason:  propertyProviderConditionReason2,
				Message: propertyProviderConditionMessage2,
			})
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
			var imc clusterv1beta1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())

			var mc clusterv1beta1.MemberCluster
			By("checking mc status")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			wantMC := clusterv1beta1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionUnknown,
						Reason:             reasonMemberClusterUnknown,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               propertyProviderConditionType1,
						Status:             propertyProviderConditionStatus1,
						Reason:             propertyProviderConditionReason1,
						Message:            propertyProviderConditionMessage1,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               propertyProviderConditionType2,
						Status:             propertyProviderConditionStatus2,
						Reason:             propertyProviderConditionReason2,
						Message:            propertyProviderConditionMessage2,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				Properties:    imc.Status.Properties,
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())

			By("simulate multiClusterService agent updating internal member cluster status as joined")
			joinedCondition := metav1.Condition{
				Type:               string(clusterv1beta1.AgentJoined),
				Status:             metav1.ConditionTrue,
				Reason:             reasonMemberClusterJoined,
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(clusterv1beta1.MultiClusterServiceAgent, joinedCondition)
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

			wantMC = clusterv1beta1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionUnknown,
						Reason:             reasonMemberClusterUnknown,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               propertyProviderConditionType1,
						Status:             propertyProviderConditionStatus1,
						Reason:             propertyProviderConditionReason1,
						Message:            propertyProviderConditionMessage1,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               propertyProviderConditionType2,
						Status:             propertyProviderConditionStatus2,
						Reason:             propertyProviderConditionReason2,
						Message:            propertyProviderConditionMessage2,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				Properties:    imc.Status.Properties,
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())

			By("simulate serviceExportImport agent updating internal member cluster status as unknown")
			joinedCondition = metav1.Condition{
				Type:               string(clusterv1beta1.AgentJoined),
				Status:             metav1.ConditionUnknown,
				Reason:             reasonMemberClusterUnknown,
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(clusterv1beta1.ServiceExportImportAgent, joinedCondition)
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

			wantMC = clusterv1beta1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionUnknown,
						Reason:             reasonMemberClusterUnknown,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               propertyProviderConditionType1,
						Status:             propertyProviderConditionStatus1,
						Reason:             propertyProviderConditionReason1,
						Message:            propertyProviderConditionMessage1,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               propertyProviderConditionType2,
						Status:             propertyProviderConditionStatus2,
						Reason:             propertyProviderConditionReason2,
						Message:            propertyProviderConditionMessage2,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				Properties:    imc.Status.Properties,
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())

			By("simulate serviceExportImport agent updating internal member cluster status as joined")
			joinedCondition = metav1.Condition{
				Type:               string(clusterv1beta1.AgentJoined),
				Status:             metav1.ConditionTrue,
				Reason:             reasonMemberClusterJoined,
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(clusterv1beta1.ServiceExportImportAgent, joinedCondition)
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

			wantMC = clusterv1beta1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterJoined,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               propertyProviderConditionType1,
						Status:             propertyProviderConditionStatus1,
						Reason:             propertyProviderConditionReason1,
						Message:            propertyProviderConditionMessage1,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               propertyProviderConditionType2,
						Status:             propertyProviderConditionStatus2,
						Reason:             propertyProviderConditionReason2,
						Message:            propertyProviderConditionMessage2,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				Properties:    imc.Status.Properties,
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			Expect(cmp.Diff(wantMC, mc.Status, ignoreOption)).Should(BeEmpty())
		})

		It("member cluster is deleted after leave workflow is completed", func() {
			By("Delete member cluster to initiate leave workflow")
			var mc clusterv1beta1.MemberCluster
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &mc))

			By("trigger reconcile again to initiate leave workflow")
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			By("getting imc status")
			var imc clusterv1beta1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())
			Expect(imc.Spec.State).To(Equal(clusterv1beta1.ClusterStateLeave))

			By("member agent marks Internal Member Cluster as left")
			imcLeftCondition := metav1.Condition{
				Type:               string(clusterv1beta1.AgentJoined),
				Status:             metav1.ConditionFalse,
				Reason:             "InternalMemberClusterLeft",
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(clusterv1beta1.MemberAgent, imcLeftCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to initiate leave workflow")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			By("checking mc status")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			wantMC := clusterv1beta1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
						Status: metav1.ConditionTrue,
						Reason: reasonMemberClusterReadyToJoin,
					},
					{
						Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
						Status: metav1.ConditionUnknown,
						Reason: reasonMemberClusterUnknown,
					},
					{
						Type:               propertyProviderConditionType1,
						Status:             propertyProviderConditionStatus1,
						Reason:             propertyProviderConditionReason1,
						Message:            propertyProviderConditionMessage1,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               propertyProviderConditionType2,
						Status:             propertyProviderConditionStatus2,
						Reason:             propertyProviderConditionReason2,
						Message:            propertyProviderConditionMessage2,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				Properties:    imc.Status.Properties,
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			options := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration")
			// ignore the ObservedGeneration here cause controller won't update the ReadyToJoin condition.
			Expect(cmp.Diff(wantMC, mc.Status, options)).Should(BeEmpty(), "mc status mismatch, (-want, +got)")

			By("multiClusterService agent marks Internal Member Cluster as joined")
			imcLeftCondition = metav1.Condition{
				Type:               string(clusterv1beta1.AgentJoined),
				Status:             metav1.ConditionTrue,
				Reason:             "InternalMemberClusterJoined",
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(clusterv1beta1.MultiClusterServiceAgent, imcLeftCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to initiate leave workflow")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Succeed())

			By("checking mc status")
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

			wantMC = clusterv1beta1.MemberClusterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
						Status:             metav1.ConditionTrue,
						Reason:             reasonMemberClusterReadyToJoin,
						ObservedGeneration: mc.GetGeneration(), // should be old observedGeneration
					},
					{
						Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
						Status:             metav1.ConditionUnknown,
						Reason:             reasonMemberClusterUnknown,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               propertyProviderConditionType1,
						Status:             propertyProviderConditionStatus1,
						Reason:             propertyProviderConditionReason1,
						Message:            propertyProviderConditionMessage1,
						ObservedGeneration: mc.GetGeneration(),
					},
					{
						Type:               propertyProviderConditionType2,
						Status:             propertyProviderConditionStatus2,
						Reason:             propertyProviderConditionReason2,
						Message:            propertyProviderConditionMessage2,
						ObservedGeneration: mc.GetGeneration(),
					},
				},
				Properties:    imc.Status.Properties,
				ResourceUsage: imc.Status.ResourceUsage,
				AgentStatus:   imc.Status.AgentStatus,
			}
			// ignore the ObservedGeneration here cause controller won't update the ReadyToJoin condition.
			Expect(cmp.Diff(wantMC, mc.Status, options)).Should(BeEmpty())

			By("multiClusterService and serviceExportImport agent mark Internal Member Cluster as left")
			imcLeftCondition = metav1.Condition{
				Type:               string(clusterv1beta1.AgentJoined),
				Status:             metav1.ConditionFalse,
				Reason:             "InternalMemberClusterLeft",
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(clusterv1beta1.MultiClusterServiceAgent, imcLeftCondition)

			imcLeftCondition = metav1.Condition{
				Type:               string(clusterv1beta1.AgentJoined),
				Status:             metav1.ConditionFalse,
				Reason:             "InternalMemberClusterLeft",
				ObservedGeneration: imc.GetGeneration(),
			}
			imc.SetConditionsWithType(clusterv1beta1.ServiceExportImportAgent, imcLeftCondition)
			Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("trigger reconcile again to initiate leave workflow")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{Requeue: true}))
			Expect(err).Should(Succeed())

			// check the cluster namespace is being deleted. There is no namespace controller so it won't be removed
			By("checking namespace is deleting")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)).Should(Succeed())
			Expect(ns.DeletionTimestamp != nil).Should(BeTrue())
		})
	})
})
