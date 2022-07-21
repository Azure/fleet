package informerfactory

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getListOption(labels map[string]string) *client.ListOptions {
	options := &client.ListOptions{}
	client.MatchingLabels(labels).ApplyToList(options)
	return options
}

func selectClusters(placement *fleetv1alpha1.ClusterResourcePlacement, client client.Client) ([]string, error) {
	if len(placement.Spec.Policy.ClusterNames) != 0 {
		return placement.Spec.Policy.ClusterNames, nil
	}
	selectedClusters := make(map[string]bool)
	for _, clusterSelector := range placement.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms {
		memberClusterList := &fleetv1alpha1.MemberClusterList{}
		err := client.List(context.TODO(), memberClusterList, getListOption(clusterSelector.LabelSelector.MatchLabels))
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("selector = %v", clusterSelector.LabelSelector))
		}
		for _, memberCluster := range memberClusterList.Items {
			selectedClusters[memberCluster.ClusterName] = true
		}
	}
	clusterNames := make([]string, 0)
	for cluster := range selectedClusters {
		klog.Infof("matched a cluster", "cluster", cluster, "placement", placement.Name)
		clusterNames = append(clusterNames, cluster)
	}
	return clusterNames, nil
}

func listAllResourcesFromResourceSelectors(informerFactory InformerFactory, resourceSelectors []fleetv1alpha1.ClusterResourceSelector) ([]interface{}, error) {
	rs := make([]interface{}, 0)
	for _, selector := range resourceSelectors {
		s, _ := metav1.LabelSelectorAsSelector(selector.LabelSelector)
		if selector.Kind == "Namespace" {
			// selector selects namespace
			objects, err := informerFactory.ListFromNamespace(selector.Name, s)
			if err != nil {
				return rs, err
			}
			rs = append(rs, objects...)
		} else {
			// selector selects cluster scoped object
			objects, err := informerFactory.ListFromNonNamespaced(selector.Name, s)
			if err != nil {
				return rs, err
			}
			rs = append(rs, objects...)
		}
	}

	return rs, nil
}
