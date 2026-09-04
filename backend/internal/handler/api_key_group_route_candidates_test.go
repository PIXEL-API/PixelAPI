package handler

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func routeTestGroup(id int64) *service.Group {
	return &service.Group{
		ID:       id,
		Status:   service.StatusActive,
		Platform: service.PlatformOpenAI,
		Hydrated: true,
	}
}

// 候选构建必须与鉴权中间件共用同一套静态规则：停用的分组、被撤销授权的专属分组
// 都不能进候选，否则中间件为多分组路由放行之后，请求会落回不该用的分组。
func TestBuildAPIKeyGroupRouteCandidatesFiltersUnusableRoutes(t *testing.T) {
	t.Parallel()

	inactive := routeTestGroup(2)
	inactive.Status = service.StatusDisabled

	unauthorizedExclusive := routeTestGroup(3)
	unauthorizedExclusive.IsExclusive = true

	primaryID := int64(1)
	apiKey := &service.APIKey{
		ID:      9001,
		User:    &service.User{ID: 1},
		GroupID: &primaryID,
		Group:   routeTestGroup(1),
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(1)},
			{GroupID: 2, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: inactive},
			{GroupID: 3, Priority: 3, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: unauthorizedExclusive},
			{GroupID: 4, Priority: 4, Weight: 1, Enabled: false, CooldownSeconds: 30, Group: routeTestGroup(4)},
			{GroupID: 5, Priority: 5, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(5)},
		},
	}

	candidates, available := buildAPIKeyGroupRouteCandidates(apiKey)
	if !available {
		t.Fatal("available = false, want true")
	}
	got := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		got = append(got, candidate.Route.GroupID)
	}
	want := []int64{1, 5}
	if len(got) != len(want) {
		t.Fatalf("candidate group ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate group ids = %v, want %v", got, want)
		}
	}
}

// 配了路由但全部不可用时必须明确报「无可用路由」，不能悄悄回落到主分组——
// 那正是被停用/被撤销授权的那个分组。
func TestBuildAPIKeyGroupRouteCandidatesAllUnusable(t *testing.T) {
	t.Parallel()

	inactive := routeTestGroup(2)
	inactive.Status = service.StatusDisabled

	primaryID := int64(2)
	apiKey := &service.APIKey{
		ID:      9002,
		User:    &service.User{ID: 1},
		GroupID: &primaryID,
		Group:   inactive,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 2, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: inactive},
		},
	}

	candidates, available := buildAPIKeyGroupRouteCandidates(apiKey)
	if available || len(candidates) != 0 {
		t.Fatalf("candidates = %v, available = %v, want empty/false", candidates, available)
	}
}

func TestShouldSkipAPIKeyGroupRouteOnBillingError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		// 与分组绑定的失败：换一条路由确实可能救回来。
		{"订阅缺失", service.ErrSubscriptionNotFound, true},
		{"订阅无效", service.ErrSubscriptionInvalid, true},
		{"订阅过期", service.ErrSubscriptionExpired, true},
		{"订阅暂停", service.ErrSubscriptionSuspended, true},
		{"日限额超限", service.ErrDailyLimitExceeded, true},
		{"周限额超限", service.ErrWeeklyLimitExceeded, true},
		{"月限额超限", service.ErrMonthlyLimitExceeded, true},
		{"分组RPM超限", service.ErrGroupRPMExceeded, true},
		// 余额不足也是路由相关的：下一条若是订阅型分组就不吃余额。
		{"余额不足", service.ErrInsufficientBalance, true},
		{"包装后的分组业务错误", fmt.Errorf("billing gate: %w", service.ErrSubscriptionExpired), true},

		// 与 Key/用户/服务绑定的失败：换路由救不了，不该白白遍历整条链。
		{"计费服务不可用", service.ErrBillingServiceUnavailable, false},
		{"订阅仓储不可用", service.ErrSubscriptionRepositoryUnavailable, false},
		{"Key5h限额", service.ErrAPIKeyRateLimit5hExceeded, false},
		{"Key日限额", service.ErrAPIKeyRateLimit1dExceeded, false},
		{"Key7d限额", service.ErrAPIKeyRateLimit7dExceeded, false},
		{"用户RPM超限", service.ErrUserRPMExceeded, false},
		{"数据库原始错误", errors.New("query route subscription failed"), false},
		{"请求已取消", context.Canceled, false},

		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldSkipAPIKeyGroupRouteOnBillingError(tt.err); got != tt.want {
				t.Fatalf("shouldSkipAPIKeyGroupRouteOnBillingError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// 整条链都不可用时，回给客户端的应当是第一条路由的错误——那才是用户眼里的主分组。
func TestAPIKeyGroupRouteBillingGateReportsFirstError(t *testing.T) {
	t.Parallel()

	primaryID := int64(1)
	apiKey := &service.APIKey{
		ID:      9003,
		User:    &service.User{ID: 1},
		GroupID: &primaryID,
		Group:   routeTestGroup(1),
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(1)},
			{GroupID: 5, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(5)},
		},
	}
	cursor := newAPIKeyGroupRouteCursor(apiKey)

	var gate apiKeyGroupRouteBillingGate

	retry, termErr := gate.skipOrTerminate(cursor, service.ErrSubscriptionNotFound, "test", nil)
	if !retry || termErr != nil {
		t.Fatalf("first call retry = %v, termErr = %v, want true/nil", retry, termErr)
	}

	// 第二条也不行，且已无下一条：应当回报第一条的错误而不是这一条的。
	retry, termErr = gate.skipOrTerminate(cursor, service.ErrDailyLimitExceeded, "test", nil)
	if retry {
		t.Fatal("second call retry = true, want false (no next route)")
	}
	if !errors.Is(termErr, service.ErrSubscriptionNotFound) {
		t.Fatalf("termErr = %v, want ErrSubscriptionNotFound", termErr)
	}
}

// 非路由相关的错误必须原样透出，不能被当成「换条路由试试」白烧一遍链路。
func TestAPIKeyGroupRouteBillingGatePassesThroughGlobalError(t *testing.T) {
	t.Parallel()

	primaryID := int64(1)
	apiKey := &service.APIKey{
		ID:      9004,
		User:    &service.User{ID: 1},
		GroupID: &primaryID,
		Group:   routeTestGroup(1),
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(1)},
			{GroupID: 5, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(5)},
		},
	}
	cursor := newAPIKeyGroupRouteCursor(apiKey)

	var gate apiKeyGroupRouteBillingGate
	retry, termErr := gate.skipOrTerminate(cursor, service.ErrAPIKeyRateLimit1dExceeded, "test", nil)
	if retry {
		t.Fatal("retry = true, want false")
	}
	if !errors.Is(termErr, service.ErrAPIKeyRateLimit1dExceeded) {
		t.Fatalf("termErr = %v, want ErrAPIKeyRateLimit1dExceeded", termErr)
	}
}

func TestContinuationRouteCursorCanPinCoolingBoundGroup(t *testing.T) {
	primaryID := int64(1)
	apiKey := &service.APIKey{
		ID:      99123,
		User:    &service.User{ID: 1},
		GroupID: &primaryID,
		Group:   routeTestGroup(1),
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(1)},
			{GroupID: 2, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(2)},
		},
	}

	apiKeyGroupRouteBreaker.recordFailure(apiKey.ID, 2, 30)
	defer apiKeyGroupRouteBreaker.recordSuccess(apiKey.ID, 2)

	normalCursor := newAPIKeyGroupRouteCursor(apiKey)
	if got := normalCursor.groupIDs(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("normal route groups = %v, want [1]", got)
	}

	continuationCursor := newAPIKeyGroupContinuationRouteCursor(apiKey)
	if got := continuationCursor.groupIDs(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("continuation route groups = %v, want [1 2]", got)
	}
	if !continuationCursor.pinToGroup(2) {
		t.Fatal("pinToGroup(2) = false, want true")
	}
	candidate, ok := continuationCursor.current()
	if !ok || candidate.Route.GroupID != 2 {
		t.Fatalf("pinned candidate = %+v, ok = %v, want group 2", candidate, ok)
	}
	if continuationCursor.hasNext() {
		t.Fatal("pinned continuation cursor must not expose a failover route")
	}
}

func TestContinuationRouteCursorMissingOwnerDefaultsToPrimaryEvenWhenCooling(t *testing.T) {
	primaryID := int64(1)
	apiKey := &service.APIKey{
		ID:      99126,
		User:    &service.User{ID: 1},
		GroupID: &primaryID,
		Group:   routeTestGroup(1),
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(1)},
			{GroupID: 2, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(2)},
		},
	}

	apiKeyGroupRouteBreaker.recordFailure(apiKey.ID, 1, 30)
	defer apiKeyGroupRouteBreaker.recordSuccess(apiKey.ID, 1)

	normalRoute, ok := newAPIKeyGroupRouteCursor(apiKey).current()
	if !ok || normalRoute.Route.GroupID != 2 {
		t.Fatalf("normal current route = %+v, ok = %v, want cooling fallback group 2", normalRoute, ok)
	}

	continuationCursor := newAPIKeyGroupContinuationRouteCursor(apiKey)
	defaultRoute, ok := continuationCursor.current()
	if !ok || defaultRoute.Route.GroupID != 1 {
		t.Fatalf("continuation default route = %+v, ok = %v, want configured primary group 1", defaultRoute, ok)
	}
	if !continuationCursor.pinToGroup(apiKeyGroupIDValue(defaultRoute.APIKey)) {
		t.Fatal("pin continuation default route = false, want true")
	}
	if continuationCursor.hasNext() {
		t.Fatal("missing-owner continuation must not expose a backup route after pinning")
	}
}

func TestModeIsolatedRouteCursorPinsPrimaryModeGroupIgnoringBreaker(t *testing.T) {
	primaryID := int64(1)
	apiKey := &service.APIKey{
		ID:      99127,
		User:    &service.User{ID: 1},
		GroupID: &primaryID,
		Group:   routeTestGroup(1),
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(1)},
			{GroupID: 2, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: routeTestGroup(2)},
		},
	}

	apiKeyGroupRouteBreaker.recordFailure(apiKey.ID, 1, 30)
	defer apiKeyGroupRouteBreaker.recordSuccess(apiKey.ID, 1)

	classified := make([]int64, 0, 2)
	classifier := func(_ context.Context, groupID int64) (bool, error) {
		classified = append(classified, groupID)
		return groupID == 1, nil
	}
	cursor, continuationGroupIDs, err := newAPIKeyGroupRouteCursorWithModeIsolation(
		context.Background(), apiKey, classifier, true,
	)
	if err != nil {
		t.Fatalf("new mode-isolated cursor: %v", err)
	}
	current, ok := cursor.current()
	if !ok || current.Route.GroupID != 1 {
		t.Fatalf("current route = %+v, ok = %v, want cooling primary mode group 1", current, ok)
	}
	if cursor.hasNext() {
		t.Fatal("mode group cursor must not expose an ordinary backup route")
	}
	if len(continuationGroupIDs) != 1 || continuationGroupIDs[0] != 1 {
		t.Fatalf("continuation groups = %v, want [1]", continuationGroupIDs)
	}
	if len(classified) != 1 || classified[0] != 1 {
		t.Fatalf("classified groups = %v, want only primary [1]", classified)
	}
}

func TestModeIsolatedRouteCursorFiltersModeBackupFromOrdinaryKey(t *testing.T) {
	primaryID := int64(1)
	disabledNormal := routeTestGroup(4)
	disabledNormal.Status = service.StatusDisabled
	apiKey := &service.APIKey{
		ID:      99128,
		User:    &service.User{ID: 1},
		GroupID: &primaryID,
		Group:   routeTestGroup(1),
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, Group: routeTestGroup(1)},
			{GroupID: 2, Priority: 2, Weight: 1, Enabled: true, Group: routeTestGroup(2)},
			{GroupID: 3, Priority: 3, Weight: 1, Enabled: true, Group: routeTestGroup(3)},
			{GroupID: 4, Priority: 4, Weight: 1, Enabled: true, Group: disabledNormal},
		},
	}

	classifier := func(_ context.Context, groupID int64) (bool, error) {
		return groupID == 2, nil
	}
	cursor, continuationGroupIDs, err := newAPIKeyGroupRouteCursorWithModeIsolation(
		context.Background(), apiKey, classifier, true,
	)
	if err != nil {
		t.Fatalf("new mode-isolated cursor: %v", err)
	}
	if got := cursor.groupIDs(); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("ordinary route groups = %v, want [1 3]", got)
	}
	// Disabled ordinary groups remain in continuation lookup for accurate owner
	// diagnosis, while the mode-group namespace is absent altogether.
	if len(continuationGroupIDs) != 3 || continuationGroupIDs[0] != 1 || continuationGroupIDs[1] != 3 || continuationGroupIDs[2] != 4 {
		t.Fatalf("continuation groups = %v, want [1 3 4]", continuationGroupIDs)
	}
}

func TestModeIsolatedRouteCursorPropagatesClassificationError(t *testing.T) {
	primaryID := int64(1)
	apiKey := &service.APIKey{
		ID:      99129,
		User:    &service.User{ID: 1},
		GroupID: &primaryID,
		Group:   routeTestGroup(1),
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, Group: routeTestGroup(1)},
			{GroupID: 2, Priority: 2, Weight: 1, Enabled: true, Group: routeTestGroup(2)},
		},
	}

	lookupErr := errors.New("mode lookup unavailable")
	classifier := func(_ context.Context, groupID int64) (bool, error) {
		if groupID == 2 {
			return false, lookupErr
		}
		return false, nil
	}
	cursor, continuationGroupIDs, err := newAPIKeyGroupRouteCursorWithModeIsolation(
		context.Background(), apiKey, classifier, true,
	)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("error = %v, want wrapped lookup error", err)
	}
	if cursor != nil || continuationGroupIDs != nil {
		t.Fatalf("cursor = %v, continuation groups = %v, want nil/nil on classification error", cursor, continuationGroupIDs)
	}
}

func TestContinuationRouteCursorSupportsUngroupedAPIKey(t *testing.T) {
	apiKey := &service.APIKey{ID: 99124, User: &service.User{ID: 1}}

	continuationCursor := newAPIKeyGroupContinuationRouteCursor(apiKey)
	if got := continuationCursor.groupIDs(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("continuation route groups = %v, want [0]", got)
	}
	if got := apiKeyGroupContinuationLookupGroupIDs(apiKey); len(got) != 1 || got[0] != 0 {
		t.Fatalf("continuation lookup groups = %v, want [0]", got)
	}
	if !continuationCursor.pinToGroup(0) {
		t.Fatal("pinToGroup(0) = false, want true")
	}
	candidate, ok := continuationCursor.current()
	if !ok || candidate.APIKey != apiKey || candidate.APIKey.GroupID != nil {
		t.Fatalf("pinned ungrouped candidate = %+v, ok = %v", candidate, ok)
	}
}

func TestContinuationRouteLookupIncludesDisabledOwner(t *testing.T) {
	primaryID := int64(1)
	apiKey := &service.APIKey{
		ID:      99125,
		User:    &service.User{ID: 1},
		GroupID: &primaryID,
		Group:   routeTestGroup(1),
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, Group: routeTestGroup(1)},
			{GroupID: 2, Priority: 2, Weight: 1, Enabled: false, Group: routeTestGroup(2)},
		},
	}

	if got := apiKeyGroupContinuationLookupGroupIDs(apiKey); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("continuation lookup groups = %v, want [1 2]", got)
	}
	continuationCursor := newAPIKeyGroupContinuationRouteCursor(apiKey)
	if continuationCursor.pinToGroup(2) {
		t.Fatal("disabled continuation owner must be discoverable but not schedulable")
	}
}
