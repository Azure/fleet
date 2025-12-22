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

// Package actuals features common actuals used in Ginkgo/Gomega tests.
package actuals

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func WorkObjectRemovedActual(ctx context.Context, hubClient client.Client, workName, workNamespace string) func() error {
	// Wait for the removal of the Work object.
	return func() error {
		work := &fleetv1beta1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNamespace}, work); !errors.IsNotFound(err) && err != nil {
			return fmt.Errorf("work object still exists or an unexpected error occurred: %w", err)
		}
		if controllerutil.ContainsFinalizer(work, fleetv1beta1.WorkFinalizer) {
			// The Work object is being deleted, but the finalizer is still present.
			return fmt.Errorf("work object is being deleted, but the finalizer is still present")
		}
		return nil
	}
}
