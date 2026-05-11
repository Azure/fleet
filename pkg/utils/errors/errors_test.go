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

package errors

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
)

var (
	// wrappedErrComparer is a cmp option that compares the .wrapped field by pointer equality,
	// avoiding the need to recurse into unexported fields of standard-library error types.
	wrappedErrComparer = cmp.FilterPath(
		func(p cmp.Path) bool { return p.Last().String() == ".wrapped" },
		cmp.Comparer(func(x, y error) bool { return x == y }), //nolint:errorlint
	)
)

// TestCommonUsePatterns demonstrates how the common error handling patterns are enabled by the errors package.
func TestCommonUsePatterns(t *testing.T) {
	var bytesBuf bytes.Buffer
	slogHandler := slog.NewTextHandler(&bytesBuf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := logr.FromSlogHandler(slogHandler)
	klog.SetLogger(logger)

	// Example 1: handling API server errors with rich context and structured logging.
	rawAPIServerErr := apierrors.NewAlreadyExists(schema.GroupResource{Group: "", Resource: "namespaces"}, "work")
	categorizedAPIServerErr := NewAPIServerError(rawAPIServerErr, "failed to create namespace", false, "k1", "v1")
	wrappedErr := Wraps(categorizedAPIServerErr, "additional high-level error description", "k2", "v2", "k3", "v3")
	klog.ErrorS(wrappedErr, "additional top/controller-level error description", Args(wrappedErr)...)
	// If there needs to be kv attributes at the top level as well:
	// klog.ErrorS(wrappedErr, "additional top/controller-level error description", append(Args(wrappedErr), "k4", "v4", "k5", "v5")...)
	klog.Flush()

	outputStr := readFromBuffer(t, &bytesBuf)

	// The full error output looks like the follows:
	// errors_test.go:53: time=2026-04-29T02:39:44.853+10:00 level=ERROR msg="additional top/controller-level error description" err="additional high-level error description: failed to create namespace: namespaces \"work\" already exists" errCategory=APIserver k1=v1 cached=false APIServerErrReason=AlreadyExists APIServerResHTTPCode=409 k2=v2 k3=v3
	wantSubStrings := []string{
		"msg=\"additional top/controller-level error description\"",
		"err=\"additional high-level error description: failed to create namespace: namespaces \\\"work\\\" already exists\"",
		"errCategory=APIserver",
		"k1=v1",
		"k2=v2",
		"k3=v3",
		"cached=false",
		"APIServerErrReason=AlreadyExists",
		"APIServerResHTTPCode=409",
	}
	for _, subStr := range wantSubStrings {
		if strings.Contains(outputStr, subStr) == false {
			t.Errorf("got %s\nwant to contain %s\n", outputStr, subStr)
		}
	}

	// Example 2: handling unexpected errors with rich context and structured logging.
	rawUtilityCallErr := fmt.Errorf("cannot calculate resource hash")
	categorizedUnexpectedErr := NewUnexpectedError(rawUtilityCallErr, "additional low-level error description", "k1", "v1")
	wrappedUnexpectedErr := Wraps(categorizedUnexpectedErr, "additional high-level error description", "k2", "v2")
	klog.ErrorS(wrappedUnexpectedErr, "additional top/controller-level error description", Args(wrappedUnexpectedErr)...)
	klog.Flush()

	outputStr = readFromBuffer(t, &bytesBuf)

	// The full error output looks like the follows:
	// errors_test.go:86: time=2026-04-29T02:49:04.560+10:00 level=ERROR msg="additional top/controller-level error description" err="additional high-level error description: additional low-level error description: cannot calculate resource hash" errCategory=unexpected k1=v1 callers="[{Function:github.com/kubefleet-dev/kubefleet/pkg/utils/errors.TestCommonUsePatterns File:SomeFilePath Line:74} {Function:testing.tRunner File:SomeFilePath Line:1934} {Function:runtime.goexit File:SomeFilePath Line:1268}]" k2=v2
	wantSubStrings = []string{
		"msg=\"additional top/controller-level error description\"",
		"err=\"additional high-level error description: additional low-level error description: cannot calculate resource hash\"",
		"errCategory=unexpected",
		"k1=v1",
		"callers=",
		"Function:github.com/kubefleet-dev/kubefleet/pkg/utils/errors.TestCommonUsePatterns",
		"k2=v2",
	}
	for _, subStr := range wantSubStrings {
		if strings.Contains(outputStr, subStr) == false {
			t.Errorf("got %s\nwant to contain %s\n", outputStr, subStr)
		}
	}

	// Example 3: handling uncategorized errors with rich context and structured logging.
	rawErr := fmt.Errorf("something went wrong")
	uncategorizedErr := Wraps(rawErr, "additional low-level error description", "k1", "v1")
	wrappedUncategorizedErr := Wraps(uncategorizedErr, "additional high-level error description", "k2", "v2")
	klog.ErrorS(wrappedUncategorizedErr, "additional top/controller-level error description", Args(wrappedUncategorizedErr)...)
	klog.Flush()

	outputStr = readFromBuffer(t, &bytesBuf)

	// The full error output looks like the follows:
	// errors_test.go:119: time=2026-04-29T02:59:04.560+10:00 level=ERROR msg="additional top/controller-level error description" err="additional high-level error description: additional low-level error description: something went wrong" errCategory=uncategorized k1=v1 k2=v2
	wantSubStrings = []string{
		"msg=\"additional top/controller-level error description\"",
		"err=\"additional high-level error description: additional low-level error description: something went wrong\"",
		"errCategory=uncategorized",
		"k1=v1",
		"k2=v2",
	}
	for _, subStr := range wantSubStrings {
		if strings.Contains(outputStr, subStr) == false {
			t.Errorf("got %s\nwant to contain %s\n", outputStr, subStr)
		}
	}
}

func TestErrorStringer(t *testing.T) {
	wrappedErr := fmt.Errorf("something went wrong")

	testCases := []struct {
		name    string
		err     *Error
		wantMsg string
	}{
		{
			name:    "no wrapped error, no description",
			err:     &Error{},
			wantMsg: "an unknown error occurred",
		},
		{
			name:    "no wrapped error, with description",
			err:     &Error{desc: "operation failed"},
			wantMsg: "operation failed",
		},
		{
			name:    "with wrapped error, no description",
			err:     &Error{wrapped: wrappedErr},
			wantMsg: "something went wrong",
		},
		{
			name:    "with wrapped error, with description",
			err:     &Error{wrapped: wrappedErr, desc: "operation failed"},
			wantMsg: "operation failed: something went wrong",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.err.Error()
			if got != tc.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tc.wantMsg)
			}
		})
	}
}

func TestWraps(t *testing.T) {
	plainErr := fmt.Errorf("plain error")
	categorizedChildErr := &Error{
		category: ErrCategoryUser,
		desc:     "child desc",
		attrs:    []interface{}{"ck1", "cv1"},
	}

	testCases := []struct {
		name    string
		err     error
		desc    string
		kvs     []interface{}
		wantErr *Error
	}{
		{
			name: "wrapping a plain error, no kvs",
			err:  plainErr,
			desc: "high level desc",
			wantErr: &Error{
				category: ErrCategoryUncategorized,
				wrapped:  plainErr,
				desc:     "high level desc",
			},
		},
		{
			name: "wrapping a plain error, with kvs",
			err:  plainErr,
			desc: "high level desc",
			kvs:  []interface{}{"k1", "v1"},
			wantErr: &Error{
				category: ErrCategoryUncategorized,
				wrapped:  plainErr,
				desc:     "high level desc",
				attrs:    []interface{}{"k1", "v1"},
			},
		},
		{
			name: "wrapping an *Error, no extra kvs",
			err:  categorizedChildErr,
			desc: "high level desc",
			wantErr: &Error{
				category: ErrCategoryUser,
				wrapped:  categorizedChildErr,
				desc:     "high level desc",
				attrs:    []interface{}{"ck1", "cv1"},
			},
		},
		{
			name: "wrapping an *Error, with extra kvs",
			err:  categorizedChildErr,
			desc: "high level desc",
			kvs:  []interface{}{"k2", "v2"},
			wantErr: &Error{
				category: ErrCategoryUser,
				wrapped:  categorizedChildErr,
				desc:     "high level desc",
				attrs:    []interface{}{"ck1", "cv1", "k2", "v2"},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := Wraps(tc.err, tc.desc, tc.kvs...)
			if diff := cmp.Diff(gotErr, tc.wantErr,
				cmp.AllowUnexported(Error{}),
				wrappedErrComparer,
			); diff != "" {
				t.Errorf("Wraps() mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

func TestArgs(t *testing.T) {
	testCases := []struct {
		name    string
		err     error
		kvs     []interface{}
		wantKVs []interface{}
	}{
		{
			name:    "plain error (not an *Error)",
			err:     fmt.Errorf("plain error"),
			wantKVs: nil,
		},
		{
			name:    "*Error with no category set, no attrs, no extra kvs",
			err:     &Error{},
			wantKVs: []interface{}{"errCategory", ErrCategoryUncategorized},
		},
		{
			name:    "*Error with explicit category, no attrs, no extra kvs",
			err:     &Error{category: ErrCategoryUser},
			wantKVs: []interface{}{"errCategory", ErrCategoryUser},
		},
		{
			name:    "*Error with explicit category and attrs, no extra kvs",
			err:     &Error{category: ErrCategoryAPIServer, attrs: []interface{}{"k1", "v1", "k2", "v2"}},
			wantKVs: []interface{}{"errCategory", ErrCategoryAPIServer, "k1", "v1", "k2", "v2"},
		},
		{
			name:    "*Error with explicit category and attrs, with extra kvs",
			err:     &Error{category: ErrCategoryUnexpected, attrs: []interface{}{"k1", "v1"}},
			kvs:     []interface{}{"k2", "v2"},
			wantKVs: []interface{}{"errCategory", ErrCategoryUnexpected, "k1", "v1", "k2", "v2"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotKVs := Args(tc.err, tc.kvs...)
			if diff := cmp.Diff(gotKVs, tc.wantKVs); diff != "" {
				t.Errorf("Args() mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

func TestUnwrap(t *testing.T) {
	innerErr := fmt.Errorf("inner error")
	testCases := []struct {
		name    string
		err     *Error
		wantErr error
	}{
		{
			name:    "nil wrapped error",
			err:     &Error{},
			wantErr: nil,
		},
		{
			name:    "non-nil wrapped error",
			err:     &Error{wrapped: innerErr},
			wantErr: innerErr,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.err.Unwrap()
			// Note: here we actually want to just compare the error values by pointer equality.
			if gotErr != tc.wantErr { //nolint:errorlint
				t.Errorf("Unwrap() = %v, want %v", gotErr, tc.wantErr)
			}
		})
	}
}

func TestNewTransientError(t *testing.T) {
	plainErr := fmt.Errorf("plain error")
	categorizedChildErr := &Error{
		category: ErrCategoryUser,
		desc:     "child desc",
		attrs:    []interface{}{"ck1", "cv1"},
	}

	testCases := []struct {
		name    string
		err     error
		desc    string
		kvs     []interface{}
		wantErr *Error
	}{
		{
			name: "plain error, no kvs",
			err:  plainErr,
			desc: "transient condition",
			wantErr: &Error{
				category: ErrCategoryTransient,
				wrapped:  plainErr,
				desc:     "transient condition",
			},
		},
		{
			name: "plain error, with kvs",
			err:  plainErr,
			desc: "transient condition",
			kvs:  []interface{}{"k1", "v1"},
			wantErr: &Error{
				category: ErrCategoryTransient,
				wrapped:  plainErr,
				desc:     "transient condition",
				attrs:    []interface{}{"k1", "v1"},
			},
		},
		// This is technically speaking possible, but in practice users should not need to override categories.
		{
			name: "wrapping an *Error, category overridden to transient",
			err:  categorizedChildErr,
			desc: "transient condition",
			kvs:  []interface{}{"k2", "v2"},
			wantErr: &Error{
				category: ErrCategoryTransient,
				wrapped:  categorizedChildErr,
				desc:     "transient condition",
				attrs:    []interface{}{"ck1", "cv1", "k2", "v2"},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := NewTransientError(tc.err, tc.desc, tc.kvs...)
			if diff := cmp.Diff(gotErr, tc.wantErr,
				cmp.AllowUnexported(Error{}),
				wrappedErrComparer,
			); diff != "" {
				t.Errorf("NewTransientError() mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

func TestNewUserError(t *testing.T) {
	plainErr := fmt.Errorf("plain error")
	categorizedChildErr := &Error{
		category: ErrCategoryTransient,
		desc:     "child desc",
		attrs:    []interface{}{"ck1", "cv1"},
	}

	testCases := []struct {
		name    string
		err     error
		desc    string
		kvs     []interface{}
		wantErr *Error
	}{
		{
			name: "plain error, no kvs",
			err:  plainErr,
			desc: "invalid input",
			wantErr: &Error{
				category: ErrCategoryUser,
				wrapped:  plainErr,
				desc:     "invalid input",
			},
		},
		{
			name: "plain error, with kvs",
			err:  plainErr,
			desc: "invalid input",
			kvs:  []interface{}{"k1", "v1"},
			wantErr: &Error{
				category: ErrCategoryUser,
				wrapped:  plainErr,
				desc:     "invalid input",
				attrs:    []interface{}{"k1", "v1"},
			},
		},
		{
			// This is technically speaking possible, but in practice users should not need to override categories.
			name: "wrapping an *Error, category overridden to user",
			err:  categorizedChildErr,
			desc: "invalid input",
			kvs:  []interface{}{"k2", "v2"},
			wantErr: &Error{
				category: ErrCategoryUser,
				wrapped:  categorizedChildErr,
				desc:     "invalid input",
				attrs:    []interface{}{"ck1", "cv1", "k2", "v2"},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := NewUserError(tc.err, tc.desc, tc.kvs...)
			if diff := cmp.Diff(gotErr, tc.wantErr,
				cmp.AllowUnexported(Error{}),
				wrappedErrComparer,
			); diff != "" {
				t.Errorf("NewUserError() mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

func TestNewAPIServerError(t *testing.T) {
	apiServerErr := apierrors.NewAlreadyExists(schema.GroupResource{Group: "", Resource: "namespaces"}, "fleet")
	plainErr := fmt.Errorf("plain error")

	testCases := []struct {
		name    string
		err     error
		desc    string
		cached  bool
		kvs     []interface{}
		wantErr *Error
	}{
		{
			name:   "API server error, not cached, no extra kvs",
			err:    apiServerErr,
			desc:   "failed to create namespace",
			cached: false,
			wantErr: &Error{
				category: ErrCategoryAPIServer,
				wrapped:  apiServerErr,
				desc:     "failed to create namespace",
				attrs: []interface{}{
					"cached", false,
					"APIServerErrReason", apierrors.ReasonForError(apiServerErr),
					"APIServerResHTTPCode", apiServerErr.Status().Code,
				},
			},
		},
		{
			name:   "API server error, cached, with extra kvs",
			err:    apiServerErr,
			desc:   "failed to create namespace",
			cached: true,
			kvs:    []interface{}{"k1", "v1"},
			wantErr: &Error{
				category: ErrCategoryAPIServer,
				wrapped:  apiServerErr,
				desc:     "failed to create namespace",
				attrs: []interface{}{
					"k1", "v1",
					"cached", true,
					"APIServerErrReason", apierrors.ReasonForError(apiServerErr),
					"APIServerResHTTPCode", apiServerErr.Status().Code,
				},
			},
		},
		{
			name:   "non API server error, no HTTPCode in attrs",
			err:    plainErr,
			desc:   "unexpected failure",
			cached: false,
			wantErr: &Error{
				category: ErrCategoryAPIServer,
				wrapped:  plainErr,
				desc:     "unexpected failure",
				attrs: []interface{}{
					"cached", false,
					"APIServerErrReason", apierrors.ReasonForError(plainErr),
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := NewAPIServerError(tc.err, tc.desc, tc.cached, tc.kvs...)
			if diff := cmp.Diff(gotErr, tc.wantErr,
				cmp.AllowUnexported(Error{}),
				wrappedErrComparer,
			); diff != "" {
				t.Errorf("NewAPIServerError() mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

func TestNewUnexpectedError(t *testing.T) {
	plainErr := fmt.Errorf("plain error")
	gotErr := NewUnexpectedError(plainErr, "low-level desc", "k1", "v1")

	// Verify the category.
	if gotErr.category != ErrCategoryUnexpected {
		t.Errorf("NewUnexpectedError() category = %v, want %v", gotErr.category, ErrCategoryUnexpected)
	}
	// Verify wrapped error and description via Error().
	wantMsg := "low-level desc: plain error"
	if got := gotErr.Error(); got != wantMsg {
		t.Errorf("NewUnexpectedError() Error() = %q, want %q", got, wantMsg)
	}
	// Verify that the user-provided kvs appear at the start of attrs.
	if len(gotErr.attrs) < 2 || gotErr.attrs[0] != "k1" || gotErr.attrs[1] != "v1" {
		t.Errorf("NewUnexpectedError() attrs = %v, want k1=v1 at start", gotErr.attrs)
	}
	// Verify that the callers key is present in attrs and its frame count is not zero.
	found := false
	canCast := false
	var frames []CallerFrameOverview
	for i := 0; i < len(gotErr.attrs)-1; i++ {
		if gotErr.attrs[i] == "callers" {
			found = true
			frames, canCast = gotErr.attrs[i+1].([]CallerFrameOverview)
			break
		}
	}
	if !found {
		t.Errorf("NewUnexpectedError() attrs = %v, want callers key present", gotErr.attrs)
	}
	if !canCast {
		t.Errorf("NewUnexpectedError() callers value has type %T, want []CallerFrameOverview", gotErr.attrs)
	}
	if len(frames) == 0 {
		t.Errorf("NewUnexpectedError() callers = %v, want non-zero frame count", frames)
	}

	// Verify the first frame (the current test code).
	callerFunc := frames[0].Function
	if callerFunc != "github.com/kubefleet-dev/kubefleet/pkg/utils/errors.TestNewUnexpectedError" {
		t.Errorf("NewUnexpectedError() first caller function = %s, want TestNewUnexpectedError", callerFunc)
	}
	callerFile := frames[0].File
	if !strings.HasSuffix(callerFile, "pkg/utils/errors/errors_test.go") {
		t.Errorf("NewUnexpectedError() first caller file = %s, want to end with pkg/utils/errors/errors_test.go", callerFile)
	}
	// Note: skip verifying the exact line number as it may change with code edits.
}

func readFromBuffer(t *testing.T, buf *bytes.Buffer) string {
	output := make([]byte, 1000)
	n, err := buf.Read(output)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read from bytes buffer: %v", err)
	}
	output = output[:n]
	outputStr := string(output)
	return outputStr
}
