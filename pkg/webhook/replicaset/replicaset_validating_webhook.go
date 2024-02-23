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
	v1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

const (
	// ValidationPath is the webhook service path which admission requests are routed to for validating ReplicaSet resources.
	ValidationPath = "/validate-apps-v1-replicaset"
)

type replicaSetValidator struct {
	Client  client.Client
	decoder *admission.Decoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	handler := &replicaSetValidator{
		Client:  mgr.GetClient(),
		decoder: admission.NewDecoder(mgr.GetScheme()),
	}
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: handler})
	return nil
}

// Handle replicaSetValidator denies all creation requests.
func (v *replicaSetValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	if req.Operation == admissionv1.Create {
		rs := &v1.ReplicaSet{}
		if err := v.decoder.Decode(req, rs); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if !utils.IsReservedNamespace(rs.Namespace) {
			return admission.Denied(fmt.Sprintf("ReplicaSet %s/%s creation is disallowed in the fleet hub cluster.", rs.Namespace, rs.Name))
		}
	}
	return admission.Allowed("")
}
