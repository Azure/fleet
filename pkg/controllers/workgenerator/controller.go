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
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/record"
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

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
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
	// the max number of concurrent reconciles per controller.
	MaxConcurrentReconciles int
	recorder                record.EventRecorder
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
		klog.V(2).InfoS("Skip reconcile clusterResourceBinding that is not bound", "state", resourceBinding.Spec.State, "resourceBinding", bindingRef)
		return controllerruntime.Result{}, nil
	}

	// make sure that the resource binding obj has a finalizer
	if err := r.ensureFinalizer(ctx, &resourceBinding); err != nil {
		return controllerruntime.Result{}, err
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
		return controllerruntime.Result{}, controller.NewUpdateIgnoreConflictError(updateErr)
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
	// requeue if we did an update, or we failed to sync the work
	return controllerruntime.Result{Requeue: workUpdated}, syncErr
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
	controllerutil.AddFinalizer(resourceBinding, fleetv1beta1.WorkFinalizer)
	if err := r.Client.Update(ctx, resourceBinding); err != nil {
		klog.ErrorS(err, "Failed to add the work finalizer to resourceBinding", "resourceBinding", klog.KObj(resourceBinding))
		return controller.NewUpdateIgnoreConflictError(err)
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
// it returns if we actually made any changes on the hub cluster.
func (r *Reconciler) syncAllWork(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding, existingWorks map[string]*fleetv1beta1.Work) (bool, error) {
	updateAny := atomic.NewBool(false)
	resourceBindingRef := klog.KObj(resourceBinding)

	// Gather all the resource resourceSnapshots
	resourceSnapshots, err := r.fetchAllResourceSnapshots(ctx, resourceBinding)
	if err != nil {
		// TODO(RZ): handle errResourceNotFullyCreated error so we don't need to wait for all the snapshots to be created
		return false, err
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
			return false, err
		}
		var simpleManifests []fleetv1beta1.Manifest
		for _, selectedResource := range snapshot.Spec.SelectedResources {
			// we need to special treat configMap with envelopeConfigMapAnnotation annotation,
			// so we need to check the GVK and annotation of the selected resource
			var uResource unstructured.Unstructured
			if err := uResource.UnmarshalJSON(selectedResource.Raw); err != nil {
				klog.ErrorS(err, "work has invalid content", "snapshot", klog.KObj(snapshot), "selectedResource", selectedResource.Raw)
				return false, controller.NewUnexpectedBehaviorError(err)
			}
			if uResource.GetObjectKind().GroupVersionKind() == utils.ConfigMapGVK &&
				len(uResource.GetAnnotations()[fleetv1beta1.EnvelopeConfigMapAnnotation]) != 0 {
				// get a work object for the enveloped configMap
				work, err := r.getConfigMapEnvelopWorkObj(ctx, workNamePrefix, resourceBinding, snapshot, &uResource)
				if err != nil {
					return false, err
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
		for i := range newWork {
			work := newWork[i]
			errs.Go(func() error {
				updated, err := r.upsertWork(cctx, work, existingWorks[work.Name].DeepCopy(), snapshot)
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
		return false, updateErr
	}
	klog.V(2).InfoS("Successfully synced all the work associated with the resourceBinding", "updateAny", updateAny.Load(), "resourceBinding", resourceBindingRef)
	return updateAny.Load(), nil
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

// getConfigMapEnvelopWorkObj first try to locate a work object for the corresponding envelopObj of type configMap.
// we create a new one if the work object doesn't exist. We do this to avoid repeatedly delete and create the same work object.
// TODO: take into consider the override policy in the future
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
	return &work, nil
}

// generateSnapshotWorkObj generates the work object for the corresponding snapshot
// TODO: take into consider the override policy in the future
func generateSnapshotWorkObj(workName string, resourceBinding *fleetv1beta1.ClusterResourceBinding, resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot, manifest []fleetv1beta1.Manifest) *fleetv1beta1.Work {
	work := &fleetv1beta1.Work{
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
	}
	work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifest...)
	return work
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

func buildAllWorkAppliedCondition(works map[string]*fleetv1beta1.Work, binding *fleetv1beta1.ClusterResourceBinding) metav1.Condition {
	allApplied := true
	for _, work := range works {
		if !condition.IsConditionStatusTrue(meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied), work.GetGeneration()) {
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

func extractResFromConfigMap(uConfigMap *unstructured.Unstructured) ([]fleetv1beta1.Manifest, error) {
	manifests := make([]fleetv1beta1.Manifest, 0)
	var configMap v1.ConfigMap
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
				oldAppliedStatus := meta.FindStatusCondition(oldWork.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
				newAppliedStatus := meta.FindStatusCondition(newWork.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
				// we only need to handle the case the applied condition is flipped between true and NOT true between the
				// new and old work objects. Otherwise, it won't affect the binding applied condition
				if condition.IsConditionStatusTrue(oldAppliedStatus, oldWork.GetGeneration()) == condition.IsConditionStatusTrue(newAppliedStatus, newWork.GetGeneration()) {
					klog.V(2).InfoS("The work applied condition didn't flip between true and false, no need to reconcile", "oldWork", klog.KObj(oldWork), "newWork", klog.KObj(newWork))
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
