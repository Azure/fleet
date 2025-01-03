/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package parallelizer

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// TestErrorFlag tests the basic ops of an ErrorFlag.
func TestErrorFlag(t *testing.T) {
	errFlag := NewErrorFlag()
	err := fmt.Errorf("test error")

	errFlag.Raise(err)
	returnedErr := errFlag.Lower()
	if !cmp.Equal(err, returnedErr, cmpopts.EquateErrors()) {
		t.Fatalf("Lower() = %v, want %v", returnedErr, err)
	}
}
