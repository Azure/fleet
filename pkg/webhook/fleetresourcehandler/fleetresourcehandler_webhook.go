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

func (v *fleetResourceValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	var response admission.Response
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		switch req.Kind.String() {
		case retrieveCRDGVK():
			klog.V(2).InfoS("handling CRD resource", "GVK", retrieveCRDGVK())
			response = v.handleCRD(req)
		default:
			klog.V(2).InfoS("resource is not monitored by fleet resource validator webhook", "GVK", req.Kind.String())
			response = admission.Allowed("")
		}
	}
	return response
}

func (v *fleetResourceValidator) handleCRD(req admission.Request) admission.Response {
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

	group := regexp.MustCompile(groupMatch).FindStringSubmatch(crd.Name)[1]
	if validation.CheckCRDGroup(group) && !validation.ValidateUserForCRD(req.UserInfo) {
		return admission.Denied(fmt.Sprintf("failed to validate user: %s in groups: %v to modify fleet CRD: %s", req.UserInfo.Username, req.UserInfo.Groups, crd.Name))
	}
	return admission.Allowed("")
}

func retrieveCRDGVK() string {
	var crd v1.CustomResourceDefinition
	crd.APIVersion = v1.SchemeGroupVersion.Group + "/" + v1.SchemeGroupVersion.Version
	crd.Kind = "CustomResourceDefinition"
	return crd.GroupVersionKind().String()
}

func (v *fleetResourceValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
