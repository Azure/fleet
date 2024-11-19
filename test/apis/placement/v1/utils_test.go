package v1

import (
	"fmt"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

// cleanupMemberCluster removes finalizers (if any) from the member cluster, and
// wait until its final removal.
func cleanupMemberCluster(memberClusterName string) {
	// Remove the custom deletion blocker finalizer from the member cluster.
	Eventually(func() error {
		mcObj := &clusterv1beta1.MemberCluster{}
		err := hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName}, mcObj)
		if k8serrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}

		mcObj.Finalizers = []string{}
		return hubClient.Update(ctx, mcObj)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove custom deletion blocker finalizer from member cluster")

	// Wait until the member cluster is fully removed.
	Eventually(func() error {
		mcObj := &clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName}, mcObj); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("member cluster still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to fully delete member cluster")
}

func ensureMemberClusterAndRelatedResourcesDeletion(memberClusterName string) {
	// Delete the member cluster.
	mcObj := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberClusterName,
		},
	}
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, mcObj))).To(Succeed(), "Failed to delete member cluster")

	// Remove the finalizers on the member cluster, and wait until the member cluster is fully removed.
	cleanupMemberCluster(memberClusterName)

	// Verify that the member cluster and the namespace reserved for the member cluster has been removed.
	reservedNSName := fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
	Eventually(func() error {
		ns := corev1.Namespace{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: reservedNSName}, &ns); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("namespace still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove reserved namespace")
}
