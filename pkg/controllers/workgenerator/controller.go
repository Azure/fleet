/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package workgenerator features a controller to generate work objects based on resource binding objects.
package workgenerator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/workapplier"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
	"go.goms.io/fleet/pkg/utils/labels"
	"go.goms.io/fleet/pkg/utils/resource"
)

var (
	// maxFailedResourcePlacementLimit indicates the max number of failed resource placements to include in the status.
	maxFailedResourcePlacementLimit = 100
	// maxDriftedResourcePlacementLimit indicates the max number of drifted resource placements to include in the status.
	maxDriftedResourcePlacementLimit = 100
	// maxDiffedResourcePlacementLimit indicates the max number of diffed resource placements to include in the status.
	maxDiffedResourcePlacementLimit = 100

	errResourceSnapshotNotFound = fmt.Errorf("the master resource snapshot is not found")
)

// Reconciler watches binding objects and generate work objects in the designated cluster namespace
// according to the information in the binding objects.
// TODO: incorporate an overriding policy if one exists
type Reconciler struct {
	client.Client
	// the max number of concurrent reconciles per controller.
	MaxConcurrentReconciles int
	recorder                record.EventRecorder
	// the informer contains the cache for all the resources we need.
	// to check the resource scope
	InformerManager informer.Manager
}

// Reconcile triggers a single binding reconcile round.
func (r *Reconciler) Reconcile(ctx context.Context, req controllerruntime.Request) (controllerruntime.Result, error) {
	klog.V(2).InfoS("Start to reconcile a ClusterResourceBinding", "resourceBinding", req.Name)
	startTime := time.Now()
	bindingRef := klog.KRef(req.Namespace, req.Name)
	// add latency log
	defer func() {
		klog.V(2).InfoS("ClusterResourceBinding reconciliation loop ends", "resourceBinding", bindingRef, "latency", time.Since(startTime).Milliseconds())
	}()
	var resourceBinding fleetv1beta1.ClusterResourceBinding
	if err := r.Client.Get(ctx, req.NamespacedName, &resourceBinding); err != nil {
		if apierrors.IsNotFound(err) {
			return controllerruntime.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get the resource binding", "resourceBinding", bindingRef)
		return controllerruntime.Result{}, controller.NewAPIServerError(true, err)
	}

	// handle the case the binding is deleting
	if resourceBinding.DeletionTimestamp != nil {
		return r.handleDelete(ctx, resourceBinding.DeepCopy())
	}

	// we only care about the bound bindings. We treat unscheduled bindings as bound until they are deleted.
	if resourceBinding.Spec.State != fleetv1beta1.BindingStateBound && resourceBinding.Spec.State != fleetv1beta1.BindingStateUnscheduled {
		klog.V(2).InfoS("Skip reconciling clusterResourceBinding that is not bound", "state", resourceBinding.Spec.State, "resourceBinding", bindingRef)
		return controllerruntime.Result{}, nil
	}

	// Getting the member cluster before the adding the finalizer if there is no finalizer present.
	// If the member cluster is not found and finalizer is not present, we skip the reconciliation and no work will be created.
	// In this case, no need to add the finalizer to make sure we clean up all the works.
	cluster := clusterv1beta1.MemberCluster{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: resourceBinding.Spec.TargetCluster}, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("Skip reconciling clusterResourceBinding when the cluster is deleted", "memberCluster", resourceBinding.Spec.TargetCluster, "clusterResourceBinding", bindingRef)
			return controllerruntime.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get the memberCluster", "memberCluster", resourceBinding.Spec.TargetCluster, "clusterResourceBinding", bindingRef)
		return controllerruntime.Result{}, controller.NewAPIServerError(true, err)
	}

	// make sure that the resource binding obj has a finalizer
	if err := r.ensureFinalizer(ctx, &resourceBinding); err != nil {
		return controllerruntime.Result{}, err
	}

	// When the binding is in the unscheduled state, rollout controller won't update the condition anymore.
	// We treat the unscheduled binding as bound until the rollout controller deletes the binding and here controller still
	// updates the status for troubleshooting purpose.
	// Requeue until the rollout controller finishes processing the binding.
	if resourceBinding.Spec.State == fleetv1beta1.BindingStateBound {
		rolloutStartedCondition := resourceBinding.GetCondition(string(fleetv1beta1.ResourceBindingRolloutStarted))
		// Though the bounded binding is not taking the latest resourceSnapshot, we still needs to reconcile the works.
		if !condition.IsConditionStatusFalse(rolloutStartedCondition, resourceBinding.Generation) &&
			!condition.IsConditionStatusTrue(rolloutStartedCondition, resourceBinding.Generation) {
			// The rollout controller is still in the processing of updating the condition.
			//
			// Note that running this branch would also skip the refreshing of apply strategies;
			// it will resume once the rollout controller updates the rollout started condition.
			klog.V(2).InfoS("Requeue the resource binding until the rollout controller finishes updating the status", "resourceBinding", bindingRef, "generation", resourceBinding.Generation, "rolloutStartedCondition", rolloutStartedCondition)
			return controllerruntime.Result{Requeue: true}, nil
		}
	}

	workUpdated := false
	overrideSucceeded := false
	// list all the corresponding works
	works, syncErr := r.listAllWorksAssociated(ctx, &resourceBinding)
	if syncErr == nil {
		// generate and apply the workUpdated works if we have all the works
		overrideSucceeded, workUpdated, syncErr = r.syncAllWork(ctx, &resourceBinding, works, &cluster)
	}
	// Reset the conditions and failed/drifted/diffed placements.
	for i := condition.OverriddenCondition; i < condition.TotalCondition; i++ {
		resourceBinding.RemoveCondition(string(i.ResourceBindingConditionType()))
	}
	resourceBinding.Status.FailedPlacements = nil
	resourceBinding.Status.DriftedPlacements = nil
	resourceBinding.Status.DiffedPlacements = nil
	if overrideSucceeded {
		overrideReason := condition.OverriddenSucceededReason
		overrideMessage := "Successfully applied the override rules on the resources"
		if len(resourceBinding.Spec.ClusterResourceOverrideSnapshots) == 0 &&
			len(resourceBinding.Spec.ResourceOverrideSnapshots) == 0 {
			overrideReason = condition.OverrideNotSpecifiedReason
			overrideMessage = "No override rules are configured for the selected resources"
		}
		resourceBinding.SetConditions(metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceBindingOverridden),
			Reason:             overrideReason,
			Message:            overrideMessage,
			ObservedGeneration: resourceBinding.Generation,
		})
	}

	if syncErr != nil {
		klog.ErrorS(syncErr, "Failed to sync all the works", "resourceBinding", bindingRef)
		//TODO: check if it's user error and set a different failed reason
		errorMessage := syncErr.Error()
		// unwrap will return nil if syncErr is not wrapped
		// the wrapped error string format is "%w: %s" so that remove ": " from messages
		if err := errors.Unwrap(syncErr); err != nil && len(err.Error()) > 2 {
			errorMessage = errorMessage[len(err.Error())+2:]
		}
		if !overrideSucceeded {
			resourceBinding.SetConditions(metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingOverridden),
				Reason:             condition.OverriddenFailedReason,
				Message:            fmt.Sprintf("Failed to apply the override rules on the resources: %s", errorMessage),
				ObservedGeneration: resourceBinding.Generation,
			})
		} else {
			resourceBinding.SetConditions(metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
				Reason:             condition.SyncWorkFailedReason,
				Message:            fmt.Sprintf("Failed to synchronize the work to the latest: %s", errorMessage),
				ObservedGeneration: resourceBinding.Generation,
			})
		}
	} else {
		resourceBinding.SetConditions(metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
			Reason:             condition.AllWorkSyncedReason,
			ObservedGeneration: resourceBinding.Generation,
			Message:            "All of the works are synchronized to the latest",
		})
		switch {
		case !workUpdated:
			// The Work object itself is unchanged; refresh the cluster resource binding status
			// based on the status information reported on the Work object(s).
			setBindingStatus(works, &resourceBinding)
		case resourceBinding.Spec.ApplyStrategy == nil || resourceBinding.Spec.ApplyStrategy.Type != fleetv1beta1.ApplyStrategyTypeReportDiff:
			// The Work object itself has changed; set a False Applied condition which signals
			// that resources are in the process of being applied.
			resourceBinding.SetConditions(metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingApplied),
				Reason:             condition.WorkApplyInProcess,
				Message:            "Resources are being applied",
				ObservedGeneration: resourceBinding.Generation,
			})
		case resourceBinding.Spec.ApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff:
			// The Work object itself has changed; set a False DiffReported condition which signals
			// that diff reporting on resources are in progress.
			resourceBinding.SetConditions(metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingDiffReported),
				Reason:             condition.WorkDiffReportInProcess,
				Message:            "Diff reporting on resources is in progress",
				ObservedGeneration: resourceBinding.Generation,
			})
		}
	}

	// update the resource binding status
	if updateErr := r.updateBindingStatusWithRetry(ctx, &resourceBinding); updateErr != nil {
		return controllerruntime.Result{}, updateErr
	}
	if errors.Is(syncErr, controller.ErrUserError) {
		// Stop retry when the error is caused by user error
		// For example, user provides an invalid overrides or cannot extract the resources from config map.
		klog.ErrorS(syncErr, "Stopped retrying the resource binding", "resourceBinding", bindingRef)
		return controllerruntime.Result{}, nil
	}

	if errors.Is(syncErr, errResourceSnapshotNotFound) {
		// This error usually indicates that the resource snapshot is deleted since the rollout controller which fills
		// the resource snapshot share the same informer cache with this controller. We don't need to retry in this case
		// since the resource snapshot will not come back. We will get another event if the binding is pointing to a new resource.
		// However, this error can happen when the resource snapshot exists during the IT test when the client that creates
		// the resource snapshot is not the same as the controller client so that we need to retry in this case.
		// This error can also happen if the user uses a customized rollout controller that does not share the same informer cache with this controller.
		return controllerruntime.Result{Requeue: true}, nil
	}
	// requeue if we failed to sync the work
	// If we update the works, their status will be changed and will be detected by the watch event.
	return controllerruntime.Result{}, syncErr
}

// updateBindingStatusWithRetry sends the update request to API server with retry.
func (r *Reconciler) updateBindingStatusWithRetry(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) error {
	// Retry only for specific errors or conditions
	bindingRef := klog.KObj(resourceBinding)
	err := r.Client.Status().Update(ctx, resourceBinding)
	if err != nil {
		klog.ErrorS(err, "Failed to update the resourceBinding status, will retry", "resourceBinding", bindingRef, "resourceBindingStatus", resourceBinding.Status)
		errAfterRetries := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			var latestBinding fleetv1beta1.ClusterResourceBinding
			if err := r.Client.Get(ctx, client.ObjectKeyFromObject(resourceBinding), &latestBinding); err != nil {
				return err
			}
			// Work generator is the only controller that updates conditions excluding rollout started which is updated by rollout controller.
			if rolloutCond := latestBinding.GetCondition(string(fleetv1beta1.ResourceBindingRolloutStarted)); rolloutCond != nil {
				for i := range resourceBinding.Status.Conditions {
					if resourceBinding.Status.Conditions[i].Type == rolloutCond.Type {
						// Replace the existing condition
						resourceBinding.Status.Conditions[i] = *rolloutCond
						break
					}
				}
			} else {
				// At least the RolloutStarted condition for the old generation should be set.
				// RolloutStarted condition won't be removed by the rollout controller.
				klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("found an invalid resourceBinding")), "RolloutStarted condition is not set", "resourceBinding", bindingRef)
			}
			latestBinding.Status = resourceBinding.Status
			if err := r.Client.Status().Update(ctx, &latestBinding); err != nil {
				klog.ErrorS(err, "Failed to update the resourceBinding status", "resourceBinding", bindingRef, "resourceBindingStatus", latestBinding.Status)
				return err
			}
			klog.V(2).InfoS("Successfully updated the resourceBinding status", "resourceBinding", bindingRef, "resourceBindingStatus", latestBinding.Status)
			return nil
		})
		if errAfterRetries != nil {
			klog.ErrorS(errAfterRetries, "Failed to update resourceBinding status after retries", "resourceBinding", bindingRef)
			return errAfterRetries
		}
		return nil
	}
	klog.V(2).InfoS("Successfully updated the resourceBinding status", "resourceBinding", bindingRef, "resourceBindingStatus", resourceBinding.Status)
	return nil
}

// handleDelete handle a deleting binding
func (r *Reconciler) handleDelete(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) (controllerruntime.Result, error) {
	klog.V(4).InfoS("Start to handle deleting resource binding", "resourceBinding", klog.KObj(resourceBinding))
	// list all the corresponding works if exist
	works, err := r.listAllWorksAssociated(ctx, resourceBinding)
	if err != nil {
		return controllerruntime.Result{}, err
	}

	// delete all the listed works
	//
	// TO-DO: this controller should be able to garbage collect all works automatically via
	// background/foreground cascade deletion. This may render the finalizer unnecessary.
	for workName := range works {
		work := works[workName]
		if err := r.Client.Delete(ctx, work); err != nil && !apierrors.IsNotFound(err) {
			return controllerruntime.Result{}, controller.NewAPIServerError(false, err)
		}
	}

	// remove the work finalizer on the binding if all the work objects are deleted
	if len(works) == 0 {
		controllerutil.RemoveFinalizer(resourceBinding, fleetv1beta1.WorkFinalizer)
		if err = r.Client.Update(ctx, resourceBinding); err != nil {
			klog.ErrorS(err, "Failed to remove the work finalizer from resource binding", "resourceBinding", klog.KObj(resourceBinding))
			return controllerruntime.Result{}, controller.NewUpdateIgnoreConflictError(err)
		}
		klog.V(2).InfoS("The resource binding is deleted", "resourceBinding", klog.KObj(resourceBinding))
		return controllerruntime.Result{}, nil
	}
	klog.V(2).InfoS("The resource binding still has undeleted work", "resourceBinding", klog.KObj(resourceBinding),
		"number of associated work", len(works))
	// we watch the work objects deleting events, so we can afford to wait a bit longer here as a fallback case.
	return controllerruntime.Result{RequeueAfter: 30 * time.Second}, nil
}

// ensureFinalizer makes sure that the resourceSnapshot CR has a finalizer on it.
func (r *Reconciler) ensureFinalizer(ctx context.Context, resourceBinding client.Object) error {
	if controllerutil.ContainsFinalizer(resourceBinding, fleetv1beta1.WorkFinalizer) {
		return nil
	}

	// Add retries to the update behavior as the binding object can become a point of heavy
	// contention under heavy workload; simply requeueing when a write conflict occurs, though
	// functionally correct, might trigger the work queue rate limiter and eventually lead to
	// substantial delays in processing.
	//
	// Also note that here default backoff strategy (exponential backoff) rather than the Kubernetes'
	// recommended on-write-conflict backoff strategy is used, as experimentation suggests that
	// this backoff strategy yields better performance, especially for the long-tail latencies.
	//
	// TO-DO (chenyu1): evaluate if a custom backoff strategy can get an even better result.
	errAfterRetries := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(resourceBinding), resourceBinding); err != nil {
			return err
		}

		controllerutil.AddFinalizer(resourceBinding, fleetv1beta1.WorkFinalizer)
		return r.Client.Update(ctx, resourceBinding)
	})
	if errAfterRetries != nil {
		klog.ErrorS(errAfterRetries, "Failed to add the work finalizer after retries", "resourceBinding", klog.KObj(resourceBinding))
		return controller.NewUpdateIgnoreConflictError(errAfterRetries)
	}
	klog.V(2).InfoS("Successfully add the work finalizer", "resourceBinding", klog.KObj(resourceBinding))
	return nil
}

// listAllWorksAssociated finds all the live work objects that are associated with this binding.
func (r *Reconciler) listAllWorksAssociated(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) (map[string]*fleetv1beta1.Work, error) {
	namespaceMatcher := client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.Spec.TargetCluster))
	parentBindingLabelMatcher := client.MatchingLabels{
		fleetv1beta1.ParentBindingLabel: resourceBinding.Name,
	}
	currentWork := make(map[string]*fleetv1beta1.Work)
	workList := &fleetv1beta1.WorkList{}
	if err := r.Client.List(ctx, workList, parentBindingLabelMatcher, namespaceMatcher); err != nil {
		klog.ErrorS(err, "Failed to list all the work associated with the resourceSnapshot", "resourceBinding", klog.KObj(resourceBinding))
		return nil, controller.NewAPIServerError(true, err)
	}
	for _, work := range workList.Items {
		if work.DeletionTimestamp == nil {
			currentWork[work.Name] = work.DeepCopy()
		}
	}
	klog.V(2).InfoS("Get all the work associated", "numOfWork", len(currentWork), "resourceBinding", klog.KObj(resourceBinding))
	return currentWork, nil
}

// syncAllWork generates all the work for the resourceSnapshot and apply them to the corresponding target cluster.
// it returns
// 1: if we apply the overrides successfully
// 2: if we actually made any changes on the hub cluster
func (r *Reconciler) syncAllWork(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding, existingWorks map[string]*fleetv1beta1.Work, cluster *clusterv1beta1.MemberCluster) (bool, bool, error) {
	updateAny := atomic.NewBool(false)
	resourceBindingRef := klog.KObj(resourceBinding)

	// Refresh the apply strategy for all existing works.
	//
	// This step is performed separately from other refreshes as apply strategy changes are
	// CRP-scoped and independent from the resource snapshot management mechanism. In other
	// words, even if a work has become stranded (i.e., it is linked to a resource snapshot that
	// is no longer present in the system), it should still be able to receive the latest apply
	// strategy update.
	errs, cctx := errgroup.WithContext(ctx)
	for workName := range existingWorks {
		w := existingWorks[workName]
		errs.Go(func() error {
			updated, err := r.syncApplyStrategy(ctx, resourceBinding, w)
			if err != nil {
				return err
			}
			if updated {
				updateAny.Store(true)
			}
			return nil
		})
	}
	if updateErr := errs.Wait(); updateErr != nil {
		return false, false, updateErr
	}

	// the hash256 function can handle empty list https://go.dev/play/p/_4HW17fooXM
	resourceOverrideSnapshotHash, err := resource.HashOf(resourceBinding.Spec.ResourceOverrideSnapshots)
	if err != nil {
		return false, false, controller.NewUnexpectedBehaviorError(err)
	}
	clusterResourceOverrideSnapshotHash, err := resource.HashOf(resourceBinding.Spec.ClusterResourceOverrideSnapshots)
	if err != nil {
		return false, false, controller.NewUnexpectedBehaviorError(err)
	}
	// TODO: check all work synced first before fetching the snapshots after we put ParentResourceOverrideSnapshotHashAnnotation and ParentClusterResourceOverrideSnapshotHashAnnotation in all the work objects

	// Gather all the resource resourceSnapshots
	resourceSnapshots, err := r.fetchAllResourceSnapshots(ctx, resourceBinding)
	if err != nil {
		if errors.Is(err, errResourceSnapshotNotFound) {
			// the resourceIndex is deleted but the works might still be up to date with the binding.
			if areAllWorkSynced(existingWorks, resourceBinding, resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash) {
				klog.V(2).InfoS("All the works are synced with the resourceBinding even if the resource snapshot index is removed", "resourceBinding", resourceBindingRef)
				return true, updateAny.Load(), nil
			}
			return false, false, controller.NewUserError(err)
		}
		// TODO(RZ): handle errResourceNotFullyCreated error so we don't need to wait for all the snapshots to be created
		return false, false, err
	}

	croMap, err := r.fetchClusterResourceOverrideSnapshots(ctx, resourceBinding)
	if err != nil {
		return false, false, err
	}

	roMap, err := r.fetchResourceOverrideSnapshots(ctx, resourceBinding)
	if err != nil {
		return false, false, err
	}

	// issue all the create/update requests for the corresponding works for each snapshot in parallel
	activeWork := make(map[string]*fleetv1beta1.Work, len(resourceSnapshots))
	errs, cctx = errgroup.WithContext(ctx)
	// generate work objects for each resource snapshot
	for i := range resourceSnapshots {
		snapshot := resourceSnapshots[i]
		var newWork []*fleetv1beta1.Work
		workNamePrefix, err := getWorkNamePrefixFromSnapshotName(snapshot)
		if err != nil {
			klog.ErrorS(err, "Encountered a mal-formatted resource snapshot", "resourceSnapshot", klog.KObj(snapshot))
			return false, false, err
		}
		var simpleManifests []fleetv1beta1.Manifest
		for j := range snapshot.Spec.SelectedResources {
			selectedResource := snapshot.Spec.SelectedResources[j].DeepCopy()
			// TODO: override the content of the wrapped resource instead of the envelope itself
			resourceDeleted, overrideErr := r.applyOverrides(selectedResource, cluster, croMap, roMap)
			if overrideErr != nil {
				return false, false, overrideErr
			}
			if resourceDeleted {
				klog.V(2).InfoS("The resource is deleted by the override rules", "snapshot", klog.KObj(snapshot), "selectedResource", snapshot.Spec.SelectedResources[j])
				continue
			}
			// we need to special treat configMap with envelopeConfigMapAnnotation annotation,
			// so we need to check the GVK and annotation of the selected resource
			var uResource unstructured.Unstructured
			if unMarshallErr := uResource.UnmarshalJSON(selectedResource.Raw); unMarshallErr != nil {
				klog.ErrorS(unMarshallErr, "work has invalid content", "snapshot", klog.KObj(snapshot), "selectedResource", selectedResource.Raw)
				return true, false, controller.NewUnexpectedBehaviorError(unMarshallErr)
			}
			if uResource.GetObjectKind().GroupVersionKind() == utils.ConfigMapGVK &&
				len(uResource.GetAnnotations()[fleetv1beta1.EnvelopeConfigMapAnnotation]) != 0 {
				// get a work object for the enveloped configMap
				work, err := r.getConfigMapEnvelopWorkObj(ctx, workNamePrefix, resourceBinding, snapshot, &uResource, resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash)
				if err != nil {
					return true, false, err
				}
				activeWork[work.Name] = work
				newWork = append(newWork, work)
			} else {
				simpleManifests = append(simpleManifests, fleetv1beta1.Manifest(*selectedResource))
			}
		}
		if len(simpleManifests) == 0 {
			klog.V(2).InfoS("the snapshot contains no resource to apply either because of override or enveloped resources", "snapshot", klog.KObj(snapshot))
		}
		// generate a work object for the manifests even if there is nothing to place
		// to allow CRP to collect the status of the placement
		// TODO (RZ): revisit to see if we need this hack
		work := generateSnapshotWorkObj(workNamePrefix, resourceBinding, snapshot, simpleManifests, resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash)
		activeWork[work.Name] = work
		newWork = append(newWork, work)

		// issue all the create/update requests for the corresponding works for each snapshot in parallel
		for ni := range newWork {
			w := newWork[ni]
			errs.Go(func() error {
				updated, err := r.upsertWork(cctx, w, existingWorks[w.Name].DeepCopy(), snapshot)
				if err != nil {
					return err
				}
				if updated {
					updateAny.Store(true)
				}
				return nil
			})
		}
	}

	//  delete the works that are not associated with any resource snapshot
	for i := range existingWorks {
		work := existingWorks[i]
		if _, exist := activeWork[work.Name]; exist {
			continue
		}
		errs.Go(func() error {
			if err := r.Client.Delete(ctx, work); err != nil {
				if !apierrors.IsNotFound(err) {
					klog.ErrorS(err, "Failed to delete the no longer needed work", "work", klog.KObj(work))
					return controller.NewAPIServerError(false, err)
				}
			}
			klog.V(2).InfoS("Deleted the work that is not associated with any resource snapshot", "work", klog.KObj(work))
			updateAny.Store(true)
			return nil
		})
	}

	// wait for all the create/update/delete requests to finish
	if updateErr := errs.Wait(); updateErr != nil {
		return true, false, updateErr
	}
	klog.V(2).InfoS("Successfully synced all the work associated with the resourceBinding", "updateAny", updateAny.Load(), "resourceBinding", resourceBindingRef)
	return true, updateAny.Load(), nil
}

// syncApplyStrategy syncs the apply strategy specified on a ClusterResourceBinding object
// to a Work object.
func (r *Reconciler) syncApplyStrategy(
	ctx context.Context,
	resourceBinding *fleetv1beta1.ClusterResourceBinding,
	existingWork *fleetv1beta1.Work,
) (bool, error) {
	// Skip the update if no change on apply strategy is needed.
	if equality.Semantic.DeepEqual(existingWork.Spec.ApplyStrategy, resourceBinding.Spec.ApplyStrategy) {
		return false, nil
	}

	// Update the apply strategy on the work.
	existingWork.Spec.ApplyStrategy = resourceBinding.Spec.ApplyStrategy.DeepCopy()
	if err := r.Client.Update(ctx, existingWork); err != nil {
		klog.ErrorS(err, "Failed to update the apply strategy on the work", "work", klog.KObj(existingWork), "binding", klog.KObj(resourceBinding))
		return true, controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Successfully updated the apply strategy on the work", "work", klog.KObj(existingWork), "binding", klog.KObj(resourceBinding))
	return true, nil
}

// areAllWorkSynced checks if all the works are synced with the resource binding.
func areAllWorkSynced(existingWorks map[string]*fleetv1beta1.Work, resourceBinding *fleetv1beta1.ClusterResourceBinding, _, _ string) bool {
	// TODO: check resourceOverrideSnapshotHash and  clusterResourceOverrideSnapshotHash after all the work has the ParentResourceOverrideSnapshotHashAnnotation and ParentClusterResourceOverrideSnapshotHashAnnotation
	resourceSnapshotName := resourceBinding.Spec.ResourceSnapshotName
	for _, work := range existingWorks {
		recordedName, exist := work.Annotations[fleetv1beta1.ParentResourceSnapshotNameAnnotation]
		if !exist {
			// TODO: remove this block after all the work has the ParentResourceSnapshotNameAnnotation
			// the parent resource snapshot name is not recorded in the work, we need to construct it from the labels
			crpName := resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel]
			index, _ := labels.ExtractResourceSnapshotIndexFromWork(work)
			recordedName = fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crpName, index)
		}
		if recordedName != resourceSnapshotName {
			klog.V(2).InfoS("The work is not synced with the resourceBinding", "work", klog.KObj(work), "resourceBinding", klog.KObj(resourceBinding), "annotationExist", exist, "recordedName", recordedName, "resourceSnapshotName", resourceSnapshotName)
			return false
		}
	}
	return true
}

// fetchAllResourceSnapshots gathers all the resource snapshots for the resource binding.
func (r *Reconciler) fetchAllResourceSnapshots(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) (map[string]*fleetv1beta1.ClusterResourceSnapshot, error) {
	// fetch the master snapshot first
	masterResourceSnapshot := fleetv1beta1.ClusterResourceSnapshot{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: resourceBinding.Spec.ResourceSnapshotName}, &masterResourceSnapshot); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("The master resource snapshot is deleted", "resourceBinding", klog.KObj(resourceBinding), "resourceSnapshotName", resourceBinding.Spec.ResourceSnapshotName)
			return nil, errResourceSnapshotNotFound
		}
		klog.ErrorS(err, "Failed to get the resource snapshot from resource masterResourceSnapshot",
			"resourceBinding", klog.KObj(resourceBinding), "masterResourceSnapshot", resourceBinding.Spec.ResourceSnapshotName)
		return nil, controller.NewAPIServerError(true, err)
	}
	return controller.FetchAllClusterResourceSnapshots(ctx, r.Client, resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel], &masterResourceSnapshot)
}

// getConfigMapEnvelopWorkObj first try to locate a work object for the corresponding envelopObj of type configMap.
// we create a new one if the work object doesn't exist. We do this to avoid repeatedly delete and create the same work object.
func (r *Reconciler) getConfigMapEnvelopWorkObj(ctx context.Context, workNamePrefix string, resourceBinding *fleetv1beta1.ClusterResourceBinding,
	resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot, envelopeObj *unstructured.Unstructured, resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash string) (*fleetv1beta1.Work, error) {
	// we group all the resources in one configMap to one work
	manifest, err := extractResFromConfigMap(envelopeObj)
	if err != nil {
		klog.ErrorS(err, "configMap has invalid content", "snapshot", klog.KObj(resourceSnapshot),
			"resourceBinding", klog.KObj(resourceBinding), "configMapWrapper", klog.KObj(envelopeObj))
		return nil, controller.NewUserError(err)
	}
	klog.V(2).InfoS("Successfully extract the enveloped resources from the configMap", "numOfResources", len(manifest),
		"snapshot", klog.KObj(resourceSnapshot), "resourceBinding", klog.KObj(resourceBinding), "configMapWrapper", klog.KObj(envelopeObj))

	// Try to see if we already have a work represent the same enveloped object for this CRP in the same cluster
	// The ParentResourceSnapshotIndexLabel can change between snapshots so we have to exclude that label in the match
	envelopWorkLabelMatcher := client.MatchingLabels{
		fleetv1beta1.ParentBindingLabel:     resourceBinding.Name,
		fleetv1beta1.CRPTrackingLabel:       resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
		fleetv1beta1.EnvelopeTypeLabel:      string(fleetv1beta1.ConfigMapEnvelopeType),
		fleetv1beta1.EnvelopeNameLabel:      envelopeObj.GetName(),
		fleetv1beta1.EnvelopeNamespaceLabel: envelopeObj.GetNamespace(),
	}
	workList := &fleetv1beta1.WorkList{}
	if err := r.Client.List(ctx, workList, envelopWorkLabelMatcher); err != nil {
		return nil, controller.NewAPIServerError(true, err)
	}
	// we need to create a new work object
	if len(workList.Items) == 0 {
		// we limit the CRP name length to be 63 (DNS1123LabelMaxLength) characters,
		// so we have plenty of characters left to fit into 253 (DNS1123SubdomainMaxLength) characters for a CR
		workName := fmt.Sprintf(fleetv1beta1.WorkNameWithConfigEnvelopeFmt, workNamePrefix, uuid.NewUUID())
		return &fleetv1beta1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Name:      workName,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.Spec.TargetCluster),
				Labels: map[string]string{
					fleetv1beta1.ParentBindingLabel:               resourceBinding.Name,
					fleetv1beta1.CRPTrackingLabel:                 resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
					fleetv1beta1.ParentResourceSnapshotIndexLabel: resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel],
					fleetv1beta1.EnvelopeTypeLabel:                string(fleetv1beta1.ConfigMapEnvelopeType),
					fleetv1beta1.EnvelopeNameLabel:                envelopeObj.GetName(),
					fleetv1beta1.EnvelopeNamespaceLabel:           envelopeObj.GetNamespace(),
				},
				Annotations: map[string]string{
					fleetv1beta1.ParentResourceSnapshotNameAnnotation:                resourceBinding.Spec.ResourceSnapshotName,
					fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        resourceOverrideSnapshotHash,
					fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: clusterResourceOverrideSnapshotHash,
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         fleetv1beta1.GroupVersion.String(),
						Kind:               resourceBinding.Kind,
						Name:               resourceBinding.Name,
						UID:                resourceBinding.UID,
						BlockOwnerDeletion: ptr.To(true), // make sure that the k8s will call work delete when the binding is deleted
					},
				},
			},
			Spec: fleetv1beta1.WorkSpec{
				Workload: fleetv1beta1.WorkloadTemplate{
					Manifests: manifest,
				},
				ApplyStrategy: resourceBinding.Spec.ApplyStrategy,
			},
		}, nil
	}
	if len(workList.Items) > 1 {
		// return error here won't get us out of this
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("find %d work representing configMap", len(workList.Items))),
			"snapshot", klog.KObj(resourceSnapshot), "resourceBinding", klog.KObj(resourceBinding), "configMapWrapper", klog.KObj(envelopeObj))
	}
	work := workList.Items[0]
	work.Labels[fleetv1beta1.ParentResourceSnapshotIndexLabel] = resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel]
	if work.Annotations == nil {
		work.Annotations = make(map[string]string)
	}
	work.Annotations[fleetv1beta1.ParentResourceSnapshotNameAnnotation] = resourceBinding.Spec.ResourceSnapshotName
	work.Annotations[fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation] = resourceOverrideSnapshotHash
	work.Annotations[fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation] = clusterResourceOverrideSnapshotHash
	work.Spec.Workload.Manifests = manifest
	work.Spec.ApplyStrategy = resourceBinding.Spec.ApplyStrategy
	return &work, nil
}

// generateSnapshotWorkObj generates the work object for the corresponding snapshot
func generateSnapshotWorkObj(workName string, resourceBinding *fleetv1beta1.ClusterResourceBinding, resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot,
	manifest []fleetv1beta1.Manifest, resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash string) *fleetv1beta1.Work {
	return &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.Spec.TargetCluster),
			Labels: map[string]string{
				fleetv1beta1.ParentBindingLabel:               resourceBinding.Name,
				fleetv1beta1.CRPTrackingLabel:                 resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
				fleetv1beta1.ParentResourceSnapshotIndexLabel: resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel],
			},
			Annotations: map[string]string{
				fleetv1beta1.ParentResourceSnapshotNameAnnotation:                resourceBinding.Spec.ResourceSnapshotName,
				fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        resourceOverrideSnapshotHash,
				fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: clusterResourceOverrideSnapshotHash,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         fleetv1beta1.GroupVersion.String(),
					Kind:               resourceBinding.Kind,
					Name:               resourceBinding.Name,
					UID:                resourceBinding.UID,
					BlockOwnerDeletion: ptr.To(true), // make sure that the k8s will call work delete when the binding is deleted
				},
			},
		},
		Spec: fleetv1beta1.WorkSpec{
			Workload: fleetv1beta1.WorkloadTemplate{
				Manifests: manifest,
			},
			ApplyStrategy: resourceBinding.Spec.ApplyStrategy,
		},
	}
}

// upsertWork creates or updates the new work for the corresponding resource snapshot.
// it returns if any change is made to the existing work and the possible error code.
func (r *Reconciler) upsertWork(ctx context.Context, newWork, existingWork *fleetv1beta1.Work, resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot) (bool, error) {
	workObj := klog.KObj(newWork)
	resourceSnapshotObj := klog.KObj(resourceSnapshot)
	if existingWork == nil {
		if err := r.Client.Create(ctx, newWork); err != nil {
			klog.ErrorS(err, "Failed to create the work associated with the resourceSnapshot", "resourceSnapshot", resourceSnapshotObj, "work", workObj)
			return false, controller.NewCreateIgnoreAlreadyExistError(err)
		}
		klog.V(2).InfoS("Successfully create the work associated with the resourceSnapshot",
			"resourceSnapshot", resourceSnapshotObj, "work", workObj)
		return true, nil
	}
	// TODO: remove the compare after we did the check on all work in the sync all
	// check if we need to update the existing work object
	workResourceIndex, err := labels.ExtractResourceSnapshotIndexFromWork(existingWork)
	if err != nil {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "work has invalid parent resource index", "work", workObj)
	} else {
		// we already checked the label in fetchAllResourceSnapShots function so no need to check again
		resourceIndex, _ := labels.ExtractResourceIndexFromClusterResourceSnapshot(resourceSnapshot)
		if workResourceIndex == resourceIndex {
			// no need to do anything if the work is generated from the same resource/override snapshots.
			// Note that apply strategy is updated separately beforehand.
			if existingWork.Annotations[fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation] == newWork.Annotations[fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation] &&
				existingWork.Annotations[fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation] == newWork.Annotations[fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation] {
				klog.V(2).InfoS("Work is associated with the desired resource/override snapshots", "existingROHash", existingWork.Annotations[fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation],
					"existingCROHash", existingWork.Annotations[fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation], "work", workObj)
				return false, nil
			}
			klog.V(2).InfoS("Work is already associated with the desired resourceSnapshot but still not having the right override snapshots", "resourceIndex", resourceIndex, "work", workObj, "resourceSnapshot", resourceSnapshotObj)
		}
	}
	// need to copy the new work to the existing work, only 5 possible changes:
	if existingWork.Labels == nil {
		existingWork.Labels = make(map[string]string)
	}
	existingWork.Labels[fleetv1beta1.ParentResourceSnapshotIndexLabel] = newWork.Labels[fleetv1beta1.ParentResourceSnapshotIndexLabel]
	if existingWork.Annotations == nil {
		existingWork.Annotations = make(map[string]string)
	}
	existingWork.Annotations[fleetv1beta1.ParentResourceSnapshotNameAnnotation] = newWork.Annotations[fleetv1beta1.ParentResourceSnapshotNameAnnotation]
	existingWork.Annotations[fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation] = newWork.Annotations[fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation]
	existingWork.Annotations[fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation] = newWork.Annotations[fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation]
	existingWork.Spec.Workload.Manifests = newWork.Spec.Workload.Manifests
	existingWork.Spec.ApplyStrategy = newWork.Spec.ApplyStrategy
	if err := r.Client.Update(ctx, existingWork); err != nil {
		klog.ErrorS(err, "Failed to update the work associated with the resourceSnapshot", "resourceSnapshot", resourceSnapshotObj, "work", workObj)
		return true, controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Successfully updated the work associated with the resourceSnapshot", "resourceSnapshot", resourceSnapshotObj, "work", workObj)
	return true, nil
}

// getWorkNamePrefixFromSnapshotName extract the CRP and sub-index name from the corresponding resource snapshot.
// The corresponding work name prefix is the CRP name + sub-index if there is a sub-index. Otherwise, it is the CRP name +"-work".
// For example, if the resource snapshot name is "crp-1-0", the corresponding work name is "crp-0".
// If the resource snapshot name is "crp-1", the corresponding work name is "crp-work".
func getWorkNamePrefixFromSnapshotName(resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot) (string, error) {
	// The validation webhook should make sure the label and annotation are valid on all resource snapshot.
	// We are just being defensive here.
	crpName, exist := resourceSnapshot.Labels[fleetv1beta1.CRPTrackingLabel]
	if !exist {
		return "", controller.NewUnexpectedBehaviorError(fmt.Errorf("resource snapshot %s has an invalid CRP tracking label", resourceSnapshot.Name))
	}
	subIndex, exist := resourceSnapshot.Annotations[fleetv1beta1.SubindexOfResourceSnapshotAnnotation]
	if !exist {
		// master snapshot doesn't have sub-index
		return fmt.Sprintf(fleetv1beta1.FirstWorkNameFmt, crpName), nil
	}
	subIndexVal, err := strconv.Atoi(subIndex)
	if err != nil || subIndexVal < 0 {
		return "", controller.NewUnexpectedBehaviorError(fmt.Errorf("resource snapshot %s has an invalid sub-index annotation %d or err %w", resourceSnapshot.Name, subIndexVal, err))
	}
	return fmt.Sprintf(fleetv1beta1.WorkNameWithSubindexFmt, crpName, subIndexVal), nil
}

// workConditionSummarizedStatus helps produce a summary status of a group of Applied, Available, or
// DiffReported conditions.
type workConditionSummarizedStatus int

const (
	// workConditionSummarizedStatusIncomplete signals that some of the given conditions are not
	// set yet, or have become stale.
	workConditionSummarizedStatusIncomplete workConditionSummarizedStatus = iota
	// workConditionSummarizedStatusTrue signals that all of the given conditions are fresh and set to True.
	workConditionSummarizedStatusTrue
	// workConditionSummarizedStatusFalse signals that all of the given conditions are fresh and
	// at least one of the given conditions is set to False.
	workConditionSummarizedStatusFalse
)

// setBindingStatus sets the binding status based on the works associated with the binding.
func setBindingStatus(works map[string]*fleetv1beta1.Work, resourceBinding *fleetv1beta1.ClusterResourceBinding) {
	bindingRef := klog.KObj(resourceBinding)

	// Note (chenyu1): the work generator will refresh the status of a ClusterResourceBinding using
	// the following logic:
	//
	// a) If the currently active apply strategy (as dictated by the ClusterResourceBinding spec)
	//    is ClientSideApply or ServerSideApply, the work generator will update the Applied and
	//    Available conditions (plus the details about failed, diffed, and/or drifted placements)
	//    in the status, as appropriate; the DiffReported condition will not be updated.
	// b) If the currently active apply strategy is ReportDiff, the work generator will update
	//    the DiffReported condition in the status, plus the details about diffed placements;
	//    the Applied and Available conditions (plus the details about failed and/or drifted placements)
	//    will not be updated.

	// try to gather the resource binding applied status if we didn't update any associated work spec this time

	var isReportDiffModeOn = resourceBinding.Spec.ApplyStrategy != nil && resourceBinding.Spec.ApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff
	var appliedSummarizedStatus, availabilitySummarizedStatus, diffReportedSummarizedStatus workConditionSummarizedStatus
	if isReportDiffModeOn {
		// Set the DiffReported condition if (and only if) a ReportDiff apply strategy is currently
		// being used.
		diffReportedSummarizedStatus = setAllWorkDiffReportedCondition(works, resourceBinding)
	} else {
		// Set the Applied and Available condition if (and only if) a ClientSideApply or ServerSideApply
		// apply strategy is currently being used.
		appliedSummarizedStatus = setAllWorkAppliedCondition(works, resourceBinding)
		// Note that Fleet will only set the Available condition if the apply op itself is successful, i.e.,
		// the Applied condition is True.
		availabilitySummarizedStatus = setAllWorkAvailableCondition(works, resourceBinding)
	}

	resourceBinding.Status.FailedPlacements = nil
	resourceBinding.Status.DiffedPlacements = nil
	resourceBinding.Status.DriftedPlacements = nil
	// collect and set the failed resource placements to the binding if not all the works are available
	driftedResourcePlacements := make([]fleetv1beta1.DriftedResourcePlacement, 0, maxDriftedResourcePlacementLimit) // preallocate the memory
	failedResourcePlacements := make([]fleetv1beta1.FailedResourcePlacement, 0, maxFailedResourcePlacementLimit)    // preallocate the memory
	diffedResourcePlacements := make([]fleetv1beta1.DiffedResourcePlacement, 0, maxDiffedResourcePlacementLimit)    // preallocate the memory
	for _, w := range works {
		if w.DeletionTimestamp != nil {
			klog.V(2).InfoS("Ignoring the deleting work", "clusterResourceBinding", bindingRef, "work", klog.KObj(w))
			continue // ignore the deleting work
		}

		// Populate the failed, diffed, and drifted placements based on the summarized status of the Applied,
		// Available, and DiffReported conditions on all Work objects.
		//
		// Note (chenyu1): Fleet will only report apply/availability check failures, diffs, and drifts (as applicable)
		// when all the Work objects have completed their apply ops, availability checks, and diff reporting, as dictated
		// by the currently specified apply strategy (successful or not). This is to make sure that previously
		// populated failures, diffs, and/or drifts will not leak into the current reportings.
		switch {
		case isReportDiffModeOn && diffReportedSummarizedStatus == workConditionSummarizedStatusTrue:
			// The ReportDiff apply straregy is in use and all works have reported configuration
			// differences.
			//
			// In this case, set diffed placements only; failed and drifted placements will not
			// be set (apply/availability check failure and drifts cannot occur in report diff mode).
			diffedManifests := extractDiffedResourcePlacementsFromWork(w)
			diffedResourcePlacements = append(diffedResourcePlacements, diffedManifests...)
		case isReportDiffModeOn:
			// The ReportDiff apply strategy is in use but not all works have reported configuration
			// differences.
			//
			// In this case, no diffed, failed, or drifted placements will be set (diff information present
			// might be incomplete or stale; apply/availability check failure and drifts cannot occur in
			// report diff mode).
		case appliedSummarizedStatus == workConditionSummarizedStatusIncomplete:
			// The ClientSideApply or ServerSideApply apply strategy is in use but some of the works have
			// not been applied yet.
			//
			// In this case, no diffed, failed, or drifted placements will be set (as information present
			// might be incomplete or stale).
		case appliedSummarizedStatus == workConditionSummarizedStatusFalse:
			// The ClientSideApply or ServerSideApply apply strategy is in use but some of the works have
			// apply op failures.
			//
			// In this case, set failed, diffed, and drifted placements.
			failedManifests := extractFailedResourcePlacementsFromWork(w)
			failedResourcePlacements = append(failedResourcePlacements, failedManifests...)

			diffedManifests := extractDiffedResourcePlacementsFromWork(w)
			diffedResourcePlacements = append(diffedResourcePlacements, diffedManifests...)

			driftedManifests := extractDriftedResourcePlacementsFromWork(w)
			driftedResourcePlacements = append(driftedResourcePlacements, driftedManifests...)
		case availabilitySummarizedStatus == workConditionSummarizedStatusIncomplete:
			// The ClientSideApply or ServerSideApply apply strategy is in use; all works have been applied but
			// some of the works have not completed the availability check yet.
			//
			// In theory this would not happen as the Fleet work applier will always set the Applied and
			// Available conditions together. However, Fleet can still handle this case for completeness reasons.
			//
			// In this case, set drifted placements; no failed or diffed placements will be set (availability
			// check failure information might be incomplete or stale; diffs will only occur when there
			// is an apply failure or the report diff mode is on).
			driftedManifests := extractDriftedResourcePlacementsFromWork(w)
			driftedResourcePlacements = append(driftedResourcePlacements, driftedManifests...)
		case availabilitySummarizedStatus == workConditionSummarizedStatusFalse:
			// The ClientSideApply or ServerSideApply apply strategy is in use; all works have been applied but
			// some of them have failed the availability check.
			//
			// In this case, set failed and drifted placements; no diffed placements will be set (diffs
			// will only occur when there is an apply failure or the report diff mode is on).
			failedManifests := extractFailedResourcePlacementsFromWork(w)
			failedResourcePlacements = append(failedResourcePlacements, failedManifests...)

			driftedManifests := extractDriftedResourcePlacementsFromWork(w)
			driftedResourcePlacements = append(driftedResourcePlacements, driftedManifests...)
		default:
			// The ClientSideApply or ServerSideApply apply strategy is in use; all works have been applied
			// and are available.
			//
			// In this case, set only drifted placements (drifts might occur even if the apply op itself
			// completes); no failed or diffed placements will be set (apply/availability
			// check failure and diffs will not occur when all works are applied and available).
			driftedManifests := extractDriftedResourcePlacementsFromWork(w)
			driftedResourcePlacements = append(driftedResourcePlacements, driftedManifests...)
		}
	}
	// cut the list to keep only the max limit
	if len(failedResourcePlacements) > maxFailedResourcePlacementLimit {
		failedResourcePlacements = failedResourcePlacements[0:maxFailedResourcePlacementLimit]
	}
	if len(failedResourcePlacements) > 0 {
		resourceBinding.Status.FailedPlacements = failedResourcePlacements
		klog.V(2).InfoS("Populated failed manifests", "clusterResourceBinding", bindingRef, "numberOfFailedPlacements", len(failedResourcePlacements))
	}

	// cut the list to keep only the max limit
	if len(diffedResourcePlacements) > maxDiffedResourcePlacementLimit {
		// Sort the slice
		sort.Slice(diffedResourcePlacements, func(i, j int) bool {
			return utils.LessFuncDiffedResourcePlacements(diffedResourcePlacements[i], diffedResourcePlacements[j])
		})
		diffedResourcePlacements = diffedResourcePlacements[0:maxDiffedResourcePlacementLimit]
	}
	if len(diffedResourcePlacements) > 0 {
		resourceBinding.Status.DiffedPlacements = diffedResourcePlacements
		klog.V(2).InfoS("Populated diffed manifests", "clusterResourceBinding", bindingRef, "numberOfDiffedPlacements", len(diffedResourcePlacements))
	}

	// cut the list to keep only the max limit
	if len(driftedResourcePlacements) > maxDriftedResourcePlacementLimit {
		// Sort the slice
		sort.Slice(driftedResourcePlacements, func(i, j int) bool {
			return utils.LessFuncDriftedResourcePlacements(driftedResourcePlacements[i], driftedResourcePlacements[j])
		})
		driftedResourcePlacements = driftedResourcePlacements[0:maxDriftedResourcePlacementLimit]
	}
	if len(driftedResourcePlacements) > 0 {
		resourceBinding.Status.DriftedPlacements = driftedResourcePlacements
		klog.V(2).InfoS("Populated drifted manifests", "clusterResourceBinding", bindingRef, "numberOfDriftedPlacements", len(driftedResourcePlacements))
	}
}

// setAllWorkAppliedCondition sets the Applied condition on a ClusterResourceBinding
// based on the Applied conditions on all the related Work objects.
//
// The Applied condition of a ClusterResourceBinding object is set to True if and only if all the
// related Work objects have their Applied condition set to True.
func setAllWorkAppliedCondition(works map[string]*fleetv1beta1.Work, binding *fleetv1beta1.ClusterResourceBinding) workConditionSummarizedStatus {
	// Fleet here makes a clear distinction between incomplete, failed, and successful apply operations.
	// This is to ensure that stale apply information (esp. those set before
	// an apply strategy change) will not leak into the current apply operations.
	areAllWorksApplyOpsCompleted := true
	areAllWorksApplyOpsSuccessful := true

	var firstWorkWithIncompleteApplyOp *fleetv1beta1.Work
	var firstWorkWithFailedApplyOp *fleetv1beta1.Work

	for _, w := range works {
		applyCond := meta.FindStatusCondition(w.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
		switch {
		case condition.IsConditionStatusTrue(applyCond, w.GetGeneration()):
			// The Work object has completed the apply op successfully.
		case condition.IsConditionStatusFalse(applyCond, w.GetGeneration()):
			// An error has occurred during the apply op.
			areAllWorksApplyOpsSuccessful = false
			if firstWorkWithFailedApplyOp == nil {
				firstWorkWithFailedApplyOp = w
			}
		default:
			// The Work object has not yet completed the apply op.
			areAllWorksApplyOpsCompleted = false
			if firstWorkWithIncompleteApplyOp == nil {
				firstWorkWithIncompleteApplyOp = w
			}
		}
	}

	switch {
	case !areAllWorksApplyOpsCompleted:
		// Not all Work objects have completed the apply op.
		klog.V(2).InfoS("Some works are not yet completed the apply op", "binding", klog.KObj(binding), "firstWorkWithIncompleteApplyOp", klog.KObj(firstWorkWithIncompleteApplyOp))
		binding.SetConditions(metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourceBindingApplied),
			Reason:             condition.WorkNotAppliedReason,
			Message:            fmt.Sprintf("Work object %s has not yet completed the apply op", firstWorkWithIncompleteApplyOp.Name),
			ObservedGeneration: binding.GetGeneration(),
		})
		return workConditionSummarizedStatusIncomplete
	case !areAllWorksApplyOpsSuccessful:
		// All Work objects have completed the apply op, but at least one of them has failed.
		klog.V(2).InfoS("Some works have failed to apply", "binding", klog.KObj(binding), "firstWorkWithFailedApplyOp", klog.KObj(firstWorkWithFailedApplyOp))
		binding.SetConditions(metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourceBindingApplied),
			Reason:             condition.WorkNotAppliedReason,
			Message:            fmt.Sprintf("Work object %s has failed to apply", firstWorkWithFailedApplyOp.Name),
			ObservedGeneration: binding.GetGeneration(),
		})
		return workConditionSummarizedStatusFalse
	default:
		// All Work objects have completed the apply op successfully.
		klog.V(2).InfoS("All works associated with the binding are applied", "binding", klog.KObj(binding))
		binding.SetConditions(metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceBindingApplied),
			Reason:             condition.AllWorkAppliedReason,
			Message:            "All corresponding work objects are applied",
			ObservedGeneration: binding.GetGeneration(),
		})
		return workConditionSummarizedStatusTrue
	}
}

// setAllWorkDiffReportedCondition sets the DiffReported condition on a ClusterResourceBinding
// based on the DiffReported conditions on all the related Work objects.
//
// The DiffReported condition of a ClusterResourceBinding object is set to True if and only if all the
// related Work objects have their DiffReported condition set to True.
func setAllWorkDiffReportedCondition(works map[string]*fleetv1beta1.Work, binding *fleetv1beta1.ClusterResourceBinding) workConditionSummarizedStatus {
	// Fleet here makes a clear distinction between incomplete, failed, and successful diff reportings.
	// This is to ensure that stale diff information (esp. those set before
	// an apply strategy change) will not leak into the current reportings.
	areAllWorksDiffReportingCompleted := true
	areAllWorksDiffReportingSuccessful := true

	var firstWorkWithIncompleteDiffReporting *fleetv1beta1.Work
	var firstWorkWithFailedDiffReporting *fleetv1beta1.Work

	for _, w := range works {
		diffReportedCond := meta.FindStatusCondition(w.Status.Conditions, fleetv1beta1.WorkConditionTypeDiffReported)
		switch {
		case condition.IsConditionStatusTrue(diffReportedCond, w.GetGeneration()):
			// The Work object has completed diff reporting successfully.
		case condition.IsConditionStatusFalse(diffReportedCond, w.GetGeneration()):
			// An error has occurred during the diff reporting process.
			areAllWorksDiffReportingSuccessful = false
			if firstWorkWithFailedDiffReporting == nil {
				firstWorkWithFailedDiffReporting = w
			}
		default:
			// The Work object has not yet completed diff reporting.
			areAllWorksDiffReportingCompleted = false
			if firstWorkWithIncompleteDiffReporting == nil {
				firstWorkWithIncompleteDiffReporting = w
			}
		}
	}

	switch {
	case !areAllWorksDiffReportingCompleted:
		// Not all Work objects have completed diff reporting.
		klog.V(2).InfoS("Some works are not yet completed diff reporting", "binding", klog.KObj(binding), "firstWorkWithIncompleteDiffReporting", klog.KObj(firstWorkWithIncompleteDiffReporting))
		meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourceBindingDiffReported),
			Reason:             condition.WorkNotDiffReportedReason,
			Message:            fmt.Sprintf("Work object %s has not yet completed diff reporting", firstWorkWithIncompleteDiffReporting.Name),
			ObservedGeneration: binding.GetGeneration(),
		})
		return workConditionSummarizedStatusIncomplete
	case !areAllWorksDiffReportingSuccessful:
		// All Work objects have completed diff reporting, but at least one of them has failed.
		klog.V(2).InfoS("Some works have failed to report diff", "binding", klog.KObj(binding), "firstWorkWithFailedDiffReporting", klog.KObj(firstWorkWithFailedDiffReporting))
		meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourceBindingDiffReported),
			Reason:             condition.WorkNotDiffReportedReason,
			Message:            fmt.Sprintf("Work object %s has failed to report diff", firstWorkWithFailedDiffReporting.Name),
			ObservedGeneration: binding.GetGeneration(),
		})
		return workConditionSummarizedStatusFalse
	default:
		// All Work objects have completed diff reporting successfully.
		klog.V(2).InfoS("All works associated with the binding have reported diff", "binding", klog.KObj(binding))
		meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceBindingDiffReported),
			Reason:             condition.AllWorkDiffReportedReason,
			Message:            "All corresponding work objects have reported diff",
			ObservedGeneration: binding.GetGeneration(),
		})
		return workConditionSummarizedStatusTrue
	}
}

// setAllWorkAvailableCondition sets the Available condition on a ClusterResourceBinding
// based on the Available conditions on all the related Work objects.
//
// The Available condition of a ClusterResourceBinding object is set to True if and only if all the
// related Work objects have their Available condition set to True.
func setAllWorkAvailableCondition(works map[string]*fleetv1beta1.Work, binding *fleetv1beta1.ClusterResourceBinding) workConditionSummarizedStatus {
	// If the Applied condition has been set to False, skip setting the Available condition.
	appliedCond := meta.FindStatusCondition(binding.Status.Conditions, string(fleetv1beta1.ResourceBindingApplied))
	if !condition.IsConditionStatusTrue(appliedCond, binding.GetGeneration()) {
		klog.V(2).InfoS("Some works are not yet applied or have failed to get applied; skip populating the Available condition", "binding", klog.KObj(binding))
		return workConditionSummarizedStatusFalse
	}

	// Fleet here makes a clear distinction between incomplete, failed and successful availability checks.
	// This is to ensure that stale information will not leak into the current reportings.
	areAllWorksAvailabilityCheckCompleted := true
	areAllWorksAvailabilityCheckSuccessful := true

	var firstWorkWithIncompleteAvailabilityCheck *fleetv1beta1.Work
	var firstWorkWithFailedAvailabilityCheck *fleetv1beta1.Work
	var firstWorkWithSuccessfulAvailabilityCheckDueToUntrackableRes *fleetv1beta1.Work
	for _, w := range works {
		availableCond := meta.FindStatusCondition(w.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		switch {
		case condition.IsConditionStatusTrue(availableCond, w.GetGeneration()) && availableCond.Reason == workapplier.WorkNotAllManifestsTrackableReasonNew:
			// The Work object has completed the availability check successfully, due to the
			// resources being untrackable.
			//
			// This branch is currently never visited as the work applier would still populate
			// the Available condition using the old reason string for compatibility reasons.
			if firstWorkWithSuccessfulAvailabilityCheckDueToUntrackableRes == nil {
				firstWorkWithSuccessfulAvailabilityCheckDueToUntrackableRes = w
			}
		case condition.IsConditionStatusTrue(availableCond, w.GetGeneration()) && availableCond.Reason == workapplier.WorkNotAllManifestsTrackableReason:
			// The Work object has completed the availability check successfully, due to the
			// resources being untrackable. This is the same branch as the one above but checks
			// for the old reason string; it is kept for compatibility reasons.
			//
			// TO-DO (chenyu1): drop this branch after the rollout completes.
			if firstWorkWithSuccessfulAvailabilityCheckDueToUntrackableRes == nil {
				firstWorkWithSuccessfulAvailabilityCheckDueToUntrackableRes = w
			}
		case condition.IsConditionStatusTrue(availableCond, w.GetGeneration()):
			// The Work object has completed the availability check successfully.
		case condition.IsConditionStatusFalse(availableCond, w.GetGeneration()):
			// The Work object has failed the availability check.
			areAllWorksAvailabilityCheckSuccessful = false
			if firstWorkWithFailedAvailabilityCheck == nil {
				firstWorkWithFailedAvailabilityCheck = w
			}
		default:
			// The Work object has not yet completed the availability check.
			//
			// This in theory should never happen as the Fleet work applier always set the Applied
			// and Available conditions on a Work object together in one call and Fleet will not
			// check resource availability if the apply op itself has failed. However, Fleet can
			// still handle this case for completeness reasons.
			areAllWorksAvailabilityCheckCompleted = false
			if firstWorkWithIncompleteAvailabilityCheck == nil {
				firstWorkWithIncompleteAvailabilityCheck = w
			}
		}
	}

	switch {
	case !areAllWorksAvailabilityCheckCompleted:
		// Not all Work objects have completed the availability check.
		//
		// As previously explained, this should never happen in practice. Fleet here handles
		// this case for completeness reasons.
		klog.V(2).InfoS("Some works are not yet completed availability check", "binding", klog.KObj(binding), "firstWorkWithIncompleteAvailabilityCheck", klog.KObj(firstWorkWithIncompleteAvailabilityCheck))
		meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourceBindingAvailable),
			Reason:             condition.WorkNotAvailableReason,
			Message:            fmt.Sprintf("Work object %s has not yet completed availability check", firstWorkWithIncompleteAvailabilityCheck.Name),
			ObservedGeneration: binding.GetGeneration(),
		})
		return workConditionSummarizedStatusIncomplete
	case !areAllWorksAvailabilityCheckSuccessful:
		// All Work objects have completed the availability check, but at least one of them has failed.
		klog.V(2).InfoS("Some works have failed to get available", "binding", klog.KObj(binding), "firstWorkWithFailedAvailabilityCheck", klog.KObj(firstWorkWithFailedAvailabilityCheck))
		meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourceBindingAvailable),
			Reason:             condition.WorkNotAvailableReason,
			Message:            fmt.Sprintf("Work object %s is not yet available", firstWorkWithFailedAvailabilityCheck.Name),
			ObservedGeneration: binding.GetGeneration(),
		})
		return workConditionSummarizedStatusFalse
	case firstWorkWithSuccessfulAvailabilityCheckDueToUntrackableRes != nil:
		// All Work objects have completed the availability check successfully, and at least one of them has succeeded due to untrackable resources.
		klog.V(2).InfoS("All works associated with the binding are available; untrackable resources are present", "binding", klog.KObj(binding), "firstWorkWithSuccessfulAvailabilityCheckDueToUntrackableRes", klog.KObj(firstWorkWithSuccessfulAvailabilityCheckDueToUntrackableRes))
		meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceBindingAvailable),
			Reason:             condition.WorkNotAvailabilityTrackableReason,
			Message:            fmt.Sprintf("The availability of work object %s is not trackable", firstWorkWithSuccessfulAvailabilityCheckDueToUntrackableRes.Name),
			ObservedGeneration: binding.GetGeneration(),
		})
		return workConditionSummarizedStatusTrue
	default:
		// All Work objects have completed the availability check successfully.
		klog.V(2).InfoS("All works associated with the binding are available", "binding", klog.KObj(binding))
		meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceBindingAvailable),
			Reason:             condition.AllWorkAvailableReason,
			Message:            "All corresponding work objects are available",
			ObservedGeneration: binding.GetGeneration(),
		})
		return workConditionSummarizedStatusTrue
	}
}

func extractResFromConfigMap(uConfigMap *unstructured.Unstructured) ([]fleetv1beta1.Manifest, error) {
	manifests := make([]fleetv1beta1.Manifest, 0)
	var configMap corev1.ConfigMap
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(uConfigMap.Object, &configMap)
	if err != nil {
		return nil, err
	}
	// the list order is not stable as the map traverse is random
	for _, value := range configMap.Data {
		content, jsonErr := yaml.ToJSON([]byte(value))
		if jsonErr != nil {
			return nil, jsonErr
		}
		manifests = append(manifests, fleetv1beta1.Manifest{
			RawExtension: runtime.RawExtension{Raw: content},
		})
	}
	// stable sort the manifests so that we can have a deterministic order
	sort.Slice(manifests, func(i, j int) bool {
		obj1 := manifests[i].Raw
		obj2 := manifests[j].Raw
		// order by its json formatted string
		return strings.Compare(string(obj1), string(obj2)) > 0
	})
	return manifests, nil
}

// extractFailedResourcePlacementsFromWork extracts the failed resource placements from the work.
func extractFailedResourcePlacementsFromWork(work *fleetv1beta1.Work) []fleetv1beta1.FailedResourcePlacement {
	appliedCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
	availableCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)

	// The applied condition and available condition are always updated in one call.
	// It means the observedGeneration of these two are always the same.
	// If IsConditionStatusFalse is true, means both are observing the latest work.
	if !condition.IsConditionStatusFalse(appliedCond, work.Generation) &&
		!condition.IsConditionStatusFalse(availableCond, work.Generation) {
		return nil
	}

	// check if the work is generated by an enveloped object
	envelopeType, isEnveloped := work.GetLabels()[fleetv1beta1.EnvelopeTypeLabel]
	var envelopObjName, envelopObjNamespace string
	if isEnveloped {
		// If the work  generated by an enveloped object, it must contain those labels.
		envelopObjName = work.GetLabels()[fleetv1beta1.EnvelopeNameLabel]
		envelopObjNamespace = work.GetLabels()[fleetv1beta1.EnvelopeNamespaceLabel]
	}
	res := make([]fleetv1beta1.FailedResourcePlacement, 0, len(work.Status.ManifestConditions))
	for _, manifestCondition := range work.Status.ManifestConditions {
		failedManifest := fleetv1beta1.FailedResourcePlacement{
			ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
				Group:     manifestCondition.Identifier.Group,
				Version:   manifestCondition.Identifier.Version,
				Kind:      manifestCondition.Identifier.Kind,
				Name:      manifestCondition.Identifier.Name,
				Namespace: manifestCondition.Identifier.Namespace,
			},
		}
		if isEnveloped {
			failedManifest.ResourceIdentifier.Envelope = &fleetv1beta1.EnvelopeIdentifier{
				Name:      envelopObjName,
				Namespace: envelopObjNamespace,
				Type:      fleetv1beta1.EnvelopeType(envelopeType),
			}
		}

		appliedCond = meta.FindStatusCondition(manifestCondition.Conditions, fleetv1beta1.WorkConditionTypeApplied)
		// collect if there is an explicit fail
		// The observedGeneration of the manifest condition is the generation of the applied manifest.
		// The overall applied and available conditions are observing the latest work generation.
		// So that the manifest condition should be latest, assuming they're populated by the work agent in one update call.
		if appliedCond != nil && appliedCond.Status == metav1.ConditionFalse {
			if isEnveloped {
				klog.V(2).InfoS("Find a failed to apply enveloped manifest",
					"manifestName", manifestCondition.Identifier.Name,
					"group", manifestCondition.Identifier.Group,
					"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind,
					"envelopeType", envelopeType, "envelopObjName", envelopObjName, "envelopObjNamespace", envelopObjNamespace)
			} else {
				klog.V(2).InfoS("Find a failed to apply manifest",
					"manifestName", manifestCondition.Identifier.Name, "group", manifestCondition.Identifier.Group,
					"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind)
			}
			failedManifest.Condition = *appliedCond
			res = append(res, failedManifest)
			continue //jump to the next manifest
		}
		availableCond = meta.FindStatusCondition(manifestCondition.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		if availableCond != nil && availableCond.Status == metav1.ConditionFalse {
			if isEnveloped {
				klog.V(2).InfoS("Find an unavailable enveloped manifest",
					"manifestName", manifestCondition.Identifier.Name,
					"group", manifestCondition.Identifier.Group,
					"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind,
					"envelopeType", envelopeType, "envelopObjName", envelopObjName, "envelopObjNamespace", envelopObjNamespace)
			} else {
				klog.V(2).InfoS("Find an unavailable manifest",
					"manifestName", manifestCondition.Identifier.Name, "group", manifestCondition.Identifier.Group,
					"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind)
			}
			failedManifest.Condition = *availableCond
			res = append(res, failedManifest)
		}
	}
	return res
}

// extractDriftedResourcePlacementsFromWork extracts the drifted placements from work
func extractDriftedResourcePlacementsFromWork(work *fleetv1beta1.Work) []fleetv1beta1.DriftedResourcePlacement {
	// check if the work is generated by an enveloped object
	envelopeType, isEnveloped := work.GetLabels()[fleetv1beta1.EnvelopeTypeLabel]
	var envelopObjName, envelopObjNamespace string
	if isEnveloped {
		// If the work  generated by an enveloped object, it must contain those labels.
		envelopObjName = work.GetLabels()[fleetv1beta1.EnvelopeNameLabel]
		envelopObjNamespace = work.GetLabels()[fleetv1beta1.EnvelopeNamespaceLabel]
	}
	res := make([]fleetv1beta1.DriftedResourcePlacement, 0, len(work.Status.ManifestConditions))
	for _, manifestCondition := range work.Status.ManifestConditions {
		if manifestCondition.DriftDetails == nil {
			continue
		}
		driftedManifest := fleetv1beta1.DriftedResourcePlacement{
			ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
				Group:     manifestCondition.Identifier.Group,
				Version:   manifestCondition.Identifier.Version,
				Kind:      manifestCondition.Identifier.Kind,
				Name:      manifestCondition.Identifier.Name,
				Namespace: manifestCondition.Identifier.Namespace,
			},
			ObservationTime:                 manifestCondition.DriftDetails.ObservationTime,
			TargetClusterObservedGeneration: manifestCondition.DriftDetails.ObservedInMemberClusterGeneration,
			FirstDriftedObservedTime:        manifestCondition.DriftDetails.FirstDriftedObservedTime,
			ObservedDrifts:                  manifestCondition.DriftDetails.ObservedDrifts,
		}

		if isEnveloped {
			driftedManifest.ResourceIdentifier.Envelope = &fleetv1beta1.EnvelopeIdentifier{
				Name:      envelopObjName,
				Namespace: envelopObjNamespace,
				Type:      fleetv1beta1.EnvelopeType(envelopeType),
			}
			klog.V(2).InfoS("Found a drifted enveloped manifest",
				"manifestName", manifestCondition.Identifier.Name,
				"group", manifestCondition.Identifier.Group,
				"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind,
				"envelopeType", envelopeType, "envelopObjName", envelopObjName, "envelopObjNamespace", envelopObjNamespace)
		} else {
			klog.V(2).InfoS("Found a drifted manifest",
				"manifestName", manifestCondition.Identifier.Name, "group", manifestCondition.Identifier.Group,
				"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind)
		}
		res = append(res, driftedManifest)
	}
	return res
}

// extractDiffedResourcePlacementsFromWork extracts the diffed placements from work
func extractDiffedResourcePlacementsFromWork(work *fleetv1beta1.Work) []fleetv1beta1.DiffedResourcePlacement {
	// check if the work is generated by an enveloped object
	envelopeType, isEnveloped := work.GetLabels()[fleetv1beta1.EnvelopeTypeLabel]
	var envelopObjName, envelopObjNamespace string
	if isEnveloped {
		// If the work  generated by an enveloped object, it must contain those labels.
		envelopObjName = work.GetLabels()[fleetv1beta1.EnvelopeNameLabel]
		envelopObjNamespace = work.GetLabels()[fleetv1beta1.EnvelopeNamespaceLabel]
	}
	res := make([]fleetv1beta1.DiffedResourcePlacement, 0, len(work.Status.ManifestConditions))
	for _, manifestCondition := range work.Status.ManifestConditions {
		if manifestCondition.DiffDetails == nil {
			continue
		}
		diffedManifest := fleetv1beta1.DiffedResourcePlacement{
			ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
				Group:     manifestCondition.Identifier.Group,
				Version:   manifestCondition.Identifier.Version,
				Kind:      manifestCondition.Identifier.Kind,
				Name:      manifestCondition.Identifier.Name,
				Namespace: manifestCondition.Identifier.Namespace,
			},
			ObservationTime:                 manifestCondition.DiffDetails.ObservationTime,
			TargetClusterObservedGeneration: manifestCondition.DiffDetails.ObservedInMemberClusterGeneration,
			FirstDiffedObservedTime:         manifestCondition.DiffDetails.FirstDiffedObservedTime,
			ObservedDiffs:                   manifestCondition.DiffDetails.ObservedDiffs,
		}

		if isEnveloped {
			diffedManifest.ResourceIdentifier.Envelope = &fleetv1beta1.EnvelopeIdentifier{
				Name:      envelopObjName,
				Namespace: envelopObjNamespace,
				Type:      fleetv1beta1.EnvelopeType(envelopeType),
			}
			klog.V(2).InfoS("Found a diffed enveloped manifest",
				"manifestName", manifestCondition.Identifier.Name,
				"group", manifestCondition.Identifier.Group,
				"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind,
				"envelopeType", envelopeType, "envelopObjName", envelopObjName, "envelopObjNamespace", envelopObjNamespace)
		} else {
			klog.V(2).InfoS("Found a diffed manifest",
				"manifestName", manifestCondition.Identifier.Name, "group", manifestCondition.Identifier.Group,
				"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind)
		}
		res = append(res, diffedManifest)
	}
	return res
}

// SetupWithManager sets up the controller with the Manager.
// It watches binding events and also update/delete events for work.
func (r *Reconciler) SetupWithManager(mgr controllerruntime.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("work generator")
	return controllerruntime.NewControllerManagedBy(mgr).Named("work-generator").
		WithOptions(ctrl.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}). // set the max number of concurrent reconciles
		For(&fleetv1beta1.ClusterResourceBinding{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&fleetv1beta1.Work{}, &handler.Funcs{
			// we care about work delete event as we want to know when a work is deleted so that we can
			// delete the corresponding resource binding fast.
			DeleteFunc: func(ctx context.Context, evt event.DeleteEvent, queue workqueue.RateLimitingInterface) {
				if evt.Object == nil {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("deleteEvent %v received with no metadata", evt)),
						"Failed to process a delete event for work object")
					return
				}
				parentBindingName, exist := evt.Object.GetLabels()[fleetv1beta1.ParentBindingLabel]
				if !exist {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("deleted work has no binding parent")),
						"Could not find the parent binding label", "deleted work", evt.Object, "existing label", evt.Object.GetLabels())
					return
				}
				// Make sure the work is not deleted behind our back
				klog.V(2).InfoS("Received a work delete event", "work", klog.KObj(evt.Object), "parentBindingName", parentBindingName)
				queue.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name: parentBindingName,
				}})
			},
			// we care about work update event as we want to know when a work is applied so that we can
			// update the corresponding resource binding status fast.
			UpdateFunc: func(ctx context.Context, evt event.UpdateEvent, queue workqueue.RateLimitingInterface) {
				if evt.ObjectOld == nil || evt.ObjectNew == nil {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("updateEvent %v received with no metadata", evt)),
						"Failed to process an update event for work object")
					return
				}
				parentBindingName, exist := evt.ObjectNew.GetLabels()[fleetv1beta1.ParentBindingLabel]
				if !exist {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("work has no binding parent")),
						"Could not find the parent binding label", "updatedWork", evt.ObjectNew, "existing label", evt.ObjectNew.GetLabels())
					return
				}
				oldWork, ok := evt.ObjectOld.(*fleetv1beta1.Work)
				if !ok {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("received old object %v not a work object", evt.ObjectOld)),
						"Failed to process an update event for work object")
					return
				}
				newWork, ok := evt.ObjectNew.(*fleetv1beta1.Work)
				if !ok {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("received new object %v not a work object", evt.ObjectNew)),
						"Failed to process an update event for work object")
					return
				}

				if !equality.Semantic.DeepEqual(oldWork.Status, newWork.Status) {
					klog.V(2).InfoS("Work status has been changed", "oldWork", klog.KObj(oldWork), "newWork", klog.KObj(newWork))
				} else {
					oldResourceSnapshot := oldWork.Labels[fleetv1beta1.ParentResourceSnapshotIndexLabel]
					newResourceSnapshot := newWork.Labels[fleetv1beta1.ParentResourceSnapshotIndexLabel]
					if oldResourceSnapshot == "" || newResourceSnapshot == "" {
						klog.ErrorS(controller.NewUnexpectedBehaviorError(errors.New("found an invalid work without parent-resource-snapshot-index")),
							"Could not find the parent resource snapshot index label", "oldWork", klog.KObj(oldWork), "oldResourceSnapshotLabelValue", oldResourceSnapshot,
							"newWork", klog.KObj(newWork), "newResourceSnapshotLabelValue", newResourceSnapshot)
						return
					}
					// There is an edge case that, the work spec is the same but from different resourceSnapshots.
					// WorkGenerator will update the work because of the label changes, but the generation is the same.
					// When the normal update happens, the controller will set the applied condition as false and wait
					// until the work condition has been changed.
					// In this edge case, we need to requeue the binding to update the binding status.
					if oldResourceSnapshot == newResourceSnapshot {
						klog.V(2).InfoS("The work applied or available condition stayed as true, no need to reconcile", "oldWork", klog.KObj(oldWork), "newWork", klog.KObj(newWork))
						return
					}
				}

				// We need to update the binding status in this case
				klog.V(2).InfoS("Received a work update event that we need to handle", "work", klog.KObj(newWork), "parentBindingName", parentBindingName)
				queue.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name: parentBindingName,
				}})
			},
		}).
		Complete(r)
}
