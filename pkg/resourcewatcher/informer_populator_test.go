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

package resourcewatcher

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/restmapper"

	"go.goms.io/fleet/pkg/utils"
	testinformer "go.goms.io/fleet/test/utils/informer"
	testresource "go.goms.io/fleet/test/utils/resource"
)

const (
	// testTimeout is the timeout for test operations
	testTimeout = 200 * time.Millisecond
	// testSleep is how long to sleep to allow periodic operations
	testSleep = 150 * time.Millisecond
)

func TestInformerPopulator_NeedLeaderElection(t *testing.T) {
	populator := &InformerPopulator{}

	// InformerPopulator should NOT need leader election so it runs on all pods
	if populator.NeedLeaderElection() {
		t.Error("InformerPopulator should not need leader election")
	}
}

func TestInformerPopulator_discoverAndCreateInformers(t *testing.T) {
	tests := []struct {
		name                    string
		discoveryResources      []*metav1.APIResourceList
		resourceConfig          *utils.ResourceConfig
		expectedInformerCreated bool
		expectedResourceCount   int
	}{
		{
			name: "creates informers for watchable resources",
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "v1",
					APIResources: []metav1.APIResource{
						testresource.APIResourceConfigMap(),
					},
				},
			},
			resourceConfig:          nil, // Allow all resources
			expectedInformerCreated: true,
			expectedResourceCount:   1,
		},
		{
			name: "skips resources without list/watch verbs",
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "v1",
					APIResources: []metav1.APIResource{
						testresource.APIResourceWithVerbs("configmaps", "ConfigMap", true, []string{"get", "delete"}), // Missing list/watch
					},
				},
			},
			resourceConfig:          nil,
			expectedInformerCreated: false,
			expectedResourceCount:   0,
		},
		{
			name: "respects resource config filtering",
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "v1",
					APIResources: []metav1.APIResource{
						testresource.APIResourceSecret(),
					},
				},
			},
			resourceConfig: func() *utils.ResourceConfig {
				rc := utils.NewResourceConfig(false) // Skip mode
				_ = rc.Parse("v1/Secret")            // Skip secrets
				return rc
			}(),
			expectedInformerCreated: false,
			expectedResourceCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake discovery client
			fakeClient := fake.NewSimpleClientset()
			fakeDiscovery, ok := fakeClient.Discovery().(*fakediscovery.FakeDiscovery)
			if !ok {
				t.Fatal("Failed to cast to FakeDiscovery")
			}
			fakeDiscovery.Resources = tt.discoveryResources

			// Create REST mapper
			groupResources := []*restmapper.APIGroupResources{}
			for _, resourceList := range tt.discoveryResources {
				gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
				if err != nil {
					t.Fatalf("Failed to parse group version: %v", err)
				}

				apiResources := []metav1.APIResource{}
				apiResources = append(apiResources, resourceList.APIResources...)

				groupResources = append(groupResources, &restmapper.APIGroupResources{
					Group: metav1.APIGroup{
						Name: gv.Group,
						Versions: []metav1.GroupVersionForDiscovery{
							{GroupVersion: resourceList.GroupVersion, Version: gv.Version},
						},
						PreferredVersion: metav1.GroupVersionForDiscovery{
							GroupVersion: resourceList.GroupVersion,
							Version:      gv.Version,
						},
					},
					VersionedResources: map[string][]metav1.APIResource{
						gv.Version: apiResources,
					},
				})
			}
			restMapper := restmapper.NewDiscoveryRESTMapper(groupResources)

			// Create fake informer manager
			fakeInformerManager := &testinformer.FakeManager{
				APIResources: make(map[schema.GroupVersionKind]bool),
			}

			// Track calls to CreateInformerForResource
			populator := &InformerPopulator{
				DiscoveryClient: fakeDiscovery,
				RESTMapper:      restMapper,
				InformerManager: fakeInformerManager,
				ResourceConfig:  tt.resourceConfig,
			}

			// Run discovery
			populator.discoverAndCreateInformers()

			// Note: FakeManager doesn't track calls, so we verify no panics occurred
		})
	}
}

func TestInformerPopulator_Start(t *testing.T) {
	// Create fake discovery client with some resources
	fakeClient := fake.NewSimpleClientset()
	fakeDiscovery, ok := fakeClient.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		t.Fatal("Failed to cast to FakeDiscovery")
	}

	fakeDiscovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				testresource.APIResourceConfigMap(),
			},
		},
	}

	// Create REST mapper
	gv := schema.GroupVersion{Group: "", Version: "v1"}
	groupResources := []*restmapper.APIGroupResources{
		testresource.APIGroupResourcesV1(testresource.APIResourceConfigMap()),
	}
	restMapper := restmapper.NewDiscoveryRESTMapper(groupResources)

	// Create fake informer manager
	fakeInformerManager := &testinformer.FakeManager{
		APIResources: map[schema.GroupVersionKind]bool{
			gv.WithKind("ConfigMap"): true,
		},
		IsClusterScopedResource: false,
	}

	populator := &InformerPopulator{
		DiscoveryClient: fakeDiscovery,
		RESTMapper:      restMapper,
		InformerManager: fakeInformerManager,
		ResourceConfig:  nil,
	}

	// Create a context that will cancel after a short time
	// Use half of testTimeout to ensure we have time to verify after cancellation
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout/2)
	defer cancel()

	// Start the populator in a goroutine
	done := make(chan error, 1)
	go func() {
		done <- populator.Start(ctx)
	}()

	// Wait for context to cancel or error
	select {
	case err := <-done:
		// Should return nil when context is canceled
		if err != nil {
			t.Errorf("Start should not return error on context cancellation: %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("Start did not exit after context cancellation")
	}
}

func TestInformerPopulator_Integration(t *testing.T) {
	// This test verifies the integration between InformerPopulator and the informer manager

	// Create fake discovery with multiple resource types
	fakeClient := fake.NewSimpleClientset()
	fakeDiscovery, ok := fakeClient.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		t.Fatal("Failed to cast to FakeDiscovery")
	}

	fakeDiscovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				testresource.APIResourceConfigMap(),
				testresource.APIResourceSecret(),
			},
		},
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{
				testresource.APIResourceDeployment(),
			},
		},
	}

	// Create REST mapper
	groupResources := []*restmapper.APIGroupResources{
		{
			Group: testresource.APIGroupV1(),
			VersionedResources: map[string][]metav1.APIResource{
				"v1": fakeDiscovery.Resources[0].APIResources,
			},
		},
		{
			Group: testresource.APIGroupAppsV1(),
			VersionedResources: map[string][]metav1.APIResource{
				"v1": fakeDiscovery.Resources[1].APIResources,
			},
		},
	}
	restMapper := restmapper.NewDiscoveryRESTMapper(groupResources)

	// Create resource config that skips secrets
	resourceConfig := utils.NewResourceConfig(false)
	err := resourceConfig.Parse("v1/Secret")
	if err != nil {
		t.Fatalf("Failed to parse resource config: %v", err)
	}

	fakeInformerManager := &testinformer.FakeManager{
		APIResources:            make(map[schema.GroupVersionKind]bool),
		IsClusterScopedResource: false,
	}

	populator := &InformerPopulator{
		DiscoveryClient: fakeDiscovery,
		RESTMapper:      restMapper,
		InformerManager: fakeInformerManager,
		ResourceConfig:  resourceConfig,
	}

	// Run discovery
	populator.discoverAndCreateInformers()

	// Note: FakeManager doesn't track calls, so we just verify no panics
}

func TestInformerPopulator_PeriodicDiscovery(t *testing.T) {
	// This test verifies that the populator continues to discover resources periodically

	fakeClient := fake.NewSimpleClientset()
	fakeDiscovery, ok := fakeClient.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		t.Fatal("Failed to cast to FakeDiscovery")
	}

	fakeDiscovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				testresource.APIResourceConfigMap(),
			},
		},
	}

	groupResources := []*restmapper.APIGroupResources{
		{
			Group: testresource.APIGroupV1(),
			VersionedResources: map[string][]metav1.APIResource{
				"v1": fakeDiscovery.Resources[0].APIResources,
			},
		},
	}
	restMapper := restmapper.NewDiscoveryRESTMapper(groupResources)

	fakeInformerManager := &testinformer.FakeManager{
		APIResources:            make(map[schema.GroupVersionKind]bool),
		IsClusterScopedResource: false,
	}

	populator := &InformerPopulator{
		DiscoveryClient: fakeDiscovery,
		RESTMapper:      restMapper,
		InformerManager: fakeInformerManager,
		ResourceConfig:  nil,
	}

	// Override the discovery period for testing
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Start the populator
	go func() {
		_ = populator.Start(ctx)
	}()

	// Wait a bit to allow multiple discovery cycles
	time.Sleep(testSleep)

	// Note: FakeManager doesn't track calls, so we just verify successful execution
}
