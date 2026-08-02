//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// groupScopeRecordingRepo 记录 GetAllGroups / GetAllGroupsByPlatform 实际走了哪个仓储方法。
//
// 这组用例守护的是一个性能契约而非功能契约：生产库有 11.7 万个 user_private 分组、
// 仅 11 个 public 分组。若作用域过滤退回应用层（ListActive + 内存过滤），单次调用会
// 物化 11.7 万行，实测约 2.0 秒。因此指定作用域时必须走下推到 SQL 的方法。
type groupScopeRecordingRepo struct {
	groupRepoNoop

	listActiveCalls           int
	listActiveByPlatformCalls int
	scopeCalls                []string
	platformScopeCalls        [][2]string
	scopedGroups              []Group
	unscopedGroups            []Group
}

func (r *groupScopeRecordingRepo) ListActive(context.Context) ([]Group, error) {
	r.listActiveCalls++
	return r.unscopedGroups, nil
}

func (r *groupScopeRecordingRepo) ListActiveByPlatform(context.Context, string) ([]Group, error) {
	r.listActiveByPlatformCalls++
	return r.unscopedGroups, nil
}

func (r *groupScopeRecordingRepo) ListActiveByScope(_ context.Context, scope string) ([]Group, error) {
	r.scopeCalls = append(r.scopeCalls, scope)
	return r.scopedGroups, nil
}

func (r *groupScopeRecordingRepo) ListActiveByPlatformAndScope(_ context.Context, platform, scope string) ([]Group, error) {
	r.platformScopeCalls = append(r.platformScopeCalls, [2]string{platform, scope})
	return r.scopedGroups, nil
}

func TestGetAllGroupsPushesScopeToRepository(t *testing.T) {
	publicGroups := []Group{
		{ID: 1, Name: "公共池", Scope: GroupScopePublic},
	}
	// 若过滤退回应用层，这些私有分组会被读出来——正是要避免的行为。
	unscoped := append([]Group{}, publicGroups...)
	for i := int64(0); i < 5; i++ {
		unscoped = append(unscoped, Group{ID: 100 + i, Name: "私有", Scope: GroupScopeUserPrivate})
	}

	t.Run("指定 public 时下推到仓储，不读取全部分组", func(t *testing.T) {
		repo := &groupScopeRecordingRepo{scopedGroups: publicGroups, unscopedGroups: unscoped}
		svc := &adminServiceImpl{groupRepo: repo}

		got, err := svc.GetAllGroups(context.Background(), GroupScopePublic)

		require.NoError(t, err)
		require.Equal(t, publicGroups, got)
		require.Equal(t, []string{GroupScopePublic}, repo.scopeCalls)
		require.Zero(t, repo.listActiveCalls, "指定作用域时不应再全量读取分组")
	})

	t.Run("指定 user_private 时同样下推", func(t *testing.T) {
		privateGroups := []Group{{ID: 100, Name: "私有", Scope: GroupScopeUserPrivate}}
		repo := &groupScopeRecordingRepo{scopedGroups: privateGroups, unscopedGroups: unscoped}
		svc := &adminServiceImpl{groupRepo: repo}

		got, err := svc.GetAllGroups(context.Background(), GroupScopeUserPrivate)

		require.NoError(t, err)
		require.Equal(t, privateGroups, got)
		require.Equal(t, []string{GroupScopeUserPrivate}, repo.scopeCalls)
		require.Zero(t, repo.listActiveCalls)
	})

	t.Run("scope 为 all 或空时保持原有全量语义", func(t *testing.T) {
		for _, scope := range []string{"", "all"} {
			repo := &groupScopeRecordingRepo{scopedGroups: publicGroups, unscopedGroups: unscoped}
			svc := &adminServiceImpl{groupRepo: repo}

			got, err := svc.GetAllGroups(context.Background(), scope)

			require.NoError(t, err)
			require.Equal(t, unscoped, got, "scope=%q 应返回全部活跃分组", scope)
			require.Equal(t, 1, repo.listActiveCalls, "scope=%q", scope)
			require.Empty(t, repo.scopeCalls, "scope=%q 不应走收窄路径", scope)
		}
	})
}

func TestGetAllGroupsByPlatformPushesScopeToRepository(t *testing.T) {
	publicGroups := []Group{{ID: 1, Name: "公共池", Platform: PlatformOpenAI, Scope: GroupScopePublic}}
	unscoped := append([]Group{}, publicGroups...)
	unscoped = append(unscoped, Group{ID: 100, Name: "私有", Platform: PlatformOpenAI, Scope: GroupScopeUserPrivate})

	t.Run("平台与作用域一起下推", func(t *testing.T) {
		repo := &groupScopeRecordingRepo{scopedGroups: publicGroups, unscopedGroups: unscoped}
		svc := &adminServiceImpl{groupRepo: repo}

		got, err := svc.GetAllGroupsByPlatform(context.Background(), PlatformOpenAI, GroupScopePublic)

		require.NoError(t, err)
		require.Equal(t, publicGroups, got)
		require.Equal(t, [][2]string{{PlatformOpenAI, GroupScopePublic}}, repo.platformScopeCalls)
		require.Zero(t, repo.listActiveByPlatformCalls, "指定作用域时不应再按平台全量读取")
	})

	t.Run("scope 为 all 时保持原有全量语义", func(t *testing.T) {
		repo := &groupScopeRecordingRepo{scopedGroups: publicGroups, unscopedGroups: unscoped}
		svc := &adminServiceImpl{groupRepo: repo}

		got, err := svc.GetAllGroupsByPlatform(context.Background(), PlatformOpenAI, "all")

		require.NoError(t, err)
		require.Equal(t, unscoped, got)
		require.Equal(t, 1, repo.listActiveByPlatformCalls)
		require.Empty(t, repo.platformScopeCalls)
	})
}

// TestFilterGroupsByScopeTreatsUnknownScopeAsPublic 固定 NormalizeGroupScope 的语义，
// 仓储侧的 scopePredicate 依赖它：public 的谓词是「≠ user_private」而非「= public」。
func TestFilterGroupsByScopeTreatsUnknownScopeAsPublic(t *testing.T) {
	groups := []Group{
		{ID: 1, Scope: GroupScopePublic},
		{ID: 2, Scope: ""},
		{ID: 3, Scope: "legacy_unknown"},
		{ID: 4, Scope: GroupScopeUserPrivate},
	}

	publicOnly := filterGroupsByScope(groups, GroupScopePublic)
	require.Len(t, publicOnly, 3, "空值与未知取值都应归为 public")

	privateOnly := filterGroupsByScope(groups, GroupScopeUserPrivate)
	require.Len(t, privateOnly, 1)
	require.Equal(t, int64(4), privateOnly[0].ID)
}
