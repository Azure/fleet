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

// Package resourceplacement implements the webhook for v1beta1 ResourcePlacement.
package resourceplacement

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/validator"
)

var (
	// ValidationPath is the webhook service path which admission requests are routed to for validating v1beta1 RP resources.
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, placementv1beta1.GroupVersion.Group, placementv1beta1.GroupVersion.Version, "resourceplacement")
)

type resourcePlacementValidator struct {
	decoder webhook.AdmissionDecoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &resourcePlacementValidator{admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle resourcePlacementValidator handles create, update RP requests.
func (v *resourcePlacementValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	return validator.HandlePlacementValidation(
		ctx,
		req,
		v.decoder,
		"RP",
		// decodeFunc
		func(req admission.Request, decoder webhook.AdmissionDecoder) (placementv1beta1.PlacementObj, error) {
			var rp placementv1beta1.ResourcePlacement
			err := decoder.Decode(req, &rp)
			return &rp, err
		},
		// decodeOldFunc
		func(req admission.Request, decoder webhook.AdmissionDecoder) (placementv1beta1.PlacementObj, error) {
			var oldRP placementv1beta1.ResourcePlacement
			err := decoder.DecodeRaw(req.OldObject, &oldRP)
			return &oldRP, err
		},
		// validateFunc
		func(obj placementv1beta1.PlacementObj) error {
			return validator.ValidateResourcePlacement(obj.(*placementv1beta1.ResourcePlacement))
		},
	)
}
