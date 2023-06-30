/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
)

// StateKey is the key for a state value stored in a CycleState.
type StateKey string

// StateValue is the value stored in a CycleState under a specific key.
type StateValue interface{}

// CycleStatePluginReadWriter is an interface through which plugins can store and retrieve data.
//
// TO-DO (chenyu1): Add methods which allow plugins to query for bindings of different types being
// evaluated in the current scheduling cycle.
type CycleStatePluginReadWriter interface {
	Read(key StateKey) (StateValue, error)
	Write(key StateKey, val StateValue)
	Delete(key StateKey)
}

// CycleState is, similar to its namesake in kube-scheduler, provides a way for plugins to
// store and retrieve arbitrary data during a scheduling cycle. The scheduler also uses
// this struct to keep some global states during a scheduling cycle; note that these
// state are only accessible to the scheduler itself, not to plugins.
//
// It uses a sync.Map for concurrency-safe storage.
type CycleState struct {
	// store is a concurrency-safe store (a map).
	store sync.Map

	// skippedFilterPlugins is a set of Filter plugins that should be skipped in the current scheduling cycle.
	//
	// TO-DO (chenyu1): the sets package has added support for Go generic types in 1.26, and
	// the String set has been deprecated; transition to the generic set when the new version
	// becomes available.
	skippedFilterPlugins sets.String
}

// Read retrieves a value from CycleState by a key.
func (c *CycleState) Read(key StateKey) (StateValue, error) {
	if v, ok := c.store.Load(key); ok {
		return v, nil
	}
	return nil, fmt.Errorf("key %s is not found", key)
}

// Write stores a value in CycleState under a key.
func (c *CycleState) Write(key StateKey, val StateValue) {
	c.store.Store(key, val)
}

// Delete deletes a key from CycleState.
func (c *CycleState) Delete(key StateKey) {
	c.store.Delete(key)
}

// NewCycleState creates a CycleState.
func NewCycleState() *CycleState {
	return &CycleState{
		store:                sync.Map{},
		skippedFilterPlugins: sets.NewString(),
	}
}
