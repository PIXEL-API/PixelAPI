package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/identityadoptiondecision"
	"github.com/Wei-Shaw/sub2api/ent/pendingauthsession"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestApplySuggestedProfileToCompletionResponse(t *testing.T) {
	payload := map[string]any{
		"access_token": "token",
	}
	upstream := map[string]any{
		"suggested_display_name": "Alice",
		"suggested_avatar_url":   "https://cdn.example/avatar.png",
	}

	applySuggestedProfileToCompletionResponse(payload, upstream)

	require.Equal(t, "Alice", payload["suggested_display_name"])
	require.Equal(t, "https://cdn.example/avatar.png", payload["suggested_avatar_url"])
	require.Equal(t, true, payload["adoption_required"])
}

func TestApplySuggestedProfileToCompletionResponseKeepsExistingPayloadValues(t *testing.T) {
	payload := map[string]any{
		"suggested_display_name": "Existing",
		"adoption_required":      false,
	}
	upstream := map[string]any{
		"suggested_display_name": "Alice",
		"suggested_avatar_url":   "https://cdn.example/avatar.png",
	}

	applySuggestedProfileToCompletionResponse(payload, upstream)

	require.Equal(t, "Existing", payload["suggested_display_name"])
	require.Equal(t, "https://cdn.example/avatar.png", payload["suggested_avatar_url"])
	require.Equal(t, true, payload["adoption_required"])
}

func TestExchangePendingOAuthCompletionPreviewThenFinalizeAppliesAdoptionDecision(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(t, false)
	ctx := context.Background()

	userEntity, err := client.User.Create().
		SetEmail("linuxdo-123@linuxdo-connect.invalid").
		SetUsername("legacy-name").
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	session, err := client.PendingAuthSession.Create().
		SetSessionToken("pending-session-token").
		SetIntent("login").
		SetProviderType("linuxdo").
		SetProviderKey("linuxdo").
		SetProviderSubject("123").
		SetTargetUserID(userEntity.ID).
		SetResolvedEmail(userEntity.Email).
		SetBrowserSessionKey("browser-session-key").
		SetUpstreamIdentityClaims(map[string]any{
			"username":               "linuxdo_user",
			"suggested_display_name": "Alice Example",
			"suggested_avatar_url":   "https://cdn.example/alice.png",
		}).
		SetLocalFlowState(map[string]any{
			oauthCompletionResponseKey: map[string]any{
				"access_token": "access-token",
				"redirect":     "/dashboard",
			},
		}).
		SetExpiresAt(time.Now().UTC().Add(10 * time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	previewRecorder := httptest.NewRecorder()
	previewCtx, _ := gin.CreateTestContext(previewRecorder)
	previewReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/pending/exchange", nil)
	previewReq.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	previewReq.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("browser-session-key")})
	previewCtx.Request = previewReq

	handler.ExchangePendingOAuthCompletion(previewCtx)

	require.Equal(t, http.StatusOK, previewRecorder.Code)
	previewData := decodeJSONResponseData(t, previewRecorder)
	require.Equal(t, "Alice Example", previewData["suggested_display_name"])
	require.Equal(t, "https://cdn.example/alice.png", previewData["suggested_avatar_url"])
	require.Equal(t, true, previewData["adoption_required"])

	storedUser, err := client.User.Get(ctx, userEntity.ID)
	require.NoError(t, err)
	require.Equal(t, "legacy-name", storedUser.Username)

	previewSession, err := client.PendingAuthSession.Query().
		Where(pendingauthsession.IDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Nil(t, previewSession.ConsumedAt)

	body := bytes.NewBufferString(`{"adopt_display_name":true,"adopt_avatar":true}`)
	finalizeRecorder := httptest.NewRecorder()
	finalizeCtx, _ := gin.CreateTestContext(finalizeRecorder)
	finalizeReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/pending/exchange", body)
	finalizeReq.Header.Set("Content-Type", "application/json")
	finalizeReq.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	finalizeReq.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("browser-session-key")})
	finalizeCtx.Request = finalizeReq

	handler.ExchangePendingOAuthCompletion(finalizeCtx)

	require.Equal(t, http.StatusOK, finalizeRecorder.Code)

	storedUser, err = client.User.Get(ctx, userEntity.ID)
	require.NoError(t, err)
	require.Equal(t, "Alice Example", storedUser.Username)

	identity, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ("linuxdo"),
			authidentity.ProviderKeyEQ("linuxdo"),
			authidentity.ProviderSubjectEQ("123"),
		).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, userEntity.ID, identity.UserID)
	require.Equal(t, "Alice Example", identity.Metadata["display_name"])
	require.Equal(t, "https://cdn.example/alice.png", identity.Metadata["avatar_url"])

	decision, err := client.IdentityAdoptionDecision.Query().
		Where(identityadoptiondecision.PendingAuthSessionIDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, decision.IdentityID)
	require.Equal(t, identity.ID, *decision.IdentityID)
	require.True(t, decision.AdoptDisplayName)
	require.True(t, decision.AdoptAvatar)

	consumed, err := client.PendingAuthSession.Query().
		Where(pendingauthsession.IDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, consumed.ConsumedAt)
}

func TestExchangePendingOAuthCompletionBindCurrentUserPreviewThenFinalizeBindsIdentityWithoutAdoption(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(t, false)
	ctx := context.Background()

	userEntity, err := client.User.Create().
		SetEmail("bind-target@example.com").
		SetUsername("legacy-name").
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	session, err := client.PendingAuthSession.Create().
		SetSessionToken("bind-pending-session-token").
		SetIntent("bind_current_user").
		SetProviderType("linuxdo").
		SetProviderKey("linuxdo").
		SetProviderSubject("bind-123").
		SetTargetUserID(userEntity.ID).
		SetResolvedEmail(userEntity.Email).
		SetBrowserSessionKey("bind-browser-session-key").
		SetUpstreamIdentityClaims(map[string]any{
			"username":               "linuxdo_user",
			"suggested_display_name": "Bound Example",
			"suggested_avatar_url":   "https://cdn.example/bound.png",
		}).
		SetLocalFlowState(map[string]any{
			oauthCompletionResponseKey: map[string]any{
				"access_token": "access-token",
				"redirect":     "/settings/profile",
			},
		}).
		SetExpiresAt(time.Now().UTC().Add(10 * time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	previewRecorder := httptest.NewRecorder()
	previewCtx, _ := gin.CreateTestContext(previewRecorder)
	previewReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/pending/exchange", nil)
	previewReq.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	previewReq.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("bind-browser-session-key")})
	previewCtx.Request = previewReq

	handler.ExchangePendingOAuthCompletion(previewCtx)

	require.Equal(t, http.StatusOK, previewRecorder.Code)
	previewData := decodeJSONResponseData(t, previewRecorder)
	require.Equal(t, "Bound Example", previewData["suggested_display_name"])
	require.Equal(t, "https://cdn.example/bound.png", previewData["suggested_avatar_url"])
	require.Equal(t, true, previewData["adoption_required"])

	identityCount, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ("linuxdo"),
			authidentity.ProviderKeyEQ("linuxdo"),
			authidentity.ProviderSubjectEQ("bind-123"),
		).
		Count(ctx)
	require.NoError(t, err)
	require.Zero(t, identityCount)

	previewSession, err := client.PendingAuthSession.Query().
		Where(pendingauthsession.IDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Nil(t, previewSession.ConsumedAt)

	body := bytes.NewBufferString(`{"adopt_display_name":false,"adopt_avatar":false}`)
	finalizeRecorder := httptest.NewRecorder()
	finalizeCtx, _ := gin.CreateTestContext(finalizeRecorder)
	finalizeReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/pending/exchange", body)
	finalizeReq.Header.Set("Content-Type", "application/json")
	finalizeReq.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	finalizeReq.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("bind-browser-session-key")})
	finalizeCtx.Request = finalizeReq

	handler.ExchangePendingOAuthCompletion(finalizeCtx)

	require.Equal(t, http.StatusOK, finalizeRecorder.Code)

	storedUser, err := client.User.Get(ctx, userEntity.ID)
	require.NoError(t, err)
	require.Equal(t, "legacy-name", storedUser.Username)

	identity, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ("linuxdo"),
			authidentity.ProviderKeyEQ("linuxdo"),
			authidentity.ProviderSubjectEQ("bind-123"),
		).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, userEntity.ID, identity.UserID)
	require.Equal(t, "Bound Example", identity.Metadata["suggested_display_name"])
	require.Equal(t, "https://cdn.example/bound.png", identity.Metadata["suggested_avatar_url"])
	_, hasDisplayName := identity.Metadata["display_name"]
	require.False(t, hasDisplayName)
	_, hasAvatarURL := identity.Metadata["avatar_url"]
	require.False(t, hasAvatarURL)

	decision, err := client.IdentityAdoptionDecision.Query().
		Where(identityadoptiondecision.PendingAuthSessionIDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, decision.IdentityID)
	require.Equal(t, identity.ID, *decision.IdentityID)
	require.False(t, decision.AdoptDisplayName)
	require.False(t, decision.AdoptAvatar)

	consumed, err := client.PendingAuthSession.Query().
		Where(pendingauthsession.IDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, consumed.ConsumedAt)
}

func TestExchangePendingOAuthCompletionBindCurrentUserOwnershipConflict(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(t, false)
	ctx := context.Background()

	targetUser, err := client.User.Create().
		SetEmail("bind-conflict-target@example.com").
		SetUsername("target-user").
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	ownerUser, err := client.User.Create().
		SetEmail("bind-conflict-owner@example.com").
		SetUsername("owner-user").
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	existingIdentity, err := client.AuthIdentity.Create().
		SetUserID(ownerUser.ID).
		SetProviderType("linuxdo").
		SetProviderKey("linuxdo").
		SetProviderSubject("conflict-123").
		SetMetadata(map[string]any{"username": "owner-user"}).
		Save(ctx)
	require.NoError(t, err)

	session, err := client.PendingAuthSession.Create().
		SetSessionToken("bind-conflict-session-token").
		SetIntent("bind_current_user").
		SetProviderType("linuxdo").
		SetProviderKey("linuxdo").
		SetProviderSubject("conflict-123").
		SetTargetUserID(targetUser.ID).
		SetResolvedEmail(targetUser.Email).
		SetBrowserSessionKey("bind-conflict-browser-session-key").
		SetUpstreamIdentityClaims(map[string]any{
			"suggested_display_name": "Conflict Example",
			"suggested_avatar_url":   "https://cdn.example/conflict.png",
		}).
		SetLocalFlowState(map[string]any{
			oauthCompletionResponseKey: map[string]any{
				"access_token": "access-token",
			},
		}).
		SetExpiresAt(time.Now().UTC().Add(10 * time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	body := bytes.NewBufferString(`{"adopt_display_name":false,"adopt_avatar":false}`)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/pending/exchange", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	req.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("bind-conflict-browser-session-key")})
	ginCtx.Request = req

	handler.ExchangePendingOAuthCompletion(ginCtx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	payload := decodeJSONBody(t, recorder)
	require.Equal(t, "PENDING_AUTH_ADOPTION_APPLY_FAILED", payload["reason"])

	identity, err := client.AuthIdentity.Get(ctx, existingIdentity.ID)
	require.NoError(t, err)
	require.Equal(t, ownerUser.ID, identity.UserID)

	decision, err := client.IdentityAdoptionDecision.Query().
		Where(identityadoptiondecision.PendingAuthSessionIDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Nil(t, decision.IdentityID)
	require.False(t, decision.AdoptDisplayName)
	require.False(t, decision.AdoptAvatar)

	storedSession, err := client.PendingAuthSession.Query().
		Where(pendingauthsession.IDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Nil(t, storedSession.ConsumedAt)
}

func TestExchangePendingOAuthCompletionLoginFalseFalseDoesNotBindIdentity(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(t, false)
	ctx := context.Background()

	userEntity, err := client.User.Create().
		SetEmail("login-false@example.com").
		SetUsername("legacy-name").
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	session, err := client.PendingAuthSession.Create().
		SetSessionToken("login-false-session-token").
		SetIntent("login").
		SetProviderType("linuxdo").
		SetProviderKey("linuxdo").
		SetProviderSubject("login-false-123").
		SetTargetUserID(userEntity.ID).
		SetResolvedEmail(userEntity.Email).
		SetBrowserSessionKey("login-false-browser-session-key").
		SetUpstreamIdentityClaims(map[string]any{
			"suggested_display_name": "Login Example",
			"suggested_avatar_url":   "https://cdn.example/login.png",
		}).
		SetLocalFlowState(map[string]any{
			oauthCompletionResponseKey: map[string]any{
				"access_token": "access-token",
			},
		}).
		SetExpiresAt(time.Now().UTC().Add(10 * time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	body := bytes.NewBufferString(`{"adopt_display_name":false,"adopt_avatar":false}`)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/pending/exchange", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	req.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("login-false-browser-session-key")})
	ginCtx.Request = req

	handler.ExchangePendingOAuthCompletion(ginCtx)

	require.Equal(t, http.StatusOK, recorder.Code)

	identityCount, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ("linuxdo"),
			authidentity.ProviderKeyEQ("linuxdo"),
			authidentity.ProviderSubjectEQ("login-false-123"),
		).
		Count(ctx)
	require.NoError(t, err)
	require.Zero(t, identityCount)

	decision, err := client.IdentityAdoptionDecision.Query().
		Where(identityadoptiondecision.PendingAuthSessionIDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Nil(t, decision.IdentityID)
	require.False(t, decision.AdoptDisplayName)
	require.False(t, decision.AdoptAvatar)

	storedSession, err := client.PendingAuthSession.Query().
		Where(pendingauthsession.IDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, storedSession.ConsumedAt)
}

func TestExchangePendingOAuthCompletionInvitationRequiredFalseFalsePersistsDecisionWithoutBinding(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(t, true)
	ctx := context.Background()

	session, err := client.PendingAuthSession.Create().
		SetSessionToken("invitation-required-session-token").
		SetIntent("login").
		SetProviderType("linuxdo").
		SetProviderKey("linuxdo").
		SetProviderSubject("invitation-123").
		SetBrowserSessionKey("invitation-required-browser-session-key").
		SetUpstreamIdentityClaims(map[string]any{
			"suggested_display_name": "Invite Example",
			"suggested_avatar_url":   "https://cdn.example/invite.png",
		}).
		SetLocalFlowState(map[string]any{
			oauthCompletionResponseKey: map[string]any{
				"error": "invitation_required",
			},
		}).
		SetExpiresAt(time.Now().UTC().Add(10 * time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	body := bytes.NewBufferString(`{"adopt_display_name":false,"adopt_avatar":false}`)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/pending/exchange", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	req.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("invitation-required-browser-session-key")})
	ginCtx.Request = req

	handler.ExchangePendingOAuthCompletion(ginCtx)

	require.Equal(t, http.StatusOK, recorder.Code)
	data := decodeJSONResponseData(t, recorder)
	require.Equal(t, "invitation_required", data["error"])
	require.Equal(t, true, data["adoption_required"])

	identityCount, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ("linuxdo"),
			authidentity.ProviderKeyEQ("linuxdo"),
			authidentity.ProviderSubjectEQ("invitation-123"),
		).
		Count(ctx)
	require.NoError(t, err)
	require.Zero(t, identityCount)

	decision, err := client.IdentityAdoptionDecision.Query().
		Where(identityadoptiondecision.PendingAuthSessionIDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Nil(t, decision.IdentityID)
	require.False(t, decision.AdoptDisplayName)
	require.False(t, decision.AdoptAvatar)

	storedSession, err := client.PendingAuthSession.Query().
		Where(pendingauthsession.IDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Nil(t, storedSession.ConsumedAt)
}

func newOAuthPendingFlowTestHandler(t *testing.T, invitationEnabled bool) (*AuthHandler, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:auth_oauth_pending_flow_handler?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))

	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:                   "test-secret",
			ExpireHour:               1,
			AccessTokenExpireMinutes: 60,
			RefreshTokenExpireDays:   7,
		},
		Default: config.DefaultConfig{
			UserBalance:     0,
			UserConcurrency: 1,
		},
	}
	settingSvc := service.NewSettingService(&oauthPendingFlowSettingRepoStub{
		values: map[string]string{
			service.SettingKeyRegistrationEnabled:   "true",
			service.SettingKeyInvitationCodeEnabled: boolSettingValue(invitationEnabled),
		},
	}, cfg)
	userRepo := &oauthPendingFlowUserRepo{client: client}
	authSvc := service.NewAuthService(
		client,
		userRepo,
		nil,
		&oauthPendingFlowRefreshTokenCacheStub{},
		cfg,
		settingSvc,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	userSvc := service.NewUserService(userRepo, nil, nil, nil)

	return &AuthHandler{
		authService: authSvc,
		userService: userSvc,
		settingSvc:  settingSvc,
	}, client
}

func boolSettingValue(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func boolPtr(v bool) *bool {
	return &v
}

type oauthPendingFlowSettingRepoStub struct {
	values map[string]string
}

func (s *oauthPendingFlowSettingRepoStub) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (s *oauthPendingFlowSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (s *oauthPendingFlowSettingRepoStub) Set(context.Context, string, string) error {
	return nil
}

func (s *oauthPendingFlowSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *oauthPendingFlowSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (s *oauthPendingFlowSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	result := make(map[string]string, len(s.values))
	for key, value := range s.values {
		result[key] = value
	}
	return result, nil
}

func (s *oauthPendingFlowSettingRepoStub) Delete(context.Context, string) error {
	return nil
}

type oauthPendingFlowRefreshTokenCacheStub struct{}

func (s *oauthPendingFlowRefreshTokenCacheStub) StoreRefreshToken(context.Context, string, *service.RefreshTokenData, time.Duration) error {
	return nil
}

func (s *oauthPendingFlowRefreshTokenCacheStub) GetRefreshToken(context.Context, string) (*service.RefreshTokenData, error) {
	return nil, service.ErrRefreshTokenNotFound
}

func (s *oauthPendingFlowRefreshTokenCacheStub) DeleteRefreshToken(context.Context, string) error {
	return nil
}

func (s *oauthPendingFlowRefreshTokenCacheStub) DeleteUserRefreshTokens(context.Context, int64) error {
	return nil
}

func (s *oauthPendingFlowRefreshTokenCacheStub) DeleteTokenFamily(context.Context, string) error {
	return nil
}

func (s *oauthPendingFlowRefreshTokenCacheStub) AddToUserTokenSet(context.Context, int64, string, time.Duration) error {
	return nil
}

func (s *oauthPendingFlowRefreshTokenCacheStub) AddToFamilyTokenSet(context.Context, string, string, time.Duration) error {
	return nil
}

func (s *oauthPendingFlowRefreshTokenCacheStub) GetUserTokenHashes(context.Context, int64) ([]string, error) {
	return nil, nil
}

func (s *oauthPendingFlowRefreshTokenCacheStub) GetFamilyTokenHashes(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *oauthPendingFlowRefreshTokenCacheStub) IsTokenInFamily(context.Context, string, string) (bool, error) {
	return false, nil
}

func decodeJSONResponseData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope.Data
}

func decodeJSONBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload
}

type oauthPendingFlowUserRepo struct {
	client *dbent.Client
}

func (r *oauthPendingFlowUserRepo) Create(ctx context.Context, user *service.User) error {
	entity, err := r.client.User.Create().
		SetEmail(user.Email).
		SetUsername(user.Username).
		SetNotes(user.Notes).
		SetPasswordHash(user.PasswordHash).
		SetRole(user.Role).
		SetBalance(user.Balance).
		SetConcurrency(user.Concurrency).
		SetStatus(user.Status).
		SetSignupSource(user.SignupSource).
		SetNillableLastLoginAt(user.LastLoginAt).
		SetNillableLastActiveAt(user.LastActiveAt).
		Save(ctx)
	if err != nil {
		return err
	}
	user.ID = entity.ID
	user.CreatedAt = entity.CreatedAt
	user.UpdatedAt = entity.UpdatedAt
	return nil
}

func (r *oauthPendingFlowUserRepo) GetByID(ctx context.Context, id int64) (*service.User, error) {
	entity, err := r.client.User.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrUserNotFound
		}
		return nil, err
	}
	return oauthPendingFlowServiceUser(entity), nil
}

func (r *oauthPendingFlowUserRepo) GetByEmail(ctx context.Context, email string) (*service.User, error) {
	entity, err := r.client.User.Query().Where(dbuser.EmailEQ(email)).Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrUserNotFound
		}
		return nil, err
	}
	return oauthPendingFlowServiceUser(entity), nil
}

func (r *oauthPendingFlowUserRepo) GetFirstAdmin(context.Context) (*service.User, error) {
	panic("unexpected GetFirstAdmin call")
}

func (r *oauthPendingFlowUserRepo) Update(ctx context.Context, user *service.User) error {
	entity, err := r.client.User.UpdateOneID(user.ID).
		SetEmail(user.Email).
		SetUsername(user.Username).
		SetNotes(user.Notes).
		SetPasswordHash(user.PasswordHash).
		SetRole(user.Role).
		SetBalance(user.Balance).
		SetConcurrency(user.Concurrency).
		SetStatus(user.Status).
		SetSignupSource(user.SignupSource).
		SetNillableLastLoginAt(user.LastLoginAt).
		SetNillableLastActiveAt(user.LastActiveAt).
		Save(ctx)
	if err != nil {
		return err
	}
	user.UpdatedAt = entity.UpdatedAt
	return nil
}

func (r *oauthPendingFlowUserRepo) Delete(ctx context.Context, id int64) error {
	return r.client.User.DeleteOneID(id).Exec(ctx)
}

func (r *oauthPendingFlowUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func (r *oauthPendingFlowUserRepo) UpsertUserAvatar(context.Context, int64, service.UpsertUserAvatarInput) (*service.UserAvatar, error) {
	panic("unexpected UpsertUserAvatar call")
}

func (r *oauthPendingFlowUserRepo) DeleteUserAvatar(context.Context, int64) error {
	return nil
}

func (r *oauthPendingFlowUserRepo) List(context.Context, pagination.PaginationParams) ([]service.User, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (r *oauthPendingFlowUserRepo) ListWithFilters(context.Context, pagination.PaginationParams, service.UserListFilters) ([]service.User, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (r *oauthPendingFlowUserRepo) UpdateBalance(context.Context, int64, float64) error {
	panic("unexpected UpdateBalance call")
}

func (r *oauthPendingFlowUserRepo) DeductBalance(context.Context, int64, float64) error {
	panic("unexpected DeductBalance call")
}

func (r *oauthPendingFlowUserRepo) UpdateConcurrency(context.Context, int64, int) error {
	panic("unexpected UpdateConcurrency call")
}

func (r *oauthPendingFlowUserRepo) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	count, err := r.client.User.Query().Where(dbuser.EmailEQ(email)).Count(ctx)
	return count > 0, err
}

func (r *oauthPendingFlowUserRepo) RemoveGroupFromAllowedGroups(context.Context, int64) (int64, error) {
	panic("unexpected RemoveGroupFromAllowedGroups call")
}

func (r *oauthPendingFlowUserRepo) AddGroupToAllowedGroups(context.Context, int64, int64) error {
	panic("unexpected AddGroupToAllowedGroups call")
}

func (r *oauthPendingFlowUserRepo) RemoveGroupFromUserAllowedGroups(context.Context, int64, int64) error {
	panic("unexpected RemoveGroupFromUserAllowedGroups call")
}

func (r *oauthPendingFlowUserRepo) ListUserAuthIdentities(ctx context.Context, userID int64) ([]service.UserAuthIdentityRecord, error) {
	identities, err := r.client.AuthIdentity.Query().
		Where(authidentity.UserIDEQ(userID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	records := make([]service.UserAuthIdentityRecord, 0, len(identities))
	for _, identity := range identities {
		if identity == nil {
			continue
		}
		records = append(records, service.UserAuthIdentityRecord{
			ProviderType:    identity.ProviderType,
			ProviderKey:     identity.ProviderKey,
			ProviderSubject: identity.ProviderSubject,
			VerifiedAt:      identity.VerifiedAt,
			Issuer:          identity.Issuer,
			Metadata:        identity.Metadata,
			CreatedAt:       identity.CreatedAt,
			UpdatedAt:       identity.UpdatedAt,
		})
	}
	return records, nil
}

func (r *oauthPendingFlowUserRepo) UpdateTotpSecret(context.Context, int64, *string) error {
	panic("unexpected UpdateTotpSecret call")
}

func (r *oauthPendingFlowUserRepo) EnableTotp(context.Context, int64) error {
	panic("unexpected EnableTotp call")
}

func (r *oauthPendingFlowUserRepo) DisableTotp(context.Context, int64) error {
	panic("unexpected DisableTotp call")
}

func oauthPendingFlowServiceUser(entity *dbent.User) *service.User {
	if entity == nil {
		return nil
	}
	return &service.User{
		ID:           entity.ID,
		Email:        entity.Email,
		Username:     entity.Username,
		Notes:        entity.Notes,
		PasswordHash: entity.PasswordHash,
		Role:         entity.Role,
		Balance:      entity.Balance,
		Concurrency:  entity.Concurrency,
		Status:       entity.Status,
		SignupSource: entity.SignupSource,
		LastLoginAt:  entity.LastLoginAt,
		LastActiveAt: entity.LastActiveAt,
		CreatedAt:    entity.CreatedAt,
		UpdatedAt:    entity.UpdatedAt,
	}
}
