//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDiagnoseModelAvailabilityForPlatform_NoMatchingModel(t *testing.T) {
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	repo.accountsByID[1] = &repo.accounts[0]
	svc := &GatewayService{accountRepo: repo, cfg: testConfig()}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5.1-codex-mini", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.False(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityForPlatform_EmptyMappingAllowsAll(t *testing.T) {
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}},
		accountsByID: map[int64]*Account{},
	}
	repo.accountsByID[1] = &repo.accounts[0]
	svc := &GatewayService{accountRepo: repo, cfg: testConfig()}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5.1-codex-mini", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityForPlatform_NoAccountsStaysEmptyPool(t *testing.T) {
	svc := &GatewayService{accountRepo: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{}}, cfg: testConfig()}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5", PlatformOpenAI)

	require.False(t, diag.HasAccountsInPool)
	require.False(t, diag.HasModelSupport)
}

func TestOpenAIGatewayDiagnoseModelAvailability_NoMatchingModel(t *testing.T) {
	svc := &OpenAIGatewayService{
		accountRepo: stubOpenAIAccountRepo{accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}},
			},
		}},
		cfg: &config.Config{RunMode: config.RunModeStandard},
	}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5.1-codex-mini", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.False(t, diag.HasModelSupport)
}

func TestOpenAIGatewayDiagnoseModelAvailability_EmptyMappingAllowsAll(t *testing.T) {
	svc := &OpenAIGatewayService{
		accountRepo: stubOpenAIAccountRepo{accounts: []Account{
			{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true},
		}},
		cfg: &config.Config{RunMode: config.RunModeStandard},
	}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5.1-codex-mini", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport)
}
