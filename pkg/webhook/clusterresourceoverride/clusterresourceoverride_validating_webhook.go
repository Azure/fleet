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

// Package clusterresourceoverride provides a validating webhook for the ClusterResourceOverride custom resource in the fleet API group.
package clusterresourceoverride

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fleetv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/validator"
)

var (
	// ValidationPath is the webhook service path which admission requests are routed to for validating ClusterResourceOverride resources.
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, fleetv1alpha1.GroupVersion.Group, fleetv1alpha1.GroupVersion.Version, "clusterresourceoverride")
)

type clusterResourceOverrideValidator struct {
	client  client.Client
	decoder webhook.AdmissionDecoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &clusterResourceOverrideValidator{mgr.GetClient(), admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle clusterResourceOverrideValidator checks to see if cluster resource override is valid
func (v *clusterResourceOverrideValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	var cro fleetv1alpha1.ClusterResourceOverride
	klog.V(2).InfoS("Validating webhook handling cluster resource override", "operation", req.Operation)
	if err := v.decoder.Decode(req, &cro); err != nil {
		klog.ErrorS(err, "Failed to decode cluster resource override object for validating fields", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
		return admission.Errored(http.StatusBadRequest, err)
	}

	// List of cluster resource overrides
	croList, err := listClusterResourceOverride(ctx, v.client)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check if the override count limit has been reached, if there are at most 100 cluster resource overrides
	if req.Operation == admissionv1.Create && len(croList.Items) >= 100 {
		klog.Errorf("ClusterResourceOverride limit has been reached: at most 100 cluster resources can be created.")
		return admission.Denied("clusterResourceOverride limit has been reached: at most 100 cluster resources can be created.")
	}

	if err := validator.ValidateClusterResourceOverride(cro, croList); err != nil {
		klog.V(2).ErrorS(err, "ClusterResourceOverride has invalid fields, request is denied", "operation", req.Operation)
		return admission.Denied(err.Error())
	}
	return admission.Allowed("clusterResourceOverride has valid fields")
}

// listClusterResourceOverride returns a list of cluster resource overrides.
func listClusterResourceOverride(ctx context.Context, client client.Client) (*fleetv1alpha1.ClusterResourceOverrideList, error) {
	croList := &fleetv1alpha1.ClusterResourceOverrideList{}
	if err := client.List(ctx, croList); err != nil {
		klog.ErrorS(err, "Failed to list clusterResourceOverrides when validating")
		return nil, fmt.Errorf("failed to list clusterResourceOverrides, please retry the request: %w", err)
	}
	return croList, nil
}
