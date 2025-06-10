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

// Package main contains the CRD installer utility for KubeFleet
package main

import (
	"context"
	"flag"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"go.goms.io/fleet/cmd/crdinstaller/utils"
)

var (
	// Common flags.
	enablev1alpha1API = flag.Bool("enablev1alpha1API", false, "Enable v1alpha1 APIs (default false)")
	enablev1beta1API  = flag.Bool("enablev1beta1API", false, "Enable v1beta1 APIs (default false)")
	mode              = flag.String("mode", "", "Mode to run in: 'hub' or 'member' (required)")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// Validate required flags.
	if *mode != "hub" && *mode != "member" {
		klog.Fatal("--mode flag must be either 'hub' or 'member'")
	}

	klog.Infof("Starting CRD installer in %s mode", *mode)

	// Print all flags for debugging.
	flag.VisitAll(func(f *flag.Flag) {
		klog.V(2).InfoS("flag:", "name", f.Name, "value", f.Value)
	})

	// Set up controller-runtime logger.
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	// Create context for API operations.
	ctx := ctrl.SetupSignalHandler()

	// Get Kubernetes config using controller-runtime.
	config := ctrl.GetConfigOrDie()

	// Create a scheme that knows about CRD types.
	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		klog.Fatalf("Failed to add apiextensions scheme: %v", err)
	}
	client, err := client.New(config, client.Options{
		Scheme: scheme,
	})

	if err != nil {
		klog.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// Install CRDs from the fixed location.
	const crdPath = "/workspace/config/crd/bases"
	if err := installCRDs(ctx, client, crdPath, *mode, *enablev1alpha1API, *enablev1beta1API); err != nil {
		klog.Fatalf("Failed to install CRDs: %v", err)
	}

	klog.Infof("Successfully installed %s CRDs", *mode)
}

// installCRDs installs the CRDs from the specified directory based on the mode.
func installCRDs(ctx context.Context, client client.Client, crdPath, mode string, enablev1alpha1API, enablev1beta1API bool) error {
	// Set of CRD filenames to install based on mode.
	crdFilesToInstall, err := utils.CollectCRDFileNames(crdPath, mode, enablev1alpha1API, enablev1beta1API)
	if err != nil {
		return err
	}

	if len(crdFilesToInstall) == 0 {
		return fmt.Errorf("no CRDs found for mode %s in directory %s", mode, crdPath)
	}

	klog.Infof("Found %d CRDs to install for mode %s", len(crdFilesToInstall), mode)

	// Install each CRD.
	for path := range crdFilesToInstall {
		if err := utils.InstallCRD(ctx, client, path); err != nil {
			return err
		}
	}

	return nil
}
