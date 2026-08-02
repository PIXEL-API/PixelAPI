package service

// 多分组路由的「静态可用性」判定。
//
// 拆成静态/动态两层是刻意的：
//   - 静态维度（本文件）：路由启用、分组存在且未停用、用户对专属分组的授权仍在。
//     这些只依赖已经随鉴权快照加载好的数据，零额外查询，因此可以在鉴权中间件的
//     热路径上对整条路由链求值。
//   - 动态维度（订阅、余额、限额、RPM）：由 handler 在路由循环里逐条调用
//     CheckBillingEligibility 判定，失败就换下一条路由。放到中间件里对每条路由
//     预判会把一次鉴权放大成 N 次订阅查询。
//
// 中间件与 handler 必须共用同一套静态规则，否则会出现「中间件放行、handler 又把
// 所有候选过滤空」这类两头不一致的 503。

// APIKeyGroupRouteStaticallyUsable 判定单条路由在静态维度上是否仍可用。
func APIKeyGroupRouteStaticallyUsable(user *User, route *APIKeyGroupRoute) bool {
	if route == nil || !route.Enabled || route.GroupID <= 0 {
		return false
	}
	group := route.Group
	if group == nil {
		return false
	}
	if !group.IsActive() {
		return false
	}
	return GroupAuthorizedForUser(user, group)
}

// GroupAuthorizedForUser 复核用户对某分组的访问授权。
//
// 与鉴权中间件的专属分组复核同源，两处必须共用，避免规则漂移导致
// 「主分组被拦、备用分组却绕过授权」的越权。
//
// 放行条件：
//   - 订阅型分组：访问权由订阅有效性决定，不看 allowed_groups
//     （自研的 user_private_group 是「专属 + 订阅型」，属主也在 allowed_groups 里，两条路径都放行）；
//   - 非专属分组：所有用户可用；
//   - 专属分组：用户的 allowed_groups 中必须仍包含该分组。
//
// user 为空属于信息缺失而非越权证据，交由既有分支处理，这里不越权拦截——
// 在鉴权热路径上 fail-closed 的误判会直接变成全站 403。
func GroupAuthorizedForUser(user *User, group *Group) bool {
	if group == nil {
		return false
	}
	if group.IsSubscriptionType() {
		return true
	}
	if user == nil {
		return true
	}
	return user.CanBindGroup(group.ID, group.IsExclusive)
}

// APIKeyHasUsableAlternateGroupRoute 判断除主分组外，是否还有静态可用的路由。
//
// 中间件用它决定「主分组这一条判定不过时，是就地终结请求，还是放行交给 handler
// 的路由循环逐条尝试」。返回 false 时保持原有的就地 403/429 语义，单分组 Key
// 的行为完全不变。
func APIKeyHasUsableAlternateGroupRoute(apiKey *APIKey) bool {
	if apiKey == nil || len(apiKey.GroupRoutes) == 0 {
		return false
	}
	var primaryGroupID int64
	if apiKey.GroupID != nil {
		primaryGroupID = *apiKey.GroupID
	}
	for i := range apiKey.GroupRoutes {
		route := &apiKey.GroupRoutes[i]
		if route.GroupID == primaryGroupID {
			continue
		}
		if APIKeyGroupRouteStaticallyUsable(apiKey.User, route) {
			return true
		}
	}
	return false
}
