package webhook

import "go.goms.io/fleet/pkg/webhook/fleetresourcehandler"

func init() {
	// AddToManagerFuncs is a list of functions to create webhook and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, fleetresourcehandler.Add)
}
