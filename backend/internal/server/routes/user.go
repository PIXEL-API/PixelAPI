package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterUserRoutes 注册用户相关路由（需要认证）
func RegisterUserRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	settingService *service.SettingService,
	panelRL *middleware.PanelRateLimiter,
) {
	public := v1.Group("/public")
	{
		usage := public.Group("/usage")
		{
			usage.GET("/today", h.Usage.PublicTodayStats)
		}
	}

	shopPublic := v1.Group("/shop")
	{
		shopPublic.GET("/categories", h.Shop.ListCategories)
		shopPublic.GET("/products", h.Shop.ListProducts)
		shopPublic.GET("/products/:id", h.Shop.GetProduct)
	}

	authenticated := v1.Group("")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	// 全局宽松档：覆盖所有登录后端点，按用户 ID 分桶。
	// 必须挂在 jwtAuth 之后——限流依赖上下文里的认证主体。
	if panelRL != nil {
		authenticated.Use(panelRL.Global())
	}
	shop := authenticated.Group("/shop")
	{
		shop.GET("/draw-progress", h.Shop.ListDrawProgress)
		shop.POST("/orders", h.Shop.CreateOrder)
		shop.GET("/orders/:id", h.Shop.GetOrder)
		shop.GET("/orders/:id/files/download.zip", h.Shop.DownloadOrderFilesZip)
		shop.GET("/orders/:id/files/:card_id/download", h.Shop.DownloadOrderFile)
	}
	activities := authenticated.Group("/activities")
	{
		activities.GET("", h.Activity.ListWelfareActivities)
		activities.GET("/winners", h.Activity.ListMyWinners)
		activities.GET("/:id/public-winners", h.Activity.ListPublicWinners)
		activities.POST("/:id/join", h.Activity.JoinDraw)
		activities.POST("/winners/:id/claim", h.Activity.SubmitWinnerClaim)
	}
	{
		// 用户接口
		user := authenticated.Group("/user")
		{
			user.GET("/profile", h.User.GetProfile)
			user.PUT("/password", h.User.ChangePassword)
			user.PUT("", h.User.UpdateProfile)
			user.GET("/receipt-code", h.ReceiptCode.Get)
			user.POST("/receipt-code", h.ReceiptCode.Upload)
			user.DELETE("/receipt-code", h.ReceiptCode.Delete)
			user.GET("/withdrawals", h.Withdrawal.ListMine)
			user.POST("/withdrawals", h.Withdrawal.Submit)
			user.POST("/withdrawals/:id/cancel", h.Withdrawal.Cancel)
			user.GET("/invoices/profiles", h.Invoice.ListProfiles)
			user.POST("/invoices/profiles", h.Invoice.CreateProfile)
			user.PUT("/invoices/profiles/:id", h.Invoice.UpdateProfile)
			user.DELETE("/invoices/profiles/:id", h.Invoice.DeleteProfile)
			user.POST("/invoices/profiles/:id/default", h.Invoice.SetDefaultProfile)
			user.GET("/invoices/eligible-sources", h.Invoice.ListEligibleSources)
			user.GET("/invoices/requests", h.Invoice.ListRequests)
			user.POST("/invoices/requests", h.Invoice.CreateRequest)
			user.GET("/invoices/requests/:id", h.Invoice.GetRequest)
			user.POST("/invoices/requests/:id/cancel", h.Invoice.CancelRequest)
			user.GET("/aff/share", h.User.GetAffiliateShare)
			user.GET("/aff", h.User.GetAffiliate)
			user.POST("/aff/transfer", h.User.TransferAffiliateQuota)
			user.POST("/account-bindings/email/send-code", h.User.SendEmailBindingCode)
			user.POST("/account-bindings/email", h.User.BindEmailIdentity)
			user.DELETE("/account-bindings/:provider", h.User.UnbindIdentity)
			user.POST("/auth-identities/bind/start", h.User.StartIdentityBinding)

			// 通知邮箱管理
			notifyEmail := user.Group("/notify-email")
			{
				notifyEmail.POST("/send-code", h.User.SendNotifyEmailCode)
				notifyEmail.POST("/verify", h.User.VerifyNotifyEmail)
				notifyEmail.PUT("/toggle", h.User.ToggleNotifyEmail)
				notifyEmail.DELETE("", h.User.RemoveNotifyEmail)
			}

			// TOTP 双因素认证
			totp := user.Group("/totp")
			{
				totp.GET("/status", h.Totp.GetStatus)
				totp.GET("/verification-method", h.Totp.GetVerificationMethod)
				totp.POST("/send-code", h.Totp.SendVerifyCode)
				totp.POST("/setup", h.Totp.InitiateSetup)
				totp.POST("/enable", h.Totp.Enable)
				totp.POST("/disable", h.Totp.Disable)
			}
		}

		// API Key管理
		keys := authenticated.Group("/keys")
		{
			keys.GET("", h.APIKey.List)
			keys.GET("/:id", h.APIKey.GetByID)
			keys.POST("", h.APIKey.Create)
			keys.PUT("/:id", h.APIKey.Update)
			keys.DELETE("/:id", h.APIKey.Delete)
		}

		accounts := authenticated.Group("/accounts")
		{
			// 严格档只挂在聚合统计与全量导出这些真正重的读端点上，
			// 不整组套用——本组还有大量轻量 CRUD，整组限流会误伤正常操作。
			heavy := noopMiddleware
			if panelRL != nil {
				heavy = panelRL.Heavy()
			}
			accounts.GET("", h.UserAccount.List)
			accounts.GET("/quota-dashboard", heavy, h.UserAccount.GetQuotaPoolDashboard)
			accounts.GET("/data", heavy, h.UserAccount.ExportData)
			accounts.POST("/today-stats/batch", heavy, h.UserAccount.GetBatchTodayStats)
			accounts.GET("/:id/usage", heavy, h.UserAccount.GetUsage)
			accounts.GET("/:id/openai-quota", h.UserAccount.QueryOpenAIQuota)
			accounts.POST("/:id/openai-quota/reset", h.UserAccount.ResetOpenAIQuota)
			accounts.GET("/:id/stats", heavy, h.UserAccount.GetStats)
			accounts.GET("/:id/today-stats", heavy, h.UserAccount.GetTodayStats)
			accounts.GET("/:id/moderation/config", h.UserAccount.GetModerationConfig)
			accounts.PUT("/:id/moderation/config", h.UserAccount.UpdateModerationConfig)
			accounts.POST("/:id/moderation/test", h.UserAccount.TestModeration)
			accounts.GET("/:id/moderation/logs", h.UserAccount.ListModerationLogs)
			accounts.GET("/:id", h.UserAccount.GetByID)
			accounts.POST("", h.UserAccount.Create)
			accounts.POST("/import", h.UserAccount.Import)
			accounts.POST("/import-credentials", h.UserAccount.ImportCredentials)
			accounts.POST("/bulk-update", h.UserAccount.BulkUpdate)
			accounts.POST("/bulk-delete", h.UserAccount.BulkDelete)
			accounts.POST("/batch-refresh/async", h.UserAccount.CreateBatchRefreshTask)
			accounts.POST("/batch-test/async", h.UserAccount.CreateBatchTestConnectionTask)
			accounts.POST("/batch-revalidate-public-share/async", h.UserAccount.CreateBatchRevalidatePublicShareTask)
			accounts.GET("/batch-tasks/:task_id", h.UserAccount.GetBatchTask)
			accounts.POST("/external-placement:convert-batch", h.UserAccount.ConvertExternalPlacementBatch)
			accounts.POST("/:id/test", h.UserAccount.Test)
			accounts.GET("/:id/models", h.UserAccount.GetAvailableModels)
			accounts.POST("/:id/recover-state", h.UserAccount.RecoverState)
			accounts.POST("/:id/refresh", h.UserAccount.Refresh)
			accounts.POST("/:id/set-privacy", h.UserAccount.SetPrivacy)
			accounts.POST("/:id/revalidate-public-share", h.UserAccount.RevalidatePublicShare)
			accounts.POST("/:id/external-placement:convert", h.UserAccount.ConvertExternalPlacement)
			accounts.PUT("/:id", h.UserAccount.Update)
			accounts.DELETE("/:id", h.UserAccount.Delete)
		}

		// User-scoped OAuth endpoints for creating personal accounts.
		accountOAuth := authenticated.Group("/account-oauth")
		{
			accountOAuth.POST("/anthropic/auth-url", h.UserAccount.GenerateAnthropicOAuthURL)
			accountOAuth.POST("/anthropic/exchange-code", h.UserAccount.ExchangeAnthropicOAuthCode)
			accountOAuth.POST("/anthropic/setup-token/auth-url", h.UserAccount.GenerateAnthropicSetupTokenURL)
			accountOAuth.POST("/anthropic/setup-token/exchange-code", h.UserAccount.ExchangeAnthropicSetupTokenCode)
			accountOAuth.POST("/anthropic/cookie-auth", h.UserAccount.AnthropicCookieAuth)
			accountOAuth.POST("/anthropic/setup-token-cookie-auth", h.UserAccount.AnthropicSetupTokenCookieAuth)
			accountOAuth.POST("/openai/auth-url", h.UserAccount.GenerateOpenAIOAuthURL)
			accountOAuth.POST("/openai/exchange-code", h.UserAccount.ExchangeOpenAIOAuthCode)
			accountOAuth.POST("/openai/refresh-token", h.UserAccount.RefreshOpenAIToken)
			accountOAuth.GET("/gemini/capabilities", h.UserAccount.GetGeminiOAuthCapabilities)
			accountOAuth.POST("/gemini/auth-url", h.UserAccount.GenerateGeminiOAuthURL)
			accountOAuth.POST("/gemini/exchange-code", h.UserAccount.ExchangeGeminiOAuthCode)
			accountOAuth.POST("/antigravity/auth-url", h.UserAccount.GenerateAntigravityOAuthURL)
			accountOAuth.POST("/antigravity/exchange-code", h.UserAccount.ExchangeAntigravityOAuthCode)
			accountOAuth.POST("/antigravity/refresh-token", h.UserAccount.RefreshAntigravityToken)
			accountOAuth.POST("/grok/auth-url", h.UserAccount.GenerateGrokOAuthURL)
			accountOAuth.POST("/grok/exchange-code", h.UserAccount.ExchangeGrokOAuthCode)
			accountOAuth.POST("/grok/refresh-token", h.UserAccount.RefreshGrokToken)
		}

		accountShare := authenticated.Group("/account-share")
		{
			accountShare.GET("/mode-groups", h.AccountShareMode.ListModeGroups)
			accountShare.GET("/me/capabilities", h.AccountShareMode.GetCapabilities)
			accountShare.POST("/openai/auth-url", h.AccountShareMode.GenerateOpenAIAuthURL)
			accountShare.POST("/openai/exchange-code", h.AccountShareMode.ExchangeOpenAICode)
			accountShare.POST("/anthropic/auth-url", h.AccountShareMode.GenerateAnthropicAuthURL)
			accountShare.POST("/anthropic/exchange-code", h.AccountShareMode.ExchangeAnthropicCode)
			// 用户不再上传/管理代理，只能选择平台代理；仅保留只读的可选代理列表。
			accountShare.GET("/proxies", h.AccountShareMode.ListAvailableProxies)
			accountShare.POST("/rooms", h.AccountShareMode.CreateRoom)
			accountShare.GET("/listings", h.AccountShareMode.ListListings)
			accountShare.GET("/history/memberships", h.AccountShareMode.ListMembershipHistory)
			accountShare.GET("/recommendations/usage-profile", h.AccountShareMode.GetRecommendationUsageProfile)
			accountShare.POST("/recommendations", h.AccountShareMode.RecommendListings)
			accountShare.GET("/listings/:id", h.AccountShareMode.GetListing)
			accountShare.GET("/listings/:id/management-state", h.AccountShareMode.GetRoomManagementState)
			accountShare.GET("/listings/:id/accounts", h.AccountShareMode.ListRoomAccounts)
			accountShare.POST("/listings/:id/accounts/attach-batch", h.AccountShareMode.AttachRoomAccounts)
			accountShare.POST("/listings/:id/accounts/detach-batch", h.AccountShareMode.DetachRoomAccounts)
			accountShare.GET("/listings/:id/my-spend", h.AccountShareMode.GetMySpendSummary)
			accountShare.GET("/listings/:id/reviews", h.AccountShareMode.ListListingReviews)
			accountShare.GET("/owners/:owner_id/reviews", h.AccountShareMode.ListOwnerReviews)
			accountShare.POST("/listings/:id/edit-session", h.AccountShareMode.BeginListingEdit)
			accountShare.POST("/listings/:id/edit-session/release", h.AccountShareMode.ReleaseListingEdit)
			accountShare.PATCH("/listings/:id", h.AccountShareMode.UpdateListing)
			accountShare.POST("/listings/:id/drain", h.AccountShareMode.DrainRoom)
			accountShare.POST("/listings/:id/activate", h.AccountShareMode.ActivateRoom)
			accountShare.POST("/listings/:id/suspend", h.AccountShareMode.SuspendRoom)
			accountShare.POST("/listings/:id/delete-intent", h.AccountShareMode.CreateRoomDeleteIntent)
			accountShare.DELETE("/listings/:id", h.AccountShareMode.DeleteRoom)
			accountShare.POST("/listings/:id/join-intent", h.AccountShareMode.CreateJoinIntent)
			accountShare.POST("/listings/:id/join", h.AccountShareMode.JoinListing)
			accountShare.GET("/room-operations/:operation_id", h.AccountShareMode.GetRoomOperation)
			accountShare.GET("/api-key-bindings/:apiKeyID/status", h.AccountShareMode.GetAPIKeyBindingStatus)
			accountShare.GET("/queue/:apiKeyID", h.AccountShareMode.ListMembershipQueue)
			accountShare.PATCH("/queue", h.AccountShareMode.ReorderMembershipQueue)
			accountShare.PATCH("/memberships/:id/idle-timeout", h.AccountShareMode.UpdateMembershipIdleTimeout)
			accountShare.POST("/memberships/:id/end-intent", h.AccountShareMode.CreateEndMembershipIntent)
			accountShare.POST("/memberships/:id/end", h.AccountShareMode.EndMembership)
			accountShare.POST("/memberships/:id/review", h.AccountShareMode.SubmitReview)
		}

		// 用户可用分组（非管理员接口）
		groups := authenticated.Group("/groups")
		{
			groups.GET("/available", h.APIKey.GetAvailableGroups)
			groups.GET("/rates", h.APIKey.GetUserGroupRates)
		}

		// 用户可用渠道（非管理员接口）
		channels := authenticated.Group("/channels")
		{
			channels.GET("/available", h.AvailableChannel.List)
		}

		// 使用记录
		// 严格档：这一组全是聚合统计重查询，是打爆数据库最容易的入口。
		usage := authenticated.Group("/usage")
		if panelRL != nil {
			usage.Use(panelRL.Heavy())
		}
		{
			usage.GET("", h.Usage.List)
			usage.GET("/balance-ledger/stats", h.Usage.BalanceLedgerStats)
			usage.GET("/balance-ledger", h.Usage.ListBalanceLedger)
			usage.GET("/stats", h.Usage.Stats)
			// User dashboard endpoints
			usage.GET("/dashboard/stats", h.Usage.DashboardStats)
			usage.GET("/dashboard/trend", h.Usage.DashboardTrend)
			usage.GET("/dashboard/models", h.Usage.DashboardModels)
			usage.GET("/dashboard/account-sharing", h.Usage.DashboardAccountSharing)
			usage.POST("/dashboard/api-keys-usage", h.Usage.DashboardAPIKeysUsage)
			usage.GET("/:id", h.Usage.GetByID)
		}

		// 公告（用户可见）
		announcements := authenticated.Group("/announcements")
		{
			announcements.GET("", h.Announcement.List)
			announcements.POST("/:id/read", h.Announcement.MarkRead)
		}

		// 工单服务
		conversations := authenticated.Group("/conversations")
		{
			conversations.GET("", h.Conversation.List)
			conversations.POST("", h.Conversation.Create)
			conversations.GET("/unread-count", h.Conversation.UnreadCount)
			conversations.GET("/:id", h.Conversation.Get)
			conversations.GET("/:id/messages", h.Conversation.ListMessages)
			conversations.POST("/:id/messages", h.Conversation.AddMessage)
			conversations.POST("/:id/read", h.Conversation.MarkRead)
			conversations.POST("/:id/close", h.Conversation.Close)
		}

		// 卡密兑换
		redeem := authenticated.Group("/redeem")
		{
			redeem.POST("", h.Redeem.Redeem)
			redeem.GET("/history", h.Redeem.GetHistory)
		}

		// 用户订阅
		subscriptions := authenticated.Group("/subscriptions")
		{
			subscriptions.GET("", h.Subscription.List)
			subscriptions.GET("/active", h.Subscription.GetActive)
			subscriptions.GET("/progress", h.Subscription.GetProgress)
			subscriptions.GET("/summary", h.Subscription.GetSummary)
		}

		// 渠道监控（用户只读）
		monitors := authenticated.Group("/channel-monitors")
		{
			monitors.GET("", h.ChannelMonitor.List)
			monitors.GET("/capacity-summary", h.ChannelMonitor.CapacitySummary)
			monitors.GET("/:id/status", h.ChannelMonitor.GetStatus)
		}
	}
}
