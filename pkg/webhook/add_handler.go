package webhook

import (
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/clusterresourceoverride"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/clusterresourceplacement"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/clusterresourceplacementdisruptionbudget"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/clusterresourceplacementeviction"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/fleetresourcehandler"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/membercluster"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/pod"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/replicaset"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/resourceoverride"
)

func init() {
	// AddToManagerFleetResourceValidator is a function to register fleet guard rail resource validator to the webhook server
	AddToManagerFleetResourceValidator = fleetresourcehandler.Add
	// AddToManagerFuncs is a list of functions to register webhook validators and mutators to the webhook server
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacement.AddMutating)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacement.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, pod.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, replicaset.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, membercluster.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceoverride.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, resourceoverride.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacementeviction.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacementdisruptionbudget.Add)
}
