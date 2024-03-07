package informer

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

var _ = Describe("Informer Manager Suite", func() {
	Context("add then remove", Ordered, func() {
		It("add one dynamic resource", func() {
			//Create a resource event handler
			handler := &cache.ResourceEventHandlerFuncs{
				AddFunc:    func(obj interface{}) {},
				UpdateFunc: func(oldObj, newObj interface{}) {},
				DeleteFunc: func(obj interface{}) {},
			}

			// Create a dynamic resource
			dynResource := APIResourceMeta{
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
			}

			// Make sure that the add method returns with no errors
			//Check that the dynamic resource was added to the informer manager
			impl.AddDynamicResources([]APIResourceMeta{dynResource}, handler, true)
			addedResource, ok := impl.apiResources[dynResource.GroupVersionKind]
			Expect(ok).Should(BeTrue(), "Expected dynamic resource %v to be added to informer manager", dynResource)

			// Check that the informer was created for the dynamic resource
			informer := impl.informerFactory.ForResource(addedResource.GroupVersionResource).Informer()
			Expect(informer).ShouldNot(BeNil())

			// Remove the dynamic resource from the informer manager
			impl.AddDynamicResources([]APIResourceMeta{}, handler, true)

			// Verify the map. Check that the dynamic resource was removed from the informer manager
			_, ok = impl.apiResources[dynResource.GroupVersionKind]
			Expect(ok).ShouldNot(BeTrue(), "Expected dynamic resource %v to be removed from informer manager", dynResource)
		})
		It("multiple dynamic resources", func() {
			handler := &cache.ResourceEventHandlerFuncs{
				AddFunc:    func(obj interface{}) {},
				UpdateFunc: func(oldObj, newObj interface{}) {},
				DeleteFunc: func(obj interface{}) {},
			}

			// Create dynamic resources
			dynResources := []APIResourceMeta{
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
			}

			// Make sure that the add method returns with no errors
			// Check that the dynamic resources were added to the informer manager
			impl.AddDynamicResources([]APIResourceMeta{dynResources[0]}, handler, false)
			impl.AddDynamicResources(dynResources, handler, true)
			for _, dynResource := range dynResources {
				_, ok := impl.apiResources[dynResource.GroupVersionKind]
				Expect(ok).Should(BeTrue(), "Expected dynamic resource %v to be added to informer manager", dynResource)
			}

			// Check that the informer was created for the dynamic resources
			for _, dynResource := range dynResources {
				informer := impl.informerFactory.ForResource(dynResource.GroupVersionResource).Informer()
				Expect(informer).ShouldNot(BeNil())
			}

			// Remove the dynamic resource from the informer manager
			impl.AddDynamicResources([]APIResourceMeta{dynResources[0]}, handler, true)

			// verify the map. Check that the dynamic resource was removed from the informer manager
			_, ok := impl.apiResources[dynResources[1].GroupVersionKind]
			Expect(ok).ShouldNot(BeTrue(), "Expected dynamic resource %v to be removed from informer manager", dynResources[1])
			// Remove the dynamic resource from the informer manager
			impl.AddDynamicResources([]APIResourceMeta{}, handler, true)

			// verify the map. Check that the dynamic resource was removed from the informer manager
			_, ok = impl.apiResources[dynResources[0].GroupVersionKind]
			Expect(ok).ShouldNot(BeTrue(), "Expected dynamic resource %v to be removed from informer manager", dynResources[0])
		})
	})
})
