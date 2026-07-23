package repository

import (
	"context"
	"database/sql"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestPublicGroupSchedulableAccountPredicateRequiresApprovedOwnedShare(t *testing.T) {
	db, err := sql.Open("sqlite", "file:account_repo_public_share_predicate?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	owner, err := client.User.Create().
		SetEmail("public-share-predicate@example.com").
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	ownerID := owner.ID
	accounts := []*dbent.Account{
		createPublicSharePredicateTestAccount(t, ctx, client, "system", nil, service.AccountShareModePrivate, service.AccountShareStatusApproved),
		createPublicSharePredicateTestAccount(t, ctx, client, "owned-approved", &ownerID, service.AccountShareModePublic, service.AccountShareStatusApproved),
		createPublicSharePredicateTestAccount(t, ctx, client, "owned-pending", &ownerID, service.AccountShareModePublic, service.AccountShareStatusPending),
		createPublicSharePredicateTestAccount(t, ctx, client, "owned-suspended", &ownerID, service.AccountShareModePublic, service.AccountShareStatusSuspended),
		createPublicSharePredicateTestAccount(t, ctx, client, "owned-private", &ownerID, service.AccountShareModePrivate, service.AccountShareStatusApproved),
	}

	ids, err := client.Account.Query().
		Where(publicGroupSchedulableAccountPredicate()).
		IDs(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{accounts[0].ID, accounts[1].ID}, ids)
}

func createPublicSharePredicateTestAccount(
	t *testing.T,
	ctx context.Context,
	client *dbent.Client,
	name string,
	ownerUserID *int64,
	shareMode string,
	shareStatus string,
) *dbent.Account {
	t.Helper()
	builder := client.Account.Create().
		SetName(name).
		SetPlatform(service.PlatformOpenAI).
		SetType(service.AccountTypeOAuth).
		SetShareMode(shareMode).
		SetShareStatus(shareStatus).
		SetStatus(service.StatusActive).
		SetSchedulable(true)
	if ownerUserID != nil {
		builder.SetOwnerUserID(*ownerUserID)
	}
	account, err := builder.Save(ctx)
	require.NoError(t, err)
	return account
}
