package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"testing"
	"testing/fstest"

	"github.com/Wei-Shaw/sub2api/migrations"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

var migrationIndexCatalogColumns = []string{
	"table_schema",
	"table_name",
	"access_method",
	"relation_kind",
	"indisunique",
	"indisprimary",
	"indisexclusion",
	"indisready",
	"indisvalid",
	"indislive",
	"indnkeyatts",
	"indnatts",
	"keys_json",
	"include_columns_json",
	"predicate",
}

const openAIAgentIdentityDuplicatePrecheckPattern = `(?s)WITH identities AS.*identity_value_1.*identity_value_2.*FROM public\.accounts.*GROUP BY identity_name, owner_user_id, identity_value_1, identity_value_2`

func matchingMigrationIndexCatalogRows(
	t *testing.T,
	requirement migrationIndexRequirement,
	mutate func(*migrationIndexCatalogState),
) *sqlmock.Rows {
	t.Helper()
	state := migrationIndexCatalogState{
		exists:         true,
		table:          requirement.table,
		accessMethod:   requirement.accessMethod,
		relationKind:   "i",
		unique:         requirement.unique,
		ready:          true,
		valid:          true,
		live:           true,
		keyCount:       len(requirement.keys),
		attributeCount: len(requirement.keys) + len(requirement.includeColumns),
		keys:           make([]migrationIndexCatalogKey, len(requirement.keys)),
		includeColumns: append([]string(nil), requirement.includeColumns...),
	}
	for i, expected := range requirement.keys {
		definition := expected.column
		isExpression := expected.expressionCanonical != ""
		if isExpression {
			definition = expected.expressionCanonical
		}
		state.keys[i] = migrationIndexCatalogKey{
			Position:        i + 1,
			IsExpression:    isExpression,
			Definition:      definition,
			TypeSchema:      expected.resultType.schema,
			TypeName:        expected.resultType.name,
			OpClassSchema:   expected.operatorClass.schema,
			OpClassName:     expected.operatorClass.name,
			CollationSchema: expected.collation.schema,
			CollationName:   expected.collation.name,
		}
	}
	if requirement.predicateCanonical != "" {
		state.predicate = sql.NullString{
			String: requirement.predicateCanonical,
			Valid:  true,
		}
	}
	if mutate != nil {
		mutate(&state)
	}
	keysJSON, err := json.Marshal(state.keys)
	require.NoError(t, err)
	includeColumnsJSON, err := json.Marshal(state.includeColumns)
	require.NoError(t, err)
	var predicate any
	if state.predicate.Valid {
		predicate = state.predicate.String
	}
	return sqlmock.NewRows(migrationIndexCatalogColumns).AddRow(
		state.table.schema,
		state.table.name,
		state.accessMethod,
		state.relationKind,
		state.unique,
		state.primary,
		state.exclusion,
		state.ready,
		state.valid,
		state.live,
		state.keyCount,
		state.attributeCount,
		string(keysJSON),
		string(includeColumnsJSON),
		predicate,
	)
}

func expectMigrationIndexCatalogQuery(
	mock sqlmock.Sqlmock,
	requirement migrationIndexRequirement,
	rows *sqlmock.Rows,
) {
	mock.ExpectQuery("SELECT\\s+tbl_ns\\.nspname.*ELSE table_attribute\\.attname::text").
		WithArgs(requirement.index.schema, requirement.index.name).
		WillReturnRows(rows)
}

func TestValidateMigrationExecutionMode(t *testing.T) {
	t.Run("事务迁移包含CONCURRENTLY会被拒绝", func(t *testing.T) {
		nonTx, err := validateMigrationExecutionMode("001_add_idx.sql", "CREATE INDEX CONCURRENTLY idx_a ON t(a);")
		require.False(t, nonTx)
		require.Error(t, err)
	})

	t.Run("notx迁移要求CREATE使用IF NOT EXISTS", func(t *testing.T) {
		nonTx, err := validateMigrationExecutionMode("001_add_idx_notx.sql", "CREATE INDEX CONCURRENTLY idx_a ON t(a);")
		require.False(t, nonTx)
		require.Error(t, err)
	})

	t.Run("notx迁移要求DROP使用IF EXISTS", func(t *testing.T) {
		nonTx, err := validateMigrationExecutionMode("001_drop_idx_notx.sql", "DROP INDEX CONCURRENTLY idx_a;")
		require.False(t, nonTx)
		require.Error(t, err)
	})

	t.Run("notx迁移禁止事务控制语句", func(t *testing.T) {
		nonTx, err := validateMigrationExecutionMode("001_add_idx_notx.sql", "BEGIN; CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_a ON t(a); COMMIT;")
		require.False(t, nonTx)
		require.Error(t, err)
	})

	t.Run("notx迁移禁止混用非CONCURRENTLY语句", func(t *testing.T) {
		nonTx, err := validateMigrationExecutionMode("001_add_idx_notx.sql", "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_a ON t(a); UPDATE t SET a = 1;")
		require.False(t, nonTx)
		require.Error(t, err)
	})

	t.Run("notx迁移允许幂等并发索引语句", func(t *testing.T) {
		nonTx, err := validateMigrationExecutionMode("001_add_idx_notx.sql", `
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_a ON t(a);
DROP INDEX CONCURRENTLY IF EXISTS idx_b;
`)
		require.True(t, nonTx)
		require.NoError(t, err)
	})
}

func TestApplyMigrationsFS_NonTransactionalMigration_LatestAPIKeyIPIndexDropsInvalidIndexBeforeRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(latestAPIKeyIPIndexMigration).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT EXISTS \\(").
		WithArgs(latestAPIKeyIPIndex).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec("DROP INDEX CONCURRENTLY IF EXISTS idx_usage_logs_api_key_latest_ip").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_api_key_latest_ip").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations \\(filename, checksum\\) VALUES \\(\\$1, \\$2\\)").
		WithArgs(latestAPIKeyIPIndexMigration, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		latestAPIKeyIPIndexMigration: &fstest.MapFile{
			Data: []byte(`
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_api_key_latest_ip
    ON usage_logs (api_key_id, created_at DESC, id DESC)
    INCLUDE (ip_address)
    WHERE ip_address IS NOT NULL AND ip_address <> '';
`),
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCanonicalizeMigrationIndexExpressionNormalizesPostgreSQLVarcharCasts(t *testing.T) {
	t.Run("生产character varying谓词", func(t *testing.T) {
		predicate := "((reason)::text = ANY ((ARRAY['account_share_mode_seat_prepay'::character varying, 'account_share_mode_seat_refund'::character varying, 'account_share_mode_seat_waiver_refund'::character varying])::text[]))"
		require.Equal(t, accountShareSeatReasonPredicateCanonical, canonicalizeMigrationIndexExpression(predicate))

		membershipPredicate := predicate + " AND (NULLIF((metadata ->> 'membership_id'::text), ''::text) IS NOT NULL)"
		require.Equal(
			t,
			accountShareSeatReasonPredicateCanonical+"AND"+accountShareSeatMembershipExpressionCanonical+"ISNOTNULL",
			canonicalizeMigrationIndexExpression(membershipPredicate),
		)
	})

	t.Run("varchar别名与数组转换", func(t *testing.T) {
		withCasts := "reason = ANY ((ARRAY['seat'::varchar])::varchar[])"
		withoutCasts := "reason = ANY (ARRAY['seat'])"
		require.Equal(t, canonicalizeMigrationIndexExpression(withoutCasts), canonicalizeMigrationIndexExpression(withCasts))
	})

	t.Run("保留带长度varchar语义", func(t *testing.T) {
		require.NotEqual(t, canonicalizeMigrationIndexExpression("reason50"), canonicalizeMigrationIndexExpression("reason::varchar(50)"))
		require.NotEqual(t, canonicalizeMigrationIndexExpression("reason50"), canonicalizeMigrationIndexExpression("reason::character varying(50)"))
	})

	t.Run("OpenAI身份索引表达式兼容PostgreSQL规范化输出", func(t *testing.T) {
		actualOrganization := "lower(NULLIF(btrim((credentials ->> 'organization_id'::text)), ''::text))"
		require.Equal(
			t,
			canonicalizeMigrationIndexExpression(openAIOwnedOrganizationExpression),
			canonicalizeMigrationIndexExpression(actualOrganization),
		)

		actualAgentPredicate := "(deleted_at IS NULL) AND (owner_user_id IS NOT NULL) AND ((platform)::text = 'openai'::text) AND ((type)::text = 'oauth'::text) AND (lower(NULLIF(btrim((credentials ->> 'auth_mode'::text)), ''::text)) = 'agentidentity'::text) AND (NULLIF(btrim((credentials ->> 'chatgpt_account_id'::text)), ''::text) IS NOT NULL)"
		require.Equal(
			t,
			canonicalizeMigrationIndexExpression(openAIOwnedAgentIdentityPredicate),
			canonicalizeMigrationIndexExpression(actualAgentPredicate),
		)
	})
}

func TestPrepareAccountShareSeatCostIndexesMigrationDropsInvalidIndexBeforeRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	firstLedgerIndex := accountShareSeatCostIndexRequirements[0]
	mock.ExpectQuery("SELECT\\s+tbl_ns\\.nspname").
		WithArgs(firstLedgerIndex.index.schema, firstLedgerIndex.index.name).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT GREATEST\\(c\\.reltuples, 0\\)::bigint").
		WithArgs("user_balance_ledger").
		WillReturnRows(sqlmock.NewRows([]string{"estimated_rows", "table_bytes"}).AddRow(int64(100), int64(4096)))

	for i, requirement := range accountShareSeatCostIndexRequirements {
		indexName := requirement.index.name
		invalid := i == 0
		mock.ExpectQuery("SELECT EXISTS \\(").
			WithArgs(indexName).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(invalid))
		if invalid {
			mock.ExpectExec(regexp.QuoteMeta(
				`DROP INDEX CONCURRENTLY IF EXISTS "public"."` + indexName + `"`,
			)).
				WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}

	err = prepareNonTransactionalMigration(context.Background(), db, accountShareSeatCostQueryIndexesMigration)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareAccountShareSeatCostIndexesMigrationRequiresManualBuildForLargeLedger(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	firstLedgerIndex := accountShareSeatCostIndexRequirements[0]
	mock.ExpectQuery("SELECT\\s+tbl_ns\\.nspname").
		WithArgs(firstLedgerIndex.index.schema, firstLedgerIndex.index.name).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT GREATEST\\(c\\.reltuples, 0\\)::bigint").
		WithArgs("user_balance_ledger").
		WillReturnRows(sqlmock.NewRows([]string{"estimated_rows", "table_bytes"}).
			AddRow(accountShareSeatCostAutoIndexMaxRows+1, accountShareSeatCostAutoIndexMaxTableBytes+1))

	err = prepareNonTransactionalMigration(context.Background(), db, accountShareSeatCostQueryIndexesMigration)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires manual low-traffic CREATE INDEX CONCURRENTLY")
	require.Contains(t, err.Error(), "DROP INDEX CONCURRENTLY")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareAccountShareSeatCostIndexesMigrationAcceptsMatchingManualIndexes(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	for _, requirement := range accountShareSeatCostIndexRequirements[:2] {
		expectMigrationIndexCatalogQuery(mock, requirement, matchingMigrationIndexCatalogRows(t, requirement, nil))
	}
	for _, requirement := range accountShareSeatCostIndexRequirements {
		indexName := requirement.index.name
		mock.ExpectQuery("SELECT EXISTS \\(").
			WithArgs(indexName).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	}

	err = prepareNonTransactionalMigration(context.Background(), db, accountShareSeatCostQueryIndexesMigration)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareAccountShareSeatCostIndexesMigrationRejectsWrongManualDefinitionOnLargeLedger(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	requirement := accountShareSeatCostIndexRequirements[0]
	expectMigrationIndexCatalogQuery(mock, requirement, matchingMigrationIndexCatalogRows(t, requirement, func(state *migrationIndexCatalogState) {
		state.keys[0].Definition = "created_at"
		state.keys[0].IsExpression = false
	}))
	mock.ExpectQuery("SELECT GREATEST\\(c\\.reltuples, 0\\)::bigint").
		WithArgs("user_balance_ledger").
		WillReturnRows(sqlmock.NewRows([]string{"estimated_rows", "table_bytes"}).
			AddRow(accountShareSeatCostAutoIndexMaxRows+1, accountShareSeatCostAutoIndexMaxTableBytes+1))

	err = prepareNonTransactionalMigration(context.Background(), db, accountShareSeatCostQueryIndexesMigration)
	require.Error(t, err)
	require.Contains(t, err.Error(), "wrong definition")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVerifyAccountShareSeatCostIndexesMigrationRejectsWrongDefinition(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	for i, requirement := range accountShareSeatCostIndexRequirements {
		var mutate func(*migrationIndexCatalogState)
		if i == len(accountShareSeatCostIndexRequirements)-1 {
			mutate = func(state *migrationIndexCatalogState) {
				state.keys[0], state.keys[1] = state.keys[1], state.keys[0]
			}
		}
		expectMigrationIndexCatalogQuery(mock, requirement, matchingMigrationIndexCatalogRows(t, requirement, mutate))
	}

	err = verifyNonTransactionalMigrationResult(context.Background(), db, accountShareSeatCostQueryIndexesMigration)
	require.Error(t, err)
	require.Contains(t, err.Error(), "do not match migration 208")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsFS_NonTransactionalMigration(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs("001_add_idx_notx.sql").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_t_a ON t\\(a\\)").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations \\(filename, checksum\\) VALUES \\(\\$1, \\$2\\)").
		WithArgs("001_add_idx_notx.sql", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		"001_add_idx_notx.sql": &fstest.MapFile{
			Data: []byte("CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_t_a ON t(a);"),
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsFS_NonTransactionalMigration_MultiStatements(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs("001_add_multi_idx_notx.sql").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_t_a ON t\\(a\\)").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_t_b ON t\\(b\\)").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations \\(filename, checksum\\) VALUES \\(\\$1, \\$2\\)").
		WithArgs("001_add_multi_idx_notx.sql", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		"001_add_multi_idx_notx.sql": &fstest.MapFile{
			Data: []byte(`
-- first
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_t_a ON t(a);
-- second
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_t_b ON t(b);
`),
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsFS_PaymentOrdersOutTradeNoUniqueMigration_FailsFastOnDuplicatePrecheck(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs("120_enforce_payment_orders_out_trade_no_unique_notx.sql").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT out_trade_no, COUNT\\(\\*\\) AS duplicate_count FROM payment_orders").
		WillReturnRows(sqlmock.NewRows([]string{"out_trade_no", "duplicate_count"}).AddRow("dup-out-trade-no", 2))
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		"120_enforce_payment_orders_out_trade_no_unique_notx.sql": &fstest.MapFile{
			Data: []byte(`
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS paymentorder_out_trade_no_unique
    ON payment_orders (out_trade_no)
    WHERE out_trade_no <> '';

DROP INDEX CONCURRENTLY IF EXISTS paymentorder_out_trade_no;
`),
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate out_trade_no")
	require.Contains(t, err.Error(), "dup-out-trade-no")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsFS_PaymentOrdersOutTradeNoUniqueMigration_DropsInvalidIndexBeforeRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs("120_enforce_payment_orders_out_trade_no_unique_notx.sql").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT out_trade_no, COUNT\\(\\*\\) AS duplicate_count FROM payment_orders").
		WillReturnRows(sqlmock.NewRows([]string{"out_trade_no", "duplicate_count"}))
	mock.ExpectQuery("SELECT EXISTS \\(").
		WithArgs("paymentorder_out_trade_no_unique").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec("DROP INDEX CONCURRENTLY IF EXISTS paymentorder_out_trade_no_unique").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS paymentorder_out_trade_no_unique").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DROP INDEX CONCURRENTLY IF EXISTS paymentorder_out_trade_no").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations \\(filename, checksum\\) VALUES \\(\\$1, \\$2\\)").
		WithArgs("120_enforce_payment_orders_out_trade_no_unique_notx.sql", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		"120_enforce_payment_orders_out_trade_no_unique_notx.sql": &fstest.MapFile{
			Data: []byte(`
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS paymentorder_out_trade_no_unique
    ON payment_orders (out_trade_no)
    WHERE out_trade_no <> '';

DROP INDEX CONCURRENTLY IF EXISTS paymentorder_out_trade_no;
`),
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsFS_OwnedAccountIdentityUniqueMigration_FailsFastOnDuplicatePrecheck(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs("140_owned_account_identity_unique_notx.sql").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("WITH identities AS").
		WillReturnRows(sqlmock.NewRows([]string{"identity_name", "owner_user_id", "duplicate_count", "sample_ids"}).
			AddRow("openai.chatgpt_account_id", int64(101), 2, "1,2"))
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		"140_owned_account_identity_unique_notx.sql": &fstest.MapFile{
			Data: []byte("CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owned_openai_chatgpt_account_id_uniq ON accounts (owner_user_id, NULLIF(BTRIM(credentials->>'chatgpt_account_id'), '')) WHERE deleted_at IS NULL;"),
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate owned account identities")
	require.Contains(t, err.Error(), "openai.chatgpt_account_id")
	require.Contains(t, err.Error(), "sample_account_ids=1,2")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsFS_OwnedAccountIdentityUniqueMigration_DropsInvalidIndexesBeforeRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs("140_owned_account_identity_unique_notx.sql").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("WITH identities AS").
		WillReturnRows(sqlmock.NewRows([]string{"identity_name", "owner_user_id", "duplicate_count", "sample_ids"}))
	for i, indexName := range ownedAccountIdentityUniqueIndexes {
		invalid := i == 0
		mock.ExpectQuery("SELECT EXISTS \\(").
			WithArgs(indexName).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(invalid))
		if invalid {
			mock.ExpectExec("DROP INDEX CONCURRENTLY IF EXISTS " + indexName).
				WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}
	mock.ExpectExec("CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owned_openai_chatgpt_account_id_uniq").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations \\(filename, checksum\\) VALUES \\(\\$1, \\$2\\)").
		WithArgs("140_owned_account_identity_unique_notx.sql", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		"140_owned_account_identity_unique_notx.sql": &fstest.MapFile{
			Data: []byte("CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owned_openai_chatgpt_account_id_uniq ON accounts (owner_user_id, NULLIF(BTRIM(credentials->>'chatgpt_account_id'), '')) WHERE deleted_at IS NULL;"),
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsFS_OpenAIOwnedAccountOrgIdentityUniqueMigration_FailsFastOnDuplicatePrecheck(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs("168_openai_owned_account_org_identity_unique_notx.sql").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("WITH identities AS").
		WillReturnRows(sqlmock.NewRows([]string{"identity_name", "owner_user_id", "duplicate_count", "sample_ids"}).
			AddRow("openai.org_user", int64(101), 2, "1,2"))
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		"168_openai_owned_account_org_identity_unique_notx.sql": &fstest.MapFile{
			Data: []byte("CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owned_openai_org_user_uniq ON accounts (owner_user_id, LOWER(NULLIF(BTRIM(credentials->>'organization_id'), '')), NULLIF(BTRIM(credentials->>'chatgpt_user_id'), '')) WHERE deleted_at IS NULL;"),
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate OpenAI owned account org identities")
	require.Contains(t, err.Error(), "openai.org_user")
	require.Contains(t, err.Error(), "sample_account_ids=1,2")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsFS_OpenAIOwnedAccountOrgIdentityUniqueMigration_DropsInvalidIndexesBeforeRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs("168_openai_owned_account_org_identity_unique_notx.sql").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("WITH identities AS").
		WillReturnRows(sqlmock.NewRows([]string{"identity_name", "owner_user_id", "duplicate_count", "sample_ids"}))
	for i, indexName := range openAIOwnedAccountOrgIdentityUniqueIndexes {
		invalid := i == 0
		mock.ExpectQuery("SELECT EXISTS \\(").
			WithArgs(indexName).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(invalid))
		if invalid {
			mock.ExpectExec("DROP INDEX CONCURRENTLY IF EXISTS " + indexName).
				WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}
	mock.ExpectExec("CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owned_openai_org_user_uniq").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DROP INDEX CONCURRENTLY IF EXISTS idx_accounts_owned_openai_chatgpt_account_id_uniq").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations \\(filename, checksum\\) VALUES \\(\\$1, \\$2\\)").
		WithArgs("168_openai_owned_account_org_identity_unique_notx.sql", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		"168_openai_owned_account_org_identity_unique_notx.sql": &fstest.MapFile{
			Data: []byte(`
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owned_openai_org_user_uniq
    ON accounts (owner_user_id, LOWER(NULLIF(BTRIM(credentials->>'organization_id'), '')), NULLIF(BTRIM(credentials->>'chatgpt_user_id'), ''))
    WHERE deleted_at IS NULL;

DROP INDEX CONCURRENTLY IF EXISTS idx_accounts_owned_openai_chatgpt_account_id_uniq;
`),
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareOpenAIOwnedAgentIdentityUniqueMigrationFailsFastOnDuplicates(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(openAIAgentIdentityDuplicatePrecheckPattern).
		WillReturnRows(sqlmock.NewRows([]string{"identity_name", "owner_user_id", "duplicate_count", "sample_ids"}).
			AddRow("openai.agent_identity_team", int64(101), 2, "41,42"))

	err = prepareNonTransactionalMigration(context.Background(), db, openAIOwnedAgentIdentityUniqueMigration)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate OpenAI owned Agent Identity migration identities")
	require.Contains(t, err.Error(), "openai.agent_identity_team")
	require.Contains(t, err.Error(), "sample_account_ids=41,42")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareOpenAIOwnedAgentIdentityUniqueMigrationDropsInvalidIndexesBeforeRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(openAIAgentIdentityDuplicatePrecheckPattern).
		WillReturnRows(sqlmock.NewRows([]string{"identity_name", "owner_user_id", "duplicate_count", "sample_ids"}))
	for i, requirement := range openAIOwnedAgentIdentityUniqueIndexRequirements {
		invalid := i == 0
		mock.ExpectQuery("SELECT EXISTS \\(").
			WithArgs(requirement.index.name).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(invalid))
		if i == 0 {
			mock.ExpectExec(regexp.QuoteMeta("DROP INDEX CONCURRENTLY IF EXISTS " + quoteMigrationCatalogName(requirement.index))).
				WillReturnResult(sqlmock.NewResult(0, 0))
			continue
		}
		mock.ExpectQuery("SELECT\\s+tbl_ns\\.nspname").
			WithArgs(requirement.index.schema, requirement.index.name).
			WillReturnError(sql.ErrNoRows)
	}

	err = prepareNonTransactionalMigration(context.Background(), db, openAIOwnedAgentIdentityUniqueMigration)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareOpenAIOwnedAgentIdentityUniqueMigrationRejectsValidWrongDefinition(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(openAIAgentIdentityDuplicatePrecheckPattern).
		WillReturnRows(sqlmock.NewRows([]string{"identity_name", "owner_user_id", "duplicate_count", "sample_ids"}))
	requirement := openAIOwnedAgentIdentityUniqueIndexRequirements[0]
	mock.ExpectQuery("SELECT EXISTS \\(").
		WithArgs(requirement.index.name).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectMigrationIndexCatalogQuery(mock, requirement, matchingMigrationIndexCatalogRows(t, requirement, func(state *migrationIndexCatalogState) {
		state.keys[1].Definition = "lower(credentials->>'wrong_field')"
	}))

	err = prepareNonTransactionalMigration(context.Background(), db, openAIOwnedAgentIdentityUniqueMigration)
	require.ErrorContains(t, err, "valid but unexpected definition")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVerifyOpenAIOwnedAgentIdentityUniqueMigrationRequiresExactDefinitions(t *testing.T) {
	t.Run("all indexes match", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		for _, requirement := range openAIOwnedAgentIdentityUniqueIndexRequirements {
			expectMigrationIndexCatalogQuery(mock, requirement, matchingMigrationIndexCatalogRows(t, requirement, nil))
		}

		err = verifyNonTransactionalMigrationResult(context.Background(), db, openAIOwnedAgentIdentityUniqueMigration)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing index fails verification", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		requirement := openAIOwnedAgentIdentityUniqueIndexRequirements[0]
		mock.ExpectQuery("SELECT\\s+tbl_ns\\.nspname").
			WithArgs(requirement.index.schema, requirement.index.name).
			WillReturnError(sql.ErrNoRows)

		err = verifyNonTransactionalMigrationResult(context.Background(), db, openAIOwnedAgentIdentityUniqueMigration)
		require.ErrorContains(t, err, "missing, invalid, or do not match migration 217")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("wrong expression fails verification", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		requirement := openAIOwnedAgentIdentityUniqueIndexRequirements[0]
		expectMigrationIndexCatalogQuery(mock, requirement, matchingMigrationIndexCatalogRows(t, requirement, func(state *migrationIndexCatalogState) {
			state.keys[1].Definition = "lower(credentials->>'wrong_field')"
		}))

		err = verifyNonTransactionalMigrationResult(context.Background(), db, openAIOwnedAgentIdentityUniqueMigration)
		require.ErrorContains(t, err, "do not match migration 217")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestOpenAIOwnedAgentIdentityIndexMatcherRejectsCatalogDrift(t *testing.T) {
	requirement := openAIOwnedAgentIdentityUniqueIndexRequirements[0]
	tests := []struct {
		name   string
		mutate func(*migrationIndexCatalogState)
	}{
		{name: "not unique", mutate: func(state *migrationIndexCatalogState) { state.unique = false }},
		{name: "not ready", mutate: func(state *migrationIndexCatalogState) { state.ready = false }},
		{name: "not valid", mutate: func(state *migrationIndexCatalogState) { state.valid = false }},
		{name: "wrong result type", mutate: func(state *migrationIndexCatalogState) { state.keys[1].TypeName = "varchar" }},
		{name: "wrong operator class", mutate: func(state *migrationIndexCatalogState) { state.keys[1].OpClassName = "varchar_ops" }},
		{name: "wrong collation", mutate: func(state *migrationIndexCatalogState) { state.keys[1].CollationName = "C" }},
		{name: "wrong predicate", mutate: func(state *migrationIndexCatalogState) { state.predicate.String = "owner_user_id IS NULL" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			expectMigrationIndexCatalogQuery(mock, requirement, matchingMigrationIndexCatalogRows(t, requirement, test.mutate))
			state, err := loadMigrationIndexCatalogState(context.Background(), db, requirement.index)
			require.NoError(t, err)
			require.False(t, migrationIndexMatchesRequirement(state, requirement))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestApplyOpenAIOwnedAgentIdentityMigrationVerifiesNewIndexesBeforeProtectedDrops(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(openAIOwnedAgentIdentityUniqueMigration).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(openAIAgentIdentityDuplicatePrecheckPattern).
		WillReturnRows(sqlmock.NewRows([]string{"identity_name", "owner_user_id", "duplicate_count", "sample_ids"}))
	for _, requirement := range openAIOwnedAgentIdentityUniqueIndexRequirements {
		mock.ExpectQuery("SELECT EXISTS \\(").
			WithArgs(requirement.index.name).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
		mock.ExpectQuery("SELECT\\s+tbl_ns\\.nspname").
			WithArgs(requirement.index.schema, requirement.index.name).
			WillReturnError(sql.ErrNoRows)
	}
	mock.ExpectExec("CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_test").
		WillReturnResult(sqlmock.NewResult(0, 0))
	firstRequirement := openAIOwnedAgentIdentityUniqueIndexRequirements[0]
	mock.ExpectQuery("SELECT\\s+tbl_ns\\.nspname").
		WithArgs(firstRequirement.index.schema, firstRequirement.index.name).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		openAIOwnedAgentIdentityUniqueMigration: &fstest.MapFile{
			Data: []byte("CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_test ON accounts (owner_user_id);\nDROP INDEX CONCURRENTLY IF EXISTS idx_protected_old;\n"),
		},
	}
	err = applyMigrationsFS(context.Background(), db, fsys)

	require.ErrorContains(t, err, "before dropping protected indexes")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsFS_TransactionalMigration(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs("001_add_col.sql").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectExec("ALTER TABLE t ADD COLUMN name TEXT").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations \\(filename, checksum\\) VALUES \\(\\$1, \\$2\\)").
		WithArgs("001_add_col.sql", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		"001_add_col.sql": &fstest.MapFile{
			Data: []byte("ALTER TABLE t ADD COLUMN name TEXT;"),
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsFSUsageLogImageInputTokensPinsPublicSchemaAndValidatesColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	migrationSQL, err := migrations.FS.ReadFile(usageLogImageInputTokensMigration)
	require.NoError(t, err)
	require.Contains(t, string(migrationSQL), "ADD COLUMN IF NOT EXISTS image_input_tokens")
	require.Contains(t, string(migrationSQL), "ADD COLUMN IF NOT EXISTS image_input_cost")

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(usageLogImageInputTokensMigration).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectExec("SET LOCAL search_path = pg_catalog, public, pg_temp").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS image_input_tokens").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT\\s+a\\.attname").
		WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "attnotnull", "default"}).
			AddRow("image_input_cost", "numeric(20,10)", true, "0").
			AddRow("image_input_tokens", "integer", true, "0"))
	mock.ExpectExec("INSERT INTO schema_migrations \\(filename, checksum\\) VALUES \\(\\$1, \\$2\\)").
		WithArgs(usageLogImageInputTokensMigration, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		usageLogImageInputTokensMigration: &fstest.MapFile{
			Data: migrationSQL,
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVerifyTransactionalMigrationResultRejectsMalformedUsageLogImageInputColumns(t *testing.T) {
	tests := []struct {
		name           string
		costType       string
		costNotNull    bool
		costDefault    string
		includeCostCol bool
		wantErr        string
	}{
		{
			name:           "wrong default",
			costType:       "numeric(20,10)",
			costNotNull:    true,
			costDefault:    "1",
			includeCostCol: true,
			wantErr:        "public.usage_logs.image_input_cost has unexpected definition",
		},
		{
			name:           "wrong type",
			costType:       "numeric(20,9)",
			costNotNull:    true,
			costDefault:    "0",
			includeCostCol: true,
			wantErr:        "public.usage_logs.image_input_cost has unexpected definition",
		},
		{
			name:           "nullable",
			costType:       "numeric(20,10)",
			costNotNull:    false,
			costDefault:    "0",
			includeCostCol: true,
			wantErr:        "public.usage_logs.image_input_cost has unexpected definition",
		},
		{
			name:           "missing default",
			costType:       "numeric(20,10)",
			costNotNull:    true,
			costDefault:    "",
			includeCostCol: true,
			wantErr:        "public.usage_logs.image_input_cost has unexpected definition",
		},
		{
			name:           "missing column",
			includeCostCol: false,
			wantErr:        "public.usage_logs.image_input_cost is missing",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			rows := sqlmock.NewRows([]string{"attname", "format_type", "attnotnull", "default"}).
				AddRow("image_input_tokens", "integer", true, "0")
			if test.includeCostCol {
				rows.AddRow("image_input_cost", test.costType, test.costNotNull, test.costDefault)
			}
			mock.ExpectQuery("SELECT\\s+a\\.attname").WillReturnRows(rows)

			err = verifyTransactionalMigrationResult(context.Background(), db, usageLogImageInputTokensMigration)
			require.ErrorContains(t, err, test.wantErr)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestApplyMigrationsFSUsageLogImageInputTokensRollsBackWhenValidationFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	migrationSQL, err := migrations.FS.ReadFile(usageLogImageInputTokensMigration)
	require.NoError(t, err)

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(usageLogImageInputTokensMigration).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectExec("SET LOCAL search_path = pg_catalog, public, pg_temp").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS image_input_tokens").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT\\s+a\\.attname").
		WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "attnotnull", "default"}).
			AddRow("image_input_cost", "numeric(20,10)", false, "0").
			AddRow("image_input_tokens", "integer", true, "0"))
	mock.ExpectRollback()
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		usageLogImageInputTokensMigration: &fstest.MapFile{Data: migrationSQL},
	}
	err = applyMigrationsFS(context.Background(), db, fsys)

	require.ErrorContains(t, err, "verify migration "+usageLogImageInputTokensMigration)
	require.ErrorContains(t, err, "public.usage_logs.image_input_cost has unexpected definition")
	require.NoError(t, mock.ExpectationsWereMet())
}

func prepareMigrationsBootstrapExpectations(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT pg_try_advisory_lock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT EXISTS \\(").
		WithArgs("schema_migrations").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS \\(").
		WithArgs("atlas_schema_revisions").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM atlas_schema_revisions").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
}
