package webhook

import (
	"go.goms.io/fleet/pkg/webhook/clusterresourceoverride"
	"go.goms.io/fleet/pkg/webhook/clusterresourceplacement"
	"go.goms.io/fleet/pkg/webhook/clusterresourceplacementdisruptionbudget"
	"go.goms.io/fleet/pkg/webhook/clusterresourceplacementeviction"
	"go.goms.io/fleet/pkg/webhook/fleetresourcehandler"
	"go.goms.io/fleet/pkg/webhook/membercluster"
	"go.goms.io/fleet/pkg/webhook/pod"
	"go.goms.io/fleet/pkg/webhook/replicaset"
	"go.goms.io/fleet/pkg/webhook/resourceoverride"
	"go.goms.io/fleet/pkg/webhook/resourceplacement"
)

func init() {
	// AddToManagerFleetResourceValidator is a function to register fleet guard rail resource validator to the webhook server
	AddToManagerFleetResourceValidator = fleetresourcehandler.Add
	// AddToManagerFuncs is a list of functions to register webhook validators and mutators to the webhook server
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacement.AddMutating)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacement.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, resourceplacement.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, pod.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, replicaset.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, membercluster.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceoverride.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, resourceoverride.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacementeviction.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacementdisruptionbudget.Add)
}
