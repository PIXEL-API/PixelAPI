package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestQueryAffiliatePeriodRebateCombinesPublicAndRoomSettlements(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(SUM(settlements.rebate_credit), 0)::double precision")).
		WithArgs(int64(42), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"rebate"}).AddRow(6.75))

	total, err := queryAffiliatePeriodRebate(context.Background(), db, 42, service.AffiliateDetailQuery{
		PeriodStart: &start,
		PeriodEnd:   &end,
	})
	require.NoError(t, err)
	require.InDelta(t, 6.75, total, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())

	require.Contains(t, affiliatePeriodRebateSQL, "FROM account_share_settlement_entries ase")
	require.Contains(t, affiliatePeriodRebateSQL, "FROM account_share_mode_settlement_entries asmse")
	require.Contains(t, affiliatePeriodRebateSQL, "UNION ALL")
	require.Contains(t, affiliatePeriodRebateSQL, "IN ('usage_request', 'seat_charge')")
	require.Contains(t, affiliatePeriodRebateSQL, "THEN -asmse.invite_credit")
	require.Contains(t, affiliatePeriodRebateSQL, "IN ('usage_request', 'seat_charge', 'seat_waiver_refund')")
	require.NotContains(t, affiliatePeriodRebateSQL, "'seat_refund'")
	require.Equal(t, 2, strings.Count(affiliatePeriodRebateSQL, "created_at >= $2::timestamptz"))
	require.Equal(t, 2, strings.Count(affiliatePeriodRebateSQL, "created_at < $3::timestamptz"))
}

func TestAffiliateInviteesSettlementSQLPreAggregatesWithoutCartesianAmplification(t *testing.T) {
	require.Equal(t, 1, strings.Count(affiliateInviteesSettlementSQL, "FROM account_share_settlement_entries ase"))
	require.Equal(t, 1, strings.Count(affiliateInviteesSettlementSQL, "FROM account_share_mode_settlement_entries asmse"))
	require.Equal(t, 1, strings.Count(affiliateInviteesSettlementSQL, "UNION ALL"))
	require.Contains(t, affiliateInviteesSettlementSQL, "FROM settlement_parts")
	require.Contains(t, affiliateInviteesSettlementSQL, "GROUP BY consumer_user_id")
	require.Contains(t, affiliateInviteesSettlementSQL, "LEFT JOIN settlement_totals st")

	require.Contains(t, affiliateInviteesSettlementSQL, "THEN asmse.total_charge")
	require.Contains(t, affiliateInviteesSettlementSQL, "THEN asmse.invite_credit")
	require.Contains(t, affiliateInviteesSettlementSQL, "THEN -asmse.refund_amount")
	require.Contains(t, affiliateInviteesSettlementSQL, "THEN -asmse.invite_credit")
	require.Contains(t, affiliateInviteesSettlementSQL, "IN ('usage_request', 'seat_charge', 'seat_waiver_refund')")
	require.NotContains(t, affiliateInviteesSettlementSQL, "'seat_refund'")

	require.Contains(t, affiliateInviteesSettlementSQL, "ase.created_at >= si.invited_at")
	require.Contains(t, affiliateInviteesSettlementSQL, "asmse.created_at >= si.invited_at")
	require.Contains(t, affiliateInviteesSettlementSQL, "created_at >= $2::timestamptz")
	require.Contains(t, affiliateInviteesSettlementSQL, "created_at < $3::timestamptz")
	require.Contains(t, affiliateInviteesSettlementSQL, "ORDER BY si.invited_at DESC")
	require.Contains(t, affiliateInviteesSettlementSQL, "LIMIT $4")
}
