package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func newAccountCreateProxyGuardRepository(t *testing.T) (*accountRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })
	return newAccountRepositoryWithSQL(client, db, nil), mock
}

func accountCreateProxyGuardAccount(proxyID int64) *service.Account {
	return &service.Account{
		Name:         "guarded-account",
		Platform:     service.PlatformOpenAI,
		AccountLevel: service.AccountLevelPro,
		Type:         service.AccountTypeAPIKey,
		Credentials:  map[string]any{"api_key": "test-key"},
		Extra:        map[string]any{},
		ProxyID:      &proxyID,
		Concurrency:  1,
		Priority:     1,
		Status:       service.StatusActive,
		Schedulable:  true,
	}
}

func expectAccountCreateProxyLock(
	mock sqlmock.Sqlmock,
	proxyID int64,
	ownerUserID any,
	platform, accountLevel, status string,
	maxAccounts int,
) {
	mock.ExpectQuery(`(?s)SELECT id, owner_user_id, platform, required_account_level, status, max_accounts, expires_at.*FROM proxies.*deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "platform", "required_account_level", "status", "max_accounts", "expires_at",
		}).AddRow(proxyID, ownerUserID, platform, accountLevel, status, maxAccounts, nil))
}

func expectAccountCreateProxyCount(mock sqlmock.Sqlmock, proxyID, current int64) {
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1.*deleted_at IS NULL`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(current))
}

func TestAccountCreateEntrypointsCannotBypassLockedProxyCapacity(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(context.Context, *accountRepository, *service.Account) error
	}{
		{
			name: "ordinary create",
			invoke: func(ctx context.Context, repo *accountRepository, account *service.Account) error {
				return repo.Create(ctx, account)
			},
		},
		{
			name: "admin duplicate create with groups",
			invoke: func(ctx context.Context, repo *accountRepository, account *service.Account) error {
				return repo.CreateWithAccountGroups(ctx, account, []service.AccountGroup{{GroupID: 3, Priority: 1}})
			},
		},
		{
			name: "owned create",
			invoke: func(ctx context.Context, repo *accountRepository, account *service.Account) error {
				ownerUserID := int64(41)
				account.OwnerUserID = &ownerUserID
				return repo.CreateOwnedWithProxyCapacity(ctx, ownerUserID, account)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo, mock := newAccountCreateProxyGuardRepository(t)
			account := accountCreateProxyGuardAccount(7)

			mock.ExpectBegin()
			expectAccountCreateProxyLock(mock, 7, nil, service.PlatformOpenAI, service.AccountLevelPro, service.StatusActive, 1)
			expectAccountCreateProxyCount(mock, 7, 1)
			mock.ExpectRollback()

			err := test.invoke(context.Background(), repo, account)

			require.ErrorIs(t, err, service.ErrProxyAccountLimitExceeded)
			require.Zero(t, account.ID)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCRSAccountCreateRequiresActivePublicMatchingProxyScope(t *testing.T) {
	tests := []struct {
		name          string
		ownerUserID   any
		proxyPlatform string
		proxyLevel    string
		proxyStatus   string
	}{
		{name: "inactive", proxyPlatform: service.PlatformOpenAI, proxyLevel: service.AccountLevelPro, proxyStatus: service.StatusDisabled},
		{name: "private", ownerUserID: int64(41), proxyPlatform: service.PlatformOpenAI, proxyLevel: service.AccountLevelPro, proxyStatus: service.StatusActive},
		{name: "wrong platform", proxyPlatform: service.PlatformGemini, proxyLevel: service.AccountLevelPro, proxyStatus: service.StatusActive},
		{name: "wrong level", proxyPlatform: service.PlatformOpenAI, proxyLevel: service.AccountLevelTeam, proxyStatus: service.StatusActive},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo, mock := newAccountCreateProxyGuardRepository(t)
			account := accountCreateProxyGuardAccount(7)
			account.Extra["crs_account_id"] = "remote-account-7"

			mock.ExpectBegin()
			expectAccountCreateProxyLock(mock, 7, test.ownerUserID, test.proxyPlatform, test.proxyLevel, test.proxyStatus, 0)
			mock.ExpectRollback()

			err := repo.Create(context.Background(), account)

			require.ErrorIs(t, err, service.ErrProxyNotFound)
			require.Zero(t, account.ID)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestOwnedAccountCreateRevalidatesResolvedLevelAgainstLockedProxy(t *testing.T) {
	repo, mock := newAccountCreateProxyGuardRepository(t)
	account := accountCreateProxyGuardAccount(7)
	ownerUserID := int64(41)
	account.OwnerUserID = &ownerUserID

	mock.ExpectBegin()
	expectAccountCreateProxyLock(mock, 7, nil, service.PlatformOpenAI, service.AccountLevelTeam, service.StatusActive, 0)
	mock.ExpectRollback()

	err := repo.CreateOwnedWithProxyCapacity(context.Background(), ownerUserID, account)

	require.ErrorIs(t, err, service.ErrProxyNotFound)
	require.Zero(t, account.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountCreateRejectsExpiredProxyBeforeCapacityOrInsert(t *testing.T) {
	repo, mock := newAccountCreateProxyGuardRepository(t)
	account := accountCreateProxyGuardAccount(7)
	expiredAt := time.Now().Add(-time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, owner_user_id, platform, required_account_level, status, max_accounts, expires_at.*FROM proxies.*deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "platform", "required_account_level", "status", "max_accounts", "expires_at",
		}).AddRow(int64(7), nil, service.PlatformOpenAI, service.AccountLevelPro, service.StatusActive, 0, expiredAt))
	mock.ExpectRollback()

	err := repo.Create(context.Background(), account)

	require.ErrorIs(t, err, service.ErrProxyNotFound)
	require.Zero(t, account.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOrdinaryAccountCreateProxyGuardRequiresOwnerActiveAndScope(t *testing.T) {
	t.Run("exclusive proxy rejects a platform account", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		account := accountCreateProxyGuardAccount(7)
		expectAccountCreateProxyLock(mock, 7, int64(41), service.PlatformGemini, service.AccountLevelTeam, service.StatusDisabled, 0)

		err = ensureAccountCreateProxyBindingInTx(context.Background(), db, accountCreateTransactionInput{account: account})

		require.ErrorIs(t, err, service.ErrProxyOwnerConflict)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("inactive public proxy is rejected before capacity", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		account := accountCreateProxyGuardAccount(7)
		expectAccountCreateProxyLock(mock, 7, nil, service.PlatformOpenAI, service.AccountLevelPro, service.StatusDisabled, 2)

		err = ensureAccountCreateProxyBindingInTx(context.Background(), db, accountCreateTransactionInput{account: account})

		require.ErrorIs(t, err, service.ErrProxyNotFound)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("wrong public proxy scope is rejected before capacity", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		account := accountCreateProxyGuardAccount(7)
		expectAccountCreateProxyLock(mock, 7, nil, service.PlatformGemini, service.AccountLevelTeam, service.StatusActive, 2)

		err = ensureAccountCreateProxyBindingInTx(context.Background(), db, accountCreateTransactionInput{account: account})

		require.ErrorIs(t, err, service.ErrProxyNotFound)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("active matching public proxy proceeds to capacity", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		account := accountCreateProxyGuardAccount(7)
		expectAccountCreateProxyLock(mock, 7, nil, service.PlatformOpenAI, service.AccountLevelPro, service.StatusActive, 2)
		expectAccountCreateProxyCount(mock, 7, 1)

		err = ensureAccountCreateProxyBindingInTx(context.Background(), db, accountCreateTransactionInput{account: account})

		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestProxiedCreateRollsBackWhenTransactionalOutboxWriteFails(t *testing.T) {
	repo, mock := newAccountCreateProxyGuardRepository(t)
	account := accountCreateProxyGuardAccount(7)
	account.GroupIDs = []int64{11}
	before := cloneAccountForCreate(account)

	mock.ExpectBegin()
	expectAccountCreateProxyLock(mock, 7, nil, service.PlatformOpenAI, service.AccountLevelPro, service.StatusActive, 0)
	mock.ExpectQuery(`(?s)INSERT INTO "accounts"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(91)))
	mock.ExpectExec(`(?s)INSERT INTO scheduler_outbox`).
		WillReturnError(errors.New("outbox unavailable"))
	mock.ExpectRollback()

	err := repo.Create(context.Background(), account)

	require.ErrorContains(t, err, "outbox unavailable")
	require.Equal(t, before, account, "failed transaction must not publish phantom account state")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateWithAccountGroupsRollsBackAccountGroupsAndOutboxTogether(t *testing.T) {
	repo, mock := newAccountCreateProxyGuardRepository(t)
	account := accountCreateProxyGuardAccount(7)
	account.GroupIDs = []int64{11}
	account.AccountGroups = []service.AccountGroup{{AccountID: 0, GroupID: 11, Priority: 2}}
	before := cloneAccountForCreate(account)
	groups := []service.AccountGroup{{GroupID: 3, Priority: 1}}
	groupsBefore := append([]service.AccountGroup(nil), groups...)

	mock.ExpectBegin()
	expectAccountCreateProxyLock(mock, 7, nil, service.PlatformOpenAI, service.AccountLevelPro, service.StatusActive, 0)
	mock.ExpectQuery(`(?s)INSERT INTO "accounts"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(91)))
	mock.ExpectExec(`(?s)INSERT INTO "account_groups"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO scheduler_outbox`).
		WillReturnError(errors.New("outbox unavailable"))
	mock.ExpectRollback()

	err := repo.CreateWithAccountGroups(context.Background(), account, groups)

	require.ErrorContains(t, err, "outbox unavailable")
	require.Equal(t, before, account, "failed transaction must not publish account or group state")
	require.Equal(t, groupsBefore, groups, "failed transaction must not mutate caller-owned group input")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxiedCreateUsesOneTransactionForLockCountAccountAndOutbox(t *testing.T) {
	repo, mock := newAccountCreateProxyGuardRepository(t)
	account := accountCreateProxyGuardAccount(7)

	mock.ExpectBegin()
	expectAccountCreateProxyLock(mock, 7, nil, service.PlatformOpenAI, service.AccountLevelPro, service.StatusActive, 2)
	expectAccountCreateProxyCount(mock, 7, 1)
	mock.ExpectQuery(`(?s)INSERT INTO "accounts"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(91)))
	mock.ExpectExec(`(?s)INSERT INTO scheduler_outbox`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := repo.Create(context.Background(), account)

	require.NoError(t, err)
	require.Equal(t, int64(91), account.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxiedCreateReusesOuterUnitOfWorkTransaction(t *testing.T) {
	repo, mock := newAccountCreateProxyGuardRepository(t)
	account := accountCreateProxyGuardAccount(7)

	mock.ExpectBegin()
	outerTx, err := repo.client.Tx(context.Background())
	require.NoError(t, err)
	outerCtx := dbent.NewTxContext(context.Background(), outerTx)
	expectAccountCreateProxyLock(mock, 7, nil, service.PlatformOpenAI, service.AccountLevelPro, service.StatusActive, 0)
	mock.ExpectQuery(`(?s)INSERT INTO "accounts"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(91)))
	mock.ExpectExec(`(?s)INSERT INTO scheduler_outbox`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = repo.Create(outerCtx, account)

	require.NoError(t, err)
	require.Equal(t, int64(91), account.ID)
	mock.ExpectRollback()
	require.NoError(t, outerTx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWithCRSProxyAccountUnitOfWorkOwnsOnlyTransactionsItStarts(t *testing.T) {
	t.Run("starts and commits one transaction", func(t *testing.T) {
		repo, mock := newAccountCreateProxyGuardRepository(t)
		mock.ExpectBegin()
		mock.ExpectCommit()
		called := false

		err := repo.WithCRSProxyAccountUnitOfWork(context.Background(), func(ctx context.Context) error {
			called = true
			require.NotNil(t, dbent.TxFromContext(ctx))
			require.NotSame(t, repo.client, clientFromContext(ctx, repo.client))
			return nil
		})

		require.NoError(t, err)
		require.True(t, called)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("reuses outer transaction without committing it", func(t *testing.T) {
		repo, mock := newAccountCreateProxyGuardRepository(t)
		mock.ExpectBegin()
		outerTx, err := repo.client.Tx(context.Background())
		require.NoError(t, err)
		outerCtx := dbent.NewTxContext(context.Background(), outerTx)
		called := false

		err = repo.WithCRSProxyAccountUnitOfWork(outerCtx, func(ctx context.Context) error {
			called = true
			require.Same(t, outerTx, dbent.TxFromContext(ctx))
			require.Same(t, outerTx.Client(), clientFromContext(ctx, repo.client))
			return nil
		})

		require.NoError(t, err)
		require.True(t, called)
		mock.ExpectRollback()
		require.NoError(t, outerTx.Rollback())
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestAccountRepositoryUpdateStatusAndErrorIsAtomic(t *testing.T) {
	repo, mock := newAccountCreateProxyGuardRepository(t)
	accountID := int64(41)
	status := service.StatusError
	errorMessage := "token expired"

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts.*SET status = \$2.*error_message = \$3.*error_since.*updated_at = NOW\(\).*WHERE id = \$1`).
		WithArgs(accountID, status, errorMessage, service.StatusError).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO scheduler_outbox`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.UpdateStatusAndError(context.Background(), accountID, status, errorMessage)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountRepositoryUpdateStatusAndErrorRollsBackWhenOutboxFails(t *testing.T) {
	repo, mock := newAccountCreateProxyGuardRepository(t)
	accountID := int64(42)

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts.*SET status = \$2`).
		WithArgs(accountID, service.StatusDisabled, "disabled", service.StatusError).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO scheduler_outbox`).
		WillReturnError(errors.New("outbox unavailable"))
	mock.ExpectRollback()

	err := repo.UpdateStatusAndError(context.Background(), accountID, service.StatusDisabled, "disabled")

	require.ErrorContains(t, err, "outbox unavailable")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountRepositoryBindGroupsReadsExistingGroupsInsideTransaction(t *testing.T) {
	repo, mock := newAccountCreateProxyGuardRepository(t)
	accountID := int64(43)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT .*account_id.*group_id.*priority.*created_at.*FROM "account_groups".*WHERE .*account_id.*ORDER BY`).
		WithArgs(accountID).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "group_id", "priority", "created_at"}).
			AddRow(accountID, int64(7), 1, time.Now()))
	mock.ExpectExec(`(?s)DELETE FROM "account_groups"`).
		WithArgs(accountID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO "account_groups"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO scheduler_outbox`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.BindGroups(context.Background(), accountID, []int64{9})

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
