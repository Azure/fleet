package utils

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
)

func (r *Runner) CreateResources(ctx context.Context) {
	wg := sync.WaitGroup{}

	// Run the producer.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < r.maxCRPToCreateCount; i++ {
			select {
			case r.toCreateResourcesChan <- i:
			case <-ctx.Done():
				close(r.toCreateResourcesChan)
				return
			}
		}

		close(r.toCreateResourcesChan)
	}()

	// Run the workers to create namespaces and configMaps.
	for i := 0; i < r.resourceSetupWorkerCount; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()

			for {
				// Read from the channel.
				var resIdx int
				var readOk bool
				select {
				case resIdx, readOk = <-r.toCreateResourcesChan:
					if !readOk {
						fmt.Printf("worker %d exits\n", workerIdx)
						return
					}
				case <-ctx.Done():
					return
				}

				// Create the namespace.
				namespace := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(nsNameFmt, resIdx),
					},
				}
				errAfterRetries := retry.OnError(r.retryOpsBackoff, func(err error) bool {
					return err != nil && !errors.IsAlreadyExists(err)
				}, func() error {
					return r.hubClient.Create(ctx, &namespace)
				})
				if errAfterRetries != nil && !errors.IsAlreadyExists(errAfterRetries) {
					fmt.Printf("worker %d: failed to create namespace %s after retries: %v\n", workerIdx, fmt.Sprintf(nsNameFmt, resIdx), errAfterRetries)
					continue
				}

				// Create a configMap in the namespace.
				fooValBytes := make([]byte, r.configMapDataByteCount)
				_, err := rand.Read(fooValBytes)
				if err != nil {
					// This should never run; Go documents that rand.Read will never fail unless it is run on legacy
					// Linux platforms.
					panic(fmt.Sprintf("failed to generate random bytes for configMap data: %v", err))
				}
				fooValStr := base64.StdEncoding.EncodeToString(fooValBytes)
				configMap := corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(configMapNameFmt, resIdx),
						Namespace: fmt.Sprintf(nsNameFmt, resIdx),
					},
					Data: map[string]string{
						"foo": fooValStr,
					},
				}
				errAfterRetries = retry.OnError(r.retryOpsBackoff, func(err error) bool {
					return err != nil && !errors.IsAlreadyExists(err)
				}, func() error {
					return r.hubClient.Create(ctx, &configMap)
				})
				if errAfterRetries != nil && !errors.IsAlreadyExists(errAfterRetries) {
					fmt.Printf("worker %d: failed to create configMap %s in namespace %s after retries: %v\n", workerIdx, fmt.Sprintf(configMapNameFmt, resIdx), fmt.Sprintf(nsNameFmt, resIdx), errAfterRetries)
					continue
				}

				// Create a deployment in the namespace.
				deployment := appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(deployNameFmt, resIdx),
						Namespace: fmt.Sprintf(nsNameFmt, resIdx),
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "pause",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": "pause",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "pause",
										Image: "registry.k8s.io/pause:3.9",
									},
								},
								TerminationGracePeriodSeconds: ptr.To(int64(60)),
							},
						},
					},
				}
				errAfterRetries = retry.OnError(r.retryOpsBackoff, func(err error) bool {
					return err != nil && !errors.IsAlreadyExists(err)
				}, func() error {
					return r.hubClient.Create(ctx, &deployment)
				})
				if errAfterRetries != nil && !errors.IsAlreadyExists(errAfterRetries) {
					fmt.Printf("worker %d: failed to create deployment %s in namespace %s after retries: %v\n", workerIdx, fmt.Sprintf(deployNameFmt, resIdx), fmt.Sprintf(nsNameFmt, resIdx), errAfterRetries)
					continue
				}

				fmt.Printf("worker %d: created namespace %s, configMap %s, and deployment %s\n", workerIdx, fmt.Sprintf(nsNameFmt, resIdx), fmt.Sprintf(configMapNameFmt, resIdx), fmt.Sprintf(deployNameFmt, resIdx))
				time.Sleep(r.resourceCreationCoolDownPeriod)
			}
		}(i)
	}
	wg.Wait()
}
