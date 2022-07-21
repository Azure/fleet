package informerfactory

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"sync"
	"time"
)

type ResourceEventType string

const (
	addObject    ResourceEventType = "AddObject"
	updateObject ResourceEventType = "UpdateObject"
	deleteObject ResourceEventType = "DeleteObject"
)

type InformerManager interface {
	ResourceInformer
	Start(stopCh <-chan struct{})
	Stop()
}

func NewInformerManager(obj runtime.Object, gvk schema.GroupVersionKind, config *rest.Config) (InformerManager, error) {
	impl := &informerManagerImpl{
		obj: obj,
		gvk: gvk,
		wq: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(),
			fmt.Sprintf("informer-%s-dispatcher", gvk.Kind)),
	}
	store, controller, err := impl.defaultNewInformerFunc(config, v1.NamespaceAll, obj,
		10*time.Minute, &cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				impl.wq.Add(&ResourceEvent{
					eType:  addObject,
					newObj: obj,
					oldObj: nil,
				})
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				impl.wq.Add(&ResourceEvent{
					eType:  updateObject,
					oldObj: oldObj,
					newObj: newObj,
				})
			},
			DeleteFunc: func(obj interface{}) {
				impl.wq.Add(&ResourceEvent{
					eType:  deleteObject,
					oldObj: obj,
					newObj: nil,
				})
			},
		})
	if err != nil {
		klog.Errorf("failed to add informer: %+v", err)
		return impl, err
	}

	informerObj := informer{
		store:      store,
		controller: controller,
		stopCh:     make(chan struct{}),
	}

	klog.Infof("starting informer for %+v", impl.gvk)
	impl.informerObj = informerObj

	go informerObj.controller.Run(informerObj.stopCh)
	return impl, nil
}

type informerManagerImpl struct {
	obj runtime.Object
	gvk schema.GroupVersionKind

	informerObj informer
	newInformer NewInformerFunc
	stopCh      <-chan struct{}

	wq               workqueue.RateLimitingInterface
	eventHandlerLock sync.Mutex
	eventHandlers    []ResourceEventHandler
}

func (im *informerManagerImpl) Start(stopCh <-chan struct{}) {
	im.stopCh = stopCh
	go func() {
		wait.Until(func() {
			klog.Infof("Starting informer event dispatch loop for resource %s", im.gvk.String())
			for im.dispatchEvent() {

			}
		}, time.Second, stopCh)
	}()
}

func (im *informerManagerImpl) Stop() {
	klog.Infof("Shutting down resource %s informer", im.gvk)
	im.wq.ShutDown()
	close(im.informerObj.stopCh)
}

func (im *informerManagerImpl) TargetGVK() schema.GroupVersionKind {
	return im.gvk
}

func (im *informerManagerImpl) AddResourceEventHandler(handler ResourceEventHandler) {
	im.eventHandlerLock.Lock()
	defer im.eventHandlerLock.Unlock()
	klog.Infof("adding event handler %s for resource %s", handler.Name(), im.gvk.String())
	im.eventHandlers = append(im.eventHandlers, handler)
}

func (im *informerManagerImpl) GetByKey(key string) (interface{}, bool, error) {
	synced := cache.WaitForCacheSync(im.stopCh, im.informerObj.controller.HasSynced)
	if !synced {
		klog.Warningf("failed to get informer %s, has not synced", im.gvk.String())
		return nil, false, fmt.Errorf("informer for %s has not synced", im.gvk.String())
	}
	return im.informerObj.store.GetByKey(key)
}

func (im *informerManagerImpl) List() ([]interface{}, error) {
	synced := cache.WaitForCacheSync(im.stopCh, im.informerObj.controller.HasSynced)
	if !synced {
		klog.Warningf("failed to get informer %s, has not synced", im.gvk.String())
		return nil, fmt.Errorf("informer for %s not synced", im.gvk.String())
	}
	return im.informerObj.store.List(), nil
}

func (im *informerManagerImpl) dispatchEvent() bool {
	item, quit := im.wq.Get()
	if quit {
		klog.Infof("InformerManager for %s shutting down", im.gvk.String())
		return false
	}
	defer func() {
		im.wq.Done(item)
	}()
	event := item.(*ResourceEvent)

	if ok := cache.WaitForCacheSync(im.stopCh, im.informerObj.controller.HasSynced); !ok {
		return false
	}

	im.eventHandlerLock.Lock()
	defer im.eventHandlerLock.Unlock()

	switch event.eType {
	case addObject:
		for _, handler := range im.eventHandlers {
			handler.OnAdd(event.newObj)
		}
	case updateObject:
		for _, handler := range im.eventHandlers {
			handler.OnUpdate(event.oldObj, event.newObj)
		}
	case deleteObject:
		for _, handler := range im.eventHandlers {
			handler.OnDelete(event.oldObj)
		}
	default:
		klog.Errorf("event can not be recognized: %+v", event)
	}
	return true
}

type informer struct {
	store      cache.Store
	controller cache.Controller
	stopCh     chan struct{}
}

type informerManagerRecord struct {
	im          InformerManager
	apiResource metav1.APIResource
	stopCh      chan struct{}
}

type ResourceEvent struct {
	eType  ResourceEventType
	gvk    schema.GroupVersionKind
	oldObj interface{}
	newObj interface{}
}

type ResourceEventHandler interface {
	Name() string
	OnAdd(newObj interface{})
	OnUpdate(oldObj, newObj interface{})
	OnDelete(oldObj interface{})
}

type ResourceInformer interface {
	TargetGVK() schema.GroupVersionKind
	AddResourceEventHandler(handler ResourceEventHandler)
	GetByKey(key string) (interface{}, bool, error)
	List() ([]interface{}, error)
}

type NewInformerFunc func(config *rest.Config, namespace string, obj runtime.Object, resyncPeriod time.Duration,
	resourceEventHandlerFuncs *cache.ResourceEventHandlerFuncs) (cache.Store, cache.Controller, error)

func (c *informerManagerImpl) defaultNewInformerFunc(config *rest.Config, namespace string, obj runtime.Object, resyncPeriod time.Duration,
	resourceEventHandlerFuncs *cache.ResourceEventHandlerFuncs) (cache.Store, cache.Controller, error) {
	if _, ok := obj.(*unstructured.Unstructured); ok {
		// an untyped generic informer is requested
		return newGenericInformerFromGVK(config, namespace, c.gvk, resyncPeriod, resourceEventHandlerFuncs)
	}
	return NewGenericInformerWithEventHandler(config, namespace, obj, resyncPeriod, resourceEventHandlerFuncs)
}
