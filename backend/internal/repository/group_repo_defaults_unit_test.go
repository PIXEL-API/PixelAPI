//go:build unit

package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNormalizeGroupWriteDefaultsKeepsNewUserRateMultiplierPositive(t *testing.T) {
	group := &service.Group{}

	normalizeGroupWriteDefaults(group)

	require.Equal(t, 1.0, group.NewUserRateMultiplier)
}

func TestNormalizeGroupWriteDefaultsKeepsExplicitNewUserRateMultiplier(t *testing.T) {
	group := &service.Group{NewUserRateMultiplier: 0.5}

	normalizeGroupWriteDefaults(group)

	require.Equal(t, 0.5, group.NewUserRateMultiplier)
}

func TestApplyVisibleGroupAvailabilitySortsAndMarksScarceGroups(t *testing.T) {
	groups := []service.Group{
		{ID: 1, Name: "scarce-first"},
		{ID: 2, Name: "healthy-two"},
		{ID: 3, Name: "healthy-five"},
		{ID: 4, Name: "scarce-last"},
	}
	counts := map[int64]groupAccountCounts{
		1: {Available: 0},
		2: {Available: 2},
		3: {Available: 5},
		4: {Available: 0},
	}

	applyVisibleGroupAvailability(groups, counts)

	require.Equal(t, []int64{3, 2, 1, 4}, []int64{
		groups[0].ID,
		groups[1].ID,
		groups[2].ID,
		groups[3].ID,
	})
	require.False(t, groups[0].PoolScarce)
	require.False(t, groups[1].PoolScarce)
	require.True(t, groups[2].PoolScarce)
	require.True(t, groups[3].PoolScarce)
}
