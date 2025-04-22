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

// Package controller provides a fake controller for testing.
package controller

import (
	"context"
	"sort"
	"sync"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FakeController is a fake controller which only stores one key.
type FakeController struct {
	key string
	mu  sync.RWMutex
}

// ResetQueue resets the value in the queue.
func (f *FakeController) ResetQueue() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.key = ""
}

// Enqueue enqueues a string type key.
func (f *FakeController) Enqueue(obj interface{}) {
	key, ok := obj.(string)
	if !ok {
		return
	}
	f.mu.Lock()
	f.key = key
	f.mu.Unlock()
}

// Run does nothing.
func (f *FakeController) Run(_ context.Context, _ int) error {
	return nil
}

// Key returns the key stored in the queue.
func (f *FakeController) Key() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.key
}

// CompareConditions compares two condition slices and returns a string with the differences.
func CompareConditions(wantConditions, gotConditions []v1.Condition) string {
	ignoreOption := cmpopts.IgnoreFields(v1.Condition{}, "LastTransitionTime", "ObservedGeneration", "Message")
	// we need to sort each condition slice by type before comparing
	sort.SliceStable(wantConditions, func(i, j int) bool {
		return wantConditions[i].Type < wantConditions[j].Type
	})
	sort.SliceStable(gotConditions, func(i, j int) bool {
		return gotConditions[i].Type < gotConditions[j].Type
	})
	return cmp.Diff(wantConditions, gotConditions, ignoreOption)
}
