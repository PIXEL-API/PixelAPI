package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// UpdateCNProviderExtraIfMatch atomically merges probe metadata only when the
// account loaded before the upstream request is still the same schedulable
// account. This prevents a late probe from overwriting an administrator edit.
func (r *accountRepository) UpdateCNProviderExtraIfMatch(ctx context.Context, id int64, expected *service.Account, updates map[string]any) (bool, error) {
	if expected == nil || len(updates) == 0 {
		return false, nil
	}
	payload, err := json.Marshal(updates)
	if err != nil {
		return false, err
	}
	credentials, err := json.Marshal(expected.Credentials)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
			UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || $1::jsonb, updated_at = NOW()
			WHERE id=$2 AND deleted_at IS NULL AND platform=$3 AND type=$4 AND status=$5
			  AND schedulable=$6 AND credentials=$7::jsonb
			  AND ((proxy_id IS NULL AND $8::bigint IS NULL) OR (proxy_id=$8 AND $9::timestamptz IS NOT NULL AND EXISTS (SELECT 1 FROM proxies p WHERE p.id=proxy_id AND p.updated_at=$9)))
			RETURNING id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $10, id, NULL, NULL FROM updated
		`, string(payload), id, expected.Platform, expected.Type, expected.Status, expected.Schedulable, string(credentials), cnNullableInt64(expected.ProxyID), expectedProxyUpdatedAt(expected), service.SchedulerOutboxEventAccountChanged)
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
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}

func (r *accountRepository) SetCNProviderTempUnschedulableIfMatch(ctx context.Context, id int64, expected *service.Account, until time.Time, reason string) (bool, error) {
	if expected == nil {
		return false, nil
	}
	credentials, err := json.Marshal(expected.Credentials)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
			UPDATE accounts SET temp_unschedulable_until = CASE WHEN temp_unschedulable_until IS NULL OR temp_unschedulable_until < $1 THEN $1 ELSE temp_unschedulable_until END,
				temp_unschedulable_reason=$2, updated_at=NOW()
			WHERE id=$3 AND deleted_at IS NULL AND platform=$4 AND type=$5 AND status=$6 AND schedulable=$7
			  AND credentials=$8::jsonb AND ((proxy_id IS NULL AND $9::bigint IS NULL) OR (proxy_id=$9 AND $10::timestamptz IS NOT NULL AND EXISTS (SELECT 1 FROM proxies p WHERE p.id=proxy_id AND p.updated_at=$10)))
			  AND (temp_unschedulable_until IS NULL OR temp_unschedulable_until < NOW() OR temp_unschedulable_reason LIKE 'cn_balance_low%' OR temp_unschedulable_reason LIKE 'cn_concurrency_limit%')
			RETURNING id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $11, id, NULL, NULL FROM updated
		`, until, reason, id, expected.Platform, expected.Type, expected.Status, expected.Schedulable, string(credentials), cnNullableInt64(expected.ProxyID), expectedProxyUpdatedAt(expected), service.SchedulerOutboxEventAccountChanged)
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
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}

func (r *accountRepository) ClearCNProviderTempUnschedulableIfMatch(ctx context.Context, id int64, expected *service.Account, reasonPrefix string) (bool, error) {
	if expected == nil || expected.TempUnschedulableUntil == nil || reasonPrefix == "" {
		return false, nil
	}
	credentials, err := json.Marshal(expected.Credentials)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
			UPDATE accounts SET temp_unschedulable_until=NULL, temp_unschedulable_reason=NULL, updated_at=NOW()
			WHERE id=$1 AND deleted_at IS NULL AND platform=$2 AND type=$3 AND status=$4 AND schedulable=$5
			  AND temp_unschedulable_until=$6 AND temp_unschedulable_reason LIKE $7
			  AND credentials=$9::jsonb
			  AND ((proxy_id IS NULL AND $10::bigint IS NULL) OR (proxy_id=$10 AND $11::timestamptz IS NOT NULL AND EXISTS (SELECT 1 FROM proxies p WHERE p.id=proxy_id AND p.updated_at=$11)))
			RETURNING id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $8, id, NULL, NULL FROM updated
	`, id, expected.Platform, expected.Type, expected.Status, expected.Schedulable, *expected.TempUnschedulableUntil, reasonPrefix+"%", service.SchedulerOutboxEventAccountChanged, string(credentials), cnNullableInt64(expected.ProxyID), expectedProxyUpdatedAt(expected))
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
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}

func cnNullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func expectedProxyUpdatedAt(account *service.Account) any {
	if account != nil && account.ProxyID != nil && account.Proxy != nil && !account.Proxy.UpdatedAt.IsZero() {
		return account.Proxy.UpdatedAt
	}
	return nil
}
