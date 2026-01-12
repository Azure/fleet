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

// Package handler provides test utilities for Kubernetes event handlers.
package handler

// TestHandler is a simple implementation of cache.ResourceEventHandler for testing.
// It allows tests to track when specific event handler methods are called.
type TestHandler struct {
	OnAddFunc    func()
	OnUpdateFunc func()
	OnDeleteFunc func()
}

// OnAdd is called when an object is added.
func (h *TestHandler) OnAdd(obj interface{}, isInInitialList bool) {
	if h.OnAddFunc != nil {
		h.OnAddFunc()
	}
}

// OnUpdate is called when an object is updated.
func (h *TestHandler) OnUpdate(oldObj, newObj interface{}) {
	if h.OnUpdateFunc != nil {
		h.OnUpdateFunc()
	}
}

// OnDelete is called when an object is deleted.
func (h *TestHandler) OnDelete(obj interface{}) {
	if h.OnDeleteFunc != nil {
		h.OnDeleteFunc()
	}
}
