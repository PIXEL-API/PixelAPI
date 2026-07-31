// Package repository 实现数据访问层（Repository Pattern）。
//
// 该包提供了与数据库交互的所有操作，包括 CRUD、复杂查询和批量操作。
// 采用 Repository 模式将数据访问逻辑与业务逻辑分离，便于测试和维护。
//
// 主要特性：
//   - 使用 Ent ORM 进行类型安全的数据库操作
//   - 对于复杂查询（如批量更新、聚合统计）使用原生 SQL
//   - 提供统一的错误翻译机制，将数据库错误转换为业务错误
//   - 支持软删除，所有查询自动过滤已删除记录
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	dbaccountgroup "github.com/Wei-Shaw/sub2api/ent/accountgroup"
	dbgroup "github.com/Wei-Shaw/sub2api/ent/group"
	dbpredicate "github.com/Wei-Shaw/sub2api/ent/predicate"
	dbproxy "github.com/Wei-Shaw/sub2api/ent/proxy"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
)

// accountRepository 实现 service.AccountRepository 接口。
// 提供 AI API 账户的完整数据访问功能。
//
// 设计说明：
//   - client: Ent 客户端，用于类型安全的 ORM 操作
//   - sql: 原生 SQL 执行器，用于复杂查询和批量操作
//   - schedulerCache: 调度器缓存，用于在账号状态变更时同步快照
type accountRepository struct {
	client *dbent.Client // Ent ORM 客户端
	sql    sqlExecutor   // 原生 SQL 执行接口
	// schedulerCache 用于在账号状态变更时主动同步快照到缓存，
	// 确保粘性会话能及时感知账号不可用状态。
	// Used to proactively sync account snapshot to cache when status changes,
	// ensuring sticky sessions can promptly detect unavailable accounts.
	schedulerCache service.SchedulerCache
}

var _ service.AccountDeletionGuardRepository = (*accountRepository)(nil)
var _ service.AccountMutationGuardRepository = (*accountRepository)(nil)
var _ service.CRSPreviewSnapshotRepository = (*accountRepository)(nil)
var _ service.GrokOAuthReconcileCandidatePager = (*accountRepository)(nil)

var schedulerNeutralExtraKeyPrefixes = []string{
	"codex_primary_",
	"codex_secondary_",
	"codex_5h_",
	"codex_7d_",
	"passive_usage_",
}

var schedulerNeutralExtraKeys = map[string]struct{}{
	"codex_usage_updated_at":     {},
	"grok_billing_snapshot":      {},
	"session_window_utilization": {},
}

var schedulerRelevantExtraKeys = map[string]struct{}{
	"openai_responses_mode":      {},
	"openai_responses_supported": {},
}

const postgresParameterBatchSize = 50000

// NewAccountRepository 创建账户仓储实例。
// 这是对外暴露的构造函数，返回接口类型以便于依赖注入。
func NewAccountRepository(client *dbent.Client, sqlDB *sql.DB, schedulerCache service.SchedulerCache) service.AccountRepository {
	return newAccountRepositoryWithSQL(client, sqlDB, schedulerCache)
}

// newAccountRepositoryWithSQL 是内部构造函数，支持依赖注入 SQL 执行器。
// 这种设计便于单元测试时注入 mock 对象。
func newAccountRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor, schedulerCache service.SchedulerCache) *accountRepository {
	return &accountRepository{client: client, sql: sqlq, schedulerCache: schedulerCache}
}

func translateAccountPersistenceError(err error, notFound *infraerrors.ApplicationError) error {
	if err == nil {
		return nil
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Constraint {
		case "account_external_placement_identity_change_chk",
			"account_external_placement_level_change_chk",
			"account_external_placement_room_level_change_chk":
			return service.ErrOwnedAccountPlacementConversionRequired.WithCause(err)
		case "account_external_placements_account_identity_chk":
			return service.ErrAccountExternalPlacementConflict.WithCause(err)
		}
	}
	if isUniqueViolationOnIndex(err, ownedAccountIdentityUniqueIndexSet) {
		return service.ErrOwnedAccountAlreadyExists.WithCause(err)
	}
	return translatePersistenceError(err, notFound, nil)
}

func (r *accountRepository) Create(ctx context.Context, account *service.Account) error {
	return r.createAccount(ctx, r.client, r.sql, account)
}

// CreateWithAccountGroups atomically persists a duplicated account, its exact
// per-group priorities, and the scheduler outbox event for the new route.
func (r *accountRepository) CreateWithAccountGroups(ctx context.Context, account *service.Account, groups []service.AccountGroup) error {
	if account == nil {
		return service.ErrAccountNilInput
	}
	tx, err := r.client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return err
	}

	txCtx := ctx
	txClient := r.client
	if tx != nil {
		defer func() { _ = tx.Rollback() }()
		txCtx = dbent.NewTxContext(ctx, tx)
		txClient = tx.Client()
	}
	exec := sqlExecutorFromEntClient(txClient)
	if exec == nil {
		return fmt.Errorf("transaction sql executor is unavailable")
	}

	groupIDs := make([]int64, 0, len(groups))
	seenGroupIDs := make(map[int64]struct{}, len(groups))
	for _, group := range groups {
		if group.GroupID <= 0 {
			return fmt.Errorf("duplicate account group id must be positive")
		}
		if _, exists := seenGroupIDs[group.GroupID]; exists {
			return fmt.Errorf("duplicate account group id %d", group.GroupID)
		}
		seenGroupIDs[group.GroupID] = struct{}{}
		groupIDs = append(groupIDs, group.GroupID)
	}
	account.GroupIDs = append([]int64(nil), groupIDs...)
	if err := r.createAccountRecord(txCtx, txClient, account); err != nil {
		return err
	}

	if len(groups) > 0 {
		builders := make([]*dbent.AccountGroupCreate, 0, len(groups))
		for i := range groups {
			groups[i].AccountID = account.ID
			builders = append(builders, txClient.AccountGroup.Create().
				SetAccountID(account.ID).
				SetGroupID(groups[i].GroupID).
				SetPriority(groups[i].Priority),
			)
		}
		if _, err := txClient.AccountGroup.CreateBulk(builders...).Save(txCtx); err != nil {
			return fmt.Errorf("create duplicate account groups: %w", err)
		}
	}
	if err := enqueueSchedulerOutbox(txCtx, exec, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload(groupIDs)); err != nil {
		return fmt.Errorf("enqueue duplicate account scheduler event: %w", err)
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	account.AccountGroups = append([]service.AccountGroup(nil), groups...)
	return nil
}

func (r *accountRepository) CreateOwnedWithProxyCapacity(ctx context.Context, ownerUserID int64, account *service.Account) error {
	if account == nil {
		return service.ErrAccountNilInput
	}
	if ownerUserID <= 0 {
		return service.ErrUserNotFound
	}
	if account.ProxyID == nil || *account.ProxyID <= 0 {
		return service.ErrProxyNotFound
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	exec := sqlExecutorFromEntClient(tx.Client())
	if exec == nil {
		return fmt.Errorf("transaction sql executor is unavailable")
	}

	if err := ensureOwnedProxyCapacityForCreateInTx(txCtx, exec, ownerUserID, *account.ProxyID); err != nil {
		return err
	}
	if err := r.createAccount(txCtx, tx.Client(), exec, account); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *accountRepository) createAccount(ctx context.Context, client *dbent.Client, exec sqlExecutor, account *service.Account) error {
	if err := r.createAccountRecord(ctx, client, account); err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, exec, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload(account.GroupIDs)); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue account create failed: account=%d err=%v", account.ID, err)
	}
	return nil
}

func (r *accountRepository) createAccountRecord(ctx context.Context, client *dbent.Client, account *service.Account) error {
	if account == nil {
		return service.ErrAccountNilInput
	}
	if client == nil {
		return fmt.Errorf("account repository client is unavailable")
	}

	builder := client.Account.Create().
		SetName(account.Name).
		SetNillableNotes(account.Notes).
		SetPlatform(account.Platform).
		SetAccountLevel(service.NormalizeAccountLevel(account.AccountLevel)).
		SetType(account.Type).
		SetCredentials(normalizeJSONMap(account.Credentials)).
		SetExtra(normalizeJSONMap(account.Extra)).
		SetShareMode(service.NormalizeAccountShareMode(account.ShareMode)).
		SetShareStatus(service.NormalizeAccountShareStatus(account.ShareStatus)).
		SetConcurrency(account.Concurrency).
		SetLoadFactorPaidCeiling(normalizeLoadFactorPaidCeiling(account.LoadFactorPaidCeiling)).
		SetPriority(account.Priority).
		SetStatus(account.Status).
		SetErrorMessage(account.ErrorMessage).
		SetSchedulable(account.Schedulable).
		SetAutoPauseOnExpired(account.AutoPauseOnExpired)

	if account.RateMultiplier != nil {
		builder.SetRateMultiplier(*account.RateMultiplier)
	}
	if account.LoadFactor != nil {
		builder.SetLoadFactor(*account.LoadFactor)
	}
	if account.OwnerUserID != nil {
		builder.SetOwnerUserID(*account.OwnerUserID)
	}
	if account.SharePolicyID != nil {
		builder.SetSharePolicyID(*account.SharePolicyID)
	}

	if account.ProxyID != nil {
		builder.SetProxyID(*account.ProxyID)
	}
	if account.LastUsedAt != nil {
		builder.SetLastUsedAt(*account.LastUsedAt)
	}
	if account.ExpiresAt != nil {
		builder.SetExpiresAt(*account.ExpiresAt)
	}
	if account.RateLimitedAt != nil {
		builder.SetRateLimitedAt(*account.RateLimitedAt)
	}
	if account.RateLimitResetAt != nil {
		builder.SetRateLimitResetAt(*account.RateLimitResetAt)
	}
	if account.OverloadUntil != nil {
		builder.SetOverloadUntil(*account.OverloadUntil)
	}
	if account.SessionWindowStart != nil {
		builder.SetSessionWindowStart(*account.SessionWindowStart)
	}
	if account.SessionWindowEnd != nil {
		builder.SetSessionWindowEnd(*account.SessionWindowEnd)
	}
	if account.SessionWindowStatus != "" {
		builder.SetSessionWindowStatus(account.SessionWindowStatus)
	}

	created, err := builder.Save(ctx)
	if err != nil {
		return translateAccountPersistenceError(err, service.ErrAccountNotFound)
	}

	account.ID = created.ID
	account.CreatedAt = created.CreatedAt
	account.UpdatedAt = created.UpdatedAt
	if account.Status == service.StatusError {
		if err := r.syncAccountErrorSince(ctx, account.ID, account.Status); err != nil {
			return err
		}
	}
	return nil
}

func ensureOwnedProxyCapacityForCreateInTx(ctx context.Context, exec sqlQueryExecutor, ownerUserID, proxyID int64) error {
	if ownerUserID <= 0 {
		return service.ErrUserNotFound
	}
	if proxyID <= 0 {
		return service.ErrProxyNotFound
	}

	var maxAccounts int
	if err := scanSingleRow(ctx, exec, `
		SELECT max_accounts
		FROM proxies
		WHERE id = $1
			AND status = $2
			AND deleted_at IS NULL
			AND (owner_user_id IS NULL OR owner_user_id = $3)
		FOR UPDATE
	`, []any{proxyID, service.StatusActive, ownerUserID}, &maxAccounts); errors.Is(err, sql.ErrNoRows) {
		return service.ErrProxyNotFound
	} else if err != nil {
		return err
	}
	if maxAccounts <= 0 {
		return nil
	}

	var current int64
	if err := scanSingleRow(ctx, exec, `
		SELECT COUNT(*)
		FROM accounts
		WHERE proxy_id = $1
			AND deleted_at IS NULL
	`, []any{proxyID}, &current); err != nil {
		return err
	}
	if current+1 > int64(maxAccounts) {
		return service.ProxyAccountLimitExceededError(proxyID, current, int64(maxAccounts), 1)
	}
	return nil
}

func (r *accountRepository) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	m, err := r.client.Account.Query().Where(dbaccount.IDEQ(id)).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}

	accounts, err := r.accountsToService(ctx, []*dbent.Account{m})
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, service.ErrAccountNotFound
	}
	return &accounts[0], nil
}

func (r *accountRepository) GetByIDs(ctx context.Context, ids []int64) ([]*service.Account, error) {
	if len(ids) == 0 {
		return []*service.Account{}, nil
	}

	uniqueIDs := uniquePositiveInt64s(ids)
	if len(uniqueIDs) == 0 {
		return []*service.Account{}, nil
	}

	entAccounts := make([]*dbent.Account, 0, len(uniqueIDs))
	for start := 0; start < len(uniqueIDs); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		batch, err := r.client.Account.
			Query().
			Where(dbaccount.IDIn(uniqueIDs[start:end]...)).
			WithProxy().
			All(ctx)
		if err != nil {
			return nil, err
		}
		entAccounts = append(entAccounts, batch...)
	}
	if len(entAccounts) == 0 {
		return []*service.Account{}, nil
	}

	accountIDs := make([]int64, 0, len(entAccounts))
	entByID := make(map[int64]*dbent.Account, len(entAccounts))
	for _, acc := range entAccounts {
		entByID[acc.ID] = acc
		accountIDs = append(accountIDs, acc.ID)
	}

	groupsByAccount, groupIDsByAccount, accountGroupsByAccount, err := r.loadAccountGroups(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	externalPlacementsByAccount, err := r.loadAccountExternalPlacements(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	roomListingIDsByAccount, err := r.loadAccountShareRoomListingIDs(ctx, accountIDs)
	if err != nil {
		return nil, err
	}

	outByID := make(map[int64]*service.Account, len(entAccounts))
	for _, entAcc := range entAccounts {
		out := accountEntityToService(entAcc)
		if out == nil {
			continue
		}

		// Prefer the preloaded proxy edge when available.
		if entAcc.Edges.Proxy != nil {
			out.Proxy = proxyEntityToService(entAcc.Edges.Proxy)
		}

		if groups, ok := groupsByAccount[entAcc.ID]; ok {
			out.Groups = groups
		}
		if groupIDs, ok := groupIDsByAccount[entAcc.ID]; ok {
			out.GroupIDs = groupIDs
		}
		if ags, ok := accountGroupsByAccount[entAcc.ID]; ok {
			out.AccountGroups = ags
		}
		if placement, ok := externalPlacementsByAccount[entAcc.ID]; ok {
			placementCopy := placement
			out.ExternalPlacement = &placementCopy
		}
		if listingID, ok := roomListingIDsByAccount[entAcc.ID]; ok {
			id := listingID
			out.AccountShareModeListingID = &id
		}
		outByID[entAcc.ID] = out
	}

	// Preserve input order (first occurrence), and ignore missing IDs.
	out := make([]*service.Account, 0, len(uniqueIDs))
	for _, id := range uniqueIDs {
		if _, ok := entByID[id]; !ok {
			continue
		}
		if acc, ok := outByID[id]; ok && acc != nil {
			out = append(out, acc)
		}
	}

	return out, nil
}

// ListOwnedAccountIDs 仅返回指定用户拥有的账号 ID，用于批量授权边界检查。
// 该路径不加载凭据、Extra、代理或分组关联。
func (r *accountRepository) ListOwnedAccountIDs(ctx context.Context, ownerUserID int64, accountIDs []int64) ([]int64, error) {
	if ownerUserID <= 0 {
		return nil, service.ErrUserNotFound
	}
	uniqueIDs := uniquePositiveInt64s(accountIDs)
	if len(uniqueIDs) == 0 {
		return []int64{}, nil
	}

	ownedIDs := make([]int64, 0, len(uniqueIDs))
	for start := 0; start < len(uniqueIDs); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		ids, err := r.client.Account.
			Query().
			Where(
				dbaccount.IDIn(uniqueIDs[start:end]...),
				dbaccount.OwnerUserIDEQ(ownerUserID),
			).
			IDs(ctx)
		if err != nil {
			return nil, err
		}
		ownedIDs = append(ownedIDs, ids...)
	}
	return ownedIDs, nil
}

// ExistsByID 检查指定 ID 的账号是否存在。
// 相比 GetByID，此方法性能更优，因为：
//   - 使用 Exist() 方法生成 SELECT EXISTS 查询，只返回布尔值
//   - 不加载完整的账号实体及其关联数据（Groups、Proxy 等）
//   - 适用于删除前的存在性检查等只需判断有无的场景
func (r *accountRepository) ExistsByID(ctx context.Context, id int64) (bool, error) {
	exists, err := r.client.Account.Query().Where(dbaccount.IDEQ(id)).Exist(ctx)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// ExistsByCredentialField checks one exact credentials JSONB field without
// loading account rows. Callers keep this behind a narrow capability interface
// so the large AccountRepository contract and its test doubles stay stable.
func (r *accountRepository) ExistsByCredentialField(ctx context.Context, key, value string) (bool, error) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return false, errors.New("credential field key and value are required")
	}
	return r.client.Account.Query().
		Where(
			dbaccount.DeletedAtIsNil(),
			func(selector *entsql.Selector) {
				selector.Where(sqljson.ValueEQ(dbaccount.FieldCredentials, value, sqljson.Path(key)))
			},
		).
		Exist(ctx)
}

// GetOwnedOpenAIAgentIdentityByChatGPTAccountID returns the Agent Identity
// account owned by one user for one ChatGPT Team. The owner and auth-mode
// predicates are part of the database query so callers cannot observe or
// update another user's account through a cross-tenant lookup. A missing Team
// returns (nil, nil), which lets import callers distinguish absence from a
// persistence failure without treating the preflight lookup as an error.
func (r *accountRepository) GetOwnedOpenAIAgentIdentityByChatGPTAccountID(
	ctx context.Context,
	ownerUserID int64,
	chatGPTAccountID string,
) (*service.Account, error) {
	chatGPTAccountID = strings.TrimSpace(chatGPTAccountID)
	if ownerUserID <= 0 || chatGPTAccountID == "" {
		return nil, nil
	}
	trimFunction := "BTRIM"
	if r.client.Driver().Dialect() != dialect.Postgres {
		trimFunction = "TRIM"
	}

	m, err := r.client.Account.Query().
		Where(
			dbaccount.DeletedAtIsNil(),
			dbaccount.OwnerUserIDEQ(ownerUserID),
			dbaccount.PlatformEQ(service.PlatformOpenAI),
			dbaccount.TypeEQ(service.AccountTypeOAuth),
			func(selector *entsql.Selector) {
				credentialsColumn := selector.C(dbaccount.FieldCredentials)
				selector.Where(entsql.P(func(builder *entsql.Builder) {
					builder.WriteString("LOWER(NULLIF(").
						WriteString(trimFunction).
						WriteString("(").
						Ident(credentialsColumn).
						WriteString("->>'auth_mode'), '')) = ").
						Arg(strings.ToLower(service.OpenAIAuthModeAgentIdentity)).
						WriteString(" AND NULLIF(").
						WriteString(trimFunction).
						WriteString("(").
						Ident(credentialsColumn).
						WriteString("->>'chatgpt_account_id'), '') = ").
						Arg(chatGPTAccountID)
				}))
			},
		).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	account := accountEntityToService(m)
	if account == nil {
		return nil, nil
	}
	return account, nil
}

func (r *accountRepository) IsAccountShareModeListingAccount(ctx context.Context, id int64) (bool, error) {
	if id <= 0 {
		return false, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT room_account.listing_id
		FROM account_share_room_accounts room_account
		JOIN account_share_listings listing
			ON listing.id = room_account.listing_id
			AND listing.deleted_at IS NULL
		WHERE room_account.account_id = $1
			AND room_account.state IN ('active', 'draining')
		LIMIT 1
	`, id)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var listingID int64
	if err := rows.Scan(&listingID); err != nil {
		return false, err
	}
	return true, rows.Err()
}

func (r *accountRepository) GetByCRSAccountID(ctx context.Context, crsAccountID string) (*service.Account, error) {
	if crsAccountID == "" {
		return nil, nil
	}

	// 使用 sqljson.ValueEQ 生成 JSON 路径过滤，避免手写 SQL 片段导致语法兼容问题。
	m, err := r.client.Account.Query().
		Where(func(s *entsql.Selector) {
			s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, crsAccountID, sqljson.Path("crs_account_id")))
		}).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	accounts, err := r.accountsToService(ctx, []*dbent.Account{m})
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, nil
	}
	return &accounts[0], nil
}

func (r *accountRepository) ListCRSAccountIDs(ctx context.Context) (map[string]int64, error) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, extra->>'crs_account_id'
		FROM accounts
		WHERE deleted_at IS NULL
			AND extra->>'crs_account_id' IS NOT NULL
			AND extra->>'crs_account_id' != ''
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]int64)
	for rows.Next() {
		var id int64
		var crsID string
		if err := rows.Scan(&id, &crsID); err != nil {
			return nil, err
		}
		result[crsID] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *accountRepository) ListCRSAccountPreviewSnapshots(
	ctx context.Context,
) ([]service.CRSAccountPreviewSnapshot, error) {
	if r == nil || r.sql == nil {
		return nil, service.ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
			"stage": "repository_executor",
		})
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT
			account_row.id,
			account_row.extra->>'crs_account_id',
			listing.id,
			listing.row_version
		FROM accounts account_row
		LEFT JOIN account_share_room_accounts room_account
			ON room_account.account_id = account_row.id
		LEFT JOIN account_share_listings listing
			ON listing.id = room_account.listing_id
			AND listing.deleted_at IS NULL
		WHERE account_row.deleted_at IS NULL
			AND account_row.extra->>'crs_account_id' IS NOT NULL
			AND BTRIM(account_row.extra->>'crs_account_id') <> ''
		ORDER BY account_row.id, listing.id NULLS LAST
	`)
	if err != nil {
		return nil, service.ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
			"stage": "repository_query",
		}).WithCause(err)
	}
	defer func() { _ = rows.Close() }()

	snapshots := make([]service.CRSAccountPreviewSnapshot, 0)
	currentIndex := -1
	var currentAccountID int64
	hasCurrentAccount := false
	var lastListingID int64
	for rows.Next() {
		var (
			accountID  int64
			crsID      string
			listingID  sql.NullInt64
			rowVersion sql.NullInt64
		)
		if err := rows.Scan(&accountID, &crsID, &listingID, &rowVersion); err != nil {
			return nil, service.ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
				"stage": "repository_scan",
			}).WithCause(err)
		}
		if accountID <= 0 || strings.TrimSpace(crsID) == "" {
			return nil, service.ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
				"stage": "repository_invalid_account_snapshot",
			})
		}
		if !hasCurrentAccount || accountID != currentAccountID {
			snapshots = append(snapshots, service.CRSAccountPreviewSnapshot{
				CRSAccountID:   crsID,
				LocalAccountID: accountID,
				RoomBindings:   make([]service.CRSAccountRoomBindingSnapshot, 0),
			})
			currentIndex = len(snapshots) - 1
			currentAccountID = accountID
			hasCurrentAccount = true
			lastListingID = 0
		} else if snapshots[currentIndex].CRSAccountID != crsID {
			return nil, service.ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
				"account_id": strconv.FormatInt(accountID, 10),
				"stage":      "repository_inconsistent_crs_account_id",
			})
		}
		if listingID.Valid != rowVersion.Valid {
			return nil, service.ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
				"account_id": strconv.FormatInt(accountID, 10),
				"stage":      "repository_incomplete_room_snapshot",
			})
		}
		if !listingID.Valid {
			continue
		}
		if listingID.Int64 <= 0 || rowVersion.Int64 <= 0 {
			return nil, service.ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
				"account_id": strconv.FormatInt(accountID, 10),
				"stage":      "repository_invalid_room_snapshot",
			})
		}
		if listingID.Int64 == lastListingID {
			continue
		}
		snapshots[currentIndex].RoomBindings = append(
			snapshots[currentIndex].RoomBindings,
			service.CRSAccountRoomBindingSnapshot{
				ListingID:  listingID.Int64,
				RowVersion: rowVersion.Int64,
			},
		)
		lastListingID = listingID.Int64
	}
	if err := rows.Err(); err != nil {
		return nil, service.ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
			"stage": "repository_iteration",
		}).WithCause(err)
	}
	return snapshots, nil
}

type accountMutationRoomBinding struct {
	accountID        int64
	listingID        int64
	rowVersion       int64
	revisionID       *int64
	lifecycleStatus  string
	blockers         service.AccountShareRoomBlockers
	openBindingCount int
}

type accountMutationLockedTarget struct {
	request service.AccountMutationGuardTarget
	before  *service.Account
	groups  []int64
	diff    service.AccountMutationDiff
}

func (r *accountRepository) WithAccountMutationGuard(
	ctx context.Context,
	request service.AccountMutationGuardRequest,
	mutate func(context.Context) error,
) error {
	if r == nil || r.client == nil {
		return service.ErrAccountMutationGuardUnavailable
	}
	if mutate == nil {
		return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{"stage": "mutation_callback"})
	}
	targets, ids, err := normalizeAccountMutationTargets(request.Targets)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return mutate(ctx)
	}
	if dbent.TxFromContext(ctx) != nil {
		return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{"stage": "nested_transaction"})
	}
	if r.sql == nil {
		return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{"stage": "discovery_executor"})
	}

	// Discover room bindings before the transaction so the write path can keep
	// the global lock order: listing -> account -> membership/binding. The
	// bindings are re-read after account locks are acquired; a newly committed
	// binding that was not covered by this pre-lock set fails fast and retries
	// instead of taking a listing lock in reverse order.
	discoveredRoomBindings, err := loadAccountMutationRoomBindings(ctx, r.sql, ids)
	if err != nil {
		return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{"stage": "room_binding_discovery"}).WithCause(err)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	txClient := tx.Client()
	exec := sqlExecutorFromEntClient(txClient)
	if exec == nil {
		return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{"stage": "transaction_executor"})
	}
	if err := lockAndHydrateAccountMutationRooms(txCtx, exec, discoveredRoomBindings); err != nil {
		return err
	}

	lockedEntities, err := txClient.Account.Query().
		Where(dbaccount.IDIn(ids...)).
		Order(dbaccount.ByID()).
		ForUpdate().
		All(txCtx)
	if err != nil {
		return translateAccountPersistenceError(err, service.ErrAccountNotFound)
	}
	if len(lockedEntities) != len(ids) {
		return service.ErrAccountNotFound
	}

	lockedTargets := make(map[int64]*accountMutationLockedTarget, len(ids))
	sensitiveIDs := make([]int64, 0, len(ids))
	for _, entity := range lockedEntities {
		target := targets[entity.ID]
		before := accountEntityToService(entity)
		if before == nil || target.After == nil {
			return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
				"account_id": strconv.FormatInt(entity.ID, 10),
				"stage":      "target_snapshot",
			})
		}
		if target.ExpectedUpdatedAt.IsZero() || !entity.UpdatedAt.Equal(target.ExpectedUpdatedAt) {
			return service.ErrAccountMutationStale.WithMetadata(map[string]string{
				"account_id": strconv.FormatInt(entity.ID, 10),
			})
		}
		groups, err := loadAccountGroupIDsWithClient(txCtx, txClient, entity.ID)
		if err != nil {
			return err
		}
		diff := service.ClassifyAccountMutation(before, target.After, groups, target.GroupIDs)
		lockedTargets[entity.ID] = &accountMutationLockedTarget{
			request: target,
			before:  before,
			groups:  groups,
			diff:    diff,
		}
		if diff.Sensitive {
			sensitiveIDs = append(sensitiveIDs, entity.ID)
		}
	}

	roomBindings, err := loadAccountMutationRoomBindings(txCtx, exec, sensitiveIDs)
	if err != nil {
		return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{"stage": "room_bindings"}).WithCause(err)
	}
	if err := hydrateAccountMutationBindingsFromPrelocked(discoveredRoomBindings, roomBindings); err != nil {
		return err
	}
	if err := authorizeAccountMutation(request, lockedTargets, roomBindings); err != nil {
		return err
	}

	if err := mutate(service.WithAccountMutationGuardContext(txCtx)); err != nil {
		return err
	}

	if request.ActorIsAdmin && request.ForceActiveEdit && len(roomBindings) > 0 {
		if err := appendForcedAccountMutationEvents(txCtx, exec, request, lockedTargets, roomBindings, txClient); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.syncSchedulerAccountSnapshots(context.WithoutCancel(ctx), ids)
	return nil
}

func normalizeAccountMutationTargets(
	input []service.AccountMutationGuardTarget,
) (map[int64]service.AccountMutationGuardTarget, []int64, error) {
	targets := make(map[int64]service.AccountMutationGuardTarget, len(input))
	ids := make([]int64, 0, len(input))
	for _, target := range input {
		if target.AccountID <= 0 || target.After == nil || target.After.ID != target.AccountID {
			return nil, nil, service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{"stage": "invalid_target"})
		}
		if _, exists := targets[target.AccountID]; exists {
			return nil, nil, service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
				"account_id": strconv.FormatInt(target.AccountID, 10),
				"stage":      "duplicate_target",
			})
		}
		target.GroupIDs = uniqueSortedPositiveInt64s(target.GroupIDs)
		targets[target.AccountID] = target
		ids = append(ids, target.AccountID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return targets, ids, nil
}

func loadAccountMutationRoomBindings(
	ctx context.Context,
	exec sqlQueryExecutor,
	accountIDs []int64,
) ([]accountMutationRoomBinding, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	rows, err := exec.QueryContext(ctx, `
		SELECT room_account.account_id, room_account.listing_id
		FROM account_share_room_accounts room_account
		JOIN account_share_listings listing
			ON listing.id = room_account.listing_id
			AND listing.deleted_at IS NULL
		WHERE room_account.account_id = ANY($1)
		ORDER BY room_account.account_id, room_account.listing_id
	`, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	bindings := make([]accountMutationRoomBinding, 0, len(accountIDs))
	seen := make(map[string]struct{}, len(accountIDs))
	for rows.Next() {
		var binding accountMutationRoomBinding
		if err := rows.Scan(&binding.accountID, &binding.listingID); err != nil {
			return nil, err
		}
		key := strconv.FormatInt(binding.accountID, 10) + ":" + strconv.FormatInt(binding.listingID, 10)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return bindings, nil
}

func lockAndHydrateAccountMutationRooms(
	ctx context.Context,
	exec sqlQueryExecutor,
	bindings []accountMutationRoomBinding,
) error {
	if len(bindings) == 0 {
		return nil
	}
	listingIDs := make([]int64, 0, len(bindings))
	for _, binding := range bindings {
		listingIDs = append(listingIDs, binding.listingID)
	}
	listingIDs = uniqueSortedPositiveInt64s(listingIDs)
	rows, err := exec.QueryContext(ctx, `
		SELECT
			id,
			row_version,
			current_revision_id,
			status,
			(
				edit_session_id IS NOT NULL
				AND editing_expires_at IS NOT NULL
				AND editing_expires_at > NOW()
			),
			(pending_operation_id IS NOT NULL),
			COALESCE(pending_operation_id::text, '')
		FROM account_share_listings
		WHERE id = ANY($1)
			AND deleted_at IS NULL
		ORDER BY id
		FOR UPDATE
	`, pq.Array(listingIDs))
	if err != nil {
		return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{"stage": "room_lock"}).WithCause(err)
	}
	defer func() { _ = rows.Close() }()
	type roomVersion struct {
		version          int64
		revision         sql.NullInt64
		lifecycleStatus  string
		blockers         service.AccountShareRoomBlockers
		openBindingCount int
	}
	versions := make(map[int64]roomVersion, len(listingIDs))
	for rows.Next() {
		var id int64
		var version roomVersion
		if err := rows.Scan(
			&id,
			&version.version,
			&version.revision,
			&version.lifecycleStatus,
			&version.blockers.ValidEditSession,
			&version.blockers.ConflictingOperation,
			&version.blockers.ConflictingOperationID,
		); err != nil {
			return err
		}
		versions[id] = version
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
			"stage": "room_lock_close",
		}).WithCause(err)
	}
	if len(versions) != len(listingIDs) {
		return service.ErrAccountMutationStale.WithMetadata(map[string]string{"resource": "room"})
	}

	blockerRows, err := exec.QueryContext(ctx, `
		WITH membership_blockers AS (
			SELECT
				listing_id,
				COUNT(*) FILTER (WHERE status = 'active')::int AS active_count,
				COUNT(*) FILTER (WHERE status = 'queued')::int AS queued_count,
				COUNT(*) FILTER (WHERE status = 'ending')::int AS ending_count,
				COUNT(*) FILTER (
					WHERE settlement_status IN ('pending', 'processing', 'failed')
				)::int AS settlement_count
			FROM account_share_memberships
			WHERE listing_id = ANY($1)
				AND deleted_at IS NULL
			GROUP BY listing_id
		),
		billing_blockers AS (
			SELECT listing_id, COUNT(*)::int AS pending_count
			FROM account_share_request_billing_intents
			WHERE listing_id = ANY($1)
				AND status NOT IN ('settled', 'cancelled')
			GROUP BY listing_id
		),
		binding_blockers AS (
			SELECT listing_id, COUNT(*)::int AS open_count
			FROM account_share_membership_account_bindings
			WHERE listing_id = ANY($1)
				AND unbound_at IS NULL
			GROUP BY listing_id
		)
		SELECT
			listing.id,
			COALESCE(membership_blockers.active_count, 0),
			COALESCE(membership_blockers.queued_count, 0),
			COALESCE(membership_blockers.ending_count, 0),
			COALESCE(membership_blockers.settlement_count, 0),
			COALESCE(billing_blockers.pending_count, 0),
			COALESCE(binding_blockers.open_count, 0)
		FROM account_share_listings listing
		LEFT JOIN membership_blockers ON membership_blockers.listing_id = listing.id
		LEFT JOIN billing_blockers ON billing_blockers.listing_id = listing.id
		LEFT JOIN binding_blockers ON binding_blockers.listing_id = listing.id
		WHERE listing.id = ANY($1)
			AND listing.deleted_at IS NULL
		ORDER BY listing.id
	`, pq.Array(listingIDs))
	if err != nil {
		return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
			"stage": "room_blockers",
		}).WithCause(err)
	}
	defer func() { _ = blockerRows.Close() }()
	blockerRowsSeen := 0
	for blockerRows.Next() {
		var listingID int64
		var version roomVersion
		if err := blockerRows.Scan(
			&listingID,
			&version.blockers.ActiveMembershipCount,
			&version.blockers.QueuedMembershipCount,
			&version.blockers.EndingMembershipCount,
			&version.blockers.SynchronousBillingPendingCount,
			&version.blockers.PendingBillingIntentCount,
			&version.openBindingCount,
		); err != nil {
			return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
				"stage": "room_blocker_scan",
			}).WithCause(err)
		}
		current, ok := versions[listingID]
		if !ok {
			return service.ErrAccountMutationStale.WithMetadata(map[string]string{"resource": "room_blocker"})
		}
		current.blockers.ActiveMembershipCount = version.blockers.ActiveMembershipCount
		current.blockers.QueuedMembershipCount = version.blockers.QueuedMembershipCount
		current.blockers.EndingMembershipCount = version.blockers.EndingMembershipCount
		current.blockers.SynchronousBillingPendingCount = version.blockers.SynchronousBillingPendingCount
		current.blockers.PendingBillingIntentCount = version.blockers.PendingBillingIntentCount
		current.openBindingCount = version.openBindingCount
		versions[listingID] = current
		blockerRowsSeen++
	}
	if err := blockerRows.Err(); err != nil {
		return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
			"stage": "room_blocker_iteration",
		}).WithCause(err)
	}
	if blockerRowsSeen != len(listingIDs) {
		return service.ErrAccountMutationStale.WithMetadata(map[string]string{"resource": "room_blocker"})
	}
	for i := range bindings {
		version := versions[bindings[i].listingID]
		bindings[i].rowVersion = version.version
		bindings[i].lifecycleStatus = version.lifecycleStatus
		bindings[i].blockers = version.blockers
		bindings[i].openBindingCount = version.openBindingCount
		if version.revision.Valid {
			revisionID := version.revision.Int64
			bindings[i].revisionID = &revisionID
		}
	}
	return nil
}

func hydrateAccountMutationBindingsFromPrelocked(
	prelocked []accountMutationRoomBinding,
	current []accountMutationRoomBinding,
) error {
	type roomVersion struct {
		rowVersion       int64
		revisionID       *int64
		lifecycleStatus  string
		blockers         service.AccountShareRoomBlockers
		openBindingCount int
	}
	versions := make(map[int64]roomVersion, len(prelocked))
	for _, binding := range prelocked {
		if binding.listingID <= 0 || binding.rowVersion <= 0 {
			continue
		}
		version := roomVersion{
			rowVersion:       binding.rowVersion,
			lifecycleStatus:  binding.lifecycleStatus,
			blockers:         binding.blockers,
			openBindingCount: binding.openBindingCount,
		}
		if binding.revisionID != nil {
			revisionID := *binding.revisionID
			version.revisionID = &revisionID
		}
		versions[binding.listingID] = version
	}
	for i := range current {
		version, ok := versions[current[i].listingID]
		if !ok {
			return service.ErrAccountMutationStale.WithMetadata(map[string]string{
				"account_id": strconv.FormatInt(current[i].accountID, 10),
				"listing_id": strconv.FormatInt(current[i].listingID, 10),
				"resource":   "room_binding",
			})
		}
		current[i].rowVersion = version.rowVersion
		current[i].lifecycleStatus = version.lifecycleStatus
		current[i].blockers = version.blockers
		current[i].openBindingCount = version.openBindingCount
		if version.revisionID != nil {
			revisionID := *version.revisionID
			current[i].revisionID = &revisionID
		}
	}
	return nil
}

func authorizeAccountMutation(
	request service.AccountMutationGuardRequest,
	targets map[int64]*accountMutationLockedTarget,
	bindings []accountMutationRoomBinding,
) error {
	if len(bindings) == 0 {
		return nil
	}
	listingIDs := make([]int64, 0, len(bindings))
	accountIDs := make([]int64, 0, len(bindings))
	changedFields := make([]string, 0)
	for _, binding := range bindings {
		listingIDs = append(listingIDs, binding.listingID)
		accountIDs = append(accountIDs, binding.accountID)
		if target := targets[binding.accountID]; target != nil {
			changedFields = append(changedFields, target.diff.ChangedFields...)
		}
	}
	listingIDs = uniqueSortedPositiveInt64s(listingIDs)
	accountIDs = uniqueSortedPositiveInt64s(accountIDs)
	changedFields = uniqueSortedStrings(changedFields)
	metadata := map[string]string{
		"account_ids":    joinAccountDeletionInt64s(accountIDs),
		"listing_ids":    joinAccountDeletionInt64s(listingIDs),
		"changed_fields": strings.Join(changedFields, ","),
	}

	switch strings.TrimSpace(request.Intent) {
	case service.AccountMutationIntentSystemTokenRefresh:
		for _, binding := range bindings {
			target := targets[binding.accountID]
			if target == nil || !service.AccountMutationAllowedForSystemTokenRefresh(target.diff) {
				return service.ErrAccountMutationSystemIntentInvalid.WithMetadata(metadata)
			}
		}
		return nil
	case service.AccountMutationIntentOwner, "":
		if !request.ActorIsAdmin {
			for _, binding := range bindings {
				if binding.lifecycleStatus == service.AccountShareListingStatusPaused &&
					!binding.blockers.Any() &&
					binding.openBindingCount == 0 {
					continue
				}
				for key, value := range binding.blockers.Metadata() {
					metadata[key] = value
				}
				metadata["listing_id"] = strconv.FormatInt(binding.listingID, 10)
				metadata["lifecycle_status"] = binding.lifecycleStatus
				metadata["open_binding_count"] = strconv.Itoa(binding.openBindingCount)
				return service.ErrAccountMutationBlocked.WithMetadata(metadata)
			}
			return nil
		}
	case service.AccountMutationIntentAdmin:
	default:
		return service.ErrAccountMutationSystemIntentInvalid.WithMetadata(metadata)
	}

	if !request.ActorIsAdmin || request.ActorUserID <= 0 {
		return service.ErrAccountMutationForceRequired.WithMetadata(metadata)
	}
	if !request.ForceActiveEdit {
		metadata["missing"] = "force_active_edit"
		return service.ErrAccountMutationForceRequired.WithMetadata(metadata)
	}
	if !request.Confirmed {
		metadata["missing"] = "confirmed"
		return service.ErrAccountMutationForceRequired.WithMetadata(metadata)
	}
	if strings.TrimSpace(request.Reason) == "" {
		metadata["missing"] = "reason"
		return service.ErrAccountMutationForceRequired.WithMetadata(metadata)
	}

	if len(listingIDs) > 1 && request.ExpectedListingVersion != nil {
		metadata["missing"] = "expected_versions"
		return service.ErrAccountMutationForceRequired.WithMetadata(metadata)
	}
	for _, binding := range bindings {
		expected, ok := request.ExpectedListingVersions[binding.listingID]
		if !ok && len(listingIDs) == 1 && request.ExpectedListingVersion != nil {
			expected = *request.ExpectedListingVersion
			ok = true
		}
		if !ok || expected <= 0 {
			metadata["missing"] = "expected_version"
			if len(listingIDs) > 1 {
				metadata["missing"] = "expected_versions"
			}
			return service.ErrAccountMutationForceRequired.WithMetadata(metadata)
		}
		if expected != binding.rowVersion {
			metadata["listing_id"] = strconv.FormatInt(binding.listingID, 10)
			metadata["expected_version"] = strconv.FormatInt(expected, 10)
			metadata["actual_version"] = strconv.FormatInt(binding.rowVersion, 10)
			return service.ErrAccountMutationVersionConflict.WithMetadata(metadata)
		}
	}
	return nil
}

func appendForcedAccountMutationEvents(
	ctx context.Context,
	exec sqlQueryExecutor,
	request service.AccountMutationGuardRequest,
	targets map[int64]*accountMutationLockedTarget,
	bindings []accountMutationRoomBinding,
	txClient *dbent.Client,
) error {
	operationID := strings.TrimSpace(request.OperationID)
	if operationID == "" {
		operationID = uuid.NewString()
	}
	afterEntities, err := txClient.Account.Query().
		Where(dbaccount.IDIn(accountMutationTargetIDs(targets)...)).
		Order(dbaccount.ByID()).
		All(ctx)
	if err != nil {
		return err
	}
	afterByID := make(map[int64]*service.Account, len(afterEntities))
	afterGroupsByID := make(map[int64][]int64, len(afterEntities))
	for _, entity := range afterEntities {
		afterByID[entity.ID] = accountEntityToService(entity)
		groups, err := loadAccountGroupIDsWithClient(ctx, txClient, entity.ID)
		if err != nil {
			return err
		}
		afterGroupsByID[entity.ID] = groups
	}

	bindingsByListing := make(map[int64][]accountMutationRoomBinding)
	for _, binding := range bindings {
		bindingsByListing[binding.listingID] = append(bindingsByListing[binding.listingID], binding)
	}
	listingIDs := make([]int64, 0, len(bindingsByListing))
	for listingID := range bindingsByListing {
		listingIDs = append(listingIDs, listingID)
	}
	sort.Slice(listingIDs, func(i, j int) bool { return listingIDs[i] < listingIDs[j] })

	for _, listingID := range listingIDs {
		listingBindings := bindingsByListing[listingID]
		changes := make([]map[string]any, 0, len(listingBindings))
		var revisionID any
		var rowVersion int64
		for _, binding := range listingBindings {
			target := targets[binding.accountID]
			after := afterByID[binding.accountID]
			if target == nil || after == nil {
				return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{"stage": "audit_snapshot"})
			}
			actualDiff := service.ClassifyAccountMutation(target.before, after, target.groups, afterGroupsByID[binding.accountID])
			if !actualDiff.Sensitive {
				continue
			}
			changes = append(changes, map[string]any{
				"account_id":              binding.accountID,
				"changed_fields":          actualDiff.ChangedFields,
				"credential_changed_keys": actualDiff.CredentialChangedKeys,
				"extra_changed_keys":      actualDiff.ExtraChangedKeys,
				"before":                  accountMutationAuditSnapshot(target.before, target.groups),
				"after":                   accountMutationAuditSnapshot(after, afterGroupsByID[binding.accountID]),
			})
			rowVersion = binding.rowVersion
			if binding.revisionID != nil {
				revisionID = *binding.revisionID
			}
		}
		if len(changes) == 0 {
			continue
		}
		payload, err := json.Marshal(map[string]any{
			"operation_id":  operationID,
			"source":        service.AccountMutationIntentAdmin,
			"force_applied": true,
			"row_version":   rowVersion,
			"changes":       changes,
		})
		if err != nil {
			return err
		}
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO account_share_room_events (
				listing_id, revision_id, event_type, actor_user_id, actor_role, reason, payload, created_at
			) VALUES (
				$1, $2, 'account.admin_forced_update', $3, 'admin', $4, $5::jsonb, NOW()
			)
		`, listingID, revisionID, request.ActorUserID, strings.TrimSpace(request.Reason), string(payload)); err != nil {
			return service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
				"listing_id": strconv.FormatInt(listingID, 10),
				"stage":      "audit_event",
			}).WithCause(err)
		}
	}
	return nil
}

func accountMutationAuditSnapshot(account *service.Account, groupIDs []int64) map[string]any {
	if account == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":                    account.ID,
		"name":                  account.Name,
		"platform":              account.Platform,
		"account_level":         account.AccountLevel,
		"type":                  account.Type,
		"owner_user_id":         account.OwnerUserID,
		"share_mode":            account.ShareMode,
		"share_status":          account.ShareStatus,
		"share_policy_id":       account.SharePolicyID,
		"proxy_id":              account.ProxyID,
		"concurrency":           account.Concurrency,
		"priority":              account.Priority,
		"rate_multiplier":       account.RateMultiplier,
		"load_factor":           account.LoadFactor,
		"status":                account.Status,
		"schedulable":           account.Schedulable,
		"group_ids":             uniqueSortedPositiveInt64s(groupIDs),
		"expires_at":            account.ExpiresAt,
		"auto_pause_on_expired": account.AutoPauseOnExpired,
		"updated_at":            account.UpdatedAt,
	}
}

func accountMutationTargetIDs(targets map[int64]*accountMutationLockedTarget) []int64 {
	ids := make([]int64, 0, len(targets))
	for id := range targets {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func loadAccountGroupIDsWithClient(ctx context.Context, client *dbent.Client, accountID int64) ([]int64, error) {
	entries, err := client.AccountGroup.Query().
		Where(dbaccountgroup.AccountIDEQ(accountID)).
		Order(dbent.Asc(dbaccountgroup.FieldPriority), dbent.Asc(dbaccountgroup.FieldGroupID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.GroupID)
	}
	return ids, nil
}

func (r *accountRepository) Update(ctx context.Context, account *service.Account) error {
	if account == nil {
		return nil
	}

	client := clientFromContext(ctx, r.client)
	builder := applyAccountUpdateFields(client.Account.UpdateOneID(account.ID), account)

	updated, err := builder.Save(ctx)
	if err != nil {
		return translateAccountPersistenceError(err, service.ErrAccountNotFound)
	}
	account.UpdatedAt = updated.UpdatedAt
	if err := r.syncAccountErrorSince(ctx, account.ID, account.Status); err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, txAwareSQLExecutor(ctx, r.sql, r.client), service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload(account.GroupIDs)); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue account update failed: account=%d err=%v", account.ID, err)
	}
	// 普通账号编辑（如 model_mapping / credentials）也需要立即刷新单账号快照，
	// 否则网关在 outbox worker 延迟或异常时仍可能读到旧配置。
	if dbent.TxFromContext(ctx) == nil {
		r.syncSchedulerAccountSnapshot(ctx, account.ID)
	}
	return nil
}

func applyAccountUpdateFields(builder *dbent.AccountUpdateOne, account *service.Account) *dbent.AccountUpdateOne {
	builder.
		SetName(account.Name).
		SetNillableNotes(account.Notes).
		SetPlatform(account.Platform).
		SetAccountLevel(service.NormalizeAccountLevel(account.AccountLevel)).
		SetType(account.Type).
		SetCredentials(normalizeJSONMap(account.Credentials)).
		SetExtra(normalizeJSONMap(account.Extra)).
		SetShareMode(service.NormalizeAccountShareMode(account.ShareMode)).
		SetShareStatus(service.NormalizeAccountShareStatus(account.ShareStatus)).
		SetConcurrency(account.Concurrency).
		SetLoadFactorPaidCeiling(normalizeLoadFactorPaidCeiling(account.LoadFactorPaidCeiling)).
		SetPriority(account.Priority).
		SetStatus(account.Status).
		SetErrorMessage(account.ErrorMessage).
		SetSchedulable(account.Schedulable).
		SetAutoPauseOnExpired(account.AutoPauseOnExpired)

	if account.RateMultiplier != nil {
		builder.SetRateMultiplier(*account.RateMultiplier)
	}
	if account.LoadFactor != nil {
		builder.SetLoadFactor(*account.LoadFactor)
	} else {
		builder.ClearLoadFactor()
	}
	if account.OwnerUserID != nil {
		builder.SetOwnerUserID(*account.OwnerUserID)
	} else {
		builder.ClearOwnerUserID()
	}
	if account.SharePolicyID != nil {
		builder.SetSharePolicyID(*account.SharePolicyID)
	} else {
		builder.ClearSharePolicyID()
	}

	if account.ProxyID != nil {
		builder.SetProxyID(*account.ProxyID)
	} else {
		builder.ClearProxyID()
	}
	if account.LastUsedAt != nil {
		builder.SetLastUsedAt(*account.LastUsedAt)
	} else {
		builder.ClearLastUsedAt()
	}
	if account.ExpiresAt != nil {
		builder.SetExpiresAt(*account.ExpiresAt)
	} else {
		builder.ClearExpiresAt()
	}
	if account.RateLimitedAt != nil {
		builder.SetRateLimitedAt(*account.RateLimitedAt)
	} else {
		builder.ClearRateLimitedAt()
	}
	if account.RateLimitResetAt != nil {
		builder.SetRateLimitResetAt(*account.RateLimitResetAt)
	} else {
		builder.ClearRateLimitResetAt()
	}
	if account.OverloadUntil != nil {
		builder.SetOverloadUntil(*account.OverloadUntil)
	} else {
		builder.ClearOverloadUntil()
	}
	if account.SessionWindowStart != nil {
		builder.SetSessionWindowStart(*account.SessionWindowStart)
	} else {
		builder.ClearSessionWindowStart()
	}
	if account.SessionWindowEnd != nil {
		builder.SetSessionWindowEnd(*account.SessionWindowEnd)
	} else {
		builder.ClearSessionWindowEnd()
	}
	if account.SessionWindowStatus != "" {
		builder.SetSessionWindowStatus(account.SessionWindowStatus)
	} else {
		builder.ClearSessionWindowStatus()
	}
	if account.Notes == nil {
		builder.ClearNotes()
	}
	return builder
}

func (r *accountRepository) UpdateOwnedAccountWithLoadFactorCredits(ctx context.Context, ownerUserID int64, account *service.Account) (*service.Account, error) {
	if account == nil {
		return nil, service.ErrAccountNilInput
	}
	if ownerUserID <= 0 {
		return nil, service.ErrUserNotFound
	}
	if account.LoadFactor == nil || *account.LoadFactor <= 0 || *account.LoadFactor > service.AccountMaxLoadFactor {
		return nil, service.ErrOwnedAccountLoadFactorOutOfRange
	}

	var tx *dbent.Tx
	txCtx := ctx
	txClient := clientFromContext(ctx, r.client)
	if dbent.TxFromContext(ctx) == nil {
		var err error
		tx, err = r.client.Tx(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback() }()
		txCtx = dbent.NewTxContext(ctx, tx)
		txClient = tx.Client()
	}
	exec := sqlExecutorFromEntClient(txClient)
	if exec == nil {
		return nil, fmt.Errorf("transaction sql executor is unavailable")
	}

	creditsBalance, creditsUsedTotal, err := lockUserLoadFactorCredits(txCtx, exec, ownerUserID)
	if err != nil {
		return nil, err
	}
	dbPaidCeiling, err := lockOwnedAccountLoadFactorCeiling(txCtx, exec, ownerUserID, account.ID)
	if err != nil {
		return nil, err
	}

	targetLoadFactor := *account.LoadFactor
	paidCeiling := normalizeLoadFactorPaidCeiling(dbPaidCeiling)
	charge := targetLoadFactor - paidCeiling
	if charge < 0 {
		charge = 0
	}
	if charge > creditsBalance {
		return nil, service.ErrOwnedAccountLoadFactorCreditsInsufficient.WithMetadata(map[string]string{
			"required": strconv.Itoa(charge),
			"balance":  strconv.Itoa(creditsBalance),
		})
	}

	nextPaidCeiling := paidCeiling
	if targetLoadFactor > nextPaidCeiling {
		nextPaidCeiling = targetLoadFactor
	}
	account.LoadFactorPaidCeiling = nextPaidCeiling

	if charge > 0 {
		if err := debitUserLoadFactorCredits(txCtx, exec, userLoadFactorCreditDebitInput{
			UserID:          ownerUserID,
			AccountID:       account.ID,
			Target:          targetLoadFactor,
			PreviousCeiling: paidCeiling,
			NextCeiling:     nextPaidCeiling,
			Amount:          charge,
			BalanceBefore:   creditsBalance,
			BalanceAfter:    creditsBalance - charge,
			UsedBefore:      creditsUsedTotal,
			UsedAfter:       creditsUsedTotal + charge,
		}); err != nil {
			return nil, err
		}
	}

	updated, err := applyAccountUpdateFields(txClient.Account.UpdateOneID(account.ID), account).Save(txCtx)
	if err != nil {
		return nil, translateAccountPersistenceError(err, service.ErrAccountNotFound)
	}
	account.UpdatedAt = updated.UpdatedAt
	if err := r.syncAccountErrorSince(txCtx, account.ID, account.Status); err != nil {
		return nil, err
	}
	if err := enqueueSchedulerOutbox(txCtx, exec, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload(account.GroupIDs)); err != nil {
		return nil, err
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		r.syncSchedulerAccountSnapshot(ctx, account.ID)
	}
	return account, nil
}

type userLoadFactorCreditDebitInput struct {
	UserID          int64
	AccountID       int64
	Target          int
	PreviousCeiling int
	NextCeiling     int
	Amount          int
	BalanceBefore   int
	BalanceAfter    int
	UsedBefore      int
	UsedAfter       int
}

func lockUserLoadFactorCredits(ctx context.Context, exec sqlQueryExecutor, userID int64) (int, int, error) {
	rows, err := exec.QueryContext(ctx, `
		SELECT load_factor_credits_balance, load_factor_credits_used_total
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, userID)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return 0, 0, service.ErrUserNotFound
	}
	var balance int
	var usedTotal int
	if err := rows.Scan(&balance, &usedTotal); err != nil {
		return 0, 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	return balance, usedTotal, nil
}

func lockOwnedAccountLoadFactorCeiling(ctx context.Context, exec sqlQueryExecutor, ownerUserID, accountID int64) (int, error) {
	rows, err := exec.QueryContext(ctx, `
		SELECT load_factor_paid_ceiling
		FROM accounts
		WHERE id = $1
			AND owner_user_id = $2
			AND deleted_at IS NULL
		FOR UPDATE
	`, accountID, ownerUserID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return 0, service.ErrAccountNotFound
	}
	var paidCeiling int
	if err := rows.Scan(&paidCeiling); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return paidCeiling, nil
}

func debitUserLoadFactorCredits(ctx context.Context, exec sqlQueryExecutor, in userLoadFactorCreditDebitInput) error {
	if _, err := exec.ExecContext(ctx, `
		UPDATE users
		SET load_factor_credits_balance = $1,
			load_factor_credits_used_total = $2,
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL
	`, in.BalanceAfter, in.UsedAfter, in.UserID); err != nil {
		return err
	}

	metadata, err := json.Marshal(map[string]any{
		"account_id":       in.AccountID,
		"target":           in.Target,
		"previous_ceiling": in.PreviousCeiling,
		"next_ceiling":     in.NextCeiling,
	})
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO user_load_factor_ledger (
			user_id, account_id, direction, amount, reason,
			balance_before, balance_after, operator_user_id, metadata
		) VALUES (
			$1, $2, 'debit', $3, 'account_load_factor_increase',
			$4, $5, NULL, $6::jsonb
		)
	`, in.UserID, in.AccountID, in.Amount, in.BalanceBefore, in.BalanceAfter, string(metadata))
	return err
}

func (r *accountRepository) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	account, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	after := *account
	after.Credentials = copyJSONMap(credentials)
	target := service.AccountMutationGuardTarget{
		AccountID:         id,
		ExpectedUpdatedAt: account.UpdatedAt,
		After:             &after,
		GroupIDs:          append([]int64(nil), account.GroupIDs...),
	}
	return r.WithAccountMutationGuard(ctx, service.AccountMutationGuardRequest{
		Targets: []service.AccountMutationGuardTarget{target},
		Intent:  service.AccountMutationIntentSystemTokenRefresh,
	}, func(txCtx context.Context) error {
		client := clientFromContext(txCtx, r.client)
		_, err := client.Account.UpdateOneID(id).
			SetCredentials(normalizeJSONMap(credentials)).
			Save(txCtx)
		if err != nil {
			return translatePersistenceError(err, service.ErrAccountNotFound, nil)
		}
		return enqueueSchedulerOutbox(
			txCtx,
			txAwareSQLExecutor(txCtx, r.sql, r.client),
			service.SchedulerOutboxEventAccountChanged,
			&id,
			nil,
			nil,
		)
	})
}

func (r *accountRepository) Delete(ctx context.Context, id int64) error {
	return r.DeleteIfUnblocked(ctx, id)
}

func (r *accountRepository) DeleteIfUnblocked(ctx context.Context, accountID int64) error {
	if accountID <= 0 {
		return service.ErrAccountNotFound
	}
	return r.DeleteManyIfUnblocked(ctx, []int64{accountID})
}

func (r *accountRepository) DeleteManyIfUnblocked(ctx context.Context, accountIDs []int64) error {
	return r.deleteManyIfUnblocked(ctx, accountIDs, nil)
}

func (r *accountRepository) DeleteOwnedIfUnblocked(ctx context.Context, ownerUserID, accountID int64) error {
	if ownerUserID <= 0 || accountID <= 0 {
		return service.ErrAccountNotFound
	}
	return r.deleteManyIfUnblocked(ctx, []int64{accountID}, &ownerUserID)
}

func (r *accountRepository) DeleteManyOwnedIfUnblocked(ctx context.Context, ownerUserID int64, accountIDs []int64) error {
	if ownerUserID <= 0 {
		return service.ErrAccountNotFound
	}
	return r.deleteManyIfUnblocked(ctx, accountIDs, &ownerUserID)
}

func (r *accountRepository) deleteManyIfUnblocked(ctx context.Context, accountIDs []int64, expectedOwnerUserID *int64) error {
	for _, accountID := range accountIDs {
		if accountID <= 0 {
			return service.ErrAccountNotFound
		}
	}
	ids := normalizeAccountDeletionIDs(accountIDs)
	if len(ids) == 0 {
		return nil
	}
	if r == nil || r.client == nil {
		return accountDeletionGuardUnavailable(ids[0], "repository", errors.New("account repository is not configured"))
	}

	// Account row locks are acquired in a stable order. Besides serializing
	// competing deletions, the FOR UPDATE lock conflicts with the key-share lock
	// used by foreign-key inserts, closing the check/delete race for room rows,
	// memberships, and billing intents that retain a live account FK. Owned
	// deletion also revalidates every owner while these row locks are held.
	baseClient := clientFromContext(ctx, r.client)
	tx, err := baseClient.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return err
	}

	txCtx := ctx
	txClient := baseClient
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		txClient = tx.Client()
		txCtx = dbent.NewTxContext(ctx, tx)
	}

	lockedAccountCount := 0
	for start := 0; start < len(ids); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		lockQuery := txClient.Account.Query().
			Where(dbaccount.IDIn(ids[start:end]...))
		lockedAccounts, lockErr := lockQuery.
			Order(dbaccount.ByID()).
			ForUpdate().
			All(txCtx)
		if lockErr != nil {
			return translatePersistenceError(lockErr, service.ErrAccountNotFound, nil)
		}
		if ownershipErr := validateLockedAccountDeletionOwnership(lockedAccounts, expectedOwnerUserID); ownershipErr != nil {
			return ownershipErr
		}
		lockedAccountCount += len(lockedAccounts)
	}
	if lockedAccountCount != len(ids) {
		return service.ErrAccountNotFound
	}

	exec := sqlExecutorFromEntClient(txClient)
	if exec == nil {
		return accountDeletionGuardUnavailable(ids[0], "sql_executor", errors.New("transaction sql executor is unavailable"))
	}
	for _, accountID := range ids {
		blockers, checkErr := loadAccountDeletionBlockers(txCtx, exec, accountID)
		if checkErr != nil {
			return accountDeletionGuardUnavailable(accountID, "blocker_query", checkErr)
		}
		if blockers.hasAny() {
			return blockers.conflictError(accountID)
		}
	}

	groupIDsByAccount := make(map[int64][]int64, len(ids))
	for start := 0; start < len(ids); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		groupEntries, groupErr := txClient.AccountGroup.Query().
			Where(dbaccountgroup.AccountIDIn(ids[start:end]...)).
			All(txCtx)
		if groupErr != nil {
			return groupErr
		}
		for _, entry := range groupEntries {
			groupIDsByAccount[entry.AccountID] = append(groupIDsByAccount[entry.AccountID], entry.GroupID)
		}
	}

	for start := 0; start < len(ids); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		if _, err := txClient.AccountGroup.Delete().
			Where(dbaccountgroup.AccountIDIn(ids[start:end]...)).
			Exec(txCtx); err != nil {
			return err
		}
	}
	if _, err := txClient.ExecContext(txCtx, "DELETE FROM scheduled_test_plans WHERE account_id = ANY($1)", pq.Array(ids)); err != nil {
		return err
	}
	deletedAccountCount := 0
	for start := 0; start < len(ids); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		deleteQuery := txClient.Account.Delete().
			Where(dbaccount.IDIn(ids[start:end]...))
		if expectedOwnerUserID != nil {
			deleteQuery = deleteQuery.Where(dbaccount.OwnerUserIDEQ(*expectedOwnerUserID))
		}
		deleted, deleteErr := deleteQuery.Exec(txCtx)
		if deleteErr != nil {
			return deleteErr
		}
		deletedAccountCount += deleted
	}
	if deletedAccountCount != len(ids) {
		return service.ErrAccountNotFound
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	for _, accountID := range ids {
		r.deleteSchedulerAccountSnapshot(ctx, accountID)
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &accountID, nil, buildSchedulerGroupPayload(groupIDsByAccount[accountID])); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue account delete failed: account=%d err=%v", accountID, err)
		}
	}
	return nil
}

func validateLockedAccountDeletionOwnership(accounts []*dbent.Account, expectedOwnerUserID *int64) error {
	if expectedOwnerUserID == nil {
		return nil
	}
	if *expectedOwnerUserID <= 0 {
		return service.ErrAccountNotFound
	}
	for _, account := range accounts {
		if account == nil || account.OwnerUserID == nil || *account.OwnerUserID != *expectedOwnerUserID {
			return service.ErrAccountNotFound
		}
	}
	return nil
}

const accountDeletionBlockerSampleLimit = 10

type accountDeletionBlockers struct {
	roomListingIDs            []int64
	roomListingNames          []string
	roomStates                []string
	liveMembershipCount       int64
	liveMembershipIDs         []int64
	liveMembershipListingIDs  []int64
	liveMembershipStates      []string
	openBindingCount          int64
	openBindingIDs            []int64
	openBindingMembershipIDs  []int64
	openBindingListingIDs     []int64
	pendingBillingIntentCount int64
	pendingBillingIntentIDs   []int64
	pendingBillingStates      []string
}

func (b accountDeletionBlockers) hasAny() bool {
	return len(b.roomListingIDs) > 0 ||
		b.liveMembershipCount > 0 ||
		b.openBindingCount > 0 ||
		b.pendingBillingIntentCount > 0
}

func (b accountDeletionBlockers) conflictError(accountID int64) error {
	blockerTypes := make([]string, 0, 4)
	metadata := map[string]string{
		"account_id":                   strconv.FormatInt(accountID, 10),
		"room_account_count":           strconv.Itoa(len(b.roomListingIDs)),
		"live_membership_count":        strconv.FormatInt(b.liveMembershipCount, 10),
		"open_binding_count":           strconv.FormatInt(b.openBindingCount, 10),
		"pending_billing_intent_count": strconv.FormatInt(b.pendingBillingIntentCount, 10),
	}
	if len(b.roomListingIDs) > 0 {
		blockerTypes = append(blockerTypes, "room_account")
		metadata["room_listing_ids"] = joinAccountDeletionInt64s(b.roomListingIDs)
		metadata["room_account_states"] = strings.Join(b.roomStates, ",")
		if len(b.roomListingNames) > 0 {
			metadata["room_listing_names"] = strings.Join(b.roomListingNames, ",")
		}
	}
	if b.liveMembershipCount > 0 {
		blockerTypes = append(blockerTypes, "live_membership")
		metadata["live_membership_sample_ids"] = joinAccountDeletionInt64s(b.liveMembershipIDs)
		metadata["live_membership_listing_sample_ids"] = joinAccountDeletionInt64s(b.liveMembershipListingIDs)
		metadata["live_membership_sample_states"] = strings.Join(b.liveMembershipStates, ",")
		metadata["live_membership_sample_truncated"] = strconv.FormatBool(b.liveMembershipCount > int64(len(b.liveMembershipIDs)))
	}
	if b.openBindingCount > 0 {
		blockerTypes = append(blockerTypes, "open_binding")
		metadata["open_binding_sample_ids"] = joinAccountDeletionInt64s(b.openBindingIDs)
		metadata["open_binding_membership_sample_ids"] = joinAccountDeletionInt64s(b.openBindingMembershipIDs)
		metadata["open_binding_listing_sample_ids"] = joinAccountDeletionInt64s(b.openBindingListingIDs)
		metadata["open_binding_sample_truncated"] = strconv.FormatBool(b.openBindingCount > int64(len(b.openBindingIDs)))
	}
	if b.pendingBillingIntentCount > 0 {
		blockerTypes = append(blockerTypes, "pending_billing_intent")
		metadata["pending_billing_intent_sample_ids"] = joinAccountDeletionInt64s(b.pendingBillingIntentIDs)
		metadata["pending_billing_intent_sample_states"] = strings.Join(b.pendingBillingStates, ",")
		metadata["pending_billing_intent_sample_truncated"] = strconv.FormatBool(b.pendingBillingIntentCount > int64(len(b.pendingBillingIntentIDs)))
	}
	metadata["blocker_types"] = strings.Join(blockerTypes, ",")
	return service.ErrAccountDeletionBlocked.WithMetadata(metadata)
}

func loadAccountDeletionBlockers(ctx context.Context, exec sqlQueryExecutor, accountID int64) (accountDeletionBlockers, error) {
	var blockers accountDeletionBlockers
	if exec == nil {
		return blockers, errors.New("account deletion blocker executor is unavailable")
	}

	roomRows, err := exec.QueryContext(ctx, `
		SELECT room_account.listing_id, room_account.state, COALESCE(listing.room_name, '')
		FROM account_share_room_accounts room_account
		LEFT JOIN account_share_listings listing ON listing.id = room_account.listing_id
		WHERE room_account.account_id = $1
		ORDER BY room_account.listing_id
	`, accountID)
	if err != nil {
		return blockers, fmt.Errorf("query room account blockers: %w", err)
	}
	for roomRows.Next() {
		var listingID int64
		var state string
		var roomName string
		if err := roomRows.Scan(&listingID, &state, &roomName); err != nil {
			_ = roomRows.Close()
			return blockers, fmt.Errorf("scan room account blocker: %w", err)
		}
		blockers.roomListingIDs = append(blockers.roomListingIDs, listingID)
		blockers.roomStates = append(blockers.roomStates, state)
		// room_name 可能含逗号，替换为空格避免破坏逗号分隔的 metadata。
		blockers.roomListingNames = append(blockers.roomListingNames, strings.ReplaceAll(roomName, ",", " "))
	}
	if err := roomRows.Err(); err != nil {
		_ = roomRows.Close()
		return blockers, fmt.Errorf("iterate room account blockers: %w", err)
	}
	if err := roomRows.Close(); err != nil {
		return blockers, fmt.Errorf("close room account blockers: %w", err)
	}

	membershipRows, err := exec.QueryContext(ctx, `
		SELECT id, listing_id, status, COUNT(*) OVER ()
		FROM account_share_memberships
		WHERE account_id = $1
		  AND deleted_at IS NULL
		  AND status IN ('active', 'queued', 'ending')
		ORDER BY id
		LIMIT $2
	`, accountID, accountDeletionBlockerSampleLimit)
	if err != nil {
		return blockers, fmt.Errorf("query live membership blockers: %w", err)
	}
	for membershipRows.Next() {
		var membershipID, listingID, total int64
		var state string
		if err := membershipRows.Scan(&membershipID, &listingID, &state, &total); err != nil {
			_ = membershipRows.Close()
			return blockers, fmt.Errorf("scan live membership blocker: %w", err)
		}
		blockers.liveMembershipCount = total
		blockers.liveMembershipIDs = append(blockers.liveMembershipIDs, membershipID)
		blockers.liveMembershipListingIDs = append(blockers.liveMembershipListingIDs, listingID)
		blockers.liveMembershipStates = append(blockers.liveMembershipStates, state)
	}
	if err := membershipRows.Err(); err != nil {
		_ = membershipRows.Close()
		return blockers, fmt.Errorf("iterate live membership blockers: %w", err)
	}
	if err := membershipRows.Close(); err != nil {
		return blockers, fmt.Errorf("close live membership blockers: %w", err)
	}

	bindingTableExists, err := accountDeletionOptionalTableExists(ctx, exec, "public.account_share_membership_account_bindings")
	if err != nil {
		return blockers, err
	}
	if bindingTableExists {
		bindingRows, queryErr := exec.QueryContext(ctx, `
			SELECT id, membership_id, listing_id, COUNT(*) OVER ()
			FROM account_share_membership_account_bindings
			WHERE account_id_snapshot = $1
			  AND unbound_at IS NULL
			ORDER BY id
			LIMIT $2
		`, accountID, accountDeletionBlockerSampleLimit)
		if queryErr != nil {
			return blockers, fmt.Errorf("query open membership binding blockers: %w", queryErr)
		}
		for bindingRows.Next() {
			var bindingID, membershipID, listingID, total int64
			if err := bindingRows.Scan(&bindingID, &membershipID, &listingID, &total); err != nil {
				_ = bindingRows.Close()
				return blockers, fmt.Errorf("scan open membership binding blocker: %w", err)
			}
			blockers.openBindingCount = total
			blockers.openBindingIDs = append(blockers.openBindingIDs, bindingID)
			blockers.openBindingMembershipIDs = append(blockers.openBindingMembershipIDs, membershipID)
			blockers.openBindingListingIDs = append(blockers.openBindingListingIDs, listingID)
		}
		if err := bindingRows.Err(); err != nil {
			_ = bindingRows.Close()
			return blockers, fmt.Errorf("iterate open membership binding blockers: %w", err)
		}
		if err := bindingRows.Close(); err != nil {
			return blockers, fmt.Errorf("close open membership binding blockers: %w", err)
		}
	}

	billingIntentTableExists, err := accountDeletionOptionalTableExists(ctx, exec, "public.account_share_request_billing_intents")
	if err != nil {
		return blockers, err
	}
	if !billingIntentTableExists {
		return blockers, nil
	}

	billingRows, err := exec.QueryContext(ctx, `
		SELECT id, status, COUNT(*) OVER ()
		FROM account_share_request_billing_intents
		WHERE account_id_snapshot = $1
		  AND status NOT IN ('settled', 'cancelled')
		ORDER BY id
		LIMIT $2
	`, accountID, accountDeletionBlockerSampleLimit)
	if err != nil {
		return blockers, fmt.Errorf("query pending billing intent blockers: %w", err)
	}
	for billingRows.Next() {
		var intentID, total int64
		var state string
		if err := billingRows.Scan(&intentID, &state, &total); err != nil {
			_ = billingRows.Close()
			return blockers, fmt.Errorf("scan pending billing intent blocker: %w", err)
		}
		blockers.pendingBillingIntentCount = total
		blockers.pendingBillingIntentIDs = append(blockers.pendingBillingIntentIDs, intentID)
		blockers.pendingBillingStates = append(blockers.pendingBillingStates, state)
	}
	if err := billingRows.Err(); err != nil {
		_ = billingRows.Close()
		return blockers, fmt.Errorf("iterate pending billing intent blockers: %w", err)
	}
	if err := billingRows.Close(); err != nil {
		return blockers, fmt.Errorf("close pending billing intent blockers: %w", err)
	}
	return blockers, nil
}

func accountDeletionOptionalTableExists(ctx context.Context, exec sqlQueryExecutor, qualifiedTableName string) (bool, error) {
	if strings.TrimSpace(qualifiedTableName) == "" {
		return false, errors.New("optional account-share table name is required")
	}
	rows, err := exec.QueryContext(ctx, `
		SELECT to_regclass($1) IS NOT NULL
	`, qualifiedTableName)
	if err != nil {
		return false, fmt.Errorf("detect optional account-share table %q: %w", qualifiedTableName, err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("iterate optional account-share table %q detection: %w", qualifiedTableName, err)
		}
		return false, fmt.Errorf("optional account-share table %q detection returned no row", qualifiedTableName)
	}
	var exists bool
	if err := rows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan optional account-share table %q detection: %w", qualifiedTableName, err)
	}
	if rows.Next() {
		return false, fmt.Errorf("optional account-share table %q detection returned multiple rows", qualifiedTableName)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate optional account-share table %q detection: %w", qualifiedTableName, err)
	}
	return exists, nil
}

func normalizeAccountDeletionIDs(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(values))
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		ids = append(ids, value)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func joinAccountDeletionInt64s(values []int64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatInt(value, 10))
	}
	return strings.Join(parts, ",")
}

func accountDeletionGuardUnavailable(accountID int64, stage string, cause error) error {
	err := service.ErrAccountDeletionGuardUnavailable.WithMetadata(map[string]string{
		"account_id": strconv.FormatInt(accountID, 10),
		"stage":      stage,
	})
	if cause != nil {
		return err.WithCause(cause)
	}
	return err
}

func (r *accountRepository) DeleteStaleErrorAccounts(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT id
		FROM accounts
		WHERE status = $1
			AND error_since IS NOT NULL
			AND error_since <= $2
			AND deleted_at IS NULL
		ORDER BY error_since ASC, id ASC
		LIMIT $3
	`, service.StatusError, cutoff, limit)
	if err != nil {
		return 0, err
	}

	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var deleted int64
	for _, id := range ids {
		removed, err := r.deleteStaleErrorAccount(ctx, id, cutoff)
		if err != nil {
			return deleted, err
		}
		if removed {
			deleted++
		}
	}
	return deleted, nil
}

func (r *accountRepository) deleteStaleErrorAccount(ctx context.Context, id int64, cutoff time.Time) (bool, error) {
	groupIDs, err := r.loadAccountGroupIDs(ctx, id)
	if err != nil {
		return false, err
	}

	result, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
			AND status = $2
			AND error_since IS NOT NULL
			AND error_since <= $3
			AND deleted_at IS NULL
	`, id, service.StatusError, cutoff)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM scheduled_test_plans WHERE account_id = $1", id); err != nil {
		return false, err
	}

	r.deleteSchedulerAccountSnapshot(ctx, id)
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, buildSchedulerGroupPayload(groupIDs)); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue stale error account delete failed: account=%d err=%v", id, err)
	}
	return true, nil
}

func (r *accountRepository) List(ctx context.Context, params pagination.PaginationParams) ([]service.Account, *pagination.PaginationResult, error) {
	return r.ListWithFilters(ctx, params, "", "", "", "", "", 0, 0, "")
}

func (r *accountRepository) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search, ownerSearch string, groupID, proxyID int64, privacyMode string) ([]service.Account, *pagination.PaginationResult, error) {
	return r.listWithFilters(ctx, params, nil, platform, accountType, status, search, ownerSearch, groupID, proxyID, privacyMode)
}

func (r *accountRepository) ListOwnedWithFilters(ctx context.Context, ownerUserID int64, params pagination.PaginationParams, platform, accountType, status, search string, groupID, proxyID int64, privacyMode string) ([]service.Account, *pagination.PaginationResult, error) {
	if ownerUserID <= 0 {
		return nil, nil, service.ErrUserNotFound
	}
	return r.listWithFilters(ctx, params, &ownerUserID, platform, accountType, status, search, "", groupID, proxyID, privacyMode)
}

func (r *accountRepository) ListQuotaPoolAccounts(ctx context.Context, ownerUserID int64) ([]service.Account, error) {
	if ownerUserID <= 0 {
		return nil, service.ErrUserNotFound
	}
	if r == nil || r.sql == nil {
		return nil, fmt.Errorf("account repository sql executor is unavailable")
	}

	accounts, err := r.listQuotaPoolAccountRows(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return []service.Account{}, nil
	}
	if err := r.loadQuotaPoolAccountGroupRows(ctx, ownerUserID, accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *accountRepository) listQuotaPoolAccountRows(ctx context.Context, ownerUserID int64) ([]service.Account, error) {
	rows, err := r.sql.QueryContext(ctx, `
		WITH quota_pool_account_ids AS (
			SELECT id
			FROM accounts
			WHERE deleted_at IS NULL
				AND owner_user_id = $1
			UNION
			SELECT a.id
			FROM account_groups ag
			JOIN groups g ON g.id = ag.group_id
			JOIN accounts a ON a.id = ag.account_id
			WHERE a.deleted_at IS NULL
				AND g.deleted_at IS NULL
				AND g.is_exclusive = false
				AND g.owner_user_id IS NULL
				AND g.scope = 'public'
				AND COALESCE(g.subscription_type, '') IN ('', 'standard')
				AND (
					a.owner_user_id IS NULL
					OR (
						a.owner_user_id IS NOT NULL
						AND a.share_mode = 'public'
						AND a.share_status = 'approved'
					)
				)
		)
		SELECT
			a.id,
			a.name,
			a.platform,
			a.account_level,
			a.type,
			a.extra->>'quota_limit',
			a.extra->>'quota_used',
			a.extra->>'quota_daily_limit',
			a.extra->>'quota_daily_used',
			a.extra->>'quota_daily_start',
			a.extra->>'quota_daily_reset_mode',
			a.extra->>'quota_daily_reset_at',
			a.extra->>'quota_weekly_limit',
			a.extra->>'quota_weekly_used',
			a.extra->>'quota_weekly_start',
			a.extra->>'quota_weekly_reset_mode',
			a.extra->>'quota_weekly_reset_at',
			a.extra->>'codex_5h_used_percent',
			a.extra->>'codex_5h_reset_after_seconds',
			a.extra->>'codex_5h_reset_at',
			a.extra->>'codex_5h_limit_percent',
			a.extra->>'codex_7d_used_percent',
			a.extra->>'codex_7d_reset_after_seconds',
			a.extra->>'codex_7d_reset_at',
			a.extra->>'codex_7d_limit_percent',
			a.extra->>'codex_usage_updated_at',
			a.extra->>'privacy_mode',
			a.owner_user_id,
			a.share_mode,
			a.share_status,
			a.concurrency,
			a.status,
			a.expires_at,
			a.auto_pause_on_expired,
			a.schedulable,
			a.rate_limit_reset_at,
			a.overload_until,
			a.temp_unschedulable_until
		FROM accounts a
		JOIN quota_pool_account_ids q ON q.id = a.id
		ORDER BY a.id
	`, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	accounts := make([]service.Account, 0)
	for rows.Next() {
		var account service.Account
		var ownerUserID sql.NullInt64
		var quotaLimit, quotaUsed, quotaDailyLimit, quotaDailyUsed sql.NullString
		var quotaDailyStart, quotaDailyResetMode, quotaDailyResetAt sql.NullString
		var quotaWeeklyLimit, quotaWeeklyUsed, quotaWeeklyStart sql.NullString
		var quotaWeeklyResetMode, quotaWeeklyResetAt sql.NullString
		var codex5hUsedPercent, codex5hResetAfterSeconds, codex5hResetAt, codex5hLimitPercent sql.NullString
		var codex7dUsedPercent, codex7dResetAfterSeconds, codex7dResetAt, codex7dLimitPercent sql.NullString
		var codexUsageUpdatedAt, privacyMode sql.NullString
		if err := rows.Scan(
			&account.ID,
			&account.Name,
			&account.Platform,
			&account.AccountLevel,
			&account.Type,
			&quotaLimit,
			&quotaUsed,
			&quotaDailyLimit,
			&quotaDailyUsed,
			&quotaDailyStart,
			&quotaDailyResetMode,
			&quotaDailyResetAt,
			&quotaWeeklyLimit,
			&quotaWeeklyUsed,
			&quotaWeeklyStart,
			&quotaWeeklyResetMode,
			&quotaWeeklyResetAt,
			&codex5hUsedPercent,
			&codex5hResetAfterSeconds,
			&codex5hResetAt,
			&codex5hLimitPercent,
			&codex7dUsedPercent,
			&codex7dResetAfterSeconds,
			&codex7dResetAt,
			&codex7dLimitPercent,
			&codexUsageUpdatedAt,
			&privacyMode,
			&ownerUserID,
			&account.ShareMode,
			&account.ShareStatus,
			&account.Concurrency,
			&account.Status,
			&account.ExpiresAt,
			&account.AutoPauseOnExpired,
			&account.Schedulable,
			&account.RateLimitResetAt,
			&account.OverloadUntil,
			&account.TempUnschedulableUntil,
		); err != nil {
			return nil, err
		}
		if ownerUserID.Valid {
			account.OwnerUserID = &ownerUserID.Int64
		}
		account.AccountLevel = service.NormalizeAccountLevel(account.AccountLevel)
		account.ShareMode = service.NormalizeAccountShareMode(account.ShareMode)
		account.ShareStatus = service.NormalizeAccountShareStatus(account.ShareStatus)
		account.Extra = map[string]any{}
		setNullStringExtra(account.Extra, "quota_limit", quotaLimit)
		setNullStringExtra(account.Extra, "quota_used", quotaUsed)
		setNullStringExtra(account.Extra, "quota_daily_limit", quotaDailyLimit)
		setNullStringExtra(account.Extra, "quota_daily_used", quotaDailyUsed)
		setNullStringExtra(account.Extra, "quota_daily_start", quotaDailyStart)
		setNullStringExtra(account.Extra, "quota_daily_reset_mode", quotaDailyResetMode)
		setNullStringExtra(account.Extra, "quota_daily_reset_at", quotaDailyResetAt)
		setNullStringExtra(account.Extra, "quota_weekly_limit", quotaWeeklyLimit)
		setNullStringExtra(account.Extra, "quota_weekly_used", quotaWeeklyUsed)
		setNullStringExtra(account.Extra, "quota_weekly_start", quotaWeeklyStart)
		setNullStringExtra(account.Extra, "quota_weekly_reset_mode", quotaWeeklyResetMode)
		setNullStringExtra(account.Extra, "quota_weekly_reset_at", quotaWeeklyResetAt)
		setNullStringExtra(account.Extra, "codex_5h_used_percent", codex5hUsedPercent)
		setNullStringExtra(account.Extra, "codex_5h_reset_after_seconds", codex5hResetAfterSeconds)
		setNullStringExtra(account.Extra, "codex_5h_reset_at", codex5hResetAt)
		setNullStringExtra(account.Extra, "codex_5h_limit_percent", codex5hLimitPercent)
		setNullStringExtra(account.Extra, "codex_7d_used_percent", codex7dUsedPercent)
		setNullStringExtra(account.Extra, "codex_7d_reset_after_seconds", codex7dResetAfterSeconds)
		setNullStringExtra(account.Extra, "codex_7d_reset_at", codex7dResetAt)
		setNullStringExtra(account.Extra, "codex_7d_limit_percent", codex7dLimitPercent)
		setNullStringExtra(account.Extra, "codex_usage_updated_at", codexUsageUpdatedAt)
		setNullStringExtra(account.Extra, "privacy_mode", privacyMode)
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *accountRepository) loadQuotaPoolAccountGroupRows(ctx context.Context, ownerUserID int64, accounts []service.Account) error {
	byID := make(map[int64]*service.Account, len(accounts))
	accountIDs := make([]int64, 0, len(accounts))
	for i := range accounts {
		byID[accounts[i].ID] = &accounts[i]
		accountIDs = append(accountIDs, accounts[i].ID)
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT
			ag.account_id,
			ag.group_id,
			ag.priority,
			ag.created_at,
			g.name,
			g.platform,
			g.rate_multiplier,
			g.new_user_rate_enabled,
			g.new_user_rate_multiplier,
			g.new_user_rate_window_seconds,
			g.new_user_rate_quota_usd,
			g.is_exclusive,
			g.status,
			g.owner_user_id,
			g.scope,
			g.subscription_type,
			g.required_account_level,
			g.require_oauth_only,
			g.require_privacy_set
		FROM account_groups ag
		JOIN groups g ON g.id = ag.group_id
		WHERE ag.account_id = ANY($1)
			AND g.deleted_at IS NULL
		ORDER BY ag.account_id, ag.priority
	`, pq.Array(accountIDs))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var accountID int64
		var group service.Group
		var accountGroup service.AccountGroup
		var groupOwnerUserID sql.NullInt64
		if err := rows.Scan(
			&accountID,
			&group.ID,
			&accountGroup.Priority,
			&accountGroup.CreatedAt,
			&group.Name,
			&group.Platform,
			&group.RateMultiplier,
			&group.NewUserRateEnabled,
			&group.NewUserRateMultiplier,
			&group.NewUserRateWindowSeconds,
			&group.NewUserRateQuotaUSD,
			&group.IsExclusive,
			&group.Status,
			&groupOwnerUserID,
			&group.Scope,
			&group.SubscriptionType,
			&group.RequiredAccountLevel,
			&group.RequireOAuthOnly,
			&group.RequirePrivacySet,
		); err != nil {
			return err
		}
		if groupOwnerUserID.Valid {
			group.OwnerUserID = &groupOwnerUserID.Int64
		}
		group.Hydrated = true
		group.Scope = service.NormalizeGroupScope(group.Scope)
		group.RequiredAccountLevel = service.NormalizeRequiredAccountLevel(group.RequiredAccountLevel)

		account, ok := byID[accountID]
		if !ok {
			continue
		}
		accountGroup.AccountID = accountID
		accountGroup.GroupID = group.ID
		accountGroup.Group = &group
		account.AccountGroups = append(account.AccountGroups, accountGroup)
		account.GroupIDs = append(account.GroupIDs, group.ID)
		account.Groups = append(account.Groups, &group)
	}
	return rows.Err()
}

func setNullStringExtra(extra map[string]any, key string, value sql.NullString) {
	if !value.Valid || value.String == "" {
		return
	}
	extra[key] = value.String
}

const (
	accountRepoNumericTextPattern = `^\s*[+-]?(\d+(\.\d+)?|\.\d+)\s*$`
	accountRepoRFC3339TextPattern = `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`
)

func accountTempUnschedulableInactivePredicate() dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		col := s.C("temp_unschedulable_until")
		s.Where(entsql.Or(
			entsql.IsNull(col),
			entsql.LTE(col, entsql.Expr("NOW()")),
		))
	})
}

func accountCodexQuotaProtectedPredicate() dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		extraCol := s.C(dbaccount.FieldExtra)
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("(")
			writeCodexQuotaWindowProtectedCondition(b, extraCol, "codex_5h_used_percent", "codex_5h_reset_at", "codex_5h_limit_percent")
			b.WriteString(" OR ")
			writeCodexQuotaWindowProtectedCondition(b, extraCol, "codex_7d_used_percent", "codex_7d_reset_at", "codex_7d_limit_percent")
			b.WriteString(")")
		}))
	})
}

func writeCodexQuotaWindowProtectedCondition(b *entsql.Builder, extraCol, usedKey, resetAtKey, limitKey string) {
	b.WriteString("(")
	writeNumericExtraOrDefault(b, extraCol, usedKey, "0")
	b.WriteString(" >= ")
	writeCodexQuotaLimitExpr(b, extraCol, limitKey)
	b.WriteString(" AND ")
	writeTimestampExtraExpr(b, extraCol, resetAtKey)
	b.WriteString(" > NOW())")
}

func writeNumericExtraOrDefault(b *entsql.Builder, extraCol, key, defaultValue string) {
	b.WriteString("(CASE WHEN ")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(" ~ ").Arg(accountRepoNumericTextPattern).WriteString(" THEN (")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(")::numeric ELSE ").WriteString(defaultValue).WriteString(" END)")
}

func writeCodexQuotaLimitExpr(b *entsql.Builder, extraCol, key string) {
	b.WriteString("(CASE WHEN ")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(" ~ ").Arg(accountRepoNumericTextPattern).WriteString(" THEN CASE WHEN (")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(")::numeric BETWEEN 1 AND 100 THEN (")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(")::numeric ELSE 100 END ELSE 100 END)")
}

func writeTimestampExtraExpr(b *entsql.Builder, extraCol, key string) {
	b.WriteString("(CASE WHEN ")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(" ~ ").Arg(accountRepoRFC3339TextPattern).WriteString(" THEN (")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(")::timestamptz ELSE NULL END)")
}

func writeExtraTextExpr(b *entsql.Builder, extraCol, key string) {
	b.Ident(extraCol).WriteString(" ->> ").Arg(key)
}

func (r *accountRepository) listWithFilters(ctx context.Context, params pagination.PaginationParams, ownerUserID *int64, platform, accountType, status, search, ownerSearch string, groupID, proxyID int64, privacyMode string) ([]service.Account, *pagination.PaginationResult, error) {
	q := r.client.Account.Query()

	if ownerUserID != nil {
		q = q.Where(dbaccount.OwnerUserIDEQ(*ownerUserID))
	}
	if platform != "" {
		q = q.Where(dbaccount.PlatformEQ(platform))
	}
	if accountType != "" {
		q = q.Where(dbaccount.TypeEQ(accountType))
	}
	if status != "" {
		switch status {
		case service.StatusActive:
			q = q.Where(
				dbaccount.StatusEQ(status),
				dbaccount.SchedulableEQ(true),
				dbaccount.Or(
					dbaccount.RateLimitResetAtIsNil(),
					dbaccount.RateLimitResetAtLTE(time.Now()),
				),
				accountTempUnschedulableInactivePredicate(),
				dbaccount.Not(accountCodexQuotaProtectedPredicate()),
			)
		case service.AccountListStatusRateLimited:
			q = q.Where(
				dbaccount.StatusEQ(service.StatusActive),
				dbaccount.RateLimitResetAtGT(time.Now()),
				accountTempUnschedulableInactivePredicate(),
			)
		case service.AccountListStatusTempUnschedulable:
			q = q.Where(
				dbaccount.StatusEQ(service.StatusActive),
				dbaccount.Not(accountCodexQuotaProtectedPredicate()),
				dbpredicate.Account(func(s *entsql.Selector) {
					col := s.C("temp_unschedulable_until")
					s.Where(entsql.And(
						entsql.Not(entsql.IsNull(col)),
						entsql.GT(col, entsql.Expr("NOW()")),
					))
				}),
			)
		case service.AccountListStatusCodexQuotaProtected:
			q = q.Where(
				dbaccount.StatusEQ(service.StatusActive),
				dbaccount.PlatformEQ(service.PlatformOpenAI),
				dbaccount.TypeEQ(service.AccountTypeOAuth),
				dbaccount.Or(
					dbaccount.RateLimitResetAtIsNil(),
					dbaccount.RateLimitResetAtLTE(time.Now()),
				),
				accountCodexQuotaProtectedPredicate(),
			)
		case service.AccountListStatusUnschedulable:
			q = q.Where(
				dbaccount.StatusEQ(service.StatusActive),
				dbaccount.SchedulableEQ(false),
				dbaccount.Or(
					dbaccount.RateLimitResetAtIsNil(),
					dbaccount.RateLimitResetAtLTE(time.Now()),
				),
				accountTempUnschedulableInactivePredicate(),
				dbaccount.Not(accountCodexQuotaProtectedPredicate()),
			)
		default:
			q = q.Where(dbaccount.StatusEQ(status))
		}
	}
	if search != "" {
		q = q.Where(dbaccount.NameContainsFold(search))
	}
	ownerSearch = strings.TrimSpace(ownerSearch)
	if ownerSearch != "" {
		ownerMatches := []dbpredicate.User{
			dbuser.EmailContainsFold(ownerSearch),
			dbuser.UsernameContainsFold(ownerSearch),
		}
		if ownerID, err := strconv.ParseInt(ownerSearch, 10, 64); err == nil && ownerID > 0 {
			ownerMatches = append(ownerMatches, dbuser.IDEQ(ownerID))
		}
		q = q.Where(dbaccount.HasOwnerWith(
			dbuser.DeletedAtIsNil(),
			dbuser.Or(ownerMatches...),
		))
	}
	if groupID == service.AccountListGroupUngrouped {
		q = q.Where(accountHasNoNonPrivateGroups())
	} else if groupID > 0 {
		q = q.Where(dbaccount.HasAccountGroupsWith(dbaccountgroup.GroupIDEQ(groupID)))
	}
	if proxyID == service.AccountListProxyUnassigned {
		q = q.Where(dbaccount.ProxyIDIsNil())
	} else if proxyID > 0 {
		q = q.Where(dbaccount.ProxyIDEQ(proxyID))
	}
	if privacyMode != "" {
		q = q.Where(dbpredicate.Account(func(s *entsql.Selector) {
			path := sqljson.Path("privacy_mode")
			switch privacyMode {
			case service.AccountPrivacyModeUnsetFilter:
				s.Where(entsql.Or(
					entsql.Not(sqljson.HasKey(dbaccount.FieldExtra, path)),
					sqljson.ValueEQ(dbaccount.FieldExtra, "", path),
				))
			default:
				s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, privacyMode, path))
			}
		}))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	accountsQuery := q.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range accountListOrder(params) {
		accountsQuery = accountsQuery.Order(order)
	}

	accounts, err := accountsQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	outAccounts, err := r.accountsToService(ctx, accounts)
	if err != nil {
		return nil, nil, err
	}
	return outAccounts, paginationResultFromTotal(int64(total), params), nil
}

func accountListOrder(params pagination.PaginationParams) []func(*entsql.Selector) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderAsc)

	field := dbaccount.FieldName
	defaultOrder := true
	switch sortBy {
	case "", "name":
		field = dbaccount.FieldName
	case "id":
		field = dbaccount.FieldID
		defaultOrder = false
	case "status":
		field = dbaccount.FieldStatus
		defaultOrder = false
	case "schedulable":
		field = dbaccount.FieldSchedulable
		defaultOrder = false
	case "priority":
		field = dbaccount.FieldPriority
		defaultOrder = false
	case "rate_multiplier":
		field = dbaccount.FieldRateMultiplier
		defaultOrder = false
	case "last_used_at":
		field = dbaccount.FieldLastUsedAt
		defaultOrder = false
	case "expires_at":
		field = dbaccount.FieldExpiresAt
		defaultOrder = false
	case "created_at":
		field = dbaccount.FieldCreatedAt
		defaultOrder = false
	}

	if sortOrder == pagination.SortOrderDesc {
		return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(dbaccount.FieldID)}
	}
	if defaultOrder {
		return []func(*entsql.Selector){dbent.Asc(dbaccount.FieldName), dbent.Asc(dbaccount.FieldID)}
	}
	return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(dbaccount.FieldID)}
}

func accountHasNoNonPrivateGroups() dbpredicate.Account {
	return dbaccount.Not(dbaccount.HasAccountGroupsWith(
		dbaccountgroup.HasGroupWith(
			dbgroup.DeletedAtIsNil(),
			dbgroup.ScopeNEQ(service.GroupScopeUserPrivate),
		),
	))
}

func (r *accountRepository) ListByGroup(ctx context.Context, groupID int64) ([]service.Account, error) {
	accounts, err := r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status: service.StatusActive,
	})
	if err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *accountRepository) ListActive(ctx context.Context) ([]service.Account, error) {
	accounts, err := r.client.Account.Query().
		Where(dbaccount.StatusEQ(service.StatusActive)).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListOAuthRefreshCandidates(ctx context.Context, refreshWindow time.Duration) ([]service.Account, error) {
	if r.sql == nil {
		return nil, errors.New("account repository SQL executor not configured")
	}
	query, args := buildOAuthRefreshCandidatesQuery(refreshWindow)
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	ids := make([]int64, 0, 128)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []service.Account{}, nil
	}

	accounts, err := r.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]service.Account, 0, len(accounts))
	for _, account := range accounts {
		if account != nil {
			out = append(out, *account)
		}
	}
	return out, nil
}

func (r *accountRepository) ListGrokOAuthReconcileCandidatePage(
	ctx context.Context,
	afterID int64,
	limit int,
) (*service.GrokOAuthReconcileCandidatePage, error) {
	if r.sql == nil {
		return nil, errors.New("account repository SQL executor not configured")
	}
	if afterID < 0 || limit <= 0 {
		return nil, errors.New("invalid Grok OAuth reconciliation cursor page")
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT id
		FROM accounts
		WHERE deleted_at IS NULL
			AND status = $1
			AND platform = $2
			AND type = $3
			AND id > $4
		ORDER BY id ASC
		LIMIT $5
	`, service.StatusActive, service.PlatformGrok, service.AccountTypeOAuth, afterID, limit+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	ids := make([]int64, 0, limit+1)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	page := &service.GrokOAuthReconcileCandidatePage{}
	if len(ids) == 0 {
		page.Accounts = []service.Account{}
		return page, nil
	}
	if len(ids) > limit {
		page.HasMore = true
		ids = ids[:limit]
		page.NextAfterID = ids[len(ids)-1]
	}

	accounts, err := r.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	page.Accounts = make([]service.Account, 0, len(accounts))
	for _, account := range accounts {
		if account != nil {
			page.Accounts = append(page.Accounts, *account)
		}
	}
	sort.Slice(page.Accounts, func(i, j int) bool {
		return page.Accounts[i].ID < page.Accounts[j].ID
	})
	return page, nil
}

func buildOAuthRefreshCandidatesQuery(refreshWindow time.Duration) (string, []any) {
	refreshWindowSeconds := int64(refreshWindow / time.Second)
	if refreshWindowSeconds < 0 {
		refreshWindowSeconds = 0
	}
	return `
		WITH candidates AS (
			SELECT
				id,
				priority,
				platform,
				rate_limit_reset_at,
				NULLIF(btrim(credentials->>'expires_at'), '') AS expires_at_raw
			FROM accounts
			WHERE deleted_at IS NULL
				AND status = 'active'
				AND type IN ('oauth', 'setup-token')
				AND platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok')
				AND credentials ? 'refresh_token'
				AND btrim(credentials->>'refresh_token') <> ''
				AND (
					temp_unschedulable_until > NOW()
					AND temp_unschedulable_reason LIKE 'token refresh retry exhausted:%'
				) IS NOT TRUE
		),
		parsed AS (
			SELECT
				id,
				priority,
				platform,
				rate_limit_reset_at,
				expires_at_raw,
				CASE
					WHEN expires_at_raw ~ '^[0-9]+$' THEN to_timestamp(expires_at_raw::double precision)
					ELSE NULL
				END AS credential_expires_at,
				(expires_at_raw IS NOT NULL AND expires_at_raw !~ '^[0-9]+$') AS needs_go_time_parse
			FROM candidates
		)
		SELECT id
		FROM parsed
		WHERE
			needs_go_time_parse
			OR (
				platform = 'openai'
				AND (
					(credential_expires_at IS NOT NULL AND credential_expires_at <= NOW() + ($1::bigint * INTERVAL '1 second'))
					OR (credential_expires_at IS NULL AND rate_limit_reset_at > NOW())
				)
			)
			OR (
				platform IN ('anthropic', 'gemini')
				AND credential_expires_at IS NOT NULL
				AND credential_expires_at <= NOW() + ($1::bigint * INTERVAL '1 second')
			)
			OR (
				platform = 'antigravity'
				AND credential_expires_at IS NOT NULL
				AND credential_expires_at <= NOW() + INTERVAL '15 minutes'
			)
			OR (
				platform = 'grok'
				AND (
					credential_expires_at IS NULL
					OR credential_expires_at <= NOW() + (GREATEST($1::bigint, 3600) * INTERVAL '1 second')
				)
			)
		ORDER BY priority ASC, id ASC
	`, []any{refreshWindowSeconds}
}

func (r *accountRepository) ListByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformEQ(platform),
			dbaccount.StatusEQ(service.StatusActive),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) UpdateLastUsed(ctx context.Context, id int64) error {
	now := time.Now()
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetLastUsedAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"last_used": map[string]int64{
			strconv.FormatInt(id, 10): now.Unix(),
		},
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountLastUsed, &id, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue last used failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) BatchUpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	if len(updates) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(updates))
	args := make([]any, 0, len(updates)*2+1)
	caseSQL := "UPDATE accounts SET last_used_at = CASE id"

	idx := 1
	for id, ts := range updates {
		caseSQL += " WHEN $" + itoa(idx) + " THEN $" + itoa(idx+1) + "::timestamptz"
		args = append(args, id, ts)
		ids = append(ids, id)
		idx += 2
	}

	caseSQL += " END, updated_at = NOW() WHERE id = ANY($" + itoa(idx) + ") AND deleted_at IS NULL"
	args = append(args, pq.Array(ids))

	_, err := r.sql.ExecContext(ctx, caseSQL, args...)
	if err != nil {
		return err
	}
	lastUsedPayload := make(map[string]int64, len(updates))
	for id, ts := range updates {
		lastUsedPayload[strconv.FormatInt(id, 10)] = ts.Unix()
	}
	payload := map[string]any{"last_used": lastUsedPayload}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountLastUsed, nil, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue batch last used failed: err=%v", err)
	}
	return nil
}

func (r *accountRepository) SetError(ctx context.Context, id int64, errorMsg string) error {
	_, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET status = $2,
			error_message = $3,
			error_since = COALESCE(error_since, NOW()),
			updated_at = NOW()
		WHERE id = $1
			AND deleted_at IS NULL
	`, id, service.StatusError, errorMsg)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue set error failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) SetGrokCredentialErrorIfMatch(
	ctx context.Context,
	id int64,
	snapshot service.GrokCredentialMutationSnapshot,
	errorMsg string,
) (bool, error) {
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
			UPDATE accounts AS a
			SET status = $1,
				error_message = $2,
				error_since = COALESCE(a.error_since, NOW()),
				schedulable = FALSE,
				temp_unschedulable_until = NULL,
				temp_unschedulable_reason = NULL,
				updated_at = NOW()
			WHERE a.id = $3
				AND a.deleted_at IS NULL
				AND a.status = $4
				AND a.platform = $5
				AND a.type IN ($6, $7)
				AND a.schedulable IS TRUE
				AND a.credentials = $8::jsonb
				AND a.proxy_id IS NOT DISTINCT FROM $9
			RETURNING a.id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $10, updated.id, NULL, NULL FROM updated
	`, service.StatusError, errorMsg, id, service.StatusActive, service.PlatformGrok,
		service.AccountTypeOAuth, service.AccountTypeAPIKey, snapshot.CredentialsJSON, snapshot.ProxyID,
		service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}

func (r *accountRepository) UpdateGrokOAuthCredentialsIfUnchanged(
	ctx context.Context,
	id int64,
	expectedCredentials map[string]any,
	expectedProxyID *int64,
	credentials map[string]any,
) (bool, error) {
	expectedJSON, err := json.Marshal(expectedCredentials)
	if err != nil {
		return false, err
	}
	credentialsJSON, err := json.Marshal(credentials)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
			UPDATE accounts AS a
			SET credentials = $1::jsonb, updated_at = NOW()
			WHERE a.id = $2
				AND a.deleted_at IS NULL
				AND a.platform = $3
				AND a.type = $4
				AND a.credentials = $5::jsonb
				AND a.proxy_id IS NOT DISTINCT FROM $6
			RETURNING a.id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $7, updated.id, NULL, NULL FROM updated
	`, string(credentialsJSON), id, service.PlatformGrok, service.AccountTypeOAuth,
		string(expectedJSON), expectedProxyID, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}

// syncSchedulerAccountSnapshot 在账号状态变更时主动同步快照到调度器缓存。
// 当账号被设置为错误、禁用、不可调度或临时不可调度时调用，
// 确保调度器和粘性会话逻辑能及时感知账号的最新状态，避免继续使用不可用账号。
//
// syncSchedulerAccountSnapshot proactively syncs account snapshot to scheduler cache
// when account status changes. Called when account is set to error, disabled,
// unschedulable, or temporarily unschedulable, ensuring scheduler and sticky session
// logic can promptly detect the latest account state and avoid using unavailable accounts.
func (r *accountRepository) syncSchedulerAccountSnapshot(ctx context.Context, accountID int64) {
	if r == nil || r.schedulerCache == nil || accountID <= 0 {
		return
	}
	account, err := r.GetByID(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] sync account snapshot read failed: id=%d err=%v", accountID, err)
		return
	}
	if err := r.schedulerCache.SetAccount(ctx, account); err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] sync account snapshot write failed: id=%d err=%v", accountID, err)
	}
}

func (r *accountRepository) syncSchedulerAccountSnapshotDetached(ctx context.Context, accountID int64) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	propagationCtx, cancel := context.WithTimeout(base, 2*time.Second)
	defer cancel()
	r.syncSchedulerAccountSnapshot(propagationCtx, accountID)
}

func (r *accountRepository) deleteSchedulerAccountSnapshot(ctx context.Context, accountID int64) {
	if r == nil || r.schedulerCache == nil || accountID <= 0 {
		return
	}
	if err := r.schedulerCache.DeleteAccount(ctx, accountID); err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] delete account snapshot failed: id=%d err=%v", accountID, err)
	}
}

func (r *accountRepository) syncSchedulerAccountSnapshots(ctx context.Context, accountIDs []int64) {
	if r == nil || r.schedulerCache == nil || len(accountIDs) == 0 {
		return
	}

	uniqueIDs := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return
	}

	accounts, err := r.GetByIDs(ctx, uniqueIDs)
	if err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] batch sync account snapshot read failed: count=%d err=%v", len(uniqueIDs), err)
		return
	}

	for _, account := range accounts {
		if account == nil {
			continue
		}
		if err := r.schedulerCache.SetAccount(ctx, account); err != nil {
			logger.LegacyPrintf("repository.account", "[Scheduler] batch sync account snapshot write failed: id=%d err=%v", account.ID, err)
		}
	}
}

func (r *accountRepository) ClearError(ctx context.Context, id int64) error {
	_, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET status = $2,
			error_message = '',
			error_since = NULL,
			updated_at = NOW()
		WHERE id = $1
			AND deleted_at IS NULL
	`, id, service.StatusActive)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear error failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) syncAccountErrorSince(ctx context.Context, id int64, status string) error {
	exec := txAwareSQLExecutor(ctx, r.sql, r.client)
	if exec == nil {
		return fmt.Errorf("sql executor is unavailable")
	}
	if status == service.StatusError {
		_, err := exec.ExecContext(ctx, `
			UPDATE accounts
			SET error_since = COALESCE(error_since, NOW())
			WHERE id = $1
				AND status = $2
				AND deleted_at IS NULL
		`, id, service.StatusError)
		return err
	}

	_, err := exec.ExecContext(ctx, `
		UPDATE accounts
		SET error_since = NULL
		WHERE id = $1
			AND error_since IS NOT NULL
			AND deleted_at IS NULL
	`, id)
	return err
}

func (r *accountRepository) AddToGroup(ctx context.Context, accountID, groupID int64, priority int) error {
	_, err := r.client.AccountGroup.Create().
		SetAccountID(accountID).
		SetGroupID(groupID).
		SetPriority(priority).
		Save(ctx)
	if err != nil {
		return err
	}
	payload := buildSchedulerGroupPayload([]int64{groupID})
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue add to group failed: account=%d group=%d err=%v", accountID, groupID, err)
	}
	return nil
}

func (r *accountRepository) RemoveFromGroup(ctx context.Context, accountID, groupID int64) error {
	_, err := r.client.AccountGroup.Delete().
		Where(
			dbaccountgroup.AccountIDEQ(accountID),
			dbaccountgroup.GroupIDEQ(groupID),
		).
		Exec(ctx)
	if err != nil {
		return err
	}
	payload := buildSchedulerGroupPayload([]int64{groupID})
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue remove from group failed: account=%d group=%d err=%v", accountID, groupID, err)
	}
	return nil
}

func (r *accountRepository) GetGroups(ctx context.Context, accountID int64) ([]service.Group, error) {
	groups, err := r.client.Group.Query().
		Where(
			dbgroup.HasAccountsWith(dbaccount.IDEQ(accountID)),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	outGroups := make([]service.Group, 0, len(groups))
	for i := range groups {
		outGroups = append(outGroups, *groupEntityToService(groups[i]))
	}
	return outGroups, nil
}

func (r *accountRepository) BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error {
	existingGroupIDs, err := r.loadAccountGroupIDs(ctx, accountID)
	if err != nil {
		return err
	}
	var tx *dbent.Tx
	txCtx := ctx
	txClient := clientFromContext(ctx, r.client)
	if dbent.TxFromContext(ctx) == nil {
		tx, err = r.client.Tx(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		txClient = tx.Client()
		txCtx = dbent.NewTxContext(ctx, tx)
	}

	if _, err := txClient.AccountGroup.Delete().Where(dbaccountgroup.AccountIDEQ(accountID)).Exec(txCtx); err != nil {
		return err
	}

	if len(groupIDs) > 0 {
		builders := make([]*dbent.AccountGroupCreate, 0, len(groupIDs))
		for i, groupID := range groupIDs {
			builders = append(builders, txClient.AccountGroup.Create().
				SetAccountID(accountID).
				SetGroupID(groupID).
				SetPriority(i+1),
			)
		}

		if _, err := txClient.AccountGroup.CreateBulk(builders...).Save(txCtx); err != nil {
			return err
		}
	}

	payload := buildSchedulerGroupPayload(mergeGroupIDs(existingGroupIDs, groupIDs))
	if err := enqueueSchedulerOutbox(txCtx, txAwareSQLExecutor(txCtx, r.sql, r.client), service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		return err
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (r *accountRepository) ListSchedulable(ctx context.Context) ([]service.Account, error) {
	accounts, err := r.schedulableAccountsQuery(time.Now()).All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableAccountLoads(ctx context.Context) ([]service.AccountWithConcurrency, error) {
	accounts, err := r.schedulableAccountsQuery(time.Now()).
		Select(
			dbaccount.FieldID,
			dbaccount.FieldConcurrency,
			dbaccount.FieldLoadFactor,
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	loads := make([]service.AccountWithConcurrency, 0, len(accounts))
	for _, account := range accounts {
		projection := service.Account{
			ID:          account.ID,
			Concurrency: account.Concurrency,
			LoadFactor:  account.LoadFactor,
		}
		loads = append(loads, service.AccountWithConcurrency{
			ID:             account.ID,
			MaxConcurrency: projection.EffectiveLoadFactor(),
		})
	}
	return loads, nil
}

func (r *accountRepository) schedulableAccountsQuery(now time.Time) *dbent.AccountQuery {
	return r.client.Account.Query().
		Where(
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			notDrainingExternalPlacementPredicate(),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbaccount.FieldPriority))
}

func (r *accountRepository) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]service.Account, error) {
	return r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status:      service.StatusActive,
		schedulable: true,
	})
}

func (r *accountRepository) ListSchedulableByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformEQ(platform),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			notDrainingExternalPlacementPredicate(),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]service.Account, error) {
	// 单平台查询复用多平台逻辑，保持过滤条件与排序策略一致。
	return r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status:      service.StatusActive,
		schedulable: true,
		platforms:   []string{platform},
	})
}

func (r *accountRepository) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	// 仅返回可调度的活跃账号，并过滤处于过载/限流窗口的账号。
	// 代理与分组信息统一在 accountsToService 中批量加载，避免 N+1 查询。
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformIn(platforms...),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			notDrainingExternalPlacementPredicate(),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformEQ(platform),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			notDrainingExternalPlacementPredicate(),
			dbaccount.Not(dbaccount.HasAccountGroups()),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformIn(platforms...),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			notDrainingExternalPlacementPredicate(),
			dbaccount.Not(dbaccount.HasAccountGroups()),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	// 复用按分组查询逻辑，保证分组优先级 + 账号优先级的排序与筛选一致。
	return r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status:      service.StatusActive,
		schedulable: true,
		platforms:   platforms,
	})
}

func (r *accountRepository) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	now := time.Now()
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetRateLimitedAt(now).
		SetRateLimitResetAt(resetAt).
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue rate limit failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

// SetRateLimitedIfLater atomically extends an account-level rate limit. Grok
// requests may finish concurrently, so an older response must not overwrite a
// later reset boundary observed by another request or instance.
func (r *accountRepository) SetRateLimitedIfLater(ctx context.Context, id int64, resetAt time.Time) error {
	now := time.Now()
	updated, err := r.client.Account.Update().
		Where(
			dbaccount.IDEQ(id),
			dbaccount.Or(
				dbaccount.RateLimitResetAtIsNil(),
				dbaccount.RateLimitResetAtLT(resetAt),
			),
		).
		SetRateLimitedAt(now).
		SetRateLimitResetAt(resetAt).
		Save(ctx)
	if err != nil {
		return err
	}
	if updated == 0 {
		// This instance may not have observed the later value written elsewhere.
		// Refresh its local scheduler snapshot even though no outbox event is needed.
		r.syncSchedulerAccountSnapshot(ctx, id)
		return nil
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue extended rate limit failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

// ClearRateLimitIfObserved clears exactly the Grok rate-limit generation seen
// by a successful request. Matching both timestamps prevents a stale success
// from erasing a later clear/re-arm generation with an equal or shorter reset.
func (r *accountRepository) ClearRateLimitIfObserved(ctx context.Context, id int64, observedLimitedAt, observedResetAt time.Time) (bool, error) {
	updated, err := r.client.Account.Update().
		Where(
			dbaccount.IDEQ(id),
			dbaccount.PlatformEQ(service.PlatformGrok),
			dbaccount.TypeEQ(service.AccountTypeOAuth),
			dbaccount.RateLimitedAtEQ(observedLimitedAt),
			dbaccount.RateLimitResetAtEQ(observedResetAt),
		).
		ClearRateLimitedAt().
		ClearRateLimitResetAt().
		Save(ctx)
	if err != nil {
		return false, err
	}
	if updated == 0 {
		r.syncSchedulerAccountSnapshot(ctx, id)
		return false, nil
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue observed rate-limit clear failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return true, nil
}

func (r *accountRepository) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time) error {
	if scope == "" {
		return nil
	}
	now := time.Now().UTC()
	payload := map[string]string{
		"rate_limited_at":     now.Format(time.RFC3339),
		"rate_limit_reset_at": resetAt.UTC().Format(time.RFC3339),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		`UPDATE accounts SET 
			extra = jsonb_set(
				jsonb_set(COALESCE(extra, '{}'::jsonb), '{model_rate_limits}'::text[], COALESCE(extra->'model_rate_limits', '{}'::jsonb), true),
				ARRAY['model_rate_limits', $1]::text[],
				$2::jsonb,
				true
			),
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL`,
		scope,
		raw,
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue model rate limit failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) SetOverloaded(ctx context.Context, id int64, until time.Time) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetOverloadUntil(until).
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue overload failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	result, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET temp_unschedulable_until = $1,
			temp_unschedulable_reason = $2,
			updated_at = NOW()
		WHERE id = $3
			AND deleted_at IS NULL
			AND (temp_unschedulable_until IS NULL OR temp_unschedulable_until < $1)
	`, until, reason, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected <= 0 {
		return nil
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue temp unschedulable failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) SetGrokCredentialTempUnschedulableIfMatch(
	ctx context.Context,
	id int64,
	snapshot service.GrokCredentialMutationSnapshot,
	until time.Time,
	reason string,
) (bool, error) {
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
			UPDATE accounts AS a
			SET temp_unschedulable_until = CASE
					WHEN a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until < $1 THEN $1
					ELSE a.temp_unschedulable_until
				END,
				temp_unschedulable_reason = $2,
				updated_at = NOW()
			WHERE a.id = $3
				AND a.deleted_at IS NULL
				AND a.status = $4
				AND a.platform = $5
				AND a.type = $6
				AND a.schedulable IS TRUE
				AND a.credentials = $7::jsonb
				AND a.proxy_id IS NOT DISTINCT FROM $8
			RETURNING a.id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $9, updated.id, NULL, NULL FROM updated
	`, until, reason, id, service.StatusActive, service.PlatformGrok,
		service.AccountTypeOAuth, snapshot.CredentialsJSON, snapshot.ProxyID,
		service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}

func (r *accountRepository) ClearTempUnschedulable(ctx context.Context, id int64) error {
	_, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET temp_unschedulable_until = NULL,
			temp_unschedulable_reason = NULL,
			updated_at = NOW()
		WHERE id = $1
			AND deleted_at IS NULL
	`, id)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear temp unschedulable failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) ClearRateLimit(ctx context.Context, id int64) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		ClearRateLimitedAt().
		ClearRateLimitResetAt().
		ClearOverloadUntil().
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear rate limit failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		"UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) - 'antigravity_quota_scopes', updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL",
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear quota scopes failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) ClearModelRateLimits(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		"UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) - 'model_rate_limits', updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL",
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear model rate limit failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) UpdateSessionWindow(ctx context.Context, id int64, start, end *time.Time, status string) error {
	builder := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetSessionWindowStatus(status)
	if start != nil {
		builder.SetSessionWindowStart(*start)
	}
	if end != nil {
		builder.SetSessionWindowEnd(*end)
	}
	_, err := builder.Save(ctx)
	if err != nil {
		return err
	}
	// 触发调度器缓存更新（仅当窗口时间有变化时）
	if start != nil || end != nil {
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue session window update failed: account=%d err=%v", id, err)
		}
	}
	return nil
}

func (r *accountRepository) SetSchedulable(ctx context.Context, id int64, schedulable bool) error {
	_, err := clientFromContext(ctx, r.client).Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetSchedulable(schedulable).
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, txAwareSQLExecutor(ctx, r.sql, r.client), service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		if dbent.TxFromContext(ctx) != nil {
			return err
		}
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue schedulable change failed: account=%d err=%v", id, err)
	}
	if !schedulable && dbent.TxFromContext(ctx) == nil {
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return nil
}

func (r *accountRepository) AutoPauseExpiredAccounts(ctx context.Context, now time.Time) (int64, error) {
	rows, err := r.sql.QueryContext(ctx, `
		UPDATE accounts
		SET schedulable = FALSE,
			updated_at = NOW()
		WHERE deleted_at IS NULL
			AND schedulable = TRUE
			AND auto_pause_on_expired = TRUE
			AND expires_at IS NOT NULL
			AND expires_at <= $1
		RETURNING id
	`, now)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	accountIDs := make([]int64, 0)
	for rows.Next() {
		var accountID int64
		if err := rows.Scan(&accountID); err != nil {
			return 0, err
		}
		accountIDs = append(accountIDs, accountID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(accountIDs) > 0 {
		payload := map[string]any{"account_ids": accountIDs}
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountBulkChanged, nil, nil, payload); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue auto pause account changes failed: err=%v", err)
		}
	}
	return int64(len(accountIDs)), nil
}

func (r *accountRepository) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	// 使用 JSONB 合并操作实现原子更新，避免读-改-写的并发丢失更新问题
	payload, err := json.Marshal(updates)
	if err != nil {
		return err
	}

	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		"UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || $1::jsonb, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL",
		string(payload), id,
	)

	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if shouldEnqueueSchedulerOutboxForExtraUpdates(updates) ||
		(hasCodexQuotaProtectionRelevantExtraUpdate(updates) && r.isCodexQuotaProtectionActive(ctx, id)) {
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue extra update failed: account=%d err=%v", id, err)
		}
		r.syncSchedulerAccountSnapshot(ctx, id)
	} else {
		// 观测型 extra 字段不需要触发 bucket 重建，但仍同步单账号快照，
		// 让 sticky session / GetAccount 命中缓存时也能读到最新数据，
		// 同时避免缓存局部 patch 覆盖掉并发写入的其它账号字段。
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return nil
}

func (r *accountRepository) isCodexQuotaProtectionActive(ctx context.Context, accountID int64) bool {
	account, err := r.GetByID(ctx, accountID)
	if err != nil || account == nil {
		if err != nil {
			logger.LegacyPrintf("repository.account", "[Scheduler] check codex quota protection failed: account=%d err=%v", accountID, err)
		}
		return false
	}
	return account.IsCodexQuotaProtectionActiveAt(time.Now())
}

func shouldEnqueueSchedulerOutboxForExtraUpdates(updates map[string]any) bool {
	if len(updates) == 0 {
		return false
	}
	for key := range updates {
		if _, ok := schedulerRelevantExtraKeys[strings.TrimSpace(key)]; ok {
			return true
		}
		if isCodexQuotaLimitExtraKey(key) {
			return true
		}
		if isSchedulerNeutralExtraKey(key) {
			continue
		}
		return true
	}
	return false
}

func isCodexQuotaLimitExtraKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "codex_5h_limit_percent", "codex_7d_limit_percent":
		return true
	default:
		return false
	}
}

func hasCodexQuotaProtectionRelevantExtraUpdate(updates map[string]any) bool {
	for key := range updates {
		switch strings.TrimSpace(key) {
		case "codex_5h_used_percent", "codex_5h_reset_at", "codex_5h_reset_after_seconds",
			"codex_7d_used_percent", "codex_7d_reset_at", "codex_7d_reset_after_seconds",
			"codex_5h_limit_percent", "codex_7d_limit_percent":
			return true
		}
	}
	return false
}

func isSchedulerNeutralExtraKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if _, ok := schedulerNeutralExtraKeys[key]; ok {
		return true
	}
	for _, prefix := range schedulerNeutralExtraKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func (r *accountRepository) BulkUpdate(ctx context.Context, ids []int64, updates service.AccountBulkUpdate) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	setClauses := make([]string, 0, 8)
	args := make([]any, 0, 8)

	idx := 1
	if updates.Name != nil {
		setClauses = append(setClauses, "name = $"+itoa(idx))
		args = append(args, *updates.Name)
		idx++
	}
	if updates.ProxyID != nil {
		// 0 表示清除代理（前端发送 0 而不是 null 来表达清除意图）
		if *updates.ProxyID == 0 {
			setClauses = append(setClauses, "proxy_id = NULL")
		} else {
			setClauses = append(setClauses, "proxy_id = $"+itoa(idx))
			args = append(args, *updates.ProxyID)
			idx++
		}
	}
	if updates.Concurrency != nil {
		setClauses = append(setClauses, "concurrency = $"+itoa(idx))
		args = append(args, *updates.Concurrency)
		idx++
	}
	if updates.Priority != nil {
		setClauses = append(setClauses, "priority = $"+itoa(idx))
		args = append(args, *updates.Priority)
		idx++
	}
	if updates.RateMultiplier != nil {
		setClauses = append(setClauses, "rate_multiplier = $"+itoa(idx))
		args = append(args, *updates.RateMultiplier)
		idx++
	}
	if updates.LoadFactor != nil {
		if *updates.LoadFactor <= 0 {
			setClauses = append(setClauses, "load_factor = NULL")
		} else {
			setClauses = append(setClauses, "load_factor = $"+itoa(idx))
			args = append(args, *updates.LoadFactor)
			idx++
		}
	}
	if updates.Status != nil {
		setClauses = append(setClauses, "status = $"+itoa(idx))
		args = append(args, *updates.Status)
		idx++
		if *updates.Status == service.StatusError {
			setClauses = append(setClauses, "error_since = COALESCE(error_since, NOW())")
		} else {
			setClauses = append(setClauses, "error_since = NULL")
		}
	}
	if updates.Schedulable != nil {
		setClauses = append(setClauses, "schedulable = $"+itoa(idx))
		args = append(args, *updates.Schedulable)
		idx++
	}
	if updates.AccountLevel != nil {
		setClauses = append(setClauses, "account_level = $"+itoa(idx))
		args = append(args, service.NormalizeAccountLevel(*updates.AccountLevel))
		idx++
	}
	// JSONB 需要合并而非覆盖，使用 raw SQL 保持旧行为。
	if len(updates.Credentials) > 0 {
		payload, err := json.Marshal(updates.Credentials)
		if err != nil {
			return 0, err
		}
		setClauses = append(setClauses, "credentials = COALESCE(credentials, '{}'::jsonb) || $"+itoa(idx)+"::jsonb")
		args = append(args, payload)
		idx++
	}
	if len(updates.Extra) > 0 {
		payload, err := json.Marshal(updates.Extra)
		if err != nil {
			return 0, err
		}
		setClauses = append(setClauses, "extra = COALESCE(extra, '{}'::jsonb) || $"+itoa(idx)+"::jsonb")
		args = append(args, payload)
		idx++
	}

	if len(setClauses) == 0 {
		return 0, nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := "UPDATE accounts SET " + joinClauses(setClauses, ", ") + " WHERE id = ANY($" + itoa(idx) + ") AND deleted_at IS NULL"
	args = append(args, pq.Array(ids))

	exec := txAwareSQLExecutor(ctx, r.sql, r.client)
	if exec == nil {
		return 0, service.ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{"stage": "bulk_update_executor"})
	}
	result, err := exec.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, translateAccountPersistenceError(err, nil)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows > 0 {
		payload := map[string]any{"account_ids": ids}
		if err := enqueueSchedulerOutbox(ctx, exec, service.SchedulerOutboxEventAccountBulkChanged, nil, nil, payload); err != nil {
			if dbent.TxFromContext(ctx) != nil {
				return 0, err
			}
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue bulk update failed: err=%v", err)
		}
		shouldSync := false
		if updates.Status != nil && (*updates.Status == service.StatusError || *updates.Status == service.StatusDisabled) {
			shouldSync = true
		}
		if updates.Schedulable != nil && !*updates.Schedulable {
			shouldSync = true
		}
		if shouldSync && dbent.TxFromContext(ctx) == nil {
			r.syncSchedulerAccountSnapshots(ctx, ids)
		}
	}
	return rows, nil
}

type accountGroupQueryOptions struct {
	status      string
	schedulable bool
	platforms   []string // 允许的多个平台，空切片表示不进行平台过滤
}

func (r *accountRepository) queryAccountsByGroup(ctx context.Context, groupID int64, opts accountGroupQueryOptions) ([]service.Account, error) {
	q := r.client.AccountGroup.Query().
		Where(dbaccountgroup.GroupIDEQ(groupID))

	// 通过 account_groups 中间表查询账号，并按需叠加状态/平台/调度能力过滤。
	preds := make([]dbpredicate.Account, 0, 6)
	preds = append(preds, dbaccount.DeletedAtIsNil())
	if opts.schedulable {
		group, err := r.client.Group.Query().
			Where(dbgroup.IDEQ(groupID), dbgroup.DeletedAtIsNil()).
			Only(ctx)
		if err != nil {
			if dbent.IsNotFound(err) {
				return []service.Account{}, nil
			}
			return nil, err
		}
		if service.NormalizeGroupScope(group.Scope) == service.GroupScopePublic {
			// Public groups may contain stale bindings while a share transition is
			// being repaired. System-owned accounts keep their historical behavior,
			// but user-owned accounts are schedulable publicly only after approval.
			preds = append(preds, publicGroupSchedulableAccountPredicate())
		}
		requiredLevel := service.NormalizeRequiredAccountLevel(group.RequiredAccountLevel)
		if group.Platform == service.PlatformOpenAI && requiredLevel != "" {
			allowedLevels := service.OpenAISharedPoolAllowedAccountLevels(requiredLevel)
			if len(allowedLevels) == 0 {
				return []service.Account{}, nil
			}
			preds = append(preds, dbaccount.PlatformEQ(service.PlatformOpenAI), dbaccount.AccountLevelIn(allowedLevels...))
		}
	}
	if opts.status != "" {
		preds = append(preds, dbaccount.StatusEQ(opts.status))
	}
	if len(opts.platforms) > 0 {
		preds = append(preds, dbaccount.PlatformIn(opts.platforms...))
	}
	if opts.schedulable {
		now := time.Now()
		preds = append(preds,
			dbaccount.SchedulableEQ(true),
			notDrainingExternalPlacementPredicate(),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		)
	}

	if len(preds) > 0 {
		q = q.Where(dbaccountgroup.HasAccountWith(preds...))
	}

	groups, err := q.
		Order(
			dbaccountgroup.ByPriority(),
			dbaccountgroup.ByAccountField(dbaccount.FieldPriority),
		).
		WithAccount().
		All(ctx)
	if err != nil {
		return nil, err
	}

	orderedIDs := make([]int64, 0, len(groups))
	accountMap := make(map[int64]*dbent.Account, len(groups))
	for _, ag := range groups {
		if ag.Edges.Account == nil {
			continue
		}
		if _, exists := accountMap[ag.AccountID]; exists {
			continue
		}
		accountMap[ag.AccountID] = ag.Edges.Account
		orderedIDs = append(orderedIDs, ag.AccountID)
	}

	accounts := make([]*dbent.Account, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		if acc, ok := accountMap[id]; ok {
			accounts = append(accounts, acc)
		}
	}

	return r.accountsToService(ctx, accounts)
}

func publicGroupSchedulableAccountPredicate() dbpredicate.Account {
	return dbaccount.Or(
		dbaccount.OwnerUserIDIsNil(),
		dbaccount.And(
			dbaccount.ShareModeEQ(service.AccountShareModePublic),
			dbaccount.ShareStatusEQ(service.AccountShareStatusApproved),
		),
	)
}

func (r *accountRepository) accountsToService(ctx context.Context, accounts []*dbent.Account) ([]service.Account, error) {
	if len(accounts) == 0 {
		return []service.Account{}, nil
	}

	accountIDs := make([]int64, 0, len(accounts))
	proxyIDs := make([]int64, 0, len(accounts))
	for _, acc := range accounts {
		accountIDs = append(accountIDs, acc.ID)
		if acc.ProxyID != nil {
			proxyIDs = append(proxyIDs, *acc.ProxyID)
		}
	}

	proxyMap, err := r.loadProxies(ctx, proxyIDs)
	if err != nil {
		return nil, err
	}
	groupsByAccount, groupIDsByAccount, accountGroupsByAccount, err := r.loadAccountGroups(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	errorSinceByAccount, err := r.loadAccountErrorSince(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	externalPlacementsByAccount, err := r.loadAccountExternalPlacements(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	roomListingIDsByAccount, err := r.loadAccountShareRoomListingIDs(ctx, accountIDs)
	if err != nil {
		return nil, err
	}

	outAccounts := make([]service.Account, 0, len(accounts))
	for _, acc := range accounts {
		out := accountEntityToService(acc)
		if out == nil {
			continue
		}
		if errorSince, ok := errorSinceByAccount[acc.ID]; ok {
			out.ErrorSince = errorSince
		}
		if acc.ProxyID != nil {
			if proxy, ok := proxyMap[*acc.ProxyID]; ok {
				out.Proxy = proxy
			}
		}
		if groups, ok := groupsByAccount[acc.ID]; ok {
			out.Groups = groups
		}
		if groupIDs, ok := groupIDsByAccount[acc.ID]; ok {
			out.GroupIDs = groupIDs
		}
		if ags, ok := accountGroupsByAccount[acc.ID]; ok {
			out.AccountGroups = ags
		}
		if placement, ok := externalPlacementsByAccount[acc.ID]; ok {
			placementCopy := placement
			out.ExternalPlacement = &placementCopy
		}
		if listingID, ok := roomListingIDsByAccount[acc.ID]; ok {
			id := listingID
			out.AccountShareModeListingID = &id
		}
		outAccounts = append(outAccounts, *out)
	}

	return outAccounts, nil
}

func (r *accountRepository) loadAccountExternalPlacements(ctx context.Context, accountIDs []int64) (map[int64]service.AccountExternalPlacement, error) {
	out := make(map[int64]service.AccountExternalPlacement)
	if len(accountIDs) == 0 {
		return out, nil
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT
			placement.account_id,
			placement.placement_type,
			placement.public_group_id,
			placement.state,
			placement.version
		FROM account_external_placements placement
		WHERE placement.account_id = ANY($1)
	`, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var accountID int64
		var placement service.AccountExternalPlacement
		var publicGroupID sql.NullInt64
		if err := rows.Scan(
			&accountID,
			&placement.Target,
			&publicGroupID,
			&placement.State,
			&placement.Version,
		); err != nil {
			return nil, err
		}
		placement.PublicGroupID = sqlNullInt64Ptr(publicGroupID)
		out[accountID] = placement
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *accountRepository) loadAccountShareRoomListingIDs(ctx context.Context, accountIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64)
	if len(accountIDs) == 0 {
		return out, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT room_account.account_id, room_account.listing_id
		FROM account_share_room_accounts room_account
		JOIN account_share_listings listing
			ON listing.id = room_account.listing_id
			AND listing.deleted_at IS NULL
		WHERE room_account.account_id = ANY($1)
			AND room_account.state IN ('active', 'draining')
	`, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var accountID, listingID int64
		if err := rows.Scan(&accountID, &listingID); err != nil {
			return nil, err
		}
		out[accountID] = listingID
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *accountRepository) loadAccountErrorSince(ctx context.Context, accountIDs []int64) (map[int64]*time.Time, error) {
	out := make(map[int64]*time.Time)
	if len(accountIDs) == 0 {
		return out, nil
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, error_since
		FROM accounts
		WHERE id = ANY($1)
			AND error_since IS NOT NULL
	`, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id int64
		var errorSince time.Time
		if err := rows.Scan(&id, &errorSince); err != nil {
			return nil, err
		}
		value := errorSince
		out[id] = &value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func tempUnschedulablePredicate() dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		col := s.C("temp_unschedulable_until")
		s.Where(entsql.Or(
			entsql.IsNull(col),
			entsql.LTE(col, entsql.Expr("NOW()")),
		))
	})
}

func notDrainingExternalPlacementPredicate() dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		placement := entsql.Table("account_external_placements")
		subquery := entsql.Select(placement.C("account_id")).
			From(placement).
			Where(entsql.And(
				entsql.ColumnsEQ(placement.C("account_id"), s.C(dbaccount.FieldID)),
				entsql.EQ(placement.C("state"), "draining"),
			))
		s.Where(entsql.Not(entsql.Exists(subquery)))
	})
}

func notExpiredPredicate(now time.Time) dbpredicate.Account {
	return dbaccount.Or(
		dbaccount.ExpiresAtIsNil(),
		dbaccount.ExpiresAtGT(now),
		dbaccount.AutoPauseOnExpiredEQ(false),
	)
}

func (r *accountRepository) loadProxies(ctx context.Context, proxyIDs []int64) (map[int64]*service.Proxy, error) {
	proxyMap := make(map[int64]*service.Proxy)
	proxyIDs = uniquePositiveInt64s(proxyIDs)
	if len(proxyIDs) == 0 {
		return proxyMap, nil
	}

	for start := 0; start < len(proxyIDs); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(proxyIDs) {
			end = len(proxyIDs)
		}
		proxies, err := r.client.Proxy.Query().Where(dbproxy.IDIn(proxyIDs[start:end]...)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range proxies {
			proxyMap[p.ID] = proxyEntityToService(p)
		}
	}
	return proxyMap, nil
}

func (r *accountRepository) loadAccountGroups(ctx context.Context, accountIDs []int64) (map[int64][]*service.Group, map[int64][]int64, map[int64][]service.AccountGroup, error) {
	groupsByAccount := make(map[int64][]*service.Group)
	groupIDsByAccount := make(map[int64][]int64)
	accountGroupsByAccount := make(map[int64][]service.AccountGroup)

	accountIDs = uniquePositiveInt64s(accountIDs)
	if len(accountIDs) == 0 {
		return groupsByAccount, groupIDsByAccount, accountGroupsByAccount, nil
	}

	for start := 0; start < len(accountIDs); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(accountIDs) {
			end = len(accountIDs)
		}
		entries, err := r.client.AccountGroup.Query().
			Where(dbaccountgroup.AccountIDIn(accountIDs[start:end]...)).
			WithGroup().
			Order(dbaccountgroup.ByAccountID(), dbaccountgroup.ByPriority()).
			All(ctx)
		if err != nil {
			return nil, nil, nil, err
		}

		for _, ag := range entries {
			groupSvc := groupEntityToService(ag.Edges.Group)
			agSvc := service.AccountGroup{
				AccountID: ag.AccountID,
				GroupID:   ag.GroupID,
				Priority:  ag.Priority,
				CreatedAt: ag.CreatedAt,
				Group:     groupSvc,
			}
			accountGroupsByAccount[ag.AccountID] = append(accountGroupsByAccount[ag.AccountID], agSvc)
			groupIDsByAccount[ag.AccountID] = append(groupIDsByAccount[ag.AccountID], ag.GroupID)
			if groupSvc != nil {
				groupsByAccount[ag.AccountID] = append(groupsByAccount[ag.AccountID], groupSvc)
			}
		}
	}

	return groupsByAccount, groupIDsByAccount, accountGroupsByAccount, nil
}

func (r *accountRepository) loadAccountGroupIDs(ctx context.Context, accountID int64) ([]int64, error) {
	entries, err := clientFromContext(ctx, r.client).AccountGroup.
		Query().
		Where(dbaccountgroup.AccountIDEQ(accountID)).
		Order(dbent.Asc(dbaccountgroup.FieldPriority), dbent.Asc(dbaccountgroup.FieldGroupID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.GroupID)
	}
	return ids, nil
}

func mergeGroupIDs(a []int64, b []int64) []int64 {
	seen := make(map[int64]struct{}, len(a)+len(b))
	out := make([]int64, 0, len(a)+len(b))
	for _, id := range a {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range b {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func buildSchedulerGroupPayload(groupIDs []int64) map[string]any {
	if len(groupIDs) == 0 {
		return nil
	}
	return map[string]any{"group_ids": groupIDs}
}

func accountEntityToService(m *dbent.Account) *service.Account {
	if m == nil {
		return nil
	}

	rateMultiplier := m.RateMultiplier

	return &service.Account{
		ID:                      m.ID,
		Name:                    m.Name,
		Notes:                   m.Notes,
		Platform:                m.Platform,
		AccountLevel:            service.NormalizeAccountLevel(m.AccountLevel),
		Type:                    m.Type,
		Credentials:             copyJSONMap(m.Credentials),
		Extra:                   copyJSONMap(m.Extra),
		OwnerUserID:             m.OwnerUserID,
		ShareMode:               service.NormalizeAccountShareMode(m.ShareMode),
		ShareStatus:             service.NormalizeAccountShareStatus(m.ShareStatus),
		SharePolicyID:           m.SharePolicyID,
		ProxyID:                 m.ProxyID,
		Concurrency:             m.Concurrency,
		Priority:                m.Priority,
		RateMultiplier:          &rateMultiplier,
		LoadFactor:              m.LoadFactor,
		LoadFactorPaidCeiling:   m.LoadFactorPaidCeiling,
		Status:                  m.Status,
		ErrorMessage:            derefString(m.ErrorMessage),
		LastUsedAt:              m.LastUsedAt,
		ExpiresAt:               m.ExpiresAt,
		AutoPauseOnExpired:      m.AutoPauseOnExpired,
		CreatedAt:               m.CreatedAt,
		UpdatedAt:               m.UpdatedAt,
		Schedulable:             m.Schedulable,
		RateLimitedAt:           m.RateLimitedAt,
		RateLimitResetAt:        m.RateLimitResetAt,
		OverloadUntil:           m.OverloadUntil,
		TempUnschedulableUntil:  m.TempUnschedulableUntil,
		TempUnschedulableReason: derefString(m.TempUnschedulableReason),
		SessionWindowStart:      m.SessionWindowStart,
		SessionWindowEnd:        m.SessionWindowEnd,
		SessionWindowStatus:     derefString(m.SessionWindowStatus),
	}
}

func normalizeJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	return in
}

func normalizeLoadFactorPaidCeiling(value int) int {
	if value < service.OwnedPersonalDefaultLoadFactor {
		return service.OwnedPersonalDefaultLoadFactor
	}
	return value
}

func copyJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func joinClauses(clauses []string, sep string) string {
	if len(clauses) == 0 {
		return ""
	}
	out := clauses[0]
	for i := 1; i < len(clauses); i++ {
		out += sep + clauses[i]
	}
	return out
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

// FindByExtraField 根据 extra 字段中的键值对查找账号。
// 使用 PostgreSQL JSONB @> 操作符进行高效查询（需要 GIN 索引支持）。
//
// FindByExtraField finds accounts by key-value pairs in the extra field.
// Uses PostgreSQL JSONB @> operator for efficient queries (requires GIN index).
func (r *accountRepository) FindByExtraField(ctx context.Context, key string, value any) ([]service.Account, error) {
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.DeletedAtIsNil(),
			func(s *entsql.Selector) {
				path := sqljson.Path(key)
				switch v := value.(type) {
				case string:
					preds := []*entsql.Predicate{sqljson.ValueEQ(dbaccount.FieldExtra, v, path)}
					if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
						preds = append(preds, sqljson.ValueEQ(dbaccount.FieldExtra, parsed, path))
					}
					if len(preds) == 1 {
						s.Where(preds[0])
					} else {
						s.Where(entsql.Or(preds...))
					}
				case int:
					s.Where(entsql.Or(
						sqljson.ValueEQ(dbaccount.FieldExtra, v, path),
						sqljson.ValueEQ(dbaccount.FieldExtra, strconv.Itoa(v), path),
					))
				case int64:
					s.Where(entsql.Or(
						sqljson.ValueEQ(dbaccount.FieldExtra, v, path),
						sqljson.ValueEQ(dbaccount.FieldExtra, strconv.FormatInt(v, 10), path),
					))
				case json.Number:
					if parsed, err := v.Int64(); err == nil {
						s.Where(entsql.Or(
							sqljson.ValueEQ(dbaccount.FieldExtra, parsed, path),
							sqljson.ValueEQ(dbaccount.FieldExtra, v.String(), path),
						))
					} else {
						s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, v.String(), path))
					}
				default:
					s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, value, path))
				}
			},
		).
		All(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}

	return r.accountsToService(ctx, accounts)
}

// nowUTC is a SQL expression to generate a UTC RFC3339 timestamp string.
const nowUTC = `to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`

// dailyExpiredExpr is a SQL expression that evaluates to TRUE when daily quota period has expired.
// Supports both rolling (24h from start) and fixed (pre-computed reset_at) modes.
const dailyExpiredExpr = `(
	CASE WHEN COALESCE(extra->>'quota_daily_reset_mode', 'rolling') = 'fixed'
	THEN NOW() >= COALESCE((extra->>'quota_daily_reset_at')::timestamptz, '1970-01-01'::timestamptz)
	ELSE COALESCE((extra->>'quota_daily_start')::timestamptz, '1970-01-01'::timestamptz)
		+ '24 hours'::interval <= NOW()
	END
)`

// weeklyExpiredExpr is a SQL expression that evaluates to TRUE when weekly quota period has expired.
const weeklyExpiredExpr = `(
	CASE WHEN COALESCE(extra->>'quota_weekly_reset_mode', 'rolling') = 'fixed'
	THEN NOW() >= COALESCE((extra->>'quota_weekly_reset_at')::timestamptz, '1970-01-01'::timestamptz)
	ELSE COALESCE((extra->>'quota_weekly_start')::timestamptz, '1970-01-01'::timestamptz)
		+ '168 hours'::interval <= NOW()
	END
)`

// nextDailyResetAtExpr is a SQL expression to compute the next daily reset_at when a reset occurs.
// For fixed mode: computes the next future reset time based on NOW(), timezone, and configured hour.
// This correctly handles long-inactive accounts by jumping directly to the next valid reset point.
const nextDailyResetAtExpr = `(
	CASE WHEN COALESCE(extra->>'quota_daily_reset_mode', 'rolling') = 'fixed'
	THEN to_char((
		-- Compute today's reset point in the configured timezone, then pick next future one
		CASE WHEN NOW() >= (
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_daily_reset_hour')::int, 0) || ' hours')::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		-- NOW() is at or past today's reset point → next reset is tomorrow
		THEN (
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_daily_reset_hour')::int, 0) || ' hours')::interval
			+ '1 day'::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		-- NOW() is before today's reset point → next reset is today
		ELSE (
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_daily_reset_hour')::int, 0) || ' hours')::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		END
	) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
	ELSE NULL END
)`

// nextWeeklyResetAtExpr is a SQL expression to compute the next weekly reset_at when a reset occurs.
// For fixed mode: computes the next future reset time based on NOW(), timezone, configured day and hour.
// This correctly handles long-inactive accounts by jumping directly to the next valid reset point.
const nextWeeklyResetAtExpr = `(
	CASE WHEN COALESCE(extra->>'quota_weekly_reset_mode', 'rolling') = 'fixed'
	THEN to_char((
		-- Compute this week's reset point in the configured timezone
		-- Step 1: get today's date at reset hour in configured tz
		-- Step 2: compute days forward to target weekday
		-- Step 3: if same day but past reset hour, advance 7 days
		CASE
		WHEN (
			-- days_forward = (target_day - current_day + 7) % 7
			(COALESCE((extra->>'quota_weekly_reset_day')::int, 1)
			 - EXTRACT(DOW FROM NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))::int
			 + 7) % 7
		) = 0 AND NOW() >= (
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_weekly_reset_hour')::int, 0) || ' hours')::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		-- Same weekday and past reset hour → next week
		THEN (
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_weekly_reset_hour')::int, 0) || ' hours')::interval
			+ '7 days'::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		ELSE (
			-- Advance to target weekday this week (or next if days_forward > 0)
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_weekly_reset_hour')::int, 0) || ' hours')::interval
			+ ((
				(COALESCE((extra->>'quota_weekly_reset_day')::int, 1)
				 - EXTRACT(DOW FROM NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))::int
				 + 7) % 7
			) || ' days')::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		END
	) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
	ELSE NULL END
)`

// IncrementQuotaUsed 原子递增账号的配额用量（总/日/周三个维度）
// 日/周额度在周期过期时自动重置为 0 再递增。
// 支持滚动窗口（rolling）和固定时间（fixed）两种重置模式。
func (r *accountRepository) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) error {
	rows, err := r.sql.QueryContext(ctx,
		`UPDATE accounts SET extra = (
			COALESCE(extra, '{}'::jsonb)
			-- 总额度：始终递增
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)
			-- 日额度：仅在 quota_daily_limit > 0 时处理
			|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_daily_used',
					CASE WHEN `+dailyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
					'quota_daily_start',
					CASE WHEN `+dailyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_daily_start', `+nowUTC+`) END
				)
				-- 固定模式重置时更新下次重置时间
				|| CASE WHEN `+dailyExpiredExpr+` AND `+nextDailyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_daily_reset_at', `+nextDailyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
			-- 周额度：仅在 quota_weekly_limit > 0 时处理
			|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_weekly_used',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
					'quota_weekly_start',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_weekly_start', `+nowUTC+`) END
				)
				-- 固定模式重置时更新下次重置时间
				|| CASE WHEN `+weeklyExpiredExpr+` AND `+nextWeeklyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_weekly_reset_at', `+nextWeeklyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
		), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING
			COALESCE((extra->>'quota_used')::numeric, 0),
			COALESCE((extra->>'quota_limit')::numeric, 0)`,
		amount, id)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	var newUsed, limit float64
	if rows.Next() {
		if err := rows.Scan(&newUsed, &limit); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// 任一维度配额刚超限时触发调度快照刷新
	if limit > 0 && newUsed >= limit && (newUsed-amount) < limit {
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", id, err)
		}
	}
	return nil
}

// ResetQuotaUsed 重置账号所有维度的配额用量为 0
// 保留固定重置模式的配置字段（quota_daily_reset_mode 等），仅清零用量和窗口起始时间
func (r *accountRepository) ResetQuotaUsed(ctx context.Context, id int64) error {
	_, err := r.sql.ExecContext(ctx,
		`UPDATE accounts SET extra = (
			COALESCE(extra, '{}'::jsonb)
			|| '{"quota_used": 0, "quota_daily_used": 0, "quota_weekly_used": 0}'::jsonb
		) - 'quota_daily_start' - 'quota_weekly_start' - 'quota_daily_reset_at' - 'quota_weekly_reset_at', updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`,
		id)
	if err != nil {
		return err
	}
	// 重置配额后触发调度快照刷新，使账号重新参与调度
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue quota reset failed: account=%d err=%v", id, err)
	}
	return nil
}
