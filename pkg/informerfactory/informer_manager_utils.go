package informerfactory

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"go.goms.io/fleet/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"strings"
	"time"
)

func newGenericInformerFromGVK(config *rest.Config, namespace string, gvk schema.GroupVersionKind, resyncPeriod time.Duration, resourceEventHandlerFuncs *cache.ResourceEventHandlerFuncs) (cache.Store, cache.Controller, error) {
	dc, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}
	resource := schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: PluralName(gvk.Kind),
	}
	var client dynamic.ResourceInterface
	if namespace == corev1.NamespaceAll {
		client = dc.Resource(resource)
	} else {
		client = dc.Resource(resource).Namespace(namespace)
	}
	// use unstructured for a generic resource
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	store, controller := cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				return client.List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				// Watch needs to be set to true separately
				opts.Watch = true
				return client.Watch(context.TODO(), opts)
			},
		},
		obj,
		resyncPeriod,
		resourceEventHandlerFuncs,
	)
	return store, controller, nil
}

// PluralName computes the plural name from the kind by
// lowercasing and suffixing with 's' or `es`.
func PluralName(kind string) string {
	lowerKind := strings.ToLower(kind)
	if strings.HasSuffix(lowerKind, "s") || strings.HasSuffix(lowerKind, "x") ||
		strings.HasSuffix(lowerKind, "ch") || strings.HasSuffix(lowerKind, "sh") ||
		strings.HasSuffix(lowerKind, "z") || strings.HasSuffix(lowerKind, "o") {
		return fmt.Sprintf("%ses", lowerKind)
	}
	if strings.HasSuffix(lowerKind, "y") {
		lowerKind = strings.TrimSuffix(lowerKind, "y")
		return fmt.Sprintf("%sies", lowerKind)
	}
	return fmt.Sprintf("%ss", lowerKind)
}

func NewGenericInformerWithEventHandler(config *rest.Config, namespace string, obj runtime.Object, resyncPeriod time.Duration, resourceEventHandlerFuncs *cache.ResourceEventHandlerFuncs) (cache.Store, cache.Controller, error) {
	gvk, err := apiutil.GVKForObject(obj, v1alpha1.Scheme)
	if err != nil {
		return nil, nil, err
	}

	mapper, err := apiutil.NewDiscoveryRESTMapper(config)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Could not create RESTMapper from config")
	}

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, nil, err
	}

	client, err := apiutil.RESTClientForGVK(gvk, false, config, v1alpha1.Codecs)
	if err != nil {
		return nil, nil, err
	}

	listGVK := gvk.GroupVersion().WithKind(gvk.Kind + "List")
	listObj, err := v1alpha1.Scheme.New(listGVK)
	if err != nil {
		return nil, nil, err
	}

	store, controller := cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				res := listObj.DeepCopyObject()
				isNamespaceScoped := namespace != "" && mapping.Scope.Name() != meta.RESTScopeNameRoot
				err := client.Get().NamespaceIfScoped(namespace, isNamespaceScoped).Resource(mapping.Resource.Resource).VersionedParams(&opts, v1alpha1.ParameterCodec).Do(context.Background()).Into(res)
				return res, err
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				// Watch needs to be set to true separately
				opts.Watch = true
				isNamespaceScoped := namespace != "" && mapping.Scope.Name() != meta.RESTScopeNameRoot
				return client.Get().NamespaceIfScoped(namespace, isNamespaceScoped).Resource(mapping.Resource.Resource).VersionedParams(&opts, v1alpha1.ParameterCodec).Watch(context.Background())
			},
		},
		obj,
		resyncPeriod,
		resourceEventHandlerFuncs,
	)
	return store, controller, nil
}
