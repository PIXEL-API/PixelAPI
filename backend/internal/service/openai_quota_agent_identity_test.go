package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestOpenAIQuotaResetRejectsAgentIdentityBeforeUpstreamPreparation(t *testing.T) {
	account := &Account{
		ID:       701,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"auth_mode":   OpenAIAuthModeAgentIdentity,
			"private_key": "sensitive-private-key",
			"runtime_id":  "runtime-701",
			"task_id":     "task-701",
		},
	}
	repo := &agentIdentityTaskRepo{account: account}
	factoryCalled := false
	service := &OpenAIQuotaService{
		accountRepo: repo,
		privacyClientFactory: func(string) (*req.Client, error) {
			factoryCalled = true
			return req.C(), nil
		},
	}

	_, err := service.ResetCredit(context.Background(), account.ID)
	require.Error(t, err)
	require.Equal(t, "OPENAI_QUOTA_RESET_UNSUPPORTED", infraerrors.Reason(err))
	require.False(t, factoryCalled)
}
