/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package main contains the CRD cleanup job for KubeFleet.
// This job cleans up all CRDs that were installed by the CRD installer
// when the Fleet agents are uninstalled via Helm pre-delete hook.
package main

import (
	"context"
	"flag"
	"os"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/cmd/crdinstaller/utils"
)

var mode = flag.String("mode", "", "Mode to run in: 'hub' or 'member' (required)")

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// Validate required flags.
	if *mode != "hub" && *mode != "member" {
		klog.Fatal("--mode flag must be either 'hub' or 'member'")
	}

	klog.Infof("Starting CRD cleanup job in %s mode", *mode)

	// Print all flags for debugging.
	flag.VisitAll(func(f *flag.Flag) {
		klog.V(2).InfoS("flag:", "name", f.Name, "value", f.Value)
	})

	// Get Kubernetes config using controller-runtime.
	config := ctrl.GetConfigOrDie()

	// Create a scheme that knows about CRD types.
	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		klog.Fatalf("Failed to add apiextensions scheme: %v", err)
	}

	k8sClient, err := client.New(config, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		klog.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// Create context for cleanup operations.
	ctx := context.Background()

	// Perform cleanup.
	if err := cleanupCRDs(ctx, k8sClient, *mode); err != nil {
		klog.Errorf("Failed to cleanup CRDs: %v", err)
		os.Exit(1)
	}

	klog.Info("CRD cleanup completed successfully")
}

// cleanupCRDs deletes all CRDs that were installed by the CRD installer for the given mode.
// It uses the mode label to identify which CRDs to delete.
func cleanupCRDs(ctx context.Context, k8sClient client.Client, mode string) error {
	// List all CRDs with both the managed label and the matching mode label.
	crdList := &apiextensionsv1.CustomResourceDefinitionList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		utils.CRDInstallerLabelKey:  "true",
		utils.CRDInstallerModeLabel: mode,
	})

	if err := k8sClient.List(ctx, crdList, &client.ListOptions{
		LabelSelector: labelSelector,
	}); err != nil {
		return err
	}

	klog.Infof("Found %d CRDs to cleanup for mode %s", len(crdList.Items), mode)

	// Delete all matching CRDs.
	var deletedCount int
	for i := range crdList.Items {
		crd := &crdList.Items[i]

		klog.Infof("Deleting CRD: %s", crd.Name)
		if err := k8sClient.Delete(ctx, crd); err != nil {
			klog.Errorf("Failed to delete CRD %s: %v", crd.Name, err)
			// Continue with other CRDs even if one fails.
			continue
		}
		deletedCount++
		klog.Infof("Successfully deleted CRD: %s", crd.Name)
	}

	klog.Infof("Cleanup complete: deleted %d CRDs", deletedCount)
	return nil
}
