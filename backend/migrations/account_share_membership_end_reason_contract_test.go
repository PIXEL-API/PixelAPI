package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountShareMembershipEndReasonContractCoversLifecycleReasons(t *testing.T) {
	content, err := FS.ReadFile("243_account_share_membership_end_reason_contract.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	for _, reason := range []string{
		"manual",
		"idle_timeout",
		"prepay_insufficient",
		"account_unavailable",
		"queue_expired",
		"room_draining",
	} {
		require.Contains(t, sql, "'"+reason+"'")
	}
	require.Contains(t, sql, "ADD CONSTRAINT account_share_memberships_ended_reason_chk")
	require.Contains(t, sql, "NOT VALID")
	require.Contains(t, sql, "VALIDATE CONSTRAINT account_share_memberships_ended_reason_chk")
	require.NotContains(t, strings.ToUpper(sql), "DELETE FROM ")
	require.NotContains(t, strings.ToUpper(sql), "TRUNCATE ")
}
