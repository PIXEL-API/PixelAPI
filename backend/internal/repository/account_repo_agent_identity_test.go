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

func TestAccountRepositoryGetOwnedOpenAIAgentIdentityByChatGPTAccountID(t *testing.T) {
	db, err := sql.Open("sqlite", "file:account_repo_agent_identity?mode=memory&cache=shared&_fk=1")
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

	ownerA := createAgentIdentityRepositoryTestUser(t, ctx, client, "agent-owner-a@example.com")
	ownerB := createAgentIdentityRepositoryTestUser(t, ctx, client, "agent-owner-b@example.com")
	target := createAgentIdentityRepositoryTestAccount(t, ctx, client, ownerA, "target", map[string]any{
		"auth_mode":          service.OpenAIAuthModeAgentIdentity,
		"chatgpt_account_id": "team-a",
		"chatgpt_user_id":    "member-a",
		"agent_runtime_id":   "runtime-a",
	})
	otherOwner := createAgentIdentityRepositoryTestAccount(t, ctx, client, ownerB, "other-owner", map[string]any{
		"auth_mode":          service.OpenAIAuthModeAgentIdentity,
		"chatgpt_account_id": "team-a",
		"chatgpt_user_id":    "member-b",
		"agent_runtime_id":   "runtime-b",
	})
	createAgentIdentityRepositoryTestAccount(t, ctx, client, ownerA, "normal-oauth", map[string]any{
		"access_token":       "token",
		"chatgpt_account_id": "team-a",
	})
	createAgentIdentityRepositoryTestAccount(t, ctx, client, ownerA, "other-team", map[string]any{
		"auth_mode":          service.OpenAIAuthModeAgentIdentity,
		"chatgpt_account_id": "team-b",
		"chatgpt_user_id":    "member-a",
		"agent_runtime_id":   "runtime-c",
	})
	nonCanonical := createAgentIdentityRepositoryTestAccount(t, ctx, client, ownerA, "non-canonical", map[string]any{
		"auth_mode":          " AGENTIDENTITY ",
		"chatgpt_account_id": " team-non-canonical ",
		"chatgpt_user_id":    "member-non-canonical",
		"agent_runtime_id":   "runtime-non-canonical",
	})
	deleted := createAgentIdentityRepositoryTestAccount(t, ctx, client, ownerA, "deleted", map[string]any{
		"auth_mode":          service.OpenAIAuthModeAgentIdentity,
		"chatgpt_account_id": "team-deleted",
		"chatgpt_user_id":    "member-a",
		"agent_runtime_id":   "runtime-deleted",
	})
	_, err = client.Account.UpdateOneID(deleted.ID).SetDeletedAt(time.Now().UTC()).Save(ctx)
	require.NoError(t, err)

	got, err := repo.GetOwnedOpenAIAgentIdentityByChatGPTAccountID(ctx, ownerA, " team-a ")
	require.NoError(t, err)
	require.Equal(t, target.ID, got.ID)
	require.Equal(t, int64(ownerA), *got.OwnerUserID)
	require.Equal(t, "member-a", got.GetCredential("chatgpt_user_id"))

	got, err = repo.GetOwnedOpenAIAgentIdentityByChatGPTAccountID(ctx, ownerB, "team-a")
	require.NoError(t, err)
	require.Equal(t, otherOwner.ID, got.ID)

	got, err = repo.GetOwnedOpenAIAgentIdentityByChatGPTAccountID(ctx, ownerA, "team-non-canonical")
	require.NoError(t, err)
	require.Equal(t, nonCanonical.ID, got.ID)

	for _, test := range []struct {
		name      string
		ownerID   int64
		accountID string
	}{
		{name: "unknown team", ownerID: ownerA, accountID: "team-missing"},
		{name: "soft deleted", ownerID: ownerA, accountID: "team-deleted"},
		{name: "invalid owner", ownerID: 0, accountID: "team-a"},
		{name: "empty team", ownerID: ownerA, accountID: "  "},
	} {
		t.Run(test.name, func(t *testing.T) {
			account, err := repo.GetOwnedOpenAIAgentIdentityByChatGPTAccountID(ctx, test.ownerID, test.accountID)
			require.Nil(t, account)
			require.NoError(t, err)
		})
	}
}

func createAgentIdentityRepositoryTestUser(t *testing.T, ctx context.Context, client *dbent.Client, email string) int64 {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	return user.ID
}

func createAgentIdentityRepositoryTestAccount(
	t *testing.T,
	ctx context.Context,
	client *dbent.Client,
	ownerUserID int64,
	name string,
	credentials map[string]any,
) *dbent.Account {
	t.Helper()
	account, err := client.Account.Create().
		SetName(name).
		SetPlatform(service.PlatformOpenAI).
		SetType(service.AccountTypeOAuth).
		SetOwnerUserID(ownerUserID).
		SetCredentials(credentials).
		Save(ctx)
	require.NoError(t, err)
	return account
}
