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

package admissionpolicymanager

import (
	"context"
	"regexp"
	"strings"

	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// illegalCELStringChars is a string of characters that should not be used in CEL string literals.
	illegalCELStringChars = `'"\`
)

var (
	// reservedNamespacePrefixRegexp matches valid namespace prefix characters (DNS label subset).
	reservedNamespacePrefixRegexp = regexp.MustCompile(`^[a-z0-9-]+$`)
)

var (
	managedByAndPartOfKubeFleetLabelSelector = client.MatchingLabels{
		VAPManagedByKubeFleetLabelKey: VAPManagedByKubeFleetLabelValue,
		VAPPartOfKubeFleetLabelKey:    VAPPartOfKubeFleetLabelValue,
		VAPComponentKubeFleetLabelKey: VAPComponentAdmissionPolicyManagerLabelValue,
	}
)

var (
	// buildRetryUnlessCtxErr returns a function that is used with retry.OnError to stop
	// retrying if the parent context has been cancelled or is erred.
	buildRetryUnlessCtxErr = func(ctx context.Context) func(error) bool {
		return func(err error) bool {
			if err := ctx.Err(); err != nil {
				return false
			}
			return true
		}
	}
)

// addManagedByPartOfAndComponentLabels adds labels to the given object to indicate that it is managed by
// KubeFleet and belongs to the admission policy manager component.
func addManagedByPartOfAndComponentLabels(obj metav1.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	labels[VAPManagedByKubeFleetLabelKey] = VAPManagedByKubeFleetLabelValue
	labels[VAPPartOfKubeFleetLabelKey] = VAPPartOfKubeFleetLabelValue
	labels[VAPComponentKubeFleetLabelKey] = VAPComponentAdmissionPolicyManagerLabelValue
	obj.SetLabels(labels)
}

// validateCELStringLiterals checks if any of the provided strings contains characters that are illegal
// in CEL string literals (e.g., backslash, double quotes, and single quotes).
func validateCELStringLiterals(strs ...string) error {
	for _, str := range strs {
		if strings.ContainsAny(str, illegalCELStringChars) {
			return errors.NewUserError(nil, "string literal contains illegal characters for a CEL expression", "value", str)
		}
	}
	return nil
}
