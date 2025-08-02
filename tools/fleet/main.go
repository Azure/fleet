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
	"log"

	"github.com/spf13/cobra"

	"github.com/kubefleet-dev/kubefleet/tools/fleet/cmd/approve"
	"github.com/kubefleet-dev/kubefleet/tools/fleet/cmd/draincluster"
	"github.com/kubefleet-dev/kubefleet/tools/fleet/cmd/uncordoncluster"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "kubectl-fleet",
		Short: "KubeFleet cluster management plugin",
		Long:  "kubectl-fleet is a kubectl plugin for managing KubeFleet member clusters",
	}

	// Add subcommands
	rootCmd.AddCommand(approve.NewCmdApprove())
	rootCmd.AddCommand(draincluster.NewCmdDrainCluster())
	rootCmd.AddCommand(uncordoncluster.NewCmdUncordonCluster())

	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Error executing command: %v", err)
	}
}
