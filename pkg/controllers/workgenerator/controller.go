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
	"go.goms.io/fleet/pkg/controllers/work"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
	"go.goms.io/fleet/pkg/utils/labels"
)

var (
	// maxFailedResourcePlacementLimit indicates the max number of failed resource placements to include in the status.
	maxFailedResourcePlacementLimit = 100

	errResourceSnapshotNotFound = errors.New("the master resource snapshot is not found")
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

	workUpdated := false
	overrideSucceeded := false
	// Reset the conditions and failed placements.
	for i := condition.OverriddenCondition; i < condition.TotalCondition; i++ {
		resourceBinding.RemoveCondition(string(i.ResourceBindingConditionType()))
	}
	klog.V(2).Info("Check RolloutStartedCondition", "resourceBinding", bindingRef, "conditions", resourceBinding.Status.Conditions)
	if cond := resourceBinding.GetCondition(string(fleetv1beta1.ResourceBindingRolloutStarted)); cond != nil {
		klog.V(2).InfoS("Has RolloutStartedCondition", "resourceBinding", bindingRef, "condition", cond)
	}
	resourceBinding.Status.FailedPlacements = nil
	// list all the corresponding works
	works, syncErr := r.listAllWorksAssociated(ctx, &resourceBinding)
	if syncErr == nil {
		// generate and apply the workUpdated works if we have all the works
		overrideSucceeded, workUpdated, syncErr = r.syncAllWork(ctx, &resourceBinding, works, cluster)
	}

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
				Message:            fmt.Sprintf("Failed to sychronize the work to the latest: %s", errorMessage),
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
		if workUpdated {
			// revert the applied condition if we made any changes to the work
			resourceBinding.SetConditions(metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingApplied),
				Reason:             condition.WorkNeedSyncedReason,
				Message:            "In the processing of synchronizing the work to the member cluster",
				ObservedGeneration: resourceBinding.Generation,
			})
		} else {
			setBindingStatus(works, &resourceBinding)
		}
	}

	// update the resource binding status
	if updateErr := r.patchBindingStatusWithRetry(ctx, &resourceBinding); updateErr != nil {
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

// patchBindingStatusWIthRetry sends the update request to API server with retry.
func (r *Reconciler) patchBindingStatusWithRetry(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) error {
	// TODO: Check the latest binding state and handle it
	errAfterRetries := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var latestBinding fleetv1beta1.ClusterResourceBinding
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(resourceBinding), &latestBinding); err != nil {
			return err
		}
		klog.V(2).Info("Check latest binding Conditions", "latestResourceBinding", klog.KObj(&latestBinding), "conditions", latestBinding.Status.Conditions)
		klog.V(2).Info("Check Conditions", "resourceBinding", klog.KObj(resourceBinding), "conditions", resourceBinding.Status.Conditions)
		if cond := resourceBinding.GetCondition(string(fleetv1beta1.ResourceBindingRolloutStarted)); cond != nil {
			klog.V(2).InfoS("Has RolloutStartedCondition", "resourceBinding", klog.KObj(resourceBinding), "condition", cond)
		}
		if cond := resourceBinding.GetCondition(string(fleetv1beta1.ResourceBindingRolloutStarted)); cond != nil {
			klog.V(2).InfoS("Has RolloutStartedCondition", "resourceBinding", klog.KObj(resourceBinding), "condition", cond)
		}
		if rolloutCond := latestBinding.GetCondition(string(fleetv1beta1.ResourceBindingRolloutStarted)); rolloutCond != nil {
			found := false
			for i := range resourceBinding.Status.Conditions {
				if resourceBinding.Status.Conditions[i].Type == rolloutCond.Type {
					// Replace the existing condition
					resourceBinding.Status.Conditions[i] = *rolloutCond
					found = true
					break
				}
			}
			if !found {
				// Prepend the new condition if it wasn't found
				resourceBinding.Status.Conditions = append([]metav1.Condition{*rolloutCond}, resourceBinding.Status.Conditions...)
			}
		}

		if err := r.Client.Status().Patch(ctx, resourceBinding.DeepCopy(), client.MergeFromWithOptions(&latestBinding, client.MergeFromWithOptimisticLock{})); err != nil {
			klog.ErrorS(err, "Failed to patch the resourceBinding status", "resourceBinding", klog.KObj(resourceBinding), "resourceBindingStatus", resourceBinding.Status)
			return err
		}
		klog.V(2).InfoS("Successfully updated the resourceBinding status", "resourceBinding", klog.KObj(resourceBinding), "resourceBindingStatus", resourceBinding.Status)
		return nil
	})
	if errAfterRetries != nil {
		klog.ErrorS(errAfterRetries, "Failed to patch resourceBinding status after retries", "resourceBinding", klog.KObj(resourceBinding))
		return errAfterRetries
	}
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
	// Also note that here default backoff strategy (exponetial backoff) rather than the Kubernetes'
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
func (r *Reconciler) syncAllWork(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding, existingWorks map[string]*fleetv1beta1.Work, cluster clusterv1beta1.MemberCluster) (bool, bool, error) {
	updateAny := atomic.NewBool(false)
	resourceBindingRef := klog.KObj(resourceBinding)

	// Gather all the resource resourceSnapshots
	resourceSnapshots, err := r.fetchAllResourceSnapshots(ctx, resourceBinding)
	if err != nil {
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
	errs, cctx := errgroup.WithContext(ctx)
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
			selectedResource := snapshot.Spec.SelectedResources[j]
			if err := r.applyOverrides(&selectedResource, cluster, croMap, roMap); err != nil {
				return false, false, err
			}

			// we need to special treat configMap with envelopeConfigMapAnnotation annotation,
			// so we need to check the GVK and annotation of the selected resource
			var uResource unstructured.Unstructured
			if err := uResource.UnmarshalJSON(selectedResource.Raw); err != nil {
				klog.ErrorS(err, "work has invalid content", "snapshot", klog.KObj(snapshot), "selectedResource", selectedResource.Raw)
				return true, false, controller.NewUnexpectedBehaviorError(err)
			}
			if uResource.GetObjectKind().GroupVersionKind() == utils.ConfigMapGVK &&
				len(uResource.GetAnnotations()[fleetv1beta1.EnvelopeConfigMapAnnotation]) != 0 {
				// get a work object for the enveloped configMap
				work, err := r.getConfigMapEnvelopWorkObj(ctx, workNamePrefix, resourceBinding, snapshot, &uResource)
				if err != nil {
					return true, false, err
				}
				activeWork[work.Name] = work
				newWork = append(newWork, work)
			} else {
				simpleManifests = append(simpleManifests, fleetv1beta1.Manifest(selectedResource))
			}
		}
		if len(simpleManifests) == 0 {
			klog.V(2).InfoS("the snapshot contains enveloped resource only", "snapshot", klog.KObj(snapshot))
		}
		// generate a work object for the manifests even if there is nothing to place
		// to allow CRP to collect the status of the placement
		// TODO (RZ): revisit to see if we need this hack
		work := generateSnapshotWorkObj(workNamePrefix, resourceBinding, snapshot, simpleManifests)
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
	resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot, envelopeObj *unstructured.Unstructured) (*fleetv1beta1.Work, error) {
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
	// we just pick the first one if there are more than one.
	work := workList.Items[0]
	work.Labels[fleetv1beta1.ParentResourceSnapshotIndexLabel] = resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel]
	work.Spec.Workload.Manifests = manifest
	work.Spec.ApplyStrategy = resourceBinding.Spec.ApplyStrategy
	return &work, nil
}

// generateSnapshotWorkObj generates the work object for the corresponding snapshot
func generateSnapshotWorkObj(workName string, resourceBinding *fleetv1beta1.ClusterResourceBinding, resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot, manifest []fleetv1beta1.Manifest) *fleetv1beta1.Work {
	return &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.Spec.TargetCluster),
			Labels: map[string]string{
				fleetv1beta1.ParentBindingLabel:               resourceBinding.Name,
				fleetv1beta1.CRPTrackingLabel:                 resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
				fleetv1beta1.ParentResourceSnapshotIndexLabel: resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel],
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
	// check if we need to update the existing work object
	workResourceIndex, err := labels.ExtractResourceSnapshotIndexFromWork(existingWork)
	if err != nil {
		klog.ErrorS(err, "work has invalid parent resource index", "work", workObj)
		return false, controller.NewUnexpectedBehaviorError(err)
	}
	// we already checked the label in fetchAllResourceSnapShots function so no need to check again
	resourceIndex, _ := labels.ExtractResourceIndexFromClusterResourceSnapshot(resourceSnapshot)
	if workResourceIndex == resourceIndex {
		// no need to do anything if the work is generated from the same resource snapshot group since the resource snapshot is immutable.
		klog.V(2).InfoS("Work is already associated with the desired resourceSnapshot", "resourceIndex", resourceIndex, "work", workObj, "resourceSnapshot", resourceSnapshotObj)
		return false, nil
	}
	// need to update the existing work, only two possible changes:
	existingWork.Labels[fleetv1beta1.ParentResourceSnapshotIndexLabel] = resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel]
	existingWork.Spec.Workload.Manifests = newWork.Spec.Workload.Manifests
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

// setBindingStatus sets the binding status based on the works associated with the binding.
func setBindingStatus(works map[string]*fleetv1beta1.Work, resourceBinding *fleetv1beta1.ClusterResourceBinding) {
	bindingRef := klog.KObj(resourceBinding)
	// try to gather the resource binding applied status if we didn't update any associated work spec this time
	appliedCond := buildAllWorkAppliedCondition(works, resourceBinding)
	resourceBinding.SetConditions(appliedCond)
	var availableCond metav1.Condition
	// only try to gather the available status if all the work objects are applied
	if appliedCond.Status == metav1.ConditionTrue {
		availableCond = buildAllWorkAvailableCondition(works, resourceBinding)
		resourceBinding.SetConditions(availableCond)
	}
	resourceBinding.Status.FailedPlacements = nil
	// collect and set the failed resource placements to the binding if not all the works are available
	if appliedCond.Status != metav1.ConditionTrue || availableCond.Status != metav1.ConditionTrue {
		failedResourcePlacements := make([]fleetv1beta1.FailedResourcePlacement, 0, maxFailedResourcePlacementLimit) // preallocate the memory
		for _, w := range works {
			if w.DeletionTimestamp != nil {
				klog.V(2).InfoS("Ignoring the deleting work", "clusterResourceBinding", bindingRef, "work", klog.KObj(w))
				continue // ignore the deleting work
			}
			failedManifests := extractFailedResourcePlacementsFromWork(w)
			failedResourcePlacements = append(failedResourcePlacements, failedManifests...)
		}
		// cut the list to keep only the max limit
		if len(failedResourcePlacements) > maxFailedResourcePlacementLimit {
			failedResourcePlacements = failedResourcePlacements[0:maxFailedResourcePlacementLimit]
		}
		resourceBinding.Status.FailedPlacements = failedResourcePlacements
		if len(failedResourcePlacements) > 0 {
			klog.V(2).InfoS("Populated failed manifests", "clusterResourceBinding", bindingRef, "numberOfFailedPlacements", len(failedResourcePlacements))
		}
	}
}

func buildAllWorkAppliedCondition(works map[string]*fleetv1beta1.Work, binding *fleetv1beta1.ClusterResourceBinding) metav1.Condition {
	allApplied := true
	var notAppliedWork string
	for _, w := range works {
		if !condition.IsConditionStatusTrue(meta.FindStatusCondition(w.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied), w.GetGeneration()) {
			allApplied = false
			notAppliedWork = w.Name
			break
		}
	}
	if allApplied {
		klog.V(2).InfoS("All works associated with the binding are applied", "binding", klog.KObj(binding))
		return metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceBindingApplied),
			Reason:             condition.AllWorkAppliedReason,
			Message:            "All corresponding work objects are applied",
			ObservedGeneration: binding.GetGeneration(),
		}
	}
	return metav1.Condition{
		Status:             metav1.ConditionFalse,
		Type:               string(fleetv1beta1.ResourceBindingApplied),
		Reason:             condition.WorkNotAppliedReason,
		Message:            fmt.Sprintf("Work object %s is not applied", notAppliedWork),
		ObservedGeneration: binding.GetGeneration(),
	}
}

func buildAllWorkAvailableCondition(works map[string]*fleetv1beta1.Work, binding *fleetv1beta1.ClusterResourceBinding) metav1.Condition {
	allAvailable := true
	var notAvailableWork string
	var notTrackableWork string
	for _, w := range works {
		cond := meta.FindStatusCondition(w.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		if !condition.IsConditionStatusTrue(cond, w.GetGeneration()) {
			allAvailable = false
			notAvailableWork = w.Name
			break
		}
		if cond.Reason == work.WorkNotTrackableReason {
			notTrackableWork = w.Name
		}
	}
	if allAvailable {
		klog.V(2).InfoS("All works associated with the binding are available", "binding", klog.KObj(binding), "notTrackableWork", notTrackableWork)
		reason := condition.AllWorkAvailableReason
		message := "All corresponding work objects are available"
		if len(notTrackableWork) > 0 {
			reason = work.WorkNotTrackableReason
			message = fmt.Sprintf("The availability of work object %s is not trackable", notTrackableWork)
		}

		return metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceBindingAvailable),
			Reason:             reason,
			Message:            message,
			ObservedGeneration: binding.GetGeneration(),
		}
	}
	return metav1.Condition{
		Status:             metav1.ConditionFalse,
		Type:               string(fleetv1beta1.ResourceBindingAvailable),
		Reason:             condition.WorkNotAvailableReason,
		Message:            fmt.Sprintf("Work object %s is not available", notAvailableWork),
		ObservedGeneration: binding.GetGeneration(),
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
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("deleteEvent %v received with no matadata", evt)),
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
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("updateEvent %v received with no matadata", evt)),
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
				oldAppliedCondition := meta.FindStatusCondition(oldWork.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
				newAppliedCondition := meta.FindStatusCondition(newWork.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
				oldAvailableCondition := meta.FindStatusCondition(oldWork.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
				newAvailableCondition := meta.FindStatusCondition(newWork.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)

				// we try to filter out events, we only need to handle the updated event if the applied or available condition flip between true and false
				// or the failed placements are changed.
				if condition.EqualCondition(oldAppliedCondition, newAppliedCondition) && condition.EqualCondition(oldAvailableCondition, newAvailableCondition) {
					if condition.IsConditionStatusFalse(newAppliedCondition, newWork.Generation) || condition.IsConditionStatusFalse(newAvailableCondition, newWork.Generation) {
						// we need to compare the failed placement if the work is not applied or available
						oldFailedPlacements := extractFailedResourcePlacementsFromWork(oldWork)
						newFailedPlacements := extractFailedResourcePlacementsFromWork(newWork)
						if utils.IsFailedResourcePlacementsEqual(oldFailedPlacements, newFailedPlacements) {
							klog.V(2).InfoS("The failed placement list didn't change on failed work, no need to reconcile", "oldWork", klog.KObj(oldWork), "newWork", klog.KObj(newWork))
							return
						}
					} else {
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
