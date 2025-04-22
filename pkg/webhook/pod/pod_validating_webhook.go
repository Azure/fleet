/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pod

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

const (
	deniedPodResource  = "Pod creation is disallowed in the fleet hub cluster"
	allowedPodResource = "Pod creation is allowed in the fleet hub cluster"
	podDeniedFormat    = "Pod %s/%s creation is disallowed in the fleet hub cluster"
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
	decoder webhook.AdmissionDecoder
}

// Handle podValidator denies a pod if it is not created in the system namespaces.
func (v *podValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	if req.Operation == admissionv1.Create {
		klog.V(2).InfoS("handling pod resource", "operation", req.Operation, "subResource", req.SubResource, "namespacedName", namespacedName)
		pod := &corev1.Pod{}
		err := v.decoder.Decode(req, pod)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if !utils.IsReservedNamespace(pod.Namespace) {
			klog.V(2).InfoS(deniedPodResource, "user", req.UserInfo.Username, "groups", req.UserInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
			return admission.Denied(fmt.Sprintf(podDeniedFormat, pod.Namespace, pod.Name))
		}
	}
	klog.V(3).InfoS(allowedPodResource, "user", req.UserInfo.Username, "groups", req.UserInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
	return admission.Allowed("")
}
