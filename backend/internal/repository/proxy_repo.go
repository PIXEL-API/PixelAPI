package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"

	entsql "entgo.io/ent/dialect/sql"
)

type sqlQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type proxyRepository struct {
	client *dbent.Client
	sql    sqlQuerier
}

var _ service.ProxyDeletionRepository = (*proxyRepository)(nil)

func NewProxyRepository(client *dbent.Client, sqlDB *sql.DB) service.ProxyRepository {
	return newProxyRepositoryWithSQL(client, sqlDB)
}

func newProxyRepositoryWithSQL(client *dbent.Client, sqlq sqlQuerier) *proxyRepository {
	return &proxyRepository{client: client, sql: sqlq}
}

func (r *proxyRepository) Create(ctx context.Context, proxyIn *service.Proxy) error {
	if proxyIn == nil {
		return errors.New("proxy create input is nil")
	}
	if r == nil || r.client == nil {
		return errors.New("proxy create repository client is unavailable")
	}

	// Ent fills generated fields before the transaction commits. Keep them on a
	// candidate so a commit failure cannot publish a phantom ID/timestamp to the
	// caller. Outer transaction owners must likewise pass a staged object; CRS is
	// the current production caller and publishes it only after its UoW commits.
	candidate := *proxyIn
	if strings.TrimSpace(candidate.FallbackMode) == "" {
		candidate.FallbackMode = service.FallbackModeNone
	}
	if err := service.ValidateProxyLifecycleFields(&candidate); err != nil {
		return err
	}

	err := r.withProxyWriteTransaction(ctx, "proxy create", func(
		txCtx context.Context,
		txClient *dbent.Client,
		exec sqlQueryExecutor,
	) error {
		if candidate.BackupProxyID != nil {
			locked, lockErr := lockProxyMutationTargets(txCtx, exec, []int64{*candidate.BackupProxyID})
			if lockErr != nil {
				return fmt.Errorf("lock backup proxy %d for create: %w", *candidate.BackupProxyID, lockErr)
			}
			backup, ok := locked[*candidate.BackupProxyID]
			if !ok {
				return service.ErrProxyBackupInvalid
			}
			if err := service.ValidateProxyLifecycleCandidate(&candidate, backup.serviceProxy()); err != nil {
				return err
			}
		}
		return r.createWithClient(txCtx, txClient, &candidate)
	})
	if err != nil {
		return err
	}
	*proxyIn = candidate
	return nil
}

func (r *proxyRepository) createWithClient(ctx context.Context, client *dbent.Client, proxyIn *service.Proxy) error {
	builder := client.Proxy.Create().
		SetName(proxyIn.Name).
		SetProtocol(proxyIn.Protocol).
		SetHost(proxyIn.Host).
		SetPort(proxyIn.Port).
		SetStatus(proxyIn.Status).
		SetMaxAccounts(proxyIn.MaxAccounts).
		SetNillableExpiresAt(proxyIn.ExpiresAt).
		SetFallbackMode(proxyIn.FallbackMode).
		SetNillableBackupProxyID(proxyIn.BackupProxyID).
		SetExpiryWarnDays(proxyIn.ExpiryWarnDays).
		SetPlatform(service.NormalizeProxyPlatform(proxyIn.Platform)).
		SetRequiredAccountLevel(service.NormalizeRequiredAccountLevel(proxyIn.RequiredAccountLevel))
	if proxyIn.Username != "" {
		builder.SetUsername(proxyIn.Username)
	}
	if proxyIn.Password != "" {
		builder.SetPassword(proxyIn.Password)
	}
	if proxyIn.OwnerUserID != nil && *proxyIn.OwnerUserID > 0 {
		builder.SetOwnerUserID(*proxyIn.OwnerUserID)
	}

	created, err := builder.Save(ctx)
	if err == nil {
		applyProxyEntityToService(proxyIn, created)
	}
	return err
}

func (r *proxyRepository) withProxyWriteTransaction(
	ctx context.Context,
	operation string,
	mutate func(context.Context, *dbent.Client, sqlQueryExecutor) error,
) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("%s repository client is unavailable", operation)
	}
	if mutate == nil {
		return fmt.Errorf("%s transaction callback is nil", operation)
	}
	if dbent.TxFromContext(ctx) != nil {
		client := clientFromContext(ctx, r.client)
		exec := sqlExecutorFromEntClient(client)
		if exec == nil {
			return fmt.Errorf("%s transaction SQL executor is unavailable", operation)
		}
		return mutate(ctx, client, exec)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin %s transaction: %w", operation, err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	exec := sqlExecutorFromEntClient(tx.Client())
	if exec == nil {
		return fmt.Errorf("%s transaction SQL executor is unavailable", operation)
	}
	if err := mutate(txCtx, tx.Client(), exec); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s transaction: %w", operation, err)
	}
	return nil
}

func (r *proxyRepository) GetByID(ctx context.Context, id int64) (*service.Proxy, error) {
	m, err := clientFromContext(ctx, r.client).Proxy.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrProxyNotFound
		}
		return nil, err
	}
	return proxyEntityToService(m), nil
}

func (r *proxyRepository) ListByIDs(ctx context.Context, ids []int64) ([]service.Proxy, error) {
	if len(ids) == 0 {
		return []service.Proxy{}, nil
	}

	proxies, err := r.client.Proxy.Query().
		Where(proxy.IDIn(ids...)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]service.Proxy, 0, len(proxies))
	for i := range proxies {
		out = append(out, *proxyEntityToService(proxies[i]))
	}
	return out, nil
}

func (r *proxyRepository) Update(ctx context.Context, proxyIn *service.Proxy) error {
	return r.updateAtomically(ctx, proxyIn, false)
}

// UpdateWithOwnerAssignment 在通用原子更新守卫之外额外校验代理归属冲突。
func (r *proxyRepository) UpdateWithOwnerAssignment(ctx context.Context, proxyIn *service.Proxy) error {
	return r.updateAtomically(ctx, proxyIn, true)
}

// updateAtomically 统一代理更新的最终一致性守卫。所有写入口都先锁定 live proxy，
// 再读取账号绑定数；该锁与账号创建、改绑、到期改投共用同一 proxy→account 顺序，
// 因而降低 max_accounts 与并发新增绑定无法交叉产生超配状态。
//
// 已有 Ent 事务由调用方拥有，本方法只复用其 client/executor，不嵌套开启或提交事务。
func (r *proxyRepository) updateAtomically(ctx context.Context, proxyIn *service.Proxy, checkOwnerAssignment bool) error {
	if proxyIn == nil {
		return service.ErrProxyNotFound
	}
	return r.withProxyWriteTransaction(ctx, "proxy update", func(
		txCtx context.Context,
		txClient *dbent.Client,
		exec sqlQueryExecutor,
	) error {
		return r.updateLocked(txCtx, txClient, exec, proxyIn, checkOwnerAssignment)
	})
}

type lockedProxyMutationTarget struct {
	id          int64
	ownerUserID *int64
	updatedAt   time.Time
}

func (target lockedProxyMutationTarget) serviceProxy() *service.Proxy {
	var ownerUserID *int64
	if target.ownerUserID != nil {
		value := *target.ownerUserID
		ownerUserID = &value
	}
	return &service.Proxy{
		ID:          target.id,
		OwnerUserID: ownerUserID,
		UpdatedAt:   target.updatedAt,
	}
}

// lockProxyMutationTargets acquires every proxy lock in ascending ID order.
// Callers must collect the complete lock set before invoking it; taking a
// source lock and then discovering a backup would deadlock for A→B / B→A.
func lockProxyMutationTargets(
	ctx context.Context,
	exec sqlQueryExecutor,
	ids []int64,
) (targets map[int64]lockedProxyMutationTarget, err error) {
	unique := make(map[int64]struct{}, len(ids))
	ordered := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := unique[id]; exists {
			continue
		}
		unique[id] = struct{}{}
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	targets = make(map[int64]lockedProxyMutationTarget, len(ordered))
	if len(ordered) == 0 {
		return targets, nil
	}

	rows, err := exec.QueryContext(ctx, `
		SELECT id, owner_user_id, updated_at
		FROM proxies
		WHERE id = ANY($1)
			AND deleted_at IS NULL
		ORDER BY id ASC
		FOR UPDATE
	`, pq.Array(ordered))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	for rows.Next() {
		var target lockedProxyMutationTarget
		if err := rows.Scan(&target.id, &target.ownerUserID, &target.updatedAt); err != nil {
			return nil, err
		}
		targets[target.id] = target
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return targets, nil
}

func (r *proxyRepository) updateLocked(
	ctx context.Context,
	client *dbent.Client,
	exec sqlQueryExecutor,
	proxyIn *service.Proxy,
	checkOwnerAssignment bool,
) error {
	if err := service.ValidateProxyLifecycleFields(proxyIn); err != nil {
		return err
	}
	lockIDs := []int64{proxyIn.ID}
	if proxyIn.BackupProxyID != nil {
		lockIDs = append(lockIDs, *proxyIn.BackupProxyID)
	}
	locked, err := lockProxyMutationTargets(ctx, exec, lockIDs)
	if err != nil {
		return fmt.Errorf("lock proxy lifecycle targets for update: %w", err)
	}
	source, ok := locked[proxyIn.ID]
	if !ok {
		return service.ErrProxyNotFound
	}
	if proxyIn.UpdatedAt.IsZero() || !source.updatedAt.Equal(proxyIn.UpdatedAt) {
		return service.ErrProxyMutationStale
	}

	var backup *service.Proxy
	if proxyIn.BackupProxyID != nil {
		lockedBackup, exists := locked[*proxyIn.BackupProxyID]
		if !exists {
			return service.ErrProxyBackupInvalid
		}
		backup = lockedBackup.serviceProxy()
	}
	if err := service.ValidateProxyLifecycleCandidate(proxyIn, backup); err != nil {
		return err
	}

	currentAccounts, err := countLiveProxyAccounts(ctx, exec, proxyIn.ID)
	if err != nil {
		return fmt.Errorf("count proxy %d accounts for update: %w", proxyIn.ID, err)
	}
	if proxyIn.MaxAccounts > 0 && currentAccounts > int64(proxyIn.MaxAccounts) {
		return service.ProxyAccountLimitBelowCurrentError(proxyIn.ID, currentAccounts)
	}

	ownerAssignmentChanged := proxyOwnerID(source.ownerUserID) != proxyOwnerID(proxyIn.OwnerUserID)
	if (checkOwnerAssignment || ownerAssignmentChanged) && proxyOwnerID(proxyIn.OwnerUserID) > 0 {
		boundToOthers, countErr := countProxyOwnerAssignmentConflicts(
			ctx,
			exec,
			proxyIn.ID,
			proxyOwnerID(proxyIn.OwnerUserID),
		)
		if countErr != nil {
			return countErr
		}
		if boundToOthers > 0 {
			return service.ErrProxyOwnerConflict
		}
	}

	return r.updateWithClient(ctx, client, proxyIn, source.updatedAt)
}

func proxyOwnerID(ownerUserID *int64) int64 {
	if ownerUserID == nil || *ownerUserID <= 0 {
		return 0
	}
	return *ownerUserID
}

func countLiveProxyAccounts(ctx context.Context, exec sqlExecutor, proxyID int64) (int64, error) {
	var count int64
	err := scanSingleRow(ctx, exec, `
		SELECT COUNT(*)
		FROM accounts
		WHERE proxy_id = $1
			AND deleted_at IS NULL
	`, []any{proxyID}, &count)
	return count, err
}

func countLiveProxyBackupReferences(ctx context.Context, exec sqlExecutor, proxyID int64) (int64, error) {
	var count int64
	err := scanSingleRow(ctx, exec, `
		SELECT COUNT(*)
		FROM proxies
		WHERE backup_proxy_id = $1
			AND deleted_at IS NULL
	`, []any{proxyID}, &count)
	return count, err
}

// countProxyOwnerAssignmentConflicts 统计与目标专属归属不兼容的存量账号。
// owner_user_id IS NULL 表示平台账号，同样不能继续占用某个用户的专属出口。
func countProxyOwnerAssignmentConflicts(
	ctx context.Context,
	exec sqlExecutor,
	proxyID int64,
	ownerUserID int64,
) (int64, error) {
	var conflicts int64
	err := scanSingleRow(ctx, exec, `
		SELECT COUNT(*)
		FROM accounts
		WHERE proxy_id = $1
			AND deleted_at IS NULL
			AND (owner_user_id IS NULL OR owner_user_id <> $2)
	`, []any{proxyID, ownerUserID}, &conflicts)
	return conflicts, err
}

func (r *proxyRepository) updateWithClient(
	ctx context.Context,
	client *dbent.Client,
	proxyIn *service.Proxy,
	expectedUpdatedAt time.Time,
) error {
	builder := client.Proxy.UpdateOneID(proxyIn.ID).
		Where(
			proxy.UpdatedAtEQ(expectedUpdatedAt),
			proxy.DeletedAtIsNil(),
		).
		SetName(proxyIn.Name).
		SetProtocol(proxyIn.Protocol).
		SetHost(proxyIn.Host).
		SetPort(proxyIn.Port).
		SetStatus(proxyIn.Status).
		SetMaxAccounts(proxyIn.MaxAccounts).
		SetFallbackMode(proxyIn.FallbackMode).
		SetExpiryWarnDays(proxyIn.ExpiryWarnDays).
		SetPlatform(service.NormalizeProxyPlatform(proxyIn.Platform)).
		SetRequiredAccountLevel(service.NormalizeRequiredAccountLevel(proxyIn.RequiredAccountLevel))
	if proxyIn.ExpiresAt != nil {
		builder.SetExpiresAt(*proxyIn.ExpiresAt)
	} else {
		builder.ClearExpiresAt()
	}
	if proxyIn.BackupProxyID != nil {
		builder.SetBackupProxyID(*proxyIn.BackupProxyID)
	} else {
		builder.ClearBackupProxyID()
	}
	if proxyIn.Username != "" {
		builder.SetUsername(proxyIn.Username)
	} else {
		builder.ClearUsername()
	}
	if proxyIn.Password != "" {
		builder.SetPassword(proxyIn.Password)
	} else {
		builder.ClearPassword()
	}
	if proxyIn.OwnerUserID != nil && *proxyIn.OwnerUserID > 0 {
		builder.SetOwnerUserID(*proxyIn.OwnerUserID)
	} else {
		builder.ClearOwnerUserID()
	}

	updated, err := builder.Save(ctx)
	if err == nil {
		applyProxyEntityToService(proxyIn, updated)
		return nil
	}
	if dbent.IsNotFound(err) {
		return service.ErrProxyMutationStale
	}
	return err
}

func (r *proxyRepository) Delete(ctx context.Context, id int64) error {
	return r.DeleteIfUnused(ctx, id)
}

// DeleteIfUnused 将代理行锁、存量绑定检查和软删除收敛在同一事务中。
// 所有账号绑定路径使用同一代理行锁后，删除与绑定只能串行生效，避免 live account
// 最终指向已软删除代理。不存在或已删除的代理保持既有幂等删除语义。
func (r *proxyRepository) DeleteIfUnused(ctx context.Context, id int64) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("proxy deletion repository client is unavailable")
	}
	if dbent.TxFromContext(ctx) != nil {
		client := clientFromContext(ctx, r.client)
		exec := sqlExecutorFromEntClient(client)
		if exec == nil {
			return fmt.Errorf("proxy deletion transaction SQL executor is unavailable")
		}
		_, err := deleteProxyIfUnusedLocked(ctx, exec, id)
		return err
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	exec := sqlExecutorFromEntClient(tx.Client())
	if exec == nil {
		return fmt.Errorf("proxy deletion transaction SQL executor is unavailable")
	}
	deleted, err := deleteProxyIfUnusedLocked(txCtx, exec, id)
	if err != nil {
		return err
	}
	if !deleted {
		// Preserve the existing idempotent no-op behavior without committing an
		// otherwise empty transaction; the deferred rollback releases it.
		return nil
	}
	return tx.Commit()
}

func deleteProxyIfUnusedLocked(ctx context.Context, exec sqlQueryExecutor, id int64) (bool, error) {
	var lockedID int64
	err := scanSingleRow(ctx, exec, `
		SELECT id
		FROM proxies
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, []any{id}, &lockedID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	accountCount, err := countLiveProxyAccounts(ctx, exec, id)
	if err != nil {
		return false, err
	}
	if accountCount > 0 {
		return false, service.ErrProxyInUse
	}
	backupReferenceCount, err := countLiveProxyBackupReferences(ctx, exec, id)
	if err != nil {
		return false, err
	}
	if backupReferenceCount > 0 {
		return false, service.ErrProxyBackupInUse
	}

	result, err := exec.ExecContext(ctx, `
		UPDATE proxies
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	if err != nil {
		return false, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if updated != 1 {
		return false, service.ErrProxyNotFound
	}
	return true, nil
}

func (r *proxyRepository) List(ctx context.Context, params pagination.PaginationParams) ([]service.Proxy, *pagination.PaginationResult, error) {
	return r.ListWithFilters(ctx, params, "", "", "")
}

// ListWithFilters lists proxies with optional filtering by protocol, status, and search query
func (r *proxyRepository) ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]service.Proxy, *pagination.PaginationResult, error) {
	q := r.client.Proxy.Query()
	if protocol != "" {
		q = q.Where(proxy.ProtocolEQ(protocol))
	}
	if status != "" {
		q = q.Where(proxy.StatusEQ(status))
	}
	if search != "" {
		q = q.Where(proxy.NameContainsFold(search))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	proxiesQuery := q.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range proxyListOrder(params) {
		proxiesQuery = proxiesQuery.Order(order)
	}

	proxies, err := proxiesQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	outProxies := make([]service.Proxy, 0, len(proxies))
	for i := range proxies {
		outProxies = append(outProxies, *proxyEntityToService(proxies[i]))
	}

	return outProxies, paginationResultFromTotal(int64(total), params), nil
}

// ListWithFiltersAndAccountCount lists proxies with filters and includes account count per proxy
func (r *proxyRepository) ListWithFiltersAndAccountCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]service.ProxyWithAccountCount, *pagination.PaginationResult, error) {
	q := r.client.Proxy.Query()
	if protocol != "" {
		q = q.Where(proxy.ProtocolEQ(protocol))
	}
	if status != "" {
		q = q.Where(proxy.StatusEQ(status))
	}
	if search != "" {
		q = q.Where(proxy.NameContainsFold(search))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	if strings.EqualFold(strings.TrimSpace(params.SortBy), "account_count") {
		return r.listWithAccountCountSort(ctx, q, params, total)
	}

	proxiesQuery := q.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range proxyListOrder(params) {
		proxiesQuery = proxiesQuery.Order(order)
	}

	proxies, err := proxiesQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	return r.buildProxyWithAccountCountResult(ctx, proxies, params, int64(total))
}

func (r *proxyRepository) listWithAccountCountSort(ctx context.Context, q *dbent.ProxyQuery, params pagination.PaginationParams, total int) ([]service.ProxyWithAccountCount, *pagination.PaginationResult, error) {
	proxies, err := q.
		Order(dbent.Desc(proxy.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	result, _, err := r.buildProxyWithAccountCountResult(ctx, proxies, params, int64(total))
	if err != nil {
		return nil, nil, err
	}

	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].AccountCount == result[j].AccountCount {
			return result[i].ID > result[j].ID
		}
		if sortOrder == pagination.SortOrderAsc {
			return result[i].AccountCount < result[j].AccountCount
		}
		return result[i].AccountCount > result[j].AccountCount
	})

	return paginateSlice(result, params), paginationResultFromTotal(int64(total), params), nil
}

func (r *proxyRepository) buildProxyWithAccountCountResult(ctx context.Context, proxies []*dbent.Proxy, params pagination.PaginationParams, total int64) ([]service.ProxyWithAccountCount, *pagination.PaginationResult, error) {
	counts, err := r.GetAccountCountsForProxies(ctx)
	if err != nil {
		return nil, nil, err
	}

	result := make([]service.ProxyWithAccountCount, 0, len(proxies))
	for i := range proxies {
		proxyOut := proxyEntityToService(proxies[i])
		if proxyOut == nil {
			continue
		}
		result = append(result, service.ProxyWithAccountCount{
			Proxy:        *proxyOut,
			AccountCount: counts[proxyOut.ID],
		})
	}
	if err := r.attachProxyOwnerInfo(ctx, result); err != nil {
		return nil, nil, err
	}

	return result, paginationResultFromTotal(total, params), nil
}

// attachProxyOwnerInfo 为专属代理批量填充归属用户的用户名与邮箱（管理端展示用）。
func (r *proxyRepository) attachProxyOwnerInfo(ctx context.Context, proxies []service.ProxyWithAccountCount) error {
	ownerIDs := make([]int64, 0, len(proxies))
	seen := make(map[int64]struct{}, len(proxies))
	for i := range proxies {
		if proxies[i].OwnerUserID == nil {
			continue
		}
		id := *proxies[i].OwnerUserID
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ownerIDs = append(ownerIDs, id)
	}
	if len(ownerIDs) == 0 {
		return nil
	}

	owners, err := r.client.User.Query().
		Where(user.IDIn(ownerIDs...)).
		Select(user.FieldID, user.FieldUsername, user.FieldEmail).
		All(ctx)
	if err != nil {
		return err
	}
	byID := make(map[int64]*dbent.User, len(owners))
	for i := range owners {
		byID[owners[i].ID] = owners[i]
	}
	for i := range proxies {
		if proxies[i].OwnerUserID == nil {
			continue
		}
		if owner, ok := byID[*proxies[i].OwnerUserID]; ok {
			proxies[i].OwnerUsername = owner.Username
			proxies[i].OwnerEmail = owner.Email
		}
	}
	return nil
}

func proxyListOrder(params pagination.PaginationParams) []func(*entsql.Selector) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)

	var field string
	switch sortBy {
	case "name":
		field = proxy.FieldName
	case "protocol":
		field = proxy.FieldProtocol
	case "status":
		field = proxy.FieldStatus
	case "created_at":
		field = proxy.FieldCreatedAt
	default:
		field = proxy.FieldID
	}

	if sortOrder == pagination.SortOrderAsc {
		return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(proxy.FieldID)}
	}
	return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(proxy.FieldID)}
}

func (r *proxyRepository) ListActive(ctx context.Context) ([]service.Proxy, error) {
	proxies, err := r.client.Proxy.Query().
		Where(proxy.StatusEQ(service.StatusActive)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	outProxies := make([]service.Proxy, 0, len(proxies))
	for i := range proxies {
		outProxies = append(outProxies, *proxyEntityToService(proxies[i]))
	}
	return outProxies, nil
}

func (r *proxyRepository) ListActiveVisibleWithAccountCount(ctx context.Context, scope service.ProxyScope) ([]service.ProxyWithAccountCount, error) {
	proxies, err := r.client.Proxy.Query().
		Where(proxy.StatusEQ(service.StatusActive), visibleProxyPredicate(scope)).
		Order(dbent.Desc(proxy.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	counts, err := r.GetAccountCountsForProxies(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]service.ProxyWithAccountCount, 0, len(proxies))
	for i := range proxies {
		proxyOut := proxyEntityToService(proxies[i])
		if proxyOut == nil {
			continue
		}
		result = append(result, service.ProxyWithAccountCount{
			Proxy:        *proxyOut,
			AccountCount: counts[proxyOut.ID],
		})
	}
	return result, nil
}

func (r *proxyRepository) GetVisibleByID(ctx context.Context, scope service.ProxyScope, id int64) (*service.Proxy, error) {
	m, err := r.client.Proxy.Query().
		Where(proxy.IDEQ(id), visibleProxyPredicate(scope)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrProxyNotFound
		}
		return nil, err
	}
	return proxyEntityToService(m), nil
}

func (r *proxyRepository) FindVisibleActiveByEndpoint(ctx context.Context, scope service.ProxyScope, protocol, host string, port int, username, password string) (*service.Proxy, error) {
	q := r.client.Proxy.Query().
		Where(
			proxy.StatusEQ(service.StatusActive),
			visibleProxyPredicate(scope),
			proxy.ProtocolEQ(protocol),
			proxy.HostEQ(host),
			proxy.PortEQ(port),
		)

	if username == "" {
		q = q.Where(proxy.Or(proxy.UsernameIsNil(), proxy.UsernameEQ("")))
	} else {
		q = q.Where(proxy.UsernameEQ(username))
	}
	if password == "" {
		q = q.Where(proxy.Or(proxy.PasswordIsNil(), proxy.PasswordEQ("")))
	} else {
		q = q.Where(proxy.PasswordEQ(password))
	}

	m, err := q.Order(dbent.Desc(proxy.FieldID)).First(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrProxyNotFound
		}
		return nil, err
	}
	return proxyEntityToService(m), nil
}

// visibleProxyPredicate 按“账号平台 + 账号等级”筛选可用代理。
// 平台代理（owner_user_id IS NULL）按平台/等级过滤，为空分别表示通用代理与所有等级可用。
//
// 专属代理（owner_user_id 非空，来源为管理员指派或迁移 256 保留的历史自有代理）
// 仅当 scope.OwnerUserID 与其归属一致时放行，且不受平台/等级过滤限制。
func visibleProxyPredicate(scope service.ProxyScope) predicate.Proxy {
	normalized := scope.Normalized()

	platformPreds := []predicate.Proxy{proxy.OwnerUserIDIsNil()}
	if normalized.Platform == "" {
		platformPreds = append(platformPreds, proxy.PlatformEQ(""))
	} else {
		platformPreds = append(platformPreds, proxy.Or(proxy.PlatformEQ(""), proxy.PlatformEQ(normalized.Platform)))
	}
	if normalized.AccountLevel == "" {
		platformPreds = append(platformPreds, proxy.RequiredAccountLevelEQ(""))
	} else {
		platformPreds = append(platformPreds, proxy.Or(
			proxy.RequiredAccountLevelEQ(""),
			proxy.RequiredAccountLevelEQ(normalized.AccountLevel),
		))
	}
	platformProxy := proxy.And(platformPreds...)

	if normalized.OwnerUserID <= 0 {
		return platformProxy
	}
	// 平台代理 或 归属该用户的专属代理。
	return proxy.Or(platformProxy, proxy.OwnerUserIDEQ(normalized.OwnerUserID))
}

// ResetRequiredAccountLevelNotIn 将 required_account_level 落在 keepLevels 之外的代理
// 重置为 ”（所有等级可用）。” 本身始终保留。用于账号等级被删除后同步代理，
// 避免代理被永久绑死在一个已不存在的等级上而对所有账号不可见。
func (r *proxyRepository) ResetRequiredAccountLevelNotIn(ctx context.Context, keepLevels []string) (int64, error) {
	keep := make([]string, 0, len(keepLevels)+1)
	seen := map[string]struct{}{"": {}}
	keep = append(keep, "")
	for _, level := range keepLevels {
		normalized := service.NormalizeRequiredAccountLevel(level)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		keep = append(keep, normalized)
	}

	predicates := []predicate.Proxy{
		proxy.RequiredAccountLevelNEQ(""),
		proxy.RequiredAccountLevelNotIn(keep...),
	}
	affected, err := r.client.Proxy.Update().
		Where(proxy.And(predicates...)).
		SetRequiredAccountLevel("").
		Save(ctx)
	if err != nil {
		return 0, err
	}
	return int64(affected), nil
}

// ExistsByHostPortAuth checks if a proxy with the same host, port, username, and password exists
func (r *proxyRepository) ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error) {
	q := r.client.Proxy.Query().
		Where(proxy.HostEQ(host), proxy.PortEQ(port))

	if username == "" {
		q = q.Where(proxy.Or(proxy.UsernameIsNil(), proxy.UsernameEQ("")))
	} else {
		q = q.Where(proxy.UsernameEQ(username))
	}
	if password == "" {
		q = q.Where(proxy.Or(proxy.PasswordIsNil(), proxy.PasswordEQ("")))
	} else {
		q = q.Where(proxy.PasswordEQ(password))
	}

	count, err := q.Count(ctx)
	return count > 0, err
}

// CountAccountsByProxyID returns the number of accounts using a specific proxy
func (r *proxyRepository) CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error) {
	var count int64
	if err := scanSingleRow(ctx, r.sql, "SELECT COUNT(*) FROM accounts WHERE proxy_id = $1 AND deleted_at IS NULL", []any{proxyID}, &count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *proxyRepository) ListAccountSummariesByProxyID(ctx context.Context, proxyID int64) ([]service.ProxyAccountSummary, error) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, name, platform, type, notes
		FROM accounts
		WHERE proxy_id = $1 AND deleted_at IS NULL
		ORDER BY id DESC
	`, proxyID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.ProxyAccountSummary, 0)
	for rows.Next() {
		var (
			id       int64
			name     string
			platform string
			accType  string
			notes    sql.NullString
		)
		if err := rows.Scan(&id, &name, &platform, &accType, &notes); err != nil {
			return nil, err
		}
		var notesPtr *string
		if notes.Valid {
			notesPtr = &notes.String
		}
		out = append(out, service.ProxyAccountSummary{
			ID:       id,
			Name:     name,
			Platform: platform,
			Type:     accType,
			Notes:    notesPtr,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAccountCountsForProxies returns a map of proxy ID to account count for all proxies
func (r *proxyRepository) GetAccountCountsForProxies(ctx context.Context) (counts map[int64]int64, err error) {
	rows, err := r.sql.QueryContext(ctx, "SELECT proxy_id, COUNT(*) AS count FROM accounts WHERE proxy_id IS NOT NULL AND deleted_at IS NULL GROUP BY proxy_id")
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			counts = nil
		}
	}()

	counts = make(map[int64]int64)
	for rows.Next() {
		var proxyID, count int64
		if err = rows.Scan(&proxyID, &count); err != nil {
			return nil, err
		}
		counts[proxyID] = count
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

// ListActiveWithAccountCount returns all active proxies with account count, sorted by creation time descending
func (r *proxyRepository) ListActiveWithAccountCount(ctx context.Context) ([]service.ProxyWithAccountCount, error) {
	proxies, err := r.client.Proxy.Query().
		Where(proxy.StatusEQ(service.StatusActive)).
		Order(dbent.Desc(proxy.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	// Get account counts
	counts, err := r.GetAccountCountsForProxies(ctx)
	if err != nil {
		return nil, err
	}

	// Build result with account counts
	result := make([]service.ProxyWithAccountCount, 0, len(proxies))
	for i := range proxies {
		proxyOut := proxyEntityToService(proxies[i])
		if proxyOut == nil {
			continue
		}
		result = append(result, service.ProxyWithAccountCount{
			Proxy:        *proxyOut,
			AccountCount: counts[proxyOut.ID],
		})
	}
	if err := r.attachProxyOwnerInfo(ctx, result); err != nil {
		return nil, err
	}

	return result, nil
}

func proxyEntityToService(m *dbent.Proxy) *service.Proxy {
	if m == nil {
		return nil
	}
	out := &service.Proxy{
		ID:                   m.ID,
		Name:                 m.Name,
		Protocol:             m.Protocol,
		Host:                 m.Host,
		Port:                 m.Port,
		OwnerUserID:          m.OwnerUserID,
		Platform:             m.Platform,
		RequiredAccountLevel: m.RequiredAccountLevel,
		Status:               m.Status,
		MaxAccounts:          m.MaxAccounts,
		ExpiresAt:            m.ExpiresAt,
		FallbackMode:         m.FallbackMode,
		BackupProxyID:        m.BackupProxyID,
		ExpiryWarnDays:       m.ExpiryWarnDays,
		CreatedAt:            m.CreatedAt,
		UpdatedAt:            m.UpdatedAt,
	}
	if m.Username != nil {
		out.Username = *m.Username
	}
	if m.Password != nil {
		out.Password = *m.Password
	}
	return out
}

func applyProxyEntityToService(dst *service.Proxy, src *dbent.Proxy) {
	if dst == nil || src == nil {
		return
	}
	dst.ID = src.ID
	dst.OwnerUserID = src.OwnerUserID
	dst.Platform = src.Platform
	dst.RequiredAccountLevel = src.RequiredAccountLevel
	dst.MaxAccounts = src.MaxAccounts
	dst.ExpiresAt = src.ExpiresAt
	dst.FallbackMode = src.FallbackMode
	dst.BackupProxyID = src.BackupProxyID
	dst.ExpiryWarnDays = src.ExpiryWarnDays
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}

// ListAllForFallback 返回所有未软删除代理，供 fallback 链解析和导出闭包使用。
func (r *proxyRepository) ListAllForFallback(ctx context.Context) ([]service.Proxy, error) {
	rows, err := r.client.Proxy.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.Proxy, 0, len(rows))
	for _, row := range rows {
		if item := proxyEntityToService(row); item != nil {
			out = append(out, *item)
		}
	}
	return out, nil
}

var errProxyExpiryPlanChanged = errors.New("proxy expiry fallback plan changed while acquiring locks")

const maxProxyExpiryPlanRetries = 3

// SweepExpiredProxies 逐个事务处理到期代理。每个事务先无锁发现完整 fallback 链，
// 再把 source 与链上代理按 ID 升序统一锁定；账号锁只能出现在全部代理锁之后。
// 锁后计划漂移时重新预览，禁止在已持有较大 ID 锁后动态追加较小 ID 锁。
func (r *proxyRepository) SweepExpiredProxies(ctx context.Context, now time.Time) (int64, error) {
	var total int64
	planRetries := 0
	for {
		changed, processed, err := r.sweepNextExpiredProxy(ctx, now)
		if errors.Is(err, errProxyExpiryPlanChanged) {
			planRetries++
			if planRetries >= maxProxyExpiryPlanRetries {
				return total, err
			}
			continue
		}
		if err != nil {
			return total, err
		}
		if !processed {
			return total, nil
		}
		planRetries = 0
		total += changed
	}
}

func (r *proxyRepository) sweepNextExpiredProxy(ctx context.Context, now time.Time) (int64, bool, error) {
	proxyID, found, err := r.nextExpiredProxyID(ctx, now)
	if err != nil || !found {
		return 0, false, err
	}

	preview, err := loadProxyExpirySnapshot(ctx, r.sql)
	if err != nil {
		return 0, false, err
	}
	previewStart, ok := preview[proxyID]
	if !ok || !isProxyExpirySweepCandidate(previewStart, now) {
		return 0, true, nil
	}
	previewPlan := buildProxyExpiryLockPlan(previewStart, preview, now)

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	exec := sqlExecutorFromEntClient(tx.Client())
	if exec == nil {
		return 0, false, fmt.Errorf("proxy expiry transaction SQL executor is unavailable")
	}

	locked, err := lockProxyExpiryPlan(txCtx, exec, previewPlan.proxyIDs)
	if err != nil {
		return 0, false, err
	}
	if !proxyExpirySnapshotContainsIDs(locked, previewPlan.proxyIDs) {
		// 代理正被 mutation guard 或另一个 worker 使用，当前轮不等待、不持有部分锁。
		return 0, false, nil
	}

	current, err := loadProxyExpirySnapshot(txCtx, exec)
	if err != nil {
		return 0, false, err
	}
	start, ok := current[proxyID]
	if !ok || !isProxyExpirySweepCandidate(start, now) {
		// 另一个事务已完成处理，或管理员在锁获取前取消了到期条件。
		return 0, true, nil
	}
	currentPlan := buildProxyExpiryLockPlan(start, current, now)
	if !previewPlan.equal(currentPlan) {
		return 0, false, errProxyExpiryPlanChanged
	}

	result, err := exec.ExecContext(txCtx, `
		UPDATE proxies SET status=$1, updated_at=NOW()
		WHERE id=$2 AND deleted_at IS NULL AND status=$3
			AND expires_at IS NOT NULL AND expires_at <= $4
	`, service.StatusExpired, proxyID, service.StatusActive, now)
	if err != nil {
		return 0, false, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if updated == 0 {
		return 0, false, errProxyExpiryPlanChanged
	}

	changedIDs := make([]int64, 0)
	if currentPlan.change {
		accounts, lockErr := tx.Client().Account.Query().
			Where(dbaccount.ProxyIDEQ(proxyID), dbaccount.ProxyFallbackOriginIDIsNil()).
			Order(dbent.Asc(dbaccount.FieldID)).
			ForUpdate().
			All(txCtx)
		if lockErr != nil {
			return 0, false, lockErr
		}
		if currentPlan.targetID == nil {
			changedIDs, err = rerouteAccountsToDirect(txCtx, exec, proxyID)
		} else {
			target, targetExists := current[*currentPlan.targetID]
			if !targetExists {
				return 0, false, errProxyExpiryPlanChanged
			}
			var currentBindings int64
			if countErr := scanSingleRow(txCtx, exec, `
				SELECT COUNT(*)
				FROM accounts
				WHERE proxy_id = $1 AND deleted_at IS NULL
			`, []any{target.ID}, &currentBindings); countErr != nil {
				return 0, false, countErr
			}
			changedIDs, err = rerouteLockedAccountsToBackup(
				txCtx,
				exec,
				accounts,
				proxyID,
				target,
				currentBindings,
				now,
			)
		}
	}
	if err != nil {
		return 0, false, err
	}
	changedIDs = sortedUniqueProxyExpiryAccountIDs(changedIDs)
	if len(changedIDs) > 0 {
		payload := map[string]any{"account_ids": changedIDs}
		if err := enqueueSchedulerOutbox(txCtx, exec, service.SchedulerOutboxEventAccountBulkChanged, nil, nil, payload); err != nil {
			return 0, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return int64(len(changedIDs)), true, nil
}

func (r *proxyRepository) nextExpiredProxyID(ctx context.Context, now time.Time) (int64, bool, error) {
	if r.sql == nil {
		return 0, false, fmt.Errorf("proxy expiry SQL executor is unavailable")
	}
	var proxyID int64
	err := scanSingleRow(ctx, r.sql, `
		SELECT id
		FROM proxies
		WHERE deleted_at IS NULL AND status=$1
			AND expires_at IS NOT NULL AND expires_at <= $2
		ORDER BY expires_at ASC, id ASC
		LIMIT 1
	`, []any{service.StatusActive, now}, &proxyID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return proxyID, err == nil, err
}

type proxyExpiryLockPlan struct {
	sourceID int64
	path     []int64
	proxyIDs []int64
	targetID *int64
	change   bool
}

func buildProxyExpiryLockPlan(
	start service.Proxy,
	byID map[int64]service.Proxy,
	now time.Time,
) proxyExpiryLockPlan {
	plan := proxyExpiryLockPlan{
		sourceID: start.ID,
		path:     []int64{start.ID},
		proxyIDs: []int64{start.ID},
	}
	seen := map[int64]struct{}{start.ID: {}}
	current := start
	for current.FallbackMode == service.FallbackModeProxy && current.BackupProxyID != nil {
		nextID := *current.BackupProxyID
		plan.path = append(plan.path, nextID)
		if _, duplicate := seen[nextID]; duplicate {
			break
		}
		seen[nextID] = struct{}{}
		next, exists := byID[nextID]
		if !exists {
			break
		}
		plan.proxyIDs = append(plan.proxyIDs, nextID)
		if next.Status == service.StatusActive && !next.IsExpired(now) {
			break
		}
		current = next
	}
	plan.proxyIDs = sortedUniqueProxyExpiryAccountIDs(plan.proxyIDs)
	plan.targetID, plan.change = service.ResolveProxyFallbackTarget(start, byID, now)
	return plan
}

func (p proxyExpiryLockPlan) equal(other proxyExpiryLockPlan) bool {
	if p.sourceID != other.sourceID || p.change != other.change ||
		!sameOptionalInt64(p.targetID, other.targetID) {
		return false
	}
	return equalInt64Slices(p.path, other.path) && equalInt64Slices(p.proxyIDs, other.proxyIDs)
}

func sameOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func equalInt64Slices(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func isProxyExpirySweepCandidate(proxy service.Proxy, now time.Time) bool {
	return proxy.Status == service.StatusActive && proxy.ExpiresAt != nil && !proxy.ExpiresAt.After(now)
}

func loadProxyExpirySnapshot(ctx context.Context, queryer sqlQuerier) (map[int64]service.Proxy, error) {
	if queryer == nil {
		return nil, fmt.Errorf("proxy expiry SQL executor is unavailable")
	}
	rows, err := queryer.QueryContext(ctx, `
		SELECT id, owner_user_id, platform, required_account_level, status,
			max_accounts, expires_at, fallback_mode, backup_proxy_id
		FROM proxies
		WHERE deleted_at IS NULL
	`)
	if err != nil {
		return nil, err
	}
	return scanProxyExpirySnapshot(rows)
}

func lockProxyExpiryPlan(
	ctx context.Context,
	exec sqlExecutor,
	proxyIDs []int64,
) (map[int64]service.Proxy, error) {
	if len(proxyIDs) == 0 {
		return map[int64]service.Proxy{}, nil
	}
	rows, err := exec.QueryContext(ctx, `
		SELECT id, owner_user_id, platform, required_account_level, status,
			max_accounts, expires_at, fallback_mode, backup_proxy_id
		FROM proxies
		WHERE deleted_at IS NULL AND id = ANY($1)
		ORDER BY id ASC
		FOR UPDATE SKIP LOCKED
	`, pq.Array(proxyIDs))
	if err != nil {
		return nil, err
	}
	return scanProxyExpirySnapshot(rows)
}

func scanProxyExpirySnapshot(rows *sql.Rows) (map[int64]service.Proxy, error) {
	if rows == nil {
		return map[int64]service.Proxy{}, nil
	}
	defer func() { _ = rows.Close() }()
	result := make(map[int64]service.Proxy)
	for rows.Next() {
		var (
			item      service.Proxy
			ownerID   sql.NullInt64
			expiresAt sql.NullTime
			backupID  sql.NullInt64
		)
		if err := rows.Scan(
			&item.ID,
			&ownerID,
			&item.Platform,
			&item.RequiredAccountLevel,
			&item.Status,
			&item.MaxAccounts,
			&expiresAt,
			&item.FallbackMode,
			&backupID,
		); err != nil {
			return nil, err
		}
		if ownerID.Valid {
			value := ownerID.Int64
			item.OwnerUserID = &value
		}
		if expiresAt.Valid {
			value := expiresAt.Time
			item.ExpiresAt = &value
		}
		if backupID.Valid {
			value := backupID.Int64
			item.BackupProxyID = &value
		}
		result[item.ID] = item
	}
	return result, rows.Err()
}

func proxyExpirySnapshotContainsIDs(snapshot map[int64]service.Proxy, ids []int64) bool {
	if len(snapshot) != len(ids) {
		return false
	}
	for _, id := range ids {
		if _, exists := snapshot[id]; !exists {
			return false
		}
	}
	return true
}

func rerouteAccountsToDirect(ctx context.Context, exec sqlExecutor, proxyID int64) ([]int64, error) {
	rows, err := exec.QueryContext(ctx, `
		UPDATE accounts
		SET proxy_id=NULL, proxy_fallback_origin_id=$1,
			extra=CASE WHEN type='apikey' AND extra ? 'upstream_billing_probe'
				THEN extra - 'upstream_billing_probe' ELSE extra END,
			updated_at=NOW()
		WHERE proxy_id=$1 AND proxy_fallback_origin_id IS NULL AND deleted_at IS NULL
		RETURNING id
	`, proxyID)
	if err != nil {
		return nil, err
	}
	return scanProxyExpiryAccountIDs(rows)
}

func rerouteLockedAccountsToBackup(
	ctx context.Context,
	exec sqlExecutor,
	accounts []*dbent.Account,
	sourceProxyID int64,
	target service.Proxy,
	currentBindings int64,
	now time.Time,
) ([]int64, error) {
	changed := make([]int64, 0, len(accounts))
	for _, row := range accounts {
		account := accountEntityToService(row)
		if account == nil || !service.CanAccountUseProxyFallback(target, *account, currentBindings, now) {
			continue
		}
		rows, updateErr := exec.QueryContext(ctx, `
			UPDATE accounts
			SET proxy_id=$2, proxy_fallback_origin_id=$1,
				extra=CASE WHEN type='apikey' AND extra ? 'upstream_billing_probe'
					THEN extra - 'upstream_billing_probe' ELSE extra END,
				updated_at=NOW()
			WHERE id=$3 AND proxy_id=$1 AND proxy_fallback_origin_id IS NULL AND deleted_at IS NULL
			RETURNING id
		`, sourceProxyID, target.ID, row.ID)
		if updateErr != nil {
			return nil, updateErr
		}
		ids, scanErr := scanProxyExpiryAccountIDs(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if len(ids) == 1 {
			changed = append(changed, ids[0])
			currentBindings++
		}
	}
	return changed, nil
}

func scanProxyExpiryAccountIDs(rows *sql.Rows) ([]int64, error) {
	if rows == nil {
		return nil, nil
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func sortedUniqueProxyExpiryAccountIDs(ids []int64) []int64 {
	if len(ids) < 2 {
		return ids
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	write := 1
	for _, id := range ids[1:] {
		if id == ids[write-1] {
			continue
		}
		ids[write] = id
		write++
	}
	return ids[:write]
}

func (r *proxyRepository) CountExpired(ctx context.Context) (int64, error) {
	var count int64
	err := scanSingleRow(ctx, r.sql, `
		SELECT COUNT(*) FROM proxies
		WHERE deleted_at IS NULL
			AND (status=$1 OR (expires_at IS NOT NULL AND expires_at <= NOW()))
	`, []any{service.StatusExpired}, &count)
	return count, err
}

func (r *proxyRepository) CountExpiringSoon(ctx context.Context, now time.Time) (int64, error) {
	var count int64
	err := scanSingleRow(ctx, r.sql, `
		SELECT COUNT(*) FROM proxies
		WHERE deleted_at IS NULL AND status=$1 AND expires_at IS NOT NULL
			AND expires_at > $2
			AND expires_at <= $2 + (expiry_warn_days * INTERVAL '1 day')
	`, []any{service.StatusActive, now}, &count)
	return count, err
}
