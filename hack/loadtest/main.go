/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/hack/loadtest/util"
)

var (
	scheme  = runtime.NewScheme()
	crpFile = "test-crp.yaml"
)

var (
	placementDeadline   = flag.Int("placement-deadline-second", 300, "The deadline for a placement to be applied (in seconds)")
	pollInterval        = flag.Int("poll-interval-millisecond", 250, "The poll interval for verification (in milli-second)")
	maxCurrentPlacement = flag.Int("max-current-placement", 20, "The number of current placement load.")
	loadTestLength      = flag.Int("load-test-length-minute", 30, "The length of the load test in minutes.")
	useTestResources    = flag.Bool("use-test-resources", false, "Boolean to include all test resources in the test.")
	clusterNames        util.ClusterNames //will be used for PickFixed scenario, otherwise will apply to all clusters
)

func init() {
	klog.InitFlags(nil)
	flag.StringVar(&crpFile, "crp-file", "test-crp.yaml", "The CRP yaml file.")
	utilrand.Seed(time.Now().UnixNano())

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	flag.Parse()
	defer klog.Flush()

	klog.InfoS("start to run placement load test", "crpFile", crpFile, "pollInterval", *pollInterval, "placementDeadline", *placementDeadline, "maxCurrentPlacement", *maxCurrentPlacement, "useTestResources", useTestResources)
	config := config.GetConfigOrDie()
	config.QPS, config.Burst = float32(100), 500 //queries per second, max # of queries queued at once
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
	loadTestCtx, canFunc := context.WithDeadline(ctx, time.Now().Add(time.Minute*time.Duration(*loadTestLength)))
	defer canFunc()
	// run the loadtest in the background
	go runLoadTest(loadTestCtx, config)
	// setup prometheus server
	http.Handle("/metrics", promhttp.Handler())
	/* #nosec */
	if err = http.ListenAndServe(":4848", nil); err != nil {
		panic(err)
	}
}

func runLoadTest(ctx context.Context, config *rest.Config) {
	var wg sync.WaitGroup
	wg.Add(*maxCurrentPlacement)
	for i := 0; i < *maxCurrentPlacement; i++ {
		go func() {
			// each use a separate client to avoid client side throttling, start each client side with a jitter
			// to avoid creating too many clients at the same time.
			time.Sleep(time.Millisecond * time.Duration(utilrand.Intn(100**maxCurrentPlacement)))
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
					loopCtx, cancel := context.WithCancel(context.Background())
					if err = util.MeasureOnePlacement(loopCtx, hubClient, time.Duration(*placementDeadline)*time.Second, time.Duration(*pollInterval)*time.Millisecond, *maxCurrentPlacement, clusterNames, crpFile, useTestResources); err != nil {
						klog.ErrorS(err, "load test placement failed ")
					}
					cancel()
				}
			}
		}()
	}
	wg.Wait()
	klog.Info("Placement load test finished.")
	hubClient, _ := client.New(config, client.Options{
		Scheme: scheme,
	})
	if err := util.CleanupAll(hubClient); err != nil {
		klog.ErrorS(err, "clean up placement load test hit an error")
	}
	util.PrintTestMetrics(*useTestResources)
	klog.InfoS(" placement load test finished. For more metrics, please use prometheus")
}
