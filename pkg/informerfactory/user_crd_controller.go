package informerfactory

import (
	"context"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/klog"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type UserCRDController struct{}

func (u UserCRDController) Add(mgr manager.Manager) error {
	r := &UserCRDReconciler{
		client:   mgr.GetClient(),
		recorder: mgr.GetEventRecorderFor("user-crd-controller"),
		scheme:   mgr.GetScheme(),
	}
	c, err := controller.New("user-crd-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}
	err = c.Watch(&source.Kind{Type: &v1.CustomResourceDefinition{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}
	return nil
}

type UserCRDReconciler struct {
	client   client.Client
	recorder record.EventRecorder
	scheme   *runtime.Scheme
	cf       InformerFactory
}

func (c *UserCRDReconciler) Reconcile(context context.Context, request reconcile.Request) (reconcile.Result, error) {
	crd := &v1.CustomResourceDefinition{}
	err := c.client.Get(context, request.NamespacedName, crd)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if crd.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}
	// get the gvk from crd
	gvk := schema.GroupVersionKind{
		Group:   crd.Spec.Group,
		Version: crd.Spec.Versions[0].Name,
		Kind:    crd.Spec.Names.Kind,
	}

	klog.Infof("changed crd gvk is %s", gvk.String())
	// todo: start its informer by: c.cf.UnstructuredResource(gvk)
	return reconcile.Result{}, nil
}
