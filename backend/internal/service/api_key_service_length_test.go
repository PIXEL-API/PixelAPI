package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestValidateCustomKeyRejectsMoreThanMaximumCharacters(t *testing.T) {
	service := &APIKeyService{}

	require.NoError(t, service.ValidateCustomKey(strings.Repeat("a", MaxAPIKeyCredentialCharacters)))
	require.ErrorIs(t, service.ValidateCustomKey(strings.Repeat("a", MaxAPIKeyCredentialCharacters+1)), ErrAPIKeyTooLong)
}

func TestAPIKeyCredentialWithinLimitPreservesMultiBytePrefixes(t *testing.T) {
	require.True(t, apiKeyCredentialWithinLimit(strings.Repeat("界", MaxAPIKeyCredentialCharacters)))
	require.False(t, apiKeyCredentialWithinLimit(strings.Repeat("界", MaxAPIKeyCredentialCharacters+1)))
	require.False(t, apiKeyCredentialWithinLimit(string([]byte{0xff})))
}

func TestGetByKeyRejectsTooManyCharactersBeforeRepositoryAccess(t *testing.T) {
	service := &APIKeyService{}

	apiKey, err := service.GetByKey(context.Background(), strings.Repeat("a", MaxAPIKeyCredentialCharacters+1))

	require.Nil(t, apiKey)
	require.ErrorIs(t, err, ErrAPIKeyNotFound)
}

func TestGenerateKeyValidatesConfiguredPrefixLength(t *testing.T) {
	service := &APIKeyService{cfg: &config.Config{Default: config.DefaultConfig{
		APIKeyPrefix: strings.Repeat("界", MaxAPIKeyCredentialCharacters-64),
	}}}

	key, err := service.GenerateKey()
	require.NoError(t, err)
	require.Equal(t, MaxAPIKeyCredentialCharacters, len([]rune(key)))

	service.cfg.Default.APIKeyPrefix += "界"
	_, err = service.GenerateKey()
	require.ErrorContains(t, err, "default.api_key_prefix")
}
