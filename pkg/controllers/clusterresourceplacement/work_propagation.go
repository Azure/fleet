/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	apiErrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
	workController "sigs.k8s.io/work-api/pkg/controllers"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	LastUpdateAnnotationKey = "work.fleet.azure.com/last-update-time"
	SpecHashAnnotationKey   = "work.fleet.azure.com/spec-hash-value"
)

// scheduleWork creates or updates the work object to reflect the new placement decision.
func (r *Reconciler) scheduleWork(ctx context.Context, placement *fleetv1alpha1.ClusterResourcePlacement,
	manifests []workv1alpha1.Manifest) error {
	var allErr []error
	memberClusterNames := placement.Status.TargetClusters
	workName := placement.Name
	workerOwnerRef := metav1.OwnerReference{
		APIVersion:         placement.GroupVersionKind().GroupVersion().String(),
		Kind:               placement.GroupVersionKind().Kind,
		Name:               placement.GetName(),
		UID:                placement.GetUID(),
		BlockOwnerDeletion: pointer.BoolPtr(true),
		Controller:         pointer.BoolPtr(true),
	}
	workerSpec := workv1alpha1.WorkSpec{
		Workload: workv1alpha1.WorkloadTemplate{
			Manifests: manifests,
		},
	}
	specHash, err := generateSpecHash(workerSpec.Workload)
	if err != nil {
		return errors.Wrap(err, "failed to calculate the spec hash of the newly generated work resource")
	}
	workLabels := map[string]string{
		utils.LabelWorkPlacementName: placement.GetName(),
		utils.LabelFleetObj:          utils.LabelFleetObjValue,
	}
	workAnnotation := map[string]string{
		LastUpdateAnnotationKey: time.Now().Format(time.RFC3339),
		SpecHashAnnotationKey:   specHash,
	}
	changed := false
	for _, memberClusterName := range memberClusterNames {
		memberClusterNsName := fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
		curWork, err := r.getResourceBinding(memberClusterNsName, workName)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				allErr = append(allErr, errors.Wrap(err, fmt.Sprintf("failed to get the work obj %s in namespace %s", workName, memberClusterName)))
				continue
			}
			// create the work CR since it doesn't exist
			workCR := &workv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberClusterNsName,
					Name:      workName,
					OwnerReferences: []metav1.OwnerReference{
						workerOwnerRef,
					},
					Labels:      workLabels,
					Annotations: workAnnotation,
				},
				Spec: workerSpec,
			}
			if createErr := r.Client.Create(ctx, workCR, client.FieldOwner(utils.PlacementFieldManagerName)); createErr != nil {
				klog.ErrorS(createErr, "failed to create the work", "work", workName, "namespace", memberClusterNsName)
				allErr = append(allErr, errors.Wrap(createErr, fmt.Sprintf("failed to create the work obj %s in namespace %s", workName, memberClusterNsName)))
				continue
			}
			klog.V(2).InfoS("created work spec with manifests",
				"member cluster namespace", memberClusterNsName, "work name", workName, "number of manifests", len(manifests))
			changed = true
			continue
		}
		existingHash := curWork.GetAnnotations()[SpecHashAnnotationKey]
		if existingHash == specHash || reflect.DeepEqual(curWork.Spec.Workload.Manifests, workerSpec.Workload.Manifests) {
			klog.V(4).InfoS("skip updating work spec as its identical",
				"member cluster namespace", memberClusterNsName, "work name", workName, "number of manifests", len(manifests))
			continue
		}
		changed = true
		curWork.Spec = workerSpec
		curWork.SetLabels(workLabels)
		curWork.SetOwnerReferences([]metav1.OwnerReference{workerOwnerRef})
		curWork.SetAnnotations(workAnnotation)
		if updateErr := r.Client.Update(ctx, curWork, client.FieldOwner(utils.PlacementFieldManagerName)); updateErr != nil {
			allErr = append(allErr, errors.Wrap(updateErr, fmt.Sprintf("failed to update the work obj %s in namespace %s", workName, memberClusterNsName)))
			continue
		}
		klog.V(3).InfoS("updated work spec with manifests",
			"member cluster namespace", memberClusterNsName, "work name", workName, "number of manifests", len(manifests))
	}
	if changed {
		klog.V(2).InfoS("Applied all work to the selected cluster namespaces", "placement", klog.KObj(placement), "number of clusters", len(memberClusterNames))
	} else {
		klog.V(3).InfoS("Nothing new to apply for the cluster resource placement", "placement", klog.KObj(placement), "number of clusters", len(memberClusterNames))
	}

	return apiErrors.NewAggregate(allErr)
}

// removeStaleWorks removes all the work objects from the clusters that are no longer selected.
func (r *Reconciler) removeStaleWorks(ctx context.Context, placementName string, existingClusters, newClusters []string) (int, error) {
	var allErr []error
	workName := placementName
	clusterMap := make(map[string]bool)
	for _, cluster := range newClusters {
		clusterMap[cluster] = true
	}
	removed := 0
	for _, oldCluster := range existingClusters {
		if !clusterMap[oldCluster] {
			memberClusterNsName := fmt.Sprintf(utils.NamespaceNameFormat, oldCluster)
			workCR := &workv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberClusterNsName,
					Name:      workName,
				},
			}
			if deleteErr := r.Client.Delete(ctx, workCR); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
				allErr = append(allErr, errors.Wrap(deleteErr, fmt.Sprintf("failed to delete the work obj %s from namespace %s", workName, memberClusterNsName)))
				continue
			}
			removed++
			klog.V(2).InfoS("deleted a work resource from clusters no longer selected",
				"member cluster namespace", memberClusterNsName, "work name", workName, "place", placementName)
		}
	}
	return removed, apiErrors.NewAggregate(allErr)
}

// collectAllManifestsStatus goes through all the manifest this placement handles and return if there is either
// still pending manifests or error
func (r *Reconciler) collectAllManifestsStatus(placement *fleetv1alpha1.ClusterResourcePlacement) (bool, error) {
	hasPending := false
	placement.Status.FailedResourcePlacements = make([]fleetv1alpha1.FailedResourcePlacement, 0)
	workName := placement.GetName()
	for _, cluster := range placement.Status.TargetClusters {
		memberClusterNsName := fmt.Sprintf(utils.NamespaceNameFormat, cluster)
		work, err := r.getResourceBinding(memberClusterNsName, workName)
		if err != nil {
			if apierrors.IsNotFound(err) {
				klog.V(3).InfoS("the work change has not shown up in the cache yet",
					"work", klog.KRef(memberClusterNsName, workName), "cluster", cluster)
				hasPending = true
				continue
			}
			return false, errors.Wrap(err, fmt.Sprintf("failed to get the work obj %s from namespace %s", workName, memberClusterNsName))
		}
		// check the overall condition
		appliedCond := meta.FindStatusCondition(work.Status.Conditions, workController.ConditionTypeApplied)
		if appliedCond == nil {
			hasPending = true
			klog.V(4).InfoS("the work is never picked up by the member cluster",
				"work", klog.KObj(work), "cluster", cluster)
			continue
		}
		if appliedCond.ObservedGeneration < work.GetGeneration() {
			hasPending = true
			klog.V(4).InfoS("the update of the work is not picked up by the member cluster yet",
				"work", klog.KObj(work), "cluster", cluster, "work generation", work.GetGeneration(),
				"applied generation", appliedCond.ObservedGeneration)
			continue
		}
		if appliedCond.Status == metav1.ConditionTrue {
			klog.V(4).InfoS("the work is applied successfully by the member cluster",
				"work", klog.KObj(work), "cluster", cluster)
			continue
		}
		for _, manifestCondition := range work.Status.ManifestConditions {
			resourceIdentifier := fleetv1alpha1.ResourceIdentifier{
				Group:     manifestCondition.Identifier.Group,
				Version:   manifestCondition.Identifier.Version,
				Kind:      manifestCondition.Identifier.Kind,
				Name:      manifestCondition.Identifier.Name,
				Namespace: manifestCondition.Identifier.Namespace,
			}
			appliedCond = meta.FindStatusCondition(manifestCondition.Conditions, workController.ConditionTypeApplied)
			// collect if there is an explicit fail
			if appliedCond != nil && appliedCond.Status != metav1.ConditionTrue {
				klog.V(3).InfoS("find a failed to apply manifest", "member cluster namespace", memberClusterNsName,
					"manifest name", manifestCondition.Identifier.Name, "group", manifestCondition.Identifier.Group,
					"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind)
				placement.Status.FailedResourcePlacements = append(placement.Status.FailedResourcePlacements, fleetv1alpha1.FailedResourcePlacement{
					ResourceIdentifier: resourceIdentifier,
					Condition:          *appliedCond,
					ClusterName:        cluster,
				})
			}
		}
	}

	return hasPending, nil
}

// getResourceBinding retrieves a work object by its name and namespace, this will hit the informer cache.
func (r *Reconciler) getResourceBinding(namespace, name string) (*workv1alpha1.Work, error) {
	obj, err := r.InformerManager.Lister(utils.WorkGVR).ByNamespace(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	var workObj workv1alpha1.Work
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.DeepCopyObject().(*unstructured.Unstructured).Object, &workObj)
	if err != nil {
		return nil, err
	}
	return &workObj, nil
}

// Generates a hash of the workload in the work spec
func generateSpecHash(workload workv1alpha1.WorkloadTemplate) (string, error) {
	jsonBytes, err := json.Marshal(workload)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", sha256.Sum256(jsonBytes)), nil
}
