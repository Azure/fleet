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

package placementresourcesnapshot

import (
	"crypto/sha256"
	"fmt"
	"strconv"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	errors "go.goms.io/fleet/pkg/utils/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// A limit of 61 is set here; Kubernetes labels accept values of up to 63 characters, and
	// KubeFleet reserves 2 more characters as a buffer.
	placementPolicyOwnerLabelValLenLimit = 61
	placementPolicyOwnerLabelHashLen     = 12
)

func primaryPlacementResourceSnapshot(
	namespace, name string,
	ownerPlacementPolicy placementv1alpha1.PlacementPolicyAccessor,
	idx int,
	resources []placementv1alpha1.SnapshottedResource,
	resourceHash string,
	snapshotCount int,
	scheme *runtime.Scheme,
) (placementv1alpha1.PlacementResourceSnapshotAccessor, error) {
	labels := map[string]string{
		placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyOwnerLabelVal(ownerPlacementPolicy),
		placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           strconv.Itoa(idx),
		placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
		placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: strconv.Itoa(snapshotCount),
	}
	annotations := map[string]string{
		placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: resourceHash,
	}

	var primarySnapshot placementv1alpha1.PlacementResourceSnapshotAccessor
	if namespace == "" {
		primarySnapshot = &placementv1alpha1.ClusterPlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: placementv1alpha1.PlacementResourceSnapshotSpec{
				Resources: resources,
			},
		}
	} else {
		primarySnapshot = &placementv1alpha1.PlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   namespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: placementv1alpha1.PlacementResourceSnapshotSpec{
				Resources: resources,
			},
		}
	}

	if err := controllerutil.SetControllerReference(ownerPlacementPolicy, primarySnapshot, scheme); err != nil {
		return nil, errors.NewUnexpectedError(err, "failed to set controller reference on the primary placement resource snapshot",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot))
	}
	return primarySnapshot, nil
}

func secondaryPlacementResourceSnapshot(
	namespace, name string,
	ownerPlacementPolicy placementv1alpha1.PlacementPolicyAccessor,
	idx, subIdx int,
	resources []placementv1alpha1.SnapshottedResource,
	resourceHash string,
	scheme *runtime.Scheme,
) (placementv1alpha1.PlacementResourceSnapshotAccessor, error) {
	labels := map[string]string{
		placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyOwnerLabelVal(ownerPlacementPolicy),
		placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    strconv.Itoa(idx),
		placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: strconv.Itoa(subIdx),
	}
	annotations := map[string]string{
		placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: resourceHash,
	}

	var secondarySnapshot placementv1alpha1.PlacementResourceSnapshotAccessor
	if namespace == "" {
		secondarySnapshot = &placementv1alpha1.ClusterPlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: placementv1alpha1.PlacementResourceSnapshotSpec{
				Resources: resources,
			},
		}
	} else {
		secondarySnapshot = &placementv1alpha1.PlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   namespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: placementv1alpha1.PlacementResourceSnapshotSpec{
				Resources: resources,
			},
		}
	}

	if err := controllerutil.SetControllerReference(ownerPlacementPolicy, secondarySnapshot, scheme); err != nil {
		return nil, errors.NewUnexpectedError(err, "failed to set controller reference on a secondary placement resource snapshot",
			"secondaryPlacementResourceSnapshot", klog.KObj(secondarySnapshot))
	}
	return secondarySnapshot, nil
}

// placementPolicyOwnerLabelVal returns the value for the PlacementResourceSnapshotOwnedByLabelKey label.
//
// If the placement policy's name does not exceed the length limit, the label value is simply the name itself.
// Otherwise, the name is truncated and appended with a hash suffix to ensure uniqueness.
func placementPolicyOwnerLabelVal(placementPolicy placementv1alpha1.PlacementPolicyAccessor) string {
	name := placementPolicy.GetName()
	if len(name) <= placementPolicyOwnerLabelValLenLimit {
		return name
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(name)))[:placementPolicyOwnerLabelHashLen]
	prefixLen := placementPolicyOwnerLabelValLenLimit - placementPolicyOwnerLabelHashLen - 1
	return fmt.Sprintf("%s-%s", name[:prefixLen], hash)
}
