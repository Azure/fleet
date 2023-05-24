package webhook

import (
	"go.goms.io/fleet/pkg/webhook/customresourcedefinition"
)

func init() {
	// AddToManagerFuncs is a list of functions to create webhook and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, customresourcedefinition.Add)
}
