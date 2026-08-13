package service

import (
	"regexp"
	"strings"
)

var credentialUnsafeURLRegex = regexp.MustCompile(`(?i)https?://[^\s"'<>)\]]+`)

// redactCredentialUnsafeText 清洗服务端自己写进 accounts.extra 的诊断文本。
//
// 探测失败、上游报错这类文本会被原样存进 extra（例如 openai_compact_last_error），
// 而 Go 的 *url.Error 一定会带上完整 URL、上游 HTML 拦截页里也常有链接。自有账号的
// 凭证安全扫描把任何含 http(s):// 的字符串视为"用户私自配置了上游"，于是服务端写的
// 一行诊断信息就能把账号所有者挡在门外。写入端先清洗，扫描规则一条都不用放宽。
func redactCredentialUnsafeText(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	out := credentialUnsafeURLRegex.ReplaceAllString(text, "[url]")
	for _, needle := range forbiddenCredentialTextNeedles {
		out = replaceAllFold(out, needle, "[redacted]")
	}
	for _, prefix := range forbiddenCredentialTextPrefixes {
		out = replaceAllFold(out, prefix+"=", "[redacted]")
		out = replaceAllFold(out, prefix+":", "[redacted]")
	}
	return out
}

// replaceAllFold 做大小写不敏感的整串替换，保留未匹配部分的原始大小写。
func replaceAllFold(text, needle, replacement string) string {
	if needle == "" {
		return text
	}
	lowerText := strings.ToLower(text)
	lowerNeedle := strings.ToLower(needle)
	if !strings.Contains(lowerText, lowerNeedle) {
		return text
	}
	var builder strings.Builder
	for {
		index := strings.Index(lowerText, lowerNeedle)
		if index < 0 {
			_, _ = builder.WriteString(text)
			return builder.String()
		}
		_, _ = builder.WriteString(text[:index])
		_, _ = builder.WriteString(replacement)
		text = text[index+len(needle):]
		lowerText = lowerText[index+len(needle):]
	}
}
