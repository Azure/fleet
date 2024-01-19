/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"
	"strconv"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/clusterresourceplacement"
	scheduler "go.goms.io/fleet/pkg/scheduler/framework"
	"go.goms.io/fleet/test/e2e/framework"
)

func validateWorkNamespaceOnCluster(cluster *framework.Cluster, name types.NamespacedName) error {
	ns := &corev1.Namespace{}
	if err := cluster.KubeClient.Get(ctx, name, ns); err != nil {
		return err
	}

	// Use the object created in the hub cluster as reference; this helps to avoid the trouble
	// of having to ignore default fields in the spec.
	wantNS := &corev1.Namespace{}
	if err := hubClient.Get(ctx, name, wantNS); err != nil {
		return err
	}

	if diff := cmp.Diff(
		ns, wantNS,
		ignoreNamespaceStatusField,
		ignoreObjectMetaAutoGeneratedFields,
		ignoreObjectMetaAnnotationField,
	); diff != "" {
		return fmt.Errorf("work namespace diff (-got, +want): %s", diff)
	}
	return nil
}

func validateConfigMapOnCluster(cluster *framework.Cluster, name types.NamespacedName) error {
	configMap := &corev1.ConfigMap{}
	if err := cluster.KubeClient.Get(ctx, name, configMap); err != nil {
		return err
	}

	// Use the object created in the hub cluster as reference.
	wantConfigMap := &corev1.ConfigMap{}
	if err := hubClient.Get(ctx, name, wantConfigMap); err != nil {
		return err
	}

	if diff := cmp.Diff(
		configMap, wantConfigMap,
		ignoreObjectMetaAutoGeneratedFields,
		ignoreObjectMetaAnnotationField,
	); diff != "" {
		return fmt.Errorf("app config map diff (-got, +want): %s", diff)
	}

	return nil
}

func validateSecretOnCluster(cluster *framework.Cluster, name types.NamespacedName) error {
	secret := &corev1.Secret{}
	if err := cluster.KubeClient.Get(ctx, name, secret); err != nil {
		return err
	}

	// Use the object created in the hub cluster as reference.
	wantSecret := &corev1.Secret{}
	if err := hubClient.Get(ctx, name, wantSecret); err != nil {
		return err
	}
	if diff := cmp.Diff(
		secret, wantSecret,
		ignoreObjectMetaAutoGeneratedFields,
		ignoreObjectMetaAnnotationField,
	); diff != "" {
		return fmt.Errorf("app secret %s diff (-got, +want): %s", name.Name, diff)
	}

	return nil
}

func workNamespaceAndConfigMapPlacedOnClusterActual(cluster *framework.Cluster) func() error {
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

	return func() error {
		if err := validateWorkNamespaceOnCluster(cluster, types.NamespacedName{Name: workNamespaceName}); err != nil {
			return err
		}

		return validateConfigMapOnCluster(cluster, types.NamespacedName{Namespace: workNamespaceName, Name: appConfigMapName})
	}
}

func workNamespacePlacedOnClusterActual(cluster *framework.Cluster) func() error {
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	return func() error {
		return validateWorkNamespaceOnCluster(cluster, types.NamespacedName{Name: workNamespaceName})
	}
}

func secretsPlacedOnClusterActual(cluster *framework.Cluster) func() error {
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	return func() error {
		for i := 0; i < 3; i++ {
			if err := validateSecretOnCluster(cluster, types.NamespacedName{Name: fmt.Sprintf(appSecretNameTemplate, i), Namespace: workNamespaceName}); err != nil {
				return err
			}
		}
		return nil
	}
}

func crpSyncFailedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             scheduler.FullyScheduledReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementSynchronizedConditionType),
			Status:             metav1.ConditionFalse,
			Reason:             clusterresourceplacement.SynchronizePendingReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
			Status:             metav1.ConditionUnknown,
			Reason:             clusterresourceplacement.ApplyPendingReason,
			ObservedGeneration: generation,
		},
	}
}

func crpRolloutFailedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: generation,
			Reason:             scheduler.NotFullyScheduledReason,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementSynchronizedConditionType),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: generation,
			Reason:             clusterresourceplacement.SynchronizeSucceededReason,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: generation,
			Reason:             clusterresourceplacement.ApplySucceededReason,
		},
	}
}

func crpRolloutCompletedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             scheduler.FullyScheduledReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementSynchronizedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             clusterresourceplacement.SynchronizeSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterResourcePlacementAppliedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             clusterresourceplacement.ApplySucceededReason,
			ObservedGeneration: generation,
		},
	}
}

func resourcePlacementSyncPendingConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ResourceScheduledConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             clusterresourceplacement.ResourceScheduleSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceWorkSynchronizedConditionType),
			Status:             metav1.ConditionFalse,
			Reason:             clusterresourceplacement.WorkSynchronizePendingReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourcesAppliedConditionType),
			Status:             metav1.ConditionUnknown,
			Reason:             clusterresourceplacement.ResourceApplyPendingReason,
			ObservedGeneration: generation,
		},
	}
}

func resourcePlacementApplyFailedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ResourceScheduledConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             clusterresourceplacement.ResourceScheduleSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceWorkSynchronizedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             clusterresourceplacement.WorkSynchronizeSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourcesAppliedConditionType),
			Status:             metav1.ConditionFalse,
			Reason:             clusterresourceplacement.ResourceApplyFailedReason,
			ObservedGeneration: generation,
		},
	}
}

func resourcePlacementRolloutCompletedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ResourceScheduledConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             clusterresourceplacement.ResourceScheduleSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourceWorkSynchronizedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             clusterresourceplacement.WorkSynchronizeSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ResourcesAppliedConditionType),
			Status:             metav1.ConditionTrue,
			Reason:             clusterresourceplacement.ResourceApplySucceededReason,
			ObservedGeneration: generation,
		},
	}
}

func resourcePlacementRolloutFailedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ResourceScheduledConditionType),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: generation,
			Reason:             clusterresourceplacement.ResourceScheduleFailedReason,
		},
	}
}

func workResourceIdentifiers() []placementv1beta1.ResourceIdentifier {
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

	return []placementv1beta1.ResourceIdentifier{
		{
			Kind:    "Namespace",
			Name:    workNamespaceName,
			Version: "v1",
		},
		{
			Kind:      "ConfigMap",
			Name:      appConfigMapName,
			Version:   "v1",
			Namespace: workNamespaceName,
		},
	}
}

func resourceIdentifiersForMultipleResourcesSnapshots() []placementv1beta1.ResourceIdentifier {
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	var placementResourceIdentifiers []placementv1beta1.ResourceIdentifier

	for i := 2; i >= 0; i-- {
		placementResourceIdentifiers = append(placementResourceIdentifiers, placementv1beta1.ResourceIdentifier{
			Kind:      "Secret",
			Name:      fmt.Sprintf(appSecretNameTemplate, i),
			Namespace: workNamespaceName,
			Version:   "v1",
		})
	}

	placementResourceIdentifiers = append(placementResourceIdentifiers, placementv1beta1.ResourceIdentifier{
		Kind:    "Namespace",
		Name:    workNamespaceName,
		Version: "v1",
	})
	placementResourceIdentifiers = append(placementResourceIdentifiers, placementv1beta1.ResourceIdentifier{
		Kind:      "ConfigMap",
		Name:      fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess()),
		Version:   "v1",
		Namespace: workNamespaceName,
	})

	return placementResourceIdentifiers
}

func crpStatusUpdatedActual(
	wantSelectedResourceIdentifiers []placementv1beta1.ResourceIdentifier,
	wantSelectedClusters, wantUnselectedClusters []string,
	wantObservedResourceIndex string,
) func() error {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	return func() error {
		crp := &placementv1beta1.ClusterResourcePlacement{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return err
		}

		wantPlacementStatus := []placementv1beta1.ResourcePlacementStatus{}
		for _, name := range wantSelectedClusters {
			wantPlacementStatus = append(wantPlacementStatus, placementv1beta1.ResourcePlacementStatus{
				ClusterName: name,
				Conditions:  resourcePlacementRolloutCompletedConditions(crp.Generation),
			})
		}
		for i := 0; i < len(wantUnselectedClusters); i++ {
			wantPlacementStatus = append(wantPlacementStatus, placementv1beta1.ResourcePlacementStatus{
				Conditions: resourcePlacementRolloutFailedConditions(crp.Generation),
			})
		}

		wantCRPConditions := crpRolloutCompletedConditions(crp.Generation)
		if len(wantUnselectedClusters) > 0 {
			wantCRPConditions = crpRolloutFailedConditions(crp.Generation)
		}

		// Note that the CRP controller will only keep decisions regarding unselected clusters for a CRP if:
		//
		// * The CRP is of the PickN placement type and the required N count cannot be fulfilled; or
		// * The CRP is of the PickFixed placement type and the list of target clusters specified cannot be fulfilled.
		wantStatus := placementv1beta1.ClusterResourcePlacementStatus{
			Conditions:            wantCRPConditions,
			PlacementStatuses:     wantPlacementStatus,
			SelectedResources:     wantSelectedResourceIdentifiers,
			ObservedResourceIndex: wantObservedResourceIndex,
		}
		if diff := cmp.Diff(crp.Status, wantStatus, crpStatusCmpOptions...); diff != "" {
			return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
		}
		return nil
	}
}

func workNamespaceRemovedFromClusterActual(cluster *framework.Cluster) func() error {
	client := cluster.KubeClient

	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	return func() error {
		if err := client.Get(ctx, types.NamespacedName{Name: workNamespaceName}, &corev1.Namespace{}); !errors.IsNotFound(err) {
			return fmt.Errorf("work namespace %s still exists or an unexpected error occurred: %w", workNamespaceName, err)
		}
		return nil
	}
}

func allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual() func() error {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	return func() error {
		crp := &placementv1beta1.ClusterResourcePlacement{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return err
		}

		wantFinalizers := []string{customDeletionBlockerFinalizer}
		finalizer := crp.Finalizers
		if diff := cmp.Diff(finalizer, wantFinalizers); diff != "" {
			return fmt.Errorf("CRP finalizers diff (-got, +want): %s", diff)
		}

		return nil
	}
}

func crpRemovedActual() func() error {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	return func() error {
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &placementv1beta1.ClusterResourcePlacement{}); !errors.IsNotFound(err) {
			return fmt.Errorf("CRP still exists or an unexpected error occurred: %w", err)
		}

		return nil
	}
}

func multipleResourceSnapshotsCreatedActual(numberOfResourceSnapshotsAnnotation, resourceIndexLabel string) func() error {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	return func() error {
		var resourceSnapshotList placementv1beta1.ClusterResourceSnapshotList
		masterResourceSnapshotLabels := client.MatchingLabels{
			placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
			placementv1beta1.CRPTrackingLabel:      crpName,
		}
		if err := hubClient.List(ctx, &resourceSnapshotList, masterResourceSnapshotLabels); err != nil {
			return err
		}
		// there should be only one master resource snapshot.
		if len(resourceSnapshotList.Items) != 1 {
			return fmt.Errorf("number of master cluster resource snapshots has unexpected value: got %d, want %d", len(resourceSnapshotList.Items), 1)
		}
		masterResourceSnapshot := resourceSnapshotList.Items[0]
		resourceSnapshotListLabels := client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crpName}
		if err := hubClient.List(ctx, &resourceSnapshotList, resourceSnapshotListLabels); err != nil {
			return err
		}
		numberOfResourceSnapshots := masterResourceSnapshot.Annotations[placementv1beta1.NumberOfResourceSnapshotsAnnotation]
		if numberOfResourceSnapshots != numberOfResourceSnapshotsAnnotation {
			return fmt.Errorf("NumberOfResourceSnapshotsAnnotation in master cluster resource snapshot has unexpected value:  got %s, want %s", numberOfResourceSnapshots, numberOfResourceSnapshotsAnnotation)
		}
		if strconv.Itoa(len(resourceSnapshotList.Items)) != numberOfResourceSnapshots {
			return fmt.Errorf("number of cluster resource snapshots has unexpected value: got %s, want %s", strconv.Itoa(len(resourceSnapshotList.Items)), numberOfResourceSnapshots)
		}
		masterResourceIndex := masterResourceSnapshot.Labels[placementv1beta1.ResourceIndexLabel]
		if masterResourceIndex != resourceIndexLabel {
			return fmt.Errorf("resource index for master cluster resource snapshot %s has unexpected value: got %s, want %s", masterResourceSnapshot.Name, masterResourceIndex, resourceIndexLabel)
		}
		for i := range resourceSnapshotList.Items {
			resourceSnapshot := resourceSnapshotList.Items[i]
			index := resourceSnapshot.Labels[placementv1beta1.ResourceIndexLabel]
			if index != masterResourceIndex {
				return fmt.Errorf("resource index for cluster resource snapshot %s has unexpected value: got %s, want %s", resourceSnapshot.Name, index, masterResourceIndex)
			}
		}
		return nil
	}
}

func validateCRPSnapshotRevisions(crpName string, wantPolicySnapshotRevision, wantResourceSnapshotRevision int) error {
	matchingLabels := client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crpName}

	snapshotList := &placementv1beta1.ClusterSchedulingPolicySnapshotList{}
	if err := hubClient.List(ctx, snapshotList, matchingLabels); err != nil {
		return err
	}
	if len(snapshotList.Items) != wantPolicySnapshotRevision {
		return fmt.Errorf("clusterSchedulingPolicySnapshotList got %v, want 1", len(snapshotList.Items))
	}
	resourceSnapshotList := &placementv1beta1.ClusterResourceSnapshotList{}
	if err := hubClient.List(ctx, resourceSnapshotList, matchingLabels); err != nil {
		return err
	}
	if len(resourceSnapshotList.Items) != wantResourceSnapshotRevision {
		return fmt.Errorf("clusterResourceSnapshotList got %v, want 2", len(snapshotList.Items))
	}
	return nil
}
