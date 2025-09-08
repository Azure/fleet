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

package workapplier

import (
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

type waveNumber int

const (
	lastWave waveNumber = 999
)

var (
	// The default wave number for all known Kubernetes resource type.
	//
	// Note (chenyu1): the waves below are based on the Helm resource installation
	// order (see also the Helm source code). Similar objects are grouped together
	// to achieve best performance.
	defaultWaveNumberByResourceType = map[string]waveNumber{
		// Apply namespaces and priority classes first.
		"namespaces":      0,
		"priorityclasses": 0,
		// Apply policies, configuration data, and other static resources second.
		"networkpolicies":           1,
		"resourcequotas":            1,
		"limitranges":               1,
		"podsecuritypolicies":       1,
		"poddisruptionbudgets":      1,
		"serviceaccounts":           1,
		"secrets":                   1,
		"configmaps":                1,
		"storageclasses":            1,
		"persistentvolumes":         1,
		"persistentvolumeclaims":    1,
		"customresourcedefinitions": 1,
		"ingressclasses":            1,
		// Apply RBAC resources (cluster roles and roles).
		"clusterroles": 2,
		"roles":        2,
		// Apply RBAC resources (cluster role bindings and role bindings).
		"clusterrolebindings": 3,
		"rolebindings":        3,
		// Apply workloads and services.
		"services":                 4,
		"daemonsets":               4,
		"pods":                     4,
		"replicationcontrollers":   4,
		"replicasets":              4,
		"deployments":              4,
		"horizontalpodautoscalers": 4,
		"statefulsets":             4,
		"jobs":                     4,
		"cronjobs":                 4,
		"ingresses":                4,
		// Apply API services and webhooks.
		"apiservices":                     5,
		"validatingwebhookconfigurations": 5,
		"mutatingwebhookconfigurations":   5,
	}

	// The API groups for all known Kubernetes resource types.
	knownAPIGroups = sets.New(
		// The core API group.
		"",
		// The networking API group (`networking.k8s.io`).
		"networking.k8s.io",
		// The scheduling API group (`scheduling.k8s.io`).
		"scheduling.k8s.io",
		// The policy API group (`policy.k8s.io`).
		"policy",
		// The storage API group (`storage.k8s.io`).
		"storage.k8s.io",
		// The API extensions API group (`apiextensions.k8s.io`).
		"apiextensions.k8s.io",
		// The RBAC authorization API group (`rbac.authorization.k8s.io`).
		"rbac.authorization.k8s.io",
		// The apps API group (`apps`).
		"apps",
		// The autoscaling API group (`autoscaling`).
		"autoscaling",
		// The API registration API group (`apiregistration.k8s.io`).
		"apiregistration.k8s.io",
		// The admission registration API group (`admissionregistration.k8s.io`).
		"admissionregistration.k8s.io",
		// The batch API group (`batch`).
		"batch",
	)
)

// bundleProcessingWave is a wave of bundles that can be processed in parallel.
type bundleProcessingWave struct {
	num     waveNumber
	bundles []*manifestProcessingBundle
}

// organizeBundlesIntoProcessingWaves organizes the list of bundles into different
// waves of bundles for parallel processing based on their GVR information.
func organizeBundlesIntoProcessingWaves(bundles []*manifestProcessingBundle, workRef klog.ObjectRef) []*bundleProcessingWave {
	// Pre-allocate the map; 7 is the total count of default wave numbers, though
	// not all wave numbers might be used.
	waveByNum := make(map[waveNumber]*bundleProcessingWave, 7)

	getOrAddWave := func(num waveNumber) *bundleProcessingWave {
		wave, ok := waveByNum[num]
		if !ok {
			wave = &bundleProcessingWave{
				num: num,
				// Pre-allocate with a reasonable size.
				bundles: make([]*manifestProcessingBundle, 0, 5),
			}
			waveByNum[num] = wave
		}
		return wave
	}

	// For simplicity reasons, the organization itself runs in sequential order.
	// Considering that the categorization itself is quick and the total number of bundles
	// should be limited in most cases, this should not introduce significant overhead.
	for idx := range bundles {
		bundle := bundles[idx]
		if bundle.gvr == nil {
			// For manifest data that cannot be decoded, there might not be any available GVR
			// information. Skip such processing bundles; this is not considered as an error.
			klog.V(2).InfoS("Skipping a bundle with no GVR; no wave is assigned",
				"ordinal", idx, "work", workRef)
			continue
		}

		if bundle.applyOrReportDiffErr != nil {
			// An error has occurred before this step; such bundles need no further processing,
			// skip them. This is not considered as an error.
			klog.V(2).InfoS("Skipping a bundle with prior processing error; no wave is assigned",
				"manifestObj", klog.KObj(bundle.manifestObj), "GVR", *bundle.gvr, "work", workRef)
			continue
		}

		waveNum := lastWave
		defaultWaveNum, foundInDefaultWaveNumber := defaultWaveNumberByResourceType[bundle.gvr.Resource]
		if foundInDefaultWaveNumber && knownAPIGroups.Has(bundle.gvr.Group) {
			// The resource is a known one; assign the bundle to its default wave.
			waveNum = defaultWaveNum
		}

		wave := getOrAddWave(waveNum)
		wave.bundles = append(wave.bundles, bundle)
		klog.V(2).InfoS("Assigned manifest to a wave",
			"waveNumber", waveNum,
			"manifestObj", klog.KObj(bundle.manifestObj), "GVR", *bundle.gvr, "work", workRef)
	}

	// Retrieve all the waves and sort them by their wave number.
	waves := make([]*bundleProcessingWave, 0, len(waveByNum))
	for _, w := range waveByNum {
		waves = append(waves, w)
	}
	// Sort the waves in ascending order.
	slices.SortFunc(waves, func(a, b *bundleProcessingWave) int {
		return int(a.num) - int(b.num)
	})
	return waves
}
