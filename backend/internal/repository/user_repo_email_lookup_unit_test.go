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

func newUserEntRepo(t *testing.T) (*userRepository, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:user_repo_email_lookup?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return newUserRepositoryWithSQL(client, db), client
}

func TestUserRepositoryGetByEmailNormalizesLegacySpacingAndCase(t *testing.T) {
	repo, _ := newUserEntRepo(t)
	ctx := context.Background()

	err := repo.Create(ctx, &service.User{
		Email:        " Legacy@Example.com ",
		Username:     "legacy-user",
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	require.NoError(t, err)

	got, err := repo.GetByEmail(ctx, "legacy@example.com")
	require.NoError(t, err)
	require.Equal(t, " Legacy@Example.com ", got.Email)
}

func TestUserRepositoryExistsByEmailNormalizesLegacySpacingAndCase(t *testing.T) {
	repo, _ := newUserEntRepo(t)
	ctx := context.Background()

	err := repo.Create(ctx, &service.User{
		Email:        " Legacy@Example.com ",
		Username:     "legacy-user",
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	require.NoError(t, err)

	exists, err := repo.ExistsByEmail(ctx, "  LEGACY@example.com  ")
	require.NoError(t, err)
	require.True(t, exists)
}

func TestUserRepositoryCreateRejectsNormalizedEmailDuplicate(t *testing.T) {
	repo, _ := newUserEntRepo(t)
	ctx := context.Background()

	err := repo.Create(ctx, &service.User{
		Email:        " Existing@Example.com ",
		Username:     "existing-user",
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	require.NoError(t, err)

	err = repo.Create(ctx, &service.User{
		Email:        "existing@example.com",
		Username:     "duplicate-user",
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	require.ErrorIs(t, err, service.ErrEmailExists)
}

func TestUserRepositoryUpdateRejectsNormalizedEmailDuplicate(t *testing.T) {
	repo, _ := newUserEntRepo(t)
	ctx := context.Background()

	first := &service.User{
		Email:        " Existing@Example.com ",
		Username:     "existing-user",
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, first))

	second := &service.User{
		Email:        "second@example.com",
		Username:     "second-user",
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, second))

	second.Email = " existing@example.com "
	err := repo.Update(ctx, second)
	require.ErrorIs(t, err, service.ErrEmailExists)
}

func TestUserRepositoryGetByEmailReportsNormalizedEmailConflict(t *testing.T) {
	repo, client := newUserEntRepo(t)
	ctx := context.Background()

	_, err := client.User.Create().
		SetEmail("Conflict@Example.com").
		SetUsername("conflict-user-1").
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.User.Create().
		SetEmail(" conflict@example.com ").
		SetUsername("conflict-user-2").
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	_, err = repo.GetByEmail(ctx, "conflict@example.com")
	require.Error(t, err)
	require.ErrorContains(t, err, "normalized email lookup matched multiple users")
}
