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

// TO-DO: apply the new error pattern incrementally when it is ready to do so.

// errors features custom error categorizing, wrapping and string formatting logic
// used in the KubeFleet project.
package errors

import (
	"errors"
	"fmt"
	"runtime"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type ErrCategory string

const (
	// An error is considered of the unexpected error category when a KubeFleet controller
	// encounters a situation that it does not know how to handle and cannot be resolved
	// by itself or by user fixing their inputs.
	ErrCategoryUnexpected ErrCategory = "unexpected"

	// An error is considered of the transient error category when a KubeFleet controller
	// encounters a situation that would self-resolve soon, e.g., cache not sync'd yet.
	ErrCategoryTransient ErrCategory = "transient"

	// An error is considered of the API server error category when it arises during a call
	// to the Kubernetes API server.
	ErrCategoryAPIServer ErrCategory = "APIserver"

	// An error is considered of the user error category when it arises due to some
	// incorrect input or action from the user side and can resolve when the
	// user fixes their inputs.
	ErrCategoryUser ErrCategory = "user"

	// An error is considered uncategorized when it does not fit into any of the other
	// predefined error categories or no category has been explicitly assigned.
	ErrCategoryUncategorized ErrCategory = "uncategorized"
)

var _ error = &Error{}

type Error struct {
	// category is the category of the error.
	category ErrCategory

	// wrapped is an error that this error wraps.
	wrapped error

	// desc is the additional description about the wrapped error.
	desc string

	// attrs are the additional attributes associated with the wrapped error.
	attrs []interface{}
}

func (e *Error) categoryWithDefault() ErrCategory {
	if len(e.category) == 0 {
		return ErrCategoryUncategorized
	}
	return e.category
}

// Error implements the error interface.
//
// Note the output intentionally does not include the additional attributes, so as to keep the cardinality
// low; to print them out, pass them explicitly to the klog functions.
func (e *Error) Error() string {
	switch {
	case e.wrapped != nil && len(e.desc) == 0:
		// With wrapped error but no description.
		return e.wrapped.Error()
	case e.wrapped != nil:
		// With wrapped error, with description.
		return fmt.Sprintf("%s: %s", e.desc, e.wrapped.Error())
	case e.wrapped == nil && len(e.desc) > 0:
		// No wrapped error but with description.
		return e.desc
	default:
		// No wrapped error and no description.
		return "an unknown error occurred"
	}
}

// Unwrap returns the wrapped error, so that errors.Is and errors.As can traverse the error chain as
// expected.
func (e *Error) Unwrap() error {
	return e.wrapped
}

// Wraps wraps an existing error with a higher-level description and optional key-value attributes,
// and returns a new *Error. If the existing error (or its children) is already an *Error,
// Wraps will surface the category and the attributes to the top-level.
func Wraps(err error, desc string, kvs ...interface{}) *Error {
	// Find if any of error down in the chain is already an *Error; if so, use its category and surface its
	// attributes to the current level.
	var category ErrCategory
	var mergedKVs []interface{}
	var childError *Error
	if errors.As(err, &childError) {
		category = childError.category
		mergedKVs = append(mergedKVs, childError.attrs...)
	}
	mergedKVs = append(mergedKVs, kvs...)
	if len(category) == 0 {
		category = ErrCategoryUncategorized
	}

	return &Error{
		category: category,
		wrapped:  err,
		desc:     desc,
		attrs:    mergedKVs,
	}
}

// Args returns the k-v attributes associated with an *Error (and its children).
//
// The function is designed to work with klog calls; to use it, call klog.ErrorS with:
//
// klog.ErrorS(err, "additional top-level error description", Args(err)...).
//
// If you need to add additional key-value attributes at the top level in addition to those
// already captured from the error chain, append them to Args as follows:
//
// klog.ErrorS(err, "additional top-level error description", append(Args(err), "k", "v")...).
func Args(err error, kvs ...interface{}) []interface{} {
	var compositeKVs []interface{}
	var childError *Error
	if errors.As(err, &childError) {
		// Make sure that the error category is always included as the first kv pair.
		compositeKVs = append(compositeKVs, "errCategory", childError.categoryWithDefault())
		compositeKVs = append(compositeKVs, childError.attrs...)
		compositeKVs = append(compositeKVs, kvs...)
	}
	return compositeKVs
}

// CallerFrameOverview is a simple struct that summarizes a single call frame.
type CallerFrameOverview struct {
	Function string
	File     string
	Line     int
}

// captureCallers captures the current call stack and returns a slice of CallerFrameOverview
// summarizing each frame in the stack, skipping the top 3 frames for simplicity.
func captureCallers() []CallerFrameOverview {
	pcs := make([]uintptr, 32)
	// Skip 3 frames (the runtime.Callers function itself, this utility function, and the caller of this utility)
	// when capturing the call stack for simplicity reasons.
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var callerFrames []CallerFrameOverview
	for {
		frame, more := frames.Next()
		callerFrames = append(callerFrames, CallerFrameOverview{
			Function: frame.Function,
			File:     frame.File,
			Line:     frame.Line,
		})
		if !more {
			break
		}
	}
	return callerFrames
}

// NewUnexpectedError creates a new Error categorized as ErrCategoryUnexpected. It is supposed to be called
// at the lowest level possible, where the error category can be determined. The error can then be returned
// and wrapped (w/ additional attributes if applicable).
func NewUnexpectedError(err error, desc string, kvs ...interface{}) *Error {
	// For unexpected errors, collect the caller frames.
	//
	// Note (chenyu1): previously for unexpected errors, we use the debug.Stack() to print out a full stack
	// trace in the logs. Effective as it is, the operation is expensive and it always captures the full stack,
	// where the top of the stack is always the error handling utility function/method itself rather than
	// the actual caller site where the error originates. Furthermore, as debug.Stack() returns a multi-line string,
	// often the logging backend cannot render it properly, making it difficult to read/summarize. To address this,
	// in this new implementation we switch to runtime.Callers and runtime.CallersFrames to capture the call stack
	// in a structured way with the unrelated stacks skipped.
	frames := captureCallers()
	kvs = append(kvs, "callers", frames)

	newErr := Wraps(err, desc, kvs...)
	newErr.category = ErrCategoryUnexpected
	return newErr
}

// NewTransientError creates a new Error categorized as ErrCategoryTransient. It is supposed to be called
// at the lowest level possible, where the error category can be determined. The error can then be returned
// and wrapped (w/ additional attributes if applicable).
func NewTransientError(err error, desc string, kvs ...interface{}) *Error {
	newErr := Wraps(err, desc, kvs...)
	newErr.category = ErrCategoryTransient
	return newErr
}

// NewUserError creates a new Error categorized as ErrCategoryUser. It is supposed to be called
// at the lowest level possible, where the error category can be determined. The error can then be returned
// and wrapped (w/ additional attributes if applicable).
func NewUserError(err error, desc string, kvs ...interface{}) *Error {
	newErr := Wraps(err, desc, kvs...)
	newErr.category = ErrCategoryUser
	return newErr
}

// NewAPIServerError creates a new Error categorized as ErrCategoryAPIServer. It is supposed to be called
// at the lowest level possible, where the error category can be determined. The error can then be returned
// and wrapped (w/ additional attributes if applicable).
func NewAPIServerError(err error, desc string, cached bool, kvs ...interface{}) *Error {
	// For API server errors, collect caching and HTTP response related information.
	kvs = append(kvs, "cached", cached)
	kvs = append(kvs, "APIServerErrReason", apierrors.ReasonForError(err))

	apiErr, ok := err.(apierrors.APIStatus)
	if ok {
		kvs = append(kvs, "APIServerResHTTPCode", apiErr.Status().Code)
	}

	newErr := Wraps(err, desc, kvs...)
	newErr.category = ErrCategoryAPIServer
	return newErr
}
