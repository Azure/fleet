/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package controller provides a fake controller for testing.
package controller

import (
	"context"
	"sync"
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
