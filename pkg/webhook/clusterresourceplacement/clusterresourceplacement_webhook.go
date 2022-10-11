/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"sigs.k8s.io/controller-runtime/pkg/manager"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

const (
	ValidationPath = "/validate-fleet-azure-com-v1alpha1-clusterresourceplacement"
)

func Add(mgr manager.Manager) error {
	return (&fleetv1alpha1.ClusterResourcePlacement{}).SetupWebhookWithManager(mgr)
}
