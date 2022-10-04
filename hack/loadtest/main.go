/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"flag"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/hack/loadtest/util"
)

var (
	scheme = runtime.NewScheme()
)

var (
	placementDeadline   = flag.Int("placement-deadline-second", 60, "The deadline for a placement to be applied (in seconds)")
	maxCurrentPlacement = flag.Int("max-current-placement", 10, "The number of current placement load.")
	clusterNames        util.ClusterNames
)

func init() {
	klog.InitFlags(nil)
	utilrand.Seed(time.Now().UnixNano())

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
	utilruntime.Must(workv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	flag.Var(&clusterNames, "cluster", "The name of a member cluster")
	flag.Parse()
	defer klog.Flush()

	klog.InfoS("start to run placement load test", "placementDeadline", *placementDeadline, "maxCurrentPlacement", *maxCurrentPlacement, "clusterNames", clusterNames)
	config := config.GetConfigOrDie()
	config.QPS, config.Burst = float32(100), 500
	hubClient, err := client.New(config, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		panic(err)
	}
	ctx := ctrl.SetupSignalHandler()
	if err = util.ApplyClusterScopeManifests(ctx, hubClient); err != nil {
		panic(err)
	}

	// run the loadtest in the background
	go runLoadTest(ctx, config)

	// setup prometheus server
	http.Handle("/metrics", promhttp.Handler())
	if err = http.ListenAndServe(":4848", nil); err != nil {
		panic(err)
	}
}

func runLoadTest(ctx context.Context, config *rest.Config) {
	var wg sync.WaitGroup
	wg.Add(*maxCurrentPlacement)
	for i := 0; i < *maxCurrentPlacement; i++ {
		go func() {
			// each use a separate client to avoid client side throttling
			time.Sleep(time.Millisecond * time.Duration(utilrand.Intn(1000)))
			hubClient, err := client.New(config, client.Options{
				Scheme: scheme,
			})
			if err != nil {
				panic(err)
			}
			defer wg.Done()
			// continuously apply and delete resources
			for {
				select {
				case <-ctx.Done():
					return
				default:
					if err := util.MeasureOnePlacement(ctx, hubClient, time.Duration(*placementDeadline)*time.Second, *maxCurrentPlacement, clusterNames); err != nil {
						klog.ErrorS(err, "placement load test failed")
					}
				}
			}
		}()
	}
	wg.Wait()
	klog.InfoS(" placement load test finished")
}
