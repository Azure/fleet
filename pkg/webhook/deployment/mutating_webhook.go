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

package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

// MutatingPath is the webhook service path for mutating Deployment resources.
var MutatingPath = fmt.Sprintf(utils.MutatingPathFmt, appsv1.SchemeGroupVersion.Group, appsv1.SchemeGroupVersion.Version, "deployment")

type deploymentMutator struct {
	decoder webhook.AdmissionDecoder
}

// AddMutating registers the mutating webhook for Deployments with the manager.
func AddMutating(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(MutatingPath, &webhook.Admission{Handler: &deploymentMutator{decoder: admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle injects the fleet.azure.com/reconcile=managed label onto the
// Deployment and its pod template when the request originated from the aksService user.
func (m *deploymentMutator) Handle(_ context.Context, req admission.Request) admission.Response {
	klog.V(2).InfoS("handling deployment mutating webhook",
		"operation", req.Operation, "namespace", req.Namespace, "name", req.Name, "user", req.UserInfo.Username)

	// Pass through requests targeting reserved system namespaces.
	if utils.IsReservedNamespace(req.Namespace) {
		return admission.Allowed(fmt.Sprintf("namespace %s is a reserved system namespace, no mutation needed", req.Namespace))
	}

	// Only mutate when the request was made by the aksService user with
	// system:masters group membership.
	if !utils.IsAKSService(req.UserInfo) {
		return admission.Allowed("user is not aksService, no mutation needed")
	}

	var deploy appsv1.Deployment
	if err := m.decoder.Decode(req, &deploy); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Add the reconcile label on the Deployment metadata.
	if deploy.Labels == nil {
		deploy.Labels = map[string]string{}
	}
	deploy.Labels[utils.ReconcileLabelKey] = utils.ReconcileLabelValue

	// Add the reconcile label on the pod template metadata.
	if deploy.Spec.Template.Labels == nil {
		deploy.Spec.Template.Labels = map[string]string{}
	}
	deploy.Spec.Template.Labels[utils.ReconcileLabelKey] = utils.ReconcileLabelValue

	marshaled, err := json.Marshal(deploy)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	klog.V(2).InfoS("mutated deployment with reconcile label",
		"operation", req.Operation, "namespace", req.Namespace, "name", req.Name)
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}
