package webhook

import (
	"go.goms.io/fleet/pkg/webhook/clusterresourceplacement"
	"go.goms.io/fleet/pkg/webhook/fleetresourcehandler"
	"go.goms.io/fleet/pkg/webhook/pod"
	"go.goms.io/fleet/pkg/webhook/replicaset"
)

func init() {
	// AddToManagerFuncs is a list of functions to create webhook and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, fleetresourcehandler.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacement.AddV1Alpha1)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacement.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, pod.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, replicaset.Add)
}
