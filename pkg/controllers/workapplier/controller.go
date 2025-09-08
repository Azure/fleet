/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package workapplier

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"k8s.io/utils/set"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrloption "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/priorityqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/defaulter"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/parallelizer"
)

const (
	patchDetailPerObjLimit = 100
)

const (
	workFieldManagerName = "work-api-agent"
)

var (
	workAgeToReconcile = 1 * time.Hour
)

// Custom type to hold a reconcile.Request and a priority value
type priorityQueueItem struct {
	reconcile.Request
	Priority int
}

// PriorityQueueEventHandler is a custom event handler for adding objects to the priority queue.
type PriorityQueueEventHandler struct {
	Queue  priorityqueue.PriorityQueue[priorityQueueItem] // The priority queue to manage events
	Client client.Client                                  // store the client to make API calls
}

// Implement priorityqueue.Item interface for priorityQueueItem
func (i priorityQueueItem) GetPriority() int {
	return i.Priority
}

func (h *PriorityQueueEventHandler) WorkPendingApply(ctx context.Context, obj client.Object) bool {
	var work fleetv1beta1.Work
	ns := obj.GetNamespace()
	name := obj.GetName()
	err := h.Client.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      name,
	}, &work)
	if err != nil {
		// Log and return
		klog.ErrorS(err, "Failed to get the work", "name", name, "ns", ns)
		return true
	}
	availCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
	appliedCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)

	if availCond != nil && appliedCond != nil {
		// check if the object has been recently modified
		availCondLastUpdatedTime := availCond.LastTransitionTime.Time
		appliedCondLastUpdatedTime := appliedCond.LastTransitionTime.Time
		if time.Since(availCondLastUpdatedTime) < workAgeToReconcile || time.Since(appliedCondLastUpdatedTime) < workAgeToReconcile {
			return true
		}
	}

	if condition.IsConditionStatusTrue(availCond, work.GetGeneration()) &&
		condition.IsConditionStatusTrue(appliedCond, work.GetGeneration()) {
		return false
	}

	// Work not yet applied
	return true
}

func (h *PriorityQueueEventHandler) AddToPriorityQueue(ctx context.Context, obj client.Object, alwaysAdd bool) {
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		},
	}

	objAge := time.Since(obj.GetCreationTimestamp().Time)

	var objPriority int
	if alwaysAdd || objAge < workAgeToReconcile || h.WorkPendingApply(ctx, obj) {
		// Newer or pending objects get higher priority
		// Negate the Unix timestamp to give higher priority to newer timestamps
		objPriority = -int(time.Now().Unix())
	} else {
		// skip adding older objects with no changes
		klog.V(2).InfoS("adding old item to priorityQueueItem", "obj", req.Name, "age", objAge)
		objPriority = int(obj.GetCreationTimestamp().Unix())
	}

	// Create the custom priorityQueueItem with the request and priority
	item := priorityQueueItem{
		Request:  req,
		Priority: objPriority,
	}

	h.Queue.Add(item)
	klog.V(2).InfoS("Created PriorityQueueItem", "priority", objPriority, "obj", req.Name, "queue size", h.Queue.Len())
}

func (h *PriorityQueueEventHandler) Create(ctx context.Context, evt event.TypedCreateEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.AddToPriorityQueue(ctx, evt.Object, false)
}

func (h *PriorityQueueEventHandler) Delete(ctx context.Context, evt event.TypedDeleteEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.AddToPriorityQueue(ctx, evt.Object, true)
}

func (h *PriorityQueueEventHandler) Update(ctx context.Context, evt event.TypedUpdateEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Ignore updates where only the status changed
	oldObj := evt.ObjectOld.DeepCopyObject()
	newObj := evt.ObjectNew.DeepCopyObject()

	// Zero out the status
	if oldWork, ok := oldObj.(*fleetv1beta1.Work); ok {
		oldWork.Status = fleetv1beta1.WorkStatus{}
	}
	if newWork, ok := newObj.(*fleetv1beta1.Work); ok {
		newWork.Status = fleetv1beta1.WorkStatus{}
	}

	if !equality.Semantic.DeepEqual(oldObj, newObj) {
		// ignore status changes to prevent noise
		h.AddToPriorityQueue(ctx, evt.ObjectNew, true)
		return
	}
	klog.V(4).InfoS("ignoring update event with only status change", "work", evt.ObjectNew.GetName())
}

func (h *PriorityQueueEventHandler) Generic(ctx context.Context, evt event.TypedGenericEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.AddToPriorityQueue(ctx, evt.Object, false)
}

var defaultRequeueRateLimiter *RequeueMultiStageWithExponentialBackoffRateLimiter = NewRequeueMultiStageWithExponentialBackoffRateLimiter(
	// Allow 1 attempt of fixed delay; this helps give objects a bit of headroom to get available (or have
	// diffs reported).
	1,
	// Use a fixed delay of 5 seconds for the first two attempts.
	//
	// Important (chenyu1): before the introduction of the requeue rate limiter, the work
	// applier uses static requeue intervals, specifically 5 seconds (if the work object is unavailable),
	// and 15 seconds (if the work object is available). There are a number of test cases that
	// implicitly assume this behavior (e.g., a test case might expect that the availability check completes
	// w/in 10 seconds), which is why the rate limiter uses the 5 seconds fast requeue delay by default.
	// If you need to change this value and see that some test cases begin to fail, update the test
	// cases accordingly.
	5,
	// Then switch to slow exponential backoff with a base of 1.2 with an initial delay of 2 seconds
	// and a cap of 15 seconds (12 requeues in total, ~90 seconds in total).
	// This is to allow fast checkups in cases where objects are not yet available or have not yet reported diffs.
	1.2,
	2,
	15,
	// Eventually, switch to a fast exponential backoff with a base of 1.5 with an initial delay of 15 seconds
	// and a cap of 15 minutes (10 requeues in total, ~42 minutes in total).
	1.5,
	900,
	// Allow skipping to the fast exponential backoff stage if the Work object becomes available
	// or has reported diffs.
	true,
	// When the Work object spec does not change and the processing result remains the same (unavailable or failed
	// to report diffs), the requeue pattern is essentially:
	// * 1 attempt of requeue with fixed delays (5 seconds); then
	// * 12 attempts of requeues with slow exponential backoff (factor of 1.2, ~90 seconds in total); then
	// * 10 attempts of requeues with fast exponential backoff (factor of 1.5, ~42 minutes in total);
	// * afterwards, requeue with a delay of 15 minutes indefinitely.
)

// Reconciler reconciles a Work object.
type Reconciler struct {
	hubClient                    client.Client
	workNameSpace                string
	spokeDynamicClient           dynamic.Interface
	spokeClient                  client.Client
	restMapper                   meta.RESTMapper
	recorder                     record.EventRecorder
	concurrentReconciles         int
	watchWorkWithPriorityQueue   bool
	watchWorkReconcileAgeMinutes int
	deletionWaitTime             time.Duration
	joined                       *atomic.Bool
	parallelizer                 *parallelizer.Parallerlizer
	requeueRateLimiter           *RequeueMultiStageWithExponentialBackoffRateLimiter
}

// NewReconciler returns a new Work object reconciler for the work applier.
func NewReconciler(
	hubClient client.Client, workNameSpace string,
	spokeDynamicClient dynamic.Interface, spokeClient client.Client, restMapper meta.RESTMapper,
	recorder record.EventRecorder,
	concurrentReconciles int,
	workerCount int,
	deletionWaitTime time.Duration,
	watchWorkWithPriorityQueue bool,
	watchWorkReconcileAgeMinutes int,
	requeueRateLimiter *RequeueMultiStageWithExponentialBackoffRateLimiter,
) *Reconciler {
	if requeueRateLimiter == nil {
		requeueRateLimiter = defaultRequeueRateLimiter
	}

	return &Reconciler{
		hubClient:                    hubClient,
		spokeDynamicClient:           spokeDynamicClient,
		spokeClient:                  spokeClient,
		restMapper:                   restMapper,
		recorder:                     recorder,
		concurrentReconciles:         concurrentReconciles,
		parallelizer:                 parallelizer.NewParallelizer(workerCount),
		watchWorkWithPriorityQueue:   watchWorkWithPriorityQueue,
		watchWorkReconcileAgeMinutes: watchWorkReconcileAgeMinutes,
		workNameSpace:                workNameSpace,
		joined:                       atomic.NewBool(false),
		deletionWaitTime:             deletionWaitTime,
		requeueRateLimiter:           requeueRateLimiter,
	}
}

type ManifestProcessingApplyOrReportDiffResultType string

const (
	// The result types for apply op failures.
	ApplyOrReportDiffResTypeDecodingErred                  ManifestProcessingApplyOrReportDiffResultType = "DecodingErred"
	ApplyOrReportDiffResTypeFoundGenerateName              ManifestProcessingApplyOrReportDiffResultType = "FoundGenerateName"
	ApplyOrReportDiffResTypeDuplicated                     ManifestProcessingApplyOrReportDiffResultType = "Duplicated"
	ApplyOrReportDiffResTypeFailedToFindObjInMemberCluster ManifestProcessingApplyOrReportDiffResultType = "FailedToFindObjInMemberCluster"
	ApplyOrReportDiffResTypeFailedToTakeOver               ManifestProcessingApplyOrReportDiffResultType = "FailedToTakeOver"
	ApplyOrReportDiffResTypeNotTakenOver                   ManifestProcessingApplyOrReportDiffResultType = "NotTakenOver"
	ApplyOrReportDiffResTypeFailedToRunDriftDetection      ManifestProcessingApplyOrReportDiffResultType = "FailedToRunDriftDetection"
	ApplyOrReportDiffResTypeFoundDrifts                    ManifestProcessingApplyOrReportDiffResultType = "FoundDrifts"
	// Note that the reason string below uses the same value as kept in the old work applier.
	ApplyOrReportDiffResTypeFailedToApply ManifestProcessingApplyOrReportDiffResultType = "ManifestApplyFailed"

	// The result type and description for successful apply ops.
	ApplyOrReportDiffResTypeApplied ManifestProcessingApplyOrReportDiffResultType = "Applied"
)

const (
	// The descriptions for different apply op result types.

	// The description for partially successful apply ops.
	ApplyOrReportDiffResTypeAppliedWithFailedDriftDetection ManifestProcessingApplyOrReportDiffResultType = "AppliedWithFailedDriftDetection"
	// The description for successful apply ops.
	ApplyOrReportDiffResTypeAppliedDescription = "Manifest has been applied successfully"
)

const (
	// The result type for diff reporting failures.
	ApplyOrReportDiffResTypeFailedToReportDiff ManifestProcessingApplyOrReportDiffResultType = "FailedToReportDiff"

	// The result type for successful diff reportings.
	ApplyOrReportDiffResTypeFoundDiff   ManifestProcessingApplyOrReportDiffResultType = "FoundDiff"
	ApplyOrReportDiffResTypeNoDiffFound ManifestProcessingApplyOrReportDiffResultType = "NoDiffFound"
)

const (
	// The descriptions for different diff reporting result types.
	ApplyOrReportDiffResTypeFailedToReportDiffDescription = "Failed to report the diff between the hub cluster and the member cluster (error = %s)"
	ApplyOrReportDiffResTypeNoDiffFoundDescription        = "No diff has been found between the hub cluster and the member cluster"
	ApplyOrReportDiffResTypeFoundDiffDescription          = "Diff has been found between the hub cluster and the member cluster"
)

var (
	// A set for all apply related result types.
	manifestProcessingApplyResTypSet = set.New(
		ApplyOrReportDiffResTypeDecodingErred,
		ApplyOrReportDiffResTypeFoundGenerateName,
		ApplyOrReportDiffResTypeDuplicated,
		ApplyOrReportDiffResTypeFailedToFindObjInMemberCluster,
		ApplyOrReportDiffResTypeFailedToTakeOver,
		ApplyOrReportDiffResTypeNotTakenOver,
		ApplyOrReportDiffResTypeFailedToRunDriftDetection,
		ApplyOrReportDiffResTypeFoundDrifts,
		ApplyOrReportDiffResTypeFailedToApply,
		ApplyOrReportDiffResTypeAppliedWithFailedDriftDetection,
		ApplyOrReportDiffResTypeApplied,
	)
)

type ManifestProcessingAvailabilityResultType string

const (
	// The result type for availability check being skipped.
	AvailabilityResultTypeSkipped ManifestProcessingAvailabilityResultType = "Skipped"

	// The result type for availability check failures.
	AvailabilityResultTypeFailed ManifestProcessingAvailabilityResultType = "Failed"

	// The result types for completed availability checks.
	AvailabilityResultTypeAvailable ManifestProcessingAvailabilityResultType = "Available"
	// Note that the reason string below uses the same value as kept in the old work applier.
	AvailabilityResultTypeNotYetAvailable ManifestProcessingAvailabilityResultType = "ManifestNotAvailableYet"
	AvailabilityResultTypeNotTrackable    ManifestProcessingAvailabilityResultType = "NotTrackable"
)

const (
	// The description for availability check failures.
	AvailabilityResultTypeFailedDescription = "Failed to track the availability of the applied manifest (error = %s)"

	// The descriptions for completed availability checks.
	AvailabilityResultTypeAvailableDescription       = "Manifest is available"
	AvailabilityResultTypeNotYetAvailableDescription = "Manifest is not yet available; Fleet will check again later"
	AvailabilityResultTypeNotTrackableDescription    = "Manifest's availability is not trackable; Fleet assumes that the applied manifest is available"
)

type manifestProcessingBundle struct {
	// The manifest data in the raw form (not decoded yet).
	manifest *fleetv1beta1.Manifest
	// The work resource identifier of the manifest.
	// If the manifest data cannot be decoded as a Kubernetes API object at all, the identifier
	// will feature only the ordinal of the manifest data (its rank in the list of the resources).
	// If the manifest data can be decoded as a Kubernetes API object, but the API is not available
	// on the member cluster, the resource field of the identifier will be empty.
	id *fleetv1beta1.WorkResourceIdentifier
	// A string representation of the work resource identifier (sans the resources field).
	// This is only populated if the manifest data can be successfully decoded.
	//
	// It is of the format `GV=API_GROUP/VERSION, Kind=KIND, Namespace=NAMESPACE, Name=NAME`,
	// where API_GROUP, VERSION, KIND, NAMESPACE, and NAME are the API group/version/kind of the
	// manifest object, and its owner namespace (if applicable) and name, respectively.
	workResourceIdentifierStr string
	// The manifest data, decoded as a Kubernetes API object.
	manifestObj *unstructured.Unstructured
	// The object in the member cluster that corresponds to the manifest object.
	inMemberClusterObj *unstructured.Unstructured
	// The GVR of the manifest object.
	gvr *schema.GroupVersionResource
	// The result type of the apply op or the diff reporting op.
	applyOrReportDiffResTyp ManifestProcessingApplyOrReportDiffResultType
	// The result type of the availability check op.
	availabilityResTyp ManifestProcessingAvailabilityResultType
	// The error that stops the apply op or the diff reporting op.
	applyOrReportDiffErr error
	// The error that stops the availability check op.
	availabilityErr error
	// Configuration drifts/diffs detected during the apply op or the diff reporting op.
	drifts []fleetv1beta1.PatchDetail
	diffs  []fleetv1beta1.PatchDetail
}

// Reconcile implement the control loop logic for Work object.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.joined.Load() {
		klog.V(2).InfoS("Work applier has not started yet", "work", req.NamespacedName)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	startTime := time.Now()
	klog.V(2).InfoS("Work applier reconciliation starts", "work", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Work applier reconciliation ends", "work", req.NamespacedName, "latency", latency)
	}()

	// Retrieve the Work object.
	work := &fleetv1beta1.Work{}
	err := r.hubClient.Get(ctx, req.NamespacedName, work)
	switch {
	case apierrors.IsNotFound(err):
		klog.V(2).InfoS("Work object has been deleted", "work", req.NamespacedName)
		return ctrl.Result{}, nil
	case err != nil:
		klog.ErrorS(err, "Failed to retrieve the work", "work", req.NamespacedName)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}

	workRef := klog.KObj(work)

	// Garbage collect the AppliedWork object if the Work object has been deleted.
	if !work.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("Work object has been marked for deletion; start garbage collection", work.Kind, workRef)
		return r.garbageCollectAppliedWork(ctx, work)
	}

	// ensure that the appliedWork and the finalizer exist
	appliedWork, err := r.ensureAppliedWork(ctx, work)
	if err != nil {
		return ctrl.Result{}, err
	}
	expectedAppliedWorkOwnerRef := &metav1.OwnerReference{
		APIVersion:         fleetv1beta1.GroupVersion.String(),
		Kind:               fleetv1beta1.AppliedWorkKind,
		Name:               appliedWork.GetName(),
		UID:                appliedWork.GetUID(),
		BlockOwnerDeletion: ptr.To(true),
	}

	// Set the default values for the Work object to avoid additional validation logic in the
	// later steps.
	defaulter.SetDefaultsWork(work)

	age := time.Since(work.CreationTimestamp.Time)
	klog.V(4).InfoS("reconciling Work", "work", req.NamespacedName, "age", age)

	// Note (chenyu1): In the current version, for simplicity reasons, Fleet has dropped support
	// for objects with generate names; any attempt to place such objects will yield an apply error.
	// Originally this was supported, but a bug has stopped Fleet from handling such objects correctly.
	// The code has been updated to automatically ignore identifiers with empty names so that
	// reconciliation can resume in a previously erred setup.
	//
	// TO-DO (chenyu1): evaluate if it is necessary to add support for objects with generate
	// names.

	// Prepare the bundles.
	bundles := prepareManifestProcessingBundles(work)

	// Pre-process the manifests to apply.
	//
	// In this step, Fleet will:
	// a) decode the manifests; and
	// b) write ahead the manifest processing attempts; and
	// c) remove any applied manifests left over from previous runs.
	if err := r.preProcessManifests(ctx, bundles, work, expectedAppliedWorkOwnerRef); err != nil {
		klog.ErrorS(err, "Failed to pre-process the manifests", "work", workRef)
		return ctrl.Result{}, err
	}

	// Process the manifests.
	//
	// In this step, Fleet will:
	// a) find if there has been a corresponding object in the member cluster for each manifest;
	// b) take over the object if applicable;
	// c) report configuration differences if applicable;
	// d) check for configuration drifts if applicable;
	// e) apply each manifest.
	r.processManifests(ctx, bundles, work, expectedAppliedWorkOwnerRef)

	// Track the availability information.
	r.trackInMemberClusterObjAvailability(ctx, bundles, workRef)

	// Refresh the status of the Work object.
	if err := r.refreshWorkStatus(ctx, work, bundles); err != nil {
		return ctrl.Result{}, err
	}

	// Refresh the status of the AppliedWork object.
	if err := r.refreshAppliedWorkStatus(ctx, appliedWork, bundles); err != nil {
		return ctrl.Result{}, err
	}

	trackWorkAndManifestProcessingRequestMetrics(work)

	// Requeue the Work object with a delay based on the requeue rate limiter.
	requeueDelay := r.requeueRateLimiter.When(work, bundles)
	klog.V(2).InfoS("Requeue the Work object for re-processing", "work", workRef, "delaySeconds", requeueDelay.Seconds())
	return ctrl.Result{RequeueAfter: requeueDelay}, nil
}

func (r *Reconciler) garbageCollectAppliedWork(ctx context.Context, work *fleetv1beta1.Work) (ctrl.Result, error) {
	deletePolicy := metav1.DeletePropagationForeground
	if !controllerutil.ContainsFinalizer(work, fleetv1beta1.WorkFinalizer) {
		return ctrl.Result{}, nil
	}
	appliedWork := &fleetv1beta1.AppliedWork{
		ObjectMeta: metav1.ObjectMeta{Name: work.Name},
	}
	// Get the AppliedWork object
	if err := r.spokeClient.Get(ctx, types.NamespacedName{Name: work.Name}, appliedWork); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("The appliedWork is already deleted, removing the finalizer from the work", "appliedWork", work.Name)
			return r.forgetWorkAndRemoveFinalizer(ctx, work)
		}
		klog.ErrorS(err, "Failed to get AppliedWork", "appliedWork", work.Name)
		return ctrl.Result{}, controller.NewAPIServerError(false, err)
	}

	// Handle stuck deletion after 5 minutes where the other owner references might not exist or are invalid.
	if !appliedWork.DeletionTimestamp.IsZero() && time.Since(appliedWork.DeletionTimestamp.Time) >= r.deletionWaitTime {
		klog.V(2).InfoS("AppliedWork deletion appears stuck; attempting to patch owner references", "appliedWork", work.Name)
		if err := r.updateOwnerReference(ctx, work, appliedWork); err != nil {
			klog.ErrorS(err, "Failed to update owner references for AppliedWork", "appliedWork", work.Name)
			return ctrl.Result{}, controller.NewAPIServerError(false, err)
		}
		return ctrl.Result{}, fmt.Errorf("AppliedWork %s is being deleted, waiting for the deletion to complete", work.Name)
	}

	if err := r.spokeClient.Delete(ctx, appliedWork, &client.DeleteOptions{PropagationPolicy: &deletePolicy}); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("AppliedWork already deleted", "appliedWork", work.Name)
			return r.forgetWorkAndRemoveFinalizer(ctx, work)
		}
		klog.V(2).ErrorS(err, "Failed to delete the appliedWork", "appliedWork", work.Name)
		return ctrl.Result{}, controller.NewAPIServerError(false, err)
	}

	klog.V(2).InfoS("AppliedWork deletion in progress", "appliedWork", work.Name)
	return ctrl.Result{}, fmt.Errorf("AppliedWork %s is being deleted, waiting for the deletion to complete", work.Name)
}

// updateOwnerReference updates the AppliedWork owner reference in the manifest objects.
// It changes the blockOwnerDeletion field to false, so that the AppliedWork can be deleted in cases where
// the other owner references do not exist or are invalid.
// https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/#owner-references-in-object-specifications
func (r *Reconciler) updateOwnerReference(ctx context.Context, work *fleetv1beta1.Work, appliedWork *fleetv1beta1.AppliedWork) error {
	appliedWorkOwnerRef := &metav1.OwnerReference{
		APIVersion: fleetv1beta1.GroupVersion.String(),
		Kind:       "AppliedWork",
		Name:       appliedWork.Name,
		UID:        appliedWork.UID,
	}

	if err := r.hubClient.Get(ctx, types.NamespacedName{Name: work.Name, Namespace: work.Namespace}, work); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("Work object not found, skipping owner reference update", "work", work.Name, "namespace", work.Namespace)
			return nil
		}
		klog.ErrorS(err, "Failed to get Work object for owner reference update", "work", work.Name, "namespace", work.Namespace)
		return controller.NewAPIServerError(false, err)
	}

	for _, cond := range work.Status.ManifestConditions {
		res := cond.Identifier
		gvr := schema.GroupVersionResource{
			Group:    res.Group,
			Version:  res.Version,
			Resource: res.Resource,
		}

		var obj *unstructured.Unstructured
		var err error
		if obj, err = r.spokeDynamicClient.Resource(gvr).Namespace(res.Namespace).Get(ctx, res.Name, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			klog.ErrorS(err, "Failed to get manifest", "gvr", gvr, "name", res.Name, "namespace", res.Namespace)
			return err
		}
		// Check if there is more than one owner reference. If there is only one owner reference, it is the appliedWork itself.
		// Otherwise, at least one other owner reference exists, and we need to leave resource alone.
		if len(obj.GetOwnerReferences()) > 1 {
			ownerRefs := obj.GetOwnerReferences()
			updated := false
			for idx := range ownerRefs {
				if areOwnerRefsEqual(&ownerRefs[idx], appliedWorkOwnerRef) {
					ownerRefs[idx].BlockOwnerDeletion = ptr.To(false)
					updated = true
				}
			}
			if updated {
				obj.SetOwnerReferences(ownerRefs)
				if _, err = r.spokeDynamicClient.Resource(gvr).Namespace(obj.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
					klog.ErrorS(err, "Failed to update manifest owner references", "gvr", gvr, "name", res.Name, "namespace", res.Namespace)
					return err
				}
				klog.V(4).InfoS("Patched manifest owner references", "gvr", gvr, "name", res.Name, "namespace", res.Namespace)
			}
		}
	}
	return nil
}

// forgetWorkAndRemoveFinalizer untracks the Work object in the requeue rate limiter and removes
// the finalizer from the Work object.
func (r *Reconciler) forgetWorkAndRemoveFinalizer(ctx context.Context, work *fleetv1beta1.Work) (ctrl.Result, error) {
	r.requeueRateLimiter.Forget(work)

	controllerutil.RemoveFinalizer(work, fleetv1beta1.WorkFinalizer)
	if err := r.hubClient.Update(ctx, work, &client.UpdateOptions{}); err != nil {
		klog.ErrorS(err, "Failed to remove the finalizer from the work", "work", klog.KObj(work))
		return ctrl.Result{}, controller.NewAPIServerError(false, err)
	}
	return ctrl.Result{}, nil
}

// ensureAppliedWork makes sure that an associated appliedWork and a finalizer on the work resource exists on the cluster.
func (r *Reconciler) ensureAppliedWork(ctx context.Context, work *fleetv1beta1.Work) (*fleetv1beta1.AppliedWork, error) {
	workRef := klog.KObj(work)
	appliedWork := &fleetv1beta1.AppliedWork{}
	hasFinalizer := false
	if controllerutil.ContainsFinalizer(work, fleetv1beta1.WorkFinalizer) {
		hasFinalizer = true
		err := r.spokeClient.Get(ctx, types.NamespacedName{Name: work.Name}, appliedWork)
		switch {
		case apierrors.IsNotFound(err):
			klog.ErrorS(err, "AppliedWork finalizer resource does not exist even with the finalizer, it will be recreated", "appliedWork", workRef.Name)
		case err != nil:
			klog.ErrorS(err, "Failed to retrieve the appliedWork ", "appliedWork", workRef.Name)
			return nil, controller.NewAPIServerError(true, err)
		default:
			return appliedWork, nil
		}
	}

	// we create the appliedWork before setting the finalizer, so it should always exist unless it's deleted behind our back
	appliedWork = &fleetv1beta1.AppliedWork{
		ObjectMeta: metav1.ObjectMeta{
			Name: work.Name,
		},
		Spec: fleetv1beta1.AppliedWorkSpec{
			WorkName:      work.Name,
			WorkNamespace: work.Namespace,
		},
	}
	if err := r.spokeClient.Create(ctx, appliedWork); err != nil && !apierrors.IsAlreadyExists(err) {
		klog.ErrorS(err, "AppliedWork create failed", "appliedWork", workRef.Name)
		return nil, controller.NewAPIServerError(false, err)
	}
	if !hasFinalizer {
		klog.InfoS("Add the finalizer to the work", "work", workRef)
		work.Finalizers = append(work.Finalizers, fleetv1beta1.WorkFinalizer)

		if err := r.hubClient.Update(ctx, work, &client.UpdateOptions{}); err != nil {
			klog.ErrorS(err, "Failed to add the finalizer to the work", "work", workRef)
			return nil, controller.NewAPIServerError(false, err)
		}
	}
	klog.InfoS("Recreated the appliedWork resource", "appliedWork", workRef.Name)
	return appliedWork, nil
}

// prepareManifestProcessingBundles prepares the manifest processing bundles.
func prepareManifestProcessingBundles(work *fleetv1beta1.Work) []*manifestProcessingBundle {
	// Pre-allocate the bundles.
	bundles := make([]*manifestProcessingBundle, 0, len(work.Spec.Workload.Manifests))
	for idx := range work.Spec.Workload.Manifests {
		manifest := work.Spec.Workload.Manifests[idx]
		bundles = append(bundles, &manifestProcessingBundle{
			manifest: &manifest,
		})
	}
	return bundles
}

// Join starts to reconcile
func (r *Reconciler) Join(_ context.Context) error {
	if !r.joined.Load() {
		klog.InfoS("Mark the apply work reconciler joined")
	}
	r.joined.Store(true)
	return nil
}

// Leave start
func (r *Reconciler) Leave(ctx context.Context) error {
	var works fleetv1beta1.WorkList
	if r.joined.Load() {
		klog.InfoS("Mark the apply work reconciler left")
	}
	r.joined.Store(false)
	// list all the work object we created in the member cluster namespace
	listOpts := []client.ListOption{
		client.InNamespace(r.workNameSpace),
	}
	if err := r.hubClient.List(ctx, &works, listOpts...); err != nil {
		klog.ErrorS(err, "Failed to list all the work object", "clusterNS", r.workNameSpace)
		return client.IgnoreNotFound(err)
	}
	// we leave the resources on the member cluster for now
	for _, work := range works.Items {
		staleWork := work.DeepCopy()
		if controllerutil.ContainsFinalizer(staleWork, fleetv1beta1.WorkFinalizer) {
			controllerutil.RemoveFinalizer(staleWork, fleetv1beta1.WorkFinalizer)
			if updateErr := r.hubClient.Update(ctx, staleWork, &client.UpdateOptions{}); updateErr != nil {
				klog.ErrorS(updateErr, "Failed to remove the work finalizer from the work",
					"clusterNS", r.workNameSpace, "work", klog.KObj(staleWork))
				return updateErr
			}
		}
	}
	klog.V(2).InfoS("Successfully removed all the work finalizers in the cluster namespace",
		"clusterNS", r.workNameSpace, "number of work", len(works.Items))
	return nil
}

// SetupWithManager wires up the controller.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create the priority queue using the rate limiter and a queue name
	queue := priorityqueue.New[priorityQueueItem]("apply-work-queue")

	// Create the event handler that uses the priority queue
	eventHandler := &PriorityQueueEventHandler{
		Queue:  queue, // Attach the priority queue to the event handler
		Client: r.hubClient,
	}

	if r.watchWorkWithPriorityQueue {
		workAgeToReconcile = time.Duration(r.watchWorkReconcileAgeMinutes) * time.Minute
		return ctrl.NewControllerManagedBy(mgr).Named("work-applier-controller").
			WithOptions(ctrloption.Options{
				MaxConcurrentReconciles: r.concurrentReconciles,
			}).
			For(&fleetv1beta1.Work{}).
			Watches(&fleetv1beta1.Work{}, eventHandler).
			Complete(r)
	}

	return ctrl.NewControllerManagedBy(mgr).Named("work-applier-controller").
		WithOptions(ctrloption.Options{
			MaxConcurrentReconciles: r.concurrentReconciles,
		}).
		For(&fleetv1beta1.Work{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
