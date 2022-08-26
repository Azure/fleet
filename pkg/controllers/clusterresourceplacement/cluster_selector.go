/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"fmt"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		return
	}
	// a fix list of clusters set
	if len(placement.Spec.Policy.ClusterNames) != 0 {
		klog.V(4).InfoS("use the cluster names provided as the list of cluster we select",
			"placement", placement.Name, "clusters", placement.Spec.Policy.ClusterNames)
		// TODO: filter by cluster health
		var selectedClusters []string
		for _, clusterName := range placement.Spec.Policy.ClusterNames {
			_, err = r.InformerManager.Lister(utils.MemberClusterGVR).Get(clusterName)
			if err != nil {
				klog.ErrorS(err, "cannot get the cluster", "clusterName", clusterName)
				if !apierrors.IsNotFound(err) {
					return nil, err
				}
			} else {
				selectedClusters = append(selectedClusters, clusterName)
			}
		}
		return selectedClusters, nil
	}

	// no Affinity or ClusterAffinity set
	if placement.Spec.Policy.Affinity == nil || placement.Spec.Policy.Affinity.ClusterAffinity == nil {
		clusterNames, err = r.listClusters(labels.Everything())
		if err != nil {
			return nil, err
		}
		klog.V(4).InfoS("we select all the available clusters in the fleet without a cluster affinity",
			"placement", placement.Name, "clusters", clusterNames)
		return
	}

	selectedClusters := make(map[string]bool)
	for _, clusterSelector := range placement.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms {
		selector, err := metav1.LabelSelectorAsSelector(&clusterSelector.LabelSelector)
		if err != nil {
			return nil, errors.Wrap(err, "cannot convert the label clusterSelector to a clusterSelector")
		}
		matchClusters, err := r.listClusters(selector)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("selector = %v", clusterSelector.LabelSelector))
		}
		klog.V(4).InfoS("selector matches some cluster", "clusterNum", len(matchClusters), "placement", placement.Name, "selector", clusterSelector.LabelSelector)
		for _, clusterName := range matchClusters {
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

	clusterNames := make([]string, 0)
	for _, obj := range objs {
		uObj := obj.DeepCopyObject().(*unstructured.Unstructured)
		var clusterObj fleetv1alpha1.MemberCluster
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.Object, &clusterObj)
		if err != nil {
			return nil, errors.Wrap(err, "cannot decode the member cluster object")
		}
		// only schedule the resource to an eligible cluster
		// TODO: check the health condition of the cluster when its aggregated
		joinCond := clusterObj.GetCondition(fleetv1alpha1.ConditionTypeMemberClusterJoin)
		if joinCond != nil && joinCond.Status == metav1.ConditionTrue && joinCond.ObservedGeneration == clusterObj.Generation {
			clusterNames = append(clusterNames, clusterObj.GetName())
		}
	}
	return clusterNames, nil
}
