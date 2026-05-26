package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const groupRateScheduleMultiplierEpsilon = 0.0000001

type groupRateScheduleRepository struct {
	db  *sql.DB
	sql sqlExecutor
}

func NewGroupRateScheduleRepository(sqlDB *sql.DB) service.GroupRateScheduleRepository {
	return &groupRateScheduleRepository{db: sqlDB, sql: sqlDB}
}

func (r *groupRateScheduleRepository) ListByGroupID(ctx context.Context, groupID int64) ([]service.GroupRateSchedule, error) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, group_id, start_minute, end_minute, rate_multiplier, enabled, created_at, updated_at
		FROM group_rate_schedules
		WHERE group_id = $1
		ORDER BY start_minute, end_minute, id
	`, groupID)
	if err != nil {
		return nil, err
	}
	return scanGroupRateSchedules(rows)
}

func (r *groupRateScheduleRepository) ReplaceForGroup(ctx context.Context, groupID int64, schedules []service.GroupRateScheduleInput) ([]service.GroupRateSchedule, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var existingGroupID int64
	if err := scanSingleRow(ctx, tx, `
		SELECT id
		FROM groups
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, []any{groupID}, &existingGroupID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrGroupNotFound
		}
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM group_rate_schedules WHERE group_id = $1`, groupID); err != nil {
		return nil, err
	}

	if len(schedules) > 0 {
		startMinutes := make([]int, len(schedules))
		endMinutes := make([]int, len(schedules))
		multipliers := make([]float64, len(schedules))
		enabled := make([]bool, len(schedules))
		for i, schedule := range schedules {
			startMinutes[i] = schedule.StartMinute
			endMinutes[i] = schedule.EndMinute
			multipliers[i] = schedule.RateMultiplier
			enabled[i] = schedule.Enabled
		}
		now := time.Now()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO group_rate_schedules (
				group_id, start_minute, end_minute, rate_multiplier, enabled, created_at, updated_at
			)
			SELECT
				$1::bigint,
				data.start_minute,
				data.end_minute,
				data.rate_multiplier,
				data.enabled,
				$2::timestamptz,
				$2::timestamptz
			FROM unnest($3::integer[], $4::integer[], $5::double precision[], $6::boolean[])
				AS data(start_minute, end_minute, rate_multiplier, enabled)
		`, groupID, now, pq.Array(startMinutes), pq.Array(endMinutes), pq.Array(multipliers), pq.Array(enabled)); err != nil {
			return nil, err
		}
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, group_id, start_minute, end_minute, rate_multiplier, enabled, created_at, updated_at
		FROM group_rate_schedules
		WHERE group_id = $1
		ORDER BY start_minute, end_minute, id
	`, groupID)
	if err != nil {
		return nil, err
	}
	out, err := scanGroupRateSchedules(rows)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *groupRateScheduleRepository) ListEnabled(ctx context.Context) ([]service.GroupRateSchedule, error) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT s.id, s.group_id, s.start_minute, s.end_minute, s.rate_multiplier, s.enabled, s.created_at, s.updated_at
		FROM group_rate_schedules s
		JOIN groups g ON g.id = s.group_id AND g.deleted_at IS NULL
		WHERE s.enabled = TRUE
		ORDER BY s.group_id, s.start_minute, s.end_minute, s.id
	`)
	if err != nil {
		return nil, err
	}
	return scanGroupRateSchedules(rows)
}

func (r *groupRateScheduleRepository) ListManagedGroupIDs(ctx context.Context) ([]int64, error) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT group_id
		FROM (
			SELECT DISTINCT s.group_id
			FROM group_rate_schedules s
			JOIN groups g ON g.id = s.group_id AND g.deleted_at IS NULL
			WHERE s.enabled = TRUE
			UNION
			SELECT st.group_id
			FROM group_rate_schedule_states st
			JOIN groups g ON g.id = st.group_id AND g.deleted_at IS NULL
		) AS managed
		ORDER BY group_id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var groupIDs []int64
	for rows.Next() {
		var groupID int64
		if err := rows.Scan(&groupID); err != nil {
			return nil, err
		}
		groupIDs = append(groupIDs, groupID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groupIDs, nil
}

func (r *groupRateScheduleRepository) ApplyScheduledMultiplier(ctx context.Context, groupID int64, scheduleID int64, rateMultiplier float64) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var currentMultiplier float64
	if err := scanSingleRow(ctx, tx, `
		SELECT rate_multiplier
		FROM groups
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, []any{groupID}, &currentMultiplier); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, service.ErrGroupNotFound
		}
		return false, err
	}

	now := time.Now()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO group_rate_schedule_states (
			group_id, base_rate_multiplier, applied_schedule_id, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (group_id)
		DO UPDATE SET
			applied_schedule_id = EXCLUDED.applied_schedule_id,
			updated_at = EXCLUDED.updated_at
	`, groupID, currentMultiplier, scheduleID, now); err != nil {
		return false, err
	}

	changed := mathAbs(currentMultiplier-rateMultiplier) > groupRateScheduleMultiplierEpsilon
	if changed {
		if _, err := tx.ExecContext(ctx, `
			UPDATE groups
			SET rate_multiplier = $2, updated_at = $3
			WHERE id = $1 AND deleted_at IS NULL
		`, groupID, rateMultiplier, now); err != nil {
			return false, err
		}
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventGroupChanged, nil, &groupID, nil); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return changed, nil
}

func (r *groupRateScheduleRepository) RestoreBaseMultiplier(ctx context.Context, groupID int64) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var baseMultiplier float64
	if err := scanSingleRow(ctx, tx, `
		SELECT base_rate_multiplier
		FROM group_rate_schedule_states
		WHERE group_id = $1
		FOR UPDATE
	`, []any{groupID}, &baseMultiplier); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	var currentMultiplier float64
	if err := scanSingleRow(ctx, tx, `
		SELECT rate_multiplier
		FROM groups
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, []any{groupID}, &currentMultiplier); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, service.ErrGroupNotFound
		}
		return false, err
	}

	now := time.Now()
	changed := mathAbs(currentMultiplier-baseMultiplier) > groupRateScheduleMultiplierEpsilon
	if changed {
		if _, err := tx.ExecContext(ctx, `
			UPDATE groups
			SET rate_multiplier = $2, updated_at = $3
			WHERE id = $1 AND deleted_at IS NULL
		`, groupID, baseMultiplier, now); err != nil {
			return false, err
		}
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventGroupChanged, nil, &groupID, nil); err != nil {
			return false, err
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM group_rate_schedule_states WHERE group_id = $1`, groupID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return changed, nil
}

func scanGroupRateSchedules(rows *sql.Rows) ([]service.GroupRateSchedule, error) {
	defer func() { _ = rows.Close() }()
	var out []service.GroupRateSchedule
	for rows.Next() {
		var schedule service.GroupRateSchedule
		if err := rows.Scan(
			&schedule.ID,
			&schedule.GroupID,
			&schedule.StartMinute,
			&schedule.EndMinute,
			&schedule.RateMultiplier,
			&schedule.Enabled,
			&schedule.CreatedAt,
			&schedule.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, schedule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func mathAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
