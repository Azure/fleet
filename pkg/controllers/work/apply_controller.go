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
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/resource"
)

const (
	workFieldManagerName = "work-api-agent"
)

// WorkCondition condition reasons
const (
	// AppliedWorkFailedReason is the reason string of work condition when it failed to apply the work.
	AppliedWorkFailedReason = "AppliedWorkFailed"
	// AppliedWorkCompleteReason is the reason string of work condition when it finished applying work.
	AppliedWorkCompleteReason = "AppliedWorkComplete"
	// AppliedManifestFailedReason is the reason string of condition when it failed to apply manifest.
	AppliedManifestFailedReason = "AppliedManifestFailedReason"
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

// applyAction represents the action we take to apply the manifest
// +enum
type applyAction string

const (
	// ManifestCreatedAction indicates that we created the manifest for the first time.
	ManifestCreatedAction applyAction = "ManifestCreated"

	// ManifestThreeWayMergePatchAction indicates that we updated the manifest using three-way merge patch.
	ManifestThreeWayMergePatchAction applyAction = "ManifestThreeWayMergePatched"

	// ManifestServerSideAppliedAction indicates that we updated the manifest using server side apply.
	ManifestServerSideAppliedAction applyAction = "ManifestServerSideApplied"

	// ManifestNoChangeAction indicates that we don't need to change the manifest.
	ManifestNoChangeAction applyAction = "ManifestNoChange"
)

// applyResult contains the result of a manifest being applied.
type applyResult struct {
	identifier fleetv1beta1.WorkResourceIdentifier
	generation int64
	action     applyAction
	err        error
}

// Reconcile implement the control loop logic for Work object.
func (r *ApplyWorkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.joined.Load() {
		klog.V(2).InfoS("work controller is not started yet, requeue the request", "work", req.NamespacedName)
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
		klog.V(2).InfoS("the work resource is deleted", "work", req.NamespacedName)
		return ctrl.Result{}, nil
	case err != nil:
		klog.ErrorS(err, "failed to retrieve the work", "work", req.NamespacedName)
		return ctrl.Result{}, err
	}
	logObjRef := klog.KObj(work)

	// Handle deleting work, garbage collect the resources
	if !work.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("resource is in the process of being deleted", work.Kind, logObjRef)
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
	results := r.applyManifests(ctx, work.Spec.Workload.Manifests, owner)

	// collect the latency from the work update time to now.
	lastUpdateTime, ok := work.GetAnnotations()[utils.LastWorkUpdateTimeAnnotationKey]
	if ok {
		workUpdateTime, parseErr := time.Parse(time.RFC3339, lastUpdateTime)
		if parseErr != nil {
			klog.ErrorS(parseErr, "failed to parse the last work update time", "work", logObjRef)
		} else {
			latency := time.Since(workUpdateTime)
			metrics.WorkApplyTime.WithLabelValues(work.GetName()).Observe(latency.Seconds())
			klog.V(2).InfoS("work is applied", "work", work.GetName(), "latency", latency.Milliseconds())
		}
	} else {
		klog.V(2).InfoS("work has no last update time", "work", work.GetName())
	}

	// generate the work condition based on the manifest apply result
	errs := r.generateWorkCondition(results, work)

	// update the work status
	if err = r.client.Status().Update(ctx, work, &client.SubResourceUpdateOptions{}); err != nil {
		klog.ErrorS(err, "failed to update work status", "work", logObjRef)
		return ctrl.Result{}, err
	}
	if len(errs) == 0 {
		klog.InfoS("successfully applied the work to the cluster", "work", logObjRef)
		r.recorder.Event(work, v1.EventTypeNormal, "ApplyWorkSucceed", "apply the work successfully")
	}

	// now we sync the status from work to appliedWork no matter if apply succeeds or not
	newRes, staleRes, genErr := r.generateDiff(ctx, work, appliedWork)
	if genErr != nil {
		klog.ErrorS(err, "failed to generate the diff between work status and appliedWork status", work.Kind, logObjRef)
		return ctrl.Result{}, err
	}
	// delete all the manifests that should not be in the cluster.
	if err = r.deleteStaleManifest(ctx, staleRes, owner); err != nil {
		klog.ErrorS(err, "resource garbage-collection incomplete; some Work owned resources could not be deleted", work.Kind, logObjRef)
		// we can't proceed to update the applied
		return ctrl.Result{}, err
	} else if len(staleRes) > 0 {
		klog.V(2).InfoS("successfully garbage-collected all stale manifests", work.Kind, logObjRef, "number of GCed res", len(staleRes))
		for _, res := range staleRes {
			klog.V(2).InfoS("successfully garbage-collected a stale manifest", work.Kind, logObjRef, "res", res)
		}
	}

	// update the appliedWork with the new work after the stales are deleted
	appliedWork.Status.AppliedResources = newRes
	if err = r.spokeClient.Status().Update(ctx, appliedWork, &client.SubResourceUpdateOptions{}); err != nil {
		klog.ErrorS(err, "failed to update appliedWork status", appliedWork.Kind, appliedWork.GetName())
		return ctrl.Result{}, err
	}
	err = utilerrors.NewAggregate(errs)
	if err != nil {
		klog.ErrorS(err, "manifest apply incomplete; the message is queued again for reconciliation",
			"work", logObjRef)
	}

	// we periodically reconcile the work to make sure the member cluster state is in sync with the work
	// even if the reconciling succeeds in case the resources on the member cluster is removed/changed.
	return ctrl.Result{RequeueAfter: time.Minute * 5}, err
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
		klog.V(2).InfoS("the appliedWork is already deleted", "appliedWork", work.Name)
	case err != nil:
		klog.ErrorS(err, "failed to delete the appliedWork", "appliedWork", work.Name)
		return ctrl.Result{}, err
	default:
		klog.InfoS("successfully deleted the appliedWork", "appliedWork", work.Name)
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
			klog.ErrorS(err, "appliedWork finalizer resource does not exist even with the finalizer, it will be recreated", "appliedWork", workRef.Name)
		case err != nil:
			klog.ErrorS(err, "failed to retrieve the appliedWork ", "appliedWork", workRef.Name)
			return nil, err
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
		klog.ErrorS(err, "appliedWork create failed", "appliedWork", workRef.Name)
		return nil, err
	}
	if !hasFinalizer {
		klog.InfoS("add the finalizer to the work", "work", workRef)
		work.Finalizers = append(work.Finalizers, fleetv1beta1.WorkFinalizer)
		return appliedWork, r.client.Update(ctx, work, &client.UpdateOptions{})
	}
	klog.InfoS("recreated the appliedWork resource", "appliedWork", workRef.Name)
	return appliedWork, nil
}

// applyManifests processes a given set of Manifests by: setting ownership, validating the manifest, and passing it on for application to the cluster.
func (r *ApplyWorkReconciler) applyManifests(ctx context.Context, manifests []fleetv1beta1.Manifest, owner metav1.OwnerReference) []applyResult {
	var appliedObj *unstructured.Unstructured

	results := make([]applyResult, len(manifests))
	for index, manifest := range manifests {
		var result applyResult
		gvr, rawObj, err := r.decodeManifest(manifest)
		switch {
		case err != nil:
			result.err = err
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
			appliedObj, result.action, result.err = r.applyUnstructured(ctx, gvr, rawObj)
			result.identifier = buildResourceIdentifier(index, rawObj, gvr)
			logObjRef := klog.ObjectRef{
				Name:      result.identifier.Name,
				Namespace: result.identifier.Namespace,
			}
			if result.err == nil {
				result.generation = appliedObj.GetGeneration()
				klog.V(2).InfoS("apply manifest succeeded", "gvr", gvr, "manifest", logObjRef,
					"apply action", result.action, "new ObservedGeneration", result.generation)
			} else {
				klog.ErrorS(result.err, "manifest upsert failed", "gvr", gvr, "manifest", logObjRef)
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
		return nil, ManifestNoChangeAction, err
	}

	// extract the common create procedure to reuse
	var createFunc = func() (*unstructured.Unstructured, applyAction, error) {
		// record the raw manifest with the hash annotation in the manifest
		if _, err := setModifiedConfigurationAnnotation(manifestObj); err != nil {
			return nil, ManifestNoChangeAction, err
		}
		actual, err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).Create(
			ctx, manifestObj, metav1.CreateOptions{FieldManager: workFieldManagerName})
		if err == nil {
			klog.V(2).InfoS("successfully created the manifest", "gvr", gvr, "manifest", manifestRef)
			return actual, ManifestCreatedAction, nil
		}
		return nil, ManifestNoChangeAction, err
	}

	// support resources with generated name
	if manifestObj.GetName() == "" && manifestObj.GetGenerateName() != "" {
		klog.InfoS("create the resource with generated name regardless", "gvr", gvr, "manifest", manifestRef)
		return createFunc()
	}

	// get the current object and create one if not found
	curObj, err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).Get(ctx, manifestObj.GetName(), metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		return createFunc()
	case err != nil:
		return nil, ManifestNoChangeAction, err
	}

	// check if the existing manifest is managed by the work
	if !isManifestManagedByWork(curObj.GetOwnerReferences()) {
		err = fmt.Errorf("resource is not managed by the work controller")
		klog.ErrorS(err, "skip applying a not managed manifest", "gvr", gvr, "obj", manifestRef)
		return nil, ManifestNoChangeAction, err
	}

	// We only try to update the object if its spec hash value has changed.
	if manifestObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation] != curObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation] {
		// we need to merge the owner reference between the current and the manifest since we support one manifest
		// belong to multiple work, so it contains the union of all the appliedWork.
		manifestObj.SetOwnerReferences(mergeOwnerReference(curObj.GetOwnerReferences(), manifestObj.GetOwnerReferences()))
		// record the raw manifest with the hash annotation in the manifest.
		isModifiedConfigAnnotationNotEmpty, err := setModifiedConfigurationAnnotation(manifestObj)
		if err != nil {
			return nil, ManifestNoChangeAction, err
		}
		if !isModifiedConfigAnnotationNotEmpty {
			klog.V(2).InfoS("using server side apply for manifest", "gvr", gvr, "manifest", manifestRef)
			return r.applyObject(ctx, gvr, manifestObj)
		}
		klog.V(2).InfoS("using three way merge for manifest", "gvr", gvr, "manifest", manifestRef)
		return r.patchCurrentResource(ctx, gvr, manifestObj, curObj)
	}

	return curObj, ManifestNoChangeAction, nil
}

// applyObject uses server side apply to apply the manifest.
func (r *ApplyWorkReconciler) applyObject(ctx context.Context, gvr schema.GroupVersionResource,
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
		klog.ErrorS(err, "failed to apply object", "gvr", gvr, "manifest", manifestRef)
		return nil, ManifestNoChangeAction, err
	}
	klog.V(2).InfoS("manifest apply succeeded", "gvr", gvr, "manifest", manifestRef)
	return manifestObj, ManifestServerSideAppliedAction, nil
}

// patchCurrentResource uses three-way merge to patch the current resource with the new manifest we get from the work.
func (r *ApplyWorkReconciler) patchCurrentResource(ctx context.Context, gvr schema.GroupVersionResource,
	manifestObj, curObj *unstructured.Unstructured) (*unstructured.Unstructured, applyAction, error) {
	manifestRef := klog.ObjectRef{
		Name:      manifestObj.GetName(),
		Namespace: manifestObj.GetNamespace(),
	}
	klog.V(2).InfoS("manifest is modified", "gvr", gvr, "manifest", manifestRef,
		"new hash", manifestObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation],
		"existing hash", curObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation])
	// create the three-way merge patch between the current, original and manifest similar to how kubectl apply does
	patch, err := threeWayMergePatch(curObj, manifestObj)
	if err != nil {
		klog.ErrorS(err, "failed to generate the three way patch", "gvr", gvr, "manifest", manifestRef)
		return nil, ManifestNoChangeAction, err
	}
	data, err := patch.Data(manifestObj)
	if err != nil {
		klog.ErrorS(err, "failed to generate the three way patch", "gvr", gvr, "manifest", manifestRef)
		return nil, ManifestNoChangeAction, err
	}
	// Use client side apply the patch to the member cluster
	manifestObj, patchErr := r.spokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).
		Patch(ctx, manifestObj.GetName(), patch.Type(), data, metav1.PatchOptions{FieldManager: workFieldManagerName})
	if patchErr != nil {
		klog.ErrorS(patchErr, "failed to patch the manifest", "gvr", gvr, "manifest", manifestRef)
		return nil, ManifestNoChangeAction, patchErr
	}
	klog.V(2).InfoS("manifest patch succeeded", "gvr", gvr, "manifest", manifestRef)
	return manifestObj, ManifestThreeWayMergePatchAction, nil
}

// generateWorkCondition constructs the work condition based on the apply result
func (r *ApplyWorkReconciler) generateWorkCondition(results []applyResult, work *fleetv1beta1.Work) []error {
	var errs []error
	// Update manifestCondition based on the results.
	manifestConditions := make([]fleetv1beta1.ManifestCondition, len(results))
	for index, result := range results {
		if result.err != nil {
			errs = append(errs, result.err)
		}
		appliedCondition := buildManifestAppliedCondition(result.err, result.action, result.generation)
		manifestCondition := fleetv1beta1.ManifestCondition{
			Identifier: result.identifier,
			Conditions: []metav1.Condition{appliedCondition},
		}
		foundmanifestCondition := findManifestConditionByIdentifier(result.identifier, work.Status.ManifestConditions)
		if foundmanifestCondition != nil {
			manifestCondition.Conditions = foundmanifestCondition.Conditions
			meta.SetStatusCondition(&manifestCondition.Conditions, appliedCondition)
		}
		manifestConditions[index] = manifestCondition
	}

	work.Status.ManifestConditions = manifestConditions
	workCond := generateWorkAppliedCondition(manifestConditions, work.Generation)
	work.Status.Conditions = []metav1.Condition{workCond}
	return errs
}

// Join starts to reconcile
func (r *ApplyWorkReconciler) Join(_ context.Context) error {
	if !r.joined.Load() {
		klog.InfoS("mark the apply work reconciler joined")
	}
	r.joined.Store(true)
	return nil
}

// Leave start
func (r *ApplyWorkReconciler) Leave(ctx context.Context) error {
	var works fleetv1beta1.WorkList
	if r.joined.Load() {
		klog.InfoS("mark the apply work reconciler left")
	}
	r.joined.Store(false)
	// list all the work object we created in the member cluster namespace
	listOpts := []client.ListOption{
		client.InNamespace(r.workNameSpace),
	}
	if err := r.client.List(ctx, &works, listOpts...); err != nil {
		klog.ErrorS(err, "failed to list all the work object", "clusterNS", r.workNameSpace)
		return client.IgnoreNotFound(err)
	}
	// we leave the resources on the member cluster for now
	for _, work := range works.Items {
		staleWork := work.DeepCopy()
		if controllerutil.ContainsFinalizer(staleWork, fleetv1beta1.WorkFinalizer) {
			controllerutil.RemoveFinalizer(staleWork, fleetv1beta1.WorkFinalizer)
			if updateErr := r.client.Update(ctx, staleWork, &client.UpdateOptions{}); updateErr != nil {
				klog.ErrorS(updateErr, "failed to remove the work finalizer from the work",
					"clusterNS", r.workNameSpace, "work", klog.KObj(staleWork))
				return updateErr
			}
		}
	}
	klog.V(2).InfoS("successfully removed all the work finalizers in the cluster namespace",
		"clusterNS", r.workNameSpace, "number of work", len(works.Items))
	return nil
}

// SetupWithManager wires up the controller.
func (r *ApplyWorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
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

func buildManifestAppliedCondition(err error, action applyAction, observedGeneration int64) metav1.Condition {
	if err != nil {
		return metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: observedGeneration,
			LastTransitionTime: metav1.Now(),
			Reason:             AppliedManifestFailedReason,
			Message:            fmt.Sprintf("Failed to apply manifest: %v", err),
		}
	}

	return metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeApplied,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: observedGeneration,
		Reason:             string(action),
		Message:            string(action),
	}
}

// generateWorkAppliedCondition generate applied status condition for work.
// If one of the manifests is applied failed on the spoke, the applied status condition of the work is false.
func generateWorkAppliedCondition(manifestConditions []fleetv1beta1.ManifestCondition, observedGeneration int64) metav1.Condition {
	for _, manifestCond := range manifestConditions {
		if meta.IsStatusConditionFalse(manifestCond.Conditions, fleetv1beta1.WorkConditionTypeApplied) {
			return metav1.Condition{
				Type:               fleetv1beta1.WorkConditionTypeApplied,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             AppliedWorkFailedReason,
				Message:            "Failed to apply work",
				ObservedGeneration: observedGeneration,
			}
		}
	}

	return metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeApplied,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             AppliedWorkCompleteReason,
		Message:            "Apply work complete",
		ObservedGeneration: observedGeneration,
	}
}
