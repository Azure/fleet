/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"time"

	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"

	// Note that Kubernetes registers workqueue metrics to default prometheus Registry. And the registry will be
	// initialized by the package 'k8s.io/apiserver/pkg/server'.
	// See https://github.com/kubernetes/kubernetes/blob/f61ed439882e34d9dad28b602afdc852feb2337a/staging/src/k8s.io/component-base/metrics/prometheus/workqueue/metrics.go#L25
	// But the controller-runtime registers workqueue metrics to its own Registry instead of default prometheus Registry.
	// See https://github.com/kubernetes-sigs/controller-runtime/blob/4d10a0615b11507451ecb58bfd59f0f6ef313a29/pkg/metrics/workqueue.go#L24-L26
	// However, global workqueue metrics factory will be only initialized once.
	// See https://github.com/kubernetes/kubernetes/blob/f61ed439882e34d9dad28b602afdc852feb2337a/staging/src/k8s.io/client-go/util/workqueue/metrics.go#L257-L261
	// So this package should be initialized before 'k8s.io/apiserver/pkg/server', thus the internal registry of
	// controller-runtime could be set first.
	_ "sigs.k8s.io/controller-runtime/pkg/metrics"

	apiserver "k8s.io/apiserver/pkg/server"

	"go.goms.io/fleet/cmd/hub-manager/app"
)

// TODO: see if we should just use plain cmd instead of corba

func main() {
	defer klog.Flush()
	ctx := apiserver.SetupSignalContext()
	command := app.NewhubManagerCommand(ctx)
	code := cli.Run(command)
	klog.FlushAndExit(time.Second*15, code)
}
