/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/configprovider"
	"go.goms.io/fleet/pkg/controllers/memberinternalmembercluster"
	"go.goms.io/fleet/pkg/controllers/membership"
	//+kubebuilder:scaffold:imports
)

var (
	tokenFilePath        = "/config/token.json"
	scheme               = runtime.NewScheme()
	hubProbeAddr         = flag.String("hub-health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	hubMetricsAddr       = flag.String("hub-metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	probeAddr            = flag.String("health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	metricsAddr          = flag.String("metrics-bind-address", ":8090", "The address the metric endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
)

func init() {
	klog.InitFlags(nil)

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	flag.Parse()

	hubURL := os.Getenv("HUB_SERVER_URL")

	if hubURL == "" {
		klog.Error("hub server api cannot be empty")
		os.Exit(1)
	}

	hubOpts := ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     *hubMetricsAddr,
		Port:                   8443,
		HealthProbeBindAddress: *hubProbeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "984738fa.hub.fleet.azure.com",
	}

	//+kubebuilder:scaffold:builder

	klog.Info("starting memebragent")

	token := configprovider.EmptyToken()
	currentFile, err := os.ReadFile(tokenFilePath)
	if err != nil {
		klog.Error(err, "cannot read token file")
		os.Exit(1)
	}
	if len(currentFile) == 0 {
		callRefreshToken()
	}

	if err = json.Unmarshal(currentFile, &token); err != nil {
		klog.Error(err, "cannot parse token file")
		os.Exit(1)
	}
	if token.ExpiresOn == "0" || token.WillExpireIn(configprovider.RefreshBefore) {
		callRefreshToken()
	}

	hubConfig := rest.Config{
		BearerToken: token.AccessToken,
		Host:        hubURL,
		// TODO (mng): Remove TLSClientConfig/Insecure(true) once server CA can be passed in as flag.
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}

	if err := Start(ctrl.SetupSignalHandler(), &hubConfig, hubOpts); err != nil {
		klog.Error(err, "problem running controllers")
		os.Exit(1)
	}
}

// Start Start the member controllers with the supplied config
func Start(ctx context.Context, hubCfg *rest.Config, hubOpts ctrl.Options) error {
	hubMrg, err := ctrl.NewManager(hubCfg, hubOpts)
	if err != nil {
		return errors.Wrap(err, "unable to start hub manager")
	}

	memberOpts := ctrl.Options{
		Scheme:                 hubOpts.Scheme,
		MetricsBindAddress:     *metricsAddr,
		Port:                   8446,
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         hubOpts.LeaderElection,
		LeaderElectionID:       "984738fa.member.fleet.azure.com",
	}
	memberMgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), memberOpts)
	if err != nil {
		return errors.Wrap(err, "unable to start member manager")
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(hubCfg, apiutil.WithLazyDiscovery)
	if err != nil {
		return errors.Wrap(err, "unable to start member manager")
	}

	membershipChan := make(chan fleetv1alpha1.ClusterState)
	internalMemberClusterChan := make(chan fleetv1alpha1.ClusterState)
	defer close(membershipChan)
	defer close(internalMemberClusterChan)

	if err = memberinternalmembercluster.NewReconciler(
		hubMrg.GetClient(), memberMgr.GetClient(), restMapper, internalMemberClusterChan,
		membershipChan).SetupWithManager(hubMrg); err != nil {
		return errors.Wrap(err, "unable to create controller hub_member")
	}

	if err := hubMrg.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up health check for hub manager")
		os.Exit(1)
	}
	if err := memberMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up ready check for hub manager")

		os.Exit(1)
	}

	if err = membership.NewReconciler(memberMgr.GetClient(), internalMemberClusterChan, membershipChan).
		SetupWithManager(memberMgr); err != nil {
		return errors.Wrap(err, "unable to create controller membership")
	}

	if err := memberMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up health check for member manager")
		os.Exit(1)
	}
	if err := memberMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up ready check for member manager")

		os.Exit(1)
	}

	klog.Info("starting hub manager")
	startErr := make(chan error)
	go func() {
		defer klog.Info("shutting down hub manager")
		err := hubMrg.Start(ctx)
		if err != nil {
			startErr <- errors.Wrap(err, "problem starting hub manager")
			return
		}
	}()

	klog.Info("starting member manager")
	defer klog.Info("shutting down member manager")
	if err := memberMgr.Start(ctx); err != nil {
		return errors.Wrap(err, "problem starting member manager")
	}

	return nil
}

func callRefreshToken() {
	values := map[string]string{"provider": "azure"}

	json_data, err := json.Marshal(values)
	if err != nil {
		klog.InfoS("an error has occurred while parsing the request")
		os.Exit(1)
	}

	resp, err := http.Post("http://localhost:4000/refreshtoken", "application/json", bytes.NewBuffer(json_data))
	if err != nil {
		klog.Error(err, " an error has occurred while refreshing token")
		os.Exit(1)
	}
	if resp.ContentLength != 0 && resp.Body != nil {

		defer resp.Body.Close()
		if err != nil {
			klog.Error(err, "an error has occurred while refreshing token")
			os.Exit(1)
		}

		if resp.StatusCode != http.StatusOK {
			klog.Fatalf("an error has occurred while refreshing token. status: %s", resp.Status)
			os.Exit(1)
		}
	}
}
