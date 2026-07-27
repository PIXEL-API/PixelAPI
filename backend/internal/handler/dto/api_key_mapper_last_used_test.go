package dto

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFromService_MapsLastUsedAt(t *testing.T) {
	lastUsed := time.Now().UTC().Truncate(time.Second)
	src := &service.APIKey{
		ID:         1,
		UserID:     2,
		Key:        "sk-map-last-used",
		Name:       "Mapper",
		Status:     service.StatusActive,
		LastUsedAt: &lastUsed,
	}

	out := APIKeyFromService(src)
	require.NotNil(t, out)
	require.NotNil(t, out.LastUsedAt)
	require.WithinDuration(t, lastUsed, *out.LastUsedAt, time.Second)
}

func TestAPIKeyFromService_MapsNilLastUsedAt(t *testing.T) {
	src := &service.APIKey{
		ID:     1,
		UserID: 2,
		Key:    "sk-map-last-used-nil",
		Name:   "MapperNil",
		Status: service.StatusActive,
	}

	out := APIKeyFromService(src)
	require.NotNil(t, out)
	require.Nil(t, out.LastUsedAt)
}

func TestGroupFromService_MapsPublicAPIKeyBadge(t *testing.T) {
	src := &service.Group{
		ID:              10,
		Scope:           service.GroupScopePublic,
		APIKeyBadgeType: service.GroupAPIKeyBadgeTypeCustom,
		APIKeyBadgeText: "自定义标签",
	}

	out := GroupFromService(src)

	require.NotNil(t, out)
	require.Equal(t, service.GroupAPIKeyBadgeTypeCustom, out.APIKeyBadgeType)
	require.Equal(t, "自定义标签", out.APIKeyBadgeText)
}

func TestGroupFromService_HidesAPIKeyBadgeForUserPrivateGroup(t *testing.T) {
	src := &service.Group{
		ID:              11,
		Scope:           service.GroupScopeUserPrivate,
		APIKeyBadgeType: service.GroupAPIKeyBadgeTypeCustom,
		APIKeyBadgeText: "不应显示",
	}

	out := GroupFromService(src)

	require.NotNil(t, out)
	require.Equal(t, service.GroupAPIKeyBadgeTypeHidden, out.APIKeyBadgeType)
	require.Empty(t, out.APIKeyBadgeText)
}
