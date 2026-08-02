package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// SettingHandler 公开设置处理器（无需认证）
type SettingHandler struct {
	settingService *service.SettingService
	version        string
}

// NewSettingHandler 创建公开设置处理器
func NewSettingHandler(settingService *service.SettingService, version string) *SettingHandler {
	return &SettingHandler{
		settingService: settingService,
		version:        version,
	}
}

// GetPublicSettings 获取公开设置
// GET /api/v1/settings/public
func (h *SettingHandler) GetPublicSettings(c *gin.Context) {
	settings, err := h.settingService.GetPublicSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.PublicSettings{
		RegistrationEnabled:              settings.RegistrationEnabled,
		EmailVerifyEnabled:               settings.EmailVerifyEnabled,
		ForceEmailOnThirdPartySignup:     settings.ForceEmailOnThirdPartySignup,
		RegistrationEmailSuffixWhitelist: settings.RegistrationEmailSuffixWhitelist,
		PromoCodeEnabled:                 settings.PromoCodeEnabled,
		PasswordResetEnabled:             settings.PasswordResetEnabled,
		InvitationCodeEnabled:            settings.InvitationCodeEnabled,
		TotpEnabled:                      settings.TotpEnabled,
		LoginAgreementEnabled:            settings.LoginAgreementEnabled,
		LoginAgreementMode:               settings.LoginAgreementMode,
		LoginAgreementUpdatedAt:          settings.LoginAgreementUpdatedAt,
		LoginAgreementRevision:           settings.LoginAgreementRevision,
		LoginAgreementDocuments:          loginAgreementDocumentsToDTO(settings.LoginAgreementDocuments),
		TurnstileEnabled:                 settings.TurnstileEnabled,
		TurnstileSiteKey:                 settings.TurnstileSiteKey,
		SiteName:                         settings.SiteName,
		// 下发派生 URL 而非原始 data URI，与 SSR 注入出口保持一致。
		// 两个出口必须成对修改——它们的差分测试只比对字段名，不比对值。
		SiteLogo:                    settings.SiteLogoURL,
		SiteSubtitle:                settings.SiteSubtitle,
		APIBaseURL:                  settings.APIBaseURL,
		ContactInfo:                 settings.ContactInfo,
		DocURL:                      settings.DocURL,
		HomeContent:                 settings.HomeContent,
		HideCcsImportButton:         settings.HideCcsImportButton,
		PurchaseSubscriptionEnabled: settings.PurchaseSubscriptionEnabled,
		PurchaseSubscriptionURL:     settings.PurchaseSubscriptionURL,
		TableDefaultPageSize:        settings.TableDefaultPageSize,
		TablePageSizeOptions:        settings.TablePageSizeOptions,
		CustomMenuItems:             dto.ParseUserVisibleMenuItems(settings.CustomMenuItems),
		CustomEndpoints:             dto.ParseCustomEndpoints(settings.CustomEndpoints),
		LinuxDoOAuthEnabled:         settings.LinuxDoOAuthEnabled,
		WeChatOAuthEnabled:          settings.WeChatOAuthEnabled,
		WeChatOAuthOpenEnabled:      settings.WeChatOAuthOpenEnabled,
		WeChatOAuthMPEnabled:        settings.WeChatOAuthMPEnabled,
		WeChatOAuthMobileEnabled:    settings.WeChatOAuthMobileEnabled,
		OIDCOAuthEnabled:            settings.OIDCOAuthEnabled,
		OIDCOAuthProviderName:       settings.OIDCOAuthProviderName,
		BackendModeEnabled:          settings.BackendModeEnabled,
		PaymentEnabled:              settings.PaymentEnabled,
		Version:                     h.version,
		BalanceLowNotifyEnabled:     settings.BalanceLowNotifyEnabled,
		AccountQuotaNotifyEnabled:   settings.AccountQuotaNotifyEnabled,
		BalanceLowNotifyThreshold:   settings.BalanceLowNotifyThreshold,
		BalanceLowNotifyRechargeURL: settings.BalanceLowNotifyRechargeURL,

		ChannelMonitorEnabled:                settings.ChannelMonitorEnabled,
		ChannelMonitorDefaultIntervalSeconds: settings.ChannelMonitorDefaultIntervalSeconds,

		AvailableChannelsEnabled: settings.AvailableChannelsEnabled,

		UserAccountImportLimit: settings.UserAccountImportLimit,

		OpenAIAccountLevels: openAIAccountLevelsToDTO(settings.OpenAIAccountLevels),

		AffiliateEnabled:                settings.AffiliateEnabled,
		UserPrivateGroupCommissionRate:  settings.UserPrivateGroupCommissionRate,
		RiskControlEnabled:              settings.RiskControlEnabled,
		InvoiceManagementEnabled:        settings.InvoiceManagementEnabled,
		WithdrawalManagementEnabled:     settings.WithdrawalManagementEnabled,
		WithdrawalRateLimitWindowDays:   settings.WithdrawalRateLimitWindowDays,
		WithdrawalRateLimitMax:          settings.WithdrawalRateLimitMax,
		WithdrawalRateLimitExemptAmount: settings.WithdrawalRateLimitExemptAmount,
	})
}

func openAIAccountLevelsToDTO(levels []service.OpenAIAccountLevelConfig) []dto.OpenAIAccountLevelConfig {
	normalized := service.OpenAIAccountLevelConfigSelectable(levels)
	out := make([]dto.OpenAIAccountLevelConfig, 0, len(normalized))
	for _, level := range normalized {
		out = append(out, dto.OpenAIAccountLevelConfig{
			Key:                level.Key,
			Label:              level.Label,
			Aliases:            append([]string(nil), level.Aliases...),
			SortOrder:          level.SortOrder,
			Enabled:            level.Enabled,
			RequiresProxyLogin: level.RequiresProxyLogin,
		})
	}
	return out
}

// loginAgreementDocumentsToDTO 转换条款文档列表。
//
// ContentMD 刻意不下发（恒为空串）：四篇条款的正文合计约 43KB，而登录页、
// 注册页与同意提示只需要 id 与 title。正文改由
// GET /api/v1/settings/legal-documents/:id 按需获取。
// 字段保留而非删除，见 service.loginAgreementDocumentsWithoutContent 的说明。
func loginAgreementDocumentsToDTO(docs []service.LoginAgreementDocument) []dto.LoginAgreementDocument {
	out := make([]dto.LoginAgreementDocument, 0, len(docs))
	for _, doc := range docs {
		out = append(out, dto.LoginAgreementDocument{
			ID:    doc.ID,
			Title: doc.Title,
		})
	}
	return out
}

// ServeSiteLogo 提供站点 logo 图片本体。
// GET /brand/site-logo?v=<hash>
//
// 刻意不放在 /api/ 前缀下：生产边缘对 /api/ 统一 BYPASS 缓存，非 /api 路径才会
// 命中。配合 URL 里的内容哈希，这里可以安全地发 immutable。
func (h *SettingHandler) ServeSiteLogo(c *gin.Context) {
	asset, err := h.settingService.GetSiteLogoAsset(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if asset == nil {
		// 未配置或不可解码。返回 404 而不是兜底图：公开设置在这种情况下
		// 下发的是空串，前端不会请求到这里；真到这里说明是脏 URL。
		c.Status(http.StatusNotFound)
		c.Abort()
		return
	}

	etag := `"` + asset.Hash + `"`
	if match := c.GetHeader("If-None-Match"); match == etag {
		c.Status(http.StatusNotModified)
		c.Abort()
		return
	}

	header := c.Writer.Header()
	header.Set("ETag", etag)
	// URL 携带内容哈希，同一 URL 的内容不可能变，可以安全地长缓存。
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	// 图片以附件语义之外的方式直出，禁止浏览器猜测类型。
	header.Set("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, asset.ContentType, asset.Bytes)
	c.Abort()
}

// GetLegalDocument 按需返回单篇条款正文。
// GET /api/v1/settings/legal-documents/:id
func (h *SettingHandler) GetLegalDocument(c *gin.Context) {
	doc, err := h.settingService.FindLoginAgreementDocument(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if doc == nil {
		response.NotFound(c, "document not found")
		return
	}
	response.Success(c, dto.LoginAgreementDocument{
		ID:        doc.ID,
		Title:     doc.Title,
		ContentMD: doc.ContentMD,
	})
}
