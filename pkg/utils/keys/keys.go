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

package keys

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// ClusterWideKey is the object key which is a unique identifier under a cluster, across all resources.
type ClusterWideKey struct {
	placementv1beta1.ResourceIdentifier
}

// String returns the key's printable info with format:
// "<GroupVersion>, kind=<Kind>, <NamespaceKey>"
func (k ClusterWideKey) String() string {
	return fmt.Sprintf("%s, kind=%s, %s", k.GroupVersion().String(), k.Kind, k.NamespaceKey())
}

// NamespaceKey returns the traditional key of an object.
func (k *ClusterWideKey) NamespaceKey() string {
	if len(k.Namespace) > 0 {
		return k.Namespace + "/" + k.Name
	}

	return k.Name
}

// GroupVersionKind returns the group, version, and kind of resource being referenced.
func (k *ClusterWideKey) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   k.Group,
		Version: k.Version,
		Kind:    k.Kind,
	}
}

// GroupVersion returns the group and version of resource being referenced.
func (k *ClusterWideKey) GroupVersion() schema.GroupVersion {
	return schema.GroupVersion{
		Group:   k.Group,
		Version: k.Version,
	}
}

// GroupKind returns the group and kind of resource being referenced.
func (k *ClusterWideKey) GroupKind() schema.GroupKind {
	return schema.GroupKind{
		Group: k.Group,
		Kind:  k.Kind,
	}
}

// GetClusterWideKeyForObject generates a ClusterWideKey for object.
func GetClusterWideKeyForObject(obj interface{}) (ClusterWideKey, error) {
	key := ClusterWideKey{}

	runtimeObject, ok := obj.(runtime.Object)
	if !ok { // should not happen
		return key, fmt.Errorf("object %+v is not a runtime object", obj)
	}
	object := runtimeObject.DeepCopyObject()
	gvk := object.GetObjectKind().GroupVersionKind()

	metaInfo, err := meta.Accessor(object)
	if err != nil { // should not happen
		return key, fmt.Errorf("object %s has no meta: %w", gvk.String(), err)
	}

	key.Group = gvk.Group
	key.Version = gvk.Version
	key.Kind = gvk.Kind
	key.Namespace = metaInfo.GetNamespace()
	key.Name = metaInfo.GetName()

	return key, nil
}

// GetNamespaceKeyForObject generates a namespace/name key for object.
func GetNamespaceKeyForObject(obj interface{}) (string, error) {
	if key, ok := obj.(string); ok {
		return key, nil
	}
	metaInfo, err := meta.Accessor(obj)
	if err != nil { // should not happen
		return "", fmt.Errorf("object %+v has no meta: %w", obj, err)
	}
	if len(metaInfo.GetNamespace()) > 0 {
		return metaInfo.GetNamespace() + "/" + metaInfo.GetName(), nil
	}
	return metaInfo.GetName(), nil
}
