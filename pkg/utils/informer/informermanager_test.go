package informer

import (
	"context"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"
)

func TestAddDynamicResources(t *testing.T) {
	tests := map[string]struct {
		dynResources  []APIResourceMeta
		dynamicClient *fake.FakeDynamicClient
	}{
		"add one dynamic resource": {
			dynResources: []APIResourceMeta{
				{
					GroupVersionKind: schema.GroupVersionKind{
						Group:   "example.com",
						Version: "v1",
						Kind:    "ExampleResource",
					},
					GroupVersionResource: schema.GroupVersionResource{
						Group:    "example.com",
						Version:  "v1",
						Resource: "exampleresources",
					},
					IsClusterScoped:  true,
					isStaticResource: false,
					Registration:     nil,
				},
			},
			dynamicClient: fake.NewSimpleDynamicClient(runtime.NewScheme()),
		},
		"add multiple resources": {
			dynResources: []APIResourceMeta{
				{
					GroupVersionKind: schema.GroupVersionKind{
						Group:   "example.com",
						Version: "v1",
						Kind:    "ExampleResource",
					},
					GroupVersionResource: schema.GroupVersionResource{
						Group:    "example.com",
						Version:  "v1",
						Resource: "exampleresources",
					},
					IsClusterScoped:  true,
					isStaticResource: false,
					Registration:     nil,
				},
				{
					GroupVersionKind: schema.GroupVersionKind{
						Group:   "anotherexample.com",
						Version: "v1",
						Kind:    "AnotherExampleResource",
					},
					GroupVersionResource: schema.GroupVersionResource{
						Group:    "anotherexample.com",
						Version:  "v1",
						Resource: "anotherexampleresources",
					},
					IsClusterScoped:  true,
					isStaticResource: false,
					Registration:     nil,
				},
			},
		},
	}
	for testName, tc := range tests {
		t.Run(testName, func(t *testing.T) {
			// Create a resource event handler
			handler := &cache.ResourceEventHandlerFuncs{
				AddFunc:    func(obj interface{}) {},
				UpdateFunc: func(oldObj, newObj interface{}) {},
				DeleteFunc: func(obj interface{}) {},
			}

			// Create a fake dynamic client and informer factory
			client := fake.NewSimpleDynamicClient(runtime.NewScheme())
			factory := dynamicinformer.NewDynamicSharedInformerFactory(client, 0)

			// Create a new informerManagerImpl with the fake client and factory
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			impl := &informerManagerImpl{
				dynamicClient:   client,
				ctx:             ctx,
				cancel:          cancel,
				informerFactory: factory,
				apiResources:    make(map[schema.GroupVersionKind]*APIResourceMeta),
				resourcesLock:   sync.RWMutex{},
			}
			// Add the dynamic resource to the informer manager
			impl.AddDynamicResources(tc.dynResources, handler, true)
			for _, dynResource := range tc.dynResources {
				if _, ok := impl.apiResources[dynResource.GroupVersionKind]; !ok {
					t.Errorf("Expected dynamic resource %v to be added to informer manager", dynResource)
				}
			}

			// Check that the informer was created for the dynamic resources
			for _, dynResource := range tc.dynResources {
				informer := impl.informerFactory.ForResource(dynResource.GroupVersionResource).Informer()
				if informer == nil {
					t.Fatalf("Expected informer to be created for resource %v", dynResource)
				}
			}

			//Remove the dynamic resource from the informer manager
			impl.AddDynamicResources([]APIResourceMeta{}, handler, true)
			for _, dynResource := range tc.dynResources {
				if _, ok := impl.apiResources[dynResource.GroupVersionKind]; ok {
					t.Errorf("Expected dynamic resource %v to be removed to informer manager", dynResource)
				}
			}
		})
	}
}
