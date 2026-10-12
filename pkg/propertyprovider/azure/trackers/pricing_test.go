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
	"testing"
)

// TestNewAKSKarpenterPricingClient tests the NewAKSKarpenterPricingClient function.
func TestNewAKSKarpenterPricingClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pricingClient, err := NewAKSKarpenterPricingClient(ctx, "eastus")
	if err != nil {
		t.Fatalf("NewAKSKarpenterPricingClient(ctx, \"eastus\") = %v, want no error", err)
	}
	if pricingClient == nil {
		t.Fatal("NewAKSKarpenterPricingClient(ctx, \"eastus\") = nil, want non-nil client")
	}

	// The pricing provider ships with static pricing data, so known instance types
	// resolve to a positive on-demand price even before any sync with the live API.
	price, found := pricingClient.OnDemandPrice("Standard_D2s_v3")
	if !found {
		t.Error("OnDemandPrice(\"Standard_D2s_v3\") not found, want found")
	}
	if price <= 0 {
		t.Errorf("OnDemandPrice(\"Standard_D2s_v3\") = %v, want positive price", price)
	}

	if _, found := pricingClient.OnDemandPrice("not-a-real-instance-type"); found {
		t.Error("OnDemandPrice(\"not-a-real-instance-type\") found, want not found")
	}

	// LastUpdated reports the static data timestamp before any live sync completes.
	if pricingClient.LastUpdated().IsZero() {
		t.Error("LastUpdated() = zero time, want non-zero timestamp")
	}
}
