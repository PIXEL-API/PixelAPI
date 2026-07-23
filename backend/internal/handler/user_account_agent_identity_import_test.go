package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestResolveUserOpenAICredentialImportMode(t *testing.T) {
	agentIdentity := service.AccountCredentialImportSource{
		Kind:     service.AccountCredentialImportKindOpenAIAgentIdentity,
		Platform: service.PlatformOpenAI,
	}
	oauth := service.AccountCredentialImportSource{
		Kind:     service.AccountCredentialImportKindOAuthCredentials,
		Platform: service.PlatformOpenAI,
	}

	tests := []struct {
		name      string
		req       importUserAccountCredentialsRequest
		sources   []service.AccountCredentialImportSource
		wantAgent bool
		wantErr   bool
	}{
		{
			name:      "declared Agent Identity",
			req:       importUserAccountCredentialsRequest{Platform: service.PlatformOpenAI, OpenAIAuthMode: userOpenAIAuthModeAgentIdentity},
			sources:   []service.AccountCredentialImportSource{agentIdentity},
			wantAgent: true,
		},
		{
			name:      "legacy client infers Agent Identity",
			req:       importUserAccountCredentialsRequest{Platform: service.PlatformOpenAI},
			sources:   []service.AccountCredentialImportSource{agentIdentity},
			wantAgent: true,
		},
		{
			name:    "declared Agent Identity rejects OAuth",
			req:     importUserAccountCredentialsRequest{Platform: service.PlatformOpenAI, OpenAIAuthMode: userOpenAIAuthModeAgentIdentity},
			sources: []service.AccountCredentialImportSource{oauth},
			wantErr: true,
		},
		{
			name:    "declared OAuth rejects Agent Identity",
			req:     importUserAccountCredentialsRequest{Platform: service.PlatformOpenAI, OpenAIAuthMode: userOpenAIAuthModeOAuth},
			sources: []service.AccountCredentialImportSource{agentIdentity},
			wantErr: true,
		},
		{
			name:    "mixed credentials are rejected",
			req:     importUserAccountCredentialsRequest{Platform: service.PlatformOpenAI},
			sources: []service.AccountCredentialImportSource{agentIdentity, oauth},
			wantErr: true,
		},
		{
			name:    "wrong platform is rejected",
			req:     importUserAccountCredentialsRequest{Platform: service.PlatformAnthropic, OpenAIAuthMode: userOpenAIAuthModeAgentIdentity},
			sources: []service.AccountCredentialImportSource{agentIdentity},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotAgent, err := resolveUserOpenAICredentialImportMode(test.req, test.sources)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantAgent, gotAgent)
		})
	}
}
