package framework

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

const (
	conditionTypeApplied = "Applied"
	timeout              = time.Second * 60
	interval             = time.Second * 1
)

func AddToManifestList(object runtime.Object, manifestList []workapi.Manifest) []workapi.Manifest {
	manifestList = append(manifestList, workapi.Manifest{RawExtension: runtime.RawExtension{Object: object}})
	return manifestList
}

func CreateWork(workName string, workNamespace string, hubCluster Cluster, manifestObjectList []workapi.Manifest) {
	ginkgo.By(fmt.Sprintf("Creating Work with Name %s, %s", workName, workNamespace))
	work := &workapi.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: workNamespace,
		},
		Spec: workapi.WorkSpec{
			Workload: workapi.WorkloadTemplate{
				Manifests: manifestObjectList,
			},
		},
	}

	err := hubCluster.KubeClient.Create(context.Background(), work)
	gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
}

func GetWork(workName string, workNamespace string, hubCluster Cluster) (*workapi.Work, error) {
	work := &workapi.Work{}
	err := hubCluster.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: workNamespace, Name: workName}, work)
	return work, err
}

func UpdateWork(work *workapi.Work, hubCluster Cluster) error {
	ginkgo.By(fmt.Sprintf("Updating work %s/%s in cluster %s", work.Namespace, work.Name, hubCluster.ClusterName))
	return hubCluster.KubeClient.Update(context.Background(), work)
}

func AddManifestToWork(manifestObject runtime.Object, hubCluster Cluster, workName, workNamespace string) error {
	currentWork, err := GetWork(workName, workNamespace, hubCluster)
	gomega.Expect(err).To(gomega.BeNil())

	currentWork.Spec.Workload.Manifests = append(currentWork.Spec.Workload.Manifests, workapi.Manifest{RawExtension: runtime.RawExtension{Object: manifestObject}})
	return UpdateWork(currentWork, hubCluster)
}

func ReplaceWorkManifest(manifestObject runtime.Object, hubCluster Cluster, workName, workNamespace string) error {
	currentWork, err := GetWork(workName, workNamespace, hubCluster)
	gomega.Expect(err).To(gomega.BeNil())
	currentWork.Spec.Workload.Manifests = []workapi.Manifest{
		{
			RawExtension: runtime.RawExtension{Object: manifestObject},
		},
	}
	return UpdateWork(currentWork, hubCluster)
}

func CheckIfAppliedConditionIsTrue(name string, namespace string, hubCluster *Cluster) {
	gomega.Eventually(func() bool {
		work, err := GetWork(name, namespace, *hubCluster)
		if err != nil {
			return false
		}

		if len(work.Status.Conditions) < 1 {
			return false
		}

		for _, condition := range work.Status.Conditions {
			if condition.Type == conditionTypeApplied && condition.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}, timeout, interval).Should(gomega.BeTrue())
}

func GetAppliedWork(workName string, workNamespace string, memberCluster Cluster) (*workapi.AppliedWork, error) {
	appliedWork := &workapi.AppliedWork{}
	err := memberCluster.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: workNamespace, Name: workName}, appliedWork)
	return appliedWork, err
}

func RemoveWork(workName string, workNamespace string, hubCluster Cluster) {
	work := &workapi.Work{ObjectMeta: metav1.ObjectMeta{Name: workName, Namespace: workNamespace}}
	ginkgo.By(fmt.Sprintf("Removing work %s in namespace %s", workName, workNamespace))
	err := hubCluster.KubeClient.Delete(context.Background(), work)
	if !apierrors.IsNotFound(err) {
		gomega.Expect(err).Should(gomega.SatisfyAny(gomega.Succeed()))
	}
}

func WaitWorkPresent(workName string, workNamespace string, hubCluster Cluster) {
	ginkgo.By(fmt.Sprintf("Waiting for Work to be created with Name %s, %s on hubCluster %s", workName, workNamespace, hubCluster.ClusterName))
	gomega.Eventually(func() error {
		_, err := GetWork(workName, workNamespace, hubCluster)
		return err
	}, timeout, interval).Should(gomega.BeNil())
}

func WaitAppliedWorkPresent(workName string, workNamespace string, memberCluster Cluster) {
	ginkgo.By(fmt.Sprintf("Waiting for AppliedWork to be created with Name %s, %s on memberCluster %s", workName, workNamespace, memberCluster.ClusterName))
	gomega.Eventually(func() error {
		_, err := GetAppliedWork(workName, workNamespace, memberCluster)
		return err
	}, timeout, interval).Should(gomega.BeNil())
}

func WaitAppliedWorkAbsent(workName string, workNamespace string, memberCluster Cluster) {
	ginkgo.By(fmt.Sprintf("Waiting for AppliedWork with Name %s, %s on memberCluster %s to be deleted", workName, workNamespace, memberCluster.ClusterName))
	gomega.Eventually(func() error {
		_, err := GetAppliedWork(workName, workNamespace, memberCluster)
		return err
	}, timeout, interval).ShouldNot(gomega.BeNil())
}
