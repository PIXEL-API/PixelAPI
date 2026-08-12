package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestAccountRepositoryGetOwnedOpenAIPersonalAccessTokenByChatGPTUserID(t *testing.T) {
	db, err := sql.Open("sqlite", "file:account_repo_pat?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAccountRepositoryWithSQL(client, db, nil)
	ctx := context.Background()

	ownerA := createAgentIdentityRepositoryTestUser(t, ctx, client, "pat-owner-a@example.com")
	ownerB := createAgentIdentityRepositoryTestUser(t, ctx, client, "pat-owner-b@example.com")
	target := createAgentIdentityRepositoryTestAccount(t, ctx, client, ownerA, "target", map[string]any{
		"auth_mode":       service.OpenAIAuthModePersonalAccessToken,
		"chatgpt_user_id": "member-a",
		"access_token":    "at-test-target",
	})
	otherOwner := createAgentIdentityRepositoryTestAccount(t, ctx, client, ownerB, "other owner", map[string]any{
		"openai_auth_mode": "personal_access_token",
		"chatgpt_user_id":  "member-a",
		"access_token":     "at-test-other-owner",
	})
	createAgentIdentityRepositoryTestAccount(t, ctx, client, ownerA, "ordinary OAuth", map[string]any{
		"chatgpt_user_id": "member-oauth",
		"access_token":    "oauth-access",
		"refresh_token":   "oauth-refresh",
	})
	nonCanonical := createAgentIdentityRepositoryTestAccount(t, ctx, client, ownerA, "non canonical", map[string]any{
		"auth_mode":       " PERSONAL_ACCESS_TOKEN ",
		"chatgpt_user_id": " member-non-canonical ",
		"access_token":    "at-test-non-canonical",
	})
	deleted := createAgentIdentityRepositoryTestAccount(t, ctx, client, ownerA, "deleted", map[string]any{
		"auth_mode":       service.OpenAIAuthModePersonalAccessToken,
		"chatgpt_user_id": "member-deleted",
		"access_token":    "at-test-deleted",
	})
	_, err = client.Account.UpdateOneID(deleted.ID).SetDeletedAt(time.Now().UTC()).Save(ctx)
	require.NoError(t, err)

	got, err := repo.GetOwnedOpenAIPersonalAccessTokenByChatGPTUserID(ctx, ownerA, " member-a ")
	require.NoError(t, err)
	require.Equal(t, target.ID, got.ID)

	got, err = repo.GetOwnedOpenAIPersonalAccessTokenByChatGPTUserID(ctx, ownerB, "member-a")
	require.NoError(t, err)
	require.Equal(t, otherOwner.ID, got.ID)

	got, err = repo.GetOwnedOpenAIPersonalAccessTokenByChatGPTUserID(ctx, ownerA, "member-non-canonical")
	require.NoError(t, err)
	require.Equal(t, nonCanonical.ID, got.ID)

	for _, test := range []struct {
		name    string
		ownerID int64
		userID  string
	}{
		{name: "ordinary OAuth", ownerID: ownerA, userID: "member-oauth"},
		{name: "soft deleted", ownerID: ownerA, userID: "member-deleted"},
		{name: "invalid owner", ownerID: 0, userID: "member-a"},
		{name: "empty user", ownerID: ownerA, userID: "  "},
	} {
		t.Run(test.name, func(t *testing.T) {
			account, err := repo.GetOwnedOpenAIPersonalAccessTokenByChatGPTUserID(ctx, test.ownerID, test.userID)
			require.NoError(t, err)
			require.Nil(t, account)
		})
	}
}
