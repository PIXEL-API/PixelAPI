//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestResolveOwnedOpenAIImportLevel(t *testing.T) {
	configs := service.DefaultOpenAIAccountLevelConfigs()

	tests := []struct {
		name            string
		probePlanType   string
		probeFailed     bool
		targetLevel     string
		wantLevel       string
		wantErrContains string
	}{
		{
			name:          "plus matches plus",
			probePlanType: "plus",
			targetLevel:   service.AccountLevelPlus,
			wantLevel:     service.AccountLevelPlus,
		},
		{
			name:            "plus cannot impersonate pro",
			probePlanType:   "plus",
			targetLevel:     service.AccountLevelPro,
			wantErrContains: "不符",
		},
		{
			name:            "pro cannot downgrade to plus",
			probePlanType:   "pro",
			targetLevel:     service.AccountLevelPlus,
			wantErrContains: "不符",
		},
		{
			name:            "unrecognized plan_type is rejected",
			probePlanType:   "some-new-plan",
			targetLevel:     service.AccountLevelPro,
			wantErrContains: "无法识别",
		},
		{
			name:        "probe failed allows free",
			probeFailed: true,
			targetLevel: service.AccountLevelFree,
			wantLevel:   service.AccountLevelFree,
		},
		{
			name:        "probe failed allows unknown",
			probeFailed: true,
			targetLevel: service.AccountLevelUnknown,
			wantLevel:   service.AccountLevelUnknown,
		},
		{
			name:            "probe failed rejects pro",
			probeFailed:     true,
			targetLevel:     service.AccountLevelPro,
			wantErrContains: "无法验证",
		},
		{
			name:            "probe failed rejects plus",
			probeFailed:     true,
			targetLevel:     service.AccountLevelPlus,
			wantErrContains: "无法验证",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			level, err := resolveOwnedOpenAIImportLevel(test.probePlanType, test.probeFailed, test.targetLevel, configs)
			if test.wantErrContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), test.wantErrContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantLevel, level)
		})
	}
}
