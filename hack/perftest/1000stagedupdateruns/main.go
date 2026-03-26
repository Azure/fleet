package main

import (
	"context"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/kubefleet-dev/kubefleet/hack/perftest/1000stagedupdateruns/utils"
)

const (
	// The number of workers to create/delete resources concurrently.
	resourceSetupWorkerCount = 15
	// The number of workers to long poll staged update runs concurrently.
	longPollingWorkerCount = 15
	// The cool down period between two stages (e.g., creating resources and long polling them).
	betweenStageCoolDownPeriod = time.Second * 30
	// The cool down period between two polls.
	longPollingCoolDownPeriod = time.Second * 45

	// The max concurrency per stage setting for each staged update run.
	maxConcurrencyPerStage = "50%"
	// The number of placements (and thus the number of staged update runs) to create.
	maxCRPToUpdateCount = 1000
)

var (
	retryOpsBackoff = wait.Backoff{
		Steps:    4,
		Duration: 4 * time.Second,
		Factor:   2.0,
		Jitter:   0.1,
	}
)

func main() {
	ctx := context.Background()

	// Read the arguments.
	doCleanUp := false
	cleanUpFlag := os.Getenv("CLEANUP")
	if len(cleanUpFlag) != 0 {
		doCleanUp = true
	}

	runner := utils.New(
		resourceSetupWorkerCount,
		longPollingWorkerCount,
		betweenStageCoolDownPeriod,
		longPollingCoolDownPeriod,
		maxConcurrencyPerStage,
		maxCRPToUpdateCount,
		retryOpsBackoff,
	)

	if doCleanUp {
		runner.CleanUp(ctx)
		return
	}

	// Prepare the staged update run strategy.
	println("Preparing the staged update run strategy...")
	runner.CreateStagedUpdateRunStrategy(ctx)

	// Patch existing resources.
	println("Updating existing resources...")
	runner.UpdateResources(ctx)

	// Cool down.
	println("Cooling down...")
	runner.CoolDown()

	// Create the staged update runs.
	println("Creating staged update runs...")
	runner.CreateStagedUpdateRuns(ctx)

	// Cool down.
	println("Cooling down...")
	runner.CoolDown()

	// Long poll the staged update runs.
	println("Long polling staged update runs...")
	runner.LongPollStagedUpdateRuns(ctx)

	// Track the latency.
	println("Tracking latency...")
	runner.TrackLatency(ctx)

	// Tally the latency quantiles.
	println("Tallying latency quantiles...")
	runner.TallyLatencyQuantiles()

	println("All staged update runs have been completed, exiting.")
}
