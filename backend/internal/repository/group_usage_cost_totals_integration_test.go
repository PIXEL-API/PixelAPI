//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const (
	groupCostCatchupMigration = "271_group_usage_cost_catchup.sql"
	groupCostTotalsMigration  = "272_group_usage_cost_totals.sql"
)

func TestGroupUsageCostTotalsTracksCommittedInsertsExactlyOnce(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	unique := uuid.NewString()
	user := mustCreateUser(t, client, &service.User{Email: "group-cost-" + unique + "@example.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-group-cost-" + unique, Name: "k"})
	account := mustCreateAccount(t, client, &service.Account{Name: "group-cost-" + unique})
	group := mustCreateGroup(t, client, &service.Group{Name: "group-cost-" + unique})

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM usage_logs WHERE api_key_id = $1", apiKey.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM group_usage_cost_totals WHERE group_id = $1", group.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM api_keys WHERE id = $1", apiKey.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id = $1", group.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	now := time.Now().UTC()
	requestID := "group-cost-single-" + unique
	var firstLogID int64
	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO usage_logs (
			user_id, api_key_id, account_id, request_id, model, group_id,
			input_tokens, output_tokens, total_cost, actual_cost, created_at
		) VALUES ($1, $2, $3, $4, 'test-model', $5, 1, 1, 0.75, 0.75, $6)
		RETURNING id
	`, user.ID, apiKey.ID, account.ID, requestID, group.ID, now).Scan(&firstLogID)
	require.NoError(t, err)

	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO usage_logs (
			user_id, api_key_id, account_id, request_id, model, group_id,
			input_tokens, output_tokens, total_cost, actual_cost, created_at
		) VALUES
			($1, $2, $3, $4, 'test-model', $6, 1, 1, 0.50, 0.50, $7),
			($1, $2, $3, $5, 'test-model', $6, 1, 1, 0.75, 0.75, $7)
	`, user.ID, apiKey.ID, account.ID, "group-cost-batch-a-"+unique, "group-cost-batch-b-"+unique, group.ID, now)
	require.NoError(t, err)

	result, err := integrationDB.ExecContext(ctx, `
		INSERT INTO usage_logs (
			user_id, api_key_id, account_id, request_id, model, group_id,
			input_tokens, output_tokens, total_cost, actual_cost, created_at
		) VALUES ($1, $2, $3, $4, 'test-model', $5, 1, 1, 99, 99, $6)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
	`, user.ID, apiKey.ID, account.ID, requestID, group.ID, now)
	require.NoError(t, err)
	rowsAffected, err := result.RowsAffected()
	require.NoError(t, err)
	require.Zero(t, rowsAffected)

	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO usage_logs (
			user_id, api_key_id, account_id, request_id, model, group_id,
			input_tokens, output_tokens, total_cost, actual_cost, created_at
		) VALUES ($1, $2, $3, $4, 'test-model', NULL, 1, 1, 50, 50, $5)
	`, user.ID, apiKey.ID, account.ID, "group-cost-null-"+unique, now)
	require.NoError(t, err)

	repo := newUsageLogRepositoryWithSQL(client, integrationDB)
	summaries, err := repo.GetAllGroupUsageSummary(ctx, now.Add(-time.Minute), []int64{group.ID})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, group.ID, summaries[0].GroupID)
	require.InDelta(t, 2.0, summaries[0].TotalCost, 0.000000001)
	require.InDelta(t, 2.0, summaries[0].TodayCost, 0.000000001)

	_, err = integrationDB.ExecContext(ctx, "DELETE FROM usage_logs WHERE id = $1", firstLogID)
	require.NoError(t, err)
	var totalAfterRawDelete float64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT total_cost FROM group_usage_cost_totals WHERE group_id = $1", group.ID,
	).Scan(&totalAfterRawDelete))
	require.InDelta(t, 2.0, totalAfterRawDelete, 0.000000001,
		fmt.Sprintf("group %d cumulative total must not fall when retained raw rows are deleted", group.ID))
}

func TestGroupUsageCostTotalsCutoverIncludesConcurrentCommitExactlyOnce(t *testing.T) {
	ctx := context.Background()
	schema := createIsolatedGroupCostSchema(t)
	require.NoError(t, insertIsolatedUsageLog(ctx, schema, 7, 1))

	applyIsolatedGroupCostMigration(t, schema, groupCostCatchupMigration)

	writer := openGroupCostSchemaConn(t, schema)
	writerTx, err := writer.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerTx.Rollback() })
	_, err = writerTx.ExecContext(ctx, "INSERT INTO usage_logs (group_id, actual_cost) VALUES (7, 2)")
	require.NoError(t, err)

	migrationConn := openGroupCostSchemaConn(t, schema)
	migrationPID := postgresBackendPID(t, migrationConn)
	result := make(chan error, 1)
	go func() {
		result <- executeIsolatedGroupCostMigration(ctx, migrationConn, groupCostTotalsMigration)
	}()

	requireMigrationWaitingForUsageLogLock(t, schema, migrationPID)
	require.NoError(t, writerTx.Commit())
	require.NoError(t, receiveMigrationResult(t, result))

	require.InDelta(t, 3, isolatedGroupCostTotal(t, schema, 7), 0.000000001)
	require.NoError(t, insertIsolatedUsageLog(ctx, schema, 7, 4))
	require.InDelta(t, 7, isolatedGroupCostTotal(t, schema, 7), 0.000000001)
}

func TestGroupUsageCostTotalsCutoverCanRetryAfterCancellation(t *testing.T) {
	ctx := context.Background()
	schema := createIsolatedGroupCostSchema(t)
	require.NoError(t, insertIsolatedUsageLog(ctx, schema, 9, 1))

	applyIsolatedGroupCostMigration(t, schema, groupCostCatchupMigration)

	writer := openGroupCostSchemaConn(t, schema)
	writerTx, err := writer.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = writerTx.Rollback() })
	_, err = writerTx.ExecContext(ctx, "INSERT INTO usage_logs (group_id, actual_cost) VALUES (9, 2)")
	require.NoError(t, err)

	migrationConn := openGroupCostSchemaConn(t, schema)
	migrationPID := postgresBackendPID(t, migrationConn)
	migrationCtx, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		result <- executeIsolatedGroupCostMigration(migrationCtx, migrationConn, groupCostTotalsMigration)
	}()

	requireMigrationWaitingForUsageLogLock(t, schema, migrationPID)
	cancel()
	require.Error(t, receiveMigrationResult(t, result))
	require.NoError(t, writerTx.Commit())

	require.Equal(t, int64(1), isolatedCatchupRowCount(t, schema),
		"migration 271 must keep capturing after migration 272 rolls back")
	require.True(t, isolatedRelationExists(t, schema, "group_usage_cost_catchup"))
	require.False(t, isolatedRelationExists(t, schema, "group_usage_cost_totals"),
		"migration 272 must not expose a partial aggregate after cancellation")

	applyIsolatedGroupCostMigration(t, schema, groupCostTotalsMigration)
	require.InDelta(t, 3, isolatedGroupCostTotal(t, schema, 9), 0.000000001)
	require.False(t, isolatedRelationExists(t, schema, "group_usage_cost_catchup"))
}

func createIsolatedGroupCostSchema(t *testing.T) string {
	t.Helper()
	schema := "group_cost_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := quotePostgresIdentifier(schema)
	_, err := integrationDB.ExecContext(context.Background(), "CREATE SCHEMA "+quotedSchema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
	})
	_, err = integrationDB.ExecContext(context.Background(), "CREATE TABLE "+quotedSchema+`.usage_logs (
		id BIGSERIAL PRIMARY KEY,
		group_id BIGINT,
		actual_cost NUMERIC(20, 10)
	)`)
	require.NoError(t, err)
	return schema
}

func quotePostgresIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func openGroupCostSchemaConn(t *testing.T, schema string) *sql.Conn {
	t.Helper()
	conn, err := integrationDB.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		// database/sql 会把关闭的 Conn 放回共享池；先恢复会话级设置，
		// 避免 search_path/default_transaction_isolation 污染同包后续测试。
		_, _ = conn.ExecContext(context.Background(), "RESET ALL")
		_ = conn.Close()
	})
	_, err = conn.ExecContext(context.Background(), "SET search_path = "+quotePostgresIdentifier(schema))
	require.NoError(t, err)
	return conn
}

func applyIsolatedGroupCostMigration(t *testing.T, schema, name string) {
	t.Helper()
	conn := openGroupCostSchemaConn(t, schema)
	require.NoError(t, executeIsolatedGroupCostMigration(context.Background(), conn, name))
}

func executeIsolatedGroupCostMigration(ctx context.Context, conn *sql.Conn, name string) error {
	content, err := migrations.FS.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}
	if _, err := conn.ExecContext(ctx,
		"SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL REPEATABLE READ",
	); err != nil {
		return fmt.Errorf("set non-default session isolation for %s: %w", name, err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("execute migration %s: %w", name, err)
	}
	if name == groupCostTotalsMigration {
		var isolation string
		if err := tx.QueryRowContext(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("read effective isolation for %s: %w", name, err)
		}
		if isolation != "read committed" {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s kept unexpected isolation %q", name, isolation)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}

func postgresBackendPID(t *testing.T, conn *sql.Conn) int {
	t.Helper()
	var pid int
	require.NoError(t, conn.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&pid))
	return pid
}

func requireMigrationWaitingForUsageLogLock(t *testing.T, schema string, pid int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		err := integrationDB.QueryRowContext(context.Background(), `
			SELECT EXISTS (
				SELECT 1
				FROM pg_locks locks
				JOIN pg_class relations ON relations.oid = locks.relation
				JOIN pg_namespace namespaces ON namespaces.oid = relations.relnamespace
				WHERE locks.pid = $1
				  AND namespaces.nspname = $2
				  AND relations.relname = 'usage_logs'
				  AND locks.mode = 'ShareRowExclusiveLock'
				  AND NOT locks.granted
			)
		`, pid, schema).Scan(&waiting)
		require.NoError(t, err)
		if waiting {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("migration backend %d did not wait for the isolated usage_logs lock", pid)
}

func receiveMigrationResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for migration result")
		return nil
	}
}

func insertIsolatedUsageLog(ctx context.Context, schema string, groupID int64, cost float64) error {
	conn, err := integrationDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "RESET ALL")
		_ = conn.Close()
	}()
	if _, err := conn.ExecContext(ctx, "SET search_path = "+quotePostgresIdentifier(schema)); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "INSERT INTO usage_logs (group_id, actual_cost) VALUES ($1, $2)", groupID, cost)
	return err
}

func isolatedGroupCostTotal(t *testing.T, schema string, groupID int64) float64 {
	t.Helper()
	var total float64
	require.NoError(t, integrationDB.QueryRowContext(context.Background(),
		"SELECT total_cost FROM "+quotePostgresIdentifier(schema)+".group_usage_cost_totals WHERE group_id = $1",
		groupID,
	).Scan(&total))
	return total
}

func isolatedCatchupRowCount(t *testing.T, schema string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, integrationDB.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM "+quotePostgresIdentifier(schema)+".group_usage_cost_catchup",
	).Scan(&count))
	return count
}

func isolatedRelationExists(t *testing.T, schema, relation string) bool {
	t.Helper()
	var qualifiedName sql.NullString
	require.NoError(t, integrationDB.QueryRowContext(context.Background(),
		"SELECT to_regclass($1)", schema+"."+relation,
	).Scan(&qualifiedName))
	return qualifiedName.Valid
}
