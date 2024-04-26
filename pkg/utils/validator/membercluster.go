package validator

import (
	"fmt"

	apiErrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

var (
	invalidTaintKeyErrFmt   = "invalid taint key %+v: %s"
	invalidTaintValueErrFmt = "invalid taint value %+v: %s"
	uniqueTaintErrFmt       = "taint %+v already exists, taints must be unique"
)

// ValidateMemberCluster validates member cluster fields and returns error.
func ValidateMemberCluster(mc clusterv1beta1.MemberCluster) error {
	return validateTaints(mc.Spec.Taints)
}

func validateTaints(taints []clusterv1beta1.Taint) error {
	allErr := make([]error, 0)
	taintMap := make(map[clusterv1beta1.Taint]bool)
	for _, taint := range taints {
		for _, msg := range validation.IsQualifiedName(taint.Key) {
			allErr = append(allErr, fmt.Errorf(invalidTaintKeyErrFmt, taint, msg))
		}
		if taint.Value != "" {
			for _, msg := range validation.IsValidLabelValue(taint.Value) {
				allErr = append(allErr, fmt.Errorf(invalidTaintValueErrFmt, taint, msg))
			}
		}
		if taintMap[taint] {
			allErr = append(allErr, fmt.Errorf(uniqueTaintErrFmt, taint))
		}
		taintMap[taint] = true
	}
	return apiErrors.NewAggregate(allErr)
}
