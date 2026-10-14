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

package resourceeligibility

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

const (
	entrySeparator      = ";"
	gvkSegmentSeparator = "/"
	kindSeparator       = ","

	// coreAPIVersion is the API version (v1) of the core API group.
	coreAPIVersion = "v1"
)

// ParseGVKs parses a semicolon-separated list of GVK strings for matching purposes, e.g.,
// "apps/v1/Deployment;v1/ConfigMap".
//
// An empty input yields no GVKs. Surrounding whitespace on each entry is ignored.
//
// See the comments on the ParseOneGVK function for the accepted forms of each GVK entry.
func ParseGVKs(input string) ([]schema.GroupVersionKind, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	entries := strings.Split(input, entrySeparator)
	gvks := make([]schema.GroupVersionKind, 0, len(entries))
	for _, entry := range entries {
		entryGVKs, err := ParseOneGVK(entry)
		if err != nil {
			return nil, err
		}
		gvks = append(gvks, entryGVKs...)
	}
	return gvks, nil
}

// ParseOneGVK parses a single GVK string entry into one or more GVKs for matching purposes. The input
// should be in one of the following forms:
//
// * "[API-GROUP]", e.g., "apps";
// * "[API-GROUP]/[API-VERSION]", e.g., "apps/v1";
// * "[API-GROUP]/[API-VERSION]/[KIND]", e.g., "apps/v1/Deployment";
// * "[API-GROUP]/[API-VERSION]/[KINDS]", e.g., "apps/v1/Deployment,StatefulSet";
//
// For the core API group, which always has an empty group name, the input should be in one of the following forms:
// * "v1", e.g., "v1";
// * "v1/[KIND]", e.g., "v1/ConfigMap";
// * "v1/[KINDS]", e.g., "v1/ConfigMap,Secret";
//
// The API version or kind segments can be left empty or set to the wildcard value "*" to match with any value.
// The API group segment, however, must name a specific group. In addition, when multiple kinds are specified,
// none of them can be empty, e.g., "apps/v1/Deployment," is invalid.
//
// Surrounding whitespace on each segment and kind is ignored.
func ParseOneGVK(input string) ([]schema.GroupVersionKind, error) {
	segments := strings.Split(input, gvkSegmentSeparator)
	for i := range segments {
		segments[i] = strings.TrimSpace(segments[i])
	}

	// The core API group has an empty group name, so its entries start with the version.
	if segments[0] == coreAPIVersion {
		switch len(segments) {
		case 1:
			return []schema.GroupVersionKind{{Version: coreAPIVersion}}, nil
		case 2:
			kinds := strings.Split(segments[1], kindSeparator)
			gvks := make([]schema.GroupVersionKind, 0, len(kinds))
			for _, kind := range kinds {
				kind = strings.TrimSpace(kind)
				if kind == "" && len(kinds) > 1 {
					return nil, errors.NewUserError(nil, "kind cannot be empty", "input", input, "kinds", kinds)
				}
				gvks = append(gvks, schema.GroupVersionKind{Version: coreAPIVersion, Kind: kind})
			}
			return gvks, nil
		default:
			return nil, errors.NewUserError(nil, "core API group entry must be in the form of v1 or v1/<kind>[,<kind>...]", "input", input)
		}
	}

	if segments[0] == "" || segments[0] == wildcard {
		return nil, errors.NewUserError(nil, "the API group segment cannot be empty or a wildcard", "input", input)
	}

	switch len(segments) {
	case 1:
		return []schema.GroupVersionKind{{Group: segments[0]}}, nil
	case 2:
		return []schema.GroupVersionKind{{Group: segments[0], Version: segments[1]}}, nil
	case 3:
		kinds := strings.Split(segments[2], kindSeparator)
		gvks := make([]schema.GroupVersionKind, 0, len(kinds))
		for _, kind := range kinds {
			kind = strings.TrimSpace(kind)
			if kind == "" && len(kinds) > 1 {
				return nil, errors.NewUserError(nil, "kind cannot be empty", "input", input, "kinds", kinds)
			}
			gvks = append(gvks, schema.GroupVersionKind{Group: segments[0], Version: segments[1], Kind: kind})
		}
		return gvks, nil
	default:
		return nil, errors.NewUserError(nil, "entry must be in the form of <group>, <group>/<version>, or <group>/<version>/<kind>[,<kind>...]", "input", input)
	}
}
