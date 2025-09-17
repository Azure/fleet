/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package managedresource

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func EnsureNoVAP(ctx context.Context, c client.Client, isHub bool) error {
	objs := []client.Object{GetValidatingAdmissionPolicy(isHub), GetValidatingAdmissionPolicyBinding()}
	for _, obj := range objs {
		err := c.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			if meta.IsNoMatchError(err) {
				klog.Infof("ValidatingAdmissionPolicy is not supported in this cluster, skipping deletion")
				return nil
			}
			return err
		}
	}
	return nil
}

func EnsureVAP(ctx context.Context, c client.Client, isHub bool) error {
	objs := []client.Object{GetValidatingAdmissionPolicy(isHub), GetValidatingAdmissionPolicyBinding()}
	for _, obj := range objs {
		opResult, err := controllerutil.CreateOrUpdate(ctx, c, obj, nil)
		if err != nil {
			if meta.IsNoMatchError(err) {
				klog.Infof("ValidatingAdmissionPolicy is not supported in this cluster, skipping creation")
			}
			klog.Errorf("ValidatingAdmissionPolicy CreateOrUpdate (operation: %) failed: %s", opResult, err)
			return err
		}
	}
	return nil
}
