package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
)

// AdminHandlers contains all admin-related HTTP handlers
type AdminHandlers struct {
	Dashboard              *admin.DashboardHandler
	User                   *admin.UserHandler
	Group                  *admin.GroupHandler
	Account                *admin.AccountHandler
	AccountSharePolicy     *admin.AccountSharePolicyHandler
	Announcement           *admin.AnnouncementHandler
	Conversation           *admin.ConversationHandler
	DataManagement         *admin.DataManagementHandler
	Backup                 *admin.BackupHandler
	OAuth                  *admin.OAuthHandler
	OpenAIOAuth            *admin.OpenAIOAuthHandler
	GeminiOAuth            *admin.GeminiOAuthHandler
	AntigravityOAuth       *admin.AntigravityOAuthHandler
	GrokOAuth              *admin.GrokOAuthHandler
	Proxy                  *admin.ProxyHandler
	Redeem                 *admin.RedeemHandler
	Promo                  *admin.PromoHandler
	Setting                *admin.SettingHandler
	Ops                    *admin.OpsHandler
	Cluster                *admin.ClusterHandler
	System                 *admin.SystemHandler
	Subscription           *admin.SubscriptionHandler
	Usage                  *admin.UsageHandler
	UserAttribute          *admin.UserAttributeHandler
	ErrorPassthrough       *admin.ErrorPassthroughHandler
	TLSFingerprintProfile  *admin.TLSFingerprintProfileHandler
	APIKey                 *admin.AdminAPIKeyHandler
	ScheduledTest          *admin.ScheduledTestHandler
	Channel                *admin.ChannelHandler
	ChannelMonitor         *admin.ChannelMonitorHandler
	ChannelMonitorTemplate *admin.ChannelMonitorRequestTemplateHandler
	ContentModeration      *admin.ContentModerationHandler
	CyberPolicy            *admin.CyberPolicyHandler
	Payment                *admin.PaymentHandler
	Revenue                *admin.RevenueHandler
	Withdrawal             *admin.WithdrawalHandler
	Invoice                *admin.InvoiceHandler
	Shop                   *admin.ShopHandler
	Affiliate              *admin.AffiliateHandler
	Activity               *admin.ActivityHandler
	Ideas                  *admin.IdeasHandler
}

// Handlers contains all HTTP handlers
type Handlers struct {
	Auth             *AuthHandler
	OIDCProvider     *OIDCProviderHandler
	User             *UserHandler
	APIKey           *APIKeyHandler
	AccountShareMode *AccountShareModeHandler
	UserAccount      *UserAccountHandler
	Usage            *UsageHandler
	Redeem           *RedeemHandler
	Subscription     *SubscriptionHandler
	Announcement     *AnnouncementHandler
	Conversation     *ConversationHandler
	ChannelMonitor   *ChannelMonitorUserHandler
	Admin            *AdminHandlers
	Gateway          *GatewayHandler
	OpenAIGateway    *OpenAIGatewayHandler
	Setting          *SettingHandler
	Totp             *TotpHandler
	Payment          *PaymentHandler
	PaymentWebhook   *PaymentWebhookHandler
	AvailableChannel *AvailableChannelHandler
	ReceiptCode      *ReceiptCodeHandler
	Withdrawal       *WithdrawalHandler
	Invoice          *InvoiceHandler
	Shop             *ShopHandler
	Activity         *ActivityHandler
	Ideas            *IdeasHandler
}

// BuildInfo contains build-time information
type BuildInfo struct {
	Version   string
	BuildType string // "source" for manual builds, "release" for CI builds
	Commit    string
	Date      string
}
