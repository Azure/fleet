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
)

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/validate-apps-v1-replicaset", &webhook.Admission{Handler: &replicaSetValidator{Client: mgr.GetClient()}})
	return nil
}

type replicaSetValidator struct {
	Client  client.Client
	decoder *admission.Decoder
}

// Handle replicaSetValidator denies all creation requests.
func (v *replicaSetValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation == admissionv1.Create {
		rs := &v1.ReplicaSet{}
		err := v.decoder.Decode(req, rs)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		return admission.Denied(fmt.Sprintf("ReplicaSet %s/%s creation is disallowed in the fleet hub cluster", rs.Namespace, rs.Name))
	}
	return admission.Allowed("")
}

// InjectDecoder injects the decoder.
func (v *replicaSetValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
