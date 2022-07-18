// Code generated by mockery v2.14.0. DO NOT EDIT.

package mocks

import (
	dynamic "k8s.io/client-go/dynamic"
	cache "k8s.io/client-go/tools/cache"

	mock "github.com/stretchr/testify/mock"

	schema "k8s.io/apimachinery/pkg/runtime/schema"

	utils "go.goms.io/fleet/pkg/utils"
)

// InformerManager is an autogenerated mock type for the InformerManager type
type InformerManager struct {
	mock.Mock
}

// AddDynamicResources provides a mock function with given fields: resources, handler, listComplete
func (_m *InformerManager) AddDynamicResources(resources []utils.APIResourceMeta, handler cache.ResourceEventHandler, listComplete bool) {
	_m.Called(resources, handler, listComplete)
}

// AddStaticResource provides a mock function with given fields: resource, handler
func (_m *InformerManager) AddStaticResource(resource utils.APIResourceMeta, handler cache.ResourceEventHandler) {
	_m.Called(resource, handler)
}

// GetClient provides a mock function with given fields:
func (_m *InformerManager) GetClient() dynamic.Interface {
	ret := _m.Called()

	var r0 dynamic.Interface
	if rf, ok := ret.Get(0).(func() dynamic.Interface); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(dynamic.Interface)
		}
	}

	return r0
}

// GetNameSpaceScopedResources provides a mock function with given fields:
func (_m *InformerManager) GetNameSpaceScopedResources() []schema.GroupVersionResource {
	ret := _m.Called()

	var r0 []schema.GroupVersionResource
	if rf, ok := ret.Get(0).(func() []schema.GroupVersionResource); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]schema.GroupVersionResource)
		}
	}

	return r0
}

// IsInformerSynced provides a mock function with given fields: resource
func (_m *InformerManager) IsInformerSynced(resource schema.GroupVersionResource) bool {
	ret := _m.Called(resource)

	var r0 bool
	if rf, ok := ret.Get(0).(func(schema.GroupVersionResource) bool); ok {
		r0 = rf(resource)
	} else {
		r0 = ret.Get(0).(bool)
	}

	return r0
}

// Lister provides a mock function with given fields: resource
func (_m *InformerManager) Lister(resource schema.GroupVersionResource) cache.GenericLister {
	ret := _m.Called(resource)

	var r0 cache.GenericLister
	if rf, ok := ret.Get(0).(func(schema.GroupVersionResource) cache.GenericLister); ok {
		r0 = rf(resource)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(cache.GenericLister)
		}
	}

	return r0
}

// Start provides a mock function with given fields:
func (_m *InformerManager) Start() {
	_m.Called()
}

// Stop provides a mock function with given fields:
func (_m *InformerManager) Stop() {
	_m.Called()
}

// WaitForCacheSync provides a mock function with given fields:
func (_m *InformerManager) WaitForCacheSync() {
	_m.Called()
}

type mockConstructorTestingTNewInformerManager interface {
	mock.TestingT
	Cleanup(func())
}

// NewInformerManager creates a new instance of InformerManager. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewInformerManager(t mockConstructorTestingTNewInformerManager) *InformerManager {
	mock := &InformerManager{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
