/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package utils

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	// Lint check prohibits non "_test" ending files to have dot imports for ginkgo / gomega.
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/test/e2e/framework"
)

var (
	// PollInterval defines the interval time for a poll operation.
	PollInterval = 5 * time.Second
	// PollTimeout defines the time after which the poll operation times out.
	PollTimeout = 90 * time.Second
)

// NewMemberCluster return a new member cluster.
func NewMemberCluster(name string, heartbeat int32, state v1alpha1.ClusterState) *v1alpha1.MemberCluster {
	identity := rbacv1.Subject{
		Name:      name,
		Kind:      "ServiceAccount",
		Namespace: "fleet-system",
	}
	return &v1alpha1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.MemberClusterSpec{
			Identity:               identity,
			State:                  state,
			HeartbeatPeriodSeconds: heartbeat,
		},
	}
}

// NewInternalMemberCluster returns a new internal member cluster.
func NewInternalMemberCluster(name, namespace string) *v1alpha1.InternalMemberCluster {
	return &v1alpha1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

// NewServiceAccount returns a new service account.
func NewServiceAccount(name, namespace string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

// NewNamespace returns a new namespace.
func NewNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

// CreateMemberCluster creates MemberCluster and waits for MemberCluster to exist in the hub cluster.
func CreateMemberCluster(cluster framework.Cluster, mc *v1alpha1.MemberCluster) {
	ginkgo.By(fmt.Sprintf("Creating MemberCluster(%s)", mc.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), mc)
		gomega.Expect(err).Should(gomega.Succeed())
	})
	klog.Infof("Waiting for MemberCluster(%s) to be synced", mc.Name)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: mc.Name, Namespace: ""}, mc)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// UpdateMemberClusterState updates MemberCluster in the hub cluster.
func UpdateMemberClusterState(cluster framework.Cluster, mc *v1alpha1.MemberCluster, state v1alpha1.ClusterState) {
	err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: mc.Name, Namespace: ""}, mc)
	gomega.Expect(err).Should(gomega.Succeed())
	mc.Spec.State = state
	err = cluster.KubeClient.Update(context.TODO(), mc)
	gomega.Expect(err).Should(gomega.Succeed())
}

// DeleteMemberCluster deletes MemberCluster in the hub cluster.
func DeleteMemberCluster(cluster framework.Cluster, mc *v1alpha1.MemberCluster) {
	ginkgo.By(fmt.Sprintf("Deleting MemberCluster(%s)", mc.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), mc)
		gomega.Expect(err).Should(gomega.Succeed())
	})
}

// WaitConditionMemberCluster waits for MemberCluster to present on th hub cluster with a specific condition.
func WaitConditionMemberCluster(cluster framework.Cluster, mc *v1alpha1.MemberCluster, conditionType v1alpha1.MemberClusterConditionType, status metav1.ConditionStatus, customTimeout time.Duration) {
	klog.Infof("Waiting for MemberCluster(%s) condition(%s) status(%s) to be synced", mc.Name, conditionType, status)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: mc.Name, Namespace: ""}, mc)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		cond := mc.GetCondition(string(conditionType))
		return cond != nil && cond.Status == status
	}, customTimeout, PollInterval).Should(gomega.Equal(true))
}

// WaitInternalMemberCluster waits for InternalMemberCluster to present on th hub cluster.
func WaitInternalMemberCluster(cluster framework.Cluster, imc *v1alpha1.InternalMemberCluster) {
	klog.Infof("Waiting for InternalMemberCluster(%s) to be synced in the %s cluster", imc.Name, cluster.ClusterName)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// WaitConditionInternalMemberCluster waits for InternalMemberCluster to present on the hub cluster with a specific condition.
// Allowing custom timeout as for join cond it needs longer than defined PollTimeout for the member agent to finish joining.
func WaitConditionInternalMemberCluster(cluster framework.Cluster, imc *v1alpha1.InternalMemberCluster, conditionType v1alpha1.AgentConditionType, status metav1.ConditionStatus, customTimeout time.Duration) {
	klog.Infof("Waiting for InternalMemberCluster(%s) condition(%s) status(%s) to be synced in the %s cluster", imc.Name, conditionType, status, cluster.ClusterName)
	gomega.Eventually(func() bool {
		if err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc); err != nil {
			return false
		}
		cond := imc.GetConditionWithType(v1alpha1.MemberAgent, string(conditionType))
		return cond != nil && cond.Status == status
	}, customTimeout, PollInterval).Should(gomega.Equal(true))
}

// CreateClusterRole create cluster role in the hub cluster.
func CreateClusterRole(cluster framework.Cluster, cr *rbacv1.ClusterRole) {
	ginkgo.By(fmt.Sprintf("Creating ClusterRole (%s)", cr.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), cr)
		gomega.Expect(err).Should(gomega.Succeed())
	})
}

// WaitClusterRole waits for cluster roles to be created.
func WaitClusterRole(cluster framework.Cluster, cr *rbacv1.ClusterRole) {
	klog.Infof("Waiting for ClusterRole(%s) to be synced", cr.Name)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ""}, cr)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// DeleteClusterRole deletes cluster role on cluster.
func DeleteClusterRole(cluster framework.Cluster, cr *rbacv1.ClusterRole) {
	ginkgo.By(fmt.Sprintf("Deleting ClusterRole(%s)", cr.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), cr)
		gomega.Expect(err).Should(gomega.Succeed())
	})
}

// CreateClusterResourcePlacement created ClusterResourcePlacement and waits for ClusterResourcePlacement to exist in hub cluster.
func CreateClusterResourcePlacement(cluster framework.Cluster, crp *v1alpha1.ClusterResourcePlacement) {
	ginkgo.By(fmt.Sprintf("Creating ClusterResourcePlacement(%s)", crp.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), crp)
		gomega.Expect(err).Should(gomega.Succeed())
	})
	klog.Infof("Waiting for ClusterResourcePlacement(%s) to be synced", crp.Name)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: crp.Name, Namespace: ""}, crp)
		return err
	}, PollTimeout, PollInterval).ShouldNot(gomega.HaveOccurred())
}

// WaitConditionClusterResourcePlacement waits for ClusterResourcePlacement to present on th hub cluster with a specific condition.
func WaitConditionClusterResourcePlacement(cluster framework.Cluster, crp *v1alpha1.ClusterResourcePlacement,
	conditionName string, status metav1.ConditionStatus, customTimeout time.Duration) {
	klog.Infof("Waiting for ClusterResourcePlacement(%s) condition(%s) status(%s) to be synced", crp.Name, conditionName, status)
	gomega.Eventually(func() bool {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: crp.Name, Namespace: ""}, crp)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		cond := crp.GetCondition(conditionName)
		return cond != nil && cond.Status == status
	}, customTimeout, PollInterval).Should(gomega.Equal(true))
}

// DeleteClusterResourcePlacement is used delete ClusterResourcePlacement on the hub cluster.
func DeleteClusterResourcePlacement(cluster framework.Cluster, crp *v1alpha1.ClusterResourcePlacement) {
	ginkgo.By(fmt.Sprintf("Deleting ClusterResourcePlacement(%s)", crp.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), crp)
		gomega.Expect(err).Should(gomega.Succeed())
	})
}

// WaitWork waits for Work to be present on the hub cluster.
func WaitWork(ctx context.Context, cluster framework.Cluster, workName, workNamespace string) {
	name := types.NamespacedName{Name: workName, Namespace: workNamespace}

	klog.Infof("Waiting for Work(%s/%s) to be synced", workName, workNamespace)
	gomega.Eventually(func() error {
		var work workapi.Work

		return cluster.KubeClient.Get(ctx, name, &work)
	}, PollTimeout, PollInterval).Should(gomega.Succeed(), "Work %s not synced", name)
}

// CreateNamespace create namespace and waits for namespace to exist.
func CreateNamespace(cluster framework.Cluster, ns *corev1.Namespace) {
	ginkgo.By(fmt.Sprintf("Creating Namespace(%s)", ns.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), ns)
		gomega.Expect(err).Should(gomega.Succeed(), "Failed to create namespace %s", ns.Name)
	})
	klog.Infof("Waiting for Namespace(%s) to be synced", ns.Name)
	gomega.Eventually(func() error {
		err := cluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: ns.Name, Namespace: ""}, ns)

		return err
	}, PollTimeout, PollInterval).Should(gomega.Succeed())
}

// DeleteNamespace delete namespace.
func DeleteNamespace(cluster framework.Cluster, ns *corev1.Namespace) {
	ginkgo.By(fmt.Sprintf("Deleting Namespace(%s)", ns.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), ns)
		if err != nil && !apierrors.IsNotFound(err) {
			gomega.Expect(err).Should(gomega.Succeed())
		}
	})
}

// CreateServiceAccount create serviceaccount.
func CreateServiceAccount(cluster framework.Cluster, sa *corev1.ServiceAccount) {
	ginkgo.By(fmt.Sprintf("Creating ServiceAccount(%s)", sa.Name), func() {
		err := cluster.KubeClient.Create(context.TODO(), sa)
		gomega.Expect(err).Should(gomega.Succeed())
	})
}

// DeleteServiceAccount delete serviceaccount.
func DeleteServiceAccount(cluster framework.Cluster, sa *corev1.ServiceAccount) {
	ginkgo.By(fmt.Sprintf("Delete ServiceAccount(%s)", sa.Name), func() {
		err := cluster.KubeClient.Delete(context.TODO(), sa)
		gomega.Expect(err).Should(gomega.Succeed())
	})
}

// CreateWork creates Work object based on manifest given.
func CreateWork(ctx context.Context, hubCluster framework.Cluster, workName, workNamespace string, manifests []workapi.Manifest) {
	work := workapi.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: workNamespace,
		},
		Spec: workapi.WorkSpec{
			Workload: workapi.WorkloadTemplate{
				Manifests: manifests,
			},
		},
	}

	gomega.Expect(hubCluster.KubeClient.Create(ctx, &work)).Should(gomega.Succeed(), "Failed to create work %s in namespace %v", workName, workNamespace)
}

// DeleteWork deletes all works used in the current test.
func DeleteWork(ctx context.Context, hubCluster framework.Cluster, works []workapi.Work) {
	if len(works) > 0 {
		// Using index instead of work object itself due to lint check "Implicit memory aliasing in for loop."
		for i := range works {
			gomega.Expect(hubCluster.KubeClient.Delete(ctx, &works[i])).Should(gomega.SatisfyAny(gomega.Succeed(), &utils.NotFoundMatcher{}), "Deletion of work %s failed", works[i].Name)
		}
	}
}

// AddManifests adds manifests to be included within a Work.
func AddManifests(objects []runtime.Object, manifests []workapi.Manifest) []workapi.Manifest {
	for _, obj := range objects {
		rawObj, err := json.Marshal(obj)
		gomega.Expect(err).Should(gomega.Succeed(), "Failed to marshal object %+v", obj)
		manifests = append(manifests, workapi.Manifest{
			RawExtension: runtime.RawExtension{Object: obj, Raw: rawObj},
		})
	}
	return manifests
}

// RandomWorkName creates a work name in a correct format for e2e tests.
func RandomWorkName(length int) string {
	return "work" + rand.String(length)
}

// AlreadyExistMatcher matches the error to be already exist
type AlreadyExistMatcher struct {
}

// Match matches error.
func (matcher AlreadyExistMatcher) Match(actual interface{}) (success bool, err error) {
	if actual == nil {
		return false, nil
	}
	actualError := actual.(error)
	return apierrors.IsAlreadyExists(actualError), nil
}

// FailureMessage builds an error message.
func (matcher AlreadyExistMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "to be already exist")
}

// NegatedFailureMessage builds an error message.
func (matcher AlreadyExistMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "not to be already exist")
}

// GenerateSpecHash will generate Hash value used for annotation in the work-api for verification for each manifests given.
func GenerateSpecHash(manifests []workapi.Manifest) []string {
	specHashes := make([]string, len(manifests))
	for index, manifest := range manifests {
		unstructuredObj := &unstructured.Unstructured{}
		err := unstructuredObj.UnmarshalJSON(manifest.Raw)
		gomega.Expect(err).Should(gomega.Succeed(),
			"Invalid manifest with ordinal of %d", index)

		annotation := unstructuredObj.GetAnnotations()
		if annotation != nil {
			delete(annotation, manifestHashAnnotation)
			delete(annotation, lastAppliedConfigAnnotation)
			if len(annotation) == 0 {
				unstructuredObj.SetAnnotations(nil)
			} else {
				unstructuredObj.SetAnnotations(annotation)
			}
		}

		unstructuredObj.SetResourceVersion("")
		unstructuredObj.SetGeneration(0)
		unstructuredObj.SetUID("")
		unstructuredObj.SetSelfLink("")
		unstructuredObj.SetDeletionTimestamp(nil)
		unstructuredObj.SetManagedFields(nil)
		unstructured.RemoveNestedField(unstructuredObj.Object, "metadata", "creationTimestamp")
		unstructured.RemoveNestedField(unstructuredObj.Object, "status")
		// compute the sha256 hash of the remaining data

		jsonBytes, err := json.Marshal(unstructuredObj)
		gomega.Expect(err).Should(gomega.Succeed(),
			"Marshaling failed for manifest with ordinal of %d", index)
		specHashes = append(specHashes, fmt.Sprintf("%x", sha256.Sum256(jsonBytes)))
	}
	return specHashes
}

// GetConfigMap retrieves a configmap based on the name and namespace given.
func GetConfigMap(ctx context.Context, cluster framework.Cluster, name, namespace string) (corev1.ConfigMap, error) {
	cm, err := cluster.KubeClientSet.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return corev1.ConfigMap{}, err
	}
	return *cm, err
}
