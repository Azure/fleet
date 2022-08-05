/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package webhook

import (
	"go.goms.io/fleet/pkg/webhook/pod"
)

func init() {
	AddToManagerFuncs = append(AddToManagerFuncs, pod.Add)
}
