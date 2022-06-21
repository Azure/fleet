/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package authtoken

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"go.goms.io/fleet/pkg/interfaces"
)

type MockAuthTokenProvider struct {
	Token interfaces.AuthToken
}

func (m MockAuthTokenProvider) FetchToken(ctx context.Context) (interfaces.AuthToken, error) {
	return m.Token, nil
}

// TestRefreshTokenOnce test to refresh/rewrite token for one time
func TestRefreshTokenOnce(t *testing.T) {
	provider := MockAuthTokenProvider{
		Token: interfaces.AuthToken{
			Token:     "test token",
			ExpiresOn: time.Now(),
		},
	}
	testChan := make(chan bool)

	buf := new(strings.Builder)
	bufferWriter := NewWriter(BufferWriterFactory{buf: buf}.Create)

	createTicker := CreateTickerFuncType(func(duration time.Duration) <-chan time.Time {
		if duration != 0 {
			assert.Equal(t, provider.Token.Token, buf.String(), "TestRefreshTokenOnce")
			testChan <- true
			return nil
		}
		tickChan := make(chan time.Time, 1)
		tickChan <- time.Now()
		return tickChan
	})
	refresher := NewAuthTokenRefresher(provider, bufferWriter, DefaultRefreshDurationFunc, createTicker)
	go func() {
		_ = refresher.RefreshToken(context.TODO())
	}()

	select {
	case <-testChan:
		return
	case <-time.Tick(1 * time.Second):
		assert.Fail(t, "Test timeout", "TestRefreshTokenOnce")
	}
}

//TestRefreshToken test to refresh/rewrite token multiple times
func TestRefreshToken(t *testing.T) {
	provider := MockAuthTokenProvider{
		Token: interfaces.AuthToken{
			Token:     "test token",
			ExpiresOn: time.Now(),
		},
	}
	testChan := make(chan int32)

	buf := new(strings.Builder)
	bufferWriter := NewWriter(BufferWriterFactory{buf: buf}.Create)

	var createTickerCallCount int32
	createTicker := CreateTickerFuncType(func(duration time.Duration) <-chan time.Time {
		if duration != 0 {
			assert.Equal(t, provider.Token.Token, buf.String(), "TestRefreshToken")
			buf.Reset()
		}
		createTickerCallCount++
		testChan <- createTickerCallCount
		returnChan := make(chan time.Time)
		returnChan <- time.Now()
		return returnChan
	})
	refresher := NewAuthTokenRefresher(provider, bufferWriter, DefaultRefreshDurationFunc, createTicker)
	go func() {
		_ = refresher.RefreshToken(context.TODO())
	}()

	select {
	case count := <-testChan:
		if count == 3 {
			return
		}
	case <-time.Tick(1 * time.Second):
		assert.Fail(t, "Test timeout", "TestRefreshToken")
	}
}

//TestRefresherCancelContext test if the func will be canceled/returned once the ctx is canceled
func TestRefresherCancelContext(t *testing.T) {
	provider := MockAuthTokenProvider{
		Token: interfaces.AuthToken{
			Token:     "test token",
			ExpiresOn: time.Now(),
		},
	}
	testChan := make(chan bool)
	ctx, cancel := context.WithCancel(context.TODO())

	bufferWriter := NewWriter(BufferWriterFactory{new(strings.Builder)}.Create)

	refresher := NewAuthTokenRefresher(provider, bufferWriter, DefaultRefreshDurationFunc, DefaultCreateTicker)
	go func() {
		_ = refresher.RefreshToken(ctx)
		testChan <- true
	}()

	cancel()

	select {
	case <-testChan:
		return
	case <-time.Tick(1 * time.Second):
		assert.Fail(t, "Test timeout", "TestRefresherCancelContext")
	}
}
