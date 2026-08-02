//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type settingHandlerPublicRepoStub struct {
	values map[string]string
}

func (s *settingHandlerPublicRepoStub) Get(ctx context.Context, key string) (*service.Setting, error) {
	panic("unexpected Get call")
}

func (s *settingHandlerPublicRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *settingHandlerPublicRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *settingHandlerPublicRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *settingHandlerPublicRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *settingHandlerPublicRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *settingHandlerPublicRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func TestSettingHandler_GetPublicSettings_ExposesForceEmailOnThirdPartySignup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &settingHandlerPublicRepoStub{
		values: map[string]string{
			service.SettingKeyForceEmailOnThirdPartySignup: "true",
		},
	}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test-version")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)

	h.GetPublicSettings(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			ForceEmailOnThirdPartySignup bool `json:"force_email_on_third_party_signup"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.True(t, resp.Data.ForceEmailOnThirdPartySignup)
}

func TestSettingHandler_GetPublicSettings_ExposesLoginAgreement(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &settingHandlerPublicRepoStub{
		values: map[string]string{
			service.SettingKeyLoginAgreementEnabled:   "true",
			service.SettingKeyLoginAgreementMode:      "checkbox",
			service.SettingKeyLoginAgreementUpdatedAt: "2026/04/26",
			service.SettingKeyLoginAgreementDocuments: `[{"id":"terms","title":"服务条款","content_md":"# 服务条款"}]`,
		},
	}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test-version")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)

	h.GetPublicSettings(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			LoginAgreementEnabled   bool   `json:"login_agreement_enabled"`
			LoginAgreementMode      string `json:"login_agreement_mode"`
			LoginAgreementUpdatedAt string `json:"login_agreement_updated_at"`
			LoginAgreementRevision  string `json:"login_agreement_revision"`
			LoginAgreementDocuments []struct {
				ID        string `json:"id"`
				Title     string `json:"title"`
				ContentMD string `json:"content_md"`
			} `json:"login_agreement_documents"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.True(t, resp.Data.LoginAgreementEnabled)
	require.Equal(t, "checkbox", resp.Data.LoginAgreementMode)
	require.Equal(t, "2026/04/26", resp.Data.LoginAgreementUpdatedAt)
	require.NotEmpty(t, resp.Data.LoginAgreementRevision)
	require.Len(t, resp.Data.LoginAgreementDocuments, 1)
	require.Equal(t, "terms", resp.Data.LoginAgreementDocuments[0].ID)
	require.Equal(t, "服务条款", resp.Data.LoginAgreementDocuments[0].Title)
	// 正文不再随公开设置下发：四篇条款合计约 43KB，而登录/注册页只用 id 与 title。
	// 正文改由 GET /api/v1/settings/legal-documents/:id 按需获取。
	require.Empty(t, resp.Data.LoginAgreementDocuments[0].ContentMD,
		"公开设置不得携带条款正文")
	// 金标：revision 必须仍由**含正文**的完整文档算出。
	//
	// 这个硬编码值是剥离正文之前的实际线上取值。它是登录门禁（auth_handler 比对）
	// 与前端 localStorage 同意态的键——一旦改变，全体老用户会被要求重新同意条款。
	// 如果你因为改动 revision 算法而让这条失败，请先确认这是有意的产品决策。
	require.Equal(t, "0e0ba7f85f29a165", resp.Data.LoginAgreementRevision,
		"revision 必须与剥离正文之前逐字节一致，否则全体用户会被要求重新同意条款")
}

func TestSettingHandler_GetPublicSettings_ExposesWeChatOAuthModeCapabilities(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(service.NewSettingService(&settingHandlerPublicRepoStub{
		values: map[string]string{
			service.SettingKeyWeChatConnectEnabled:             "true",
			service.SettingKeyWeChatConnectAppID:               "wx-mp-app",
			service.SettingKeyWeChatConnectAppSecret:           "wx-mp-secret",
			service.SettingKeyWeChatConnectMode:                "mp",
			service.SettingKeyWeChatConnectScopes:              "snsapi_base",
			service.SettingKeyWeChatConnectOpenEnabled:         "true",
			service.SettingKeyWeChatConnectMPEnabled:           "true",
			service.SettingKeyWeChatConnectRedirectURL:         "https://api.example.com/api/v1/auth/oauth/wechat/callback",
			service.SettingKeyWeChatConnectFrontendRedirectURL: "/auth/wechat/callback",
		},
	}, &config.Config{}), "test-version")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)

	h.GetPublicSettings(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			WeChatOAuthEnabled     bool `json:"wechat_oauth_enabled"`
			WeChatOAuthOpenEnabled bool `json:"wechat_oauth_open_enabled"`
			WeChatOAuthMPEnabled   bool `json:"wechat_oauth_mp_enabled"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.True(t, resp.Data.WeChatOAuthEnabled)
	require.True(t, resp.Data.WeChatOAuthOpenEnabled)
	require.True(t, resp.Data.WeChatOAuthMPEnabled)
}

// TestSettingHandler_PublicSettings_SiteLogoIsDerivedURL 守护「两个公开出口下发同一个值」。
//
// 为什么单独写这条：dto.PublicSettings 与 service.PublicSettingsInjectionPayload 的
// 差分测试只比对 JSON **字段名**，不比对值。本次瘦身把 site_logo 从 base64 data URI
// 换成派生 URL 时，就出现过只改了 SSR 注入出口、漏改 HTTP 出口的情况——
// 字段名一致所以差分测试全绿，但 /api/v1/settings/public 仍在下发 81KB 的 base64。
func TestSettingHandler_PublicSettings_SiteLogoIsDerivedURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const dataURI = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	repo := &settingHandlerPublicRepoStub{
		values: map[string]string{service.SettingKeySiteLogo: dataURI},
	}
	svc := service.NewSettingService(repo, &config.Config{})
	h := NewSettingHandler(svc, "test-version")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)
	h.GetPublicSettings(c)
	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Data struct {
			SiteLogo string `json:"site_logo"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))

	require.NotContains(t, resp.Data.SiteLogo, "base64",
		"/api/v1/settings/public 不得下发 base64 data URI")
	require.True(t, strings.HasPrefix(resp.Data.SiteLogo, "/brand/site-logo?v="),
		"应下发派生 URL，实际为 %q", resp.Data.SiteLogo)

	// 与 SSR 注入出口逐字节比对：两个出口必须给出同一个值。
	injected, err := svc.GetPublicSettingsForInjection(context.Background())
	require.NoError(t, err)
	injectedJSON, err := json.Marshal(injected)
	require.NoError(t, err)
	var injectedFields struct {
		SiteLogo string `json:"site_logo"`
	}
	require.NoError(t, json.Unmarshal(injectedJSON, &injectedFields))
	require.Equal(t, injectedFields.SiteLogo, resp.Data.SiteLogo,
		"SSR 注入与 /settings/public 的 site_logo 必须一致")
}
