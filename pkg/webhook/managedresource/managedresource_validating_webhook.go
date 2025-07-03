/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package managedresource

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/webhook/validation"
)

const (
	ManagedByArmKey      = "managed-by"
	ManagedByArmValue    = "arm"
	deniedResource       = "denied admission for managed resource"
	resourceDeniedFormat = "the operation on the managed resource type '%s' name '%s' in namespace '%s' is not allowed"
)

// ValidationPath is the webhook service path which admission requests are routed to.
var (
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, "arm", "managed", "resources")
)

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager, whiteListedUsers []string) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &managedResourceValidator{
		whiteListedUsers: whiteListedUsers,
	}})
	return nil
}

type managedResourceValidator struct {
	whiteListedUsers []string
}

// Handle denies the resource admission if the request target object has a label or annotation key "fleet.azure.com".
func (v *managedResourceValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	klog.V(1).InfoS("handling resource", "operation", req.Operation, "subResource", req.SubResource, "namespacedName", namespacedName)

	var objs []runtime.RawExtension
	switch req.Operation {
	case admissionv1.Create:
		objs = append(objs, req.Object)
	case admissionv1.Update:
		objs = append(objs, req.Object, req.OldObject)
	case admissionv1.Delete:
		objs = append(objs, req.OldObject)
	}
	for _, obj := range objs {
		labels, annotations, err := getLabelsAndAnnotations(obj)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		if (managedByArm(labels) || managedByArm(annotations)) && !validation.IsAdminGroupUserOrWhiteListedUser(v.whiteListedUsers, req.UserInfo) {
			klog.V(2).InfoS(deniedResource, "user", req.UserInfo.Username, "groups", req.UserInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
			return admission.Denied(fmt.Sprintf(resourceDeniedFormat, req.Kind, req.Name, req.Namespace))
		}
	}
	return admission.Allowed("")
}

func getLabelsAndAnnotations(raw runtime.RawExtension) (map[string]string, map[string]string, error) {
	var obj runtime.Object
	if err := runtime.Convert_runtime_RawExtension_To_runtime_Object(&raw, &obj, nil); err != nil {
		return nil, nil, err
	}
	o, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, nil, err
	}
	u := unstructured.Unstructured{Object: o}
	return u.GetLabels(), u.GetAnnotations(), nil
}

func managedByArm(m map[string]string) bool {
	if len(m) == 0 {
		return false
	}
	if v, ok := m[ManagedByArmKey]; ok && v == ManagedByArmValue {
		return true
	}
	return false
}
