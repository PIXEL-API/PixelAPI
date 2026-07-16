//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAuthHandlerRevokeAllSessionsInvalidatesAccessTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &userHandlerRepoStub{
		user: &service.User{
			ID:           29,
			Email:        "session@example.com",
			Username:     "session-user",
			Role:         service.RoleUser,
			Status:       service.StatusActive,
			TokenVersion: 7,
		},
	}
	refreshTokenCache := &userHandlerRefreshTokenCacheStub{}
	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:     "test-secret",
			ExpireHour: 1,
		},
	}
	authService := service.NewAuthService(nil, repo, nil, refreshTokenCache, cfg, nil, nil, nil, nil, nil, nil, nil)
	handler := &AuthHandler{authService: authService}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/revoke-all-sessions", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 29})

	handler.RevokeAllSessions(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int64{29}, refreshTokenCache.revokedUserIDs)
	require.Equal(t, int64(8), repo.user.TokenVersion)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "All sessions have been revoked. Please log in again.", resp.Data.Message)
}

func TestAuthHandlerTokenPairResponseFailsWhenRefreshTokenUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       30,
		Email:    "login@example.com",
		Username: "login-user",
		Role:     service.RoleUser,
		Status:   service.StatusActive,
	}
	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:     "test-secret",
			ExpireHour: 1,
		},
	}
	authService := service.NewAuthService(nil, &userHandlerRepoStub{user: user}, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil)
	handler := &AuthHandler{authService: authService}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)

	handler.respondWithTokenPair(c, user)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var resp struct {
		Code int `json:"code"`
		Data any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, http.StatusInternalServerError, resp.Code)
	require.Nil(t, resp.Data)
}

func TestAuthHandlerRegisterFailsWhenTokenPairUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &userHandlerRepoStub{}
	emailCache := &userHandlerEmailCacheStub{
		data: &service.VerificationCodeData{
			Code:      "123456",
			CreatedAt: time.Now().UTC(),
			ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		},
	}
	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:     "test-secret",
			ExpireHour: 1,
		},
		Default: config.DefaultConfig{
			UserBalance:     3.5,
			UserConcurrency: 2,
		},
	}
	settingService := service.NewSettingService(&authHandlerSettingRepoStub{
		values: map[string]string{
			service.SettingKeyRegistrationEnabled:                 "true",
			service.SettingKeyEmailVerifyEnabled:                  "true",
			service.SettingKeyAuthSourceDefaultEmailGrantOnSignup: "false",
		},
	}, cfg)
	emailService := service.NewEmailService(&authHandlerSettingRepoStub{}, emailCache)
	authService := service.NewAuthService(nil, repo, nil, nil, cfg, settingService, emailService, nil, nil, nil, nil, nil)
	handler := &AuthHandler{authService: authService, settingSvc: settingService}

	body := bytes.NewReader([]byte(`{"email":"new-user@example.com","password":"password","verify_code":"123456"}`))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Register(c)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, http.StatusInternalServerError, resp.Code)
	require.Nil(t, resp.Data)
	require.NotEmpty(t, resp.Message)
}

type authHandlerSettingRepoStub struct {
	values map[string]string
}

func (s *authHandlerSettingRepoStub) Get(_ context.Context, _ string) (*service.Setting, error) {
	panic("unexpected Get call")
}

func (s *authHandlerSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if s != nil {
		if v, ok := s.values[key]; ok {
			return v, nil
		}
	}
	return "", service.ErrSettingNotFound
}

func (s *authHandlerSettingRepoStub) Set(_ context.Context, _, _ string) error {
	panic("unexpected Set call")
}

func (s *authHandlerSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	if s == nil {
		return out, nil
	}
	for _, key := range keys {
		if v, ok := s.values[key]; ok {
			out[key] = v
		}
	}
	return out, nil
}

func (s *authHandlerSettingRepoStub) SetMultiple(_ context.Context, _ map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *authHandlerSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *authHandlerSettingRepoStub) Delete(_ context.Context, _ string) error {
	panic("unexpected Delete call")
}
