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
	DefaultCreateTicker = time.Tick
)

func (at *Refresher) RefreshToken(ctx context.Context) error {
	klog.InfoS("start refresh token")
	var refreshDuration time.Duration

	for ; ; <-at.createTicker(refreshDuration) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			token, err := at.provider.FetchToken(ctx)
			if err != nil {
				return err
			}
			err = at.writer.WriteToken(token)
			if err != nil {
				return err
			}
			refreshDuration = at.refreshCalculate(token)
			klog.InfoS("token has been refreshed")
		}
	}
}
