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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/cache"

	"go.goms.io/fleet/pkg/utils"
	testinformer "go.goms.io/fleet/test/utils/informer"
	testresource "go.goms.io/fleet/test/utils/resource"
)

func TestChangeDetector_discoverResources(t *testing.T) {
	tests := []struct {
		name               string
		discoveryResources []*metav1.APIResourceList
		resourceConfig     *utils.ResourceConfig
	}{
		{
			name: "discovers and adds handlers for watchable resources",
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "v1",
					APIResources: []metav1.APIResource{
						testresource.APIResourceConfigMap(),
						testresource.APIResourceSecret(),
					},
				},
			},
			resourceConfig: nil, // Allow all resources
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
			resourceConfig: nil,
		},
		{
			name: "respects resource config filtering",
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "v1",
					APIResources: []metav1.APIResource{
						testresource.APIResourceConfigMap(),
						testresource.APIResourceSecret(),
					},
				},
			},
			resourceConfig: func() *utils.ResourceConfig {
				rc := utils.NewResourceConfig(false) // Skip mode
				_ = rc.Parse("v1/Secret")            // Skip secrets
				return rc
			}(),
		},
		{
			name: "discovers apps group resources",
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "apps/v1",
					APIResources: []metav1.APIResource{
						testresource.APIResourceDeployment(),
						testresource.APIResourceStatefulSet(),
					},
				},
			},
			resourceConfig: nil,
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
						gv.Version: resourceList.APIResources,
					},
				})
			}
			restMapper := restmapper.NewDiscoveryRESTMapper(groupResources)

			// Create fake informer manager
			fakeInformerManager := &testinformer.FakeManager{
				APIResources: make(map[schema.GroupVersionKind]bool),
			}

			// Track handler additions
			testHandler := cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {},
			}

			// Create ChangeDetector with the interface type
			detector := &ChangeDetector{
				DiscoveryClient: fakeDiscovery,
				RESTMapper:      restMapper,
				InformerManager: fakeInformerManager,
				ResourceConfig:  tt.resourceConfig,
			}

			// Test discoverResources which discovers resources and adds handlers
			detector.discoverResources(testHandler)

			// The main goal is to verify no panics occur during discovery and handler addition
		})
	}
}

func TestChangeDetector_NeedLeaderElection(t *testing.T) {
	detector := &ChangeDetector{}

	// ChangeDetector SHOULD need leader election so only the leader processes events
	if !detector.NeedLeaderElection() {
		t.Error("ChangeDetector should need leader election")
	}
}
