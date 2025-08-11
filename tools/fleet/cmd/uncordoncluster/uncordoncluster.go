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

package uncordoncluster

import (
	"context"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	toolsutils "go.goms.io/fleet/tools/utils"
)

// uncordonOptions wraps common cluster connection parameters
type uncordonOptions struct {
	hubClusterContext string
	clusterName       string

	hubClient client.Client
}

// NewCmdUncordonCluster creates a new uncordoncluster command
func NewCmdUncordonCluster() *cobra.Command {
	o := &uncordonOptions{}

	cmd := &cobra.Command{
		Use:   "uncordoncluster",
		Short: "Uncordon a member cluster",
		Long:  "Uncordon a previously drained member cluster by removing the cordon taint",
		RunE: func(command *cobra.Command, args []string) error {
			if err := o.setupClient(); err != nil {
				return err
			}
			return o.runUncordon()
		},
	}

	// Add flags specific to uncordon command
	cmd.Flags().StringVar(&o.hubClusterContext, "hubClusterContext", "", "kubectl context for the hub cluster (required)")
	cmd.Flags().StringVar(&o.clusterName, "clusterName", "", "name of the member cluster (required)")

	// Mark required flags
	_ = cmd.MarkFlagRequired("hubClusterContext")
	_ = cmd.MarkFlagRequired("clusterName")

	return cmd
}

func (o *uncordonOptions) runUncordon() error {
	ctx := context.Background()
	if err := o.uncordon(ctx); err != nil {
		return fmt.Errorf("failed to uncordon cluster %s: %w", o.clusterName, err)
	}

	log.Printf("uncordoned member cluster %s", o.clusterName)
	return nil
}

// setupClient creates and configures the Kubernetes client
func (o *uncordonOptions) setupClient() error {
	scheme := runtime.NewScheme()

	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add custom APIs (cluster) to the runtime scheme: %w", err)
	}
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add custom APIs (placement) to the runtime scheme: %w", err)
	}

	hubClient, err := toolsutils.GetClusterClientFromClusterContext(o.hubClusterContext, scheme)
	if err != nil {
		return fmt.Errorf("failed to create hub cluster client: %w", err)
	}

	o.hubClient = hubClient
	return nil
}

func (o *uncordonOptions) uncordon(ctx context.Context) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mc clusterv1beta1.MemberCluster
		if err := o.hubClient.Get(ctx, types.NamespacedName{Name: o.clusterName}, &mc); err != nil {
			return err
		}

		if len(mc.Spec.Taints) == 0 {
			return nil
		}

		var newTaints []clusterv1beta1.Taint
		for i := range mc.Spec.Taints {
			taint := mc.Spec.Taints[i]
			if taint == toolsutils.CordonTaint {
				continue
			}
			newTaints = append(newTaints, taint)
		}
		mc.Spec.Taints = newTaints

		return o.hubClient.Update(ctx, &mc)
	})
}
