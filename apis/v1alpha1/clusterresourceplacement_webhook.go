/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apiErrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"go.goms.io/fleet/pkg/utils/informer"
)

var ResourceInformer informer.Manager
var restMapper meta.RESTMapper

func (c *ClusterResourcePlacement) SetupWebhookWithManager(mgr ctrl.Manager) error {
	restMapper = mgr.GetRESTMapper()
	return ctrl.NewWebhookManagedBy(mgr).
		For(c).
		Complete()
}

var _ webhook.Validator = &ClusterResourcePlacement{}

func (c *ClusterResourcePlacement) ValidateCreate() error {
	return ValidateClusterResourcePlacement(c)
}

func (c *ClusterResourcePlacement) ValidateUpdate(old runtime.Object) error {
	// TODO: validate changes against old if needed
	return ValidateClusterResourcePlacement(c)
}

func (c *ClusterResourcePlacement) ValidateDelete() error {
	// do nothing for delete request
	return nil
}

// ValidateClusterResourcePlacement validate a ClusterResourcePlacement object
func ValidateClusterResourcePlacement(clusterResourcePlacement *ClusterResourcePlacement) error {
	allErr := make([]error, 0)

	for _, selector := range clusterResourcePlacement.Spec.ResourceSelectors {
		if selector.LabelSelector != nil {
			if len(selector.Name) != 0 {
				allErr = append(allErr, fmt.Errorf("the labelSelector and name fields are mutually exclusive in selector %+v", selector))
			}
			if _, err := metav1.LabelSelectorAsSelector(selector.LabelSelector); err != nil {
				allErr = append(allErr, errors.Wrap(err, fmt.Sprintf("the labelSelector in resource selector %+v is invalid", selector)))
			}
		}
	}

	if clusterResourcePlacement.Spec.Policy != nil && clusterResourcePlacement.Spec.Policy.Affinity != nil &&
		clusterResourcePlacement.Spec.Policy.Affinity.ClusterAffinity != nil {
		for _, selector := range clusterResourcePlacement.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms {
			if _, err := metav1.LabelSelectorAsSelector(&selector.LabelSelector); err != nil {
				allErr = append(allErr, errors.Wrap(err, fmt.Sprintf("the labelSelector in cluster selector %+v is invalid", selector)))
			}
		}
	}

	// we leverage the informermanager in the changedetector controller to do the resource scope validation
	if ResourceInformer == nil {
		allErr = append(allErr, fmt.Errorf("cannot perform resource scope check for now, please retry"))
	} else {
		for _, selector := range clusterResourcePlacement.Spec.ResourceSelectors {
			gk := schema.GroupKind{
				Group: selector.Group,
				Kind:  selector.Kind,
			}

			restMapping, err := restMapper.RESTMapping(gk, selector.Version)
			if err != nil {
				allErr = append(allErr, errors.Wrap(err, fmt.Sprintf("failed to get GVR of GVK (%s/%s/%s), please retry if the GVK is valid", selector.Group, selector.Version, selector.Kind)))
				continue
			}

			if !ResourceInformer.IsClusterScopedResources(restMapping.Resource) {
				allErr = append(allErr, fmt.Errorf("the resource is not found in schema (please retry) or it is not a cluster scoped resource: %v", restMapping.Resource))
			}
		}
	}

	return apiErrors.NewAggregate(allErr)
}
