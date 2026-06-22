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

package options

import (
	"flag"
	"fmt"
	"strconv"
)

type ApplierOptions struct {
	// The time in minutes where the KubeFleet member agent will wait before force deleting all the
	// resources from a member cluster if the placement that owns the resources has been removed
	// from the hub cluster.
	//
	// KubeFleet member agent leverages owner references and foreground deletion to ensure that
	// when a placement itself is gone, all of its selected resources will be removed as well. However,
	// there are situations where Kubernetes might not be able to complete the cascade deletion timely;
	// if the agent detects that a deletion has been stuck for a period of time longer than the value
	// specified by this option, it will issue direct DELETE calls at the left resources.
	ResourceForceDeletionWaitTimeMinutes int

	// The KubeFleet member agent periodically re-processes all resources propagated via a placement.
	// This helps ensure that KubeFleet can detect inadvertent changes on processed resources and
	// take actions accordingly.
	//
	// The system uses a rate limiter to regulate the frequency of the periodic re-processing; the
	// flow is as follows:
	//
	// * when a placement is first processed, the system re-queues the placement for re-processing
	//   at a fixed delay for a few attempts;
	// * if the processing outcomes remain the same across attempts, the system will start to
	//   re-process the placement with a slowly but exponentially increasing delay, until the delay
	//   reaches a user-configurable maximum value;
	// * if the processing outcomes continues to stay the same across attempts, the system will
	//   start to re-process the placement with a fastly and exponentially increasing delay, until the
	//   delay reaches a user-configurable maximum value;
	//
	// In addition, the rate limiter provides an optional shortcut that allows placements that
	// has been processed successfully (i.e., all the resources are applied and are found
	// to be available, or all the resources have their diffs reported) to skip the slow backoff
	// stage and jump to the fast backoff stage directly.
	//
	// See the options below for further details:

	// The number of attempts that the KubeFleet member agent will re-process a placement with a fixed delay.
	RequeueRateLimiterAttemptsWithFixedDelay int

	// The fixed delay in seconds that the KubeFleet member agent will use.
	RequeueRateLimiterFixedDelaySeconds int

	// The exponential delay growth base that the KubeFleet member agent will use for the slow backoff stage.
	RequeueRateLimiterExponentialBaseForSlowBackoff float64

	// The initial delay in seconds that the KubeFleet member agent will use for the slow backoff stage.
	RequeueRateLimiterInitialSlowBackoffDelaySeconds int

	// The maximum delay in seconds that the KubeFleet member agent will use for the slow backoff stage.
	RequeueRateLimiterMaxSlowBackoffDelaySeconds int

	// The exponential delay growth base that the KubeFleet member agent will use for the fast backoff stage.
	RequeueRateLimiterExponentialBaseForFastBackoff float64

	// The max delay in seconds that the KubeFleet member agent will use for the fast backoff stage.
	RequeueRateLimiterMaxFastBackoffDelaySeconds int

	// Allow placements that have been processed successfully to skip the slow backoff stage and jump to
	// the fast backoff stage directly or not.
	RequeueRateLimiterSkipToFastBackoffForAvailableOrDiffReportedWorkObjs bool

	// By default, the KubeFleet member agent processes all placements in a FIFO order. Alternatively,
	// one can set up the agent to prioritize placements that are just created or updated; this is most
	// helpful in cases where the number of placements per member cluster gets large and the processing
	// latencies for newly created/updated placements become a concern.
	//
	// KubeFleet calculates the priority score for a placement using the formula below:
	//
	// Pri(placement) = A * (placement age in minutes) + B,
	//
	// where A is a negative integer and B a positive one, so that the younger the placement is, the
	// higher priority it gets. The values of A and B are user-configurable.
	//
	// See the options below for further details:

	// Enable prioritized processing of newly created/updated placements or not.
	EnablePriorityQueue bool

	// The coefficient A in the linear equation for calculating the priority score of a placement.
	PriorityLinearEquationCoEffA int

	// The coefficient B in the linear equation for calculating the priority score of a placement.
	PriorityLinearEquationCoEffB int
}

func (o *ApplierOptions) AddFlags(flags *flag.FlagSet) {
	flags.Var(
		newResForceDeletionWaitTimeMinutesValue(5, &o.ResourceForceDeletionWaitTimeMinutes),
		"deletion-wait-time",
		"The time in minutes where the KubeFleet member agent will wait before force deleting all the resources from a member cluster if the placement that owns the resources has been removed from the hub cluster. Default is 5 minutes. The value must be in the range [1, 60].")

	flags.BoolVar(
		&o.EnablePriorityQueue,
		"enable-work-applier-priority-queue",
		false,
		"Enable prioritized processing of newly created/updated placements or not. Default is false, which means placements will be processed in a FIFO order.")

	flags.Var(
		newRequeueAttemptsWithFixedDelayValue(1, &o.RequeueRateLimiterAttemptsWithFixedDelay),
		"work-applier-requeue-rate-limiter-attempts-with-fixed-delay",
		"The number of attempts with a fixed delay before the KubeFleet member agent switches to exponential backoff when re-processing a placement. Default is 1. The value must be in the range [1, 40].")

	flags.Var(
		newRequeueFixedDelaySecondsValue(5, &o.RequeueRateLimiterFixedDelaySeconds),
		"work-applier-requeue-rate-limiter-fixed-delay-seconds",
		"The fixed delay in seconds for the KubeFleet member agent when re-processing a placement before switching to exponential backoff. Default is 5 seconds. The value must be in the range [2, 300].")

	flags.Var(
		newRequeueExpBaseForSlowBackoffValue(1.2, &o.RequeueRateLimiterExponentialBaseForSlowBackoff),
		"work-applier-requeue-rate-limiter-exponential-base-for-slow-backoff",
		"The exponential delay growth base for the slow backoff stage when the KubeFleet member agent re-processes a placement. Default is 1.2. The value must be in the range [1.05, 2].")

	flags.Var(
		newRequeueInitSlowBackoffDelaySecondsValue(2, &o.RequeueRateLimiterInitialSlowBackoffDelaySeconds),
		"work-applier-requeue-rate-limiter-initial-slow-backoff-delay-seconds",
		"The initial delay in seconds for the slow backoff stage when the KubeFleet member agent re-processes a placement. Default is 2 seconds. The value must be no less than 2.")

	flags.Var(
		newRequeueMaxSlowBackoffDelaySecondsValue(15, &o.RequeueRateLimiterMaxSlowBackoffDelaySeconds),
		"work-applier-requeue-rate-limiter-max-slow-backoff-delay-seconds",
		"The maximum delay in seconds for the slow backoff stage when the KubeFleet member agent re-processes a placement. Default is 15 seconds. The value must be no less than 2.")

	flags.Var(
		newRequeueExpBaseForFastBackoffValue(1.5, &o.RequeueRateLimiterExponentialBaseForFastBackoff),
		"work-applier-requeue-rate-limiter-exponential-base-for-fast-backoff",
		"The exponential delay growth base for the fast backoff stage when the KubeFleet member agent re-processes a placement. Default is 1.5. The value must be in the range (1, 2].")

	flags.Var(
		newRequeueMaxFastBackoffDelaySecondsValue(900, &o.RequeueRateLimiterMaxFastBackoffDelaySeconds),
		"work-applier-requeue-rate-limiter-max-fast-backoff-delay-seconds",
		"The maximum delay in seconds for the fast backoff stage when the KubeFleet member agent re-processes a placement. Default is 900 seconds. The value must be in the range (0, 3600].")

	flags.BoolVar(
		&o.RequeueRateLimiterSkipToFastBackoffForAvailableOrDiffReportedWorkObjs,
		"work-applier-requeue-rate-limiter-skip-to-fast-backoff-for-available-or-diff-reported-work-objs",
		true,
		"Allow placements that have been processed successfully to skip the slow backoff stage and jump to the fast backoff stage directly or not when periodically re-processing a placement. Default is true.")

	flags.Var(
		newPriCoEffAValue(-3, &o.PriorityLinearEquationCoEffA),
		"work-applier-priority-linear-equation-coeff-a",
		"The coefficient A in the linear equation for calculating the priority score of a placement. The value must be a negative integer no less than -100. Default is -3.")

	flags.Var(
		newPriCoEffBValue(100, &o.PriorityLinearEquationCoEffB),
		"work-applier-priority-linear-equation-coeff-b",
		"The coefficient B in the linear equation for calculating the priority score of a placement. The value must be a positive integer no greater than 1000. Default is 100.")
}

type ResForceDeletionWaitTimeMinutes int

func (v *ResForceDeletionWaitTimeMinutes) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *ResForceDeletionWaitTimeMinutes) Set(s string) error {
	t, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse integer value: %w", err)
	}

	if t < 1 || t > 60 {
		return fmt.Errorf("resource force deletion wait time in minutes is set to an invalid value (%d), must be a value in the range [1, 60]", t)
	}
	*v = ResForceDeletionWaitTimeMinutes(t)
	return nil
}

func newResForceDeletionWaitTimeMinutesValue(defaultValue int, p *int) *ResForceDeletionWaitTimeMinutes {
	*p = defaultValue
	return (*ResForceDeletionWaitTimeMinutes)(p)
}

type RequeueAttemptsWithFixedDelay int

func (v *RequeueAttemptsWithFixedDelay) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *RequeueAttemptsWithFixedDelay) Set(s string) error {
	t, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse integer value: %w", err)
	}

	if t < 1 || t > 40 {
		return fmt.Errorf("requeue rate limiter attempts with fixed delay is set to an invalid value (%d), must be a value in the range [1, 40]", t)
	}
	*v = RequeueAttemptsWithFixedDelay(t)
	return nil
}

func newRequeueAttemptsWithFixedDelayValue(defaultValue int, p *int) *RequeueAttemptsWithFixedDelay {
	*p = defaultValue
	return (*RequeueAttemptsWithFixedDelay)(p)
}

type RequeueFixedDelaySeconds int

func (v *RequeueFixedDelaySeconds) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *RequeueFixedDelaySeconds) Set(s string) error {
	t, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse integer value: %w", err)
	}

	if t < 2 || t > 300 {
		return fmt.Errorf("requeue rate limiter fixed delay seconds is set to an invalid value (%d), must be a value in the range [2, 300]", t)
	}
	*v = RequeueFixedDelaySeconds(t)
	return nil
}

func newRequeueFixedDelaySecondsValue(defaultValue int, p *int) *RequeueFixedDelaySeconds {
	*p = defaultValue
	return (*RequeueFixedDelaySeconds)(p)
}

// RequeueExpBaseForSlowBackoff is a custom flag value type for the
// RequeueRateLimiterExponentialBaseForSlowBackoff option.
type RequeueExpBaseForSlowBackoff float64

func (v *RequeueExpBaseForSlowBackoff) String() string {
	return fmt.Sprintf("%g", *v)
}

func (v *RequeueExpBaseForSlowBackoff) Set(s string) error {
	exp, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("failed to parse float value: %w", err)
	}

	if exp < 1.05 || exp > 2 {
		return fmt.Errorf("requeue rate limiter exponential base for slow backoff is set to an invalid value (%g), must be a value in the range [1.05, 2]", exp)
	}
	*v = RequeueExpBaseForSlowBackoff(exp)
	return nil
}

func newRequeueExpBaseForSlowBackoffValue(defaultValue float64, p *float64) *RequeueExpBaseForSlowBackoff {
	*p = defaultValue
	return (*RequeueExpBaseForSlowBackoff)(p)
}

type RequeueInitSlowBackoffDelaySeconds int

func (v *RequeueInitSlowBackoffDelaySeconds) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *RequeueInitSlowBackoffDelaySeconds) Set(s string) error {
	t, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse integer value: %w", err)
	}

	if t < 2 {
		return fmt.Errorf("requeue rate limiter initial slow backoff delay seconds is set to an invalid value (%d), must be a value no less than 2", t)
	}
	*v = RequeueInitSlowBackoffDelaySeconds(t)
	return nil
}

func newRequeueInitSlowBackoffDelaySecondsValue(defaultValue int, p *int) *RequeueInitSlowBackoffDelaySeconds {
	*p = defaultValue
	return (*RequeueInitSlowBackoffDelaySeconds)(p)
}

type RequeueMaxSlowBackoffDelaySeconds int

func (v *RequeueMaxSlowBackoffDelaySeconds) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *RequeueMaxSlowBackoffDelaySeconds) Set(s string) error {
	t, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse integer value: %w", err)
	}

	if t < 2 {
		return fmt.Errorf("requeue rate limiter max slow backoff delay seconds is set to an invalid value (%d), must be a value no less than 2", t)
	}
	*v = RequeueMaxSlowBackoffDelaySeconds(t)
	return nil
}

func newRequeueMaxSlowBackoffDelaySecondsValue(defaultValue int, p *int) *RequeueMaxSlowBackoffDelaySeconds {
	*p = defaultValue
	return (*RequeueMaxSlowBackoffDelaySeconds)(p)
}

type RequeueExpBaseForFastBackoff float64

func (v *RequeueExpBaseForFastBackoff) String() string {
	return fmt.Sprintf("%g", *v)
}

func (v *RequeueExpBaseForFastBackoff) Set(s string) error {
	exp, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("failed to parse float value: %w", err)
	}

	if exp <= 1 || exp > 2 {
		return fmt.Errorf("requeue rate limiter exponential base for fast backoff is set to an invalid value (%g), must be a value in the range (1, 2]", exp)
	}
	*v = RequeueExpBaseForFastBackoff(exp)
	return nil
}

func newRequeueExpBaseForFastBackoffValue(defaultValue float64, p *float64) *RequeueExpBaseForFastBackoff {
	*p = defaultValue
	return (*RequeueExpBaseForFastBackoff)(p)
}

type RequeueMaxFastBackoffDelaySeconds int

func (v *RequeueMaxFastBackoffDelaySeconds) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *RequeueMaxFastBackoffDelaySeconds) Set(s string) error {
	t, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse integer value: %w", err)
	}

	if t <= 0 || t > 3600 {
		return fmt.Errorf("requeue rate limiter max fast backoff delay seconds is set to an invalid value (%d), must be a value in the range (0, 3600]", t)
	}
	*v = RequeueMaxFastBackoffDelaySeconds(t)
	return nil
}

func newRequeueMaxFastBackoffDelaySecondsValue(defaultValue int, p *int) *RequeueMaxFastBackoffDelaySeconds {
	*p = defaultValue
	return (*RequeueMaxFastBackoffDelaySeconds)(p)
}

type PriCoEffA int

func (v *PriCoEffA) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *PriCoEffA) Set(s string) error {
	coeff, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse integer value: %w", err)
	}

	if coeff >= 0 || coeff < -100 {
		return fmt.Errorf("priority linear equation coefficient A is set to an invalid value (%d), must be a negative integer no less than -100", coeff)
	}

	*v = PriCoEffA(coeff)
	return nil
}

func newPriCoEffAValue(defaultValue int, p *int) *PriCoEffA {
	*p = defaultValue
	return (*PriCoEffA)(p)
}

type PriCoEffB int

func (v *PriCoEffB) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *PriCoEffB) Set(s string) error {
	coeff, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse integer value: %w", err)
	}

	if coeff <= 0 || coeff > 1000 {
		return fmt.Errorf("priority linear equation coefficient B is set to an invalid value (%d), must be a positive integer no greater than 1000", coeff)
	}

	*v = PriCoEffB(coeff)
	return nil
}

func newPriCoEffBValue(defaultValue int, p *int) *PriCoEffB {
	*p = defaultValue
	return (*PriCoEffB)(p)
}
