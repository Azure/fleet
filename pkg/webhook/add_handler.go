package webhook

import (
	"go.goms.io/fleet/pkg/webhook/clusterresourceoverride"
	crpmutating "go.goms.io/fleet/pkg/webhook/clusterresourceplacement/mutating"
	crpvalidating "go.goms.io/fleet/pkg/webhook/clusterresourceplacement/validating"
	"go.goms.io/fleet/pkg/webhook/fleetresourcehandler"
	"go.goms.io/fleet/pkg/webhook/membercluster"
	"go.goms.io/fleet/pkg/webhook/pod"
	"go.goms.io/fleet/pkg/webhook/replicaset"
	"go.goms.io/fleet/pkg/webhook/resourceoverride"
)

func init() {
	// AddToManagerFleetResourceValidator is a function to register fleet guard rail resource validator to the webhook server
	AddToManagerFleetResourceValidator = fleetresourcehandler.Add
	// AddToManagerFuncs is a list of functions to register webhook validators/mutators to the webhook server
	AddToManagerFuncs = append(AddToManagerFuncs, crpvalidating.AddV1Alpha1)
	AddToManagerFuncs = append(AddToManagerFuncs, crpvalidating.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, crpmutating.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, pod.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, replicaset.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, membercluster.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceoverride.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, resourceoverride.Add)
}
