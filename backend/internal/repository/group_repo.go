package repository

import (
	"context"
	"database/sql"
	"errors"
	"log"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type groupRepository struct {
	client *dbent.Client
	sql    sqlExecutor
}

func NewGroupRepository(client *dbent.Client, sqlDB *sql.DB) service.GroupRepository {
	return newGroupRepositoryWithSQL(client, sqlDB)
}

func newGroupRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor) *groupRepository {
	return &groupRepository{client: client, sql: sqlq}
}

func (r *groupRepository) Create(ctx context.Context, groupIn *service.Group) error {
	builder := r.client.Group.Create().
		SetName(groupIn.Name).
		SetDescription(groupIn.Description).
		SetPlatform(groupIn.Platform).
		SetRateMultiplier(groupIn.RateMultiplier).
		SetIsExclusive(groupIn.IsExclusive).
		SetStatus(groupIn.Status).
		SetSubscriptionType(groupIn.SubscriptionType).
		SetNillableDailyLimitUsd(groupIn.DailyLimitUSD).
		SetNillableWeeklyLimitUsd(groupIn.WeeklyLimitUSD).
		SetNillableMonthlyLimitUsd(groupIn.MonthlyLimitUSD).
		SetNillableImagePrice1k(groupIn.ImagePrice1K).
		SetNillableImagePrice2k(groupIn.ImagePrice2K).
		SetNillableImagePrice4k(groupIn.ImagePrice4K).
		SetDefaultValidityDays(groupIn.DefaultValidityDays).
		SetClaudeCodeOnly(groupIn.ClaudeCodeOnly).
		SetNillableFallbackGroupID(groupIn.FallbackGroupID).
		SetModelRoutingEnabled(groupIn.ModelRoutingEnabled)

	// 设置模型路由配置
	if groupIn.ModelRouting != nil {
		builder = builder.SetModelRouting(groupIn.ModelRouting)
	}

	created, err := builder.Save(ctx)
	if err == nil {
		groupIn.ID = created.ID
		groupIn.CreatedAt = created.CreatedAt
		groupIn.UpdatedAt = created.UpdatedAt
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventGroupChanged, nil, &groupIn.ID, nil); err != nil {
			log.Printf("[SchedulerOutbox] enqueue group create failed: group=%d err=%v", groupIn.ID, err)
		}
	}
	return translatePersistenceError(err, nil, service.ErrGroupExists)
}

func (r *groupRepository) GetByID(ctx context.Context, id int64) (*service.Group, error) {
	out, err := r.GetByIDLite(ctx, id)
	if err != nil {
		return nil, err
	}
	count, _ := r.GetAccountCount(ctx, out.ID)
	out.AccountCount = count
	return out, nil
}

func (r *groupRepository) GetByIDLite(ctx context.Context, id int64) (*service.Group, error) {
	// AccountCount is intentionally not loaded here; use GetByID when needed.
	m, err := r.client.Group.Query().
		Where(group.IDEQ(id)).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrGroupNotFound, nil)
	}

	return groupEntityToService(m), nil
}

func (r *groupRepository) Update(ctx context.Context, groupIn *service.Group) error {
	builder := r.client.Group.UpdateOneID(groupIn.ID).
		SetName(groupIn.Name).
		SetDescription(groupIn.Description).
		SetPlatform(groupIn.Platform).
		SetRateMultiplier(groupIn.RateMultiplier).
		SetIsExclusive(groupIn.IsExclusive).
		SetStatus(groupIn.Status).
		SetSubscriptionType(groupIn.SubscriptionType).
		SetNillableDailyLimitUsd(groupIn.DailyLimitUSD).
		SetNillableWeeklyLimitUsd(groupIn.WeeklyLimitUSD).
		SetNillableMonthlyLimitUsd(groupIn.MonthlyLimitUSD).
		SetNillableImagePrice1k(groupIn.ImagePrice1K).
		SetNillableImagePrice2k(groupIn.ImagePrice2K).
		SetNillableImagePrice4k(groupIn.ImagePrice4K).
		SetDefaultValidityDays(groupIn.DefaultValidityDays).
		SetClaudeCodeOnly(groupIn.ClaudeCodeOnly).
		SetModelRoutingEnabled(groupIn.ModelRoutingEnabled)

	// 处理 FallbackGroupID：nil 时清除，否则设置
	if groupIn.FallbackGroupID != nil {
		builder = builder.SetFallbackGroupID(*groupIn.FallbackGroupID)
	} else {
		builder = builder.ClearFallbackGroupID()
	}

	// 处理 ModelRouting：nil 时清除，否则设置
	if groupIn.ModelRouting != nil {
		builder = builder.SetModelRouting(groupIn.ModelRouting)
	} else {
		builder = builder.ClearModelRouting()
	}

	updated, err := builder.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrGroupNotFound, service.ErrGroupExists)
	}
	groupIn.UpdatedAt = updated.UpdatedAt
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventGroupChanged, nil, &groupIn.ID, nil); err != nil {
		log.Printf("[SchedulerOutbox] enqueue group update failed: group=%d err=%v", groupIn.ID, err)
	}
	return nil
}

func (r *groupRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.client.Group.Delete().Where(group.IDEQ(id)).Exec(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrGroupNotFound, nil)
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventGroupChanged, nil, &id, nil); err != nil {
		log.Printf("[SchedulerOutbox] enqueue group delete failed: group=%d err=%v", id, err)
	}
	return nil
}

func (r *groupRepository) List(ctx context.Context, params pagination.PaginationParams) ([]service.Group, *pagination.PaginationResult, error) {
	return r.ListWithFilters(ctx, params, "", "", "", nil)
}

func (r *groupRepository) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool) ([]service.Group, *pagination.PaginationResult, error) {
	q := r.client.Group.Query()

	if platform != "" {
		q = q.Where(group.PlatformEQ(platform))
	}
	if status != "" {
		q = q.Where(group.StatusEQ(status))
	}
	if search != "" {
		q = q.Where(group.Or(
			group.NameContainsFold(search),
			group.DescriptionContainsFold(search),
		))
	}
	if isExclusive != nil {
		q = q.Where(group.IsExclusiveEQ(*isExclusive))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	groups, err := q.
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(dbent.Asc(group.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	groupIDs := make([]int64, 0, len(groups))
	outGroups := make([]service.Group, 0, len(groups))
	for i := range groups {
		g := groupEntityToService(groups[i])
		outGroups = append(outGroups, *g)
		groupIDs = append(groupIDs, g.ID)
	}

	counts, err := r.loadAccountCounts(ctx, groupIDs)
	if err == nil {
		for i := range outGroups {
			outGroups[i].AccountCount = counts[outGroups[i].ID]
		}
	}

	return outGroups, paginationResultFromTotal(int64(total), params), nil
}

func (r *groupRepository) ListActive(ctx context.Context) ([]service.Group, error) {
	groups, err := r.client.Group.Query().
		Where(group.StatusEQ(service.StatusActive)).
		Order(dbent.Asc(group.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	groupIDs := make([]int64, 0, len(groups))
	outGroups := make([]service.Group, 0, len(groups))
	for i := range groups {
		g := groupEntityToService(groups[i])
		outGroups = append(outGroups, *g)
		groupIDs = append(groupIDs, g.ID)
	}

	counts, err := r.loadAccountCounts(ctx, groupIDs)
	if err == nil {
		for i := range outGroups {
			outGroups[i].AccountCount = counts[outGroups[i].ID]
		}
	}

	return outGroups, nil
}

func (r *groupRepository) ListActiveByPlatform(ctx context.Context, platform string) ([]service.Group, error) {
	groups, err := r.client.Group.Query().
		Where(group.StatusEQ(service.StatusActive), group.PlatformEQ(platform)).
		Order(dbent.Asc(group.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	groupIDs := make([]int64, 0, len(groups))
	outGroups := make([]service.Group, 0, len(groups))
	for i := range groups {
		g := groupEntityToService(groups[i])
		outGroups = append(outGroups, *g)
		groupIDs = append(groupIDs, g.ID)
	}

	counts, err := r.loadAccountCounts(ctx, groupIDs)
	if err == nil {
		for i := range outGroups {
			outGroups[i].AccountCount = counts[outGroups[i].ID]
		}
	}

	return outGroups, nil
}

func (r *groupRepository) ExistsByName(ctx context.Context, name string) (bool, error) {
	return r.client.Group.Query().Where(group.NameEQ(name)).Exist(ctx)
}

func (r *groupRepository) GetAccountCount(ctx context.Context, groupID int64) (int64, error) {
	var count int64
	if err := scanSingleRow(ctx, r.sql, "SELECT COUNT(*) FROM account_groups WHERE group_id = $1", []any{groupID}, &count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *groupRepository) DeleteAccountGroupsByGroupID(ctx context.Context, groupID int64) (int64, error) {
	res, err := r.sql.ExecContext(ctx, "DELETE FROM account_groups WHERE group_id = $1", groupID)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventGroupChanged, nil, &groupID, nil); err != nil {
		log.Printf("[SchedulerOutbox] enqueue group account clear failed: group=%d err=%v", groupID, err)
	}
	return affected, nil
}

func (r *groupRepository) DeleteCascade(ctx context.Context, id int64) ([]int64, error) {
	g, err := r.client.Group.Query().Where(group.IDEQ(id)).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrGroupNotFound, nil)
	}
	groupSvc := groupEntityToService(g)

	// 使用 ent 事务统一包裹：避免手工基于 *sql.Tx 构造 ent client 带来的驱动断言问题，
	// 同时保证级联删除的原子性。
	tx, err := r.client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return nil, err
	}
	exec := r.client
	txClient := r.client
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		exec = tx.Client()
		txClient = exec
	}
	// err 为 dbent.ErrTxStarted 时，复用当前 client 参与同一事务。

	// Lock the group row to avoid concurrent writes while we cascade.
	// 这里使用 exec.QueryContext 手动扫描，确保同一事务内加锁并能区分"未找到"与其他错误。
	rows, err := exec.QueryContext(ctx, "SELECT id FROM groups WHERE id = $1 AND deleted_at IS NULL FOR UPDATE", id)
	if err != nil {
		return nil, err
	}
	var lockedID int64
	if rows.Next() {
		if err := rows.Scan(&lockedID); err != nil {
			_ = rows.Close()
			return nil, err
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if lockedID == 0 {
		return nil, service.ErrGroupNotFound
	}

	var affectedUserIDs []int64
	if groupSvc.IsSubscriptionType() {
		// 只查询未软删除的订阅，避免通知已取消订阅的用户
		rows, err := exec.QueryContext(ctx, "SELECT user_id FROM user_subscriptions WHERE group_id = $1 AND deleted_at IS NULL", id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var userID int64
			if scanErr := rows.Scan(&userID); scanErr != nil {
				_ = rows.Close()
				return nil, scanErr
			}
			affectedUserIDs = append(affectedUserIDs, userID)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}

		// 软删除订阅：设置 deleted_at 而非硬删除
		if _, err := exec.ExecContext(ctx, "UPDATE user_subscriptions SET deleted_at = NOW() WHERE group_id = $1 AND deleted_at IS NULL", id); err != nil {
			return nil, err
		}
	}

	// 2. Clear group_id for api keys bound to this group.
	// 仅更新未软删除的记录，避免修改已删除数据，保证审计与历史回溯一致性。
	// 与 APIKeyRepository 的软删除语义保持一致，减少跨模块行为差异。
	if _, err := txClient.APIKey.Update().
		Where(apikey.GroupIDEQ(id), apikey.DeletedAtIsNil()).
		ClearGroupID().
		Save(ctx); err != nil {
		return nil, err
	}

	// 3. Remove the group id from user_allowed_groups join table.
	// Legacy users.allowed_groups 列已弃用，不再同步。
	if _, err := exec.ExecContext(ctx, "DELETE FROM user_allowed_groups WHERE group_id = $1", id); err != nil {
		return nil, err
	}

	// 4. Delete account_groups join rows.
	if _, err := exec.ExecContext(ctx, "DELETE FROM account_groups WHERE group_id = $1", id); err != nil {
		return nil, err
	}

	// 5. Soft-delete group itself.
	if _, err := txClient.Group.Delete().Where(group.IDEQ(id)).Exec(ctx); err != nil {
		return nil, err
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventGroupChanged, nil, &id, nil); err != nil {
		log.Printf("[SchedulerOutbox] enqueue group cascade delete failed: group=%d err=%v", id, err)
	}

	return affectedUserIDs, nil
}

func (r *groupRepository) loadAccountCounts(ctx context.Context, groupIDs []int64) (counts map[int64]int64, err error) {
	counts = make(map[int64]int64, len(groupIDs))
	if len(groupIDs) == 0 {
		return counts, nil
	}

	rows, err := r.sql.QueryContext(
		ctx,
		"SELECT group_id, COUNT(*) FROM account_groups WHERE group_id = ANY($1) GROUP BY group_id",
		pq.Array(groupIDs),
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			counts = nil
		}
	}()

	for rows.Next() {
		var groupID int64
		var count int64
		if err = rows.Scan(&groupID, &count); err != nil {
			return nil, err
		}
		counts[groupID] = count
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return counts, nil
}
