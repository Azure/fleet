/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
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

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/validator"
)

var (
	// ValidationPath is the webhook service path which admission requests are routed to for validating ClusterResourceOverride resources.
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, placementv1alpha1.GroupVersion.Group, placementv1alpha1.GroupVersion.Version, "clusterresourceoverride")
)

type clusterResourceOverrideValidator struct {
	client  client.Client
	decoder *admission.Decoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &clusterResourceOverrideValidator{mgr.GetClient(), admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle clusterResourceOverrideValidator checks to see if cluster resource override is valid
func (v *clusterResourceOverrideValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	var cro placementv1alpha1.ClusterResourceOverride
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
func listClusterResourceOverride(ctx context.Context, client client.Client) (*placementv1alpha1.ClusterResourceOverrideList, error) {
	croList := &placementv1alpha1.ClusterResourceOverrideList{}
	if err := client.List(ctx, croList); err != nil {
		klog.ErrorS(err, "Failed to list clusterResourceOverrides when validating")
		return nil, fmt.Errorf("failed to list clusterResourceOverrides, please retry the request: %w", err)
	}
	return croList, nil
}
