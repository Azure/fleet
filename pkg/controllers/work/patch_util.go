package controllers

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/mergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var builtinScheme = runtime.NewScheme()
var metadataAccessor = meta.NewAccessor()

func init() {
	// we use this trick to check if a resource is k8s built-in
	_ = clientgoscheme.AddToScheme(builtinScheme)
}

// threeWayMergePatch creates a patch by computing a three-way diff based on
// an object's current state, modified state, and last-applied-state recorded in its annotation.
func threeWayMergePatch(currentObj, manifestObj client.Object) (client.Patch, error) {
	//TODO: see if we should use something like json.ConfigCompatibleWithStandardLibrary.Marshal to make sure that
	// the json we created is compatible with the format that json merge patch requires.
	current, err := json.Marshal(currentObj)
	if err != nil {
		return nil, err
	}
	original, err := getOriginalConfiguration(currentObj)
	if err != nil {
		return nil, err
	}
	manifest, err := json.Marshal(manifestObj)
	if err != nil {
		return nil, err
	}

	var patchType types.PatchType
	var patchData []byte
	var lookupPatchMeta strategicpatch.LookupPatchMeta

	versionedObject, err := builtinScheme.New(currentObj.GetObjectKind().GroupVersionKind())
	switch {
	case runtime.IsNotRegisteredError(err):
		// use JSONMergePatch for custom resources
		// because StrategicMergePatch doesn't support custom resources
		patchType = types.MergePatchType
		preconditions := []mergepatch.PreconditionFunc{
			mergepatch.RequireKeyUnchanged("apiVersion"),
			mergepatch.RequireKeyUnchanged("kind"),
			mergepatch.RequireMetadataKeyUnchanged("name")}
		patchData, err = jsonmergepatch.CreateThreeWayJSONMergePatch(original, manifest, current, preconditions...)
		if err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		// use StrategicMergePatch for K8s built-in resources
		patchType = types.StrategicMergePatchType
		lookupPatchMeta, err = strategicpatch.NewPatchMetaFromStruct(versionedObject)
		if err != nil {
			return nil, err
		}
		patchData, err = strategicpatch.CreateThreeWayMergePatch(original, manifest, current, lookupPatchMeta, true)
		if err != nil {
			return nil, err
		}
	}
	return client.RawPatch(patchType, patchData), nil
}

// setModifiedConfigurationAnnotation serializes the object into byte stream.
// If `updateAnnotation` is true, it embeds the result as an annotation in the
// modified configuration. If annotations size is greater than 256 kB it sets
// to empty string. it returns true if the annotation is set returns false if
// the annotation is set to an empty string.
func setModifiedConfigurationAnnotation(obj runtime.Object) (bool, error) {
	var modified []byte
	annotations, err := metadataAccessor.Annotations(obj)
	if err != nil {
		return false, fmt.Errorf("cannot access metadata.annotations: %w", err)
	}
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// remove the annotation to avoid recursion
	delete(annotations, lastAppliedConfigAnnotation)
	// do not include an empty map
	if len(annotations) == 0 {
		_ = metadataAccessor.SetAnnotations(obj, nil)
	} else {
		_ = metadataAccessor.SetAnnotations(obj, annotations)
	}

	//TODO: see if we should use something like json.ConfigCompatibleWithStandardLibrary.Marshal to make sure that
	// the produced json format is more three way merge friendly
	modified, err = json.Marshal(obj)
	if err != nil {
		return false, err
	}
	// set the last applied annotation back
	annotations[lastAppliedConfigAnnotation] = string(modified)
	if err := validation.ValidateAnnotationsSize(annotations); err != nil {
		klog.V(2).InfoS(fmt.Sprintf("setting last applied config annotation to empty, %s", err))
		annotations[lastAppliedConfigAnnotation] = ""
		return false, metadataAccessor.SetAnnotations(obj, annotations)
	}
	return true, metadataAccessor.SetAnnotations(obj, annotations)
}

// getOriginalConfiguration gets original configuration of the object
// form the annotation, or return an error if no annotation found.
func getOriginalConfiguration(obj runtime.Object) ([]byte, error) {
	annots, err := metadataAccessor.Annotations(obj)
	if err != nil {
		klog.ErrorS(err, "cannot access metadata.annotations", "gvk", obj.GetObjectKind().GroupVersionKind())
		return nil, err
	}
	// The func threeWayMergePatch can handle the case that the original is empty.
	if annots == nil {
		klog.Warning("object does not have annotation", "obj", obj)
		return nil, nil
	}
	original, ok := annots[lastAppliedConfigAnnotation]
	if !ok {
		klog.Warning("object does not have lastAppliedConfigAnnotation", "obj", obj)
		return nil, nil
	}
	return []byte(original), nil
}
