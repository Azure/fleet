/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"fmt"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

// selectClusters selected the resources according to the placement resourceSelectors and
// update the results in its status
func (r *Reconciler) selectClusters(placement *fleetv1alpha1.ClusterResourcePlacement) (clusterNames []string, err error) {
	defer func() {
		if err == nil {
			// Set the status
			placement.Status.TargetClusters = clusterNames
		}
	}()
	// no policy set
	if placement.Spec.Policy == nil {
		clusterNames, err = r.listClusters(labels.Everything())
		if err != nil {
			return nil, err
		}
		klog.V(4).InfoS("we select all the available clusters in the fleet without a policy",
			"placement", placement.Name, "clusters", clusterNames)
		return clusterNames, nil
	}
	// a fix list of clusters set
	if len(placement.Spec.Policy.ClusterNames) != 0 {
		klog.V(4).InfoS("use the cluster names provided as the list of cluster we select",
			"placement", placement.Name, "clusters", placement.Spec.Policy.ClusterNames)
		return placement.Spec.Policy.ClusterNames, nil
	}

	// no Affinity or ClusterAffinity set
	if placement.Spec.Policy.Affinity == nil || placement.Spec.Policy.Affinity.ClusterAffinity == nil {
		clusterNames, err = r.listClusters(labels.Everything())
		if err != nil {
			return nil, err
		}
		klog.V(4).InfoS("we select all the available clusters in the fleet without a cluster affinity",
			"placement", placement.Name, "clusters", clusterNames)
		return clusterNames, nil
	}

	selectedClusters := make(map[string]bool)
	for _, clusterSelector := range placement.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms {
		selector, err := metav1.LabelSelectorAsSelector(&clusterSelector.LabelSelector)
		if err != nil {
			return nil, errors.Wrap(err, "cannot convert the label clusterSelector to a clusterSelector")
		}
		clusterNames, err := r.listClusters(selector)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("selector = %v", clusterSelector.LabelSelector))
		}
		for _, clusterName := range clusterNames {
			selectedClusters[clusterName] = true
		}
	}

	for cluster := range selectedClusters {
		klog.V(4).InfoS("matched a cluster", "cluster", cluster, "placement", placement.Name)
		clusterNames = append(clusterNames, cluster)
	}
	return clusterNames, nil
}

// listClusters retrieves the clusters according to its label selector, this will hit the informer cache.
func (r *Reconciler) listClusters(labelSelector labels.Selector) ([]string, error) {
	objs, err := r.InformerManager.Lister(utils.MemberClusterGVR).List(labelSelector)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the clusters according to obj label selector")
	}

	clusterNames := make([]string, len(objs))
	for i, obj := range objs {
		uObj := obj.DeepCopyObject().(*unstructured.Unstructured)
		var clusterObj fleetv1alpha1.MemberCluster
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.Object, &clusterObj)
		if err != nil {
			return nil, errors.Wrap(err, "cannot decode the member cluster object")
		}
		// only schedule the resource to a joined cluster
		// TODO: check the health/condition of the cluster too
		if clusterObj.Spec.State == fleetv1alpha1.ClusterStateJoin {
			clusterNames[i] = clusterObj.GetName()
		}
	}
	return clusterNames, nil
}
