//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 一张 1x1 的合法 PNG，用于构造可解码的 data URI。
const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestResolveBrandAsset(t *testing.T) {
	t.Run("可解码的 data URI 返回字节与稳定哈希", func(t *testing.T) {
		raw := "data:image/png;base64," + tinyPNGBase64

		asset := resolveBrandAsset(raw)

		require.NotNil(t, asset)
		require.Equal(t, "image/png", asset.ContentType)
		require.NotEmpty(t, asset.Bytes)
		require.Len(t, asset.Hash, 16)
		require.Equal(t, asset.Hash, resolveBrandAsset(raw).Hash, "同一输入必须得到同一哈希")
		require.Equal(t, "/brand/site-logo?v="+asset.Hash, asset.PublicURL())
	})

	t.Run("内容变化时哈希随之变化", func(t *testing.T) {
		a := resolveBrandAsset("data:image/png;base64," + tinyPNGBase64)
		b := resolveBrandAsset("data:image/jpeg;base64," + tinyPNGBase64)

		require.NotNil(t, a)
		require.NotNil(t, b)
		require.NotEqual(t, a.Hash, b.Hash)
	})

	t.Run("非 data URI 一律返回 nil", func(t *testing.T) {
		for _, raw := range []string{
			"",
			"   ",
			"/brand/custom.png",
			"https://cdn.example.com/logo.png",
			"data:text/plain;base64,aGk=",
		} {
			require.Nil(t, resolveBrandAsset(raw), "raw=%q", raw)
		}
	})

	t.Run("SVG 不在白名单内", func(t *testing.T) {
		// SVG 可内嵌脚本，而该端点公开无鉴权且同源，放行等于开一个 XSS 面。
		require.Nil(t, resolveBrandAsset("data:image/svg+xml;base64,PHN2Zz48L3N2Zz4="))
	})

	t.Run("损坏的 base64 返回 nil 而不是 panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			require.Nil(t, resolveBrandAsset("data:image/png;base64,!!!not-base64!!!"))
		})
	})

	t.Run("非 base64 编码的 data URI 不解析", func(t *testing.T) {
		require.Nil(t, resolveBrandAsset("data:image/png,rawbytes"))
	})

	t.Run("缺少逗号分隔符返回 nil", func(t *testing.T) {
		require.Nil(t, resolveBrandAsset("data:image/png;base64"))
	})

	t.Run("解码结果为空返回 nil", func(t *testing.T) {
		require.Nil(t, resolveBrandAsset("data:image/png;base64,"))
	})
}

func TestPublicSiteLogoValue(t *testing.T) {
	t.Run("data URI 换成端点 URL 且足够短", func(t *testing.T) {
		raw := "data:image/png;base64," + tinyPNGBase64

		got := publicSiteLogoValue(raw)

		require.True(t, strings.HasPrefix(got, "/brand/site-logo?v="), "got=%q", got)
		require.Less(t, len(got), 128, "对外值必须是短 URL，不能再是 base64")
		require.NotContains(t, got, "base64")
	})

	t.Run("相对路径与外链原样透传", func(t *testing.T) {
		require.Equal(t, "/brand/custom.png", publicSiteLogoValue("/brand/custom.png"))
		require.Equal(t, "https://cdn.example.com/logo.png", publicSiteLogoValue("https://cdn.example.com/logo.png"))
	})

	t.Run("空值返回空串", func(t *testing.T) {
		require.Equal(t, "", publicSiteLogoValue(""))
		require.Equal(t, "", publicSiteLogoValue("   "))
	})

	t.Run("不可解码的 data URI 返回空串而不是会 404 的 URL", func(t *testing.T) {
		// 下发空串前端才会走兜底；下发一个 404 的 URL 只会得到破图。
		require.Equal(t, "", publicSiteLogoValue("data:image/png;base64,!!!bad!!!"))
		require.Equal(t, "", publicSiteLogoValue("data:image/svg+xml;base64,PHN2Zz48L3N2Zz4="))
	})
}

func TestLoginAgreementDocumentsWithoutContent(t *testing.T) {
	docs := []LoginAgreementDocument{
		{ID: "terms", Title: "服务条款", ContentMD: "# 正文一"},
		{ID: "privacy", Title: "隐私政策", ContentMD: "# 正文二"},
	}

	stripped := loginAgreementDocumentsWithoutContent(docs)

	require.Len(t, stripped, 2)
	for i, doc := range stripped {
		require.Equal(t, docs[i].ID, doc.ID)
		require.Equal(t, docs[i].Title, doc.Title)
		require.Empty(t, doc.ContentMD)
	}

	// 关键：必须是副本。原文档若被就地清空，buildLoginAgreementRevision 的
	// sha256 输入就变了，全体老用户会被要求重新同意条款。
	require.Equal(t, "# 正文一", docs[0].ContentMD, "原切片不得被污染")
	require.Equal(t, "# 正文二", docs[1].ContentMD, "原切片不得被污染")
}

// TestLoginAgreementRevisionUnaffectedByStripping 是本次改动最重要的一条防线。
//
// revision 是登录门禁（auth_handler 比对）与前端 localStorage 同意态的键。
// 剥离正文只能发生在出口序列化层；一旦影响到 revision 计算输入，
// 后果是全体老用户被强制重新同意条款。
func TestLoginAgreementRevisionUnaffectedByStripping(t *testing.T) {
	updatedAt := "2026-04-26"
	docs := []LoginAgreementDocument{
		{ID: "terms", Title: "服务条款", ContentMD: "# PIXEL API 服务条款\n\n正文若干"},
		{ID: "usage-policy", Title: "使用政策", ContentMD: "# 使用政策\n\n正文若干"},
	}

	before := buildLoginAgreementRevision(updatedAt, docs)

	// 模拟出口序列化：剥离正文后再算一次 revision（用未被污染的原切片）。
	_ = loginAgreementDocumentsWithoutContent(docs)
	after := buildLoginAgreementRevision(updatedAt, docs)

	require.Equal(t, before, after, "剥离正文不得改变 revision")
	require.NotEmpty(t, before)

	// 反证：若真的把正文剥掉再算，revision 必然不同——说明这条断言有效。
	strippedRevision := buildLoginAgreementRevision(updatedAt, loginAgreementDocumentsWithoutContent(docs))
	require.NotEqual(t, before, strippedRevision,
		"若此断言失败说明 revision 不含正文，本测试失去意义，需重新评估")
}
