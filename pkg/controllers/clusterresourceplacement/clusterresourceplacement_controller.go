package clusterresourceplacement

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
)

func (r *Reconciler) Reconcile(_ context.Context, _ controller.QueueKey) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// handleUpdate handles the create/update clusterResourcePlacement event.
// It creates corresponding clusterPolicySnapshot and clusterResourceSnapshot if needed and updates the status based on
// clusterPolicySnapshot status and work status.
// If the error type is ErrUnexpectedBehavior, the controller will skip the reconciling.
func (r *Reconciler) handleUpdate(ctx context.Context, crp *fleetv1beta1.ClusterResourcePlacement) (ctrl.Result, error) {
	crpKObj := klog.KObj(crp)
	schedulingPolicy := *crp.Spec.Policy // will exclude the numberOfClusters
	schedulingPolicy.NumberOfClusters = nil
	policyHash, err := generatePolicyHash(&schedulingPolicy)
	if err != nil {
		klog.ErrorS(err, "Failed to generate policy hash of crp", "clusterResourcePlacement", crpKObj)
		return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
	}

	latestPolicySnapshot, latestPolicySnapshotIndex, err := r.lookupLatestClusterPolicySnapshot(ctx, crp)
	if err != nil {
		return ctrl.Result{}, err
	}

	// mark the last policy snapshot as inactive if it is different from what we have now
	if latestPolicySnapshot != nil &&
		string(latestPolicySnapshot.Spec.PolicyHash) != policyHash &&
		latestPolicySnapshot.Labels[fleetv1beta1.IsLatestSnapshotLabel] == strconv.FormatBool(true) {
		// set the latest label to false first to make sure there is only one or none active policy snapshot
		latestPolicySnapshot.Labels[fleetv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(false)
		if err := r.Client.Update(ctx, latestPolicySnapshot); err != nil {
			klog.ErrorS(err, "Failed to set the isLatestSnapshot label to false", "clusterPolicySnapshot", klog.KObj(latestPolicySnapshot))
			return ctrl.Result{}, controller.NewAPIServerError(false, err)
		}
	}
	if latestPolicySnapshot != nil && string(latestPolicySnapshot.Spec.PolicyHash) == policyHash {
		if err := r.ensureLatestPolicySnapshot(ctx, crp, latestPolicySnapshot); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// create a new policy snapshot
		latestPolicySnapshotIndex++
		latestPolicySnapshot = &fleetv1beta1.ClusterSchedulingPolicySnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, crp.Name, latestPolicySnapshotIndex),
				Labels: map[string]string{
					fleetv1beta1.CRPTrackingLabel:      crp.Name,
					fleetv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
					fleetv1beta1.PolicyIndexLabel:      strconv.Itoa(latestPolicySnapshotIndex),
				},
			},
			Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
				Policy:     &schedulingPolicy,
				PolicyHash: []byte(policyHash),
			},
		}
		policySnapshotKObj := klog.KObj(latestPolicySnapshot)
		if err := controllerutil.SetControllerReference(crp, latestPolicySnapshot, r.Scheme); err != nil {
			klog.ErrorS(err, "Failed to set owner reference", "clusterPolicySnapshot", policySnapshotKObj)
			// should never happen
			return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
		}
		// make sure each policySnapshot should always have the annotation if CRP is selectN type
		if crp.Spec.Policy != nil &&
			crp.Spec.Policy.PlacementType == fleetv1beta1.PickNPlacementType &&
			crp.Spec.Policy.NumberOfClusters != nil {
			latestPolicySnapshot.Annotations = map[string]string{
				fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(int(*crp.Spec.Policy.NumberOfClusters)),
			}
		}

		if err := r.Client.Create(ctx, latestPolicySnapshot); err != nil {
			klog.ErrorS(err, "Failed to create new clusterPolicySnapshot", "clusterPolicySnapshot", policySnapshotKObj)
			return ctrl.Result{}, controller.NewAPIServerError(false, err)
		}
	}

	// create clusterResourceSnapshot
	// TODO
	if _, err := r.selectResourcesForPlacement(crp); err != nil {
		return ctrl.Result{}, err
	}
	// update the status based on the latestPolicySnapshot status
	// update the status based on the work
	return ctrl.Result{}, nil
}

// ensureLatestPolicySnapshot ensures the latest policySnapshot has the isLatest label and the numberOfClusters are updated.
func (r *Reconciler) ensureLatestPolicySnapshot(ctx context.Context, crp *fleetv1beta1.ClusterResourcePlacement, latest *fleetv1beta1.ClusterSchedulingPolicySnapshot) error {
	needUpdate := false
	if latest.Labels[fleetv1beta1.IsLatestSnapshotLabel] != strconv.FormatBool(true) {
		// When latestPolicySnapshot.Spec.PolicyHash == policyHash,
		// It could happen when the controller just sets the latest label to false for the old snapshot, and fails to
		// create a new policy snapshot.
		// And then the customers revert back their policy to the old one again.
		// In this case, the "latest" snapshot without isLatest label has the same policy hash as the current policy.

		latest.Labels[fleetv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(true)
		needUpdate = true
	}

	if crp.Spec.Policy != nil &&
		crp.Spec.Policy.PlacementType == fleetv1beta1.PickNPlacementType &&
		crp.Spec.Policy.NumberOfClusters != nil {
		oldCount, err := parseNumberOfClustersFromAnnotation(latest)
		if err != nil {
			klog.ErrorS(err, "Failed to parse the numberOfClusterAnnotation", "clusterPolicySnapshot", klog.KObj(latest))
			return controller.NewUnexpectedBehaviorError(err)
		}
		newCount := int(*crp.Spec.Policy.NumberOfClusters)
		if oldCount != newCount {
			latest.Annotations[fleetv1beta1.NumberOfClustersAnnotation] = strconv.Itoa(newCount)
			needUpdate = true
		}
	}
	if !needUpdate {
		return nil
	}
	if err := r.Client.Update(ctx, latest); err != nil {
		klog.ErrorS(err, "Failed to update the clusterPolicySnapshot", "clusterPolicySnapshot", klog.KObj(latest))
		return controller.NewAPIServerError(false, err)
	}
	return nil
}

// lookupLatestClusterPolicySnapshot finds the latest snapshots and its policy index.
// There will be only one active policy snapshot if exists.
// It first checks whether there is an active policy snapshot.
// If not, it finds the one whose policyIndex label is the largest.
// The policy index will always start from 0.
// Return error when 1) cannot list the snapshots 2) there are more than one active policy snapshots 3) snapshot has the
// invalid label value.
// 2 & 3 should never happen.
func (r *Reconciler) lookupLatestClusterPolicySnapshot(ctx context.Context, crp *fleetv1beta1.ClusterResourcePlacement) (*fleetv1beta1.ClusterSchedulingPolicySnapshot, int, error) {
	snapshotList := &fleetv1beta1.ClusterSchedulingPolicySnapshotList{}
	latestSnapshotLabelMatcher := client.MatchingLabels{
		fleetv1beta1.CRPTrackingLabel:      crp.Name,
		fleetv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
	}
	crpKObj := klog.KObj(crp)
	if err := r.Client.List(ctx, snapshotList, latestSnapshotLabelMatcher); err != nil {
		klog.ErrorS(err, "Failed to list active clusterPolicySnapshots", "clusterResourcePlacement", crpKObj)
		return nil, -1, controller.NewAPIServerError(false, err)
	}
	if len(snapshotList.Items) == 1 {
		policyIndex, err := parsePolicyIndexFromLabel(&snapshotList.Items[0])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the policy index label", "clusterPolicySnapshot", klog.KObj(&snapshotList.Items[0]))
			return nil, -1, controller.NewUnexpectedBehaviorError(err)
		}
		return &snapshotList.Items[0], policyIndex, nil
	} else if len(snapshotList.Items) > 1 {
		// It means there are multiple active snapshots and should never happen.
		err := fmt.Errorf("there are %d active clusterPolicySnapshots owned by clusterResourcePlacement %v", len(snapshotList.Items), crp.Name)
		klog.ErrorS(err, "It should never happen", "clusterResourcePlacement", crpKObj)
		return nil, -1, controller.NewUnexpectedBehaviorError(err)
	}
	// When there are no active snapshots, find the one who has the largest policy index.
	if err := r.Client.List(ctx, snapshotList, client.MatchingLabels{fleetv1beta1.CRPTrackingLabel: crp.Name}); err != nil {
		klog.ErrorS(err, "Failed to list all clusterPolicySnapshots", "clusterResourcePlacement", crpKObj)
		return nil, -1, controller.NewAPIServerError(false, err)
	}
	if len(snapshotList.Items) == 0 {
		// The policy index of the first snapshot will start from 0.
		return nil, -1, nil
	}
	index := -1           // the index of the cluster policy snapshot array
	lastPolicyIndex := -1 // the assigned policy index of the cluster policy snapshot
	for i := range snapshotList.Items {
		policyIndex, err := parsePolicyIndexFromLabel(&snapshotList.Items[i])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the policy index label", "clusterPolicySnapshot", klog.KObj(&snapshotList.Items[i]))
			return nil, -1, controller.NewUnexpectedBehaviorError(err)
		}
		if lastPolicyIndex < policyIndex {
			index = i
			lastPolicyIndex = policyIndex
		}
	}
	return &snapshotList.Items[index], lastPolicyIndex, nil
}

// parsePolicyIndexFromLabel returns error when parsing the label which should never return error in production.
func parsePolicyIndexFromLabel(s *fleetv1beta1.ClusterSchedulingPolicySnapshot) (int, error) {
	indexLabel := s.Labels[fleetv1beta1.PolicyIndexLabel]
	v, err := strconv.Atoi(indexLabel)
	if err != nil {
		return -1, err
	}
	if v < 0 {
		return -1, fmt.Errorf("policy index should not be negative: %d", v)
	}
	return v, nil
}

// parseNumberOfClustersFromAnnotation returns error when parsing the annotation which should never return error in production.
func parseNumberOfClustersFromAnnotation(s *fleetv1beta1.ClusterSchedulingPolicySnapshot) (int, error) {
	n := s.Annotations[fleetv1beta1.NumberOfClustersAnnotation]
	v, err := strconv.Atoi(n)
	if err != nil {
		return -1, err
	}
	if v < 0 {
		return -1, fmt.Errorf("numberOfCluster should not be negative: %d", v)
	}
	return v, nil
}

func generatePolicyHash(policy *fleetv1beta1.PlacementPolicy) (string, error) {
	jsonBytes, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(jsonBytes)), nil
}
