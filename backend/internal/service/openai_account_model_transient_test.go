package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIAccountModelTransientStateIsolatesModelsAndAccounts(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	state := newOpenAIAccountModelTransientState(8)

	first := state.recordFailure(7, "gpt-5.6", now)
	require.Equal(t, 1, first.FailureStreak)
	require.Zero(t, first.Cooldown)
	require.False(t, state.isBlocked(7, "gpt-5.6", now))

	second := state.recordFailure(7, "gpt-5.6", now.Add(time.Second))
	require.Equal(t, 2, second.FailureStreak)
	require.Equal(t, openAIModelTransientShortCooldown, second.Cooldown)
	require.True(t, state.isBlocked(7, "gpt-5.6", now.Add(2*time.Second)))
	require.False(t, state.isBlocked(7, "gpt-5.5", now.Add(2*time.Second)))
	require.False(t, state.isBlocked(8, "gpt-5.6", now.Add(2*time.Second)))
	require.False(t, state.isBlocked(7, "gpt-5.6", second.BlockUntil.Add(time.Nanosecond)))
}

func TestOpenAIGatewayModelTransientUsesMappedModel(t *testing.T) {
	svc := &OpenAIGatewayService{openaiModelTransient: newOpenAIAccountModelTransientState(8)}
	account := &Account{
		ID:       11,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-alias": "gpt-5.6"},
		},
	}
	now := time.Now()

	svc.recordOpenAIAccountModelTransientFailure(account, "gpt-alias", now)
	svc.recordOpenAIAccountModelTransientFailure(account, "gpt-5.6", now.Add(time.Second))

	require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-alias"))
	require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.6"))
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.5"))
}

func TestReportOpenAIAccountScheduleSuccessClearsCanonicalModelWithoutRemapping(t *testing.T) {
	svc := &OpenAIGatewayService{openaiModelTransient: newOpenAIAccountModelTransientState(8)}
	account := &Account{
		ID:       12,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gpt-alias":  "gpt-mapped",
				"gpt-mapped": "gpt-remapped",
			},
		},
	}
	now := time.Now()

	svc.recordOpenAIAccountModelTransientFailure(account, "gpt-alias", now)
	svc.recordOpenAIAccountModelTransientFailure(account, "gpt-alias", now.Add(time.Second))
	require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-alias"))

	canonicalModel := account.GetMappedModel("gpt-alias")
	require.Equal(t, "gpt-mapped", canonicalModel)
	svc.ReportOpenAIAccountScheduleResult(account.ID, true, nil, canonicalModel)

	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-alias"))
}
