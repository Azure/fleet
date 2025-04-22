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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type MockAuthTokenProvider struct {
	Token AuthToken
}

func (m MockAuthTokenProvider) FetchToken(_ context.Context) (AuthToken, error) {
	return m.Token, nil
}

// TestRefreshTokenOnce test to refresh/rewrite token for one time
func TestRefreshTokenOnce(t *testing.T) {
	provider := MockAuthTokenProvider{
		Token: AuthToken{
			Token:     "test token",
			ExpiresOn: time.Now(),
		},
	}
	testChan := make(chan bool)

	factory := NewBufferWriterFactory()
	bufferWriter := NewWriter(factory.Create)

	createTicker := CreateTickerFuncType(func(duration time.Duration) <-chan time.Time {
		assert.Equal(t, provider.Token.Token, factory.buf.String(), "TestRefreshTokenOnce")
		testChan <- true
		return nil
	})

	refresher := NewAuthTokenRefresher(provider, bufferWriter, DefaultRefreshDurationFunc, createTicker)
	go func() {
		_ = refresher.RefreshToken(context.TODO())
	}()

	select {
	case <-testChan:
		assert.Equal(t, 1, factory.writeCount, "TestRefreshTokenOnce")
		return
	case <-time.Tick(1 * time.Second):
		assert.Fail(t, "Test timeout", "TestRefreshTokenOnce")
	}
}

// TestRefreshToken test to refresh/rewrite token multiple times
func TestRefreshToken(t *testing.T) {
	provider := MockAuthTokenProvider{
		Token: AuthToken{
			Token:     "test token",
			ExpiresOn: time.Now(),
		},
	}

	testChan := make(chan bool)
	factory := NewBufferWriterFactory()
	bufferWriter := NewWriter(factory.Create)

	createTicker := CreateTickerFuncType(func(duration time.Duration) <-chan time.Time {
		assert.Equal(t, provider.Token.Token, factory.buf.String(), "TestRefreshToken")
		factory.buf.Reset()

		if factory.writeCount == 2 {
			testChan <- true
			return nil
		}
		returnChan := make(chan time.Time, 1)
		returnChan <- time.Now()
		return returnChan
	})
	refresher := NewAuthTokenRefresher(provider, bufferWriter, DefaultRefreshDurationFunc, createTicker)
	go func() {
		_ = refresher.RefreshToken(context.TODO())
	}()

	select {
	case <-testChan:
		return
	case <-time.Tick(1 * time.Second):
		assert.Fail(t, "Test timeout", "TestRefreshToken")
		return
	}
}

// TestRefresherCancelContext test if the func will be canceled/returned once the ctx is canceled
func TestRefresherCancelContext(t *testing.T) {
	provider := MockAuthTokenProvider{
		Token: AuthToken{
			Token:     "test token",
			ExpiresOn: time.Now().Add(100 * time.Millisecond),
		},
	}
	testChan := make(chan error)
	ctx, cancel := context.WithCancel(context.TODO())

	bufferWriter := NewWriter(NewBufferWriterFactory().Create)

	refresher := NewAuthTokenRefresher(provider, bufferWriter, DefaultRefreshDurationFunc, DefaultCreateTicker)
	go func() {
		testChan <- refresher.RefreshToken(ctx)
	}()

	cancel()

	expectedErr := context.Canceled
	select {
	case err := <-testChan:
		if errors.Is(err, expectedErr) {
			return
		}
		assert.Fail(t, fmt.Sprintf("got error: \"%s\", expected error: \"%s\"", err.Error(), expectedErr))
	case <-time.Tick(1 * time.Second):
		assert.Fail(t, "Test timeout", "TestRefresherCancelContext")
	}
}
