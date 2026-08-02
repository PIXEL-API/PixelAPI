package service

import "testing"

func activeGroup(id int64) *Group {
	return &Group{ID: id, Status: StatusActive, Platform: PlatformOpenAI, Hydrated: true}
}

func TestAPIKeyGroupRouteStaticallyUsable(t *testing.T) {
	t.Parallel()

	exclusive := activeGroup(10)
	exclusive.IsExclusive = true

	exclusiveSubscription := activeGroup(11)
	exclusiveSubscription.IsExclusive = true
	exclusiveSubscription.SubscriptionType = SubscriptionTypeSubscription

	inactive := activeGroup(12)
	inactive.Status = StatusDisabled

	tests := []struct {
		name  string
		user  *User
		route *APIKeyGroupRoute
		want  bool
	}{
		{
			name:  "普通启用路由可用",
			user:  &User{ID: 1},
			route: &APIKeyGroupRoute{GroupID: 9, Enabled: true, Group: activeGroup(9)},
			want:  true,
		},
		{
			name:  "路由被关闭",
			user:  &User{ID: 1},
			route: &APIKeyGroupRoute{GroupID: 9, Enabled: false, Group: activeGroup(9)},
			want:  false,
		},
		{
			name:  "分组未加载",
			user:  &User{ID: 1},
			route: &APIKeyGroupRoute{GroupID: 9, Enabled: true},
			want:  false,
		},
		{
			name:  "分组已停用",
			user:  &User{ID: 1},
			route: &APIKeyGroupRoute{GroupID: 12, Enabled: true, Group: inactive},
			want:  false,
		},
		{
			name:  "专属分组且用户已被撤销授权",
			user:  &User{ID: 1},
			route: &APIKeyGroupRoute{GroupID: 10, Enabled: true, Group: exclusive},
			want:  false,
		},
		{
			name:  "专属分组且用户仍在授权名单",
			user:  &User{ID: 1, AllowedGroups: []int64{10}},
			route: &APIKeyGroupRoute{GroupID: 10, Enabled: true, Group: exclusive},
			want:  true,
		},
		{
			// 订阅型分组的访问权由订阅有效性决定，不看 allowed_groups。
			name:  "专属订阅型分组不看授权名单",
			user:  &User{ID: 1},
			route: &APIKeyGroupRoute{GroupID: 11, Enabled: true, Group: exclusiveSubscription},
			want:  true,
		},
		{
			name:  "nil 路由",
			user:  &User{ID: 1},
			route: nil,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := APIKeyGroupRouteStaticallyUsable(tt.user, tt.route); got != tt.want {
				t.Fatalf("APIKeyGroupRouteStaticallyUsable = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAPIKeyHasUsableAlternateGroupRoute(t *testing.T) {
	t.Parallel()

	primaryID := int64(1)
	inactiveAlternate := activeGroup(2)
	inactiveAlternate.Status = StatusDisabled

	exclusiveAlternate := activeGroup(3)
	exclusiveAlternate.IsExclusive = true

	newKey := func(routes ...APIKeyGroupRoute) *APIKey {
		return &APIKey{
			ID:          100,
			User:        &User{ID: 1},
			GroupID:     &primaryID,
			GroupRoutes: routes,
		}
	}
	primaryRoute := APIKeyGroupRoute{GroupID: 1, Enabled: true, Group: activeGroup(1)}

	tests := []struct {
		name   string
		apiKey *APIKey
		want   bool
	}{
		{
			name:   "只有主分组一条路由",
			apiKey: newKey(primaryRoute),
			want:   false,
		},
		{
			name: "备用路由被关闭",
			apiKey: newKey(primaryRoute,
				APIKeyGroupRoute{GroupID: 2, Enabled: false, Group: activeGroup(2)}),
			want: false,
		},
		{
			name: "备用分组已停用",
			apiKey: newKey(primaryRoute,
				APIKeyGroupRoute{GroupID: 2, Enabled: true, Group: inactiveAlternate}),
			want: false,
		},
		{
			// 关键：不能因为「还有别的路由」就放宽授权，否则被撤销授权的专属分组会被重新用上。
			name: "备用分组是未获授权的专属分组",
			apiKey: newKey(primaryRoute,
				APIKeyGroupRoute{GroupID: 3, Enabled: true, Group: exclusiveAlternate}),
			want: false,
		},
		{
			name: "存在健康的备用路由",
			apiKey: newKey(primaryRoute,
				APIKeyGroupRoute{GroupID: 4, Enabled: true, Group: activeGroup(4)}),
			want: true,
		},
		{
			name:   "没有配置任何路由",
			apiKey: newKey(),
			want:   false,
		},
		{
			name:   "nil",
			apiKey: nil,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := APIKeyHasUsableAlternateGroupRoute(tt.apiKey); got != tt.want {
				t.Fatalf("APIKeyHasUsableAlternateGroupRoute = %v, want %v", got, tt.want)
			}
		})
	}
}
