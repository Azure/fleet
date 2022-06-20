package authtoken

import (
	"context"
	"time"

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
	refreshDuration := 0 * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-at.createTicker(refreshDuration):
			token, err := at.provider.FetchToken(ctx)
			if err != nil {
				return err
			}
			err = at.writer.WriteToken(token)
			if err != nil {
				return err
			}

			refreshDuration = at.refreshCalculate(token)
		}
	}
}
