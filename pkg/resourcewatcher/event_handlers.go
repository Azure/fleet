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
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
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

// The next three are for the ClusterResourcePlacement informer
// onClusterResourcePlacementAdded handles object add event and push the placement to the cluster placement queue.
func (d *ChangeDetector) onClusterResourcePlacementAdded(obj interface{}) {
	placementMeta, _ := meta.Accessor(obj)
	klog.V(3).InfoS("ClusterResourcePlacement Added", "placement", klog.KObj(placementMeta))
	d.ClusterResourcePlacementControllerV1Alpha1.Enqueue(obj)
}

// onClusterResourcePlacementUpdated handles object update event and push the placement to the cluster placement queue.
func (d *ChangeDetector) onClusterResourcePlacementUpdated(oldObj, newObj interface{}) {
	oldPlacementMeta, _ := meta.Accessor(oldObj)
	newPlacementMeta, _ := meta.Accessor(newObj)
	if oldPlacementMeta.GetGeneration() == newPlacementMeta.GetGeneration() {
		klog.V(4).InfoS("ignore a cluster resource placement update event with no spec change",
			"placement", klog.KObj(oldPlacementMeta))
		return
	}
	klog.V(3).InfoS("ClusterResourcePlacement Updated",
		"placement", klog.KObj(oldPlacementMeta))
	d.ClusterResourcePlacementControllerV1Alpha1.Enqueue(newObj)
}

// onClusterResourcePlacementDeleted handles object delete event and push the placement to the cluster placement queue.
func (d *ChangeDetector) onClusterResourcePlacementDeleted(obj interface{}) {
	clientObj, err := handleTombStoneObj(obj)
	if err != nil {
		klog.ErrorS(err, "failed to handle a cluster resource placement object delete event")
	}
	klog.V(3).InfoS("a clusterResourcePlacement is deleted", "placement", klog.KObj(clientObj))
	d.ClusterResourcePlacementControllerV1Alpha1.Enqueue(clientObj)
}

// The next two are for the Work informer, we don't handle add event as placement reconciler creates the work
// onWorkUpdated handles object update event and push the corresponding placements to the cluster placement queue.
func (d *ChangeDetector) onWorkUpdated(oldObj, newObj interface{}) {
	oldWorkMeta, _ := meta.Accessor(oldObj)
	newWorkMeta, _ := meta.Accessor(newObj)
	if oldWorkMeta.GetResourceVersion() == newWorkMeta.GetResourceVersion() {
		return
	}
	// we never change the placement label of a work
	if placementName, exist := oldWorkMeta.GetLabels()[utils.LabelWorkPlacementName]; exist {
		klog.V(3).InfoS("a work object is updated, will enqueue a placement event", "work", klog.KObj(oldWorkMeta), "placement", placementName)
		// the meta key function handles string
		d.ClusterResourcePlacementControllerV1Alpha1.Enqueue(placementName)
	} else {
		klog.V(4).InfoS("ignore an updated work object without a placement label", "work", klog.KObj(oldWorkMeta))
	}
}

// onWorkDeleted handles object delete event and push the corresponding placements to the cluster placement queue.
func (d *ChangeDetector) onWorkDeleted(obj interface{}) {
	clientObj, err := handleTombStoneObj(obj)
	if err != nil {
		klog.ErrorS(err, "failed to handle a work object delete event")
		return
	}
	if placementName, exist := clientObj.GetLabels()[utils.LabelWorkPlacementName]; exist {
		klog.V(3).InfoS("a work object is deleted", "work", klog.KObj(clientObj), "placement", placementName)
		// the meta key function handles string
		d.ClusterResourcePlacementControllerV1Alpha1.Enqueue(placementName)
	} else {
		klog.V(4).InfoS("ignore a deleted work object without a placement label", "work", klog.KObj(clientObj))
	}
}

// The next one is for the memberCluster informer
// onMemberClusterUpdated handles object update event and push the memberCluster name to the memberCluster controller queue.
func (d *ChangeDetector) onMemberClusterUpdated(oldObj, newObj interface{}) {
	var oldMC, newMC fleetv1alpha1.MemberCluster
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(oldObj.(*unstructured.Unstructured).Object, &oldMC)
	if err != nil {
		// should not happen
		klog.ErrorS(err, "failed to handle a member cluster object update event")
		return
	}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(newObj.(*unstructured.Unstructured).Object, &newMC)
	if err != nil {
		klog.ErrorS(err, "failed to handle a member cluster object update event")
		return
	}
	// Only enqueue if the change can affect placement decisions. i.e. label and spec and work agent condition
	if oldMC.GetGeneration() == newMC.GetGeneration() &&
		reflect.DeepEqual(oldMC.GetLabels(), newMC.GetLabels()) {
		var oldAgentCond, newAgentCond []metav1.Condition
		for _, agentStatus := range oldMC.Status.AgentStatus {
			if agentStatus.Type == fleetv1alpha1.MemberAgent {
				oldAgentCond = agentStatus.Conditions
			}
		}
		for _, agentStatus := range newMC.Status.AgentStatus {
			if agentStatus.Type == fleetv1alpha1.MemberAgent {
				newAgentCond = agentStatus.Conditions
			}
		}
		if reflect.DeepEqual(oldAgentCond, newAgentCond) {
			klog.V(4).InfoS("ignore a memberCluster update event with no real change",
				"memberCluster", klog.KObj(&oldMC), "generation", oldMC.GetGeneration())
			return
		}
	}

	klog.V(3).InfoS("a memberCluster is updated", "memberCluster", klog.KObj(&oldMC))
	d.MemberClusterPlacementController.Enqueue(oldObj)
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
