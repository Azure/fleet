package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/kubefleet-dev/kubefleet/hack/perftest/1000placements/utils"
)

const (
	resourceSetupWorkerCount       = 15
	longPollingWorkerCount         = 15
	resourceCreationCoolDownPeriod = time.Second * 1
	longPollingCoolDownPeriod      = time.Second * 2
	betweenStageCoolDownPeriod     = time.Second * 30

	maxCRPToCreateCount = 1000

	configMapDataByteCount = 1024 // 1 KB.
)

var (
	// Add trigger points for dumping pprof profiles when a specific # of placement
	// have been created.
	triggerPtsForMemProfileDumping = map[int]bool{}
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

	runName := os.Getenv("RUN_NAME")
	if len(runName) == 0 {
		panic("RUN_NAME environment variable is not set")
	}

	retrievePprofData := false
	if s := os.Getenv("RETRIEVE_PPROF_DATA"); len(s) != 0 {
		retrievePprofData = true
	}
	var pProfEndpoint string
	if retrievePprofData {
		if pProfEndpoint = os.Getenv("PPROF_ENDPOINT"); len(pProfEndpoint) == 0 {
			panic("PPROF_ENDPOINT environment variable is not set")
		}
	}

	runner := utils.New(
		runName,
		resourceSetupWorkerCount,
		longPollingWorkerCount,
		betweenStageCoolDownPeriod,
		resourceCreationCoolDownPeriod,
		longPollingCoolDownPeriod,
		configMapDataByteCount,
		maxCRPToCreateCount,
		triggerPtsForMemProfileDumping,
		pProfEndpoint,
		retryOpsBackoff,
	)

	if doCleanUp {
		runner.CleanUp(ctx)
		return
	}

	fmt.Println("Preparing the resources...")
	runner.CreateResources(ctx)

	// Cool down.
	fmt.Println("Cooling down...")
	runner.CoolDown()

	fmt.Println("Creating the placements...")
	runner.CreatePlacements(ctx)

	// Cool down.
	fmt.Println("Cooling down...")
	runner.CoolDown()

	fmt.Println("Long polling the placements...")
	runner.LongPollPlacements(ctx)

	fmt.Println("Tracking latency")
	runner.TrackLatency(ctx)

	if retrievePprofData {
		fmt.Println("retrieving final pprof data")
		runner.RetrievePProfProfile(maxCRPToCreateCount + 1)
	}

	// Tally the latency quantiles.
	fmt.Println("Tallying latency quantiles...")
	runner.TallyLatencyQuantiles()

	fmt.Println("All placements have been completed, exiting.")
}
