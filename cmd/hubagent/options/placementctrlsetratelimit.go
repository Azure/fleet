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

package options

import (
	"flag"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/time/rate"
	"k8s.io/client-go/util/workqueue"
)

// RateLimitOptions are options for rate limiter.
type RateLimitOptions struct {
	// RateLimiterBaseDelay is the base delay for ItemExponentialFailureRateLimiter.
	RateLimiterBaseDelay time.Duration

	// RateLimiterMaxDelay is the max delay for ItemExponentialFailureRateLimiter.
	RateLimiterMaxDelay time.Duration

	// RateLimiterQPS is the qps for BucketRateLimiter
	RateLimiterQPS int

	// RateLimiterBucketSize is the bucket size for BucketRateLimiter
	RateLimiterBucketSize int
}

// AddFlags adds flags to the specified FlagSet.
func (o *RateLimitOptions) AddFlags(fs *flag.FlagSet) {
	fs.Var(
		newRateLimiterBaseDelayValueWithValidation(5*time.Millisecond, &o.RateLimiterBaseDelay),
		"rate-limiter-base-delay",
		"The base delay for the placement controller set rate limiter. Default to 5ms. Must be a value between [1ms, 200ms].",
	)

	fs.Var(
		newRateLimiterMaxDelayValueWithValidation(60*time.Second, &o.RateLimiterMaxDelay),
		"rate-limiter-max-delay",
		"The max delay for the placement controller set rate limiter. Default to 60s. Must be a value in the range [1s, 5m] and the value must be greater than the base delay.",
	)

	fs.Var(
		newRateLimiterQPSValueWithValidation(10, &o.RateLimiterQPS),
		"rate-limiter-qps",
		"The QPS for the placement controller set rate limiter. Default to 10. Must be a positive integer in the range [1, 1000].",
	)

	fs.Var(
		newRateLimiterBucketSizeValueWithValidation(100, &o.RateLimiterBucketSize),
		"rate-limiter-bucket-size",
		"The bucket size for the placement controller set rate limiter. Default to 100. Must be a positive integer in the range [1, 10000] and the value must be greater than the QPS.",
	)
}

// DefaultControllerRateLimiter provide a default rate limiter for controller, and users can tune it by corresponding flags.
func DefaultControllerRateLimiter(opts RateLimitOptions) workqueue.TypedRateLimiter[any] {
	// set defaults
	if opts.RateLimiterBaseDelay <= 0 {
		opts.RateLimiterBaseDelay = 5 * time.Millisecond
	}
	if opts.RateLimiterMaxDelay <= 0 {
		opts.RateLimiterMaxDelay = 60 * time.Second
	}
	if opts.RateLimiterQPS <= 0 {
		opts.RateLimiterQPS = 10
	}
	if opts.RateLimiterBucketSize <= 0 {
		opts.RateLimiterBucketSize = 100
	}
	return workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[any](opts.RateLimiterBaseDelay, opts.RateLimiterMaxDelay),
		&workqueue.TypedBucketRateLimiter[any]{Limiter: rate.NewLimiter(rate.Limit(opts.RateLimiterQPS), opts.RateLimiterBucketSize)},
	)
}

// A list of flag variables that allow pluggable validation logic when parsing the input args.

type RateLimiterBaseDelayValueWithValidation time.Duration

func (v *RateLimiterBaseDelayValueWithValidation) String() string {
	return time.Duration(*v).String()
}

func (v *RateLimiterBaseDelayValueWithValidation) Set(s string) error {
	duration, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("failed to parse time duration: %w", err)
	}
	if duration < time.Millisecond || duration > 200*time.Millisecond {
		return fmt.Errorf("the base delay must be a value between [1ms, 200ms]")
	}
	*v = RateLimiterBaseDelayValueWithValidation(duration)
	return nil
}

func newRateLimiterBaseDelayValueWithValidation(defaultVal time.Duration, p *time.Duration) *RateLimiterBaseDelayValueWithValidation {
	*p = defaultVal
	return (*RateLimiterBaseDelayValueWithValidation)(p)
}

type RateLimiterMaxDelayValueWithValidation time.Duration

func (v *RateLimiterMaxDelayValueWithValidation) String() string {
	return time.Duration(*v).String()
}

func (v *RateLimiterMaxDelayValueWithValidation) Set(s string) error {
	duration, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("failed to parse time duration: %w", err)
	}
	if duration < time.Second || duration > time.Minute*5 {
		return fmt.Errorf("the max delay must be a value between [1s, 5m]")
	}
	*v = RateLimiterMaxDelayValueWithValidation(duration)
	return nil
}

func newRateLimiterMaxDelayValueWithValidation(defaultVal time.Duration, p *time.Duration) *RateLimiterMaxDelayValueWithValidation {
	*p = defaultVal
	return (*RateLimiterMaxDelayValueWithValidation)(p)
}

type RateLimiterQPSValueWithValidation int

func (v *RateLimiterQPSValueWithValidation) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *RateLimiterQPSValueWithValidation) Set(s string) error {
	qps, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse integer: %w", err)
	}
	if qps < 1 || qps > 1000 {
		return fmt.Errorf("the QPS must be a positive integer in the range [1, 1000]")
	}
	*v = RateLimiterQPSValueWithValidation(qps)
	return nil
}

func newRateLimiterQPSValueWithValidation(defaultVal int, p *int) *RateLimiterQPSValueWithValidation {
	*p = defaultVal
	return (*RateLimiterQPSValueWithValidation)(p)
}

type RateLimiterBucketSizeValueWithValidation int

func (v *RateLimiterBucketSizeValueWithValidation) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *RateLimiterBucketSizeValueWithValidation) Set(s string) error {
	bucketSize, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse integer: %w", err)
	}
	if bucketSize < 1 || bucketSize > 10000 {
		return fmt.Errorf("the bucket size must be a positive integer in the range [1, 10000]")
	}
	*v = RateLimiterBucketSizeValueWithValidation(bucketSize)
	return nil
}

func newRateLimiterBucketSizeValueWithValidation(defaultVal int, bucketSize *int) *RateLimiterBucketSizeValueWithValidation {
	*bucketSize = defaultVal
	return (*RateLimiterBucketSizeValueWithValidation)(bucketSize)
}
