package handler

import (
	"errors"
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
		{"订阅过期", service.ErrSubscriptionExpired, true},
		{"日限额超限", service.ErrDailyLimitExceeded, true},
		{"周限额超限", service.ErrWeeklyLimitExceeded, true},
		{"月限额超限", service.ErrMonthlyLimitExceeded, true},
		{"分组RPM超限", service.ErrGroupRPMExceeded, true},
		// 余额不足也是路由相关的：下一条若是订阅型分组就不吃余额。
		{"余额不足", service.ErrInsufficientBalance, true},

		// 与 Key/用户/服务绑定的失败：换路由救不了，不该白白遍历整条链。
		{"计费服务不可用", service.ErrBillingServiceUnavailable, false},
		{"订阅仓储不可用", service.ErrSubscriptionRepositoryUnavailable, false},
		{"Key5h限额", service.ErrAPIKeyRateLimit5hExceeded, false},
		{"Key日限额", service.ErrAPIKeyRateLimit1dExceeded, false},
		{"Key7d限额", service.ErrAPIKeyRateLimit7dExceeded, false},
		{"用户RPM超限", service.ErrUserRPMExceeded, false},

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
