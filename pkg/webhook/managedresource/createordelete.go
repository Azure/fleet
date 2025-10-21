/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package managedresource

import (
	"context"

	v1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func EnsureNoVAP(ctx context.Context, c client.Client) error {
	objs := []client.Object{getValidatingAdmissionPolicy(), getValidatingAdmissionPolicyBinding()}
	for _, obj := range objs {
		err := c.Delete(ctx, obj)
		switch {
		case err == nil, apierrors.IsNotFound(err):
			// continue
		case meta.IsNoMatchError(err):
			klog.Infof("object type %T is not supported in this cluster, continuing", obj)
			// continue
		default:
			klog.Errorf("Delete object type %T failed: %s", obj, err)
			return err
		}
	}
	return nil
}

func EnsureVAP(ctx context.Context, c client.Client) error {
	type vapObjectAndMutator struct {
		obj    client.Object
		mutate func() error
	}
	// TODO: this can be simplified by dealing with the specific type rather than using client.Object
	vap, mutateVAP := getVAPWithMutator()
	vapb, mutateVAPB := getVAPBindingWithMutator()
	objsAndMutators := []vapObjectAndMutator{
		{
			obj:    vap,
			mutate: mutateVAP,
		},
		{
			obj:    vapb,
			mutate: mutateVAPB,
		},
	}

	for _, objectMutator := range objsAndMutators {
		opResult, err := controllerutil.CreateOrUpdate(ctx, c, objectMutator.obj, objectMutator.mutate)
		switch {
		case err == nil:
			// continue
		case meta.IsNoMatchError(err):
			klog.Infof("object type %T is not supported in this cluster, continuing", objectMutator.obj)
			// continue
		default:
			klog.Errorf("CreateOrUpdate (operation: %s) for object type %T failed: %s", opResult, objectMutator.obj, err)
			return err
		}
	}
	return nil
}

func getVAPWithMutator() (*v1.ValidatingAdmissionPolicy, func() error) {
	vap := getValidatingAdmissionPolicy()
	return vap, func() error {
		mutateValidatingAdmissionPolicy(vap)
		return nil
	}
}

func getVAPBindingWithMutator() (*v1.ValidatingAdmissionPolicyBinding, func() error) {
	vapb := getValidatingAdmissionPolicyBinding()
	return vapb, func() error {
		mutateValidatingAdmissionPolicyBinding(vapb)
		return nil
	}
}
