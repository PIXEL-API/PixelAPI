package service

import (
	"context"
	"time"
)

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

const (
	// AccountShareDefaultMaxLiveRooms limits the number of non-deleted rooms
	// owned by one user. Paused and draining rooms still consume this quota.
	AccountShareDefaultMaxLiveRooms = 5
	// AccountShareDefaultMaxRoomCreatesPer24Hours prevents create/delete loops
	// from producing an unbounded amount of immutable history.
	AccountShareDefaultMaxRoomCreatesPer24Hours = 5
	// AccountShareDefaultMaxAccountsPerRoom limits the current room projection.
	AccountShareDefaultMaxAccountsPerRoom = 20
	// AccountShareDefaultMaxRoomAccountsPerOwner limits all current room
	// projections across one owner's non-deleted rooms.
	AccountShareDefaultMaxRoomAccountsPerOwner = 100
)

var (
	ErrAccountShareRoomLimitExceeded = infraerrors.Conflict(
		"ACCOUNT_SHARE_ROOM_LIMIT_EXCEEDED",
		"account share room quota exceeded",
	)
	ErrAccountShareRoomCreateRateExceeded = infraerrors.Conflict(
		"ACCOUNT_SHARE_ROOM_CREATE_RATE_EXCEEDED",
		"account share room creation limit exceeded for the last 24 hours",
	)
	ErrAccountShareRoomAccountLimitExceeded = infraerrors.Conflict(
		"ACCOUNT_SHARE_ROOM_ACCOUNT_LIMIT_EXCEEDED",
		"account share room account quota exceeded",
	)
	ErrAccountShareOwnerRoomAccountLimitExceeded = infraerrors.Conflict(
		"ACCOUNT_SHARE_OWNER_ROOM_ACCOUNT_LIMIT_EXCEEDED",
		"account share owner room account quota exceeded",
	)
)

type AccountShareQuotaValue struct {
	Limit     int `json:"limit"`
	Used      int `json:"used"`
	Remaining int `json:"remaining"`
}

type AccountShareCapabilityBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Limit   int    `json:"limit"`
	Used    int    `json:"used"`
}

type AccountShareCapabilities struct {
	LifecycleEnabled   bool                            `json:"lifecycle_enabled"`
	CanCreateRoom      bool                            `json:"can_create_room"`
	LiveRooms          AccountShareQuotaValue          `json:"live_rooms"`
	RoomCreates24Hours AccountShareQuotaValue          `json:"room_creates_24_hours"`
	OwnerRoomAccounts  AccountShareQuotaValue          `json:"owner_room_accounts"`
	MaxAccountsPerRoom int                             `json:"max_accounts_per_room"`
	SeatLimitMinimum   int                             `json:"seat_limit_minimum"`
	SeatLimitMaximum   int                             `json:"seat_limit_maximum"`
	QuotaSource        string                          `json:"quota_source"`
	QuotaPolicyID      int64                           `json:"quota_policy_id"`
	QuotaPolicyVersion int64                           `json:"quota_policy_version"`
	QuotaOverrideKind  string                          `json:"quota_override_kind"`
	QuotaExpiresAt     *time.Time                      `json:"quota_expires_at,omitempty"`
	QuotaGrowthBlocked bool                            `json:"quota_growth_blocked"`
	CapabilityBlockers []AccountShareCapabilityBlocker `json:"capability_blockers"`
}

type AccountShareQuotaUsage struct {
	LiveRooms           int `json:"live_rooms"`
	RoomCreates24Hours  int `json:"room_creates_24_hours"`
	OwnerRoomAccounts   int `json:"owner_room_accounts"`
	LargestRoomAccounts int `json:"largest_room_accounts"`
}

func (u AccountShareQuotaUsage) Valid() bool {
	return u.LiveRooms >= 0 && u.RoomCreates24Hours >= 0 &&
		u.OwnerRoomAccounts >= 0 && u.LargestRoomAccounts >= 0
}

func AccountShareQuotaExceededDimensions(
	limits AccountShareQuotaLimits,
	usage AccountShareQuotaUsage,
) []string {
	if !limits.Valid() || !usage.Valid() {
		return nil
	}
	dimensions := make([]string, 0, 4)
	if usage.LiveRooms > limits.MaxLiveRooms {
		dimensions = append(dimensions, "max_live_rooms")
	}
	if usage.RoomCreates24Hours > limits.MaxRoomCreates24Hours {
		dimensions = append(dimensions, "max_room_creates_24_hours")
	}
	if usage.LargestRoomAccounts > limits.MaxAccountsPerRoom {
		dimensions = append(dimensions, "max_accounts_per_room")
	}
	if usage.OwnerRoomAccounts > limits.MaxRoomAccountsPerOwner {
		dimensions = append(dimensions, "max_room_accounts_per_owner")
	}
	return dimensions
}

func IsAccountShareQuotaGrowthBlocked(
	quota *AccountShareResolvedQuota,
	usage AccountShareQuotaUsage,
) bool {
	return quota != nil && (quota.GrowthBlocked || len(AccountShareQuotaExceededDimensions(quota.Limits, usage)) > 0)
}

type accountShareQuotaUsageRepository interface {
	GetAccountShareQuotaUsage(ctx context.Context, ownerUserID int64) (*AccountShareQuotaUsage, error)
	ResolveAccountShareQuota(ctx context.Context, ownerUserID int64, at time.Time) (*AccountShareResolvedQuota, error)
}

func (s *AccountShareModeService) GetCapabilities(ctx context.Context, ownerUserID int64) (*AccountShareCapabilities, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	repo, ok := s.repo.(accountShareQuotaUsageRepository)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	usage, err := repo.GetAccountShareQuotaUsage(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	if usage == nil {
		return nil, ErrServiceUnavailable
	}
	quota, err := repo.ResolveAccountShareQuota(ctx, ownerUserID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if quota == nil || !quota.Limits.Valid() {
		return nil, ErrAccountShareQuotaConfigurationUnavailable
	}
	limits := quota.Limits
	result := &AccountShareCapabilities{
		LifecycleEnabled:   true,
		LiveRooms:          newAccountShareQuotaValue(limits.MaxLiveRooms, usage.LiveRooms),
		RoomCreates24Hours: newAccountShareQuotaValue(limits.MaxRoomCreates24Hours, usage.RoomCreates24Hours),
		OwnerRoomAccounts:  newAccountShareQuotaValue(limits.MaxRoomAccountsPerOwner, usage.OwnerRoomAccounts),
		MaxAccountsPerRoom: limits.MaxAccountsPerRoom,
		SeatLimitMinimum:   AccountShareModeMinSeats,
		SeatLimitMaximum:   AccountShareModeMaxSeats,
		QuotaSource:        quota.Source,
		QuotaPolicyID:      quota.PolicyID,
		QuotaPolicyVersion: quota.PolicyVersion,
		QuotaOverrideKind:  quota.OverrideKind,
		QuotaExpiresAt:     quota.OverrideExpiresAt,
		QuotaGrowthBlocked: IsAccountShareQuotaGrowthBlocked(quota, *usage),
		CapabilityBlockers: make([]AccountShareCapabilityBlocker, 0, 4),
	}
	if quota.GrowthBlocked {
		result.CapabilityBlockers = append(result.CapabilityBlockers, AccountShareCapabilityBlocker{
			Code:    "ACCOUNT_SHARE_QUOTA_GRANDFATHER_GROWTH_BLOCKED",
			Message: "当前为历史超限保留状态，只能管理、排空或删除，不能新增房间或账号",
			Limit:   limits.MaxLiveRooms,
			Used:    usage.LiveRooms,
		})
	}
	if len(AccountShareQuotaExceededDimensions(limits, *usage)) > 0 {
		result.CapabilityBlockers = append(result.CapabilityBlockers, AccountShareCapabilityBlocker{
			Code:    "ACCOUNT_SHARE_QUOTA_HISTORICAL_GROWTH_BLOCKED",
			Message: "当前历史用量已超过生效配额，只能管理、排空或删除，不能新增房间或账号",
			Limit:   limits.MaxLiveRooms,
			Used:    usage.LiveRooms,
		})
	}
	if usage.LiveRooms >= limits.MaxLiveRooms {
		result.CapabilityBlockers = append(result.CapabilityBlockers, AccountShareCapabilityBlocker{
			Code:    "ACCOUNT_SHARE_ROOM_LIMIT_EXCEEDED",
			Message: "未删除房间数量已达到上限",
			Limit:   limits.MaxLiveRooms,
			Used:    usage.LiveRooms,
		})
	}
	if usage.RoomCreates24Hours >= limits.MaxRoomCreates24Hours {
		result.CapabilityBlockers = append(result.CapabilityBlockers, AccountShareCapabilityBlocker{
			Code:    "ACCOUNT_SHARE_ROOM_CREATE_RATE_EXCEEDED",
			Message: "最近 24 小时创建房间次数已达到上限",
			Limit:   limits.MaxRoomCreates24Hours,
			Used:    usage.RoomCreates24Hours,
		})
	}
	if usage.OwnerRoomAccounts >= limits.MaxRoomAccountsPerOwner {
		result.CapabilityBlockers = append(result.CapabilityBlockers, AccountShareCapabilityBlocker{
			Code:    "ACCOUNT_SHARE_OWNER_ROOM_ACCOUNT_LIMIT_EXCEEDED",
			Message: "房间账号总数已达到上限",
			Limit:   limits.MaxRoomAccountsPerOwner,
			Used:    usage.OwnerRoomAccounts,
		})
	}
	result.CanCreateRoom = len(result.CapabilityBlockers) == 0
	return result, nil
}

func newAccountShareQuotaValue(limit, used int) AccountShareQuotaValue {
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	return AccountShareQuotaValue{
		Limit:     limit,
		Used:      used,
		Remaining: remaining,
	}
}
