package fleetresourcehandler

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/webhook/validation"
)

const (
	// ValidationPath is the webhook service path which admission requests are routed to for validating custom resource definition resources.
	ValidationPath = "/validate-v1-fleetresourcehandler"
	groupMatch     = `^[^.]*\.(.*)`
	crdGVK         = "apiextensions.k8s.io/v1, Kind=CustomResourceDefinition"
)

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &fleetResourceValidator{Client: mgr.GetClient()}})
	return nil
}

type fleetResourceValidator struct {
	Client  client.Client
	decoder *admission.Decoder
}

func (v *fleetResourceValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	var response admission.Response
	klog.V(2).InfoS("GVKs", "request GVK", req.Kind.String(), "crd GVK", crdGVK)
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		switch req.Kind.String() {
		case crdGVK:
			klog.V(2).InfoS("handling CRD resource", "crdGVK", crdGVK)
			response = v.handleCRD(ctx, req)
		default:
			klog.V(2).InfoS("resource is not monitored by fleet resource validator webhook")
			response = admission.Allowed("")
		}
	}
	return response
}

func (v *fleetResourceValidator) handleCRD(ctx context.Context, req admission.Request) admission.Response {
	var crd v1.CustomResourceDefinition
	if req.Operation == admissionv1.Delete {
		// req.Object is not populated for delete: https://github.com/kubernetes-sigs/controller-runtime/issues/1762.
		if err := v.decoder.DecodeRaw(req.OldObject, &crd); err != nil {
			klog.ErrorS(err, "failed to decode old request object for delete operation", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
			return admission.Errored(http.StatusBadRequest, err)
		}
	} else {
		if err := v.decoder.Decode(req, &crd); err != nil {
			klog.ErrorS(err, "failed to decode request object for create/update operation", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	// Need to check to see if the user is authenticated to do the operation.
	if err := validation.ValidateUser(ctx, v.Client, req.UserInfo); err != nil {
		return admission.Denied(fmt.Sprintf("failed to validate user %s in groups: %v to modify CRD", req.UserInfo.Username, req.UserInfo.Groups))
	}
	klog.V(2).InfoS("successfully validated the user", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)

	group := regexp.MustCompile(groupMatch).FindStringSubmatch(crd.Name)[1]
	if validation.CheckCRDGroup(group) {
		return admission.Denied(fmt.Sprintf("user: %s in groups: %v cannot modify fleet CRD %s", req.UserInfo.Username, req.UserInfo.Groups, crd.Name))
	}
	klog.V(2).InfoS("successfully validated the CRD group", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
	return admission.Allowed("")
}

func (v *fleetResourceValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
