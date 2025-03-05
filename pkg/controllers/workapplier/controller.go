/*
Copyright 2021 The Kubernetes Authors.

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

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"time"

	"go.uber.org/atomic"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrloption "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/work"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/defaulter"
	"go.goms.io/fleet/pkg/utils/parallelizer"
)

const (
	patchDetailPerObjLimit = 100

	minRequestAfterDuration = time.Second * 5
)

const (
	workFieldManagerName = "work-api-agent"
)

// Reconciler reconciles a Work object.
type Reconciler struct {
	hubClient            client.Client
	workNameSpace        string
	spokeDynamicClient   dynamic.Interface
	spokeClient          client.Client
	restMapper           meta.RESTMapper
	recorder             record.EventRecorder
	concurrentReconciles int
	joined               *atomic.Bool
	parallelizer         *parallelizer.Parallerlizer

	availabilityCheckRequeueAfter time.Duration
	driftCheckRequeueAfter        time.Duration
}

func NewReconciler(
	hubClient client.Client, workNameSpace string,
	spokeDynamicClient dynamic.Interface, spokeClient client.Client, restMapper meta.RESTMapper,
	recorder record.EventRecorder,
	concurrentReconciles int,
	workerCount int,
	availabilityCheckRequestAfter time.Duration,
	driftCheckRequestAfter time.Duration,
) *Reconciler {
	acRequestAfter := availabilityCheckRequestAfter
	if acRequestAfter < minRequestAfterDuration {
		klog.V(2).InfoS("Availability check requeue after duration is too short; set to the longer default", "availabilityCheckRequestAfter", acRequestAfter)
		acRequestAfter = minRequestAfterDuration
	}

	dcRequestAfter := driftCheckRequestAfter
	if dcRequestAfter < minRequestAfterDuration {
		klog.V(2).InfoS("Drift check requeue after duration is too short; set to the longer default", "driftCheckRequestAfter", dcRequestAfter)
		dcRequestAfter = minRequestAfterDuration
	}

	return &Reconciler{
		hubClient:                     hubClient,
		spokeDynamicClient:            spokeDynamicClient,
		spokeClient:                   spokeClient,
		restMapper:                    restMapper,
		recorder:                      recorder,
		concurrentReconciles:          concurrentReconciles,
		parallelizer:                  parallelizer.NewParallelizer(workerCount),
		workNameSpace:                 workNameSpace,
		joined:                        atomic.NewBool(false),
		availabilityCheckRequeueAfter: acRequestAfter,
		driftCheckRequeueAfter:        dcRequestAfter,
	}
}

var (
	// Some exported reasons for Work object conditions. Currently only the untrackable reason is being actively used.

	// This is a new reason for the Availability condition when the manifests are not
	// trackable for availability. This value is currently unused.
	//
	// TO-DO (chenyu1): switch to the new reason after proper rollout.
	WorkNotAllManifestsTrackableReasonNew = "SomeManifestsAreNotAvailabilityTrackable"
	// This reason uses the exact same value as the one kept in the old work applier for
	// compatibility reasons. It helps guard the case where the member agent is upgraded
	// before the hub agent.
	//
	// TO-DO (chenyu1): switch off the old reason after proper rollout.
	WorkNotAllManifestsTrackableReason    = work.WorkNotTrackableReason
	WorkAllManifestsAppliedReason         = "AllManifestsApplied"
	WorkAllManifestsAvailableReason       = "AllManifestsAvailable"
	WorkAllManifestsDiffReportedReason    = "AllManifestsDiffReported"
	WorkNotAllManifestsAppliedReason      = "SomeManifestsAreNotApplied"
	WorkNotAllManifestsAvailableReason    = "SomeManifestsAreNotAvailable"
	WorkNotAllManifestsDiffReportedReason = "SomeManifestsHaveNotReportedDiff"

	// Some condition messages for Work object conditions.
	allManifestsAppliedMessage           = "All the specified manifests have been applied"
	allManifestsHaveReportedDiffMessage  = "All the specified manifests have reported diff"
	allAppliedObjectAvailableMessage     = "All of the applied manifests are available"
	someAppliedObjectUntrackableMessage  = "Some of the applied manifests cannot be tracked for availability"
	notAllManifestsAppliedMessage        = "Failed to apply all manifests (%d of %d manifests are applied)"
	notAllAppliedObjectsAvailableMessage = "Some manifests are not available (%d of %d manifests are available)"
	notAllManifestsHaveReportedDiff      = "Failed to report diff on all manifests (%d of %d manifests have reported diff)"
)

type manifestProcessingAppliedResultType string

const (
	// The result types and descriptions for processing failures.
	ManifestProcessingApplyResultTypeDecodingErred                  manifestProcessingAppliedResultType = "DecodingErred"
	ManifestProcessingApplyResultTypeFoundGenerateNames             manifestProcessingAppliedResultType = "FoundGenerateNames"
	ManifestProcessingApplyResultTypeDuplicated                     manifestProcessingAppliedResultType = "Duplicated"
	ManifestProcessingApplyResultTypeFailedToFindObjInMemberCluster manifestProcessingAppliedResultType = "FailedToFindObjInMemberCluster"
	ManifestProcessingApplyResultTypeFailedToTakeOver               manifestProcessingAppliedResultType = "FailedToTakeOver"
	ManifestProcessingApplyResultTypeNotTakenOver                   manifestProcessingAppliedResultType = "NotTakenOver"
	ManifestProcessingApplyResultTypeFailedToRunDriftDetection      manifestProcessingAppliedResultType = "FailedToRunDriftDetection"
	ManifestProcessingApplyResultTypeFoundDrifts                    manifestProcessingAppliedResultType = "FoundDrifts"
	// Note that the reason string below uses the same value as kept in the old work applier.
	ManifestProcessingApplyResultTypeFailedToApply manifestProcessingAppliedResultType = "ManifestApplyFailed"

	// The result type and description for partially successfully processing attempts.
	ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection manifestProcessingAppliedResultType = "AppliedWithFailedDriftDetection"

	ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetectionDescription = "Manifest has been applied successfully, but drift detection has failed"

	// The result type and description for successful processing attempts.
	ManifestProcessingApplyResultTypeApplied manifestProcessingAppliedResultType = "Applied"

	ManifestProcessingApplyResultTypeAppliedDescription = "Manifest has been applied successfully"

	// A special result type for the case where no apply is performed (i.e., the ReportDiff mode).
	ManifestProcessingApplyResultTypeNoApplyPerformed manifestProcessingAppliedResultType = "Skipped"
)

type ManifestProcessingAvailabilityResultType string

const (
	// The result type for availability check being skipped.
	ManifestProcessingAvailabilityResultTypeSkipped ManifestProcessingAvailabilityResultType = "Skipped"

	// The result type for availability check failures.
	ManifestProcessingAvailabilityResultTypeFailed ManifestProcessingAvailabilityResultType = "Failed"

	// The description for availability check failures.
	ManifestProcessingAvailabilityResultTypeFailedDescription = "Failed to track the availability of the applied manifest (error = %s)"

	// The result types for completed availability checks.
	ManifestProcessingAvailabilityResultTypeAvailable ManifestProcessingAvailabilityResultType = "Available"
	// Note that the reason string below uses the same value as kept in the old work applier.
	ManifestProcessingAvailabilityResultTypeNotYetAvailable ManifestProcessingAvailabilityResultType = "ManifestNotAvailableYet"

	ManifestProcessingAvailabilityResultTypeNotTrackable ManifestProcessingAvailabilityResultType = "NotTrackable"

	// The descriptions for completed availability checks.
	ManifestProcessingAvailabilityResultTypeAvailableDescription       = "Manifest is available"
	ManifestProcessingAvailabilityResultTypeNotYetAvailableDescription = "Manifest is not yet available; Fleet will check again later"
	ManifestProcessingAvailabilityResultTypeNotTrackableDescription    = "Manifest's availability is not trackable; Fleet assumes that the applied manifest is available"
)

type ManifestProcessingReportDiffResultType string

const (
	// The result type for the cases where ReportDiff mode is not enabled.
	ManifestProcessingReportDiffResultTypeNotEnabled ManifestProcessingReportDiffResultType = "NotEnabled"

	// The result type for diff reporting failures.
	ManifestProcessingReportDiffResultTypeFailed ManifestProcessingReportDiffResultType = "Failed"

	ManifestProcessingReportDiffResultTypeFailedDescription = "Failed to report the diff between the hub cluster and the member cluster (error = %s)"

	// The result type for completed diff reportings.
	ManifestProcessingReportDiffResultTypeFoundDiff   ManifestProcessingReportDiffResultType = "FoundDiff"
	ManifestProcessingReportDiffResultTypeNoDiffFound ManifestProcessingReportDiffResultType = "NoDiffFound"

	ManifestProcessingReportDiffResultTypeNoDiffFoundDescription = "No diff has been found between the hub cluster and the member cluster"
	ManifestProcessingReportDiffResultTypeFoundDiffDescription   = "Diff has been found between the hub cluster and the member cluster"
)

type manifestProcessingBundle struct {
	manifest           *fleetv1beta1.Manifest
	id                 *fleetv1beta1.WorkResourceIdentifier
	manifestObj        *unstructured.Unstructured
	inMemberClusterObj *unstructured.Unstructured
	gvr                *schema.GroupVersionResource
	applyResTyp        manifestProcessingAppliedResultType
	availabilityResTyp ManifestProcessingAvailabilityResultType
	reportDiffResTyp   ManifestProcessingReportDiffResultType
	applyErr           error
	availabilityErr    error
	reportDiffErr      error
	drifts             []fleetv1beta1.PatchDetail
	diffs              []fleetv1beta1.PatchDetail
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
		BlockOwnerDeletion: ptr.To(false),
	}

	// Set the default values for the Work object to avoid additional validation logic in the
	// later steps.
	defaulter.SetDefaultsWork(work)

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

	trackWorkApplyLatencyMetric(work)

	// Refresh the status of the Work object.
	if err := r.refreshWorkStatus(ctx, work, bundles); err != nil {
		return ctrl.Result{}, err
	}

	// Refresh the status of the AppliedWork object.
	if err := r.refreshAppliedWorkStatus(ctx, appliedWork, bundles); err != nil {
		return ctrl.Result{}, err
	}

	// If the Work object is not yet available, reconcile again.
	if !isWorkObjectAvailable(work) {
		klog.V(2).InfoS("Work object is not yet in an available state; requeue to monitor its availability", "work", workRef)
		return ctrl.Result{RequeueAfter: r.availabilityCheckRequeueAfter}, nil
	}
	// Otherwise, reconcile again for drift detection purposes.
	klog.V(2).InfoS("Work object is available; requeue to check for drifts", "work", workRef)
	return ctrl.Result{RequeueAfter: r.driftCheckRequeueAfter}, nil
}

// garbageCollectAppliedWork deletes the appliedWork and all the manifests associated with it from the cluster.
func (r *Reconciler) garbageCollectAppliedWork(ctx context.Context, work *fleetv1beta1.Work) (ctrl.Result, error) {
	deletePolicy := metav1.DeletePropagationBackground
	if !controllerutil.ContainsFinalizer(work, fleetv1beta1.WorkFinalizer) {
		return ctrl.Result{}, nil
	}
	// delete the appliedWork which will remove all the manifests associated with it
	// TODO: allow orphaned manifest
	appliedWork := fleetv1beta1.AppliedWork{
		ObjectMeta: metav1.ObjectMeta{Name: work.Name},
	}
	err := r.spokeClient.Delete(ctx, &appliedWork, &client.DeleteOptions{PropagationPolicy: &deletePolicy})
	switch {
	case apierrors.IsNotFound(err):
		klog.V(2).InfoS("The appliedWork is already deleted", "appliedWork", work.Name)
	case err != nil:
		klog.ErrorS(err, "Failed to delete the appliedWork", "appliedWork", work.Name)
		return ctrl.Result{}, controller.NewAPIServerError(false, err)
	default:
		klog.InfoS("Successfully deleted the appliedWork", "appliedWork", work.Name)
	}
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
	return ctrl.NewControllerManagedBy(mgr).Named("work-applier-controller").
		WithOptions(ctrloption.Options{
			MaxConcurrentReconciles: r.concurrentReconciles,
		}).
		For(&fleetv1beta1.Work{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
