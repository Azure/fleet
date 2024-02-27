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
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

var (
	// ValidationPath is the webhook service path which admission requests are routed to for validating Pod resources.
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, corev1.SchemeGroupVersion.Group, corev1.SchemeGroupVersion.Version, "pod")
)

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &podValidator{admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

type podValidator struct {
	decoder *admission.Decoder
}

// Handle podValidator denies a pod if it is not created in the system namespaces.
func (v *podValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	if req.Operation == admissionv1.Create {
		pod := &corev1.Pod{}
		err := v.decoder.Decode(req, pod)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if !utils.IsReservedNamespace(pod.Namespace) {
			return admission.Denied(fmt.Sprintf("Pod %s/%s creation is disallowed in the fleet hub cluster", pod.Namespace, pod.Name))
		}
	}
	return admission.Allowed("")
}
