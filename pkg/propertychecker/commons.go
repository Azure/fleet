//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

package propertychecker

import (
	"fmt"
	"strconv"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// Common error messages and validation patterns used across property checkers.
const (
	// ErrUnsupportedOperatorTemplate is a template for unsupported operator errors.
	ErrUnsupportedOperatorTemplate = "unsupported operator %q for property %q"

	// ErrInvalidValueCountTemplate is a template for invalid value count errors.
	ErrInvalidValueCountTemplate = "property %q must have exactly %d value(s), got %d"

	// ErrInvalidValueTemplate is a template for invalid value format errors.
	ErrInvalidValueTemplate = "invalid value %q for property %q: %w"
)

// ValidateSingleStringValue validates that a requirement has exactly one string value and returns it.
func ValidateSingleStringValue(req placementv1beta1.PropertySelectorRequirement) (string, error) {
	// Check that the requirement has exactly one value.
	if len(req.Values) != 1 {
		return "", fmt.Errorf(ErrInvalidValueCountTemplate, req.Name, 1, len(req.Values))
	}
	return req.Values[0], nil
}

// ValidateSingleUint32Value validates that a requirement has exactly one valid uint32 value and returns it.
func ValidateSingleUint32Value(req placementv1beta1.PropertySelectorRequirement) (uint32, error) {
	value, err := ValidateSingleStringValue(req)
	if err != nil {
		return 0, err
	}

	// Parse as uint64 first to handle overflow properly
	valueUint, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf(ErrInvalidValueTemplate, value, req.Name, err)
	}

	return uint32(valueUint), nil
}

// ValidateOperatorSupport validates that the given operator is supported for a property.
func ValidateOperatorSupport(req placementv1beta1.PropertySelectorRequirement, supportedOps ...placementv1beta1.PropertySelectorOperator) error {
	for _, op := range supportedOps {
		if req.Operator == op {
			return nil
		}
	}
	return fmt.Errorf(ErrUnsupportedOperatorTemplate, req.Operator, req.Name)
}
