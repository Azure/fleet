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

// Package deployment implements admission webhooks for Deployment resources.
// The mutating webhook injects the "fleet.azure.com/reconcile=managed" label on
// deployments created by the aksService user. The validating webhook prevents
// non-aksService users from setting this label, which would otherwise bypass
// pod and replicaset admission checks.
package deployment

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

	"go.goms.io/fleet/pkg/utils"
)

const (
	deniedReconcileLabelFmt = "the %s label is reserved for aksService and cannot be set by user %q"
)

// ValidationPath is the webhook service path for validating Deployment resources.
var ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, appsv1.SchemeGroupVersion.Group, appsv1.SchemeGroupVersion.Version, "deployment")

type deploymentValidator struct {
	decoder webhook.AdmissionDecoder
}

// Add registers the validating webhook for Deployments with the manager.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &deploymentValidator{decoder: admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle rejects deployments that carry the fleet reconcile label unless the
// request was made by the aksService user with system:masters group membership.
func (v *deploymentValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	klog.V(2).InfoS("handling deployment validating webhook",
		"operation", req.Operation, "namespacedName", namespacedName, "user", req.UserInfo.Username)

	// Only validate CREATE and UPDATE operations.
	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Allowed("operation is not CREATE or UPDATE, no validation needed")
	}

	var deploy appsv1.Deployment
	if err := v.decoder.Decode(req, &deploy); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check whether the reconcile label is present on either the Deployment
	// metadata or the pod template metadata.
	hasLabelOnDeploy := utils.HasReconcileLabel(deploy.Labels)
	hasLabelOnPodTemplate := utils.HasReconcileLabel(deploy.Spec.Template.Labels)

	if !hasLabelOnDeploy && !hasLabelOnPodTemplate {
		return admission.Allowed("deployment does not have the reconcile label, no validation needed")
	}

	// The reconcile label is present — only aksService with system:masters is
	// allowed to set it.
	if utils.IsAKSService(req.UserInfo) {
		klog.V(2).InfoS("aksService user allowed to set reconcile label",
			"namespacedName", namespacedName)
		return admission.Allowed("aksService user is allowed to set the reconcile label")
	}

	klog.V(2).InfoS("denied non-aksService user from setting reconcile label",
		"user", req.UserInfo.Username, "groups", req.UserInfo.Groups, "namespacedName", namespacedName)
	return admission.Denied(fmt.Sprintf(deniedReconcileLabelFmt, utils.ReconcileLabelKey, req.UserInfo.Username))
}
