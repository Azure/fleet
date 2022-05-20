package server

import (
	"context"
	"os"

	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
)

func newConfig(kubeconfig, hub string, inCluster bool) (*rest.Config, error) {
	var (
		config *rest.Config
		err    error
	)
	if inCluster {
		config, err = rest.InClusterConfig()
	} else {
		config, err = clientcmd.BuildConfigFromFlags(hub, kubeconfig)
	}
	if err != nil {
		return nil, err
	}
	return config, nil
}

func Run(s *RunOptions) error {
	ctx := context.Background()
	config, err := newConfig(s.KubeConfig, s.HubURL, s.InCluster)
	if err != nil {
		klog.ErrorS(err, "Failed to parse config")
		os.Exit(1)
	}
	config.QPS = float32(s.APIServerQPS)
	config.Burst = s.APIServerBurst
	stopCh := server.SetupSignalHandler()
	kubeClient := kubernetes.NewForConfigOrDie(config)

	run := func(ctx context.Context) {
		//Call reconcile
		// go mcCtrl := membercluster.Reconciler()
		// go imcCtrl := internalmembercluster.Reconciler()
		select {}
	}
	if !s.EnableLeaderElection {
		run(ctx)
	} else {
		id, err := os.Hostname()
		if err != nil {
			return err
		}
		// add a uniquifier so that two processes on the same host don't accidentally both become active
		id = id + "_" + string(uuid.NewUUID())

		rl, err := resourcelock.New("endpoints",
			"fleet-system",
			"hub-controller",
			kubeClient.CoreV1(),
			kubeClient.CoordinationV1(),
			resourcelock.ResourceLockConfig{
				Identity: id,
			})
		if err != nil {
			klog.ErrorS(err, "Resource lock creation failed")
			os.Exit(1)
		}

		leaderelection.RunOrDie(context.TODO(), leaderelection.LeaderElectionConfig{
			Lock: rl,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: run,
				OnStoppedLeading: func() {
					klog.ErrorS(err, "Leaderelection lost")
					os.Exit(1)
				},
			},
			Name: "hub controller",
		})
	}

	<-stopCh
	return nil
}
