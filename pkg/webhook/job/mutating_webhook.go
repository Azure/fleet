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

package job

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

// MutatingPath is the webhook service path for mutating Job resources.
var MutatingPath = fmt.Sprintf(utils.MutatingPathFmt, batchv1.SchemeGroupVersion.Group, batchv1.SchemeGroupVersion.Version, "job")

type jobMutator struct {
	decoder webhook.AdmissionDecoder
}

// AddMutating registers the mutating webhook for Jobs with the manager.
func AddMutating(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(MutatingPath, &webhook.Admission{Handler: &jobMutator{decoder: admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle injects the fleet reconcile label onto the Job and its pod template
// when the request originated from the aksService user.
func (m *jobMutator) Handle(_ context.Context, req admission.Request) admission.Response {
	klog.V(2).InfoS("handling job mutating webhook",
		"operation", req.Operation, "namespace", req.Namespace, "name", req.Name, "user", req.UserInfo.Username)

	if utils.IsReservedNamespace(req.Namespace) {
		return admission.Allowed(fmt.Sprintf("namespace %s is a reserved system namespace, no mutation needed", req.Namespace))
	}

	if !utils.IsAKSService(req.UserInfo) {
		return admission.Allowed("user is not aksService, no mutation needed")
	}

	var job batchv1.Job
	if err := m.decoder.Decode(req, &job); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if job.Labels == nil {
		job.Labels = map[string]string{}
	}
	job.Labels[utils.ReconcileLabelKey] = utils.ReconcileLabelValue

	if job.Spec.Template.Labels == nil {
		job.Spec.Template.Labels = map[string]string{}
	}
	// Kubernetes permits pod-template metadata updates for suspended Jobs that
	// have never started, but rejects them as immutable after a Job has started.
	job.Spec.Template.Labels[utils.ReconcileLabelKey] = utils.ReconcileLabelValue

	marshaled, err := json.Marshal(job)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	klog.V(2).InfoS("mutated job with reconcile label",
		"operation", req.Operation, "namespace", req.Namespace, "name", req.Name)
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}
