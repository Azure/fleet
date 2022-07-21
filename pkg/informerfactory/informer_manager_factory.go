package informerfactory

import (
	"fmt"
	"go.goms.io/fleet/apis/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	restclient "k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sync"
)

type InformerFactory interface {
	Resource(obj runtime.Object, apiResource metav1.APIResource) (ResourceInformer, error)
	UnstructuredResource(gvk schema.GroupVersionKind, apiResource metav1.APIResource) (ResourceInformer, error)
	ListFromNamespace(namespace string, labelSelector labels.Selector) ([]interface{}, error)
	ListFromNonNamespaced(name string, labelSelector labels.Selector) ([]interface{}, error)
}

type informerFactoryImpl struct {
	kubeConfig      *restclient.Config
	newInformerFunc NewInformerManagerFunc

	informerManagersLock sync.Mutex
	informerManagers     map[string]informerManagerRecord
}

func NewInformerFactory(config *restclient.Config) InformerFactory {
	factory := &informerFactoryImpl{
		kubeConfig:       config,
		newInformerFunc:  NewInformerManager,
		informerManagers: map[string]informerManagerRecord{},
	}
	return factory
}

func (s *informerFactoryImpl) Resource(obj runtime.Object, apiResource metav1.APIResource) (ResourceInformer, error) {
	gvk, err := apiutil.GVKForObject(obj, v1alpha1.Scheme)
	if err != nil {
		return nil, err
	}
	return s.getOrInitInformer(obj, gvk, apiResource)
}

func (s *informerFactoryImpl) UnstructuredResource(gvk schema.GroupVersionKind, apiResource metav1.APIResource) (ResourceInformer, error) {
	return s.getOrInitInformer(&unstructured.Unstructured{}, gvk, apiResource)
}

func (s *informerFactoryImpl) listNamespaceNames(labelSelector labels.Selector) ([]string, error) {
	rs := make([]string, 0)
	gvk := schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Namespace",
	}
	namespaceInformerManager := s.informerManagers[gvk.String()]
	objects, err := namespaceInformerManager.im.List()
	if err != nil {
		return rs, err
	}
	for _, obj := range objects {
		namespace, ok := obj.(*v1.Namespace)
		if !ok {
			continue
		}
		if labelSelector.Matches(labels.Set(namespace.GetLabels())) {
			rs = append(rs, namespace.Name)
		}
	}
	return rs, nil
}

func (s *informerFactoryImpl) ListFromNonNamespaced(name string, labelSelector labels.Selector) ([]interface{}, error) {
	s.informerManagersLock.Lock()
	defer s.informerManagersLock.Unlock()
	rs := make([]interface{}, 0)

	for _, imRecord := range s.informerManagers {
		if imRecord.apiResource.Namespaced {
			// ignore namespace scoped
			continue
		}
		objects, err := imRecord.im.List()
		if err != nil {
			return rs, err
		}
		for _, obj := range objects {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				klog.Errorf("failed to get accessor from %+v", obj)
				continue
			}

			if len(name) > 0 {
				if accessor.GetName() == name {
					rs = append(rs, obj)
				}
			} else {
				// label selector
				if labelSelector.Matches(labels.Set(accessor.GetLabels())) {
					rs = append(rs, obj)
				}
			}
		}
	}
	return rs, nil
}

func (s *informerFactoryImpl) ListFromNamespace(namespace string, namespaceLabelSelector labels.Selector) ([]interface{}, error) {
	s.informerManagersLock.Lock()
	defer s.informerManagersLock.Unlock()
	rs := make([]interface{}, 0)

	matchedNamespaces := make([]string, 0)
	if len(namespace) == 0 {
		// use namespace label selector
		namespaces, err := s.listNamespaceNames(namespaceLabelSelector)
		matchedNamespaces = append(matchedNamespaces, namespaces...)
		if err != nil {
			return rs, err
		}
	} else {
		matchedNamespaces = append(matchedNamespaces, namespace)
	}

	for _, imRecord := range s.informerManagers {
		if !imRecord.apiResource.Namespaced {
			// ignore cluster scoped
			continue
		}
		objects, err := imRecord.im.List()
		if err != nil {
			return rs, err
		}
		for _, obj := range objects {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				klog.Errorf("failed to get accessor from %+v", obj)
				continue
			}
			for _, ns := range matchedNamespaces {
				if accessor.GetNamespace() == ns {
					rs = append(rs, obj)
					break
				}
			}
		}
	}
	return rs, nil
}

func (s *informerFactoryImpl) getOrInitInformer(obj runtime.Object, gvk schema.GroupVersionKind, apiResource metav1.APIResource) (ResourceInformer, error) {
	s.informerManagersLock.Lock()
	defer s.informerManagersLock.Unlock()
	if im, ok := s.informerManagers[gvk.String()]; ok {
		return im.im, nil
	}
	// we dont have yet, create one
	klog.Infof("creating informer manager for resource %s", gvk.String())
	informerManager, err := s.newInformerFunc(obj, gvk, s.kubeConfig)
	if err != nil {
		klog.Errorf("failed to create informer manager for resource %s: err %+v", gvk.String(), err)
		return nil, fmt.Errorf("failed to create informer manager for resource %s", gvk.String())
	}
	stopCh := make(chan struct{})
	informerManager.Start(stopCh)

	s.informerManagers[gvk.String()] = informerManagerRecord{
		im:          informerManager,
		apiResource: apiResource,
		stopCh:      stopCh,
	}
	return informerManager, nil
}

type NewInformerManagerFunc func(obj runtime.Object, gvk schema.GroupVersionKind, config *restclient.Config) (InformerManager, error)
