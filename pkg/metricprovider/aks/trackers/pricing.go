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

// PricingProvider is an interface that the AKS metric provider uses to sync pricing information.
//
// It helps decouple the AKS metric provider from the actual pricing provider implementation; even
// though at this moment the metric provider leverages the AKS Karpenter pricing client only.
type PricingProvider interface {
	OnDemandPrice(instanceType string) (float64, bool)
	LastUpdated() time.Time
}

var _ PricingProvider = &AKSKarpenterPricingClient{}

// AKSKarpenterPricingClient is a thin wrapper around the AKS Karpenter pricing client, which
// implements the PricingProvider interface.
type AKSKarpenterPricingClient struct {
	kp *pricing.Provider
}

// OnDemandPrice returns the on-demand price of an instance type.
func (k *AKSKarpenterPricingClient) OnDemandPrice(instanceType string) (float64, bool) {
	return k.kp.OnDemandPrice(instanceType)
}

// LastUpdated returns the last time the pricing information was updated.
func (k *AKSKarpenterPricingClient) LastUpdated() time.Time {
	return k.kp.OnDemandLastUpdated()
}

func NewAKSKarpenterPricingClient(ctx context.Context, region string) *AKSKarpenterPricingClient {
	// In the case of AKS metric provider, there is no need to wait for leader election
	// successes; close the channel immediately to allow immediate boot-up of the pricing
	// client.
	ch := make(chan struct{})
	close(ch)

	return &AKSKarpenterPricingClient{
		kp: pricing.NewProvider(ctx, client.New(), region, ch),
	}
}
