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

package resourcewatcher

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// handleTombStoneObj handles the case that the delete object is a tombStone instead of the real object
func handleTombStoneObj(obj interface{}) (client.Object, error) {
	if clientObj, ok := obj.(client.Object); ok {
		return clientObj, nil
	}

	// If the object doesn't have Metadata, assume it is a tombstone object of type DeletedFinalStateUnknown
	tombstone, isTombStone := obj.(cache.DeletedFinalStateUnknown)
	if !isTombStone {
		return nil, fmt.Errorf("encountered an unknown deleted object %+v", obj)
	}
	// Pull Object out of the tombstone
	if clientObj, ok := tombstone.Obj.(client.Object); ok {
		return clientObj, nil
	}
	return nil, fmt.Errorf("encountered an known tombstone object %+v", tombstone)
}

// The next three are for any dynamic resource informer
// onResourceAdded handles object add event and push the new object to the resource queue.
func (d *ChangeDetector) onResourceAdded(obj interface{}) {
	runtimeObject, ok := obj.(runtime.Object)
	if !ok {
		klog.ErrorS(fmt.Errorf("resource %+v is not a runtime object", obj), "skip process an unknown obj")
		return
	}
	metaInfo, err := meta.Accessor(runtimeObject)
	if err != nil {
		klog.ErrorS(err, "skip process an unknown obj", "gvk", runtimeObject.GetObjectKind().GroupVersionKind().String())
		return
	}
	klog.V(3).InfoS("A resource is added", "obj", klog.KObj(metaInfo),
		"gvk", runtimeObject.GetObjectKind().GroupVersionKind().String())
	d.ResourceChangeController.Enqueue(obj)
}

// onResourceUpdated handles object update event and push the updated object to the resource queue.
func (d *ChangeDetector) onResourceUpdated(oldObj, newObj interface{}) {
	oldObjMeta, err := meta.Accessor(oldObj)
	if err != nil {
		klog.ErrorS(err, "failed to handle an object update event", "oldObj", oldObj)
		return
	}
	newObjMeta, err := meta.Accessor(newObj)
	if err != nil {
		klog.ErrorS(err, "failed to handle an object update event", "newObj", newObj)
		return
	}
	runtimeObject, ok := oldObj.(runtime.Object)
	if !ok {
		klog.ErrorS(fmt.Errorf("resource %+v is not a runtime object", oldObj), "skip process an unknown obj")
		return
	}
	if oldObjMeta.GetResourceVersion() != newObjMeta.GetResourceVersion() {
		klog.V(3).InfoS("A resource is updated", "obj", oldObjMeta.GetName(),
			"namespace", oldObjMeta.GetNamespace(), "gvk", runtimeObject.GetObjectKind().GroupVersionKind().String())
		d.ResourceChangeController.Enqueue(newObj)
		return
	}
	klog.V(4).InfoS("Received a resource updated event with no change", "obj", oldObjMeta.GetName(),
		"namespace", oldObjMeta.GetNamespace(), "gvk", runtimeObject.GetObjectKind().GroupVersionKind().String())
}

// onResourceDeleted handles object delete event and push the deleted object to the resource queue.
func (d *ChangeDetector) onResourceDeleted(obj interface{}) {
	clientObj, err := handleTombStoneObj(obj)
	if err != nil {
		klog.ErrorS(err, "failed to handle an object delete event")
		return
	}
	klog.V(3).InfoS("A resource is deleted", "obj", klog.KObj(clientObj), "gvk", clientObj.GetObjectKind().GroupVersionKind().String())
	d.ResourceChangeController.Enqueue(clientObj)
}
