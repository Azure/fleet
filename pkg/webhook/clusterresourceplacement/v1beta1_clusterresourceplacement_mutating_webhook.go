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

// Package clusterresourceplacement implements the webhook for v1beta1 ClusterResourcePlacement.
package clusterresourceplacement

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/defaulter"
)

var (
	// MutatingPath is the webhook service path for mutating v1beta1 CRP resources.
	MutatingPath = fmt.Sprintf(utils.MutatingPathFmt, v1beta1.GroupVersion.Group, v1beta1.GroupVersion.Version, "clusterresourceplacement")
)

type clusterResourcePlacementMutator struct {
	decoder webhook.AdmissionDecoder
}

// AddMutating registers the mutating webhook for v1beta1 CRP.
func AddMutating(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(MutatingPath, &webhook.Admission{Handler: &clusterResourcePlacementMutator{admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle mutates CRP objects on create and update.
func (m *clusterResourcePlacementMutator) Handle(_ context.Context, req admission.Request) admission.Response {
	klog.V(2).InfoS("handling CRP", "operation", req.Operation, "crp", req.Name)
	// Decode the request object into a ClusterResourcePlacement.
	var crp v1beta1.ClusterResourcePlacement
	if err := m.decoder.Decode(req, &crp); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Apply default values to the CRP object.
	defaulter.SetPlacementDefaults(&crp)
	marshaled, err := json.Marshal(crp)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	klog.V(2).InfoS("mutating CRP", "operation", req.Operation, "crp", req.Name)
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}
