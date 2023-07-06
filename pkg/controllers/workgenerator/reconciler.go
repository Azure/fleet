/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workgenerator

import (
	"context"
	"fmt"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"go.goms.io/fleet/pkg/utils"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
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
	klog.V(2).InfoS("Start to reconcile a ClusterResourceBinding", "cluster resource binding", req.Name)
	startTime := time.Now()
	bindingRef := klog.KRef(req.Namespace, req.Name)
	// add latency log
	defer func() {
		klog.V(2).InfoS("ClusterResourceBinding reconciliation loop ends", "resource binding", bindingRef, "latency", time.Since(startTime).Milliseconds())
	}()
	var resourceBinding fleetv1beta1.ClusterResourceBinding
	if err := r.Client.Get(ctx, req.NamespacedName, &resourceBinding); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get the resource binding", "resource binding", bindingRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}

	// handle the case the binding is deleting
	if resourceBinding.DeletionTimestamp != nil {
		return r.handleDelete(ctx, resourceBinding.DeepCopy())
	}

	// update the resource binding obj with finalizer
	if err := r.ensureFinalizer(ctx, &resourceBinding); err != nil {
		return ctrl.Result{}, err
	}

	// we only care about the bound bindings
	if resourceBinding.Spec.State != fleetv1beta1.BindingStateBound {
		klog.V(2).InfoS("skip reconcile clusterResourceBinding that is not bound", "state", resourceBinding.Spec.State, "resource binding", bindingRef)
		return ctrl.Result{}, nil
	}

	// generate the works
	return r.syncAllWork(ctx, resourceBinding.DeepCopy())
}

// handleDelete handle a deleting binding
func (r *Reconciler) handleDelete(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) (ctrl.Result, error) {
	klog.V(4).InfoS("handle deleting resource binding", "resource binding", klog.KObj(resourceBinding))
	// list all the corresponding works if exist
	works, err := r.listAllWorksAssociated(ctx, resourceBinding)
	if err != nil {
		return ctrl.Result{}, err
	}
	// remove the overrider finalizer on the binding if all the work is deleted
	if len(works) == 0 {
		controllerutil.RemoveFinalizer(resourceBinding, fleetv1beta1.WorkFinalizer)
		if err = r.Client.Update(ctx, resourceBinding); err != nil {
			klog.ErrorS(err, "failed to remove the work finalizer from resource binding", "resource binding", klog.KObj(resourceBinding))
			return ctrl.Result{}, controller.NewAPIServerError(false, err)
		}
	}
	klog.V(4).InfoS("the resource binding still has undeleted work", "resource binding", klog.KObj(resourceBinding),
		"number of associated work", len(works))
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// ensureFinalizer makes sure that the resourceBinding CR has a finalizer on it
func (r *Reconciler) ensureFinalizer(ctx context.Context, resourceBinding client.Object) error {
	if controllerutil.ContainsFinalizer(resourceBinding, fleetv1beta1.WorkFinalizer) {
		return nil
	}
	klog.InfoS("add the work finalizer", "resource resourceBinding", klog.KObj(resourceBinding))
	controllerutil.AddFinalizer(resourceBinding, fleetv1beta1.WorkFinalizer)
	if err := r.Client.Update(ctx, resourceBinding); err != nil {
		klog.ErrorS(err, "failed to add the work finalizer to resourceBinding", "resourceBinding", klog.KObj(resourceBinding))
		return controller.NewAPIServerError(false, err)
	}
	return nil
}

// listAllWorksAssociated finds all the works that are associated with this binding.
func (r *Reconciler) listAllWorksAssociated(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) (map[string]*workv1alpha1.Work, error) {
	nameSpaceMatcher := client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.Spec.TargetCluster))
	parentBindingLabelMatcher := client.MatchingLabels{
		fleetv1beta1.ParentBindingLabel: resourceBinding.Name,
	}
	existingWork := make(map[string]*workv1alpha1.Work)
	workList := &workv1alpha1.WorkList{}
	if err := r.Client.List(ctx, workList, parentBindingLabelMatcher, nameSpaceMatcher); err != nil {
		klog.ErrorS(err, "failed to list all the work associated with the resourceBinding", "resourceBinding", klog.KObj(resourceBinding))
		return nil, controller.NewAPIServerError(true, err)
	}
	for _, work := range workList.Items {
		existingWork[work.Name] = work.DeepCopy()
	}
	return existingWork, nil
}

// syncAllWork generates all the work for the resourceBinding and apply them to the
func (r *Reconciler) syncAllWork(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) (ctrl.Result, error) {
	// Gather all the resource resourceSnapshots
	resourceSnapshots, err := r.fetchAllResourceSnapshots(ctx, resourceBinding)
	if err != nil {
		return ctrl.Result{}, err
	}
	// list all the corresponding works
	works, err := r.listAllWorksAssociated(ctx, resourceBinding)
	if err != nil {
		return ctrl.Result{}, err
	}

	// create/update the corresponding work for each snapshot
	activeWork := make(map[string]bool, len(resourceSnapshots))
	for _, snapshot := range resourceSnapshots {
		activeWork[snapshot.Name] = true
		// TODO(RZ): issue those requests in parallel to speed up the process
		err = r.upsertWork(ctx, works[snapshot.Name], snapshot, resourceBinding)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	//  delete the works that are not associated with any resource snapshot
	for _, work := range works {
		if activeWork[work.Name] {
			continue
		}
		klog.V(2).InfoS("delete the work that is not associated with any resource snapshot", "work", klog.KObj(work))
		if err := r.Client.Delete(ctx, work); err != nil {
			klog.ErrorS(err, "failed to delete the no longer needed work", "work", klog.KObj(work))
			return ctrl.Result{}, controller.NewAPIServerError(false, err)
		}
	}

	klog.V(2).InfoS("successfully synced all the work associated with the resourceBinding", "resourceBinding", klog.KObj(resourceBinding))
	return ctrl.Result{}, nil
}

// fetchAllResourceSnapshots gathers all the resource snapshots for the resource binding
func (r *Reconciler) fetchAllResourceSnapshots(ctx context.Context, resourceBinding *fleetv1beta1.ClusterResourceBinding) ([]*fleetv1beta1.ClusterResourceSnapshot, error) {
	resourceSnapshots := make([]*fleetv1beta1.ClusterResourceSnapshot, 1)
	resourceSnapshot := fleetv1beta1.ClusterResourceSnapshot{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: resourceBinding.Spec.ResourceSnapshotName}, &resourceSnapshot); err != nil {
		klog.ErrorS(err, "failed to get the resource snapshot from resource resourceBinding",
			"resource resourceBinding", klog.KObj(resourceBinding), "resource snapshot name", resourceBinding.Spec.ResourceSnapshotName)
		return nil, controller.NewAPIServerError(true, err)
	}
	resourceSnapshots[0] = &resourceSnapshot

	// check if there are more snapshot in the same index group
	countAnnotation := resourceSnapshot.Annotations[fleetv1beta1.NumberOfResourceSnapshotsAnnotation]
	snapshotCount, err := strconv.Atoi(countAnnotation)
	if err != nil {
		return nil, controller.NewUnexpectedBehaviorError(err)
	}
	if snapshotCount > 1 {
		index, exist := resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel]
		if !exist {
			unexpectedErr := fmt.Errorf("resource snapshot %s has no index label", resourceSnapshot.Name)
			return nil, controller.NewUnexpectedBehaviorError(unexpectedErr)
		}
		resourceIndexLabelMatcher := client.MatchingLabels{
			fleetv1beta1.ResourceIndexLabel: index,
		}
		resourceSnapshotList := &fleetv1beta1.ClusterResourceSnapshotList{}
		if err := r.Client.List(ctx, resourceSnapshotList, resourceIndexLabelMatcher); err != nil {
			klog.ErrorS(err, "failed to list all the resource snapshot associated with the resourceBinding", "resourceBinding", klog.KObj(resourceBinding))
			return nil, controller.NewAPIServerError(true, err)
		}
		for i := 0; i < snapshotCount; i++ {
			resourceSnapshots = append(resourceSnapshots, &resourceSnapshotList.Items[i])
		}
	}
	return resourceSnapshots, nil
}

// upsertWork creates or updates the work for the corresponding resource snapshot
func (r *Reconciler) upsertWork(ctx context.Context, work *workv1alpha1.Work, resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot, resourceBinding *fleetv1beta1.ClusterResourceBinding) error {
	needCreate := false
	if work == nil {
		needCreate = true
		work = &workv1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceSnapshot.Name, // work name same as the corresponding resource snapshot name
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
	}
	workObj := klog.KObj(work)
	resourceBindingObj := klog.KObj(resourceBinding)
	// reset the manifest list regardless
	work.Spec.Workload.Manifests = make([]workv1alpha1.Manifest, 0)
	for _, selectedResource := range resourceSnapshot.Spec.SelectedResources {
		work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, workv1alpha1.Manifest{
			RawExtension: selectedResource.RawExtension,
		})
	}

	// upsert the work
	if needCreate {
		if err := r.Client.Create(ctx, work); err != nil {
			klog.ErrorS(err, "failed to create the work associated with the resourceBinding", "resourceBinding", resourceBindingObj,
				"work", workObj)
			return controller.NewAPIServerError(false, err)
		}
	} else {
		if err := r.Client.Update(ctx, work); err != nil {
			klog.ErrorS(err, "failed to update the work associated with the resourceBinding", "resourceBinding", resourceBindingObj,
				"work", workObj)
			return controller.NewAPIServerError(false, err)
		}
	}

	klog.V(4).InfoS("upsert the work associated with the resourceBinding", "create", needCreate, "resourceBinding", resourceBindingObj,
		"work", workObj)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
// It watches binding events and also delete events for work.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("work generator")
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1beta1.ClusterResourceBinding{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&source.Kind{Type: &workv1alpha1.Work{}}, &handler.Funcs{
			// we only care about work delete
			DeleteFunc: func(evt event.DeleteEvent, queue workqueue.RateLimitingInterface) {
				if evt.Object == nil {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("DeleteEvent %v received with no matadata", evt)),
						"failed to process a delete event for work object")
					return
				}
				parentBindingName, exist := evt.Object.GetLabels()[fleetv1beta1.ParentBindingLabel]
				if !exist {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("deleted work has no binding parent")),
						"Could not the parent binding label", "deleted work", evt.Object, "existing label", evt.Object.GetLabels())
					return
				}
				// Make sure the work is not deleted behind our back
				queue.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name: parentBindingName,
				}})
			},
		}).
		Complete(r)
}
