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
	"io/fs"
	"os"
	"path/filepath"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	// Common flags.
	enablev1alpha1API = flag.Bool("enablev1alpha1API", false, "Enable v1alpha1 APIs (default false)")
	enablev1beta1API  = flag.Bool("enablev1beta1API", false, "Enable v1beta1 APIs (default false)")
	mode              = flag.String("mode", "", "Mode to run in: 'hub' or 'member' (required)")
	waitForSuccess    = flag.Bool("wait", true, "Wait for CRDs to be established before returning")
	timeout           = flag.Int("timeout", 60, "Timeout in seconds for waiting for CRDs to be established")

	v1beta1HubCRDs = map[string]bool{
		"cluster.kubernetes-fleet.io_memberclusters.yaml":                              true,
		"cluster.kubernetes-fleet.io_internalmemberclusters.yaml":                      true,
		"placement.kubernetes-fleet.io_clusterapprovalrequests.yaml":                   true,
		"placement.kubernetes-fleet.io_clusterresourcebindings.yaml":                   true,
		"placement.kubernetes-fleet.io_clusterresourceenvelopes.yaml":                  true,
		"placement.kubernetes-fleet.io_clusterresourceplacements.yaml":                 true,
		"placement.kubernetes-fleet.io_clusterresourceoverrides.yaml":                  true,
		"placement.kubernetes-fleet.io_clusterresourceoverridesnapshots.yaml":          true,
		"placement.kubernetes-fleet.io_clusterresourceplacementdisruptionbudgets.yaml": true,
		"placement.kubernetes-fleet.io_clusterresourceplacementevictions.yaml":         true,
		"placement.kubernetes-fleet.io_clusterresourcesnapshots.yaml":                  true,
		"placement.kubernetes-fleet.io_clusterschedulingpolicysnapshots.yaml":          true,
		"placement.kubernetes-fleet.io_clusterstagedupdateruns.yaml":                   true,
		"placement.kubernetes-fleet.io_clusterstagedupdatestrategies.yaml":             true,
		"placement.kubernetes-fleet.io_resourceenvelopes.yaml":                         true,
		"placement.kubernetes-fleet.io_resourceoverrides.yaml":                         true,
		"placement.kubernetes-fleet.io_resourceoverridesnapshots.yaml":                 true,
		"placement.kubernetes-fleet.io_works.yaml":                                     true,
		"multicluster.x-k8s.io_clusterprofiles.yaml":                                   true,
	}
	v1beta1MemberCRDs = map[string]bool{
		"placement.kubernetes-fleet.io_appliedworks.yaml": true,
	}

	v1alpha1HubCRDs = map[string]bool{
		"fleet.azure.com_memberclusters.yaml":            true,
		"fleet.azure.com_internalmemberclusters.yaml":    true,
		"fleet.azure.com_clusterresourceplacements.yaml": true,
		"multicluster.x-k8s.io_works.yaml":               true,
	}
	v1alpha1MemberCRDs = map[string]bool{
		"multicluster.x-k8s.io_appliedworks.yaml": true,
	}
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
	if err := installCRDs(ctx, client, crdPath, *mode, *waitForSuccess, *timeout); err != nil {
		klog.Fatalf("Failed to install CRDs: %v", err)
	}

	klog.Infof("Successfully installed %s CRDs", *mode)
}

// installCRDs installs the CRDs from the specified directory based on the mode.
func installCRDs(ctx context.Context, client client.Client, crdPath, mode string, wait bool, _ int) error {
	// Set of CRD filenames to install based on mode.
	crdFilesToInstall := make(map[string]bool)

	// Walk through the CRD directory and collect filenames.
	if err := filepath.WalkDir(crdPath, func(path string, d fs.DirEntry, err error) error {
		// Handle errors from WalkDir.
		if err != nil {
			return err
		}

		// Skip directories.
		if d.IsDir() {
			return nil
		}

		// Only process yaml files.
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}

		// Process based on mode.
		filename := filepath.Base(path)
		isHubCRD := isHubCRD(filename)
		isMemberCRD := isMemberCRD(filename)

		switch mode {
		case "hub":
			if isHubCRD {
				crdFilesToInstall[path] = true
			}
		case "member":
			if isMemberCRD {
				crdFilesToInstall[path] = true
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to walk CRD directory: %w", err)
	}

	if len(crdFilesToInstall) == 0 {
		return fmt.Errorf("no CRDs found for mode %s in directory %s", mode, crdPath)
	}

	klog.Infof("Found %d CRDs to install for mode %s", len(crdFilesToInstall), mode)

	// Install each CRD.
	for path := range crdFilesToInstall {
		klog.V(2).Infof("Installing CRD from: %s", path)

		// Read and parse CRD file.
		crdBytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read CRD file %s: %w", path, err)
		}

		// Create decoder for converting raw bytes to Go types.
		codecFactory := serializer.NewCodecFactory(client.Scheme())
		decoder := codecFactory.UniversalDeserializer()

		// Decode YAML into a structured CRD object.
		obj, gvk, err := decoder.Decode(crdBytes, nil, nil)
		if err != nil {
			return fmt.Errorf("failed to decode CRD from %s: %w", path, err)
		}

		// Type assertion to make sure we have a CRD.
		crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
		if !ok {
			return fmt.Errorf("unexpected type from %s, expected CustomResourceDefinition but got %s", path, gvk)
		}

		// Create or update using the typed CRD.
		existingCRD := &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: crd.Name,
			},
		}

		createOrUpdateRes, err := controllerutil.CreateOrUpdate(ctx, client, existingCRD, func() error {
			// Copy spec from our decoded CRD to the object we're creating/updating.
			existingCRD.Spec = crd.Spec
			existingCRD.SetLabels(crd.Labels)

			// Add an additional ownership label to indicate this CRD is managed by the installer.
			if existingCRD.Labels == nil {
				existingCRD.Labels = make(map[string]string)
			}
			existingCRD.Labels["crd-installer.kubernetes-fleet.io/managed"] = "true"

			existingCRD.SetAnnotations(crd.Annotations)
			return nil
		})

		if err != nil {
			klog.ErrorS(err, "Failed to create or update CRD", "name", crd.Name, "operation", createOrUpdateRes)
			return err
		}

		klog.Infof("Successfully created/updated CRD %s", crd.Name)
	}

	if wait {
		// TODO: Implement wait logic for CRDs to be established.
		klog.Info("Waiting for CRDs to be established is not implemented yet")
	}

	return nil
}

// isHubCRD determines if a CRD should be installed on the hub cluster.
func isHubCRD(filename string) bool {
	var hubCRDs map[string]bool
	if *enablev1beta1API {
		hubCRDs = v1beta1HubCRDs
	} else if *enablev1alpha1API {
		hubCRDs = v1alpha1HubCRDs
	}

	return hubCRDs[filename]
}

// isMemberCRD determines if a CRD should be installed on the member cluster.
func isMemberCRD(filename string) bool {
	var memberCRDs map[string]bool
	if *enablev1beta1API {
		memberCRDs = v1beta1MemberCRDs
	} else if *enablev1alpha1API {
		memberCRDs = v1alpha1MemberCRDs
	}

	return memberCRDs[filename]
}
