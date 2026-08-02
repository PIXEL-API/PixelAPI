//go:build unit

package repository

import (
	"context"
	"database/sql"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newAllowedGroupsRepoSQLite(t *testing.T) (*apiKeyRepository, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:api_key_repo_allowed_groups?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return &apiKeyRepository{client: client}, client
}

// 鉴权路径必须真的把 allowed_groups 加载出来。
//
// 这是专属分组运行时授权复核（middleware 的 validateAPIKeyGroupAllowed →
// User.CanBindGroup）唯一的数据来源，而它走的是 GetByKeyForAuth，
// **不是** userRepository 那条带 loadAllowedGroups 的路径。
//
// 曾经这里有两个方向相反的洞互相抵消：Select 白名单同时漏了 group.is_exclusive
// 与 users 的 allowed_groups 边，ent 把 IsExclusive 回填成 false，
// CanBindGroup(id, false) 恒真，复核形同虚设；一旦只补上 is_exclusive，
// AllowedGroups 仍为 nil，复核就从「恒真」翻成「恒假」，专属标准分组全量 403。
//
// 注意：只在内存里构造 service.User{AllowedGroups: ...} 的测试是**假绿**——
// 它绕开了 repo 加载层，正是这个洞藏了两个版本的原因。本测试必须走真实查询。
func TestGetByKeyForAuthLoadsAllowedGroups(t *testing.T) {
	repo, client := newAllowedGroupsRepoSQLite(t)
	ctx := context.Background()

	user, err := client.User.Create().
		SetEmail("allowed-groups@test.com").
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	// 专属且**非订阅型**：订阅型会在复核里走早返回，测不到这条路径。
	exclusive, err := client.Group.Create().
		SetName("exclusive-standard").
		SetPlatform("anthropic").
		SetStatus(service.StatusActive).
		SetIsExclusive(true).
		Save(ctx)
	require.NoError(t, err)

	other, err := client.Group.Create().
		SetName("another-exclusive").
		SetPlatform("anthropic").
		SetStatus(service.StatusActive).
		SetIsExclusive(true).
		Save(ctx)
	require.NoError(t, err)

	// 故意先插大 ID 再插小 ID，验证返回结果被排序（快照序列化需要稳定顺序）。
	_, err = client.UserAllowedGroup.Create().SetUserID(user.ID).SetGroupID(other.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.UserAllowedGroup.Create().SetUserID(user.ID).SetGroupID(exclusive.ID).Save(ctx)
	require.NoError(t, err)

	const rawKey = "sk-allowed-groups-test"
	_, err = client.APIKey.Create().
		SetUserID(user.ID).
		SetGroupID(exclusive.ID).
		SetName("k").
		SetKey(rawKey).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	got, err := repo.GetByKeyForAuth(ctx, rawKey)
	require.NoError(t, err)
	require.NotNil(t, got.User)
	require.NotNil(t, got.Group)

	// is_exclusive 必须被选出来，否则复核退化成恒真。
	require.True(t, got.Group.IsExclusive, "group.is_exclusive must be selected")

	// allowed_groups 必须被 eager-load，否则复核变成恒假。
	require.Equal(t, []int64{exclusive.ID, other.ID}, got.User.AllowedGroups,
		"allowed_groups must be loaded and sorted")

	// 端到端：这把 Key 必须被放行。
	require.True(t, got.User.CanBindGroup(got.Group.ID, got.Group.IsExclusive),
		"authorized key on an exclusive standard group must pass the runtime check")
}

// 未被授权的用户必须被拒——确认复核不是又退化成恒真。
func TestGetByKeyForAuthRejectsUnauthorizedExclusiveGroup(t *testing.T) {
	repo, client := newAllowedGroupsRepoSQLite(t)
	ctx := context.Background()

	user, err := client.User.Create().
		SetEmail("revoked@test.com").
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	exclusive, err := client.Group.Create().
		SetName("revoked-exclusive").
		SetPlatform("anthropic").
		SetStatus(service.StatusActive).
		SetIsExclusive(true).
		Save(ctx)
	require.NoError(t, err)

	// 刻意不写 user_allowed_groups：模拟授权已被撤销。
	const rawKey = "sk-revoked-test"
	_, err = client.APIKey.Create().
		SetUserID(user.ID).
		SetGroupID(exclusive.ID).
		SetName("k").
		SetKey(rawKey).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	got, err := repo.GetByKeyForAuth(ctx, rawKey)
	require.NoError(t, err)
	require.Empty(t, got.User.AllowedGroups)
	require.True(t, got.Group.IsExclusive)
	require.False(t, got.User.CanBindGroup(got.Group.ID, got.Group.IsExclusive),
		"revoked authorization must be rejected")
}

// 非专属分组不受 allowed_groups 约束，任何用户都能用。
func TestGetByKeyForAuthNonExclusiveGroupNeedsNoGrant(t *testing.T) {
	repo, client := newAllowedGroupsRepoSQLite(t)
	ctx := context.Background()

	user, err := client.User.Create().
		SetEmail("public-group@test.com").
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	public, err := client.Group.Create().
		SetName("public").
		SetPlatform("anthropic").
		SetStatus(service.StatusActive).
		SetIsExclusive(false).
		Save(ctx)
	require.NoError(t, err)

	const rawKey = "sk-public-group-test"
	_, err = client.APIKey.Create().
		SetUserID(user.ID).
		SetGroupID(public.ID).
		SetName("k").
		SetKey(rawKey).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	got, err := repo.GetByKeyForAuth(ctx, rawKey)
	require.NoError(t, err)
	require.False(t, got.Group.IsExclusive)
	require.True(t, got.User.CanBindGroup(got.Group.ID, got.Group.IsExclusive))
}
