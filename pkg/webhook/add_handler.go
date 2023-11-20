package webhook

import (
	"go.goms.io/fleet/pkg/webhook/clusterresourceplacement"
	"go.goms.io/fleet/pkg/webhook/fleetresourcehandler"
	"go.goms.io/fleet/pkg/webhook/pod"
	"go.goms.io/fleet/pkg/webhook/replicaset"
)

func init() {
	// AddToManagerFuncs is a list of functions to create webhook and add them to a manager.
	AddToFleetManagerFuncs = append(AddToFleetManagerFuncs, clusterresourceplacement.AddV1Alpha1)
	AddToFleetManagerFuncs = append(AddToFleetManagerFuncs, clusterresourceplacement.Add)
	AddToFleetManagerFuncs = append(AddToFleetManagerFuncs, pod.Add)
	AddToFleetManagerFuncs = append(AddToFleetManagerFuncs, replicaset.Add)

	AddToFleetGuardRailManagerFuncs = append(AddToFleetGuardRailManagerFuncs, fleetresourcehandler.Add)
}
