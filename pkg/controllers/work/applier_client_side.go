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

package work

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// ClientSideApplier applies the manifest to the cluster and fails if the resource already exists.
type ClientSideApplier struct {
	HubClient          client.Client
	WorkNamespace      string
	SpokeDynamicClient dynamic.Interface
}

// ApplyUnstructured determines if an unstructured manifest object can & should be applied. It first validates
// the size of the last modified annotation of the manifest, it removes the annotation if the size crosses the annotation size threshold
// and then creates/updates the resource on the cluster using server side apply instead of three-way merge patch.
func (applier *ClientSideApplier) ApplyUnstructured(ctx context.Context, applyStrategy *fleetv1beta1.ApplyStrategy, gvr schema.GroupVersionResource, manifestObj *unstructured.Unstructured) (*unstructured.Unstructured, ApplyAction, error) {
	manifestRef := klog.KObj(manifestObj)

	// compute the hash without taking into consider the last applied annotation
	if err := setManifestHashAnnotation(manifestObj); err != nil {
		return nil, errorApplyAction, controller.NewUnexpectedBehaviorError(err)
	}

	// extract the common create procedure to reuse
	var createFunc = func() (*unstructured.Unstructured, ApplyAction, error) {
		// record the raw manifest with the hash annotation in the manifest
		if _, err := setModifiedConfigurationAnnotation(manifestObj); err != nil {
			return nil, errorApplyAction, controller.NewUnexpectedBehaviorError(err)
		}
		actual, err := applier.SpokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).Create(
			ctx, manifestObj, metav1.CreateOptions{FieldManager: workFieldManagerName})
		if err == nil {
			klog.V(2).InfoS("successfully created the manifest", "gvr", gvr, "manifest", manifestRef)
			return actual, manifestCreatedAction, nil
		}
		return nil, errorApplyAction, controller.NewAPIServerError(false, err)
	}

	// support resources with generated name
	if manifestObj.GetName() == "" && manifestObj.GetGenerateName() != "" {
		klog.V(2).InfoS("Create the resource with generated name regardless", "gvr", gvr, "manifest", manifestRef)
		return createFunc()
	}

	// get the current object and create one if not found
	curObj, err := applier.SpokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).Get(ctx, manifestObj.GetName(), metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		return createFunc()
	case err != nil:
		return nil, errorApplyAction, controller.NewAPIServerError(false, err)
	}

	result, err := validateOwnerReference(ctx, applier.HubClient, applier.WorkNamespace, applyStrategy, curObj.GetOwnerReferences())
	if err != nil {
		klog.ErrorS(err, "Skip applying a manifest", "result", result,
			"gvr", gvr, "manifest", manifestRef, "applyStrategy", applyStrategy, "ownerReferences", curObj.GetOwnerReferences())
		return nil, result, err
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
			return serverSideApply(ctx, applier.SpokeDynamicClient, true, gvr, manifestObj)
		}
		klog.V(2).InfoS("Using three way merge for manifest", "gvr", gvr, "manifest", manifestRef)
		return applier.patchCurrentResource(ctx, gvr, manifestObj, curObj)
	}

	return curObj, errorApplyAction, nil
}

// patchCurrentResource uses three-way merge to patch the current resource with the new manifest we get from the work.
func (applier *ClientSideApplier) patchCurrentResource(ctx context.Context, gvr schema.GroupVersionResource,
	manifestObj, curObj *unstructured.Unstructured) (*unstructured.Unstructured, ApplyAction, error) {
	manifestRef := klog.KObj(manifestObj)
	klog.V(2).InfoS("Manifest is modified", "gvr", gvr, "manifest", manifestRef,
		"new hash", manifestObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation],
		"existing hash", curObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation])
	// create the three-way merge patch between the current, original and manifest similar to how kubectl apply does
	patch, err := threeWayMergePatch(curObj, manifestObj)
	if err != nil {
		klog.ErrorS(err, "Failed to generate the three way patch", "gvr", gvr, "manifest", manifestRef)
		return nil, errorApplyAction, controller.NewUnexpectedBehaviorError(err)
	}
	data, err := patch.Data(manifestObj)
	if err != nil {
		klog.ErrorS(err, "Failed to generate the three way patch", "gvr", gvr, "manifest", manifestRef)
		return nil, errorApplyAction, controller.NewUnexpectedBehaviorError(err)
	}
	// Use three-way merge (similar to kubectl client side apply) to the patch to the member cluster
	manifestObj, patchErr := applier.SpokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).
		Patch(ctx, manifestObj.GetName(), patch.Type(), data, metav1.PatchOptions{FieldManager: workFieldManagerName})
	if patchErr != nil {
		klog.ErrorS(patchErr, "Failed to patch the manifest", "gvr", gvr, "manifest", manifestRef)
		return nil, errorApplyAction, controller.NewAPIServerError(false, patchErr)
	}
	klog.V(2).InfoS("Manifest patch succeeded", "gvr", gvr, "manifest", manifestRef)
	return manifestObj, manifestThreeWayMergePatchAction, nil
}
