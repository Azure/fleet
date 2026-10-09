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

// Package job implements admission webhooks for Job resources.
package job

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

const deniedReconcileLabelFmt = "resources with the %s label are reserved for aksService and cannot be modified by user %q"

// ValidationPath is the webhook service path for validating Job resources.
var ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, batchv1.SchemeGroupVersion.Group, batchv1.SchemeGroupVersion.Version, "job")

type jobValidator struct {
	decoder webhook.AdmissionDecoder
}

// Add registers the validating webhook for Jobs with the manager.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &jobValidator{decoder: admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle rejects Jobs that carry the fleet reconcile label unless the request
// was made by the aksService user with system:masters group membership.
func (v *jobValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	klog.V(2).InfoS("handling job validating webhook",
		"operation", req.Operation, "namespacedName", namespacedName, "user", req.UserInfo.Username)

	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Allowed("operation is not CREATE or UPDATE, no validation needed")
	}

	var job batchv1.Job
	if err := v.decoder.Decode(req, &job); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	hasLabelOnJob := utils.HasReconcileLabel(job.Labels)
	hasLabelOnPodTemplate := utils.HasReconcileLabel(job.Spec.Template.Labels)
	if hasLabelOnJob || hasLabelOnPodTemplate {
		if !utils.IsAKSService(req.UserInfo) {
			klog.V(2).InfoS("denied non-aksService user from modifying job with reconcile label",
				"user", req.UserInfo.Username, "groups", req.UserInfo.Groups, "namespacedName", namespacedName)
			return admission.Denied(fmt.Sprintf(deniedReconcileLabelFmt, utils.ReconcileLabelKey, req.UserInfo.Username))
		}
		klog.V(2).InfoS("aksService user allowed to set reconcile label",
			"namespacedName", namespacedName)
		return admission.Allowed("aksService user is allowed to set the reconcile label")
	}

	if req.Operation == admissionv1.Update && !utils.IsAKSService(req.UserInfo) {
		var oldJob batchv1.Job
		if err := v.decoder.DecodeRaw(req.OldObject, &oldJob); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if utils.HasReconcileLabel(oldJob.Labels) || utils.HasReconcileLabel(oldJob.Spec.Template.Labels) {
			klog.V(2).InfoS("denied non-aksService user from removing reconcile label",
				"user", req.UserInfo.Username, "groups", req.UserInfo.Groups, "namespacedName", namespacedName)
			return admission.Denied(fmt.Sprintf(deniedReconcileLabelFmt, utils.ReconcileLabelKey, req.UserInfo.Username))
		}
	}

	return admission.Allowed("job does not have the reconcile label, no validation needed")
}
