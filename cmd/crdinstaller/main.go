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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
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

	// Create dynamic client.
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// Install CRDs from the fixed location.
	const crdPath = "/workspace/config/crd/bases"
	if err := installCRDs(ctx, dynamicClient, crdPath, *mode, *waitForSuccess, *timeout); err != nil {
		klog.Fatalf("Failed to install CRDs: %v", err)
	}

	klog.Infof("Successfully installed %s CRDs", *mode)
}

// installCRDs installs the CRDs from the specified directory based on the mode.
func installCRDs(ctx context.Context, client dynamic.Interface, crdPath, mode string, wait bool, _ int) error {
	// CRD GVR
	crdGVR := schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}

	// Set of CRD filenames to install based on mode.
	crdFilesToInstall := sets.New[string]()

	// Walk through the CRD directory and collect filenames.
	if err := filepath.WalkDir(crdPath, func(path string, d fs.DirEntry, err error) error {
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
				crdFilesToInstall.Insert(path)
			}
		case "member":
			if isMemberCRD {
				crdFilesToInstall.Insert(path)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to walk CRD directory: %w", err)
	}

	if crdFilesToInstall.Len() == 0 {
		return fmt.Errorf("no CRDs found for mode %s in directory %s", mode, crdPath)
	}

	klog.Infof("Found %d CRDs to install for mode %s", crdFilesToInstall.Len(), mode)

	// Install each CRD.
	for path := range crdFilesToInstall {
		klog.V(2).Infof("Installing CRD from: %s", path)

		// Read and parse CRD file.
		crdBytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read CRD file %s: %w", path, err)
		}

		var crd unstructured.Unstructured
		if err := yaml.Unmarshal(crdBytes, &crd); err != nil {
			return fmt.Errorf("failed to unmarshal CRD from %s: %w", path, err)
		}

		// Apply CRD.
		crdName := crd.GetName()
		klog.V(2).Infof("Creating CRD: %s", crdName)

		_, err = client.Resource(crdGVR).Create(ctx, &crd, metav1.CreateOptions{})
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				klog.V(2).Infof("CRD %s already exists", crdName)
			} else {
				return fmt.Errorf("failed to create CRD %s: %w", crdName, err)
			}
		}

		klog.Infof("Successfully installed CRD: %s", crdName)
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
