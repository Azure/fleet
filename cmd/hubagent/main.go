/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package main

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog"

	"github.com/spf13/pflag"

	fleetv1alpha1 "github.com/Azure/fleet/apis/v1alpha1"
	"github.com/Azure/fleet/cmd/hubagent/server"
	//+kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	klog.InitFlags(nil)

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	options := server.NewRunOptions()

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	flag.Parse()

	if err := server.Run(options); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
