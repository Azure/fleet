package webhook

import (
	crpv1alpha1 "go.goms.io/fleet/pkg/webhook/clusterresourceplacement/v1alpha1"
	crpv1beta1 "go.goms.io/fleet/pkg/webhook/clusterresourceplacement/v1beta1"
	"go.goms.io/fleet/pkg/webhook/fleetresourcehandler"
	"go.goms.io/fleet/pkg/webhook/pod"
	"go.goms.io/fleet/pkg/webhook/replicaset"
)

func init() {
	// AddToManagerFuncs is a list of functions to create webhook and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, fleetresourcehandler.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, crpv1alpha1.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, crpv1beta1.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, pod.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, replicaset.Add)
}
