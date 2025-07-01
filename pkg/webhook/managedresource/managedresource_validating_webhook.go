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
	"k8s.io/apimachinery/pkg/api/meta"
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
	managedByArmKey      = "managed-by"
	managedByArmValue    = "arm"
	deniedResource       = "denied admission for managed resource"
	resourceDeniedFormat = "the operation on the managed resource type '%s' name '%s' in namespace '%s' is not allowed"
)

// ValidationPath is the webhook service path which admission requests are routed to.
var (
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, "arm", "managed", "resources")
	metaAccessor   = meta.NewAccessor()
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
	switch req.Operation {
	case admissionv1.Create, admissionv1.Update, admissionv1.Delete:
		klog.V(1).InfoS("handling resource", "operation", req.Operation, "subResource", req.SubResource, "namespacedName", namespacedName)
		for _, obj := range []runtime.Object{req.OldObject.Object, req.Object.Object} {
			labels, annotations, err := getLabelsAndAnnotations(obj)
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}
			if (managedByArm(labels) || managedByArm(annotations)) && !validation.IsAdminGroupUserOrWhiteListedUser(v.whiteListedUsers, req.UserInfo) {
				klog.V(2).InfoS(deniedResource, "user", req.UserInfo.Username, "groups", req.UserInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
				return admission.Denied(fmt.Sprintf(resourceDeniedFormat, req.Kind, req.Name, req.Namespace))
			}
		}
	}
	return admission.Allowed("")
}

func getLabelsAndAnnotations(obj runtime.Object) (map[string]string, map[string]string, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, nil, err
	}
	return accessor.GetLabels(), accessor.GetAnnotations(), nil
}

func managedByArm(m map[string]string) bool {
	if len(m) == 0 {
		return false
	}
	if v, ok := m[managedByArmKey]; ok && v == managedByArmValue {
		return true
	}
	return false
}
