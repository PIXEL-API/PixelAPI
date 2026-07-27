package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type accountShareQuotaUsageRepositoryStub struct {
	AccountShareModeRepository
	usage    *AccountShareQuotaUsage
	quota    *AccountShareResolvedQuota
	err      error
	quotaErr error
}

func (r *accountShareQuotaUsageRepositoryStub) GetAccountShareQuotaUsage(
	context.Context,
	int64,
) (*AccountShareQuotaUsage, error) {
	return r.usage, r.err
}

func (r *accountShareQuotaUsageRepositoryStub) ResolveAccountShareQuota(
	context.Context,
	int64,
	time.Time,
) (*AccountShareResolvedQuota, error) {
	return r.quota, r.quotaErr
}

func TestAccountShareModeGetCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		usage                  *AccountShareQuotaUsage
		quota                  *AccountShareResolvedQuota
		wantCreate             bool
		wantBlockers           []string
		wantMaxAccountsPerRoom int
	}{
		{
			name: "below every quota",
			usage: &AccountShareQuotaUsage{
				LiveRooms:          2,
				RoomCreates24Hours: 3,
				OwnerRoomAccounts:  7,
			},
			quota:                  defaultAccountShareResolvedQuotaForTest(),
			wantCreate:             true,
			wantBlockers:           []string{},
			wantMaxAccountsPerRoom: AccountShareDefaultMaxAccountsPerRoom,
		},
		{
			name: "each exhausted quota is explicit",
			usage: &AccountShareQuotaUsage{
				LiveRooms:          AccountShareDefaultMaxLiveRooms,
				RoomCreates24Hours: AccountShareDefaultMaxRoomCreatesPer24Hours,
				OwnerRoomAccounts:  AccountShareDefaultMaxRoomAccountsPerOwner,
			},
			quota:      defaultAccountShareResolvedQuotaForTest(),
			wantCreate: false,
			wantBlockers: []string{
				"ACCOUNT_SHARE_ROOM_LIMIT_EXCEEDED",
				"ACCOUNT_SHARE_ROOM_CREATE_RATE_EXCEEDED",
				"ACCOUNT_SHARE_OWNER_ROOM_ACCOUNT_LIMIT_EXCEEDED",
			},
			wantMaxAccountsPerRoom: AccountShareDefaultMaxAccountsPerRoom,
		},
		{
			name: "grandfathered overage never exposes a negative remainder",
			usage: &AccountShareQuotaUsage{
				LiveRooms:          AccountShareDefaultMaxLiveRooms + 2,
				RoomCreates24Hours: AccountShareDefaultMaxRoomCreatesPer24Hours + 3,
				OwnerRoomAccounts:  AccountShareDefaultMaxRoomAccountsPerOwner + 4,
			},
			quota:      defaultAccountShareResolvedQuotaForTest(),
			wantCreate: false,
			wantBlockers: []string{
				"ACCOUNT_SHARE_QUOTA_HISTORICAL_GROWTH_BLOCKED",
				"ACCOUNT_SHARE_ROOM_LIMIT_EXCEEDED",
				"ACCOUNT_SHARE_ROOM_CREATE_RATE_EXCEEDED",
				"ACCOUNT_SHARE_OWNER_ROOM_ACCOUNT_LIMIT_EXCEEDED",
			},
			wantMaxAccountsPerRoom: AccountShareDefaultMaxAccountsPerRoom,
		},
		{
			name: "manual override drives the effective limits",
			usage: &AccountShareQuotaUsage{
				LiveRooms:          5,
				RoomCreates24Hours: 5,
				OwnerRoomAccounts:  100,
			},
			quota: &AccountShareResolvedQuota{
				Limits: AccountShareQuotaLimits{
					MaxLiveRooms:            10,
					MaxRoomCreates24Hours:   12,
					MaxAccountsPerRoom:      30,
					MaxRoomAccountsPerOwner: 200,
				},
				Source:        "owner_override",
				PolicyID:      91,
				PolicyVersion: 3,
				OverrideKind:  AccountShareQuotaPolicyKindManual,
			},
			wantCreate:             true,
			wantBlockers:           []string{},
			wantMaxAccountsPerRoom: 30,
		},
		{
			name: "grandfather policy blocks all growth even below recorded limits",
			usage: &AccountShareQuotaUsage{
				LiveRooms:          4,
				RoomCreates24Hours: 4,
				OwnerRoomAccounts:  90,
			},
			quota: &AccountShareResolvedQuota{
				Limits: AccountShareQuotaLimits{
					MaxLiveRooms:            8,
					MaxRoomCreates24Hours:   8,
					MaxAccountsPerRoom:      25,
					MaxRoomAccountsPerOwner: 150,
				},
				Source:        "owner_override",
				PolicyID:      92,
				PolicyVersion: 2,
				OverrideKind:  AccountShareQuotaPolicyKindGrandfather,
				GrowthBlocked: true,
			},
			wantCreate: false,
			wantBlockers: []string{
				"ACCOUNT_SHARE_QUOTA_GRANDFATHER_GROWTH_BLOCKED",
			},
			wantMaxAccountsPerRoom: 25,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &accountShareQuotaUsageRepositoryStub{usage: tt.usage, quota: tt.quota}
			svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

			got, err := svc.GetCapabilities(context.Background(), 42)
			require.NoError(t, err)
			require.NotNil(t, got)
			require.Equal(t, tt.wantCreate, got.CanCreateRoom)
			require.Equal(t, AccountShareModeMinSeats, got.SeatLimitMinimum)
			require.Equal(t, AccountShareModeMaxSeats, got.SeatLimitMaximum)
			require.Equal(t, tt.wantMaxAccountsPerRoom, got.MaxAccountsPerRoom)
			require.GreaterOrEqual(t, got.LiveRooms.Remaining, 0)
			require.GreaterOrEqual(t, got.RoomCreates24Hours.Remaining, 0)
			require.GreaterOrEqual(t, got.OwnerRoomAccounts.Remaining, 0)

			blockerCodes := make([]string, 0, len(got.CapabilityBlockers))
			for _, blocker := range got.CapabilityBlockers {
				blockerCodes = append(blockerCodes, blocker.Code)
			}
			require.Equal(t, tt.wantBlockers, blockerCodes)
		})
	}
}

func TestAccountShareQuotaExceededDimensionsOnlyBlocksHistoricalOverage(t *testing.T) {
	t.Parallel()
	limits := DefaultAccountShareQuotaLimits()
	require.Empty(t, AccountShareQuotaExceededDimensions(limits, AccountShareQuotaUsage{
		LiveRooms:           limits.MaxLiveRooms,
		RoomCreates24Hours:  limits.MaxRoomCreates24Hours,
		LargestRoomAccounts: limits.MaxAccountsPerRoom,
		OwnerRoomAccounts:   limits.MaxRoomAccountsPerOwner,
	}))
	require.Equal(t, []string{"max_live_rooms", "max_accounts_per_room"}, AccountShareQuotaExceededDimensions(
		limits,
		AccountShareQuotaUsage{
			LiveRooms:           limits.MaxLiveRooms + 1,
			LargestRoomAccounts: limits.MaxAccountsPerRoom + 1,
		},
	))
}

func TestAccountShareModeGetCapabilitiesFailsClosed(t *testing.T) {
	t.Parallel()

	t.Run("invalid owner", func(t *testing.T) {
		t.Parallel()

		svc := NewAccountShareModeService(
			&accountShareQuotaUsageRepositoryStub{
				usage: &AccountShareQuotaUsage{},
				quota: defaultAccountShareResolvedQuotaForTest(),
			},
			nil,
			nil,
			nil,
			nil,
			nil,
		)
		_, err := svc.GetCapabilities(context.Background(), 0)
		require.ErrorIs(t, err, ErrUserNotFound)
	})

	t.Run("repository error", func(t *testing.T) {
		t.Parallel()

		expected := errors.New("quota query failed")
		svc := NewAccountShareModeService(
			&accountShareQuotaUsageRepositoryStub{err: expected},
			nil,
			nil,
			nil,
			nil,
			nil,
		)
		_, err := svc.GetCapabilities(context.Background(), 42)
		require.ErrorIs(t, err, expected)
	})

	t.Run("repository returns no snapshot", func(t *testing.T) {
		t.Parallel()

		svc := NewAccountShareModeService(
			&accountShareQuotaUsageRepositoryStub{},
			nil,
			nil,
			nil,
			nil,
			nil,
		)
		_, err := svc.GetCapabilities(context.Background(), 42)
		require.ErrorIs(t, err, ErrServiceUnavailable)
	})

	t.Run("quota policy resolver fails closed", func(t *testing.T) {
		t.Parallel()

		expected := errors.New("quota policy query failed")
		svc := NewAccountShareModeService(
			&accountShareQuotaUsageRepositoryStub{
				usage:    &AccountShareQuotaUsage{},
				quotaErr: expected,
			},
			nil,
			nil,
			nil,
			nil,
			nil,
		)
		_, err := svc.GetCapabilities(context.Background(), 42)
		require.ErrorIs(t, err, expected)
	})
}

func defaultAccountShareResolvedQuotaForTest() *AccountShareResolvedQuota {
	return &AccountShareResolvedQuota{
		Limits:        DefaultAccountShareQuotaLimits(),
		Source:        AccountShareQuotaScopeGlobal,
		PolicyID:      1,
		PolicyVersion: 1,
		OverrideKind:  AccountShareQuotaPolicyKindDefault,
	}
}
