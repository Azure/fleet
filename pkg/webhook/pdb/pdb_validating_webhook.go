/*
Copyright 2026 The KubeFleet Authors.

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

package pdb

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	usrFriendlyDenialErrMsgFmt = "PDBs %s/%s cannot be directly created in the hub cluster due to potential side effects; to place a PDB to member clusters, consider wrapping it in a resource envelope."
)

var (
	// ValidationPath is the webhook service path which admission requests are routed to for validating PodDisruptionBudget resources.
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, policyv1.SchemeGroupVersion.Group, policyv1.SchemeGroupVersion.Version, "poddisruptionbudget")
)

// Add registers the webhook for K8s built-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &pdbValidator{admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

type pdbValidator struct {
	decoder webhook.AdmissionDecoder
}

// Handle pdbValidator denies a PodDisruptionBudget if it is not created in the system namespaces.
func (v *pdbValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	if req.Operation == admissionv1.Create {
		klog.V(2).InfoS("handling PodDisruptionBudget resource", "operation", req.Operation, "subResource", req.SubResource, "namespacedName", namespacedName)
		pdb := &policyv1.PodDisruptionBudget{}
		if err := v.decoder.Decode(req, pdb); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if !utils.IsReservedNamespace(pdb.Namespace) {
			klog.V(2).InfoS("Denying creation of PDBs in non-reserved namespaces",
				"user", req.UserInfo.Username, "groups", req.UserInfo.Groups,
				"operation", req.Operation,
				"GVK", req.RequestKind,
				"subResource", req.SubResource, "namespacedName", namespacedName)
			return admission.Denied(fmt.Sprintf(usrFriendlyDenialErrMsgFmt, pdb.Namespace, pdb.Name))
		}
	}
	klog.V(3).InfoS("Allowing operations on PDBs",
		"user", req.UserInfo.Username, "groups", req.UserInfo.Groups,
		"operation", req.Operation,
		"GVK", req.RequestKind,
		"subResource", req.SubResource, "namespacedName", namespacedName)
	return admission.Allowed("")
}
