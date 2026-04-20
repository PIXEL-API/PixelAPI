//go:build unit

package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newAuthPendingIdentityServiceTestClient(t *testing.T) (*AuthPendingIdentityService, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:auth_pending_identity_service?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return NewAuthPendingIdentityService(client), client
}

func TestAuthPendingIdentityService_CreatePendingSessionStoresSeparatedState(t *testing.T) {
	svc, client := newAuthPendingIdentityServiceTestClient(t)
	ctx := context.Background()

	targetUser, err := client.User.Create().
		SetEmail("pending-target@example.com").
		SetPasswordHash("hash").
		SetRole(RoleUser).
		SetStatus(StatusActive).
		Save(ctx)
	require.NoError(t, err)

	session, err := svc.CreatePendingSession(ctx, CreatePendingAuthSessionInput{
		Intent: "bind_current_user",
		Identity: PendingAuthIdentityKey{
			ProviderType:    "wechat",
			ProviderKey:     "wechat-open",
			ProviderSubject: "union-123",
		},
		TargetUserID:           &targetUser.ID,
		RedirectTo:             "/profile",
		ResolvedEmail:          "user@example.com",
		BrowserSessionKey:      "browser-1",
		UpstreamIdentityClaims: map[string]any{"nickname": "wx-user", "avatar_url": "https://cdn.example/avatar.png"},
		LocalFlowState:         map[string]any{"step": "email_required"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, session.SessionToken)
	require.Equal(t, "bind_current_user", session.Intent)
	require.Equal(t, "wechat", session.ProviderType)
	require.NotNil(t, session.TargetUserID)
	require.Equal(t, targetUser.ID, *session.TargetUserID)
	require.Equal(t, "wx-user", session.UpstreamIdentityClaims["nickname"])
	require.Equal(t, "email_required", session.LocalFlowState["step"])
}

func TestAuthPendingIdentityService_CompletionCodeIsBrowserBoundAndOneTime(t *testing.T) {
	svc, _ := newAuthPendingIdentityServiceTestClient(t)
	ctx := context.Background()

	session, err := svc.CreatePendingSession(ctx, CreatePendingAuthSessionInput{
		Intent: "login",
		Identity: PendingAuthIdentityKey{
			ProviderType:    "linuxdo",
			ProviderKey:     "linuxdo-main",
			ProviderSubject: "subject-1",
		},
		BrowserSessionKey:      "browser-expected",
		UpstreamIdentityClaims: map[string]any{"nickname": "linux-user"},
		LocalFlowState:         map[string]any{"step": "pending"},
	})
	require.NoError(t, err)

	issued, err := svc.IssueCompletionCode(ctx, IssuePendingAuthCompletionCodeInput{
		PendingAuthSessionID: session.ID,
		BrowserSessionKey:    "browser-expected",
	})
	require.NoError(t, err)
	require.NotEmpty(t, issued.Code)

	_, err = svc.ConsumeCompletionCode(ctx, issued.Code, "browser-other")
	require.ErrorIs(t, err, ErrPendingAuthBrowserMismatch)

	consumed, err := svc.ConsumeCompletionCode(ctx, issued.Code, "browser-expected")
	require.NoError(t, err)
	require.NotNil(t, consumed.ConsumedAt)
	require.Empty(t, consumed.CompletionCodeHash)
	require.Nil(t, consumed.CompletionCodeExpiresAt)

	_, err = svc.ConsumeCompletionCode(ctx, issued.Code, "browser-expected")
	require.ErrorIs(t, err, ErrPendingAuthCodeInvalid)
}

func TestAuthPendingIdentityService_CompletionCodeExpires(t *testing.T) {
	svc, client := newAuthPendingIdentityServiceTestClient(t)
	ctx := context.Background()

	session, err := svc.CreatePendingSession(ctx, CreatePendingAuthSessionInput{
		Intent: "login",
		Identity: PendingAuthIdentityKey{
			ProviderType:    "oidc",
			ProviderKey:     "https://issuer.example",
			ProviderSubject: "subject-1",
		},
		BrowserSessionKey: "browser-expired",
	})
	require.NoError(t, err)

	issued, err := svc.IssueCompletionCode(ctx, IssuePendingAuthCompletionCodeInput{
		PendingAuthSessionID: session.ID,
		BrowserSessionKey:    "browser-expired",
		TTL:                  time.Second,
	})
	require.NoError(t, err)

	_, err = client.PendingAuthSession.UpdateOneID(session.ID).
		SetCompletionCodeExpiresAt(time.Now().UTC().Add(-time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	_, err = svc.ConsumeCompletionCode(ctx, issued.Code, "browser-expired")
	require.ErrorIs(t, err, ErrPendingAuthCodeExpired)
}

func TestAuthPendingIdentityService_UpsertAdoptionDecision(t *testing.T) {
	svc, client := newAuthPendingIdentityServiceTestClient(t)
	ctx := context.Background()

	user, err := client.User.Create().
		SetEmail("adoption@example.com").
		SetPasswordHash("hash").
		SetRole(RoleUser).
		SetStatus(StatusActive).
		Save(ctx)
	require.NoError(t, err)

	identity, err := client.AuthIdentity.Create().
		SetUserID(user.ID).
		SetProviderType("wechat").
		SetProviderKey("wechat-open").
		SetProviderSubject("union-adoption").
		SetMetadata(map[string]any{}).
		Save(ctx)
	require.NoError(t, err)

	session, err := svc.CreatePendingSession(ctx, CreatePendingAuthSessionInput{
		Intent: "bind_current_user",
		Identity: PendingAuthIdentityKey{
			ProviderType:    "wechat",
			ProviderKey:     "wechat-open",
			ProviderSubject: "union-adoption",
		},
	})
	require.NoError(t, err)

	first, err := svc.UpsertAdoptionDecision(ctx, PendingIdentityAdoptionDecisionInput{
		PendingAuthSessionID: session.ID,
		AdoptDisplayName:     true,
		AdoptAvatar:          false,
	})
	require.NoError(t, err)
	require.True(t, first.AdoptDisplayName)
	require.False(t, first.AdoptAvatar)
	require.Nil(t, first.IdentityID)

	second, err := svc.UpsertAdoptionDecision(ctx, PendingIdentityAdoptionDecisionInput{
		PendingAuthSessionID: session.ID,
		IdentityID:           &identity.ID,
		AdoptDisplayName:     true,
		AdoptAvatar:          true,
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.NotNil(t, second.IdentityID)
	require.Equal(t, identity.ID, *second.IdentityID)
	require.True(t, second.AdoptAvatar)
}

func TestAuthPendingIdentityService_ConsumeBrowserSession(t *testing.T) {
	svc, _ := newAuthPendingIdentityServiceTestClient(t)
	ctx := context.Background()

	session, err := svc.CreatePendingSession(ctx, CreatePendingAuthSessionInput{
		Intent: "login",
		Identity: PendingAuthIdentityKey{
			ProviderType:    "linuxdo",
			ProviderKey:     "linuxdo",
			ProviderSubject: "subject-session-token",
		},
		BrowserSessionKey: "browser-session",
		LocalFlowState: map[string]any{
			"completion_response": map[string]any{
				"access_token": "token",
			},
		},
	})
	require.NoError(t, err)

	_, err = svc.ConsumeBrowserSession(ctx, session.SessionToken, "browser-other")
	require.ErrorIs(t, err, ErrPendingAuthBrowserMismatch)

	consumed, err := svc.ConsumeBrowserSession(ctx, session.SessionToken, "browser-session")
	require.NoError(t, err)
	require.NotNil(t, consumed.ConsumedAt)

	_, err = svc.ConsumeBrowserSession(ctx, session.SessionToken, "browser-session")
	require.ErrorIs(t, err, ErrPendingAuthSessionConsumed)
}
