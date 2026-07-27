package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountShareRuntimeLease(t *testing.T) {
	newSlot := func(name string, ttl time.Duration, refresh func(context.Context) (bool, error), releaseOrder *[]string) *AcquireResult {
		return &AcquireResult{
			Acquired:    true,
			RefreshFunc: refresh,
			LeaseTTL:    ttl,
			ReleaseFunc: func() {
				if releaseOrder != nil {
					*releaseOrder = append(*releaseOrder, name)
				}
			},
		}
	}

	t.Run("missing refresh metadata fails closed", func(t *testing.T) {
		lease, err := NewAccountShareRuntimeLease(context.Background(),
			&AcquireResult{Acquired: true, ReleaseFunc: func() {}},
			&AcquireResult{Acquired: true, ReleaseFunc: func() {}},
		)
		require.ErrorIs(t, err, ErrAccountShareRuntimeLeaseUnavailable)
		require.Nil(t, lease)
	})

	t.Run("selection failure releases both acquired slots in reverse acquisition order", func(t *testing.T) {
		var releaseOrder []string
		selection, err := newAccountShareModeRuntimeSelection(
			context.Background(),
			&Account{ID: 1},
			&AcquireResult{Acquired: true, ReleaseFunc: func() { releaseOrder = append(releaseOrder, "account") }},
			&AcquireResult{Acquired: true, ReleaseFunc: func() { releaseOrder = append(releaseOrder, "membership") }},
		)
		require.ErrorIs(t, err, ErrAccountShareRuntimeLeaseUnavailable)
		require.Nil(t, selection)
		require.Equal(t, []string{"account", "membership"}, releaseOrder)
	})

	t.Run("client cancellation does not release and release order is stable", func(t *testing.T) {
		var releaseOrder []string
		clientCtx, cancelClient := context.WithCancel(context.Background())
		lease, err := NewAccountShareRuntimeLease(
			clientCtx,
			newSlot("account", time.Hour, func(context.Context) (bool, error) { return true, nil }, &releaseOrder),
			newSlot("membership", time.Hour, func(context.Context) (bool, error) { return true, nil }, &releaseOrder),
		)
		require.NoError(t, err)

		cancelClient()
		select {
		case <-lease.Context().Done():
			t.Fatal("client cancellation must not end the detached runtime lease")
		case <-time.After(20 * time.Millisecond):
		}
		require.Empty(t, releaseOrder)

		lease.Release()
		lease.Release()
		require.Equal(t, []string{"account", "membership"}, releaseOrder)
	})

	t.Run("missing slot ownership cancels immediately", func(t *testing.T) {
		lease, err := NewAccountShareRuntimeLease(
			context.Background(),
			newSlot("account", 30*time.Millisecond, func(context.Context) (bool, error) { return false, nil }, nil),
			newSlot("membership", 30*time.Millisecond, func(context.Context) (bool, error) { return true, nil }, nil),
		)
		require.NoError(t, err)
		defer lease.Release()

		select {
		case <-lease.Context().Done():
			require.ErrorIs(t, context.Cause(lease.Context()), ErrAccountShareRuntimeLeaseLost)
		case <-time.After(time.Second):
			t.Fatal("missing distributed slot did not cancel the runtime lease")
		}
	})

	t.Run("transient cache errors are tolerated for one ttl", func(t *testing.T) {
		now := time.Now()
		lease := &AccountShareRuntimeLease{
			accountSlot: accountShareRuntimeLeaseSlot{
				name:            "account",
				refresh:         func(context.Context) (bool, error) { return false, errors.New("redis unavailable") },
				ttl:             time.Minute,
				lastConfirmedAt: now,
			},
			membershipSlot: accountShareRuntimeLeaseSlot{
				name:            "membership",
				refresh:         func(context.Context) (bool, error) { return true, nil },
				ttl:             time.Minute,
				lastConfirmedAt: now,
			},
		}
		require.False(t, lease.refreshAt(now.Add(30*time.Second)))
		require.True(t, lease.refreshAt(now.Add(time.Minute)))
	})
}

func TestDetachAccountShareRuntimeLeaseContext(t *testing.T) {
	slot := func() *AcquireResult {
		return &AcquireResult{
			Acquired:    true,
			ReleaseFunc: func() {},
			RefreshFunc: func(context.Context) (bool, error) { return true, nil },
			LeaseTTL:    time.Hour,
		}
	}
	lease, err := NewAccountShareRuntimeLease(context.Background(), slot(), slot())
	require.NoError(t, err)
	defer lease.Release()

	clientCtx, cancelClient := context.WithCancel(context.Background())
	boundCtx, cancelBound := BindAccountShareRuntimeLeaseContext(clientCtx, lease)
	defer cancelBound()
	detachedCtx, cancelDetached := DetachAccountShareRuntimeLeaseContext(boundCtx)
	defer cancelDetached()

	cancelClient()
	require.Eventually(t, func() bool { return boundCtx.Err() != nil }, time.Second, time.Millisecond)
	select {
	case <-detachedCtx.Done():
		t.Fatal("detached drain must ignore pure client cancellation")
	case <-time.After(20 * time.Millisecond):
	}

	lease.cancel(ErrAccountShareRuntimeLeaseLost)
	require.Eventually(t, func() bool {
		return errors.Is(context.Cause(detachedCtx), ErrAccountShareRuntimeLeaseLost)
	}, time.Second, time.Millisecond)
}
