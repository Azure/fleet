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

// Package manager provides a fake controller-runtime manager for testing.
package manager

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// FakeManager is a fake controller-runtime manager for testing.
type FakeManager struct {
	manager.Manager
	// Client is the Kubernetes client used by this manager.
	Client client.Client
}

// GetClient returns the configured client.
func (f *FakeManager) GetClient() client.Client {
	return f.Client
}
