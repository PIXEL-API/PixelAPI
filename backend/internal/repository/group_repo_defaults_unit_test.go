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
