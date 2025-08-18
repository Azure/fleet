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
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resource"
)

const (
	maxAttemptsWithFixedDelay         = 40
	minAttemptsWithFixedDelay         = 1
	minFixedDelaySeconds              = 2
	minInitialSlowBackoffDelaySeconds = 2
	maxMaxFastBackoffDelaySeconds     = 3600 // 1 hour

	// Set caps on the bases to make sure that delay would be monotonically growing and
	// no overflowing would occur.
	minExponentialBaseForSlowBackoff = 1.05
	maxExponentialBaseForFastBackoff = 100.0

	// With the current set of extremums (maxAttemptsWithFixDelay = 40,
	// minInitialSlowBackoffDelaySeconds = 2, maxSlowBackoffDelaySeconds = 3600,
	// and minExponentialBaseForSlowBackoff = 1.05),
	// the number of requeues needed for the requeue delay to reach to the maximum value
	// would be 193. The rate limiter has no need to track number of requeues beyond
	// this value.
	maxNumOfRequeuesToTrack = 200
)

const (
	defaultInitialSlowBackoffDelaySeconds = 2
	defaultBaseForExponentialBackoff      = 1.2
	defaultMaxSlowBackoffDelaySeconds     = 30
	defaultMaxFastBackoffDelaySeconds     = 900 // 15 minutes
)

const (
	processingResultStrTpl = "%s,%s"
)

// RequeueMultiStageWithExponentialBackoffRateLimiter is a rate limiter that allows requeues of various
// delays in multiple stages; it helps to control the rate of requeues for Work objects while keeping
// the system as responsive as possible.
//
// It will:
//   - first, requeue with a fixed delay for a limited number of attempts;
//     this is a stage where the Work objects are expected to be unavailable (e.g., it might be known
//     already that an application takes a while to start up).
//   - then, switch to exponential backoff at a slow rate if the processing results stay consistent,
//     until the delay period reaches a cap;
//     this is a stage where the Work objects are expected to become available, and the system will
//     requeue (relatively) fast for a more responsive experience.
//   - at last, switch to exponential backoff at a fast rate with a cap, if the processing results
//     remain consistent;
//     this is a stage where the system no longer expects the Work objects to have a status change
//     and chooses to requeue (relatively) slowly to avoid overwhelming the system.
//
// For Work objects that are known to be available or have their diffs reported already, the rate
// limiter can be set skip to the fast backoff stage to save unnecessary fast requeues.
//
// Note that the implementation distinguishes between Work objects of different generations and
// processing results, so that Work object spec change and/or processing result change
// will reset the requeue counter.
//
// TO-DO (chenyu1): the current implementation tracks processing results as strings, which incurs
// additional overhead when doing comparison (albeit small); evaluate if we need to switch to a more
// performant representation.
type RequeueMultiStageWithExponentialBackoffRateLimiter struct {
	mu                              sync.Mutex
	requeueCounter                  map[types.NamespacedName]int
	lastRequeueDelayTracker         map[types.NamespacedName]time.Duration
	lastTrackedGeneration           map[types.NamespacedName]int64
	lastTrackedProcessingResultHash map[types.NamespacedName]string

	attemptsWithFixedDelay int
	fixedDelay             time.Duration

	exponentialBaseForSlowBackoff float64
	initialSlowBackoffDelay       time.Duration
	maxSlowBackoffDelay           time.Duration

	exponentialBaseForFastBackoff float64
	maxFastBackoffDelay           time.Duration

	skipToFastBackoffForAvailableOrDiffReportedWorkObjs bool
}

// NewRequeueMultiStageWithExponentialBackoffRateLimiter creates a RequeueMultiStageWithExponentialBackoffRateLimiter.
func NewRequeueMultiStageWithExponentialBackoffRateLimiter(
	attemptsWithFixedDelay int,
	fixedDelaySeconds float64,
	exponentialBaseForSlowBackoff float64,
	initialSlowBackoffDelaySeconds float64,
	maxSlowBackoffDelaySeconds float64,
	exponentialBaseForFastBackoff float64,
	maxFastBackoffDelaySeconds float64,
	skipToFastBackoffForAvailableOrDiffReportedWorkObjs bool,
) *RequeueMultiStageWithExponentialBackoffRateLimiter {
	// Validate the input parameters and reset values as necessary.
	if !(attemptsWithFixedDelay >= minAttemptsWithFixedDelay &&
		attemptsWithFixedDelay <= maxAttemptsWithFixedDelay) {
		// Reset the value.
		attemptsWithFixedDelay = minAttemptsWithFixedDelay
		klog.Errorf("attemptsWithFixedDelay is not set to a valid value; set it to the minimum allowed value (%d) instead", minAttemptsWithFixedDelay)
	}
	if fixedDelaySeconds < minFixedDelaySeconds {
		// Reset the value.
		fixedDelaySeconds = minFixedDelaySeconds
		klog.Errorf("fixedDelaySeconds is below the minimum allowed value (%d seconds); set it to the minimum allowed value instead", minFixedDelaySeconds)
	}

	if !(exponentialBaseForSlowBackoff >= minExponentialBaseForSlowBackoff &&
		exponentialBaseForSlowBackoff <= exponentialBaseForFastBackoff &&
		exponentialBaseForFastBackoff <= maxExponentialBaseForFastBackoff) {
		// Reset the values.
		exponentialBaseForSlowBackoff = defaultBaseForExponentialBackoff
		exponentialBaseForFastBackoff = defaultBaseForExponentialBackoff
		klog.Errorf(
			"exponentialBaseForSlowBackoff (%f) and exponentialBaseForFastBackoff (%f) are not set to valid values; using the default values instead (%f and %f)",
			exponentialBaseForSlowBackoff, exponentialBaseForFastBackoff,
			minExponentialBaseForSlowBackoff, maxExponentialBaseForFastBackoff)
	}

	if !(initialSlowBackoffDelaySeconds >= minInitialSlowBackoffDelaySeconds &&
		initialSlowBackoffDelaySeconds <= maxSlowBackoffDelaySeconds &&
		maxSlowBackoffDelaySeconds <= maxFastBackoffDelaySeconds &&
		maxFastBackoffDelaySeconds <= maxMaxFastBackoffDelaySeconds) {
		// Reset the values.
		initialSlowBackoffDelaySeconds = defaultInitialSlowBackoffDelaySeconds
		maxSlowBackoffDelaySeconds = defaultMaxSlowBackoffDelaySeconds
		maxFastBackoffDelaySeconds = defaultMaxFastBackoffDelaySeconds
		klog.Errorf(
			"initialSlowBackoffDelaySeconds (%f), maxSlowBackoffDelaySeconds (%f), and maxFastBackoffDelaySeconds (%f) are not set to valid values; using the default values instead (%d seconds, %d seconds, and %d seconds)",
			initialSlowBackoffDelaySeconds, maxSlowBackoffDelaySeconds, maxFastBackoffDelaySeconds,
			defaultInitialSlowBackoffDelaySeconds, defaultMaxSlowBackoffDelaySeconds, defaultMaxFastBackoffDelaySeconds)
	}

	return &RequeueMultiStageWithExponentialBackoffRateLimiter{
		requeueCounter:                                      make(map[types.NamespacedName]int),
		lastRequeueDelayTracker:                             make(map[types.NamespacedName]time.Duration),
		lastTrackedGeneration:                               make(map[types.NamespacedName]int64),
		lastTrackedProcessingResultHash:                     make(map[types.NamespacedName]string),
		attemptsWithFixedDelay:                              attemptsWithFixedDelay,
		fixedDelay:                                          time.Duration(fixedDelaySeconds * float64(time.Second)),
		exponentialBaseForSlowBackoff:                       exponentialBaseForSlowBackoff,
		initialSlowBackoffDelay:                             time.Duration(initialSlowBackoffDelaySeconds * float64(time.Second)),
		maxSlowBackoffDelay:                                 time.Duration(maxSlowBackoffDelaySeconds * float64(time.Second)),
		exponentialBaseForFastBackoff:                       exponentialBaseForFastBackoff,
		maxFastBackoffDelay:                                 time.Duration(maxFastBackoffDelaySeconds * float64(time.Second)),
		skipToFastBackoffForAvailableOrDiffReportedWorkObjs: skipToFastBackoffForAvailableOrDiffReportedWorkObjs,
	}
}

// When returns the duration to wait before requeuing the item.
//
// The implementation borrows from the workqueue package's various rate limiter implementations.
func (r *RequeueMultiStageWithExponentialBackoffRateLimiter) When(work *fleetv1beta1.Work, bundles []*manifestProcessingBundle) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	namespacedName := types.NamespacedName{
		Namespace: work.Namespace,
		Name:      work.Name,
	}

	// Check if the Work object has been processed before, and check if the rate limiter needs
	// to reset the requeue counter.
	lastTrackedGen := r.lastTrackedGeneration[namespacedName]
	lastTrackedProcessingResHash := r.lastTrackedProcessingResultHash[namespacedName]
	curProcessingResHash, hashErr := computeProcessingResultHash(work, bundles)

	// Reset the requeue counter under the following conditions:
	// * A new generation of the Work object is being processed.
	// * Cannot compute the hash of the processing result; normally this should never occur.
	// * The processing result has changed since last observation.
	if lastTrackedGen != work.Generation ||
		hashErr != nil ||
		lastTrackedProcessingResHash != curProcessingResHash {
		r.requeueCounter[namespacedName] = 0
		r.lastRequeueDelayTracker[namespacedName] = 0
	}

	// Always update the last tracked generation and processing result hash
	// for the Work object, regardless of whether the requeue counter is reset or not.
	r.lastTrackedGeneration[namespacedName] = work.Generation
	r.lastTrackedProcessingResultHash[namespacedName] = curProcessingResHash

	numRequeues := r.requeueCounter[namespacedName]
	lastRequeueDelayWithBackoff := r.lastRequeueDelayTracker[namespacedName]
	var requeueDelay time.Duration

	availableCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
	diffReportedCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeDiffReported)

	switch {
	case numRequeues < r.attemptsWithFixedDelay:
		// Requeue with a fixed delay for the first few attempts.
		requeueDelay = r.fixedDelay
	case numRequeues == r.attemptsWithFixedDelay:
		// Start to requeue with backoff.
		requeueDelay = r.initialSlowBackoffDelay
	case lastRequeueDelayWithBackoff >= r.maxFastBackoffDelay:
		// The requeue delay has reached the cap; no need to backoff anymore.
		requeueDelay = r.maxFastBackoffDelay
	case r.skipToFastBackoffForAvailableOrDiffReportedWorkObjs &&
		(condition.IsConditionStatusTrue(availableCond, work.Generation) ||
			condition.IsConditionStatusTrue(diffReportedCond, work.Generation)):
		// The Work object is already in an available or diff reported state,
		// and the rate limiter is configured to skip to the fast backoff stage under such conditions.
		requeueDelay = time.Duration(float64(lastRequeueDelayWithBackoff) * r.exponentialBaseForFastBackoff)
	case lastRequeueDelayWithBackoff >= r.maxSlowBackoffDelay:
		// The requeue delay has reached the cap for the slow backoff stage;
		// start to fast back off.
		requeueDelay = time.Duration(float64(lastRequeueDelayWithBackoff) * r.exponentialBaseForFastBackoff)
	default:
		// Continue to slow back off.
		requeueDelay = time.Duration(float64(lastRequeueDelayWithBackoff) * r.exponentialBaseForSlowBackoff)
		if requeueDelay > r.maxSlowBackoffDelay {
			// Backing off slowly this time will break the cap for the slow backoff stage;
			// switch to fast back off.
			requeueDelay = time.Duration(float64(lastRequeueDelayWithBackoff) * r.exponentialBaseForFastBackoff)
		}
	}

	// Cap on the requeue delay.
	if requeueDelay > r.maxFastBackoffDelay {
		requeueDelay = r.maxFastBackoffDelay
	}

	// Update the requeue counter and last requeue delay tracker.
	if numRequeues+1 < maxNumOfRequeuesToTrack {
		r.requeueCounter[namespacedName] = numRequeues + 1
	}
	r.lastRequeueDelayTracker[namespacedName] = requeueDelay
	return requeueDelay
}

// Forget untracks a Work object from the rate limiter.
func (r *RequeueMultiStageWithExponentialBackoffRateLimiter) Forget(work *fleetv1beta1.Work) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reset the trackers for the item.
	namespacedName := types.NamespacedName{
		Namespace: work.Namespace,
		Name:      work.Name,
	}
	delete(r.requeueCounter, namespacedName)
	delete(r.lastRequeueDelayTracker, namespacedName)
	delete(r.lastTrackedGeneration, namespacedName)
	delete(r.lastTrackedProcessingResultHash, namespacedName)
}

// computeProcessingResultHash returns the hash of the result of a Work object processing attempt,
// specifically the apply, availability check, and diff reporting results of each manifest.
//
// Note (chenyu1): there exists a corner case where even though the processing result for a specific
// manifest remains unchanged, the cause of the result have changed (e.g., an apply op first failed
// due to an API server error, then it failed again because a webhook denied it). At this moment
// the rate limiter does not distinguish between the two cases.
func computeProcessingResultHash(work *fleetv1beta1.Work, bundles []*manifestProcessingBundle) (string, error) {
	// The order of manifests is stable in a bundle.
	processingResults := make([]string, 0, len(bundles))
	for _, bundle := range bundles {
		processingResults = append(processingResults, fmt.Sprintf(processingResultStrTpl, bundle.applyOrReportDiffResTyp, bundle.availabilityResTyp))
	}

	processingResHash, err := resource.HashOf(processingResults)
	if err != nil {
		wrappedErr := fmt.Errorf("failed to marshal processing results as JSON: %w", err)
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		klog.ErrorS(wrappedErr, "Failed to compute processing result hash", "work", klog.KObj(work))
		return "", wrappedErr
	}
	return processingResHash, nil
}
