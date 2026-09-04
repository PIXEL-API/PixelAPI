//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type registerSessionResultCache struct {
	SessionLimitCache

	allowed bool
	err     error
	calls   int
}

func (c *registerSessionResultCache) RegisterSession(
	context.Context,
	int64,
	string,
	int,
	time.Duration,
) (bool, error) {
	c.calls++
	return c.allowed, c.err
}

func TestGatewayServiceRegisterAcquiredAccountSessionReleasesOnRegistrationFailure(t *testing.T) {
	account := &Account{
		ID:       71,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"max_sessions": 1,
		},
	}

	t.Run("capacity rejection is retryable and releases exactly once", func(t *testing.T) {
		cache := &registerSessionResultCache{allowed: false}
		gateway := &GatewayService{sessionLimitCache: cache}
		releaseCalls := 0

		allowed, err := gateway.registerAcquiredAccountSession(
			context.Background(),
			account,
			"new-session",
			func() { releaseCalls++ },
		)

		require.NoError(t, err)
		require.False(t, allowed)
		require.Equal(t, 1, cache.calls)
		require.Equal(t, 1, releaseCalls)
	})

	t.Run("infrastructure error is preserved and releases exactly once", func(t *testing.T) {
		infrastructureErr := errors.New("redis unavailable")
		cache := &registerSessionResultCache{err: infrastructureErr}
		gateway := &GatewayService{sessionLimitCache: cache}
		releaseCalls := 0

		allowed, err := gateway.registerAcquiredAccountSession(
			context.Background(),
			account,
			"new-session",
			func() { releaseCalls++ },
		)

		require.False(t, allowed)
		require.ErrorIs(t, err, infrastructureErr)
		require.NotErrorIs(t, err, ErrAccountSessionLimitExceeded)
		require.Equal(t, 1, cache.calls)
		require.Equal(t, 1, releaseCalls)
	})
}
