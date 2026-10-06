/*
Copyright 2026 The KubeFleet Authors.

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
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrloption "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	parallelizerutil "github.com/kubefleet-dev/kubefleet/pkg/utils/parallelizer"
)

const (
	controllerName   = "work-applier"
	fieldManagerName = "kubefleet-member-agent"

	workApplierCleanupFinalizer = "placement.kubefleet.dev/work-cleanup"

	appliedWorkForcedDeletedAnnotationKey = "placement.kubefleet.dev/applied-work-forced-deleted"
)

type ApplyResultType string

const (
	// The result types for apply op failures.
	ApplyResTypeDecodingErred                   ApplyResultType = "DecodingErred"
	ApplyResTypeFoundGenerateName               ApplyResultType = "FoundGenerateName"
	ApplyResTypeDuplicated                      ApplyResultType = "Duplicated"
	ApplyResTypeFailedToFindObjInMemberCluster  ApplyResultType = "FailedToFindObjInMemberCluster"
	ApplyResTypeOwnedByOtherKubeFleetAPIObjects ApplyResultType = "OwnedByOtherKubeFleetAPIObjects"
	ApplyResTypeFailedToTakeOver                ApplyResultType = "FailedToTakeOver"
	ApplyResTypeNotTakenOver                    ApplyResultType = "NotTakenOver"
	ApplyResTypeFailedToRunDriftDetection       ApplyResultType = "FailedToRunDriftDetection"
	ApplyResTypeFoundDrifts                     ApplyResultType = "FoundDrifts"
	ApplyResTypeFoundDriftsInDegradedMode       ApplyResultType = "FoundDriftsInDegradedMode"
	ApplyResTypeFailedToApply                   ApplyResultType = "FailedToApply"

	// The result type and description for successful apply ops.
	ApplyResTypeApplied ApplyResultType = "Applied"
	// The apply op has succeeded, but KubeFleet cannot tell whether the object has drifted.
	ApplyResTypeAppliedWithFailedDriftDetection ApplyResultType = "AppliedWithFailedDriftDetection"
)

var allApplyResTypes = sets.New(
	ApplyResTypeDecodingErred,
	ApplyResTypeFoundGenerateName,
	ApplyResTypeDuplicated,
	ApplyResTypeFailedToFindObjInMemberCluster,
	ApplyResTypeOwnedByOtherKubeFleetAPIObjects,
	ApplyResTypeFailedToTakeOver,
	ApplyResTypeNotTakenOver,
	ApplyResTypeFailedToRunDriftDetection,
	ApplyResTypeFoundDrifts,
	ApplyResTypeFoundDriftsInDegradedMode,
	ApplyResTypeFailedToApply,
	ApplyResTypeApplied,
	ApplyResTypeAppliedWithFailedDriftDetection,
)

// The messages reported on the Applied condition of a manifest.
const (
	ApplyResTypeAppliedDescription                         = "The manifest has been applied successfully"
	ApplyResTypeAppliedWithFailedDriftDetectionDescription = "The manifest has been applied successfully, but KubeFleet cannot determine whether the object has drifted"
	ApplyResTypeFailedToApplyDescription                   = "Failed to apply the manifest (error: %s)"
)

type AvailabilityCheckResultType string

const (
	// The result type for availability check being skipped.
	AvailabilityResultTypeSkipped AvailabilityCheckResultType = "Skipped"

	// The result type for availability check failures.
	AvailabilityResultTypeFailed AvailabilityCheckResultType = "FailedToCheck"

	// The result types for completed availability checks.
	AvailabilityResultTypeAvailable       AvailabilityCheckResultType = "Available"
	AvailabilityResultTypeNotYetAvailable AvailabilityCheckResultType = "Unavailable"
	AvailabilityResultTypeNotTrackable    AvailabilityCheckResultType = "NotTrackable"
)

// The messages reported on the Available condition of a manifest.
const (
	AvailabilityResultTypeAvailableDescription       = "The manifest is available"
	AvailabilityResultTypeNotYetAvailableDescription = "The manifest is not yet available; KubeFleet will check again later"
	AvailabilityResultTypeNotTrackableDescription    = "The manifest's availability is not trackable; KubeFleet assumes that the applied manifest is available"
	AvailabilityResultTypeFailedDescription          = "Failed to track the availability of the applied manifest (error: %s)"
)

type manifestProcessingState struct {
	// The manifest data in its raw form (not yet decoded).
	manifest *placementv1alpha1.Manifest

	// The manifest identifier.
	//
	// If the manifest data cannot be decoded as a Kubernetes API object at all, the identifier
	// will feature only the ordinal of the manifest data (its rank in the list of the resources).
	// If the manifest data can be decoded as a Kubernetes API object, but the API is not available
	// on the member cluster, the resource field of the identifier will be empty.
	id *placementv1alpha1.ManifestIdentifier
	// A string representation of the resource identifier (sans the resources field).
	// This is only populated if the manifest data can be successfully decoded.
	//
	// It is of the format `GV=[API_GROUP]/[VERSION], Kind=[KIND], Namespace=[NAMESPACE], Name=[NAME]`,
	// where [API_GROUP], [VERSION], [KIND], [NAMESPACE], and [NAME] are the API group/version/kind of the
	// manifest object, and its owner namespace (if applicable)/name, respectively.
	idStr string

	// The manifest data, decoded as a Kubernetes API object.
	manifestObj *unstructured.Unstructured

	// The object in the member cluster that corresponds to the manifest object.
	inMemberClusterObj *unstructured.Unstructured

	// The GVR of the manifest object.
	gvr *schema.GroupVersionResource

	// The result of the apply operation.
	applyRes ApplyResultType
	// The result of the availability check operation.
	availabilityCheckRes AvailabilityCheckResultType
	// The error that occurred during the apply operation, if any.
	applyErr error
	// The error that occurred during the availability check operation, if any.
	availabilityCheckErr error
	// The diffs detected in the apply operation.
	diffs []placementv1alpha1.PatchDetail
	// A link back to the work object that includes the manifest.
	fromWorkObj *placementv1alpha1.Work
	// A link back to the primary work object.
	fromPrimaryWorkObject *placementv1alpha1.Work
	// The expected owner reference for the manifest object.
	ownedBy *metav1.OwnerReference
}

type workObjectProcessingState struct {
	// The work object being processed.
	work *placementv1alpha1.Work

	// The corresponding appliedWork object for the work object.
	appliedWork *placementv1alpha1.AppliedWork
	// The owner reference to use for all the manifests within the work object when they are applied.
	appliedWorkOwnerRef *metav1.OwnerReference

	// The processing state of each manifest within the work object.
	manifestProcessingStates []*manifestProcessingState
}

type Reconciler struct {
	hubClient          client.Client
	hubUncachedReader  client.Reader
	spokeClient        client.Client
	spokeDynamicClient dynamic.Interface

	workNSName string

	restMapper meta.RESTMapper

	parallelizer parallelizerutil.Parallelizer

	concurrentReconciles int
	cleanupRequeueAfter  time.Duration
	periodicRequeueAfter time.Duration
	cleanupWaitTime      time.Duration

	ready atomic.Bool
}

func New(workNSName string,
	hubClient client.Client, hubUncachedReader client.Reader,
	spokeClient client.Client, spokeDynamicClient dynamic.Interface,
	restMapper meta.RESTMapper,
	parallelizer parallelizerutil.Parallelizer,
	concurrentReconciles int,
	cleanupRequeueAfter time.Duration,
	periodicRequeueAfter time.Duration,
	cleanupWaitTime time.Duration,
) *Reconciler {
	return &Reconciler{
		workNSName:           workNSName,
		hubClient:            hubClient,
		hubUncachedReader:    hubUncachedReader,
		spokeDynamicClient:   spokeDynamicClient,
		spokeClient:          spokeClient,
		restMapper:           restMapper,
		parallelizer:         parallelizer,
		concurrentReconciles: concurrentReconciles,
		cleanupRequeueAfter:  cleanupRequeueAfter,
		periodicRequeueAfter: periodicRequeueAfter,
		cleanupWaitTime:      cleanupWaitTime,
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.ready.Load() {
		klog.V(2).InfoS("Work applier is not yet ready to start; the member agent might still be connecting to the hub cluster",
			"work", req.NamespacedName)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "work", req.NamespacedName, "controller", controllerName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "work", req.NamespacedName, "latency", latency, "controller", controllerName)
	}()

	// Retrieve the work object.
	work := &placementv1alpha1.Work{}
	err := r.hubClient.Get(ctx, req.NamespacedName, work)
	switch {
	case apierrors.IsNotFound(err):
		klog.V(2).InfoS("The work object cannot be found", "work", req.NamespacedName, "controller", controllerName)
		return ctrl.Result{}, nil
	case err != nil:
		wrappedErr := errors.NewAPIServerError(err, "", true, "work", req.NamespacedName, "controller", controllerName)
		klog.ErrorS(err, "Failed to retrieve the work", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Check if the work object is a primary one, i.e., it has the count annotation set.
	_, found := work.GetAnnotations()[placementv1alpha1.LinkedWorkCountAnnotationKey]
	if !found {
		klog.V(2).InfoS("The work object is not a primary one; skipping reconciliation", "work", req.NamespacedName, "controller", controllerName)
		return ctrl.Result{}, nil
	}

	// Clean things up if the work object has been marked for deletion.
	if !work.DeletionTimestamp.IsZero() {
		requeueAfter, err := r.cleanupWhenBindingDeleted(ctx, work)
		if err != nil {
			wrappedErr := errors.Wraps(err, "", "primaryWork", klog.KObj(work), "controller", controllerName)
			klog.ErrorS(err, "Failed to clean up linked work objects", errors.Args(wrappedErr)...)
			return ctrl.Result{}, wrappedErr
		}
		if requeueAfter != nil {
			return ctrl.Result{RequeueAfter: *requeueAfter}, nil
		}
		return ctrl.Result{}, nil
	}

	// Retrieve all linked work objects.
	linkedWorks, leftOverWorks, err := r.retrieveLinkedAndLeftOverWorks(ctx, work)
	if err != nil {
		wrappedErr := errors.Wraps(err, "", "primaryWork", klog.KObj(work), "controller", controllerName)
		klog.ErrorS(err, "Failed to retrieve linked work objects", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Add cleanup finalizer to all linked work objects.
	if err := r.addCleanupFinalizerTo(ctx, linkedWorks); err != nil {
		wrappedErr := errors.Wraps(err, "", "primaryWork", klog.KObj(work), "controller", controllerName)
		klog.ErrorS(err, "Failed to add cleanup finalizer to linked work objects", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Ensure an appliedWork object for each work object.
	//
	// The appliedWork object that corresponds to the primary work object will be set as the owner for all
	// applied manifests across the linked work objects.
	//
	// Note that the appliedWork objects are returned in the same order as their corresponding work objects in the
	// input.
	appliedWorks, err := r.ensureAppliedWorks(ctx, linkedWorks)
	if err != nil {
		wrappedErr := errors.Wraps(err, "", "primaryWork", klog.KObj(work), "controller", controllerName)
		klog.ErrorS(err, "Failed to ensure appliedWork objects for linked work objects", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Prepare the processing states for the work objects and their manifests.
	workObjProcessingStates, manifestProcessingStates := prepareWorkObjectAndManifestProcessingStates(linkedWorks, appliedWorks)

	// Pre-process the manifests to apply.
	//
	// In this step, the work applier will:
	// a) decode the manifests; and
	// b) write ahead the manifest processing attempts; and
	// c) remove any applied manifests left over from previous runs.
	seenManifestIDs, err := r.preProcessWorkObjects(ctx, workObjProcessingStates)
	if err != nil {
		wrappedErr := errors.Wraps(err, "", "primaryWork", klog.KObj(work), "controller", controllerName)
		klog.ErrorS(err, "Failed to pre-process work objects", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Before processing the manifests in the linked work objects, handle the left-over work objects: check if
	// there is any applied manifest on them that needs to be cleaned up, and drop the cleanup finalizer
	// from these work objects.
	if err := r.cleanupLeftOverWorks(ctx, leftOverWorks, seenManifestIDs, workObjProcessingStates[0].appliedWorkOwnerRef); err != nil {
		wrappedErr := errors.Wraps(err, "", "primaryWork", klog.KObj(work), "controller", controllerName)
		klog.ErrorS(err, "Failed to clean up left-over work objects", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Set default values in the work object spec to avoid additional validation logic in the later steps.
	setDefaultSyncStrategy(work)

	// Process the manifests.
	//
	// In this step, the work applier will:
	// a) find if there has been a corresponding object in the member cluster for each manifest;
	// b) take over the object if applicable;
	// c) check for diffs/drifts if applicable;
	// e) apply each manifest.
	if err := r.processManifests(ctx, manifestProcessingStates); err != nil {
		wrappedErr := errors.Wraps(err, "", "primaryWork", klog.KObj(work), "controller", controllerName)
		klog.ErrorS(err, "Failed to process manifests", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Track the availability information.
	if err := r.trackInMemberClusterObjAvailability(ctx, manifestProcessingStates); err != nil {
		wrappedErr := errors.Wraps(err, "", "primaryWork", klog.KObj(work), "controller", controllerName)
		klog.ErrorS(err, "Failed to check for object availability", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Refresh the status of the Work object.
	if err := r.refreshWorkStatus(ctx, workObjProcessingStates); err != nil {
		wrappedErr := errors.Wraps(err, "", "primaryWork", klog.KObj(work), "controller", controllerName)
		klog.ErrorS(err, "Failed to refresh work object status", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Refresh the status of the AppliedWork object.
	if err := r.refreshAppliedWorkStatus(ctx, workObjProcessingStates); err != nil {
		wrappedErr := errors.Wraps(err, "", "primaryWork", klog.KObj(work), "controller", controllerName)
		klog.ErrorS(err, "Failed to refresh appliedWork object status", errors.Args(wrappedErr)...)
		return ctrl.Result{}, wrappedErr
	}

	// Periodically requeue the work object to check and (re-)apply manifests.
	return ctrl.Result{RequeueAfter: r.periodicRequeueAfter}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(ctrloption.Options{
			MaxConcurrentReconciles: r.concurrentReconciles,
		}).
		For(&placementv1alpha1.Work{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
