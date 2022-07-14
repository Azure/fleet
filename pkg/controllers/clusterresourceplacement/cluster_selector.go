package clusterresourceplacement

import (
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

// selectClusters selected the resources according to the placement resourceSelectors and
// update the results in its status
func (r *Reconciler) selectClusters(placement *fleetv1alpha1.ClusterResourcePlacement) ([]string, error) {
	if len(placement.Spec.Policy.ClusterNames) != 0 {
		klog.V(4).InfoS("use the cluster names provided as the list of cluster we select", "placement", placement.Name)
		return placement.Spec.Policy.ClusterNames, nil
	}
	selectedClusters := make(map[string]bool)
	for _, clusterSelector := range placement.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms {
		clusterNames, err := r.listClusters(&clusterSelector.LabelSelector)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("selector = %v", clusterSelector.LabelSelector))
		}
		for _, clusterName := range clusterNames {
			selectedClusters[clusterName] = true
		}
	}
	clusterNames := make([]string, 0)
	for cluster := range selectedClusters {
		klog.V(5).InfoS("matched a cluster", "cluster", cluster, "placement", placement.Name)
		clusterNames = append(clusterNames, cluster)
	}
	return clusterNames, nil
}

// listClusters retrieves the clusters according to its label selector, this will hit the informer cache.
func (r *Reconciler) listClusters(labelSelector *metav1.LabelSelector) ([]string, error) {
	if !r.memberClusterInformerSynced && !r.InformerManager.IsInformerSynced(utils.MemberClusterGVR) {
		return nil, fmt.Errorf("informer cache for memberCluster is not synced yet")
	}
	r.memberClusterInformerSynced = true

	clusterSelector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, errors.Wrap(err, "cannot convert the label clusterSelector to a clusterSelector")
	}
	clusterNames := make([]string, 0)
	objs, err := r.InformerManager.Lister(utils.MemberClusterGVR).List(clusterSelector)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the clusters according to obj label selector")
	}
	for _, obj := range objs {
		clusterObj, err := meta.Accessor(obj)
		if err != nil {
			return nil, errors.Wrap(err, "cannot get the name of a cluster object")
		}
		clusterNames = append(clusterNames, clusterObj.GetName())
	}
	return clusterNames, nil
}
