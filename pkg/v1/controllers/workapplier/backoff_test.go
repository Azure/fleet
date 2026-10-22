/*
Copyright 2026 The KubeFleet Authors.

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
	"math"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

const (
	backoffTestAltNSName = "fleet-member-experimental-2"

	// The SHA-256 checksum of `[]`, i.e., the JSON representation of an empty string slice.
	emptyJSONListSHA256 = "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945"

	// Account for the precision loss when multiplying time.Duration values with float exponential bases.
	delayToleranceSeconds = 0.01
)

var (
	backoffTestPrimaryWorkName   = fmt.Sprintf(workNameTemplate, "1")
	backoffTestSecondaryWorkName = fmt.Sprintf(workNameTemplate, "1-1")
	backoffTestOtherWorkName     = fmt.Sprintf(workNameTemplate, "2")

	// The configuration of the default requeue rate limiter.
	defaultRateLimiterCfg = rateLimiterConfig{
		attemptsWithFixedDelay:                               1,
		fixedDelaySeconds:                                    5,
		exponentialBaseForSlowBackoff:                        1.2,
		initialSlowBackoffDelaySeconds:                       2,
		maxSlowBackoffDelaySeconds:                           15,
		exponentialBaseForFastBackoff:                        1.5,
		maxFastBackoffDelaySeconds:                           900,
		skipToFastBackoffWhenAllAppliedManifestsAreAvailable: true,
	}

	// A configuration that yields easy-to-follow requeue delays: 5 (fixed), 10 (initial slow backoff),
	// 20 (slow backoff), 100 (fast backoff, as 40 breaks the slow backoff cap), 200 (capped).
	simpleRateLimiterCfg = rateLimiterConfig{
		attemptsWithFixedDelay:         1,
		fixedDelaySeconds:              5,
		exponentialBaseForSlowBackoff:  2,
		initialSlowBackoffDelaySeconds: 10,
		maxSlowBackoffDelaySeconds:     30,
		exponentialBaseForFastBackoff:  5,
		maxFastBackoffDelaySeconds:     200,
	}

	rateLimiterCmpOpts = []cmp.Option{
		cmp.AllowUnexported(MultiStageRequeueWithExponentialBackoffRateLimiter{}),
		cmpopts.IgnoreFields(MultiStageRequeueWithExponentialBackoffRateLimiter{}, "mu"),
	}
)

// rateLimiterConfig is the set of parameters for creating a MultiStageRequeueWithExponentialBackoffRateLimiter.
type rateLimiterConfig struct {
	attemptsWithFixedDelay                               int
	fixedDelaySeconds                                    int
	exponentialBaseForSlowBackoff                        float64
	initialSlowBackoffDelaySeconds                       int
	maxSlowBackoffDelaySeconds                           int
	exponentialBaseForFastBackoff                        float64
	maxFastBackoffDelaySeconds                           int
	skipToFastBackoffWhenAllAppliedManifestsAreAvailable bool
}

// requeueStep is a single call to the When method, along with the requeue delay it should return.
type requeueStep struct {
	linkedWorks      []*placementv1alpha1.Work
	states           []*manifestProcessingState
	wantDelaySeconds float64
}

// trackedRequeueState is the per-work object state that the rate limiter tracks.
type trackedRequeueState struct {
	requeues             int
	lastDelay            time.Duration
	linkedWorksGenHash   string
	processingResultHash string
}

func newRateLimiterForTest(t *testing.T, cfg rateLimiterConfig) *MultiStageRequeueWithExponentialBackoffRateLimiter {
	t.Helper()
	rateLimiter, err := NewMultiStageRequeueWithExponentialBackoffRateLimiter(
		cfg.attemptsWithFixedDelay,
		cfg.fixedDelaySeconds,
		cfg.exponentialBaseForSlowBackoff,
		cfg.initialSlowBackoffDelaySeconds,
		cfg.maxSlowBackoffDelaySeconds,
		cfg.exponentialBaseForFastBackoff,
		cfg.maxFastBackoffDelaySeconds,
		cfg.skipToFastBackoffWhenAllAppliedManifestsAreAvailable,
	)
	if err != nil {
		t.Fatalf("NewMultiStageRequeueWithExponentialBackoffRateLimiter(%+v) = %v, want no error", cfg, err)
	}
	return rateLimiter
}

func withTrackedRequeueStates(
	rateLimiter *MultiStageRequeueWithExponentialBackoffRateLimiter,
	trackedStates map[types.NamespacedName]trackedRequeueState,
	forgets int,
) *MultiStageRequeueWithExponentialBackoffRateLimiter {
	for key, state := range trackedStates {
		rateLimiter.requeueCounter[key] = state.requeues
		rateLimiter.lastRequeueDelayTracker[key] = state.lastDelay
		rateLimiter.lastTrackedLinkedWorksGenHash[key] = state.linkedWorksGenHash
		rateLimiter.lastTrackedProcessingResultHash[key] = state.processingResultHash
	}
	rateLimiter.forgets = forgets
	return rateLimiter
}

func workForBackoffTest(namespace, name string, generation int64) *placementv1alpha1.Work {
	return &placementv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  namespace,
			Name:       name,
			Generation: generation,
		},
	}
}

// workWithLabelsAndStatusForBackoffTest returns a work object with some labels and status conditions set,
// which (unlike the name and the generation) should not affect the requeue delays.
func workWithLabelsAndStatusForBackoffTest(namespace, name string, generation int64) *placementv1alpha1.Work {
	work := workForBackoffTest(namespace, name, generation)
	work.Labels = map[string]string{"foo": "bar"}
	work.Status = placementv1alpha1.WorkStatus{
		Conditions: []metav1.Condition{
			{
				Type:   placementv1alpha1.WorkCondTypeApplied,
				Status: metav1.ConditionTrue,
			},
			{
				Type:   placementv1alpha1.WorkCondTypeAvailable,
				Status: metav1.ConditionTrue,
			},
		},
	}
	return work
}

func stateWith(applyRes ApplyResultType, availabilityCheckRes AvailabilityCheckResultType) *manifestProcessingState {
	return &manifestProcessingState{
		applyRes:             applyRes,
		availabilityCheckRes: availabilityCheckRes,
	}
}

// requeueSteps builds a sequence of requeue steps that share the same inputs.
func requeueSteps(
	linkedWorks []*placementv1alpha1.Work,
	states []*manifestProcessingState,
	wantDelaysSeconds ...float64,
) []requeueStep {
	steps := make([]requeueStep, 0, len(wantDelaysSeconds))
	for _, wantDelaySeconds := range wantDelaysSeconds {
		steps = append(steps, requeueStep{
			linkedWorks:      linkedWorks,
			states:           states,
			wantDelaySeconds: wantDelaySeconds,
		})
	}
	return steps
}

// verifyRequeueSteps calls the When method for each step in order and verifies the returned delays.
//
// As the rate limiter is stateful, the verification stops at the first mismatch.
func verifyRequeueSteps(t *testing.T, rateLimiter *MultiStageRequeueWithExponentialBackoffRateLimiter, steps []requeueStep) {
	t.Helper()
	for idx, step := range steps {
		gotDelay := rateLimiter.When(step.linkedWorks, step.states)
		if !cmp.Equal(gotDelay.Seconds(), step.wantDelaySeconds, cmpopts.EquateApprox(0, delayToleranceSeconds)) {
			t.Fatalf("When() (step #%d) = %v, want %vs", idx+1, gotDelay, step.wantDelaySeconds)
		}
	}
}

// TestNewMultiStageRequeueWithExponentialBackoffRateLimiter tests the
// NewMultiStageRequeueWithExponentialBackoffRateLimiter function.
func TestNewMultiStageRequeueWithExponentialBackoffRateLimiter(t *testing.T) {
	typicalCfg := rateLimiterConfig{
		attemptsWithFixedDelay:                               10,
		fixedDelaySeconds:                                    5,
		exponentialBaseForSlowBackoff:                        1.2,
		initialSlowBackoffDelaySeconds:                       2,
		maxSlowBackoffDelaySeconds:                           15,
		exponentialBaseForFastBackoff:                        1.5,
		maxFastBackoffDelaySeconds:                           60,
		skipToFastBackoffWhenAllAppliedManifestsAreAvailable: true,
	}
	// mutated returns a copy of the typical configuration with one or more parameters changed.
	mutated := func(mutateFn func(cfg *rateLimiterConfig)) rateLimiterConfig {
		cfg := typicalCfg
		mutateFn(&cfg)
		return cfg
	}

	testCases := []struct {
		name            string
		cfg             rateLimiterConfig
		wantRateLimiter *MultiStageRequeueWithExponentialBackoffRateLimiter
		wantErred       bool
	}{
		{
			name: "typical parameters",
			cfg:  typicalCfg,
			wantRateLimiter: &MultiStageRequeueWithExponentialBackoffRateLimiter{
				requeueCounter:                                       map[types.NamespacedName]int{},
				lastRequeueDelayTracker:                              map[types.NamespacedName]time.Duration{},
				lastTrackedLinkedWorksGenHash:                        map[types.NamespacedName]string{},
				lastTrackedProcessingResultHash:                      map[types.NamespacedName]string{},
				attemptsWithFixedDelay:                               10,
				fixedDelay:                                           5 * time.Second,
				exponentialBaseForSlowBackoff:                        1.2,
				initialSlowBackoffDelay:                              2 * time.Second,
				maxSlowBackoffDelay:                                  15 * time.Second,
				exponentialBaseForFastBackoff:                        1.5,
				maxFastBackoffDelay:                                  60 * time.Second,
				skipToFastBackoffWhenAllAppliedManifestsAreAvailable: true,
			},
		},
		{
			name: "all parameters at their lower bounds",
			cfg: rateLimiterConfig{
				attemptsWithFixedDelay:         1,
				fixedDelaySeconds:              2,
				exponentialBaseForSlowBackoff:  1.05,
				initialSlowBackoffDelaySeconds: 2,
				maxSlowBackoffDelaySeconds:     2,
				exponentialBaseForFastBackoff:  1.05,
				maxFastBackoffDelaySeconds:     2,
			},
			wantRateLimiter: &MultiStageRequeueWithExponentialBackoffRateLimiter{
				requeueCounter:                  map[types.NamespacedName]int{},
				lastRequeueDelayTracker:         map[types.NamespacedName]time.Duration{},
				lastTrackedLinkedWorksGenHash:   map[types.NamespacedName]string{},
				lastTrackedProcessingResultHash: map[types.NamespacedName]string{},
				attemptsWithFixedDelay:          1,
				fixedDelay:                      2 * time.Second,
				exponentialBaseForSlowBackoff:   1.05,
				initialSlowBackoffDelay:         2 * time.Second,
				maxSlowBackoffDelay:             2 * time.Second,
				exponentialBaseForFastBackoff:   1.05,
				maxFastBackoffDelay:             2 * time.Second,
			},
		},
		{
			name: "all parameters at their upper bounds",
			cfg: rateLimiterConfig{
				attemptsWithFixedDelay:         40,
				fixedDelaySeconds:              3600,
				exponentialBaseForSlowBackoff:  100,
				initialSlowBackoffDelaySeconds: 3600,
				maxSlowBackoffDelaySeconds:     3600,
				exponentialBaseForFastBackoff:  100,
				maxFastBackoffDelaySeconds:     3600,
			},
			wantRateLimiter: &MultiStageRequeueWithExponentialBackoffRateLimiter{
				requeueCounter:                  map[types.NamespacedName]int{},
				lastRequeueDelayTracker:         map[types.NamespacedName]time.Duration{},
				lastTrackedLinkedWorksGenHash:   map[types.NamespacedName]string{},
				lastTrackedProcessingResultHash: map[types.NamespacedName]string{},
				attemptsWithFixedDelay:          40,
				fixedDelay:                      3600 * time.Second,
				exponentialBaseForSlowBackoff:   100,
				initialSlowBackoffDelay:         3600 * time.Second,
				maxSlowBackoffDelay:             3600 * time.Second,
				exponentialBaseForFastBackoff:   100,
				maxFastBackoffDelay:             3600 * time.Second,
			},
		},
		{
			name: "fixed delay equal to max fast backoff delay",
			cfg:  mutated(func(cfg *rateLimiterConfig) { cfg.fixedDelaySeconds = 60 }),
			wantRateLimiter: &MultiStageRequeueWithExponentialBackoffRateLimiter{
				requeueCounter:                                       map[types.NamespacedName]int{},
				lastRequeueDelayTracker:                              map[types.NamespacedName]time.Duration{},
				lastTrackedLinkedWorksGenHash:                        map[types.NamespacedName]string{},
				lastTrackedProcessingResultHash:                      map[types.NamespacedName]string{},
				attemptsWithFixedDelay:                               10,
				fixedDelay:                                           60 * time.Second,
				exponentialBaseForSlowBackoff:                        1.2,
				initialSlowBackoffDelay:                              2 * time.Second,
				maxSlowBackoffDelay:                                  15 * time.Second,
				exponentialBaseForFastBackoff:                        1.5,
				maxFastBackoffDelay:                                  60 * time.Second,
				skipToFastBackoffWhenAllAppliedManifestsAreAvailable: true,
			},
		},
		{
			name:      "attempts with fixed delay (zero)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.attemptsWithFixedDelay = 0 }),
			wantErred: true,
		},
		{
			name:      "attempts with fixed delay (negative)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.attemptsWithFixedDelay = -1 }),
			wantErred: true,
		},
		{
			name:      "attempts with fixed delay (too many)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.attemptsWithFixedDelay = 41 }),
			wantErred: true,
		},
		{
			name:      "fixed delay (too small)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.fixedDelaySeconds = 1 }),
			wantErred: true,
		},
		{
			name:      "fixed delay (zero)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.fixedDelaySeconds = 0 }),
			wantErred: true,
		},
		{
			name:      "fixed delay (above max fast backoff delay)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.fixedDelaySeconds = 61 }),
			wantErred: true,
		},
		{
			name:      "exponential base for slow backoff (too small)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.exponentialBaseForSlowBackoff = 1.04 }),
			wantErred: true,
		},
		{
			name:      "exponential base for slow backoff (NaN)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.exponentialBaseForSlowBackoff = math.NaN() }),
			wantErred: true,
		},
		{
			name:      "exponential base for slow backoff (above the base for fast backoff)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.exponentialBaseForSlowBackoff = 2.0 }),
			wantErred: true,
		},
		{
			name:      "exponential base for fast backoff (too large)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.exponentialBaseForFastBackoff = 100.01 }),
			wantErred: true,
		},
		{
			name:      "exponential base for fast backoff (NaN)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.exponentialBaseForFastBackoff = math.NaN() }),
			wantErred: true,
		},
		{
			name:      "initial slow backoff delay (too small)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.initialSlowBackoffDelaySeconds = 1 }),
			wantErred: true,
		},
		{
			name:      "initial slow backoff delay (above max slow backoff delay)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.initialSlowBackoffDelaySeconds = 16 }),
			wantErred: true,
		},
		{
			name:      "max slow backoff delay (above max fast backoff delay)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.maxSlowBackoffDelaySeconds = 61 }),
			wantErred: true,
		},
		{
			name:      "max fast backoff delay (too large)",
			cfg:       mutated(func(cfg *rateLimiterConfig) { cfg.maxFastBackoffDelaySeconds = 3601 }),
			wantErred: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rateLimiter, err := NewMultiStageRequeueWithExponentialBackoffRateLimiter(
				tc.cfg.attemptsWithFixedDelay,
				tc.cfg.fixedDelaySeconds,
				tc.cfg.exponentialBaseForSlowBackoff,
				tc.cfg.initialSlowBackoffDelaySeconds,
				tc.cfg.maxSlowBackoffDelaySeconds,
				tc.cfg.exponentialBaseForFastBackoff,
				tc.cfg.maxFastBackoffDelaySeconds,
				tc.cfg.skipToFastBackoffWhenAllAppliedManifestsAreAvailable,
			)
			if tc.wantErred {
				if err == nil || kferrors.Category(err) != kferrors.ErrCategoryUser {
					t.Fatalf("NewMultiStageRequeueWithExponentialBackoffRateLimiter(%+v) = %v, want a user error", tc.cfg, err)
				}
				if rateLimiter != nil {
					t.Errorf("NewMultiStageRequeueWithExponentialBackoffRateLimiter(%+v) = %+v, want nil", tc.cfg, rateLimiter)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewMultiStageRequeueWithExponentialBackoffRateLimiter(%+v) = %v, want no error", tc.cfg, err)
			}

			// Note that the comparison does not equate nil and empty maps; the trackers must be initialized.
			if diff := cmp.Diff(rateLimiter, tc.wantRateLimiter, rateLimiterCmpOpts...); diff != "" {
				t.Errorf("NewMultiStageRequeueWithExponentialBackoffRateLimiter(%+v) mismatches (-got, +want):\n%s", tc.cfg, diff)
			}
		})
	}
}

// TestDefaultRequeueRateLimiter tests the configuration of the default requeue rate limiter.
func TestDefaultRequeueRateLimiter(t *testing.T) {
	if defaultRequeueRateLimiter == nil {
		t.Fatalf("defaultRequeueRateLimiter = nil, want a rate limiter")
	}

	wantRateLimiter := &MultiStageRequeueWithExponentialBackoffRateLimiter{
		attemptsWithFixedDelay:        defaultRateLimiterCfg.attemptsWithFixedDelay,
		fixedDelay:                    time.Duration(defaultRateLimiterCfg.fixedDelaySeconds) * time.Second,
		exponentialBaseForSlowBackoff: defaultRateLimiterCfg.exponentialBaseForSlowBackoff,
		initialSlowBackoffDelay:       time.Duration(defaultRateLimiterCfg.initialSlowBackoffDelaySeconds) * time.Second,
		maxSlowBackoffDelay:           time.Duration(defaultRateLimiterCfg.maxSlowBackoffDelaySeconds) * time.Second,
		exponentialBaseForFastBackoff: defaultRateLimiterCfg.exponentialBaseForFastBackoff,
		maxFastBackoffDelay:           time.Duration(defaultRateLimiterCfg.maxFastBackoffDelaySeconds) * time.Second,
		skipToFastBackoffWhenAllAppliedManifestsAreAvailable: defaultRateLimiterCfg.skipToFastBackoffWhenAllAppliedManifestsAreAvailable,
	}
	// The default rate limiter is shared with the integration tests in the same package; compare
	// only the configuration, which is never written after initialization.
	if diff := cmp.Diff(
		defaultRequeueRateLimiter, wantRateLimiter,
		cmp.AllowUnexported(MultiStageRequeueWithExponentialBackoffRateLimiter{}),
		cmpopts.IgnoreFields(MultiStageRequeueWithExponentialBackoffRateLimiter{},
			"mu", "requeueCounter", "lastRequeueDelayTracker",
			"lastTrackedLinkedWorksGenHash", "lastTrackedProcessingResultHash", "forgets"),
	); diff != "" {
		t.Errorf("defaultRequeueRateLimiter mismatches (-got, +want):\n%s", diff)
	}
}

// TestNewDefaultRequeueRateLimiterWithMaxDelay tests that the capped rate limiter keeps the default parameters
// and lowers only those that exceed the cap.
func TestNewDefaultRequeueRateLimiterWithMaxDelay(t *testing.T) {
	// wantCapped returns the wanted rate limiter for the given (already capped) delays in seconds.
	wantCapped := func(fixedDelaySeconds, initialSlowBackoffDelaySeconds, maxSlowBackoffDelaySeconds, maxFastBackoffDelaySeconds int) *MultiStageRequeueWithExponentialBackoffRateLimiter {
		return &MultiStageRequeueWithExponentialBackoffRateLimiter{
			requeueCounter:                                       map[types.NamespacedName]int{},
			lastRequeueDelayTracker:                              map[types.NamespacedName]time.Duration{},
			lastTrackedLinkedWorksGenHash:                        map[types.NamespacedName]string{},
			lastTrackedProcessingResultHash:                      map[types.NamespacedName]string{},
			attemptsWithFixedDelay:                               defaultRateLimiterCfg.attemptsWithFixedDelay,
			fixedDelay:                                           time.Duration(fixedDelaySeconds) * time.Second,
			exponentialBaseForSlowBackoff:                        defaultRateLimiterCfg.exponentialBaseForSlowBackoff,
			initialSlowBackoffDelay:                              time.Duration(initialSlowBackoffDelaySeconds) * time.Second,
			maxSlowBackoffDelay:                                  time.Duration(maxSlowBackoffDelaySeconds) * time.Second,
			exponentialBaseForFastBackoff:                        defaultRateLimiterCfg.exponentialBaseForFastBackoff,
			maxFastBackoffDelay:                                  time.Duration(maxFastBackoffDelaySeconds) * time.Second,
			skipToFastBackoffWhenAllAppliedManifestsAreAvailable: defaultRateLimiterCfg.skipToFastBackoffWhenAllAppliedManifestsAreAvailable,
		}
	}

	testCases := []struct {
		name            string
		maxDelaySeconds int
		wantRateLimiter *MultiStageRequeueWithExponentialBackoffRateLimiter
		wantErred       bool
	}{
		{
			name:            "cap below the fixed delay",
			maxDelaySeconds: 3,
			wantRateLimiter: wantCapped(3, 2, 3, 3),
		},
		{
			name:            "cap at the fixed delay",
			maxDelaySeconds: 5,
			wantRateLimiter: wantCapped(5, 2, 5, 5),
		},
		{
			name:            "cap between the slow and fast backoff caps",
			maxDelaySeconds: 60,
			wantRateLimiter: wantCapped(5, 2, 15, 60),
		},
		{
			name:            "cap above the defaults",
			maxDelaySeconds: 3600,
			wantRateLimiter: wantCapped(
				defaultRateLimiterCfg.fixedDelaySeconds,
				defaultRateLimiterCfg.initialSlowBackoffDelaySeconds,
				defaultRateLimiterCfg.maxSlowBackoffDelaySeconds,
				defaultRateLimiterCfg.maxFastBackoffDelaySeconds,
			),
		},
		{
			name:            "cap below the minimum fixed delay",
			maxDelaySeconds: 1,
			wantErred:       true,
		},
		{
			name:            "negative cap",
			maxDelaySeconds: -5,
			wantErred:       true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rateLimiter, err := NewDefaultRequeueRateLimiterWithMaxDelay(tc.maxDelaySeconds)
			if tc.wantErred {
				if err == nil || kferrors.Category(err) != kferrors.ErrCategoryUser {
					t.Fatalf("NewDefaultRequeueRateLimiterWithMaxDelay(%d) = %v, want a user error", tc.maxDelaySeconds, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewDefaultRequeueRateLimiterWithMaxDelay(%d) = %v, want no error", tc.maxDelaySeconds, err)
			}
			if diff := cmp.Diff(rateLimiter, tc.wantRateLimiter, rateLimiterCmpOpts...); diff != "" {
				t.Errorf("NewDefaultRequeueRateLimiterWithMaxDelay(%d) mismatches (-got, +want):\n%s", tc.maxDelaySeconds, diff)
			}
		})
	}
}

// TestWhenWithFullSequences tests the When method with full requeue sequences.
func TestWhenWithFullSequences(t *testing.T) {
	linkedWorks := []*placementv1alpha1.Work{
		workForBackoffTest(memberReservedNSName1, backoffTestPrimaryWorkName, 1),
	}
	states := []*manifestProcessingState{
		stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotYetAvailable),
	}

	testCases := []struct {
		name              string
		cfg               rateLimiterConfig
		wantDelaysSeconds []float64
	}{
		{
			// This sequence matches the requeue pattern documented with the default rate limiter.
			name: "default configuration",
			cfg:  defaultRateLimiterCfg,
			wantDelaysSeconds: []float64{
				// 1 attempt with fixed delay.
				5,
				// 12 attempts with slow backoff (base 1.2).
				2, 2.4, 2.88, 3.456, 4.1472, 4.9766, 5.9720, 7.1664, 8.5996, 10.3196, 12.3835, 14.8602,
				// 10 attempts with fast backoff (base 1.5), as 14.8602 * 1.2 breaks the slow backoff cap (15).
				22.2903, 33.4354, 50.1531, 75.2296, 112.8444, 169.2666, 253.8999, 380.8498, 571.2748, 856.9121,
				// Capped.
				900, 900,
			},
		},
		{
			name: "multiple attempts with fixed delay, full slow and fast backoff stages",
			cfg: rateLimiterConfig{
				attemptsWithFixedDelay:         2,
				fixedDelaySeconds:              5,
				exponentialBaseForSlowBackoff:  1.2,
				initialSlowBackoffDelaySeconds: 2,
				maxSlowBackoffDelaySeconds:     15,
				exponentialBaseForFastBackoff:  1.5,
				maxFastBackoffDelaySeconds:     60,
			},
			wantDelaysSeconds: []float64{
				5, 5,
				2, 2.4, 2.88, 3.456, 4.1472, 4.9766, 5.9720, 7.1664, 8.5996, 10.3196, 12.3835, 14.8602,
				22.2903, 33.4354, 50.1531,
				// 50.1531 * 1.5 = 75.2296, capped at 60.
				60, 60,
			},
		},
		{
			name: "no slow backoff stage",
			cfg: rateLimiterConfig{
				attemptsWithFixedDelay:         1,
				fixedDelaySeconds:              5,
				exponentialBaseForSlowBackoff:  2,
				initialSlowBackoffDelaySeconds: 2,
				maxSlowBackoffDelaySeconds:     2,
				exponentialBaseForFastBackoff:  5,
				maxFastBackoffDelaySeconds:     20,
			},
			// The initial slow backoff delay reaches the slow backoff cap; fast backoff starts immediately
			// (2 * 5 = 10), then 10 * 5 = 50 is capped at 20.
			wantDelaysSeconds: []float64{5, 2, 10, 20, 20},
		},
		{
			name: "no fast backoff stage",
			cfg: rateLimiterConfig{
				attemptsWithFixedDelay:         1,
				fixedDelaySeconds:              5,
				exponentialBaseForSlowBackoff:  2,
				initialSlowBackoffDelaySeconds: 2,
				maxSlowBackoffDelaySeconds:     10,
				exponentialBaseForFastBackoff:  5,
				maxFastBackoffDelaySeconds:     10,
			},
			// 8 * 2 = 16 breaks the slow backoff cap; 8 * 5 = 40 is capped at 10.
			wantDelaysSeconds: []float64{5, 2, 4, 8, 10, 10},
		},
		{
			name: "no backoff at all",
			cfg: rateLimiterConfig{
				attemptsWithFixedDelay:         1,
				fixedDelaySeconds:              5,
				exponentialBaseForSlowBackoff:  2,
				initialSlowBackoffDelaySeconds: 10,
				maxSlowBackoffDelaySeconds:     10,
				exponentialBaseForFastBackoff:  5,
				maxFastBackoffDelaySeconds:     10,
			},
			wantDelaysSeconds: []float64{5, 10, 10, 10},
		},
		{
			name: "slow backoff lands exactly on the slow backoff cap",
			cfg: rateLimiterConfig{
				attemptsWithFixedDelay:         1,
				fixedDelaySeconds:              5,
				exponentialBaseForSlowBackoff:  2,
				initialSlowBackoffDelaySeconds: 2,
				maxSlowBackoffDelaySeconds:     8,
				exponentialBaseForFastBackoff:  4,
				maxFastBackoffDelaySeconds:     100,
			},
			// 4 * 2 = 8 does not break the slow backoff cap; afterwards, fast backoff starts (8 * 4 = 32),
			// then 32 * 4 = 128 is capped at 100.
			wantDelaysSeconds: []float64{5, 2, 4, 8, 32, 100, 100},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rateLimiter := newRateLimiterForTest(t, tc.cfg)
			verifyRequeueSteps(t, rateLimiter, requeueSteps(linkedWorks, states, tc.wantDelaysSeconds...))
		})
	}
}

// TestWhenWithLinkedWorksAndProcessingResultChanges tests the When method when the linked work objects
// and/or the processing results change.
func TestWhenWithLinkedWorksAndProcessingResultChanges(t *testing.T) {
	primaryWorkGen1 := workForBackoffTest(memberReservedNSName1, backoffTestPrimaryWorkName, 1)
	primaryWorkGen2 := workForBackoffTest(memberReservedNSName1, backoffTestPrimaryWorkName, 2)
	secondaryWorkGen1 := workForBackoffTest(memberReservedNSName1, backoffTestSecondaryWorkName, 1)
	secondaryWorkGen2 := workForBackoffTest(memberReservedNSName1, backoffTestSecondaryWorkName, 2)
	primaryWorkGen1WithStatus := workWithLabelsAndStatusForBackoffTest(memberReservedNSName1, backoffTestPrimaryWorkName, 1)

	notYetAvailableStates := []*manifestProcessingState{
		stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotYetAvailable),
	}
	availableStates := []*manifestProcessingState{
		stateWith(ApplyResTypeApplied, AvailabilityResultTypeAvailable),
	}
	notTrackableStates := []*manifestProcessingState{
		stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotTrackable),
	}
	failedStates := []*manifestProcessingState{
		stateWith(ApplyResTypeFailedToApply, AvailabilityResultTypeSkipped),
		stateWith(ApplyResTypeApplied, AvailabilityResultTypeFailed),
		stateWith(ApplyResTypeFoundDrifts, AvailabilityResultTypeSkipped),
	}
	// The same processing results as failedStates, with different errors and diffs.
	failedStatesWithDetails := []*manifestProcessingState{
		{
			applyRes:             ApplyResTypeFailedToApply,
			availabilityCheckRes: AvailabilityResultTypeSkipped,
			applyErr:             fmt.Errorf("failed to apply the manifest"),
		},
		{
			applyRes:             ApplyResTypeApplied,
			availabilityCheckRes: AvailabilityResultTypeFailed,
			availabilityCheckErr: fmt.Errorf("failed to check the availability"),
		},
		{
			applyRes:             ApplyResTypeFoundDrifts,
			availabilityCheckRes: AvailabilityResultTypeSkipped,
			diffs: []placementv1alpha1.PatchDetail{
				{
					Path:          "/spec/replicas",
					ValueInMember: "1",
					ValueInHub:    "2",
				},
			},
		},
	}

	testCases := []struct {
		name  string
		steps []requeueStep
	}{
		{
			name: "primary work object generation change",
			steps: slices.Concat(
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, notYetAvailableStates, 5, 10, 20),
				// Reset.
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen2}, notYetAvailableStates, 5, 10),
			),
		},
		{
			name: "secondary work object generation change",
			steps: slices.Concat(
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1, secondaryWorkGen1}, notYetAvailableStates, 5, 10, 20, 100),
				// Reset.
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1, secondaryWorkGen2}, notYetAvailableStates, 5, 10),
			),
		},
		{
			name: "linked work object added",
			steps: slices.Concat(
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, notYetAvailableStates, 5, 10),
				// Reset.
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1, secondaryWorkGen1}, notYetAvailableStates, 5, 10),
			),
		},
		{
			name: "linked work object removed",
			steps: slices.Concat(
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1, secondaryWorkGen1}, notYetAvailableStates, 5, 10),
				// Reset.
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, notYetAvailableStates, 5, 10),
			),
		},
		{
			name: "processing result change",
			steps: slices.Concat(
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, notYetAvailableStates, 5, 10, 20, 100, 200),
				// Reset.
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, availableStates, 5, 10),
			),
		},
		{
			name: "both generation and processing result change",
			steps: slices.Concat(
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, notYetAvailableStates, 5, 10, 20),
				// Reset.
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen2}, availableStates, 5, 10),
			),
		},
		{
			name: "availability check result change between Available and NotTrackable",
			steps: slices.Concat(
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, availableStates, 5, 10, 20),
				// Reset, even though both results count as available.
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, notTrackableStates, 5),
			),
		},
		{
			name: "error and diff changes only (no reset)",
			steps: slices.Concat(
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, failedStates, 5, 10, 20),
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, failedStatesWithDetails, 100),
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, failedStates, 200),
			),
		},
		{
			name: "work object label and status changes only (no reset)",
			steps: slices.Concat(
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1}, notYetAvailableStates, 5, 10, 20),
				requeueSteps([]*placementv1alpha1.Work{primaryWorkGen1WithStatus}, notYetAvailableStates, 100),
			),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rateLimiter := newRateLimiterForTest(t, simpleRateLimiterCfg)
			verifyRequeueSteps(t, rateLimiter, tc.steps)
		})
	}
}

// TestWhenWithSkipToFastBackoff tests the When method with the skip to fast backoff option.
func TestWhenWithSkipToFastBackoff(t *testing.T) {
	linkedWorks := []*placementv1alpha1.Work{
		workForBackoffTest(memberReservedNSName1, backoffTestPrimaryWorkName, 1),
	}
	available := stateWith(ApplyResTypeApplied, AvailabilityResultTypeAvailable)
	notTrackable := stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotTrackable)
	notYetAvailable := stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotYetAvailable)
	failedToCheck := stateWith(ApplyResTypeApplied, AvailabilityResultTypeFailed)
	skipped := stateWith(ApplyResTypeFailedToApply, AvailabilityResultTypeSkipped)

	skipEnabledCfg := simpleRateLimiterCfg
	skipEnabledCfg.skipToFastBackoffWhenAllAppliedManifestsAreAvailable = true

	testCases := []struct {
		name  string
		cfg   rateLimiterConfig
		steps []requeueStep
	}{
		{
			// The skip never applies in the fixed delay stage or at the start of the slow backoff stage;
			// afterwards, 10 * 5 = 50, then 50 * 5 = 250 is capped at 200.
			name:  "all manifests available",
			cfg:   skipEnabledCfg,
			steps: requeueSteps(linkedWorks, []*manifestProcessingState{available, available}, 5, 10, 50, 200, 200),
		},
		{
			name:  "all manifests not trackable",
			cfg:   skipEnabledCfg,
			steps: requeueSteps(linkedWorks, []*manifestProcessingState{notTrackable, notTrackable}, 5, 10, 50, 200, 200),
		},
		{
			name:  "mixed available and not trackable manifests",
			cfg:   skipEnabledCfg,
			steps: requeueSteps(linkedWorks, []*manifestProcessingState{available, notTrackable}, 5, 10, 50, 200, 200),
		},
		{
			// With no manifests, all processed manifests are considered to be available.
			name:  "no manifests",
			cfg:   skipEnabledCfg,
			steps: requeueSteps(linkedWorks, []*manifestProcessingState{}, 5, 10, 50, 200, 200),
		},
		{
			name:  "one manifest not yet available",
			cfg:   skipEnabledCfg,
			steps: requeueSteps(linkedWorks, []*manifestProcessingState{available, notYetAvailable}, 5, 10, 20, 100, 200),
		},
		{
			name:  "one manifest failed availability check",
			cfg:   skipEnabledCfg,
			steps: requeueSteps(linkedWorks, []*manifestProcessingState{available, failedToCheck}, 5, 10, 20, 100, 200),
		},
		{
			name:  "one manifest skipped availability check (failed to apply)",
			cfg:   skipEnabledCfg,
			steps: requeueSteps(linkedWorks, []*manifestProcessingState{available, skipped}, 5, 10, 20, 100, 200),
		},
		{
			name:  "all manifests available, skip disabled",
			cfg:   simpleRateLimiterCfg,
			steps: requeueSteps(linkedWorks, []*manifestProcessingState{available, available}, 5, 10, 20, 100, 200),
		},
		{
			name: "manifests become available",
			cfg:  skipEnabledCfg,
			steps: slices.Concat(
				requeueSteps(linkedWorks, []*manifestProcessingState{notYetAvailable}, 5, 10, 20),
				// Reset, as the processing result has changed.
				requeueSteps(linkedWorks, []*manifestProcessingState{available}, 5, 10, 50, 200),
			),
		},
		{
			name: "manifests become unavailable",
			cfg:  skipEnabledCfg,
			steps: slices.Concat(
				requeueSteps(linkedWorks, []*manifestProcessingState{available}, 5, 10, 50),
				// Reset, as the processing result has changed.
				requeueSteps(linkedWorks, []*manifestProcessingState{notYetAvailable}, 5, 10, 20, 100),
			),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rateLimiter := newRateLimiterForTest(t, tc.cfg)
			verifyRequeueSteps(t, rateLimiter, tc.steps)
		})
	}
}

// TestWhenWithMultipleWorks tests the When method with multiple work objects tracked at the same time.
func TestWhenWithMultipleWorks(t *testing.T) {
	workA := workForBackoffTest(memberReservedNSName1, backoffTestPrimaryWorkName, 1)
	workB := workForBackoffTest(memberReservedNSName1, backoffTestOtherWorkName, 1)
	// A work object with the same name as work A, but in a different namespace.
	workC := workForBackoffTest(backoffTestAltNSName, backoffTestPrimaryWorkName, 1)
	states := []*manifestProcessingState{
		stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotYetAvailable),
	}

	rateLimiter := newRateLimiterForTest(t, simpleRateLimiterCfg)
	verifyRequeueSteps(t, rateLimiter, slices.Concat(
		requeueSteps([]*placementv1alpha1.Work{workA}, states, 5, 10),
		requeueSteps([]*placementv1alpha1.Work{workB}, states, 5),
		requeueSteps([]*placementv1alpha1.Work{workA}, states, 20),
		requeueSteps([]*placementv1alpha1.Work{workC}, states, 5),
		requeueSteps([]*placementv1alpha1.Work{workB}, states, 10),
		requeueSteps([]*placementv1alpha1.Work{workC}, states, 10),
		requeueSteps([]*placementv1alpha1.Work{workA}, states, 100),
	))

	keyA := types.NamespacedName{Namespace: memberReservedNSName1, Name: backoffTestPrimaryWorkName}
	keyB := types.NamespacedName{Namespace: memberReservedNSName1, Name: backoffTestOtherWorkName}
	keyC := types.NamespacedName{Namespace: backoffTestAltNSName, Name: backoffTestPrimaryWorkName}
	wantRequeueCounter := map[types.NamespacedName]int{
		keyA: 4,
		keyB: 2,
		keyC: 2,
	}
	if diff := cmp.Diff(rateLimiter.requeueCounter, wantRequeueCounter); diff != "" {
		t.Errorf("requeueCounter mismatches (-got, +want):\n%s", diff)
	}
	wantLastRequeueDelayTracker := map[types.NamespacedName]time.Duration{
		keyA: 100 * time.Second,
		keyB: 10 * time.Second,
		keyC: 10 * time.Second,
	}
	if diff := cmp.Diff(rateLimiter.lastRequeueDelayTracker, wantLastRequeueDelayTracker); diff != "" {
		t.Errorf("lastRequeueDelayTracker mismatches (-got, +want):\n%s", diff)
	}
}

// TestWhenWithRequeueCounterCap tests the When method when the requeue counter reaches its cap.
func TestWhenWithRequeueCounterCap(t *testing.T) {
	linkedWorks := []*placementv1alpha1.Work{
		workForBackoffTest(memberReservedNSName1, backoffTestPrimaryWorkName, 1),
	}
	states := []*manifestProcessingState{
		stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotYetAvailable),
	}
	key := types.NamespacedName{Namespace: memberReservedNSName1, Name: backoffTestPrimaryWorkName}
	wantRequeueCounter := maxNumOfRequeuesToTrack - 1

	t.Run("cap is above the fixed delay and initial slow backoff stages", func(t *testing.T) {
		// The requeue counter is only consulted in the fixed delay stage and at the start of the slow backoff
		// stage; capping it must never send a work object back to these stages.
		if maxNumOfRequeuesToTrack <= attemptsWithFixedDelayUpperBound+1 {
			t.Fatalf("maxNumOfRequeuesToTrack = %d, want > %d", maxNumOfRequeuesToTrack, attemptsWithFixedDelayUpperBound+1)
		}
	})

	t.Run("counter stops at the cap", func(t *testing.T) {
		rateLimiter := newRateLimiterForTest(t, defaultRateLimiterCfg)
		var gotDelay time.Duration
		for range maxNumOfRequeuesToTrack + 50 {
			gotDelay = rateLimiter.When(linkedWorks, states)
		}

		wantDelay := time.Duration(defaultRateLimiterCfg.maxFastBackoffDelaySeconds) * time.Second
		if gotDelay != wantDelay {
			t.Errorf("When() = %v, want %v", gotDelay, wantDelay)
		}
		if gotRequeueCounter := rateLimiter.requeueCounter[key]; gotRequeueCounter != wantRequeueCounter {
			t.Errorf("requeueCounter[%v] = %d, want %d", key, gotRequeueCounter, wantRequeueCounter)
		}
	})

	t.Run("backoff continues at the cap", func(t *testing.T) {
		linkedWorksGenHash, err := computeLinkedWorksObjectGenHash(linkedWorks)
		if err != nil {
			t.Fatalf("computeLinkedWorksObjectGenHash() = %v, want no error", err)
		}
		processingResultHash, err := computeProcessingResultHash(states)
		if err != nil {
			t.Fatalf("computeProcessingResultHash() = %v, want no error", err)
		}
		rateLimiter := withTrackedRequeueStates(
			newRateLimiterForTest(t, defaultRateLimiterCfg),
			map[types.NamespacedName]trackedRequeueState{
				key: {
					requeues:             wantRequeueCounter,
					lastDelay:            20 * time.Second,
					linkedWorksGenHash:   linkedWorksGenHash,
					processingResultHash: processingResultHash,
				},
			},
			0,
		)

		// 20 seconds is above the slow backoff cap (15 seconds); fast backoff continues (20 * 1.5 = 30).
		gotDelay := rateLimiter.When(linkedWorks, states)
		if wantDelay := 30 * time.Second; gotDelay != wantDelay {
			t.Errorf("When() = %v, want %v", gotDelay, wantDelay)
		}
		if gotRequeueCounter := rateLimiter.requeueCounter[key]; gotRequeueCounter != wantRequeueCounter {
			t.Errorf("requeueCounter[%v] = %d, want %d", key, gotRequeueCounter, wantRequeueCounter)
		}
	})
}

// TestForget tests the Forget method.
func TestForget(t *testing.T) {
	work1 := workForBackoffTest(memberReservedNSName1, fmt.Sprintf(workNameTemplate, "1"), 1)
	work2 := workForBackoffTest(memberReservedNSName1, fmt.Sprintf(workNameTemplate, "2"), 1)
	work3 := workForBackoffTest(memberReservedNSName1, fmt.Sprintf(workNameTemplate, "3"), 1)
	key1 := types.NamespacedName{Namespace: work1.Namespace, Name: work1.Name}
	key2 := types.NamespacedName{Namespace: work2.Namespace, Name: work2.Name}
	key3 := types.NamespacedName{Namespace: work3.Namespace, Name: work3.Name}
	trackedState1 := trackedRequeueState{
		requeues:             1,
		lastDelay:            5 * time.Second,
		linkedWorksGenHash:   "gen-hash-1",
		processingResultHash: "res-hash-1",
	}
	trackedState2 := trackedRequeueState{
		requeues:             5,
		lastDelay:            20 * time.Second,
		linkedWorksGenHash:   "gen-hash-2",
		processingResultHash: "res-hash-2",
	}
	trackedState3 := trackedRequeueState{
		requeues:             9,
		lastDelay:            200 * time.Second,
		linkedWorksGenHash:   "gen-hash-3",
		processingResultHash: "res-hash-3",
	}

	testCases := []struct {
		name              string
		trackedStates     map[types.NamespacedName]trackedRequeueState
		forgets           int
		work              *placementv1alpha1.Work
		wantTrackedStates map[types.NamespacedName]trackedRequeueState
		wantForgets       int
	}{
		{
			name: "forget tracked work",
			trackedStates: map[types.NamespacedName]trackedRequeueState{
				key1: trackedState1,
				key2: trackedState2,
				key3: trackedState3,
			},
			work: work2,
			wantTrackedStates: map[types.NamespacedName]trackedRequeueState{
				key1: trackedState1,
				key3: trackedState3,
			},
			wantForgets: 1,
		},
		{
			name: "forget untracked work",
			trackedStates: map[types.NamespacedName]trackedRequeueState{
				key1: trackedState1,
				key3: trackedState3,
			},
			forgets: 10,
			work:    work2,
			wantTrackedStates: map[types.NamespacedName]trackedRequeueState{
				key1: trackedState1,
				key3: trackedState3,
			},
			wantForgets: 11,
		},
		{
			name: "forget tracked work, triggering garbage collection",
			trackedStates: map[types.NamespacedName]trackedRequeueState{
				key1: trackedState1,
				key2: trackedState2,
				key3: trackedState3,
			},
			forgets: gcTriggeringForgetThreshold - 1,
			work:    work2,
			wantTrackedStates: map[types.NamespacedName]trackedRequeueState{
				key1: trackedState1,
				key3: trackedState3,
			},
			wantForgets: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rateLimiter := withTrackedRequeueStates(newRateLimiterForTest(t, simpleRateLimiterCfg), tc.trackedStates, tc.forgets)
			rateLimiter.Forget(tc.work)

			wantRateLimiter := withTrackedRequeueStates(newRateLimiterForTest(t, simpleRateLimiterCfg), tc.wantTrackedStates, tc.wantForgets)
			if diff := cmp.Diff(rateLimiter, wantRateLimiter, rateLimiterCmpOpts...); diff != "" {
				t.Errorf("Forget() mismatches (-got, +want):\n%s", diff)
			}

			// After the work object is forgotten, its requeues start over with the fixed delay.
			gotDelay := rateLimiter.When(
				[]*placementv1alpha1.Work{tc.work},
				[]*manifestProcessingState{stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotYetAvailable)},
			)
			if wantDelay := time.Duration(simpleRateLimiterCfg.fixedDelaySeconds) * time.Second; gotDelay != wantDelay {
				t.Errorf("When() after Forget() = %v, want %v", gotDelay, wantDelay)
			}
		})
	}
}

// TestGarbageCollect tests the garbageCollect method.
func TestGarbageCollect(t *testing.T) {
	testCases := []struct {
		name          string
		trackedStates map[types.NamespacedName]trackedRequeueState
		forgets       int
	}{
		{
			name: "populated trackers",
			trackedStates: map[types.NamespacedName]trackedRequeueState{
				{Namespace: memberReservedNSName1, Name: backoffTestPrimaryWorkName}: {
					requeues:             3,
					lastDelay:            20 * time.Second,
					linkedWorksGenHash:   "gen-hash-1",
					processingResultHash: "res-hash-1",
				},
				{Namespace: backoffTestAltNSName, Name: backoffTestOtherWorkName}: {
					requeues:             7,
					lastDelay:            200 * time.Second,
					linkedWorksGenHash:   "gen-hash-2",
					processingResultHash: "res-hash-2",
				},
			},
			forgets: 42,
		},
		{
			name:          "empty trackers",
			trackedStates: map[types.NamespacedName]trackedRequeueState{},
			forgets:       7,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rateLimiter := withTrackedRequeueStates(newRateLimiterForTest(t, simpleRateLimiterCfg), tc.trackedStates, tc.forgets)
			oldTrackers := []any{
				rateLimiter.requeueCounter,
				rateLimiter.lastRequeueDelayTracker,
				rateLimiter.lastTrackedLinkedWorksGenHash,
				rateLimiter.lastTrackedProcessingResultHash,
			}

			rateLimiter.garbageCollect()

			// Note that the comparison does not equate nil and empty maps; the rebuilt trackers must be initialized.
			wantRateLimiter := withTrackedRequeueStates(newRateLimiterForTest(t, simpleRateLimiterCfg), tc.trackedStates, 0)
			if diff := cmp.Diff(rateLimiter, wantRateLimiter, rateLimiterCmpOpts...); diff != "" {
				t.Errorf("garbageCollect() mismatches (-got, +want):\n%s", diff)
			}

			newTrackers := []any{
				rateLimiter.requeueCounter,
				rateLimiter.lastRequeueDelayTracker,
				rateLimiter.lastTrackedLinkedWorksGenHash,
				rateLimiter.lastTrackedProcessingResultHash,
			}
			for idx := range oldTrackers {
				if reflect.ValueOf(oldTrackers[idx]).Pointer() == reflect.ValueOf(newTrackers[idx]).Pointer() {
					t.Errorf("garbageCollect() did not rebuild tracker #%d, want a new map", idx)
				}
			}

			// The rate limiter should remain usable after garbage collection.
			gotDelay := rateLimiter.When(
				[]*placementv1alpha1.Work{workForBackoffTest(memberReservedNSName1, backoffTestOtherWorkName, 1)},
				[]*manifestProcessingState{stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotYetAvailable)},
			)
			if wantDelay := time.Duration(simpleRateLimiterCfg.fixedDelaySeconds) * time.Second; gotDelay != wantDelay {
				t.Errorf("When() after garbageCollect() = %v, want %v", gotDelay, wantDelay)
			}
		})
	}
}

// TestComputeLinkedWorksObjectGenHash tests the computeLinkedWorksObjectGenHash function.
func TestComputeLinkedWorksObjectGenHash(t *testing.T) {
	primaryWorkGen1 := workForBackoffTest(memberReservedNSName1, backoffTestPrimaryWorkName, 1)
	secondaryWorkGen1 := workForBackoffTest(memberReservedNSName1, backoffTestSecondaryWorkName, 1)

	t.Run("no linked work objects", func(t *testing.T) {
		for _, linkedWorks := range [][]*placementv1alpha1.Work{nil, {}} {
			gotHash, err := computeLinkedWorksObjectGenHash(linkedWorks)
			if err != nil {
				t.Fatalf("computeLinkedWorksObjectGenHash(%v) = %v, want no error", linkedWorks, err)
			}
			if gotHash != emptyJSONListSHA256 {
				t.Errorf("computeLinkedWorksObjectGenHash(%v) = %s, want %s", linkedWorks, gotHash, emptyJSONListSHA256)
			}
		}
	})

	primaryWorkGen1WithStatus := workWithLabelsAndStatusForBackoffTest(memberReservedNSName1, backoffTestPrimaryWorkName, 1)

	testCases := []struct {
		name         string
		linkedWorksA []*placementv1alpha1.Work
		linkedWorksB []*placementv1alpha1.Work
		wantSameHash bool
	}{
		{
			name:         "same names and generations (different objects)",
			linkedWorksA: []*placementv1alpha1.Work{primaryWorkGen1, secondaryWorkGen1},
			linkedWorksB: []*placementv1alpha1.Work{
				workForBackoffTest(memberReservedNSName1, backoffTestPrimaryWorkName, 1),
				workForBackoffTest(memberReservedNSName1, backoffTestSecondaryWorkName, 1),
			},
			wantSameHash: true,
		},
		{
			name:         "labels and status do not count",
			linkedWorksA: []*placementv1alpha1.Work{primaryWorkGen1},
			linkedWorksB: []*placementv1alpha1.Work{primaryWorkGen1WithStatus},
			wantSameHash: true,
		},
		{
			// Linked work objects always share the same namespace.
			name:         "namespace does not count",
			linkedWorksA: []*placementv1alpha1.Work{primaryWorkGen1},
			linkedWorksB: []*placementv1alpha1.Work{workForBackoffTest(backoffTestAltNSName, backoffTestPrimaryWorkName, 1)},
			wantSameHash: true,
		},
		{
			name:         "primary work object generation change",
			linkedWorksA: []*placementv1alpha1.Work{primaryWorkGen1},
			linkedWorksB: []*placementv1alpha1.Work{workForBackoffTest(memberReservedNSName1, backoffTestPrimaryWorkName, 2)},
		},
		{
			name:         "secondary work object generation change",
			linkedWorksA: []*placementv1alpha1.Work{primaryWorkGen1, secondaryWorkGen1},
			linkedWorksB: []*placementv1alpha1.Work{
				primaryWorkGen1,
				workForBackoffTest(memberReservedNSName1, backoffTestSecondaryWorkName, 2),
			},
		},
		{
			name:         "linked work object added",
			linkedWorksA: []*placementv1alpha1.Work{primaryWorkGen1},
			linkedWorksB: []*placementv1alpha1.Work{primaryWorkGen1, secondaryWorkGen1},
		},
		{
			name:         "linked work object order change",
			linkedWorksA: []*placementv1alpha1.Work{primaryWorkGen1, secondaryWorkGen1},
			linkedWorksB: []*placementv1alpha1.Work{secondaryWorkGen1, primaryWorkGen1},
		},
		{
			name:         "name change with the same generation",
			linkedWorksA: []*placementv1alpha1.Work{primaryWorkGen1},
			linkedWorksB: []*placementv1alpha1.Work{workForBackoffTest(memberReservedNSName1, backoffTestOtherWorkName, 1)},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hashA, err := computeLinkedWorksObjectGenHash(tc.linkedWorksA)
			if err != nil {
				t.Fatalf("computeLinkedWorksObjectGenHash(A) = %v, want no error", err)
			}
			hashB, err := computeLinkedWorksObjectGenHash(tc.linkedWorksB)
			if err != nil {
				t.Fatalf("computeLinkedWorksObjectGenHash(B) = %v, want no error", err)
			}
			if gotSameHash := hashA == hashB; gotSameHash != tc.wantSameHash {
				t.Errorf("computeLinkedWorksObjectGenHash(A) == computeLinkedWorksObjectGenHash(B): %t, want %t (A: %s, B: %s)",
					gotSameHash, tc.wantSameHash, hashA, hashB)
			}
		})
	}
}

// TestComputeProcessingResultHash tests the computeProcessingResultHash function.
func TestComputeProcessingResultHash(t *testing.T) {
	t.Run("no manifests", func(t *testing.T) {
		for _, states := range [][]*manifestProcessingState{nil, {}} {
			gotHash, err := computeProcessingResultHash(states)
			if err != nil {
				t.Fatalf("computeProcessingResultHash(%v) = %v, want no error", states, err)
			}
			if gotHash != emptyJSONListSHA256 {
				t.Errorf("computeProcessingResultHash(%v) = %s, want %s", states, gotHash, emptyJSONListSHA256)
			}
		}
	})

	failedToApply := stateWith(ApplyResTypeFailedToApply, AvailabilityResultTypeSkipped)
	available := stateWith(ApplyResTypeApplied, AvailabilityResultTypeAvailable)
	notTrackable := stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotTrackable)
	notYetAvailable := stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotYetAvailable)

	testCases := []struct {
		name         string
		statesA      []*manifestProcessingState
		statesB      []*manifestProcessingState
		wantSameHash bool
	}{
		{
			name:    "apply error does not count",
			statesA: []*manifestProcessingState{failedToApply},
			statesB: []*manifestProcessingState{
				{
					applyRes:             ApplyResTypeFailedToApply,
					availabilityCheckRes: AvailabilityResultTypeSkipped,
					applyErr:             fmt.Errorf("failed to apply the manifest"),
				},
			},
			wantSameHash: true,
		},
		{
			name:    "availability check error does not count",
			statesA: []*manifestProcessingState{stateWith(ApplyResTypeApplied, AvailabilityResultTypeFailed)},
			statesB: []*manifestProcessingState{
				{
					applyRes:             ApplyResTypeApplied,
					availabilityCheckRes: AvailabilityResultTypeFailed,
					availabilityCheckErr: fmt.Errorf("failed to check the availability"),
				},
			},
			wantSameHash: true,
		},
		{
			name:    "diffs do not count",
			statesA: []*manifestProcessingState{stateWith(ApplyResTypeFoundDrifts, AvailabilityResultTypeSkipped)},
			statesB: []*manifestProcessingState{
				{
					applyRes:             ApplyResTypeFoundDrifts,
					availabilityCheckRes: AvailabilityResultTypeSkipped,
					diffs: []placementv1alpha1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInMember: "1",
							ValueInHub:    "2",
						},
					},
				},
			},
			wantSameHash: true,
		},
		{
			name:    "manifest identity does not count",
			statesA: []*manifestProcessingState{available},
			statesB: []*manifestProcessingState{
				{
					id: &placementv1alpha1.ManifestIdentifier{
						Ordinal:   1,
						Name:      configMapName,
						Namespace: ns.Name,
					},
					idStr:                "GV=/v1, Kind=ConfigMap, Namespace=ns-1, Name=configmap-1",
					fromWorkObj:          workForBackoffTest(memberReservedNSName1, backoffTestSecondaryWorkName, 1),
					applyRes:             ApplyResTypeApplied,
					availabilityCheckRes: AvailabilityResultTypeAvailable,
				},
			},
			wantSameHash: true,
		},
		{
			// This is a case that normally should not occur.
			name:    "zero-value state vs. no manifests",
			statesA: []*manifestProcessingState{},
			statesB: []*manifestProcessingState{{}},
		},
		{
			name:    "availability check result change (NotYetAvailable vs. Available)",
			statesA: []*manifestProcessingState{notYetAvailable},
			statesB: []*manifestProcessingState{available},
		},
		{
			name:    "availability check result change (Available vs. NotTrackable)",
			statesA: []*manifestProcessingState{available},
			statesB: []*manifestProcessingState{notTrackable},
		},
		{
			name:    "apply result change",
			statesA: []*manifestProcessingState{stateWith(ApplyResTypeDecodingErred, AvailabilityResultTypeSkipped)},
			statesB: []*manifestProcessingState{failedToApply},
		},
		{
			name:    "manifest order change",
			statesA: []*manifestProcessingState{failedToApply, available, notTrackable},
			statesB: []*manifestProcessingState{available, failedToApply, notTrackable},
		},
		{
			name:    "manifest added",
			statesA: []*manifestProcessingState{available},
			statesB: []*manifestProcessingState{available, available},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hashA, err := computeProcessingResultHash(tc.statesA)
			if err != nil {
				t.Fatalf("computeProcessingResultHash(A) = %v, want no error", err)
			}
			hashB, err := computeProcessingResultHash(tc.statesB)
			if err != nil {
				t.Fatalf("computeProcessingResultHash(B) = %v, want no error", err)
			}
			if gotSameHash := hashA == hashB; gotSameHash != tc.wantSameHash {
				t.Errorf("computeProcessingResultHash(A) == computeProcessingResultHash(B): %t, want %t (A: %s, B: %s)",
					gotSameHash, tc.wantSameHash, hashA, hashB)
			}
		})
	}
}

// TestAreAllProcessedManifestsAvailable tests the areAllProcessedManifestsAvailable function.
func TestAreAllProcessedManifestsAvailable(t *testing.T) {
	available := stateWith(ApplyResTypeApplied, AvailabilityResultTypeAvailable)
	notTrackable := stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotTrackable)
	notYetAvailable := stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotYetAvailable)

	testCases := []struct {
		name   string
		states []*manifestProcessingState
		want   bool
	}{
		{
			name:   "nil",
			states: nil,
			want:   true,
		},
		{
			name:   "no manifests",
			states: []*manifestProcessingState{},
			want:   true,
		},
		{
			name:   "available",
			states: []*manifestProcessingState{available},
			want:   true,
		},
		{
			name:   "not trackable",
			states: []*manifestProcessingState{notTrackable},
			want:   true,
		},
		{
			name:   "not yet available",
			states: []*manifestProcessingState{notYetAvailable},
			want:   false,
		},
		{
			name:   "failed to check availability",
			states: []*manifestProcessingState{stateWith(ApplyResTypeApplied, AvailabilityResultTypeFailed)},
			want:   false,
		},
		{
			name:   "availability check skipped",
			states: []*manifestProcessingState{stateWith(ApplyResTypeFailedToApply, AvailabilityResultTypeSkipped)},
			want:   false,
		},
		{
			name:   "no availability check result",
			states: []*manifestProcessingState{stateWith(ApplyResTypePreparingToProcess, "")},
			want:   false,
		},
		{
			name:   "mixed available and not trackable",
			states: []*manifestProcessingState{available, notTrackable},
			want:   true,
		},
		{
			name:   "mixed available and not yet available",
			states: []*manifestProcessingState{available, notYetAvailable},
			want:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := areAllProcessedManifestsAvailable(tc.states); got != tc.want {
				t.Errorf("areAllProcessedManifestsAvailable() = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestRateLimiterConcurrentAccess tests that the rate limiter can be used concurrently.
//
// Run with the race detector on for this test to be meaningful.
func TestRateLimiterConcurrentAccess(t *testing.T) {
	const (
		concurrentWorkerCount  = 16
		iterationsPerWorker    = 100
		forgetEveryNIterations = 10
		workCount              = 4
	)

	works := make([]*placementv1alpha1.Work, 0, workCount)
	for idx := range workCount {
		works = append(works, workForBackoffTest(memberReservedNSName1, fmt.Sprintf(workNameTemplate, fmt.Sprint(idx)), 1))
	}
	states := []*manifestProcessingState{
		stateWith(ApplyResTypeApplied, AvailabilityResultTypeNotYetAvailable),
	}

	rateLimiter := newRateLimiterForTest(t, defaultRateLimiterCfg)
	var wg sync.WaitGroup
	for workerIdx := range concurrentWorkerCount {
		wg.Go(func() {
			for iterIdx := range iterationsPerWorker {
				// Workers share work objects with each other.
				work := works[(workerIdx+iterIdx)%workCount]
				rateLimiter.When([]*placementv1alpha1.Work{work}, states)
				if (iterIdx+1)%forgetEveryNIterations == 0 {
					rateLimiter.Forget(work)
				}
			}
		})
	}
	wg.Wait()

	wantForgets := concurrentWorkerCount * iterationsPerWorker / forgetEveryNIterations
	if rateLimiter.forgets != wantForgets {
		t.Errorf("forgets = %d, want %d", rateLimiter.forgets, wantForgets)
	}

	// All trackers should always track the same set of work objects.
	wantKeys := sets.KeySet(rateLimiter.requeueCounter)
	if diff := cmp.Diff(sets.KeySet(rateLimiter.lastRequeueDelayTracker), wantKeys); diff != "" {
		t.Errorf("lastRequeueDelayTracker keys mismatch (-got, +want):\n%s", diff)
	}
	if diff := cmp.Diff(sets.KeySet(rateLimiter.lastTrackedLinkedWorksGenHash), wantKeys); diff != "" {
		t.Errorf("lastTrackedLinkedWorksGenHash keys mismatch (-got, +want):\n%s", diff)
	}
	if diff := cmp.Diff(sets.KeySet(rateLimiter.lastTrackedProcessingResultHash), wantKeys); diff != "" {
		t.Errorf("lastTrackedProcessingResultHash keys mismatch (-got, +want):\n%s", diff)
	}
}
