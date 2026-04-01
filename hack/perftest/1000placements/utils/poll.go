package utils

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func (r *Runner) LongPollPlacements(ctx context.Context) {
	wg := sync.WaitGroup{}

	// Start waiting for all placements to be available.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			c1 := r.longPollingPlacementsCount.Load()
			c2 := r.completedPlacementCount.Load()
			if c2 >= c1 {
				fmt.Println("all CRPs are now available")
				close(r.toLongPollPlacementsChan)
				return
			}

			fmt.Printf("waiting for %d CRPs to be available, %d are available\n", c1, c2)
			time.Sleep(time.Second * 5)
		}
	}()

	// Run the long pollers.
	for i := 0; i < r.longPollingWorkerCount; i++ {
		wg.Add(1)

		go func(pollerIdx int) {
			defer wg.Done()

			for {
				// Read from the channel.
				var resIdx int
				var readOK bool
				select {
				case resIdx, readOK = <-r.toLongPollPlacementsChan:
					if !readOK {
						fmt.Printf("long poller %d exits\n", pollerIdx)
						return
					}
				case <-ctx.Done():
					return
				}

				// Read the CRP.
				var crp placementv1beta1.ClusterResourcePlacement
				if err := r.hubClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementNameFmt, resIdx)}, &crp); err != nil {
					fmt.Printf("poller %d: failed to get CRP %s: %v\n", pollerIdx, fmt.Sprintf(placementNameFmt, resIdx), err)
					// Requeue this CRP.
					time.Sleep(r.longPollingCoolDownPeriod)
					r.toLongPollPlacementsChan <- resIdx
					continue
				}

				// Check the status of the CRP.
				availableCond := meta.FindStatusCondition(crp.Status.Conditions, string(placementv1beta1.ClusterResourcePlacementAvailableConditionType))
				if availableCond == nil || availableCond.Status != metav1.ConditionTrue || availableCond.ObservedGeneration != crp.Generation {
					fmt.Printf("poller %d: CRP %s is not available yet\n", pollerIdx, fmt.Sprintf(placementNameFmt, resIdx))
					// Requeue this CRP.
					time.Sleep(r.longPollingCoolDownPeriod)
					r.toLongPollPlacementsChan <- resIdx
				} else {
					// The CRP is available.
					fmt.Printf("poller %d: CRP %s is available\n", pollerIdx, fmt.Sprintf(placementNameFmt, resIdx))
					createdTimestamp := crp.GetCreationTimestamp()
					availablityLatency := availableCond.LastTransitionTime.Time.Sub(createdTimestamp.Time)
					r.toTrackLatencyChan <- latencyTrackAttempt{
						latency: availablityLatency,
						resIdx:  resIdx,
					}

					r.completedPlacementCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	fmt.Println("All long pollers have completed")
	close(r.toTrackLatencyChan)
}
