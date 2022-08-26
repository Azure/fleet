/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package webhook

import (
	"go.goms.io/fleet/pkg/webhook/clusterresourceplacement"
)

func init() {
	// AddToManagerFuncs is a list of functions to create webhook and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, clusterresourceplacement.Add)
}
