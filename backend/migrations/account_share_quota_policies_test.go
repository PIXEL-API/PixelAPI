package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountShareQuotaPoliciesMigrationIsVersionedExpiringAndImmutable(t *testing.T) {
	content, err := FS.ReadFile("247_account_share_quota_policies.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	lowerSQL := strings.ToLower(sql)

	for _, required := range []string{
		"set local lock_timeout = '2s'",
		"set local statement_timeout = '60s'",
		"create table if not exists account_share_quota_policies",
		"scope_type = 'global'",
		"scope_type = 'owner'",
		"override_kind in ('default', 'manual', 'grandfather')",
		"status in ('active', 'revoked')",
		"expires_at > effective_at",
		"owner_user_id bigint references users(id) on delete restrict",
		"actor_user_id bigint references users(id) on delete set null",
		"actor_user_id_snapshot",
		"initial account-share quota defaults",
		"before update or delete",
		"before truncate",
		"account-share quota policy revisions are immutable",
	} {
		require.Contains(t, lowerSQL, required)
	}

	require.Contains(t, lowerSQL, "max_live_rooms")
	require.Contains(t, lowerSQL, "max_room_creates_24_hours")
	require.Contains(t, lowerSQL, "max_accounts_per_room")
	require.Contains(t, lowerSQL, "max_room_accounts_per_owner")
	require.NotContains(t, lowerSQL, "delete from account_share_quota_policies")
	require.NotContains(t, lowerSQL, "update account_share_quota_policies")
	require.NotContains(t, lowerSQL, "insert into wallets")
	require.NotContains(t, lowerSQL, "update wallets")
}
