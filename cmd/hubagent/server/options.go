package server

import (
	"github.com/spf13/pflag"
)

type RunOptions struct {
	KubeConfig           string
	HubURL               string
	InCluster            bool
	APIServerQPS         int
	APIServerBurst       int
	EnableLeaderElection bool
}

func NewRunOptions() *RunOptions {
	options := &RunOptions{}
	options.addAllFlags()
	return options
}

func (s *RunOptions) addAllFlags() {
	pflag.BoolVar(&s.InCluster, "incluster", s.InCluster, "If controller run incluster.")
	pflag.StringVar(&s.KubeConfig, "kubeConfig", s.KubeConfig, "Kube Config path if not run in cluster.")
	pflag.StringVar(&s.HubURL, "HubUrl", s.HubURL, "Hub Url if not run in cluster.")
	pflag.IntVar(&s.APIServerQPS, "qps", 5, "qps of query apiserver.")
	pflag.IntVar(&s.APIServerBurst, "burst", 10, "burst of query apiserver.")
	pflag.BoolVar(&s.EnableLeaderElection, "enableLeaderElection", s.EnableLeaderElection, "If EnableLeaderElection for controller.")
}
