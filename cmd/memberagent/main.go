/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	fleetv1alpha1 "github.com/Azure/fleet/apis/v1alpha1"
	"github.com/Azure/fleet/pkg/controllers"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog"
	"k8s.io/klog/v2/klogr"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	//+kubebuilder:scaffold:imports
)

var (
	scheme               = runtime.NewScheme()
	setupLog             = ctrl.Log.WithName("setup")
	metricsAddr          = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	//TODO to be replaced with get token impl
	var hubKubeconfig string
	flag.Parse()

	// Set the Klog format, as the Serialize format shouldn't be used anymore.
	// This makes sure that the logs are formatted correctly, i.e.:
	// * JSON logging format: msg isn't serialized twice
	// * text logging format: values are formatted with their .String() func.
	ctrl.SetLogger(klogr.NewWithOptions(klogr.WithFormat(klogr.FormatKlog)))

	opts := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: *metricsAddr,
		LeaderElection:     *enableLeaderElection,
		Port:               9443,
	}

	hubConfig, err := getKubeConfig(hubKubeconfig)
	if err != nil {
		klog.Error(err, "error reading kubeconfig to connect to hub")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	klog.Info("starting memebragent")
	if err := controllers.Start(ctrl.SetupSignalHandler(), hubConfig, ctrl.GetConfigOrDie(), setupLog, opts); err != nil {
		klog.Error(err, "problem running controllers")
		os.Exit(1)
	}
}

func getKubeConfig(hubKubeconfig string) (*restclient.Config, error) {
	hubClientSet, err := kubernetes.NewForConfig(ctrl.GetConfigOrDie())
	if err != nil {
		return nil, errors.Wrap(err, "cannot create the spoke client")
	}

	secret, err := hubClientSet.CoreV1().Secrets("work").Get(context.Background(), hubKubeconfig, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "cannot find kubeconfig secrete")
	}

	kubeConfigData, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeConfigData) == 0 {
		return nil, fmt.Errorf("wrong formatted kube config")
	}

	kubeConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeConfigData)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create the rest client")
	}

	return kubeConfig, nil
}
