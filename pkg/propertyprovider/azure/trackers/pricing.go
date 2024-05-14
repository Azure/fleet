/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trackers

import (
	"context"
	"time"

	"github.com/Azure/karpenter/pkg/providers/pricing"
	"github.com/Azure/karpenter/pkg/providers/pricing/client"
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
func NewAKSKarpenterPricingClient(ctx context.Context, region string) *AKSKarpenterPricingClient {
	// In the case of Azure property provider, there is no need to wait for leader election
	// successes; close the channel immediately to allow immediate boot-up of the pricing
	// client.
	ch := make(chan struct{})
	close(ch)

	return &AKSKarpenterPricingClient{
		karpenterPricingClient: pricing.NewProvider(ctx, client.New(), region, ch),
	}
}
