//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type settingWeChatRepoStub struct {
	values map[string]string
}

func (s *settingWeChatRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *settingWeChatRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (s *settingWeChatRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (s *settingWeChatRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *settingWeChatRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *settingWeChatRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *settingWeChatRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func TestSettingService_GetWeChatConnectOAuthConfig_UsesDatabaseOverrides(t *testing.T) {
	repo := &settingWeChatRepoStub{
		values: map[string]string{
			SettingKeyWeChatConnectEnabled:             "true",
			SettingKeyWeChatConnectAppID:               "wx-db-app",
			SettingKeyWeChatConnectAppSecret:           "wx-db-secret",
			SettingKeyWeChatConnectMode:                "mp",
			SettingKeyWeChatConnectScopes:              "snsapi_base",
			SettingKeyWeChatConnectOpenEnabled:         "true",
			SettingKeyWeChatConnectMPEnabled:           "true",
			SettingKeyWeChatConnectRedirectURL:         "https://api.example.com/api/v1/auth/oauth/wechat/callback",
			SettingKeyWeChatConnectFrontendRedirectURL: "/auth/wechat/callback",
		},
	}
	svc := NewSettingService(repo, &config.Config{})

	got, err := svc.GetWeChatConnectOAuthConfig(context.Background())
	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, "wx-db-app", got.AppIDForMode("mp"))
	require.Equal(t, "wx-db-secret", got.AppSecretForMode("mp"))
	require.True(t, got.OpenEnabled)
	require.True(t, got.MPEnabled)
	require.Equal(t, "mp", got.Mode)
	require.Equal(t, "snsapi_base", got.Scopes)
	require.Equal(t, "https://api.example.com/api/v1/auth/oauth/wechat/callback", got.RedirectURL)
	require.Equal(t, "/auth/wechat/callback", got.FrontendRedirectURL)
}
