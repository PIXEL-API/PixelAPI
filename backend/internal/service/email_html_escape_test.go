//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 这些测试锁死"HTML 邮件正文里的动态变量必须转义"这条不变量。
// 站点名是后台可配置项、重置链接由 frontend_url 设置拼出，两者都不可信。

// ---------- buildVerifyCodeEmailBody ----------

func TestBuildVerifyCodeEmailBody_EscapesSiteName(t *testing.T) {
	svc := &EmailService{}

	t.Run("escapes_script_injection", func(t *testing.T) {
		body := svc.buildVerifyCodeEmailBody("123456", `</h1><script>alert(1)</script><h1>`)

		assert.NotContains(t, body, "<script>")
		assert.NotContains(t, body, "</script>")
		assert.Contains(t, body, "&lt;script&gt;")
	})

	t.Run("escapes_html_entities", func(t *testing.T) {
		body := svc.buildVerifyCodeEmailBody("123456", `A&B<C>"D`)

		assert.Contains(t, body, "A&amp;B&lt;C&gt;&#34;D")
	})

	t.Run("escapes_code", func(t *testing.T) {
		// 验证码正常是 6 位数字，但正文不应假设它一定干净。
		body := svc.buildVerifyCodeEmailBody(`<img src=x onerror=alert(1)>`, "Site")

		assert.NotContains(t, body, "<img src=x")
		assert.Contains(t, body, "&lt;img")
	})

	t.Run("normal_site_name_unchanged", func(t *testing.T) {
		body := svc.buildVerifyCodeEmailBody("654321", "My Site")

		assert.Contains(t, body, "<h1>My Site</h1>")
		assert.Contains(t, body, `<div class="code">654321</div>`)
		assert.NotContains(t, body, "%!")
	})
}

// ---------- buildPasswordResetEmailBody ----------

func TestBuildPasswordResetEmailBody_EscapesSiteNameAndURL(t *testing.T) {
	svc := &EmailService{}

	t.Run("escapes_html_tags_in_site_name", func(t *testing.T) {
		body := svc.buildPasswordResetEmailBody("https://example.com/reset?token=abc", `</h1><img src=x onerror=alert(1)>`)

		assert.NotContains(t, body, "<img src=x")
		assert.True(t, strings.Contains(body, "&lt;img"))
	})

	t.Run("escapes_html_entities", func(t *testing.T) {
		body := svc.buildPasswordResetEmailBody("https://example.com/reset", `A&B<C>`)

		assert.Contains(t, body, "A&amp;B&lt;C&gt;")
	})

	t.Run("normal_site_name_and_url_render", func(t *testing.T) {
		resetURL := "https://example.com/reset?token=xyz"
		body := svc.buildPasswordResetEmailBody(resetURL, "Sub2API")

		assert.Contains(t, body, "<h1>Sub2API</h1>")
		assert.Contains(t, body, `href="https://example.com/reset?token=xyz"`)
		assert.NotContains(t, body, "%!")
	})

	t.Run("escapes_ampersand_in_reset_url", func(t *testing.T) {
		resetURL := "https://example.com/reset?a=1&b=2"
		body := svc.buildPasswordResetEmailBody(resetURL, "Site")

		assert.NotContains(t, body, `href="https://example.com/reset?a=1&b=2"`)
		assert.Contains(t, body, `href="https://example.com/reset?a=1&amp;b=2"`)
	})

	t.Run("escapes_quote_breaking_out_of_href_attribute", func(t *testing.T) {
		resetURL := `https://example.com/reset?token=a" onclick="alert(1)`
		body := svc.buildPasswordResetEmailBody(resetURL, "Site")

		// 含空格/引号的 URL 不是合法的绝对 http URL，按钮直接不渲染；
		// 即便渲染也绝不能让裸引号闭合 href 属性。
		assert.NotContains(t, body, `onclick="alert(1)"`)
		assert.NotContains(t, body, `token=a" onclick`)
	})

	t.Run("rejects_javascript_pseudo_scheme_in_href", func(t *testing.T) {
		body := svc.buildPasswordResetEmailBody("javascript:alert(document.cookie)", "Site")

		assert.NotContains(t, body, `href="javascript:`)
		assert.NotContains(t, body, "<a href")
		// 纯文本兜底位置仍会展示（转义后的）原始值，文本节点不会被执行。
		assert.Contains(t, body, "javascript:alert(document.cookie)")
	})

	t.Run("rejects_data_scheme_in_href", func(t *testing.T) {
		body := svc.buildPasswordResetEmailBody("data:text/html;base64,PHNjcmlwdD4=", "Site")

		assert.NotContains(t, body, `href="data:`)
		assert.NotContains(t, body, "<a href")
	})

	t.Run("rejects_relative_url_in_href", func(t *testing.T) {
		body := svc.buildPasswordResetEmailBody("/reset-password?token=abc", "Site")

		assert.NotContains(t, body, "<a href")
	})
}

// ---------- emailSafeLinkURL ----------

func TestEmailSafeLinkURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"https", "https://example.com/reset?token=1", "https://example.com/reset?token=1"},
		{"http", "http://example.com/reset", "http://example.com/reset"},
		{"uppercase_scheme", "HTTPS://example.com/reset", "HTTPS://example.com/reset"},
		{"trims_space", "  https://example.com/reset  ", "https://example.com/reset"},
		{"empty", "", ""},
		{"blank", "   ", ""},
		{"javascript", "javascript:alert(1)", ""},
		{"javascript_mixed_case", "JaVaScRiPt:alert(1)", ""},
		{"data", "data:text/html,<script>alert(1)</script>", ""},
		{"vbscript", "vbscript:msgbox(1)", ""},
		{"file", "file:///etc/passwd", ""},
		{"relative_path", "/reset-password", ""},
		{"scheme_relative", "//example.com/reset", ""},
		{"no_host", "https:///reset", ""},
		{"embedded_newline", "https://example.com/reset\nSet-Cookie: x=1", ""},
		{"embedded_cr", "java\rscript:alert(1)", ""},
		{"embedded_tab", "java\tscript:alert(1)", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, emailSafeLinkURL(tc.in))
		})
	}
}

// ---------- notify email verification body ----------

func TestBuildNotifyVerifyEmailBody_EscapesSiteName(t *testing.T) {
	t.Run("escapes_script_injection", func(t *testing.T) {
		body := buildNotifyVerifyEmailBody("123456", `</h1><script>alert(1)</script>`)

		assert.NotContains(t, body, "<script>")
		assert.Contains(t, body, "&lt;script&gt;")
	})

	t.Run("normal_site_name_unchanged", func(t *testing.T) {
		body := buildNotifyVerifyEmailBody("654321", "My Site")

		assert.Contains(t, body, "<h1>My Site</h1>")
		assert.Contains(t, body, `<div class="code">654321</div>`)
		assert.NotContains(t, body, "%!")
	})
}

// ---------- 邮件头（非 HTML 上下文）不得被 HTML 转义 ----------

// 主题行走的是 SMTP 头，不是 HTML 上下文。只清洗 CR/LF 防头注入，
// 绝不能顺手 HTML 转义，否则用户会在主题里看到 &amp;。
func TestSanitizeEmailHeader_DoesNotHTMLEscape(t *testing.T) {
	require.Equal(t, `A&B<C>"D`, sanitizeEmailHeader(`A&B<C>"D`))
	require.Equal(t, "SiteX-Test", sanitizeEmailHeader("Site\r\nX-Test"))
}
