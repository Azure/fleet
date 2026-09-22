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

package trackers

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/karpenter-provider-azure/pkg/auth"
	"github.com/Azure/karpenter-provider-azure/pkg/providers/pricing"
	"github.com/Azure/karpenter-provider-azure/pkg/providers/pricing/client"
)

// PricingProvider is an interface that the Azure property provider uses to sync pricing information.
//
// It helps decouple the Azure property provider from the actual pricing provider implementation; even
// though at this moment the property provider leverages the AKS Karpenter pricing client only.
type PricingProvider interface {
	OnDemandPrice(instanceType string) (float64, bool)
	LastUpdated() time.Time
}

var _ PricingProvider = &AKSKarpenterPricingClient{}

// AKSKarpenterPricingClient is a thin wrapper around the AKS Karpenter pricing client, which
// implements the PricingProvider interface.
type AKSKarpenterPricingClient struct {
	karpenterPricingClient *pricing.Provider
}

// OnDemandPrice returns the on-demand price of an instance type.
func (k *AKSKarpenterPricingClient) OnDemandPrice(instanceType string) (float64, bool) {
	return k.karpenterPricingClient.OnDemandPrice(instanceType)
}

// LastUpdated returns the last time the pricing information was updated.
func (k *AKSKarpenterPricingClient) LastUpdated() time.Time {
	return k.karpenterPricingClient.OnDemandLastUpdated()
}

// NewAKSKarpenterPricingClient returns a new AKS Karpenter pricing client, which implements
// the PricingProvider interface.
func NewAKSKarpenterPricingClient(ctx context.Context, region string) (*AKSKarpenterPricingClient, error) {
	// Pin the public cloud: the 1.5 pricing client had no environment and always queried the
	// public retail prices endpoint, so this preserves existing behaviour. 1.14 can reject
	// non-public clouds outright; wire that up with the member agent's cloud config
	// (see the TODO in cmd/memberagent/main.go).
	env, err := auth.EnvironmentFromName("AzurePublicCloud")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the Azure public cloud environment: %w", err)
	}

	// In the case of Azure property provider, there is no need to wait for leader election
	// successes; close the channel immediately to allow immediate boot-up of the pricing
	// client.
	ch := make(chan struct{})
	close(ch)

	return &AKSKarpenterPricingClient{
		karpenterPricingClient: pricing.NewProvider(ctx, env, client.New(env.Cloud), region, ch),
	}, nil
}
