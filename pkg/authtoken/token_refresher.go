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

package authtoken

import (
	"context"
	"time"

	"k8s.io/klog/v2"
)

type RefreshDurationFuncType func(token AuthToken) time.Duration
type CreateTickerFuncType func(time.Duration) <-chan time.Time

type Refresher struct {
	provider         Provider
	writer           Writer
	refreshCalculate RefreshDurationFuncType
	createTicker     CreateTickerFuncType
}

func NewAuthTokenRefresher(tokenProvider Provider,
	writer Writer,
	refreshCalculate RefreshDurationFuncType,
	createTicker CreateTickerFuncType) *Refresher {
	return &Refresher{
		provider:         tokenProvider,
		writer:           writer,
		refreshCalculate: refreshCalculate,
		createTicker:     createTicker,
	}
}

var (
	DefaultRefreshDurationFunc = func(token AuthToken) time.Duration {
		return time.Until(token.ExpiresOn) / 2
	}
	DefaultCreateTicker    = time.Tick
	DefaultRefreshDuration = time.Second * 30
)

func (at *Refresher) callFetchToken(ctx context.Context) (AuthToken, error) {
	klog.V(2).InfoS("FetchToken start")
	deadline := time.Now().Add(DefaultRefreshDuration)
	fetchTokenContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	return at.provider.FetchToken(fetchTokenContext)
}

func (at *Refresher) RefreshToken(ctx context.Context) error {
	klog.V(2).InfoS("RefreshToken start")
	refreshDuration := DefaultRefreshDuration

	for ; ; <-at.createTicker(refreshDuration) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			token, err := at.callFetchToken(ctx)
			if err != nil {
				klog.ErrorS(err, "Failed to FetchToken")
				continue
			}

			klog.V(2).InfoS("WriteToken start")
			err = at.writer.WriteToken(token)
			if err != nil {
				klog.ErrorS(err, "Failed to WriteToken")
				continue
			}
			refreshDuration = at.refreshCalculate(token)
			klog.V(2).InfoS("RefreshToken succeeded")
		}
	}
}
