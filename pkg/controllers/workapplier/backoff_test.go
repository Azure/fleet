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
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// TestWhenWithFullSequence tests the When method.
func TestWhenWithFullNormalSequence(t *testing.T) {
	work := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberReservedNSName,
			Name:      workName,
		},
	}
	bundles := []*manifestProcessingBundle{}
	rateLimiter := NewRequeueMultiStageWithExponentialBackoffRateLimiter(
		2,   // 2 attempts with fix delays.
		5,   // Use a fix delay of 5 seconds for the first two attempts.
		1.2, // For slow backoffs, use an exponential base of 1.2.
		2,   // Start the slow backoff with a delay of 2 seconds.
		15,  // Cap the slow backoff at 15 seconds.
		1.5, // For fast backoffs, use an exponential base of 1.5.
		60,  // Cap the fast backoff at 60 seconds.
		// Do not skip to the fast backoff stage for applicable Work objects. See the other test case for relevant cases.
		false,
	)

	testCases := []struct {
		name                    string
		wantRequeueDelaySeconds float64
	}{
		{
			name:                    "attempt #1",
			wantRequeueDelaySeconds: 5, // First attempt with a fix delay of 5 seconds.
		},
		{
			name:                    "attempt #2",
			wantRequeueDelaySeconds: 5, // Second attempt with a fix delay of 5 seconds.
		},
		{
			name:                    "attempt #3",
			wantRequeueDelaySeconds: 2, // Start the slow backoff with a delay of 2 seconds.
		},
		{
			name:                    "attempt #4",
			wantRequeueDelaySeconds: 2.4, // slow backoff: 2 * 1.2 = 2.4 seconds.
		},
		{
			name:                    "attempt #5",
			wantRequeueDelaySeconds: 2.88, // slow backoff: 2.4 * 1.2 = 2.88 seconds.
		},
		{
			name:                    "attempt #6",
			wantRequeueDelaySeconds: 3.456, // slow backoff: 2.88 * 1.2 = 3.456 seconds.
		},
		{
			name:                    "attempt #7",
			wantRequeueDelaySeconds: 4.1472, // slow backoff: 3.456 * 1.2 = 4.1472 seconds.
		},
		{
			name:                    "attempt #8",
			wantRequeueDelaySeconds: 4.9766, // slow backoff: 4.1472 * 1.2 = 4.97664 seconds.
		},
		{
			name:                    "attempt #9",
			wantRequeueDelaySeconds: 5.9720, // slow backoff: 4.97664 * 1.2 = 5.971968 seconds.
		},
		{
			name:                    "attempt #10",
			wantRequeueDelaySeconds: 7.1664, // slow backoff: 5.971968 * 1.2 = 7.1663616 seconds.
		},
		{
			name:                    "attempt #11",
			wantRequeueDelaySeconds: 8.5996, // slow backoff: 7.1663616 * 1.2 = 8.59963392 seconds.
		},
		{
			name:                    "attempt #12",
			wantRequeueDelaySeconds: 10.3194, // slow backoff: 8.59963392 * 1.2 = 10.319560704 seconds.
		},
		{
			name:                    "attempt #13",
			wantRequeueDelaySeconds: 12.3835, // slow backoff: 10.319560704 * 1.2 = 12.3834728448 seconds.
		},
		{
			name:                    "attempt #14",
			wantRequeueDelaySeconds: 14.8602, // slow backoff: 12.3834728448 * 1.2 = 14.86016741376 seconds.
		},
		{
			name:                    "attempt #15",
			wantRequeueDelaySeconds: 22.2903, // fast backoff: 14.86016741376 * 1.5 = 22.29025112064 seconds.
		},
		{
			name:                    "attempt #16",
			wantRequeueDelaySeconds: 33.4354, // fast backoff: 22.29025112064 * 1.5 = 33.43537668096 seconds.
		},
		{
			name:                    "attempt #17",
			wantRequeueDelaySeconds: 50.1531, // fast backoff: 33.43537668096 * 1.5 = 50.15306502144 seconds.
		},
		{
			name:                    "attempt #18",
			wantRequeueDelaySeconds: 60, // fast backoff: 50.15306502144 * 1.5 = 75.22959753216 seconds, but capped at 60 seconds.
		},
		{
			name:                    "attempt #19",
			wantRequeueDelaySeconds: 60, // reached the max. delay cap.
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requeueDelay := rateLimiter.When(work, bundles)
			requeueDelaySeconds := requeueDelay.Seconds()
			if !cmp.Equal(
				requeueDelaySeconds, tc.wantRequeueDelaySeconds,
				cmpopts.EquateApprox(0.0, 0.01), // Account for precision loss and approximation.
			) {
				t.Errorf("When() = %v, want %v", requeueDelay, tc.wantRequeueDelaySeconds)
			}
		})
	}
}

// TestWhenWithFullNoSlowBackoffSequence tests the When method.
func TestWhenWithFullNoSlowBackoffSequence(t *testing.T) {
	work := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberReservedNSName,
			Name:      workName,
		},
	}
	bundles := []*manifestProcessingBundle{}
	rateLimiter := NewRequeueMultiStageWithExponentialBackoffRateLimiter(
		1,  // 1 attempts with fix delays.
		5,  // Use a fix delay of 5 seconds for the first attempt.
		2,  // For slow backoffs, use an exponential base of 2.
		2,  // Start the slow backoff with a delay of 2 seconds.
		2,  // Cap the slow backoff at 2 seconds.
		5,  // For fast backoffs, use an exponential base of 5.
		20, // Cap the fast backoff at 20 seconds.
		// Do not skip to the fast backoff stage for applicable Work objects. See the other test case for relevant cases.
		false,
	)

	testCases := []struct {
		name                    string
		wantRequeueDelaySeconds float64
	}{
		{
			name:                    "attempt #1",
			wantRequeueDelaySeconds: 5, // First attempt with a fix delay of 5 seconds.
		},
		{
			name:                    "attempt #2",
			wantRequeueDelaySeconds: 2, // Start the slow backoff with a delay of 2 seconds.
		},
		{
			name:                    "attempt #3",
			wantRequeueDelaySeconds: 10, // Immediately start the fast backoff: 2 * 5 = 10 seconds.
		},
		{
			name:                    "attempt #4",
			wantRequeueDelaySeconds: 20, // Fast backoff: 10 * 5 = 50 seconds, but capped at 20 seconds.
		},
		{
			name:                    "attempt #5",
			wantRequeueDelaySeconds: 20, // Reached the max. delay cap.
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requeueDelay := rateLimiter.When(work, bundles)
			requeueDelaySeconds := requeueDelay.Seconds()
			if !cmp.Equal(
				requeueDelaySeconds, tc.wantRequeueDelaySeconds,
				cmpopts.EquateApprox(0.0, 0.01), // Account for precision loss and approximation.
			) {
				t.Errorf("When() = %v, want %v", requeueDelay, tc.wantRequeueDelaySeconds)
			}
		})
	}
}

// TestWhenWithFullNoFastBackoffSequeuce tests the When method.
func TestWhenWithFullNoFastBackoffSequeuce(t *testing.T) {
	work := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberReservedNSName,
			Name:      workName,
		},
	}
	bundles := []*manifestProcessingBundle{}
	rateLimiter := NewRequeueMultiStageWithExponentialBackoffRateLimiter(
		1,  // 1 attempts with fix delays.
		5,  // Use a fix delay of 5 seconds for the first attempt.
		2,  // For slow backoffs, use an exponential base of 2.
		2,  // Start the slow backoff with a delay of 2 seconds.
		10, // Cap the slow backoff at 10 seconds.
		5,  // For fast backoffs, use an exponential base of 5.
		10, // Cap the fast backoff at 20 seconds.
		// Do not skip to the fast backoff stage for applicable Work objects. See the other test case for relevant cases.
		false,
	)

	testCases := []struct {
		name                    string
		wantRequeueDelaySeconds float64
	}{
		{
			name:                    "attempt #1",
			wantRequeueDelaySeconds: 5, // First attempt with a fix delay of 5 seconds.
		},
		{
			name:                    "attempt #2",
			wantRequeueDelaySeconds: 2, // Start the slow backoff with a delay of 2 seconds.
		},
		{
			name:                    "attempt #3",
			wantRequeueDelaySeconds: 4, // slow backoff: 2 * 2 = 4 seconds.
		},
		{
			name:                    "attempt #4",
			wantRequeueDelaySeconds: 8, // slow backoff: 4 * 2 = 8 seconds.
		},
		{
			name:                    "attempt #5",
			wantRequeueDelaySeconds: 10, // fast backoff: 8 * 5 = 40 seconds, but capped at 10 seconds.
		},
		{
			name:                    "attempt #6",
			wantRequeueDelaySeconds: 10, // Reached the max. delay cap.
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requeueDelay := rateLimiter.When(work, bundles)
			requeueDelaySeconds := requeueDelay.Seconds()
			if !cmp.Equal(
				requeueDelaySeconds, tc.wantRequeueDelaySeconds,
				cmpopts.EquateApprox(0.0, 0.01), // Account for precision loss and approximation.
			) {
				t.Errorf("When() = %v, want %v", requeueDelay, tc.wantRequeueDelaySeconds)
			}
		})
	}
}

// TestWhenWithNoBackoffSequence tests the When method.
func TestWhenWithNoBackoffSequence(t *testing.T) {
	work := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberReservedNSName,
			Name:      workName,
		},
	}
	bundles := []*manifestProcessingBundle{}
	rateLimiter := NewRequeueMultiStageWithExponentialBackoffRateLimiter(
		1,  // 1 attempts with fix delays.
		5,  // Use a fix delay of 5 seconds for the first attempt.
		2,  // For slow backoffs, use an exponential base of 2.
		10, // Start the slow backoff with a delay of 10 seconds.
		10, // Cap the slow backoff at 10 seconds.
		5,  // For fast backoffs, use an exponential base of 5.
		10, // Cap the fast backoff at 10 seconds.
		// Do not skip to the fast backoff stage for applicable Work objects. See the other test case for relevant cases.
		false,
	)

	testCases := []struct {
		name                    string
		wantRequeueDelaySeconds float64
	}{
		{
			name:                    "attempt #1",
			wantRequeueDelaySeconds: 5, // First attempt with a fix delay of 5 seconds.
		},
		{
			name:                    "attempt #2",
			wantRequeueDelaySeconds: 10, // Start the slow backoff with a delay of 10 seconds.
		},
		{
			name:                    "attempt #3",
			wantRequeueDelaySeconds: 10, // fast backoff: 10 * 5 = 50 seconds, but capped at 10 seconds.
		},
		{
			name:                    "attempt #4",
			wantRequeueDelaySeconds: 10, // Reached the max. delay cap.
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requeueDelay := rateLimiter.When(work, bundles)
			requeueDelaySeconds := requeueDelay.Seconds()
			if !cmp.Equal(
				requeueDelaySeconds, tc.wantRequeueDelaySeconds,
				cmpopts.EquateApprox(0.0, 0.01), // Account for precision loss and approximation.
			) {
				t.Errorf("When() = %v, want %v", requeueDelay, tc.wantRequeueDelaySeconds)
			}
		})
	}
}

// TestNewRequeueMultiStageWithExponentialBackoffRateLimiter tests the NewRequeueMultiStageWithExponentialBackoffRateLimiter function.
func TestNewRequeueMultiStageWithExponentialBackoffRateLimiter(t *testing.T) {
	testCases := []struct {
		name                                                   string
		attemptsWithFixedDelay                                 int
		fixedDelaySeconds                                      float64
		exponentialBaseForSlowBackoff                          float64
		initialSlowBackoffDelaySeconds                         float64
		maxSlowBackoffDelaySeconds                             float64
		exponentialBaseForFastBackoff                          float64
		maxFastBackoffDelaySeconds                             float64
		skipToFastBackoffForAvailableOrDiffReportedWorkObjs    bool
		wantRequeueMultiStageWithExponentialBackoffRateLimiter *RequeueMultiStageWithExponentialBackoffRateLimiter
	}{
		{
			name:                           "all valid parameters",
			attemptsWithFixedDelay:         10,
			fixedDelaySeconds:              5,
			exponentialBaseForSlowBackoff:  1.2,
			initialSlowBackoffDelaySeconds: 2,
			maxSlowBackoffDelaySeconds:     15,
			exponentialBaseForFastBackoff:  1.5,
			maxFastBackoffDelaySeconds:     60,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: true,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              10,
				fixedDelay:                                          time.Duration(5) * time.Second,
				exponentialBaseForSlowBackoff:                       1.2,
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,
				maxSlowBackoffDelay:                                 time.Duration(15) * time.Second,
				exponentialBaseForFastBackoff:                       1.5,
				maxFastBackoffDelay:                                 time.Duration(60) * time.Second,
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: true,
			},
		},
		{
			name:                           "invalid attemptsWithFixedDelay (too little)",
			attemptsWithFixedDelay:         -1,
			fixedDelaySeconds:              5,
			exponentialBaseForSlowBackoff:  1.2,
			initialSlowBackoffDelaySeconds: 2,
			maxSlowBackoffDelaySeconds:     15,
			exponentialBaseForFastBackoff:  1.5,
			maxFastBackoffDelaySeconds:     60,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              minAttemptsWithFixedDelay,
				fixedDelay:                                          time.Duration(5) * time.Second,
				exponentialBaseForSlowBackoff:                       1.2,
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,
				maxSlowBackoffDelay:                                 time.Duration(15) * time.Second,
				exponentialBaseForFastBackoff:                       1.5,
				maxFastBackoffDelay:                                 time.Duration(60) * time.Second,
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "invalid attemptsWithFixedDelay (too large)",
			attemptsWithFixedDelay:         41,
			fixedDelaySeconds:              5,
			exponentialBaseForSlowBackoff:  1.2,
			initialSlowBackoffDelaySeconds: 2,
			maxSlowBackoffDelaySeconds:     15,
			exponentialBaseForFastBackoff:  1.5,
			maxFastBackoffDelaySeconds:     60,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              minAttemptsWithFixedDelay,
				fixedDelay:                                          time.Duration(5) * time.Second,
				exponentialBaseForSlowBackoff:                       1.2,
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,
				maxSlowBackoffDelay:                                 time.Duration(15) * time.Second,
				exponentialBaseForFastBackoff:                       1.5,
				maxFastBackoffDelay:                                 time.Duration(60) * time.Second,
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "invalid fixedDelaySeconds (too small)",
			attemptsWithFixedDelay:         5,
			fixedDelaySeconds:              1,
			exponentialBaseForSlowBackoff:  1.2,
			initialSlowBackoffDelaySeconds: 2,
			maxSlowBackoffDelaySeconds:     15,
			exponentialBaseForFastBackoff:  1.5,
			maxFastBackoffDelaySeconds:     60,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              5,
				fixedDelay:                                          time.Duration(2) * time.Second, // Should be reset to minFixedDelaySeconds
				exponentialBaseForSlowBackoff:                       1.2,
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,
				maxSlowBackoffDelay:                                 time.Duration(15) * time.Second,
				exponentialBaseForFastBackoff:                       1.5,
				maxFastBackoffDelay:                                 time.Duration(60) * time.Second,
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "invalid exponentialBaseForSlowBackoff (too small)",
			attemptsWithFixedDelay:         5,
			fixedDelaySeconds:              5,
			exponentialBaseForSlowBackoff:  1.0,
			initialSlowBackoffDelaySeconds: 2,
			maxSlowBackoffDelaySeconds:     15,
			exponentialBaseForFastBackoff:  1.5,
			maxFastBackoffDelaySeconds:     60,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              5,
				fixedDelay:                                          time.Duration(5) * time.Second,
				exponentialBaseForSlowBackoff:                       1.2, // Reset to default value
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,
				maxSlowBackoffDelay:                                 time.Duration(15) * time.Second,
				exponentialBaseForFastBackoff:                       1.2, // Reset to default value
				maxFastBackoffDelay:                                 time.Duration(60) * time.Second,
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "invalid exponentialBaseForFastBackoff (too large)",
			attemptsWithFixedDelay:         5,
			fixedDelaySeconds:              5,
			exponentialBaseForSlowBackoff:  1.2,
			initialSlowBackoffDelaySeconds: 2,
			maxSlowBackoffDelaySeconds:     15,
			exponentialBaseForFastBackoff:  150.0,
			maxFastBackoffDelaySeconds:     60,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              5,
				fixedDelay:                                          time.Duration(5) * time.Second,
				exponentialBaseForSlowBackoff:                       1.2, // Reset to default value
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,
				maxSlowBackoffDelay:                                 time.Duration(15) * time.Second,
				exponentialBaseForFastBackoff:                       1.2, // Reset to default value
				maxFastBackoffDelay:                                 time.Duration(60) * time.Second,
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "invalid slow > fast exponential base relationship",
			attemptsWithFixedDelay:         5,
			fixedDelaySeconds:              5,
			exponentialBaseForSlowBackoff:  2.0,
			initialSlowBackoffDelaySeconds: 2,
			maxSlowBackoffDelaySeconds:     15,
			exponentialBaseForFastBackoff:  1.5,
			maxFastBackoffDelaySeconds:     60,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              5,
				fixedDelay:                                          time.Duration(5) * time.Second,
				exponentialBaseForSlowBackoff:                       1.2, // Reset to default value
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,
				maxSlowBackoffDelay:                                 time.Duration(15) * time.Second,
				exponentialBaseForFastBackoff:                       1.2, // Reset to default value
				maxFastBackoffDelay:                                 time.Duration(60) * time.Second,
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "invalid initialSlowBackoffDelaySeconds (too small)",
			attemptsWithFixedDelay:         5,
			fixedDelaySeconds:              5,
			exponentialBaseForSlowBackoff:  1.2,
			initialSlowBackoffDelaySeconds: 1,
			maxSlowBackoffDelaySeconds:     15,
			exponentialBaseForFastBackoff:  1.5,
			maxFastBackoffDelaySeconds:     60,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              5,
				fixedDelay:                                          time.Duration(5) * time.Second,
				exponentialBaseForSlowBackoff:                       1.2,
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,  // Reset to default
				maxSlowBackoffDelay:                                 time.Duration(30) * time.Second, // Reset to default
				exponentialBaseForFastBackoff:                       1.5,
				maxFastBackoffDelay:                                 time.Duration(900) * time.Second, // Reset to default
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "invalid delay hierarchy (initial > max slow)",
			attemptsWithFixedDelay:         5,
			fixedDelaySeconds:              5,
			exponentialBaseForSlowBackoff:  1.2,
			initialSlowBackoffDelaySeconds: 20,
			maxSlowBackoffDelaySeconds:     15,
			exponentialBaseForFastBackoff:  1.5,
			maxFastBackoffDelaySeconds:     60,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              5,
				fixedDelay:                                          time.Duration(5) * time.Second,
				exponentialBaseForSlowBackoff:                       1.2,
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,  // Reset to default
				maxSlowBackoffDelay:                                 time.Duration(30) * time.Second, // Reset to default
				exponentialBaseForFastBackoff:                       1.5,
				maxFastBackoffDelay:                                 time.Duration(900) * time.Second, // Reset to default
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "invalid delay hierarchy (max slow > max fast)",
			attemptsWithFixedDelay:         5,
			fixedDelaySeconds:              5,
			exponentialBaseForSlowBackoff:  1.2,
			initialSlowBackoffDelaySeconds: 2,
			maxSlowBackoffDelaySeconds:     120,
			exponentialBaseForFastBackoff:  1.5,
			maxFastBackoffDelaySeconds:     60,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              5,
				fixedDelay:                                          time.Duration(5) * time.Second,
				exponentialBaseForSlowBackoff:                       1.2,
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,  // Reset to default
				maxSlowBackoffDelay:                                 time.Duration(30) * time.Second, // Reset to default
				exponentialBaseForFastBackoff:                       1.5,
				maxFastBackoffDelay:                                 time.Duration(900) * time.Second, // Reset to default
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "invalid maxFastBackoffDelaySeconds (too large)",
			attemptsWithFixedDelay:         5,
			fixedDelaySeconds:              5,
			exponentialBaseForSlowBackoff:  1.2,
			initialSlowBackoffDelaySeconds: 2,
			maxSlowBackoffDelaySeconds:     15,
			exponentialBaseForFastBackoff:  1.5,
			maxFastBackoffDelaySeconds:     4000, // Exceeds maxMaxFastBackoffDelaySeconds (3600)
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              5,
				fixedDelay:                                          time.Duration(5) * time.Second,
				exponentialBaseForSlowBackoff:                       1.2,
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,  // Reset to default
				maxSlowBackoffDelay:                                 time.Duration(30) * time.Second, // Reset to default
				exponentialBaseForFastBackoff:                       1.5,
				maxFastBackoffDelay:                                 time.Duration(900) * time.Second, // Reset to default
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "boundary values (minimum valid)",
			attemptsWithFixedDelay:         1,    // minAttemptsWithFixedDelay
			fixedDelaySeconds:              2,    // minFixedDelaySeconds
			exponentialBaseForSlowBackoff:  1.05, // minExponentialBaseForSlowBackoff
			initialSlowBackoffDelaySeconds: 2,    // minInitialSlowBackoffDelaySeconds
			maxSlowBackoffDelaySeconds:     2,
			exponentialBaseForFastBackoff:  1.05,
			maxFastBackoffDelaySeconds:     2,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              1,
				fixedDelay:                                          time.Duration(2) * time.Second,
				exponentialBaseForSlowBackoff:                       1.05,
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,
				maxSlowBackoffDelay:                                 time.Duration(2) * time.Second,
				exponentialBaseForFastBackoff:                       1.05,
				maxFastBackoffDelay:                                 time.Duration(2) * time.Second,
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "boundary values (maximum valid)",
			attemptsWithFixedDelay:         40, // maxAttemptsWithFixedDelay
			fixedDelaySeconds:              100,
			exponentialBaseForSlowBackoff:  50.0,
			initialSlowBackoffDelaySeconds: 100,
			maxSlowBackoffDelaySeconds:     1000,
			exponentialBaseForFastBackoff:  100.0, // maxExponentialBaseForFastBackoff
			maxFastBackoffDelaySeconds:     3600,  // maxMaxFastBackoffDelaySeconds
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: true,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              40,
				fixedDelay:                                          time.Duration(100) * time.Second,
				exponentialBaseForSlowBackoff:                       50.0,
				initialSlowBackoffDelay:                             time.Duration(100) * time.Second,
				maxSlowBackoffDelay:                                 time.Duration(1000) * time.Second,
				exponentialBaseForFastBackoff:                       100.0,
				maxFastBackoffDelay:                                 time.Duration(3600) * time.Second,
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: true,
			},
		},
		{
			name:                           "zero values test",
			attemptsWithFixedDelay:         0,
			fixedDelaySeconds:              0,
			exponentialBaseForSlowBackoff:  0,
			initialSlowBackoffDelaySeconds: 0,
			maxSlowBackoffDelaySeconds:     0,
			exponentialBaseForFastBackoff:  0,
			maxFastBackoffDelaySeconds:     0,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              minAttemptsWithFixedDelay,        // Reset to minimum
				fixedDelay:                                          time.Duration(2) * time.Second,   // Reset to minimum
				exponentialBaseForSlowBackoff:                       1.2,                              // Reset to default value
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,   // Reset to default
				maxSlowBackoffDelay:                                 time.Duration(30) * time.Second,  // Reset to default
				exponentialBaseForFastBackoff:                       1.2,                              // Reset to default value
				maxFastBackoffDelay:                                 time.Duration(900) * time.Second, // Reset to default
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
			},
		},
		{
			name:                           "negative values test",
			attemptsWithFixedDelay:         -5,
			fixedDelaySeconds:              -2,
			exponentialBaseForSlowBackoff:  -1.2,
			initialSlowBackoffDelaySeconds: -3,
			maxSlowBackoffDelaySeconds:     -10,
			exponentialBaseForFastBackoff:  -1.5,
			maxFastBackoffDelaySeconds:     -60,
			skipToFastBackoffForAvailableOrDiffReportedWorkObjs: true,
			wantRequeueMultiStageWithExponentialBackoffRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter:                                      make(map[types.NamespacedName]int),
				lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
				lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
				lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
				attemptsWithFixedDelay:                              minAttemptsWithFixedDelay,        // Reset to minimum
				fixedDelay:                                          time.Duration(2) * time.Second,   // Reset to minimum
				exponentialBaseForSlowBackoff:                       1.2,                              // Reset to default value
				initialSlowBackoffDelay:                             time.Duration(2) * time.Second,   // Reset to default
				maxSlowBackoffDelay:                                 time.Duration(30) * time.Second,  // Reset to default
				exponentialBaseForFastBackoff:                       1.2,                              // Reset to default value
				maxFastBackoffDelay:                                 time.Duration(900) * time.Second, // Reset to default
				skipToFastBackoffForAvailableOrDiffReportedWorkObjs: true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rateLimiter := NewRequeueMultiStageWithExponentialBackoffRateLimiter(
				tc.attemptsWithFixedDelay,
				tc.fixedDelaySeconds,
				tc.exponentialBaseForSlowBackoff,
				tc.initialSlowBackoffDelaySeconds,
				tc.maxSlowBackoffDelaySeconds,
				tc.exponentialBaseForFastBackoff,
				tc.maxFastBackoffDelaySeconds,
				tc.skipToFastBackoffForAvailableOrDiffReportedWorkObjs,
			)

			if diff := cmp.Diff(
				rateLimiter, tc.wantRequeueMultiStageWithExponentialBackoffRateLimiter,
				cmpopts.EquateEmpty(),
				cmp.AllowUnexported(RequeueMultiStageWithExponentialBackoffRateLimiter{}),
				cmpopts.IgnoreFields(RequeueMultiStageWithExponentialBackoffRateLimiter{}, "mu"),
			); diff != "" {
				t.Errorf("RequeueMultiStageWithExponentialBackoffRateLimiter mismatches (-got +want):\n%s", diff)
			}
		})
	}
}

// TestWhenWithGenerationAndProcessingResultChange tests the When method.
func TestWhenWithGenerationAndProcessingResultChange(t *testing.T) {
	rateLimiter := NewRequeueMultiStageWithExponentialBackoffRateLimiter(
		1,     // 1 attempts with fixed delay.
		5,     // Use a fixed delay of 5 seconds for the first attempt.
		2,     // For slow backoffs, use an exponential base of 2.
		10,    // Start the slow backoff with a delay of 10 seconds.
		30,    // Cap the slow backoff at 30 seconds.
		5,     // For fast backoffs, use an exponential base of 5.
		200,   // max delay of 200 seconds.
		false, // Do not skip to the fast backoff stage for applicable Work objects.
	)

	testCases := []struct {
		name                    string
		work                    *fleetv1beta1.Work
		bundles                 []*manifestProcessingBundle
		wantRequeueDelaySeconds float64
	}{
		{
			name: "first requeue",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberReservedNSName,
					Name:      workName,
				},
			},
			bundles:                 []*manifestProcessingBundle{},
			wantRequeueDelaySeconds: 5,
		},
		{
			name: "second requeue",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberReservedNSName,
					Name:      workName,
				},
			},
			bundles:                 []*manifestProcessingBundle{},
			wantRequeueDelaySeconds: 10, // Start the slow backoff.
		},
		{
			name: "requeue (#3) w/ gen change",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
			},
			bundles:                 []*manifestProcessingBundle{},
			wantRequeueDelaySeconds: 5, // Use fixed delay again, since the generation has changed.
		},
		{
			name: "requeue #4",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
			},
			bundles: []*manifestProcessingBundle{},
			// This is the second requeue after the generation change; the slow backoff starts again.
			wantRequeueDelaySeconds: 10,
		},
		{
			name: "requeue #5",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
			},
			bundles:                 []*manifestProcessingBundle{},
			wantRequeueDelaySeconds: 20, // The slow backoff continues.
		},
		{
			name: "requeue #6",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
			},
			bundles:                 []*manifestProcessingBundle{},
			wantRequeueDelaySeconds: 100, // Start to fast back off.
		},
		{
			name: "requeue #7 w/ processing result change",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp: ManifestProcessingApplyResultTypeApplied,
				},
			},
			wantRequeueDelaySeconds: 5, // Use fixed delay for the third time, since the processing result has changed.
		},
		{
			name: "requeue #8",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp: ManifestProcessingApplyResultTypeApplied,
				},
			},
			wantRequeueDelaySeconds: 10, // Start to slow back off for the third time.
		},
		{
			name: "requeue #9",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp: ManifestProcessingApplyResultTypeApplied,
				},
			},
			wantRequeueDelaySeconds: 20, // The slow back off continues.
		},
		{
			name: "requeue #10",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp: ManifestProcessingApplyResultTypeApplied,
				},
			},
			wantRequeueDelaySeconds: 100, // Start to fast back off again.
		},
		{
			name: "requeue #11",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp: ManifestProcessingApplyResultTypeApplied,
				},
			},
			wantRequeueDelaySeconds: 200, // Reached the max. cap.
		},
		{
			name: "requeue #12 w/ both gen and processing result change",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 3,
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp: ManifestProcessingApplyResultTypeFailedToApply,
				},
			},
			wantRequeueDelaySeconds: 5, // Use fixed delay for the fourth time, since both generation and processing result have changed.
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requeueDelay := rateLimiter.When(tc.work, tc.bundles)
			requeueDelaySeconds := requeueDelay.Seconds()
			if !cmp.Equal(
				requeueDelaySeconds, tc.wantRequeueDelaySeconds,
				cmpopts.EquateApprox(0.0, 0.001), // Account for float precision limits.
			) {
				t.Errorf("When() = %v, want %v", requeueDelay, tc.wantRequeueDelaySeconds)
			}
		})
	}
}

// TestWhenWithSkipToFastBackoff tests the When method.
func TestWhenWithSkipToFastBackoff(t *testing.T) {
	rateLimiter := NewRequeueMultiStageWithExponentialBackoffRateLimiter(
		1,    // 1 attempts with fixed delay.
		5,    // Use a fixed delay of 5 seconds for the first attempt.
		2,    // For slow backoffs, use an exponential base of 2.
		10,   // Start the slow backoff with a delay of 10 seconds.
		30,   // Cap the slow backoff at 30 seconds.
		5,    // For fast backoffs, use an exponential base of 5.
		200,  // max delay of 200 seconds.
		true, // Skip to the fast backoff stage for applicable Work objects.
	)

	testCases := []struct {
		name                    string
		work                    *fleetv1beta1.Work
		bundles                 []*manifestProcessingBundle
		wantRequeueDelaySeconds float64
	}{
		{
			name: "first requeue",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberReservedNSName,
					Name:      workName,
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
				},
			},
			wantRequeueDelaySeconds: 5,
		},
		{
			name: "requeue #2, work becomes available",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberReservedNSName,
					Name:      workName,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeAvailable,
				},
			},
			wantRequeueDelaySeconds: 5, // Use fixed delay, since the processing result has changed.
		},
		{
			name: "requeue #3, work stays available",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberReservedNSName,
					Name:      workName,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeAvailable,
				},
			},
			wantRequeueDelaySeconds: 10, // Start the slow backoff.
		},
		{
			name: "requeue #4, work stays available",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberReservedNSName,
					Name:      workName,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeAvailable,
				},
			},
			wantRequeueDelaySeconds: 50, // Skip to fast back off.
		},
		{
			name: "requeue #5, work stays available",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberReservedNSName,
					Name:      workName,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeAvailable,
				},
			},
			wantRequeueDelaySeconds: 200, // Reached the max. cap.
		},
		{
			name: "requeue #6, work changed to ReportDiff mode",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					reportDiffResTyp: ManifestProcessingReportDiffResultTypeNoDiffFound,
				},
			},
			wantRequeueDelaySeconds: 5, // Use fixed delay, since the processing result has changed.
		},
		{
			name: "requeue #7, no diff found",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					reportDiffResTyp: ManifestProcessingReportDiffResultTypeNoDiffFound,
				},
			},
			wantRequeueDelaySeconds: 10, // Start the slow backoff.
		},
		{
			name: "requeue #8, no diff found",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					reportDiffResTyp: ManifestProcessingReportDiffResultTypeNoDiffFound,
				},
			},
			wantRequeueDelaySeconds: 50, // Skip to fast back off.
		},
		{
			name: "requeue #9, no diff found",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					reportDiffResTyp: ManifestProcessingReportDiffResultTypeNoDiffFound,
				},
			},
			wantRequeueDelaySeconds: 200, // Reached the max. cap.
		},
		{
			name: "requeue #9, diff found",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					reportDiffResTyp: ManifestProcessingReportDiffResultTypeFoundDiff,
				},
			},
			wantRequeueDelaySeconds: 5, // Use fixed delay, since the processing result has changed.
		},
		{
			name: "requeue #10, diff found",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					reportDiffResTyp: ManifestProcessingReportDiffResultTypeFoundDiff,
				},
			},
			wantRequeueDelaySeconds: 10, // Start the slow backoff.
		},
		{
			name: "requeue #11, diff found",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberReservedNSName,
					Name:       workName,
					Generation: 2,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeDiffReported,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
						},
					},
				},
			},
			bundles: []*manifestProcessingBundle{
				{
					reportDiffResTyp: ManifestProcessingReportDiffResultTypeFoundDiff,
				},
			},
			wantRequeueDelaySeconds: 50, // Skip to fast back off.
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requeueDelay := rateLimiter.When(tc.work, tc.bundles)
			requeueDelaySeconds := requeueDelay.Seconds()
			if !cmp.Equal(
				requeueDelaySeconds, tc.wantRequeueDelaySeconds,
				cmpopts.EquateApprox(0.0, 0.001), // Account for float precision limits.
			) {
				t.Errorf("When() = %v, want %v", requeueDelay, tc.wantRequeueDelaySeconds)
			}
		})
	}
}

// TestForget tests the Forget method.
func TestForget(t *testing.T) {
	workNamespacedName1 := types.NamespacedName{
		Namespace: memberReservedNSName,
		Name:      fmt.Sprintf(workNameTemplate, "1"),
	}
	workNamespacedName2 := types.NamespacedName{
		Namespace: memberReservedNSName,
		Name:      fmt.Sprintf(workNameTemplate, "2"),
	}
	workNamespacedName3 := types.NamespacedName{
		Namespace: memberReservedNSName,
		Name:      fmt.Sprintf(workNameTemplate, "3"),
	}

	bundles := []*manifestProcessingBundle{}

	defaultAttemptsWithFixedDelay := 3
	defaultFixedDelaySeconds := 5.0
	defaultExponentialBaseForSlowBackoff := 2.0
	defaultInitialSlowBackoffDelaySeconds := 10.0
	defaultMaxSlowBackoffDelaySeconds := 60.0
	defaultExponentialBaseForFastBackoff := 4.0
	defaultMaxFastBackoffDelaySeconds := 600.0

	testCases := []struct {
		name            string
		rateLimiter     *RequeueMultiStageWithExponentialBackoffRateLimiter
		work            *fleetv1beta1.Work
		wantRateLimiter *RequeueMultiStageWithExponentialBackoffRateLimiter
	}{
		{
			name: "forget tracked work",
			rateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter: map[types.NamespacedName]int{
					workNamespacedName1: 1,
					workNamespacedName2: 5,
					workNamespacedName3: 9,
				},
				lastRequeueDelayTracker: map[types.NamespacedName]time.Duration{
					workNamespacedName1: 5 * time.Second,
					workNamespacedName2: 20 * time.Second,
					workNamespacedName3: 600 * time.Second,
				},
				lastTrackedGeneration: map[types.NamespacedName]int64{
					workNamespacedName1: 1,
					workNamespacedName2: 2,
					workNamespacedName3: 3,
				},
				lastTrackedProcessingResultHash: map[types.NamespacedName]string{
					workNamespacedName1: "hash-1",
					workNamespacedName2: "hash-2",
					workNamespacedName3: "hash-3",
				},
				attemptsWithFixedDelay:        defaultAttemptsWithFixedDelay,
				fixedDelay:                    time.Duration(defaultFixedDelaySeconds) * time.Second,
				exponentialBaseForSlowBackoff: defaultExponentialBaseForSlowBackoff,
				initialSlowBackoffDelay:       time.Duration(defaultInitialSlowBackoffDelaySeconds) * time.Second,
				maxSlowBackoffDelay:           time.Duration(defaultMaxSlowBackoffDelaySeconds) * time.Second,
				exponentialBaseForFastBackoff: defaultExponentialBaseForFastBackoff,
				maxFastBackoffDelay:           time.Duration(defaultMaxFastBackoffDelaySeconds) * time.Second,
			},
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberReservedNSName,
					Name:      workNamespacedName2.Name,
				},
			},
			wantRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter: map[types.NamespacedName]int{
					workNamespacedName1: 1,
					workNamespacedName3: 9,
				},
				lastRequeueDelayTracker: map[types.NamespacedName]time.Duration{
					workNamespacedName1: 5 * time.Second,
					workNamespacedName3: 600 * time.Second,
				},
				lastTrackedGeneration: map[types.NamespacedName]int64{
					workNamespacedName1: 1,
					workNamespacedName3: 3,
				},
				lastTrackedProcessingResultHash: map[types.NamespacedName]string{
					workNamespacedName1: "hash-1",
					workNamespacedName3: "hash-3",
				},
				attemptsWithFixedDelay:        defaultAttemptsWithFixedDelay,
				fixedDelay:                    time.Duration(defaultFixedDelaySeconds) * time.Second,
				exponentialBaseForSlowBackoff: defaultExponentialBaseForSlowBackoff,
				initialSlowBackoffDelay:       time.Duration(defaultInitialSlowBackoffDelaySeconds) * time.Second,
				maxSlowBackoffDelay:           time.Duration(defaultMaxSlowBackoffDelaySeconds) * time.Second,
				exponentialBaseForFastBackoff: defaultExponentialBaseForFastBackoff,
				maxFastBackoffDelay:           time.Duration(defaultMaxFastBackoffDelaySeconds) * time.Second,
			},
		},
		{
			name: "forget untracked work",
			rateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter: map[types.NamespacedName]int{
					workNamespacedName1: 1,
					workNamespacedName2: 5,
				},
				lastRequeueDelayTracker: map[types.NamespacedName]time.Duration{
					workNamespacedName1: 5 * time.Second,
					workNamespacedName2: 20 * time.Second,
				},
				lastTrackedGeneration: map[types.NamespacedName]int64{
					workNamespacedName1: 1,
					workNamespacedName2: 2,
				},
				lastTrackedProcessingResultHash: map[types.NamespacedName]string{
					workNamespacedName1: "hash-1",
					workNamespacedName2: "hash-2",
				},
				attemptsWithFixedDelay:        defaultAttemptsWithFixedDelay,
				fixedDelay:                    time.Duration(defaultFixedDelaySeconds) * time.Second,
				exponentialBaseForSlowBackoff: defaultExponentialBaseForSlowBackoff,
				initialSlowBackoffDelay:       time.Duration(defaultInitialSlowBackoffDelaySeconds) * time.Second,
				maxSlowBackoffDelay:           time.Duration(defaultMaxSlowBackoffDelaySeconds) * time.Second,
				exponentialBaseForFastBackoff: defaultExponentialBaseForFastBackoff,
				maxFastBackoffDelay:           time.Duration(defaultMaxFastBackoffDelaySeconds) * time.Second,
			},
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberReservedNSName,
					Name:      workNamespacedName3.Name,
				},
			},
			wantRateLimiter: &RequeueMultiStageWithExponentialBackoffRateLimiter{
				requeueCounter: map[types.NamespacedName]int{
					workNamespacedName1: 1,
					workNamespacedName2: 5,
				},
				lastRequeueDelayTracker: map[types.NamespacedName]time.Duration{
					workNamespacedName1: 5 * time.Second,
					workNamespacedName2: 20 * time.Second,
				},
				lastTrackedGeneration: map[types.NamespacedName]int64{
					workNamespacedName1: 1,
					workNamespacedName2: 2,
				},
				lastTrackedProcessingResultHash: map[types.NamespacedName]string{
					workNamespacedName1: "hash-1",
					workNamespacedName2: "hash-2",
				},
				attemptsWithFixedDelay:        defaultAttemptsWithFixedDelay,
				fixedDelay:                    time.Duration(defaultFixedDelaySeconds) * time.Second,
				exponentialBaseForSlowBackoff: defaultExponentialBaseForSlowBackoff,
				initialSlowBackoffDelay:       time.Duration(defaultInitialSlowBackoffDelaySeconds) * time.Second,
				maxSlowBackoffDelay:           time.Duration(defaultMaxSlowBackoffDelaySeconds) * time.Second,
				exponentialBaseForFastBackoff: defaultExponentialBaseForFastBackoff,
				maxFastBackoffDelay:           time.Duration(defaultMaxFastBackoffDelaySeconds) * time.Second,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.rateLimiter.Forget(tc.work)

			if diff := cmp.Diff(
				tc.rateLimiter, tc.wantRateLimiter,
				cmpopts.IgnoreFields(RequeueMultiStageWithExponentialBackoffRateLimiter{}, "mu"),
				cmp.AllowUnexported(RequeueMultiStageWithExponentialBackoffRateLimiter{})); diff != "" {
				t.Errorf("Forget() mismatch (-got +want):\n%s", diff)
			}

			// Ensure that after forgetting the work, the rate limiter will return
			// an expected delay when the work is requeued again.
			requeueDelay := tc.rateLimiter.When(tc.work, bundles)
			requeueDelaySeconds := requeueDelay.Seconds()
			wantRequeueDelaySeconds := defaultFixedDelaySeconds
			// Account for float precision limits and approximation.
			if !cmp.Equal(requeueDelaySeconds, wantRequeueDelaySeconds, cmpopts.EquateApprox(0.0, 0.001)) {
				t.Errorf("When() after Forget() = %f, want %f", requeueDelaySeconds, wantRequeueDelaySeconds)
			}
		})
	}
}

// TestComputeProcessingResultHash tests the computeProcessingResultHash function.
func TestComputeProcessingResultHash(t *testing.T) {
	work := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberReservedNSName,
			Name:      workName,
		},
	}

	testCases := []struct {
		name     string
		bundles  []*manifestProcessingBundle
		wantHash string
	}{
		{
			// This is a case that normally should not occur.
			name:     "no manifest",
			bundles:  []*manifestProcessingBundle{},
			wantHash: "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945",
		},
		{
			// This is a case that normally should not occur.
			name: "single manifest, no result of any type",
			bundles: []*manifestProcessingBundle{
				{},
			},
			wantHash: "ec6e5a3a69851e2b956b6f682bad1d2355faa874e635b4d2f3e33ce84a8f788a",
		},
		{
			name: "single manifest, apply op failure (pre-processing)",
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp: ManifestProcessingApplyResultTypeDecodingErred,
				},
			},
			wantHash: "a4cce45a59ced1c0b218b7e2b07920e6515a0bd4e80141f114cf29a1e2062790",
		},
		{
			name: "single manifest, apply op failure (processing, no error message)",
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp: ManifestProcessingApplyResultTypeFailedToApply,
				},
			},
			wantHash: "f4610fbac163e867a62672a3e95547e8321fa09709ecac73308dfff8fde49511",
		},
		{
			name: "single manifest, apply op failure (processing, with error message)",
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp: ManifestProcessingApplyResultTypeFailedToApply,
					applyErr:    fmt.Errorf("failed to apply manifest"),
				},
			},
			// Note that this expected hash value is the same as the previous one.
			wantHash: "f4610fbac163e867a62672a3e95547e8321fa09709ecac73308dfff8fde49511",
		},
		{
			name: "single manifest, availability check failure",
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeNotYetAvailable,
				},
			},
			wantHash: "9110cc26c9559ba84e909593a089fd495eb6e86479c9430d5673229ebe2d1275",
		},
		{
			name: "single manifest, apply op + availability check success",
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeAvailable,
				},
			},
			wantHash: "d922098ce1f87b79fc26fad06355ea4eba77cc5a86e742e9159c58cce5bd4a31",
		},
		{
			name: "single manifest, diff reporting failure",
			bundles: []*manifestProcessingBundle{
				{
					reportDiffResTyp: ManifestProcessingReportDiffResultTypeFailed,
				},
			},
			wantHash: "dd541a034eb568cf92da960b884dece6d136460399ab68958ce8fc6730c91d45",
		},
		{
			name: "single manifest, diff reporting success",
			bundles: []*manifestProcessingBundle{
				{
					reportDiffResTyp: ManifestProcessingReportDiffResultTypeNoDiffFound,
				},
			},
			wantHash: "f9b66724190d196e1cf19247a0447a6ed0d71697dcb8016c0bc3b3726a757e1a",
		},
		{
			name: "multiple manifests (assorted)",
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp: ManifestProcessingApplyResultTypeFailedToApply,
					applyErr:    fmt.Errorf("failed to apply manifest"),
				},
				{
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeAvailable,
				},
				{
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeNotTrackable,
				},
			},
			wantHash: "09c6195d94bfc84cdbb365bb615d3461a457a355b9f74049488a1db38e979018",
		},
		{
			name: "multiple manifests (assorted, different order)",
			bundles: []*manifestProcessingBundle{
				{
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeAvailable,
				},
				{
					applyResTyp: ManifestProcessingApplyResultTypeFailedToApply,
					applyErr:    fmt.Errorf("failed to apply manifest"),
				},
				{
					applyResTyp:        ManifestProcessingApplyResultTypeApplied,
					availabilityResTyp: ManifestProcessingAvailabilityResultTypeNotTrackable,
				},
			},
			// Note that different orders of the manifests result in different hashes.
			wantHash: "ef1a6e8d207f5b86a8c7f39417eede40abc6e4f1d5ef9feceb5797f14a834f58",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := computeProcessingResultHash(work, tc.bundles)
			if err != nil {
				t.Fatalf("computeProcessingResultHash() = %v, want no error", err)
			}
			if hash != tc.wantHash {
				t.Errorf("computeProcessingResultHash() = %v, want %v", hash, tc.wantHash)
			}
		})
	}
}
