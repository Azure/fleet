/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package replicaset

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

var (
	// ValidationPath is the webhook service path which admission requests are routed to for validating ReplicaSet resources.
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, appsv1.SchemeGroupVersion.Group, appsv1.SchemeGroupVersion.Version, "replicaset")
)

type replicaSetValidator struct {
	decoder *admission.Decoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &replicaSetValidator{admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle replicaSetValidator denies all creation requests.
func (v *replicaSetValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	if req.Operation == admissionv1.Create {
		rs := &appsv1.ReplicaSet{}
		if err := v.decoder.Decode(req, rs); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if !utils.IsReservedNamespace(rs.Namespace) {
			return admission.Denied(fmt.Sprintf("ReplicaSet %s/%s creation is disallowed in the fleet hub cluster.", rs.Namespace, rs.Name))
		}
	}
	return admission.Allowed("")
}
