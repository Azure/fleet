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
