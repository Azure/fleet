/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package pod

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// ValidationPath is the webhook service path which admission requests are routed to for validating Pod resources.
	ValidationPath = "/validate-v1-pod"
)

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &podValidator{Client: mgr.GetClient()}})
	return nil
}

type podValidator struct {
	Client  client.Client
	decoder *admission.Decoder
}

// Handle podValidator denies a pod if it is not created in the system namespaces.
func (v *podValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation == admissionv1.Create {
		pod := &corev1.Pod{}
		err := v.decoder.Decode(req, pod)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if pod.Namespace != "kube-system" && pod.Namespace != "fleet-system" {
			return admission.Denied(fmt.Sprintf("Pod %s/%s creation is disallowed in the fleet hub cluster", pod.Namespace, pod.Name))
		}
	}
	return admission.Allowed("")
}

// InjectDecoder injects the decoder.
func (v *podValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
