//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiTokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected string
	}{
		{
			name: "with_project_id",
			account: &Account{
				ID: 100,
				Credentials: map[string]any{
					"project_id": "my-project-123",
				},
			},
			expected: "my-project-123",
		},
		{
			name: "project_id_with_whitespace",
			account: &Account{
				ID: 101,
				Credentials: map[string]any{
					"project_id": "  project-with-spaces  ",
				},
			},
			expected: "project-with-spaces",
		},
		{
			name: "empty_project_id_fallback_to_account_id",
			account: &Account{
				ID: 102,
				Credentials: map[string]any{
					"project_id": "",
				},
			},
			expected: "account:102",
		},
		{
			name: "whitespace_only_project_id_fallback_to_account_id",
			account: &Account{
				ID: 103,
				Credentials: map[string]any{
					"project_id": "   ",
				},
			},
			expected: "account:103",
		},
		{
			name: "no_project_id_key_fallback_to_account_id",
			account: &Account{
				ID:          104,
				Credentials: map[string]any{},
			},
			expected: "account:104",
		},
		{
			name: "nil_credentials_fallback_to_account_id",
			account: &Account{
				ID:          105,
				Credentials: nil,
			},
			expected: "account:105",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GeminiTokenCacheKey(tt.account)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestAntigravityTokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected string
	}{
		{
			name: "with_project_id",
			account: &Account{
				ID: 200,
				Credentials: map[string]any{
					"project_id": "ag-project-456",
				},
			},
			expected: "ag:ag-project-456",
		},
		{
			name: "project_id_with_whitespace",
			account: &Account{
				ID: 201,
				Credentials: map[string]any{
					"project_id": "  ag-project-spaces  ",
				},
			},
			expected: "ag:ag-project-spaces",
		},
		{
			name: "empty_project_id_fallback_to_account_id",
			account: &Account{
				ID: 202,
				Credentials: map[string]any{
					"project_id": "",
				},
			},
			expected: "ag:account:202",
		},
		{
			name: "whitespace_only_project_id_fallback_to_account_id",
			account: &Account{
				ID: 203,
				Credentials: map[string]any{
					"project_id": "   ",
				},
			},
			expected: "ag:account:203",
		},
		{
			name: "no_project_id_key_fallback_to_account_id",
			account: &Account{
				ID:          204,
				Credentials: map[string]any{},
			},
			expected: "ag:account:204",
		},
		{
			name: "nil_credentials_fallback_to_account_id",
			account: &Account{
				ID:          205,
				Credentials: nil,
			},
			expected: "ag:account:205",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AntigravityTokenCacheKey(tt.account)
			require.Equal(t, tt.expected, result)
		})
	}
}
