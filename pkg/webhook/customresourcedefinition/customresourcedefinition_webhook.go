package customresourcedefinition

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/webhook/validation"
)

const (
	// ValidationPath is the webhook service path which admission requests are routed to for validating custom resource definition resources.
	ValidationPath = "/validate-v1-customresourcedefinition"
	groupMatch     = `^[^.]*\.(.*)`
)

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &customResourceDefintionValidator{Client: mgr.GetClient()}})
	return nil
}

type customResourceDefintionValidator struct {
	Client  client.Client
	decoder *admission.Decoder
}

func (v *customResourceDefintionValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		crd := &v1.CustomResourceDefinition{}
		err := v.decoder.Decode(req, crd)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		// Need to check to see if the user is authorized to do the operation.
		if !validation.ValidateUserGroups(req.UserInfo.Groups) {
			return admission.Denied(fmt.Sprintf("failed to validate user %s to modify CRD", req.UserInfo.Username))
		}
		group := regexp.MustCompile(groupMatch).FindStringSubmatch(crd.Name)[1]
		if validation.CheckCRDGroup(group) {
			return admission.Denied(fmt.Sprintf("cannot modify fleet CRDs %s", crd.Name))
		}
	}
	return admission.Allowed("")
}

func (v *customResourceDefintionValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
