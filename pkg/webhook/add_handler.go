package webhook

import (
	"go.goms.io/fleet/pkg/webhook/clusterresourceoverride"
	"go.goms.io/fleet/pkg/webhook/clusterresourceplacement"
	"go.goms.io/fleet/pkg/webhook/fleetresourcehandler"
	"go.goms.io/fleet/pkg/webhook/membercluster"
	"go.goms.io/fleet/pkg/webhook/pod"
	"go.goms.io/fleet/pkg/webhook/replicaset"
	"go.goms.io/fleet/pkg/webhook/resourceoverride"
)

func init() {
	// AddToManagerFleetResourceValidator is a function to register fleet guard rail resource validator to the webhook server
	AddToManagerFleetResourceValidator = fleetresourcehandler.Add
	// AddToManagerFuncs is a list of functions to register webhook validators to the webhook server
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacement.AddV1Alpha1)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacement.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, pod.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, replicaset.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, membercluster.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceoverride.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, resourceoverride.Add)
}
