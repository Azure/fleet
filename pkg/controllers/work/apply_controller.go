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

package work

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/atomic"
	appv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
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
	"go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/resource"
)

const (
	workFieldManagerName = "work-api-agent"
)

// WorkCondition condition reasons
const (
	workAppliedFailedReason       = "WorkAppliedFailed"
	workAppliedCompleteReason     = "WorkAppliedComplete"
	workNotAvailableYetReason     = "WorkNotAvailableYet"
	workAvailabilityUnknownReason = "WorkAvailabilityUnknown"
	workAvailableReason           = "WorkAvailable"
	workNotTrackableReason        = "WorkNotTrackable"
	// ManifestApplyFailedReason is the reason string of condition when it failed to apply manifest.
	ManifestApplyFailedReason = "ManifestApplyFailed"
	// ManifestAlreadyUpToDateReason is the reason string of condition when the manifest is already up to date.
	ManifestAlreadyUpToDateReason = "ManifestAlreadyUpToDate"
	// ManifestNeedsUpdateReason is the reason string of condition when the manifest needs to be updated.
	ManifestNeedsUpdateReason = "ManifestNeedsUpdate"
)

// ApplyWorkReconciler reconciles a Work object
type ApplyWorkReconciler struct {
	client             client.Client
	spokeDynamicClient dynamic.Interface
	spokeClient        client.Client
	restMapper         meta.RESTMapper
	recorder           record.EventRecorder
	concurrency        int
	workNameSpace      string
	joined             *atomic.Bool
}

func NewApplyWorkReconciler(hubClient client.Client, spokeDynamicClient dynamic.Interface, spokeClient client.Client,
	restMapper meta.RESTMapper, recorder record.EventRecorder, concurrency int, workNameSpace string) *ApplyWorkReconciler {
	return &ApplyWorkReconciler{
		client:             hubClient,
		spokeDynamicClient: spokeDynamicClient,
		spokeClient:        spokeClient,
		restMapper:         restMapper,
		recorder:           recorder,
		concurrency:        concurrency,
		workNameSpace:      workNameSpace,
		joined:             atomic.NewBool(false),
	}
}

// applyAction represents the action we take to apply the manifest.
// It is used only internally to track the result of the apply function.
// +enum
type applyAction string

const (
	// manifestCreatedAction indicates that we created the manifest for the first time.
	manifestCreatedAction applyAction = "ManifestCreated"

	// manifestThreeWayMergePatchAction indicates that we updated the manifest using three-way merge patch.
	manifestThreeWayMergePatchAction applyAction = "ManifestThreeWayMergePatched"

	// manifestServerSideAppliedAction indicates that we updated the manifest using server side apply.
	manifestServerSideAppliedAction applyAction = "ManifestServerSideApplied"

	// errorApplyAction indicates that there was an error during the apply action.
	errorApplyAction applyAction = "ErrorApply"

	// manifestNotAvailableYetAction indicates that we still need to wait for the manifest to be available.
	manifestNotAvailableYetAction applyAction = "ManifestNotAvailableYet"

	// manifestNotTrackableAction indicates that the manifest is already up to date but we don't have a way to track its availabilities.
	manifestNotTrackableAction applyAction = "ManifestNotTrackable"

	// manifestAvailableAction indicates that the manifest is available.
	manifestAvailableAction applyAction = "ManifestAvailable"
)

// applyResult contains the result of a manifest being applied.
type applyResult struct {
	identifier fleetv1beta1.WorkResourceIdentifier
	generation int64
	action     applyAction
	applyErr   error
}

// Reconcile implement the control loop logic for Work object.
func (r *ApplyWorkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.joined.Load() {
		klog.V(2).InfoS("Work controller is not started yet, requeue the request", "work", req.NamespacedName)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	startTime := time.Now()
	klog.V(2).InfoS("ApplyWork reconciliation starts", "work", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("ApplyWork reconciliation ends", "work", req.NamespacedName, "latency", latency)
	}()

	// Fetch the work resource
	work := &fleetv1beta1.Work{}
	err := r.client.Get(ctx, req.NamespacedName, work)
	switch {
	case apierrors.IsNotFound(err):
		klog.V(2).InfoS("The work resource is deleted", "work", req.NamespacedName)
		return ctrl.Result{}, nil
	case err != nil:
		klog.ErrorS(err, "Failed to retrieve the work", "work", req.NamespacedName)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}
	logObjRef := klog.KObj(work)

	// Handle deleting work, garbage collect the resources
	if !work.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("Resource is in the process of being deleted", work.Kind, logObjRef)
		return r.garbageCollectAppliedWork(ctx, work)
	}

	// ensure that the appliedWork and the finalizer exist
	appliedWork, err := r.ensureAppliedWork(ctx, work)
	if err != nil {
		return ctrl.Result{}, err
	}
	owner := metav1.OwnerReference{
		APIVersion:         fleetv1beta1.GroupVersion.String(),
		Kind:               fleetv1beta1.AppliedWorkKind,
		Name:               appliedWork.GetName(),
		UID:                appliedWork.GetUID(),
		BlockOwnerDeletion: ptr.To(false),
	}

	// apply the manifests to the member cluster
	results := r.applyManifests(ctx, work.Spec.Workload.Manifests, owner, work.Spec.ApplyStrategy)

	// collect the latency from the work update time to now.
	lastUpdateTime, ok := work.GetAnnotations()[utils.LastWorkUpdateTimeAnnotationKey]
	if ok {
		workUpdateTime, parseErr := time.Parse(time.RFC3339, lastUpdateTime)
		if parseErr != nil {
			klog.ErrorS(parseErr, "Failed to parse the last work update time", "work", logObjRef)
		} else {
			latency := time.Since(workUpdateTime)
			metrics.WorkApplyTime.WithLabelValues(work.GetName()).Observe(latency.Seconds())
			klog.V(2).InfoS("Work is applied", "work", work.GetName(), "latency", latency.Milliseconds())
		}
	} else {
		klog.V(2).InfoS("Work has no last update time", "work", work.GetName())
	}

	// generate the work condition based on the manifest apply result
	errs := constructWorkCondition(results, work)

	// update the work status
	if err = r.client.Status().Update(ctx, work, &client.SubResourceUpdateOptions{}); err != nil {
		klog.ErrorS(err, "Failed to update work status", "work", logObjRef)
		return ctrl.Result{}, err
	}
	if len(errs) == 0 {
		klog.InfoS("Successfully applied the work to the cluster", "work", logObjRef)
		r.recorder.Event(work, v1.EventTypeNormal, "ApplyWorkSucceed", "apply the work successfully")
	}

	// now we sync the status from work to appliedWork no matter if apply succeeds or not
	newRes, staleRes, genErr := r.generateDiff(ctx, work, appliedWork)
	if genErr != nil {
		klog.ErrorS(err, "Failed to generate the diff between work status and appliedWork status", work.Kind, logObjRef)
		return ctrl.Result{}, err
	}
	// delete all the manifests that should not be in the cluster.
	if err = r.deleteStaleManifest(ctx, staleRes, owner); err != nil {
		klog.ErrorS(err, "Resource garbage-collection incomplete; some Work owned resources could not be deleted", work.Kind, logObjRef)
		// we can't proceed to update the applied
		return ctrl.Result{}, err
	} else if len(staleRes) > 0 {
		klog.V(2).InfoS("Successfully garbage-collected all stale manifests", work.Kind, logObjRef, "number of GCed res", len(staleRes))
		for _, res := range staleRes {
			klog.V(2).InfoS("Successfully garbage-collected a stale manifest", work.Kind, logObjRef, "res", res)
		}
	}
	// update the appliedWork with the new work after the stales are deleted
	appliedWork.Status.AppliedResources = newRes
	if err = r.spokeClient.Status().Update(ctx, appliedWork, &client.SubResourceUpdateOptions{}); err != nil {
		klog.ErrorS(err, "Failed to update appliedWork status", appliedWork.Kind, appliedWork.GetName())
		return ctrl.Result{}, err
	}

	if err = utilerrors.NewAggregate(errs); err != nil {
		klog.ErrorS(err, "Manifest apply incomplete; the message is queued again for reconciliation",
			"work", logObjRef)
		return ctrl.Result{}, err
	}
	// check if the work is available, if not, we will requeue the work for reconciliation
	availableCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
	if !condition.IsConditionStatusTrue(availableCond, work.Generation) {
		klog.V(2).InfoS("Work is not available yet, check again", "work", logObjRef, "availableCond", availableCond)
		return ctrl.Result{RequeueAfter: time.Second * 3}, nil
	}
	// the work is available (might due to not trackable) but we still periodically reconcile to make sure the
	// member cluster state is in sync with the work in case the resources on the member cluster is removed/changed.
	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

// garbageCollectAppliedWork deletes the appliedWork and all the manifests associated with it from the cluster.
func (r *ApplyWorkReconciler) garbageCollectAppliedWork(ctx context.Context, work *fleetv1beta1.Work) (ctrl.Result, error) {
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
		return ctrl.Result{}, err
	default:
		klog.InfoS("Successfully deleted the appliedWork", "appliedWork", work.Name)
	}
	controllerutil.RemoveFinalizer(work, fleetv1beta1.WorkFinalizer)
	return ctrl.Result{}, r.client.Update(ctx, work, &client.UpdateOptions{})
}

// ensureAppliedWork makes sure that an associated appliedWork and a finalizer on the work resource exsits on the cluster.
func (r *ApplyWorkReconciler) ensureAppliedWork(ctx context.Context, work *fleetv1beta1.Work) (*fleetv1beta1.AppliedWork, error) {
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
		return nil, err
	}
	if !hasFinalizer {
		klog.InfoS("Add the finalizer to the work", "work", workRef)
		work.Finalizers = append(work.Finalizers, fleetv1beta1.WorkFinalizer)
		return appliedWork, r.client.Update(ctx, work, &client.UpdateOptions{})
	}
	klog.InfoS("Recreated the appliedWork resource", "appliedWork", workRef.Name)
	return appliedWork, nil
}

// applyManifests processes a given set of Manifests by: setting ownership, validating the manifest, and passing it on for application to the cluster.
func (r *ApplyWorkReconciler) applyManifests(ctx context.Context, manifests []fleetv1beta1.Manifest, owner metav1.OwnerReference, applyStrategy *fleetv1beta1.ApplyStrategy) []applyResult {
	var appliedObj *unstructured.Unstructured

	results := make([]applyResult, len(manifests))
	for index, manifest := range manifests {
		var result applyResult
		gvr, rawObj, err := r.decodeManifest(manifest)
		switch {
		case err != nil:
			result.applyErr = err
			result.identifier = fleetv1beta1.WorkResourceIdentifier{
				Ordinal: index,
			}
			if rawObj != nil {
				result.identifier.Group = rawObj.GroupVersionKind().Group
				result.identifier.Version = rawObj.GroupVersionKind().Version
				result.identifier.Kind = rawObj.GroupVersionKind().Kind
				result.identifier.Namespace = rawObj.GetNamespace()
				result.identifier.Name = rawObj.GetName()
			}

		default:
			addOwnerRef(owner, rawObj)
			appliedObj, result.action, result.applyErr = r.applyUnstructured(ctx, gvr, rawObj)
			result.identifier = buildResourceIdentifier(index, rawObj, gvr)
			logObjRef := klog.ObjectRef{
				Name:      result.identifier.Name,
				Namespace: result.identifier.Namespace,
			}
			if result.applyErr == nil {
				result.generation = appliedObj.GetGeneration()
				klog.V(2).InfoS("Apply manifest succeeded", "gvr", gvr, "manifest", logObjRef,
					"action", result.action, "applyStrategy", applyStrategy, "new ObservedGeneration", result.generation)
			} else {
				klog.ErrorS(result.applyErr, "manifest upsert failed", "gvr", gvr, "manifest", logObjRef)
			}
		}
		results[index] = result
	}
	return results
}

// Decodes the manifest into usable structs.
func (r *ApplyWorkReconciler) decodeManifest(manifest fleetv1beta1.Manifest) (schema.GroupVersionResource, *unstructured.Unstructured, error) {
	unstructuredObj := &unstructured.Unstructured{}
	err := unstructuredObj.UnmarshalJSON(manifest.Raw)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("failed to decode object: %w", err)
	}

	mapping, err := r.restMapper.RESTMapping(unstructuredObj.GroupVersionKind().GroupKind(), unstructuredObj.GroupVersionKind().Version)
	if err != nil {
		return schema.GroupVersionResource{}, unstructuredObj, fmt.Errorf("failed to find group/version/resource from restmapping: %w", err)
	}

	return mapping.Resource, unstructuredObj, nil
}

// applyUnstructured determines if an unstructured manifest object can & should be applied. It first validates
// the size of the last modified annotation of the manifest, it removes the annotation if the size crosses the annotation size threshold
// and then creates/updates the resource on the cluster using server side apply instead of three-way merge patch.
func (r *ApplyWorkReconciler) applyUnstructured(ctx context.Context, gvr schema.GroupVersionResource,
	manifestObj *unstructured.Unstructured) (*unstructured.Unstructured, applyAction, error) {
	manifestRef := klog.ObjectRef{
		Name:      manifestObj.GetName(),
		Namespace: manifestObj.GetNamespace(),
	}

	// compute the hash without taking into consider the last applied annotation
	if err := setManifestHashAnnotation(manifestObj); err != nil {
		return nil, errorApplyAction, err
	}

	// extract the common create procedure to reuse
	var createFunc = func() (*unstructured.Unstructured, applyAction, error) {
		// record the raw manifest with the hash annotation in the manifest
		if _, err := setModifiedConfigurationAnnotation(manifestObj); err != nil {
			return nil, errorApplyAction, err
		}
		actual, err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).Create(
			ctx, manifestObj, metav1.CreateOptions{FieldManager: workFieldManagerName})
		if err == nil {
			klog.V(2).InfoS("Successfully created the manifest", "gvr", gvr, "manifest", manifestRef)
			return actual, manifestCreatedAction, nil
		}
		return nil, errorApplyAction, err
	}

	// support resources with generated name
	if manifestObj.GetName() == "" && manifestObj.GetGenerateName() != "" {
		klog.InfoS("Create the resource with generated name regardless", "gvr", gvr, "manifest", manifestRef)
		return createFunc()
	}

	// get the current object and create one if not found
	curObj, err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).Get(ctx, manifestObj.GetName(), metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		return createFunc()
	case err != nil:
		return nil, errorApplyAction, controller.NewAPIServerError(false, err)
	}

	// check if the existing manifest is managed by the work
	if !isManifestManagedByWork(curObj.GetOwnerReferences()) {
		// TODO: honer the applyStrategy to decide if we should overwrite the existing resource
		err = fmt.Errorf("resource is not managed by the work controller")
		klog.ErrorS(err, "Skip applying a not managed manifest", "gvr", gvr, "obj", manifestRef)
		return nil, errorApplyAction, controller.NewExpectedBehaviorError(err)
	}

	// We only try to update the object if its spec hash value has changed.
	if manifestObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation] != curObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation] {
		// we need to merge the owner reference between the current and the manifest since we support one manifest
		// belong to multiple work, so it contains the union of all the appliedWork.
		manifestObj.SetOwnerReferences(mergeOwnerReference(curObj.GetOwnerReferences(), manifestObj.GetOwnerReferences()))
		// record the raw manifest with the hash annotation in the manifest.
		isModifiedConfigAnnotationNotEmpty, err := setModifiedConfigurationAnnotation(manifestObj)
		if err != nil {
			return nil, errorApplyAction, err
		}
		if !isModifiedConfigAnnotationNotEmpty {
			klog.V(2).InfoS("Using server side apply for manifest", "gvr", gvr, "manifest", manifestRef)
			return r.serversideApplyObject(ctx, gvr, manifestObj)
		}
		klog.V(2).InfoS("Using three way merge for manifest", "gvr", gvr, "manifest", manifestRef)
		return r.patchCurrentResource(ctx, gvr, manifestObj, curObj)
	}
	// the manifest is already up to date, we just need to track its availability
	applyAction, err := trackResourceAvailability(gvr, curObj)
	return curObj, applyAction, err
}

func trackResourceAvailability(gvr schema.GroupVersionResource, curObj *unstructured.Unstructured) (applyAction, error) {
	switch gvr {
	case utils.DeploymentGVR:
		return trackDeploymentAvailability(curObj)

	case utils.StatefulSettGVR:
		return trackStatefulSetAvailability(curObj)

	case utils.DaemonSettGVR:
		return trackDaemonSetAvailability(curObj)

	case utils.JobGVR:
		return trackJobAvailability(curObj)

	default:
		klog.V(2).InfoS("We don't know how to track the availability of the resource", "gvr", gvr, "resource", klog.KObj(curObj))
		return manifestNotTrackableAction, nil
	}
}

func trackDeploymentAvailability(curObj *unstructured.Unstructured) (applyAction, error) {
	var deployment appv1.Deployment
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(curObj.Object, &deployment); err != nil {
		return errorApplyAction, controller.NewUnexpectedBehaviorError(err)
	}
	// see if the DeploymentAvailable condition is true
	var depCond *appv1.DeploymentCondition
	for i := range deployment.Status.Conditions {
		if deployment.Status.Conditions[i].Type == appv1.DeploymentAvailable {
			depCond = &deployment.Status.Conditions[i]
			break
		}
	}
	// a deployment is available if the observedGeneration is equal to the generation and the Available condition is true
	if deployment.Status.ObservedGeneration == deployment.Generation && depCond != nil && depCond.Status == v1.ConditionTrue {
		klog.V(2).InfoS("Deployment is available", "deployment", klog.KObj(curObj))
		return manifestAvailableAction, nil
	}
	klog.V(2).InfoS("Still need to wait for deployment to be available", "deployment", klog.KObj(curObj))
	return manifestNotAvailableYetAction, nil
}

func trackStatefulSetAvailability(curObj *unstructured.Unstructured) (applyAction, error) {
	var statefulSet appv1.StatefulSet
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(curObj.Object, &statefulSet); err != nil {
		return errorApplyAction, controller.NewUnexpectedBehaviorError(err)
	}
	// a statefulSet is available if all the replicas are available and the currentReplicas is equal to the updatedReplicas
	// which means there is no more update in progress.
	requiredReplicas := int32(1)
	if statefulSet.Spec.Replicas != nil {
		requiredReplicas = *statefulSet.Spec.Replicas
	}
	if statefulSet.Status.ObservedGeneration == statefulSet.Generation &&
		statefulSet.Status.AvailableReplicas == requiredReplicas &&
		statefulSet.Status.CurrentReplicas == statefulSet.Status.UpdatedReplicas &&
		statefulSet.Status.CurrentRevision == statefulSet.Status.UpdateRevision {
		klog.V(2).InfoS("StatefulSet is available", "statefulSet", klog.KObj(curObj))
		return manifestAvailableAction, nil
	}
	klog.V(2).InfoS("Still need to wait for statefulSet to be available", "statefulSet", klog.KObj(curObj))
	return manifestNotAvailableYetAction, nil
}

func trackDaemonSetAvailability(curObj *unstructured.Unstructured) (applyAction, error) {
	var daemonSet appv1.DaemonSet
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(curObj.Object, &daemonSet); err != nil {
		return errorApplyAction, controller.NewUnexpectedBehaviorError(err)
	}
	// a daemonSet is available if all the desired replicas (equal to all node suit for this Daemonset)
	// are available and the currentReplicas is equal to the updatedReplicas which means there is no more update in progress.
	if daemonSet.Status.ObservedGeneration == daemonSet.Generation &&
		daemonSet.Status.NumberAvailable == daemonSet.Status.DesiredNumberScheduled &&
		daemonSet.Status.CurrentNumberScheduled == daemonSet.Status.UpdatedNumberScheduled {
		klog.V(2).InfoS("DaemonSet is available", "daemonSet", klog.KObj(curObj))
		return manifestAvailableAction, nil
	}
	klog.V(2).InfoS("Still need to wait for daemonSet to be available", "daemonSet", klog.KObj(curObj))
	return manifestNotAvailableYetAction, nil
}

func trackJobAvailability(curObj *unstructured.Unstructured) (applyAction, error) {
	var job batchv1.Job
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(curObj.Object, &job); err != nil {
		return errorApplyAction, controller.NewUnexpectedBehaviorError(err)
	}
	if job.Status.Succeeded > 0 {
		klog.V(2).InfoS("Job is available with at least one succeeded pod", "job", klog.KObj(curObj))
		return manifestAvailableAction, nil
	}
	if job.Status.Ready != nil {
		// we consider a job available if there is at least one pod ready
		if *job.Status.Ready > 0 {
			klog.V(2).InfoS("Job is available with at least one ready pod", "job", klog.KObj(curObj))
			return manifestAvailableAction, nil
		}
		klog.V(2).InfoS("Still need to wait for job to be available", "job", klog.KObj(curObj))
		return manifestNotAvailableYetAction, nil
	}
	// this field only exists in k8s 1.24+ by default, so we can't track the availability of the job without it
	klog.V(2).InfoS("Job does not have ready status, we can't track its availability", "job", klog.KObj(curObj))
	return manifestNotTrackableAction, nil
}

// serversideApplyObject uses server side apply to apply the manifest.
func (r *ApplyWorkReconciler) serversideApplyObject(ctx context.Context, gvr schema.GroupVersionResource,
	manifestObj *unstructured.Unstructured) (*unstructured.Unstructured, applyAction, error) {
	manifestRef := klog.ObjectRef{
		Name:      manifestObj.GetName(),
		Namespace: manifestObj.GetNamespace(),
	}
	options := metav1.ApplyOptions{
		FieldManager: workFieldManagerName,
		Force:        true,
	}
	manifestObj, err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).Apply(ctx, manifestObj.GetName(), manifestObj, options)
	if err != nil {
		klog.ErrorS(err, "Failed to apply object", "gvr", gvr, "manifest", manifestRef)
		return nil, errorApplyAction, err
	}
	klog.V(2).InfoS("Manifest apply succeeded", "gvr", gvr, "manifest", manifestRef)
	return manifestObj, manifestServerSideAppliedAction, nil
}

// patchCurrentResource uses three-way merge to patch the current resource with the new manifest we get from the work.
func (r *ApplyWorkReconciler) patchCurrentResource(ctx context.Context, gvr schema.GroupVersionResource,
	manifestObj, curObj *unstructured.Unstructured) (*unstructured.Unstructured, applyAction, error) {
	manifestRef := klog.ObjectRef{
		Name:      manifestObj.GetName(),
		Namespace: manifestObj.GetNamespace(),
	}
	klog.V(2).InfoS("Manifest is modified", "gvr", gvr, "manifest", manifestRef,
		"new hash", manifestObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation],
		"existing hash", curObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation])
	// create the three-way merge patch between the current, original and manifest similar to how kubectl apply does
	patch, err := threeWayMergePatch(curObj, manifestObj)
	if err != nil {
		klog.ErrorS(err, "Failed to generate the three way patch", "gvr", gvr, "manifest", manifestRef)
		return nil, errorApplyAction, err
	}
	data, err := patch.Data(manifestObj)
	if err != nil {
		klog.ErrorS(err, "Failed to generate the three way patch", "gvr", gvr, "manifest", manifestRef)
		return nil, errorApplyAction, err
	}
	// Use client side apply the patch to the member cluster
	manifestObj, patchErr := r.spokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).
		Patch(ctx, manifestObj.GetName(), patch.Type(), data, metav1.PatchOptions{FieldManager: workFieldManagerName})
	if patchErr != nil {
		klog.ErrorS(patchErr, "Failed to patch the manifest", "gvr", gvr, "manifest", manifestRef)
		return nil, errorApplyAction, patchErr
	}
	klog.V(2).InfoS("Manifest patch succeeded", "gvr", gvr, "manifest", manifestRef)
	return manifestObj, manifestThreeWayMergePatchAction, nil
}

// constructWorkCondition constructs the work condition based on the apply result
func constructWorkCondition(results []applyResult, work *fleetv1beta1.Work) []error {
	var errs []error
	// Update manifestCondition based on the results.
	manifestConditions := make([]fleetv1beta1.ManifestCondition, len(results))
	for index, result := range results {
		if result.applyErr != nil {
			errs = append(errs, result.applyErr)
		}
		newConditions := buildManifestCondition(result.applyErr, result.action, result.generation)
		manifestCondition := fleetv1beta1.ManifestCondition{
			Identifier: result.identifier,
		}
		existingManifestCondition := findManifestConditionByIdentifier(result.identifier, work.Status.ManifestConditions)
		if existingManifestCondition != nil {
			manifestCondition.Conditions = existingManifestCondition.Conditions
		}
		// merge the status of the manifest condition
		for _, condition := range newConditions {
			meta.SetStatusCondition(&manifestCondition.Conditions, condition)
		}
		manifestConditions[index] = manifestCondition
	}

	work.Status.ManifestConditions = manifestConditions
	// merge the status of the work condition
	newWorkConditions := buildWorkCondition(manifestConditions, work.Generation)
	for _, condition := range newWorkConditions {
		meta.SetStatusCondition(&work.Status.Conditions, condition)
	}
	return errs
}

// Join starts to reconcile
func (r *ApplyWorkReconciler) Join(_ context.Context) error {
	if !r.joined.Load() {
		klog.InfoS("Mark the apply work reconciler joined")
	}
	r.joined.Store(true)
	return nil
}

// Leave start
func (r *ApplyWorkReconciler) Leave(ctx context.Context) error {
	var works fleetv1beta1.WorkList
	if r.joined.Load() {
		klog.InfoS("Mark the apply work reconciler left")
	}
	r.joined.Store(false)
	// list all the work object we created in the member cluster namespace
	listOpts := []client.ListOption{
		client.InNamespace(r.workNameSpace),
	}
	if err := r.client.List(ctx, &works, listOpts...); err != nil {
		klog.ErrorS(err, "Failed to list all the work object", "clusterNS", r.workNameSpace)
		return client.IgnoreNotFound(err)
	}
	// we leave the resources on the member cluster for now
	for _, work := range works.Items {
		staleWork := work.DeepCopy()
		if controllerutil.ContainsFinalizer(staleWork, fleetv1beta1.WorkFinalizer) {
			controllerutil.RemoveFinalizer(staleWork, fleetv1beta1.WorkFinalizer)
			if updateErr := r.client.Update(ctx, staleWork, &client.UpdateOptions{}); updateErr != nil {
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
func (r *ApplyWorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(ctrloption.Options{
			MaxConcurrentReconciles: r.concurrency,
		}).
		For(&fleetv1beta1.Work{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

// Generates a hash of the spec annotation from an unstructured object after we remove all the fields
// we have modified.
func computeManifestHash(obj *unstructured.Unstructured) (string, error) {
	manifest := obj.DeepCopy()
	// remove the last applied Annotation to avoid unlimited recursion
	annotation := manifest.GetAnnotations()
	if annotation != nil {
		delete(annotation, fleetv1beta1.ManifestHashAnnotation)
		delete(annotation, fleetv1beta1.LastAppliedConfigAnnotation)
		if len(annotation) == 0 {
			manifest.SetAnnotations(nil)
		} else {
			manifest.SetAnnotations(annotation)
		}
	}
	// strip the live object related fields just in case
	manifest.SetResourceVersion("")
	manifest.SetGeneration(0)
	manifest.SetUID("")
	manifest.SetSelfLink("")
	manifest.SetDeletionTimestamp(nil)
	manifest.SetManagedFields(nil)
	unstructured.RemoveNestedField(manifest.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(manifest.Object, "status")
	// compute the sha256 hash of the remaining data
	return resource.HashOf(manifest.Object)
}

// isManifestManagedByWork determines if an object is managed by the work controller.
func isManifestManagedByWork(ownerRefs []metav1.OwnerReference) bool {
	// an object is managed by the work if any of its owner reference is of type appliedWork
	for _, ownerRef := range ownerRefs {
		if ownerRef.APIVersion == fleetv1beta1.GroupVersion.String() && ownerRef.Kind == fleetv1beta1.AppliedWorkKind {
			return true
		}
	}
	return false
}

// findManifestConditionByIdentifier return a ManifestCondition by identifier
// 1. Find the manifest condition with the whole identifier.
// 2. If identifier only has ordinal, and a matched cannot be found, return nil.
// 3. Try to find properties, other than the ordinal, within the identifier.
func findManifestConditionByIdentifier(identifier fleetv1beta1.WorkResourceIdentifier, manifestConditions []fleetv1beta1.ManifestCondition) *fleetv1beta1.ManifestCondition {
	for _, manifestCondition := range manifestConditions {
		if identifier == manifestCondition.Identifier {
			return &manifestCondition
		}
	}

	if identifier == (fleetv1beta1.WorkResourceIdentifier{Ordinal: identifier.Ordinal}) {
		return nil
	}

	identifierCopy := identifier.DeepCopy()
	for _, manifestCondition := range manifestConditions {
		identifierCopy.Ordinal = manifestCondition.Identifier.Ordinal
		if *identifierCopy == manifestCondition.Identifier {
			return &manifestCondition
		}
	}
	return nil
}

// setManifestHashAnnotation computes the hash of the provided manifest and sets an annotation of the
// hash on the provided unstructured object.
func setManifestHashAnnotation(manifestObj *unstructured.Unstructured) error {
	manifestHash, err := computeManifestHash(manifestObj)
	if err != nil {
		return err
	}

	annotation := manifestObj.GetAnnotations()
	if annotation == nil {
		annotation = map[string]string{}
	}
	annotation[fleetv1beta1.ManifestHashAnnotation] = manifestHash
	manifestObj.SetAnnotations(annotation)
	return nil
}

// Builds a resource identifier for a given unstructured.Unstructured object.
func buildResourceIdentifier(index int, object *unstructured.Unstructured, gvr schema.GroupVersionResource) fleetv1beta1.WorkResourceIdentifier {
	return fleetv1beta1.WorkResourceIdentifier{
		Ordinal:   index,
		Group:     object.GroupVersionKind().Group,
		Version:   object.GroupVersionKind().Version,
		Kind:      object.GroupVersionKind().Kind,
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
		Resource:  gvr.Resource,
	}
}

func buildManifestCondition(err error, action applyAction, observedGeneration int64) []metav1.Condition {
	applyCondition := metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeApplied,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: observedGeneration,
	}

	availableCondition := metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeAvailable,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: observedGeneration,
	}

	if err != nil {
		applyCondition.Status = metav1.ConditionFalse
		applyCondition.Reason = ManifestApplyFailedReason
		applyCondition.Message = fmt.Sprintf("Failed to apply manifest: %v", err)
		availableCondition.Status = metav1.ConditionUnknown
		availableCondition.Reason = ManifestApplyFailedReason
	} else {
		applyCondition.Status = metav1.ConditionTrue
		// the first three actions types means we did write to the cluster thus the availability is unknown
		// the last three actions types means we start to track the resources
		switch action {
		case manifestCreatedAction:
			applyCondition.Reason = string(manifestCreatedAction)
			availableCondition.Status = metav1.ConditionUnknown
			availableCondition.Reason = ManifestNeedsUpdateReason

		case manifestThreeWayMergePatchAction:
			applyCondition.Reason = string(manifestThreeWayMergePatchAction)
			availableCondition.Status = metav1.ConditionUnknown
			availableCondition.Reason = ManifestNeedsUpdateReason

		case manifestServerSideAppliedAction:
			applyCondition.Reason = string(manifestServerSideAppliedAction)
			availableCondition.Status = metav1.ConditionUnknown
			availableCondition.Reason = ManifestNeedsUpdateReason

		case manifestAvailableAction:
			applyCondition.Reason = ManifestAlreadyUpToDateReason
			availableCondition.Status = metav1.ConditionTrue
			availableCondition.Reason = string(manifestAvailableAction)

		case manifestNotAvailableYetAction:
			applyCondition.Reason = ManifestAlreadyUpToDateReason
			availableCondition.Status = metav1.ConditionFalse
			availableCondition.Reason = string(manifestNotAvailableYetAction)
		// we cannot stuck at unknown so we have to mark it as true
		case manifestNotTrackableAction:
			applyCondition.Reason = ManifestAlreadyUpToDateReason
			availableCondition.Status = metav1.ConditionTrue
			availableCondition.Reason = string(manifestNotTrackableAction)

		default:
			klog.ErrorS(controller.ErrUnexpectedBehavior, "Unknown apply action result", "applyResult", action)
		}
	}

	return []metav1.Condition{applyCondition, availableCondition}
}

// buildWorkCondition generate applied and available status condition for work.
// If one of the manifests is applied failed on the spoke, the applied status condition of the work is false.
// If one of the manifests is not available yet on the spoke, the available status condition of the work is false.
// If all the manifests are available, the available status condition of the work is true.
// Otherwise, the available status condition of the work is unknown as we can't track some of them.
func buildWorkCondition(manifestConditions []fleetv1beta1.ManifestCondition, observedGeneration int64) []metav1.Condition {
	applyCondition := metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeApplied,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: observedGeneration,
	}
	availableCondition := metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeAvailable,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: observedGeneration,
	}
	// the manifest condition should not be an empty list
	for _, manifestCond := range manifestConditions {
		if meta.IsStatusConditionFalse(manifestCond.Conditions, fleetv1beta1.WorkConditionTypeApplied) {
			// we mark the entire work applied condition to false if one of the manifests is applied failed
			applyCondition.Status = metav1.ConditionFalse
			applyCondition.Reason = workAppliedFailedReason
			applyCondition.Message = fmt.Sprintf("Apply manifest %+v failed", manifestCond.Identifier)
			availableCondition.Status = metav1.ConditionUnknown
			availableCondition.Reason = workAppliedFailedReason
			return []metav1.Condition{applyCondition, availableCondition}
		}
	}
	applyCondition.Status = metav1.ConditionTrue
	applyCondition.Reason = workAppliedCompleteReason
	applyCondition.Message = "Apply work complete"
	// we mark the entire work available condition to unknown if one of the manifests is not known yet
	for _, manifestCond := range manifestConditions {
		cond := meta.FindStatusCondition(manifestCond.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		if cond.Status == metav1.ConditionUnknown {
			availableCondition.Status = metav1.ConditionUnknown
			availableCondition.Reason = workAvailabilityUnknownReason
			availableCondition.Message = fmt.Sprintf("Manifest %+v availability is not known yet", manifestCond.Identifier)
			return []metav1.Condition{applyCondition, availableCondition}
		}
	}
	// now that there is no unknown, we mark the entire work available condition to false if one of the manifests is not applied yet
	for _, manifestCond := range manifestConditions {
		cond := meta.FindStatusCondition(manifestCond.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		if cond.Status == metav1.ConditionFalse {
			availableCondition.Status = metav1.ConditionFalse
			availableCondition.Reason = workNotAvailableYetReason
			availableCondition.Message = fmt.Sprintf("Manifest %+v is not available yet", manifestCond.Identifier)
			return []metav1.Condition{applyCondition, availableCondition}
		}
	}
	// now that all the conditions are true, we mark the entire work available condition reason to be not trackable if one of the manifests is not trackable
	trackable := true
	for _, manifestCond := range manifestConditions {
		cond := meta.FindStatusCondition(manifestCond.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		if cond.Reason == string(manifestNotTrackableAction) {
			trackable = false
			break
		}
	}
	availableCondition.Status = metav1.ConditionTrue
	if trackable {
		availableCondition.Reason = workAvailableReason
	} else {
		availableCondition.Reason = workNotTrackableReason
	}
	return []metav1.Condition{applyCondition, availableCondition}
}
