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

func NewProxyRepository(client *dbent.Client, sqlDB *sql.DB) service.ProxyRepository {
	return newProxyRepositoryWithSQL(client, sqlDB)
}

func newProxyRepositoryWithSQL(client *dbent.Client, sqlq sqlQuerier) *proxyRepository {
	return &proxyRepository{client: client, sql: sqlq}
}

func (r *proxyRepository) Create(ctx context.Context, proxyIn *service.Proxy) error {
	builder := r.client.Proxy.Create().
		SetName(proxyIn.Name).
		SetProtocol(proxyIn.Protocol).
		SetHost(proxyIn.Host).
		SetPort(proxyIn.Port).
		SetStatus(proxyIn.Status).
		SetMaxAccounts(proxyIn.MaxAccounts).
		SetNillableExpiresAt(proxyIn.ExpiresAt).
		SetFallbackMode(service.NormalizeProxyFallbackMode(proxyIn.FallbackMode)).
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

func (r *proxyRepository) GetByID(ctx context.Context, id int64) (*service.Proxy, error) {
	m, err := r.client.Proxy.Get(ctx, id)
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
	return r.updateWithClient(ctx, r.client, proxyIn)
}

// UpdateWithOwnerAssignment 在同一事务内锁定代理行、校验没有其他用户的账号绑定在该代理上，
// 然后保存代理。行锁与用户建号路径（ensureOwnedProxyCapacityForCreateInTx）互斥，
// 使"改归属"与"绑账号"无法交叉出「他人账号绑在专属代理上」的状态——那种状态下账号会在
// 用户端重新鉴权时因代理不可见被拒。
func (r *proxyRepository) UpdateWithOwnerAssignment(ctx context.Context, proxyIn *service.Proxy) error {
	if proxyIn == nil {
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

	var lockedID int64
	if err := scanSingleRow(txCtx, exec, `
		SELECT id
		FROM proxies
		WHERE id = $1
			AND deleted_at IS NULL
		FOR UPDATE
	`, []any{proxyIn.ID}, &lockedID); errors.Is(err, sql.ErrNoRows) {
		return service.ErrProxyNotFound
	} else if err != nil {
		return err
	}

	if proxyIn.OwnerUserID != nil && *proxyIn.OwnerUserID > 0 {
		var boundToOthers int64
		if err := scanSingleRow(txCtx, exec, `
			SELECT COUNT(*)
			FROM accounts
			WHERE proxy_id = $1
				AND deleted_at IS NULL
				AND owner_user_id IS NOT NULL
				AND owner_user_id <> $2
		`, []any{proxyIn.ID, *proxyIn.OwnerUserID}, &boundToOthers); err != nil {
			return err
		}
		if boundToOthers > 0 {
			return service.ErrProxyOwnerConflict
		}
	}

	if err := r.updateWithClient(txCtx, tx.Client(), proxyIn); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *proxyRepository) updateWithClient(ctx context.Context, client *dbent.Client, proxyIn *service.Proxy) error {
	builder := client.Proxy.UpdateOneID(proxyIn.ID).
		SetName(proxyIn.Name).
		SetProtocol(proxyIn.Protocol).
		SetHost(proxyIn.Host).
		SetPort(proxyIn.Port).
		SetStatus(proxyIn.Status).
		SetMaxAccounts(proxyIn.MaxAccounts).
		SetFallbackMode(service.NormalizeProxyFallbackMode(proxyIn.FallbackMode)).
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
		return service.ErrProxyNotFound
	}
	return err
}

func (r *proxyRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.client.Proxy.Delete().Where(proxy.IDEQ(id)).Exec(ctx)
	return err
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

// SweepExpiredProxies 逐个事务处理到期代理。FOR UPDATE SKIP LOCKED 让多实例并行时
// 同一代理只由一个 worker 处理；账号更新继续使用旧 proxy_id 谓词，避免覆盖管理员新选择。
func (r *proxyRepository) SweepExpiredProxies(ctx context.Context, now time.Time) (int64, error) {
	var total int64
	for {
		changed, processed, err := r.sweepNextExpiredProxy(ctx, now)
		if err != nil {
			return total, err
		}
		if !processed {
			return total, nil
		}
		total += changed
	}
}

func (r *proxyRepository) sweepNextExpiredProxy(ctx context.Context, now time.Time) (int64, bool, error) {
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

	var proxyID int64
	err = scanSingleRow(txCtx, exec, `
		SELECT id
		FROM proxies
		WHERE deleted_at IS NULL AND status=$1
			AND expires_at IS NOT NULL AND expires_at <= $2
		ORDER BY expires_at ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, []any{service.StatusActive, now}, &proxyID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	allRows, err := tx.Client().Proxy.Query().All(txCtx)
	if err != nil {
		return 0, false, err
	}
	byID := make(map[int64]service.Proxy, len(allRows))
	for _, row := range allRows {
		item := proxyEntityToService(row)
		byID[item.ID] = *item
	}
	start, ok := byID[proxyID]
	if !ok {
		return 0, false, service.ErrProxyNotFound
	}
	targetID, change := service.ResolveProxyFallbackTarget(start, byID, now)

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
		return 0, false, nil
	}

	changedIDs := make([]int64, 0)
	if change && targetID == nil {
		changedIDs, err = rerouteAccountsToDirect(txCtx, exec, proxyID)
	} else if change {
		changedIDs, err = r.rerouteAccountsToBackup(txCtx, tx.Client(), exec, proxyID, *targetID, now)
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

func (r *proxyRepository) rerouteAccountsToBackup(
	ctx context.Context,
	client *dbent.Client,
	exec sqlExecutor,
	sourceProxyID int64,
	targetProxyID int64,
	now time.Time,
) ([]int64, error) {
	targetRow, err := client.Proxy.Query().Where(proxy.IDEQ(targetProxyID)).ForUpdate().Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	target := proxyEntityToService(targetRow)
	if target == nil || target.Status != service.StatusActive || target.IsExpired(now) {
		return nil, nil
	}

	accounts, err := client.Account.Query().
		Where(dbaccount.ProxyIDEQ(sourceProxyID), dbaccount.ProxyFallbackOriginIDIsNil()).
		ForUpdate().
		All(ctx)
	if err != nil {
		return nil, err
	}
	currentBindings, err := client.Account.Query().Where(dbaccount.ProxyIDEQ(targetProxyID)).Count(ctx)
	if err != nil {
		return nil, err
	}

	changed := make([]int64, 0, len(accounts))
	for _, row := range accounts {
		account := accountEntityToService(row)
		if account == nil || !service.CanAccountUseProxyFallback(*target, *account, int64(currentBindings), now) {
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
		`, sourceProxyID, targetProxyID, row.ID)
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
