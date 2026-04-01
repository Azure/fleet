package utils

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func (r *Runner) CreatePlacements(ctx context.Context) {
	wg := sync.WaitGroup{}

	// Run the producer.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < r.maxCRPToCreateCount; i++ {
			select {
			case r.toCreatePlacementsChan <- i:
			case <-ctx.Done():
				close(r.toCreatePlacementsChan)
				return
			}
		}

		close(r.toCreatePlacementsChan)
	}()

	// Run the workers to create the placements.
	for i := 0; i < r.resourceSetupWorkerCount; i++ {
		wg.Add(1)

		go func(workerIdx int) {
			defer wg.Done()

			for {
				// Read from the channel.
				var resIdx int
				var readOk bool
				select {
				case resIdx, readOk = <-r.toCreatePlacementsChan:
					if !readOk {
						fmt.Printf("worker %d exits\n", workerIdx)
						return
					}
				case <-ctx.Done():
					return
				}

				// Create the CRP.
				crp := placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(placementNameFmt, resIdx),
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
							Affinity: &placementv1beta1.Affinity{
								ClusterAffinity: &placementv1beta1.ClusterAffinity{
									RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
										ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
											{
												LabelSelector: &metav1.LabelSelector{
													MatchLabels: map[string]string{
														placementGroupLabelKey: fmt.Sprintf("%d", resIdx%10),
													},
												},
											},
										},
									},
								},
							},
						},
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "",
								Kind:    "Namespace",
								Version: "v1",
								Name:    fmt.Sprintf(nsNameFmt, resIdx),
							},
						},
						Strategy: placementv1beta1.RolloutStrategy{
							Type: placementv1beta1.RollingUpdateRolloutStrategyType,
							RollingUpdate: &placementv1beta1.RollingUpdateConfig{
								MaxUnavailable:           ptr.To(intstr.Parse("100%")),
								UnavailablePeriodSeconds: ptr.To(1),
							},
						},
					},
				}
				errAfterRetries := retry.OnError(r.retryOpsBackoff, func(err error) bool {
					return err != nil && !errors.IsAlreadyExists(err)
				}, func() error {
					return r.hubClient.Create(ctx, &crp)
				})
				if errAfterRetries != nil && !errors.IsAlreadyExists(errAfterRetries) {
					fmt.Printf("worker %d: failed to create CRP %s after retries: %v\n", workerIdx, crp.Name, errAfterRetries)
					continue
				}

				// Set the CRP to the long pollers.
				r.toLongPollPlacementsChan <- resIdx
				r.longPollingPlacementsCount.Add(1)

				// Dump the memory profile if needed.
				if _, ok := r.triggerPtsForMemProfileDumping[resIdx]; ok {
					fmt.Printf("worker %d: retrieving pprof data after CRP %d is created\n", workerIdx, resIdx)
					r.RetrievePProfProfile(resIdx)
				}
				// Cool down.
				time.Sleep(r.resourceCreationCoolDownPeriod)
			}
		}(i)
	}
	wg.Wait()
}
