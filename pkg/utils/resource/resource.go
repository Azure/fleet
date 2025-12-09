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

// Package resource defines common utils for working with kubernetes resources.
package resource

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// etcd has a 1.5 MiB limit for objects by default, and Kubernetes clients might
	// reject request entities too large (~2/~3 MiB, depending on the protocol in use).
	DefaultObjSizeLimitWithPaddingBytes = 1415578 // 1.35 MiB, or ~1.42 MB.
)

// HashOf returns the hash of the resource.
func HashOf(resource any) (string, error) {
	jsonBytes, err := json.Marshal(resource)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(jsonBytes)), nil
}

// CalculateSizeDeltaOverLimitFor calculates the size delta in bytes of a given object
// over a specified size limit. It returns a positive value if the object size exceeds
// the limit or a negative value if the object size is below the limit.
//
// This utility is useful in cases where KubeFleet needs to check if it can create/update
// an object with additional information.
func CalculateSizeDeltaOverLimitFor(obj runtime.Object, sizeLimitBytes int) (int, error) {
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return 0, fmt.Errorf("cannot determine object size: %w", err)
	}
	if sizeLimitBytes < 0 {
		return 0, fmt.Errorf("size limit must be non-negative")
	}
	return len(jsonBytes) - sizeLimitBytes, nil
}
