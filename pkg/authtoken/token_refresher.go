/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package authtoken

import (
	"context"
	"time"

	"k8s.io/klog/v2"

	"go.goms.io/fleet/pkg/interfaces"
)

type RefreshDurationFuncType func(token interfaces.AuthToken) time.Duration
type CreateTickerFuncType func(time.Duration) <-chan time.Time

type Refresher struct {
	provider         interfaces.AuthTokenProvider
	writer           interfaces.AuthTokenWriter
	refreshCalculate RefreshDurationFuncType
	createTicker     CreateTickerFuncType
}

func NewAuthTokenRefresher(tokenProvider interfaces.AuthTokenProvider,
	writer interfaces.AuthTokenWriter,
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
	DefaultRefreshDurationFunc = func(token interfaces.AuthToken) time.Duration {
		return time.Until(token.ExpiresOn) / 2
	}
	DefaultCreateTicker    = time.Tick
	DefaultRefreshDuration = time.Second * 30
)

func (at *Refresher) callFetchToken(ctx context.Context) (interfaces.AuthToken, error) {
	klog.V(5).InfoS("FetchToken start")
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

			klog.V(5).InfoS("WriteToken start")
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
