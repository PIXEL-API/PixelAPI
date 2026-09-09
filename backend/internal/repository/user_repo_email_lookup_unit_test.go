package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestUserRepositoryDeleteRequiresAccountShareSettlement(t *testing.T) {
	for _, tc := range []struct {
		name           string
		owner          bool
		status         string
		deletedListing bool
		unrelated      bool
		blocked        bool
	}{
		{name: "consumer active", status: "active", blocked: true},
		{name: "consumer ending", status: "ending", blocked: true},
		{name: "owner active", owner: true, status: "active", blocked: true},
		{name: "owner ending", owner: true, status: "ending", blocked: true},
		{name: "deleted room still unsettled", owner: true, status: "ending", deletedListing: true, blocked: true},
		{name: "settled consumer", status: "ended"},
		{name: "settled owner", owner: true, status: "ended"},
		{name: "unrelated membership", status: "active", unrelated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, client := newUserEntRepo(t)
			ctx := context.Background()
			user, err := client.User.Create().SetEmail("delete@example.com").SetPasswordHash("hash").Save(ctx)
			require.NoError(t, err)
			_, err = repo.sql.ExecContext(ctx, `CREATE TABLE account_share_listings (id INTEGER PRIMARY KEY, owner_user_id BIGINT, deleted_at TIMESTAMP)`)
			require.NoError(t, err)
			_, err = repo.sql.ExecContext(ctx, `CREATE TABLE account_share_memberships (id INTEGER PRIMARY KEY, listing_id BIGINT, consumer_user_id BIGINT, status TEXT, deleted_at TIMESTAMP)`)
			require.NoError(t, err)
			ownerID, consumerID := int64(101), user.ID
			if tc.owner {
				ownerID, consumerID = user.ID, 102
			}
			if tc.unrelated {
				ownerID, consumerID = 101, 102
			}
			var deletedAt any
			if tc.deletedListing {
				deletedAt = time.Now()
			}
			_, err = repo.sql.ExecContext(ctx, `INSERT INTO account_share_listings (id, owner_user_id, deleted_at) VALUES (1, $1, $2)`, ownerID, deletedAt)
			require.NoError(t, err)
			_, err = repo.sql.ExecContext(ctx, `INSERT INTO account_share_memberships (id, listing_id, consumer_user_id, status) VALUES (1, 1, $1, $2)`, consumerID, tc.status)
			require.NoError(t, err)

			err = repo.Delete(ctx, user.ID)
			if tc.blocked {
				require.ErrorIs(t, err, service.ErrUserAccountShareUnsettled)
				_, err = repo.GetByID(ctx, user.ID)
				require.NoError(t, err, "a blocked deletion must leave the user available for settlement")
			} else {
				require.NoError(t, err)
				_, err = repo.GetByID(ctx, user.ID)
				require.ErrorIs(t, err, service.ErrUserNotFound)
			}
		})
	}
}

func TestUserRepositoryDeleteLocksUserBeforeCheckingSettlement(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := newUserRepositoryWithSQL(client, db)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "users".*FOR UPDATE`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery(`(?s)SELECT EXISTS .*m.consumer_user_id = \$1.* OR EXISTS .*l.owner_user_id = \$1`).
		WithArgs(int64(7), "active", "ending").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()
	require.ErrorIs(t, repo.Delete(context.Background(), 7), service.ErrUserAccountShareUnsettled)
	require.NoError(t, mock.ExpectationsWereMet())
}

func newUserEntRepo(t *testing.T) (*userRepository, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(10)

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

func TestUserRepositoryCreateSerializesNormalizedEmailConflictsUnderConcurrency(t *testing.T) {
	repo, client := newUserEntRepo(t)
	ctx := context.Background()

	firstCreateStarted := make(chan struct{})
	releaseFirstCreate := make(chan struct{})
	var firstCreate sync.Once
	client.User.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
			blocked := false
			if m.Op().Is(dbent.OpCreate) {
				firstCreate.Do(func() {
					blocked = true
					close(firstCreateStarted)
				})
			}
			if blocked {
				<-releaseFirstCreate
			}
			return next.Mutate(ctx, m)
		})
	})

	type createResult struct {
		err error
	}

	results := make(chan createResult, 2)
	go func() {
		results <- createResult{err: repo.Create(ctx, &service.User{
			Email:        " Race@Example.com ",
			Username:     "race-user-1",
			PasswordHash: "hash",
			Role:         service.RoleUser,
			Status:       service.StatusActive,
		})}
	}()

	<-firstCreateStarted

	go func() {
		results <- createResult{err: repo.Create(ctx, &service.User{
			Email:        "race@example.com",
			Username:     "race-user-2",
			PasswordHash: "hash",
			Role:         service.RoleUser,
			Status:       service.StatusActive,
		})}
	}()

	time.Sleep(100 * time.Millisecond)
	close(releaseFirstCreate)

	first := <-results
	second := <-results

	errors := []error{first.err, second.err}
	successes := 0
	conflicts := 0
	for _, err := range errors {
		switch err {
		case nil:
			successes++
		case service.ErrEmailExists:
			conflicts++
		default:
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	count, err := client.User.Query().Where(userEmailLookupPredicate("race@example.com")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
