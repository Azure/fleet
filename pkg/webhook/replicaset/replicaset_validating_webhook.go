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

package replicaset

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	deniedReplicaSetResource  = "ReplicaSet creation is disallowed in the fleet hub cluster"
	allowedReplicaSetResource = "ReplicaSet creation is allowed in the fleet hub cluster"
	replicaSetDeniedFormat    = "ReplicaSet %s/%s creation is disallowed in the fleet hub cluster."
)

var (
	// ValidationPath is the webhook service path which admission requests are routed to for validating ReplicaSet resources.
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, appsv1.SchemeGroupVersion.Group, appsv1.SchemeGroupVersion.Version, "replicaset")
)

type replicaSetValidator struct {
	decoder webhook.AdmissionDecoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &replicaSetValidator{admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle replicaSetValidator denies all creation requests.
func (v *replicaSetValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	if req.Operation == admissionv1.Create {
		klog.V(2).InfoS("handling replicaSet resource", "operation", req.Operation, "subResource", req.SubResource, "namespacedName", namespacedName)
		rs := &appsv1.ReplicaSet{}
		if err := v.decoder.Decode(req, rs); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if !utils.IsReservedNamespace(rs.Namespace) {
			klog.V(2).InfoS(deniedReplicaSetResource, "user", req.UserInfo.Username, "groups", req.UserInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
			return admission.Denied(fmt.Sprintf(replicaSetDeniedFormat, rs.Namespace, rs.Name))
		}
	}
	klog.V(3).InfoS(allowedReplicaSetResource, "user", req.UserInfo.Username, "groups", req.UserInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
	return admission.Allowed("")
}
