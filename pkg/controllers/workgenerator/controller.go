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
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	workapi "go.goms.io/fleet/pkg/controllers/work"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/labels"
)

const (
	allWorkSyncedReason  = "AllWorkSynced"
	syncWorkFailedReason = "SyncWorkFailed"
	workNeedSyncedReason = "StillNeedToSyncWork"
	workNotAppliedReason = "NotAllWorkHasBeenApplied"
	allWorkAppliedReason = "AllWorkHasBeenApplied"
)

var (
	errResourceSnapshotNotFound = errors.New("the master resource snapshot is not found")
	errResourceNotFullyCreated  = errors.New("not all resource snapshot in the same index group are created")
)

// Reconciler watches binding objects and generate work objects in the designated cluster namespace
// according to the information in the binding objects.
// TODO: incorporate an overriding policy if one exists
type Reconciler struct {
	client.Client
	recorder record.EventRecorder
}

// Reconcile triggers a single binding reconcile round.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get the resource binding", "resourceBinding", bindingRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}

	// handle the case the binding is deleting
	if resourceBinding.DeletionTimestamp != nil {
		return r.handleDelete(ctx, resourceBinding.DeepCopy())
	}

	// we only care about the bound bindings. We treat unscheduled bindings as bound until they are deleted.
	if resourceBinding.Spec.State != fleetv1beta1.BindingStateBound && resourceBinding.Spec.State != fleetv1beta1.BindingStateUnscheduled {
		klog.V(2).InfoS("Skip reconcile clusterResourceBinding that is not bound", "state", resourceBinding.Spec.State, "resourceBinding", bindingRef)
		return ctrl.Result{}, nil
	}

	// make sure that the resource binding obj has a finalizer
	if err := r.ensureFinalizer(ctx, &resourceBinding); err != nil {
		return ctrl.Result{}, err
	}

	workUpdated := false
	// list all the corresponding works
	works, syncErr := r.listAllWorksAssociated(ctx, &resourceBinding)
	if syncErr == nil {
		// generate and apply the workUpdated works if we have all the works
		workUpdated, syncErr = r.syncAllWork(ctx, &resourceBinding, works)
	}

	if syncErr != nil {
		klog.ErrorS(syncErr, "Failed to sync all the works", "resourceBinding", bindingRef)
		resourceBinding.SetConditions(metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourceBindingBound),
			Reason:             syncWorkFailedReason,
			Message:            syncErr.Error(),
			ObservedGeneration: resourceBinding.Generation,
		})
	} else {
		resourceBinding.SetConditions(metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceBindingBound),
			Reason:             allWorkSyncedReason,
			ObservedGeneration: resourceBinding.Generation,
		})
		if workUpdated {
			// revert the applied condition if we made any changes to the work
			resourceBinding.SetConditions(metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingApplied),
				Reason:             workNeedSyncedReason,
				Message:            "The work needs to be synced first",
				ObservedGeneration: resourceBinding.Generation,
			})
		} else {
			// try to gather the resource binding applied status if we didn't update any associated work spec this time
			resourceBinding.SetConditions(buildAllWorkAppliedCondition(works, &resourceBinding))
		}
	}

	// update the resource binding status
	if updateErr := r.Client.Status().Update(ctx, &resourceBinding); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the resourceBinding status", "resourceBinding", bindingRef)
		return ctrl.Result{}, controller.NewUpdateIgnoreConflictError(updateErr)
	}
	if errors.Is(syncErr, errResourceSnapshotNotFound) {
		// This error usually indicates that the resource snapshot is deleted since the rollout controller which fills
		// the resource snapshot share the same informer cache with this controller. We don't need to retry in this case
		// since the resource snapshot will not come back. We will get another event if the binding is pointing to a new resource.
		// However, this error can happen when the resource snapshot exists during the IT test when the client that creates
		// the resource snapshot is not the same as the controller client so that we need to retry in this case.
		// This error can also happen if the user uses a customized rollout controller that does not share the same informer cache with this controller.
		return ctrl.Result{Requeue: true}, nil
	}
	// requeue if we did an update, or we failed to sync the work
	return ctrl.Result{Requeue: workUpdated}, syncErr
}

// handleDelete handle a deleting binding
func (r *Reconciler) handleDelete(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) (ctrl.Result, error) {
	klog.V(4).InfoS("Start to handle deleting resource binding", "resourceBinding", klog.KObj(resourceBinding))
	// list all the corresponding works if exist
	works, err := r.listAllWorksAssociated(ctx, resourceBinding)
	if err != nil {
		return ctrl.Result{}, err
	}

	// delete all the listed works
	//
	// TO-DO: this controller should be able to garbage collect all works automatically via
	// background/foreground cascade deletion. This may render the finalizer unnecessary.
	for workName := range works {
		work := works[workName]

		if err := r.Client.Delete(ctx, work); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, controller.NewAPIServerError(false, err)
		}
	}

	// remove the work finalizer on the binding if all the work objects are deleted
	if len(works) == 0 {
		controllerutil.RemoveFinalizer(resourceBinding, fleetv1beta1.WorkFinalizer)
		if err = r.Client.Update(ctx, resourceBinding); err != nil {
			klog.ErrorS(err, "Failed to remove the work finalizer from resource binding", "resourceBinding", klog.KObj(resourceBinding))
			return ctrl.Result{}, controller.NewUpdateIgnoreConflictError(err)
		}
		klog.V(2).InfoS("The resource binding is deleted", "resourceBinding", klog.KObj(resourceBinding))
		return ctrl.Result{}, nil
	}
	klog.V(2).InfoS("The resource binding still has undeleted work", "resourceBinding", klog.KObj(resourceBinding),
		"number of associated work", len(works))
	// we watch the work objects deleting events, so we can afford to wait a bit longer here as a fallback case.
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// ensureFinalizer makes sure that the resourceSnapshot CR has a finalizer on it.
func (r *Reconciler) ensureFinalizer(ctx context.Context, resourceBinding client.Object) error {
	if controllerutil.ContainsFinalizer(resourceBinding, fleetv1beta1.WorkFinalizer) {
		return nil
	}
	controllerutil.AddFinalizer(resourceBinding, fleetv1beta1.WorkFinalizer)
	if err := r.Client.Update(ctx, resourceBinding); err != nil {
		klog.ErrorS(err, "Failed to add the work finalizer to resourceBinding", "resourceBinding", klog.KObj(resourceBinding))
		return controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Successfully add the work finalizer", "resourceBinding", klog.KObj(resourceBinding))
	return nil
}

// listAllWorksAssociated finds all the live work objects that are associated with this binding.
func (r *Reconciler) listAllWorksAssociated(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) (map[string]*workv1alpha1.Work, error) {
	namespaceMatcher := client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.Spec.TargetCluster))
	parentBindingLabelMatcher := client.MatchingLabels{
		fleetv1beta1.ParentBindingLabel: resourceBinding.Name,
	}
	currentWork := make(map[string]*workv1alpha1.Work)
	workList := &workv1alpha1.WorkList{}
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
// it returns if we actually made any changes on the hub cluster.
func (r *Reconciler) syncAllWork(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding, works map[string]*workv1alpha1.Work) (bool, error) {
	updateAny := false
	resourceBindingRef := klog.KObj(resourceBinding)

	// Gather all the resource resourceSnapshots
	resourceSnapshots, err := r.fetchAllResourceSnapshots(ctx, resourceBinding)
	if err != nil {
		// TODO(RZ): handle errResourceNotFullyCreated error
		return false, err
	}

	// create/update the corresponding work for each snapshot
	activeWork := make(map[string]bool, len(resourceSnapshots))
	for _, snapshot := range resourceSnapshots {
		// TODO(RZ): issue those requests in parallel to speed up the process
		updated := false
		workName, err := getWorkNameFromSnapshotName(snapshot)
		if err != nil {
			klog.ErrorS(err, "Encountered a mal-formatted resource snapshot", "resourceSnapshot", klog.KObj(snapshot))
			return false, err
		}
		activeWork[workName] = true
		if updated, err = r.upsertWork(ctx, works[workName], workName, snapshot, resourceBinding); err != nil {
			return false, err
		}
		if updated {
			updateAny = true
		}
	}

	//  delete the works that are not associated with any resource snapshot
	for _, work := range works {
		if activeWork[work.Name] {
			continue
		}
		klog.V(2).InfoS("Delete the work that is not associated with any resource snapshot", "work", klog.KObj(work))
		if err := r.Client.Delete(ctx, work); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to delete the no longer needed work", "work", klog.KObj(work))
				return false, controller.NewAPIServerError(false, err)
			}
		}
		updateAny = true
	}
	klog.V(2).InfoS("Successfully synced all the work associated with the resourceBinding", "updateAny", updateAny, "resourceBinding", resourceBindingRef)
	return updateAny, nil
}

// fetchAllResourceSnapshots gathers all the resource snapshots for the resource binding.
func (r *Reconciler) fetchAllResourceSnapshots(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) (map[string]*fleetv1beta1.ClusterResourceSnapshot, error) {
	// fetch the master snapshot first
	resourceSnapshots := make(map[string]*fleetv1beta1.ClusterResourceSnapshot)
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
	resourceSnapshots[masterResourceSnapshot.Name] = &masterResourceSnapshot

	// check if there are more snapshot in the same index group
	countAnnotation := masterResourceSnapshot.Annotations[fleetv1beta1.NumberOfResourceSnapshotsAnnotation]
	snapshotCount, err := strconv.Atoi(countAnnotation)
	if err != nil || snapshotCount < 1 {
		return nil, controller.NewUnexpectedBehaviorError(fmt.Errorf(
			"master resource snapshot %s has an invalid snapshot count %d or err %w", masterResourceSnapshot.Name, snapshotCount, err))
	}
	if snapshotCount > 1 {
		// fetch all the resource snapshot in the same index group
		index, err := labels.ExtractResourceIndexFromClusterResourceSnapshot(&masterResourceSnapshot)
		if err != nil {
			klog.ErrorS(err, "master resource snapshot has invalid resource index", "clusterResourceSnapshot", klog.KObj(&masterResourceSnapshot))
			return nil, controller.NewUnexpectedBehaviorError(err)
		}
		resourceIndexLabelMatcher := client.MatchingLabels{
			fleetv1beta1.ResourceIndexLabel: strconv.Itoa(index),
			fleetv1beta1.CRPTrackingLabel:   resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
		}
		resourceSnapshotList := &fleetv1beta1.ClusterResourceSnapshotList{}
		if err := r.Client.List(ctx, resourceSnapshotList, resourceIndexLabelMatcher); err != nil {
			klog.ErrorS(err, "Failed to list all the resource snapshot associated with the resourceBinding", "resourceBinding", klog.KObj(resourceBinding))
			return nil, controller.NewAPIServerError(true, err)
		}
		//insert all the resource snapshot into the map
		for i := 0; i < len(resourceSnapshotList.Items); i++ {
			resourceSnapshots[resourceSnapshotList.Items[i].Name] = &resourceSnapshotList.Items[i]
		}
	}
	// check if all the resource snapshots are created since that may take a while but the rollout controller may update the resource binding on master snapshot creation
	if len(resourceSnapshots) != snapshotCount {
		misMatchErr := fmt.Errorf("%w: resource snapshots are still being created for the masterResourceSnapshot %s, total snapshot in the index group = %d, num Of existing snapshot in the group= %d",
			errResourceNotFullyCreated, resourceBinding.Name, snapshotCount, len(resourceSnapshots))
		klog.ErrorS(misMatchErr, "Resource snapshot associated with the binding are not ready", "resourceBinding", klog.KObj(resourceBinding))
		// make sure the reconcile requeue the request
		return nil, controller.NewExpectedBehaviorError(misMatchErr)
	}
	klog.V(2).InfoS("Get all the resource snapshot associated with the binding", "numOfSnapshot", len(resourceSnapshots), "resourceBinding", klog.KObj(resourceBinding))
	return resourceSnapshots, nil
}

// upsertWork creates or updates the work for the corresponding resource snapshot.
// it returns if any change is made to the work and the possible error code.
func (r *Reconciler) upsertWork(ctx context.Context, work *workv1alpha1.Work, workName string, resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot,
	resourceBinding *fleetv1beta1.ClusterResourceBinding) (bool, error) {
	needCreate := false
	var workObj klog.ObjectRef
	resourceBindingObj := klog.KObj(resourceBinding)
	resourceSnapshotObj := klog.KObj(resourceSnapshot)
	// we already checked the label in fetchAllResourceSnapShots function so no need to check again
	resourceIndex, _ := labels.ExtractResourceIndexFromClusterResourceSnapshot(resourceSnapshot)
	if work == nil {
		needCreate = true
		work = &workv1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Name:      workName,
				Namespace: fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.Spec.TargetCluster),
				Labels: map[string]string{
					fleetv1beta1.ParentBindingLabel: resourceBinding.Name,
					fleetv1beta1.CRPTrackingLabel:   resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         fleetv1beta1.GroupVersion.String(),
						Kind:               resourceBinding.Kind,
						Name:               resourceBinding.Name,
						UID:                resourceBinding.UID,
						BlockOwnerDeletion: pointer.Bool(true), // make sure that the k8s will call work delete when the binding is deleted
					},
				},
			},
		}
	} else {
		// check if we need to update the work
		workObj = klog.KObj(work)
		workResourceIndex, err := labels.ExtractResourceSnapshotIndexFromWork(work)
		if err != nil {
			klog.ErrorS(err, "work has invalid parent resource index", "work", workObj)
			return false, controller.NewUnexpectedBehaviorError(err)
		}
		if workResourceIndex == resourceIndex {
			// no need to do anything since the resource snapshot is immutable.
			klog.V(2).InfoS("Work is already associated with the desired resourceSnapshot", "work", workObj, "resourceSnapshot", resourceSnapshotObj)
			return false, nil
		}
	}
	workObj = klog.KObj(work)
	// the work is pointing to a different resource snapshot, need to reset the manifest list
	// reset the manifest list regardless and make sure the work is pointing to the right resource snapshot
	work.Labels[fleetv1beta1.ParentResourceSnapshotIndexLabel] = resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel]
	work.Spec.Workload.Manifests = make([]workv1alpha1.Manifest, 0)
	for _, selectedResource := range resourceSnapshot.Spec.SelectedResources {
		work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, workv1alpha1.Manifest{
			RawExtension: selectedResource.RawExtension,
		})
	}

	// upsert the work
	if needCreate {
		if err := r.Client.Create(ctx, work); err != nil {
			klog.ErrorS(err, "Failed to create the work associated with the resourceSnapshot", "resourceBinding", resourceBindingObj,
				"resourceSnapshot", resourceSnapshotObj, "work", workObj)
			return true, controller.NewCreateIgnoreAlreadyExistError(err)
		}
	} else if err := r.Client.Update(ctx, work); err != nil {
		klog.ErrorS(err, "Failed to update the work associated with the resourceSnapshot", "resourceBinding", resourceBindingObj,
			"resourceSnapshot", resourceSnapshotObj, "work", workObj)
		return true, controller.NewUpdateIgnoreConflictError(err)
	}

	klog.V(2).InfoS("Successfully upsert the work associated with the resourceSnapshot", "isCreate", needCreate,
		"resourceBinding", resourceBindingObj, "resourceSnapshot", resourceSnapshotObj, "work", workObj)
	return true, nil
}

// getWorkNameFromSnapshotName extract the CRP and sub-index name from the corresponding resource snapshot.
// The corresponding work name is the CRP name + sub-index if there is a sub-index. Otherwise, it is the CRP name +"-work".
// For example, if the resource snapshot name is "crp-1-0", the corresponding work name is "crp-0".
// If the resource snapshot name is "crp-1", the corresponding work name is "crp-work".
func getWorkNameFromSnapshotName(resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot) (string, error) {
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

func buildAllWorkAppliedCondition(works map[string]*workv1alpha1.Work, binding *fleetv1beta1.ClusterResourceBinding) metav1.Condition {
	allApplied := true
	for _, work := range works {
		if !condition.IsConditionStatusTrue(meta.FindStatusCondition(work.Status.Conditions, workapi.ConditionTypeApplied), work.GetGeneration()) {
			allApplied = false
			break
		}
	}
	if allApplied {
		klog.V(2).InfoS("All works associated with the binding is applied", "binding", klog.KObj(binding))
		return metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceBindingApplied),
			Reason:             allWorkAppliedReason,
			ObservedGeneration: binding.GetGeneration(),
		}
	}
	return metav1.Condition{
		Status:             metav1.ConditionFalse,
		Type:               string(fleetv1beta1.ResourceBindingApplied),
		Reason:             workNotAppliedReason,
		Message:            "not all corresponding work objects are applied",
		ObservedGeneration: binding.GetGeneration(),
	}
}

// SetupWithManager sets up the controller with the Manager.
// It watches binding events and also update/delete events for work.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("work generator")
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1beta1.ClusterResourceBinding{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&source.Kind{Type: &workv1alpha1.Work{}}, &handler.Funcs{
			// we care about work delete event as we want to know when a work is deleted so that we can
			// delete the corresponding resource binding fast.
			DeleteFunc: func(evt event.DeleteEvent, queue workqueue.RateLimitingInterface) {
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
			UpdateFunc: func(evt event.UpdateEvent, queue workqueue.RateLimitingInterface) {
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
				oldWork, ok := evt.ObjectOld.(*workv1alpha1.Work)
				if !ok {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("received old object %v not a work object", evt.ObjectOld)),
						"Failed to process an update event for work object")
					return
				}
				newWork, ok := evt.ObjectNew.(*workv1alpha1.Work)
				if !ok {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("received new object %v not a work object", evt.ObjectNew)),
						"Failed to process an update event for work object")
					return
				}
				oldAppliedStatus := meta.FindStatusCondition(oldWork.Status.Conditions, workapi.ConditionTypeApplied)
				newAppliedStatus := meta.FindStatusCondition(newWork.Status.Conditions, workapi.ConditionTypeApplied)
				// we only need to handle the case the applied condition is flipped between true and NOT true between the
				// new and old work objects. Otherwise, it won't affect the binding applied condition
				if condition.IsConditionStatusTrue(oldAppliedStatus, oldWork.GetGeneration()) == condition.IsConditionStatusTrue(newAppliedStatus, newWork.GetGeneration()) {
					klog.V(2).InfoS("The work applied condition didn't flip between true and false", "oldWork", klog.KObj(oldWork), "newWork", klog.KObj(newWork))
					return
				}
				klog.V(2).InfoS("Received a work update event", "work", klog.KObj(newWork), "parentBindingName", parentBindingName)
				// We need to update the binding status in this case
				queue.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name: parentBindingName,
				}})
			},
		}).
		Complete(r)
}
