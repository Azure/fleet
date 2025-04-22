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

// Package resourceoverride provides a validating webhook for the resourceoverride custom resource in the fleet API group.
package resourceoverride

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
	// ValidationPath is the webhook service path which admission requests are routed to for validating resourceoverride resources.
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, fleetv1alpha1.GroupVersion.Group, fleetv1alpha1.GroupVersion.Version, "resourceoverride")
)

type resourceOverrideValidator struct {
	client  client.Client
	decoder webhook.AdmissionDecoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &resourceOverrideValidator{mgr.GetClient(), admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle resourceOverrideValidator checks to see if resource override is valid.
func (v *resourceOverrideValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	var ro fleetv1alpha1.ResourceOverride
	klog.V(2).InfoS("Validating webhook handling resource override", "operation", req.Operation)
	if err := v.decoder.Decode(req, &ro); err != nil {
		klog.ErrorS(err, "Failed to decode resource override object for validating fields", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
		return admission.Errored(http.StatusBadRequest, err)
	}

	// List all the resource overrides in the same namespace
	roList := &fleetv1alpha1.ResourceOverrideList{}
	if err := v.client.List(ctx, roList, client.InNamespace(ro.Namespace)); err != nil {
		klog.ErrorS(err, "Failed to list resourceOverrides when validating")
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to list resourceOverrides, please retry the request: %w", err))
	}

	// Check if the override count limit has been reached, if there are at most 100 resource overrides.
	if req.Operation == admissionv1.Create && len(roList.Items) >= 100 {
		klog.Errorf("ResourceOverride limit has been reached: at most 100 resources can be created.")
		return admission.Denied("resourceOverride limit has been reached: at most 100 resources can be created.")
	}

	if err := validator.ValidateResourceOverride(ro, roList); err != nil {
		klog.V(2).ErrorS(err, "ResourceOverride has invalid fields, request is denied", "operation", req.Operation)
		return admission.Denied(err.Error())
	}
	return admission.Allowed("resourceOverride has valid fields")
}
