package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"sync"
)

// 品牌图片（site_logo）的派生与解码。
//
// 背景：site_logo 在 settings 表里存的是完整的 base64 data URI。生产实测该值为
// 一张 1254×1254 的 JPEG（原始 60,831 字节，base64 后 81,108 字符）。它原先被
// 直接放进两个首屏出口——window.__APP_CONFIG__ 与 <link rel="icon">——于是每次
// 打开页面的 HTML 都要多背约 162KB，且 HTML 是 no-cache，无法被浏览器复用。
//
// 这里不改变数据存放位置（仍在 PG，集群广播与备份恢复零改动），只把首屏出口的
// 值换成一个带内容哈希的短 URL，图片本体改由独立端点提供，可被浏览器与边缘长缓存。

// BrandAssetPath 是站点 logo 的对外路径前缀。
//
// 刻意不放在 /api/ 下：生产边缘对 /api/ 前缀统一 X-Cache-Status: BYPASS，
// 而非 /api 的静态路径实测为 HIT。放在这里才能真正拿到边缘缓存。
// 新增该前缀时必须同步加入 web.shouldBypassEmbeddedFrontend，否则会被 SPA 兜底吞掉。
const BrandAssetPath = "/brand/site-logo"

// brandAssetMaxDecodedBytes 是解码后允许缓存/下发的上限。
// 超过则视为不可用，出口下发空串——避免一张失控的大图重新回到热路径。
const brandAssetMaxDecodedBytes = 8 << 20 // 8 MiB

// brandAssetAllowedMIME 是允许作为品牌图片下发的 MIME 白名单。
//
// 不接受 svg+xml：SVG 可内嵌脚本，而该端点是公开无鉴权的，浏览器直接导航到
// 该 URL 时会以文档方式渲染，等于把一个管理员可控的 XSS 面暴露在同源下。
var brandAssetAllowedMIME = map[string]struct{}{
	"image/png":    {},
	"image/jpeg":   {},
	"image/gif":    {},
	"image/webp":   {},
	"image/avif":   {},
	"image/x-icon": {},
}

// BrandAsset 是一张已解码并校验通过的品牌图片。
type BrandAsset struct {
	ContentType string
	Bytes       []byte
	// Hash 是对 settings 原始值取的 sha256 前 16 个十六进制字符，
	// 同时用作 URL 的 v 参数与 HTTP ETag。原值不变则 URL 不变。
	Hash string
}

// PublicURL 返回带内容哈希的对外地址。
func (a *BrandAsset) PublicURL() string {
	return BrandAssetPath + "?v=" + a.Hash
}

// brandAssetCache 缓存最近一次解码结果，避免每个请求都重复 base64 解码。
// 以原始设置值为键，值变了自然失效，无需依赖外部失效通知。
type brandAssetCache struct {
	mu     sync.RWMutex
	rawKey string
	asset  *BrandAsset
	// parsed 为 true 表示 rawKey 已解析过（结果可能是 nil，即不可用）。
	parsed bool
}

var siteLogoAssetCache brandAssetCache

// resolveBrandAsset 解析一个设置值。
//
// 返回 nil 表示该值不是可解码的 data URI（空值、外链、相对路径、损坏的 base64、
// 非白名单 MIME、超限都归此类），调用方据此决定透传原值还是下发空串。
func resolveBrandAsset(raw string) *BrandAsset {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(trimmed), "data:image/") {
		return nil
	}

	comma := strings.IndexByte(trimmed, ',')
	if comma < 0 {
		return nil
	}
	header := trimmed[5:comma] // 去掉 "data:"
	payload := trimmed[comma+1:]

	// header 形如 image/jpeg;base64 —— 只接受 base64 编码，别的形态不解。
	parts := strings.Split(header, ";")
	mime := strings.ToLower(strings.TrimSpace(parts[0]))
	isBase64 := false
	for _, p := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(p), "base64") {
			isBase64 = true
			break
		}
	}
	if !isBase64 {
		return nil
	}
	if _, ok := brandAssetAllowedMIME[mime]; !ok {
		return nil
	}

	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		// 少数写入方会产生不带 padding 的 base64，再试一次宽松解码。
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(payload, "="))
		if err != nil {
			return nil
		}
	}
	if len(decoded) == 0 || len(decoded) > brandAssetMaxDecodedBytes {
		return nil
	}

	sum := sha256.Sum256([]byte(trimmed))
	return &BrandAsset{
		ContentType: mime,
		Bytes:       decoded,
		Hash:        hex.EncodeToString(sum[:])[:16],
	}
}

// resolveSiteLogoAsset 是 resolveBrandAsset 的带缓存版本。
func resolveSiteLogoAsset(raw string) *BrandAsset {
	siteLogoAssetCache.mu.RLock()
	if siteLogoAssetCache.parsed && siteLogoAssetCache.rawKey == raw {
		asset := siteLogoAssetCache.asset
		siteLogoAssetCache.mu.RUnlock()
		return asset
	}
	siteLogoAssetCache.mu.RUnlock()

	asset := resolveBrandAsset(raw)

	siteLogoAssetCache.mu.Lock()
	siteLogoAssetCache.rawKey = raw
	siteLogoAssetCache.asset = asset
	siteLogoAssetCache.parsed = true
	siteLogoAssetCache.mu.Unlock()

	return asset
}

// publicSiteLogoValue 计算 site_logo 在公开出口（SSR 注入与 /api/v1/settings/public）
// 中应当下发的值。
//
// 三种形态：
//   - data URI 且可解码 → 返回带内容哈希的端点 URL
//   - 相对路径 / http(s) 外链 → 原样透传（管理员可以直接填 CDN 地址）
//   - 空值 / 不可解码 / 非白名单 MIME → 返回空串
//
// 返回空串而不是一个会 404 的 URL 是刻意的：前端拿到空串才会走兜底逻辑，
// 拿到 404 的 URL 只会得到一个破图。
func publicSiteLogoValue(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if asset := resolveSiteLogoAsset(raw); asset != nil {
		return asset.PublicURL()
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "data:") {
		// 是 data URI 但解不出来（损坏/非白名单），不要把它透传出去。
		return ""
	}
	// 非 data URI 的形态交给既有的 URL 安全校验（前端 sanitizeUrl / 后端
	// safeBrandImageURL）判断，这里原样透传。
	return trimmed
}

// GetSiteLogoAsset 返回当前 site_logo 对应的可下发图片。
// 未配置、非 data URI 或不可解码时返回 nil。
func (s *SettingService) GetSiteLogoAsset(ctx context.Context) (*BrandAsset, error) {
	// 同 FindLoginAgreementDocument：用 GetMultiple 容忍"该设置项尚未写入数据库"。
	// 用 GetValue 会在全新安装（未设置过 logo）时返回 not-found 错误，
	// 让端点吐 500 而不是语义正确的 404。
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeySiteLogo})
	if err != nil {
		return nil, err
	}
	return resolveSiteLogoAsset(values[SettingKeySiteLogo]), nil
}

// loginAgreementDocumentsWithoutContent 返回剥掉正文的副本，供公开出口序列化。
//
// 为什么必须是副本：buildLoginAgreementRevision 对**含正文**的完整文档取 sha256，
// 而该 revision 是登录门禁与前端同意态的键。若就地清空或复用同一底层数组，
// revision 会随之改变，后果是全体老用户被要求重新同意条款。
//
// 为什么保留 content_md 字段而不是删掉：前端类型里它是必填的 string，
// 管理端还有 doc.content_md.trim() 这类无空值防护的调用。保留字段、值恒为空串
// 是改动面最小、也最不容易引发运行时错误的形态。正文改由
// GET /api/v1/settings/legal-documents/:id 按需获取。
func loginAgreementDocumentsWithoutContent(docs []LoginAgreementDocument) []LoginAgreementDocument {
	if len(docs) == 0 {
		return docs
	}
	out := make([]LoginAgreementDocument, len(docs))
	for i, doc := range docs {
		out[i] = LoginAgreementDocument{ID: doc.ID, Title: doc.Title}
	}
	return out
}

// FindLoginAgreementDocument 按归一化后的 ID 查找单篇条款文档（含正文）。
// 供公开的按需取正文端点使用。
func (s *SettingService) FindLoginAgreementDocument(ctx context.Context, id string) (*LoginAgreementDocument, error) {
	wanted := normalizeLoginAgreementDocumentID(id)
	if wanted == "" {
		return nil, nil
	}
	// 用 GetMultiple 而不是 GetValue：设置项未写入数据库时 GetValue 会返回
	// "setting not found" 错误，而条款文档有内置默认集（parseLoginAgreementDocuments
	// 对空值回落到 defaultLoginAgreementDocuments）。GetMultiple 对缺失键只是不返回，
	// 与 GetPublicSettings 的读法一致，默认集才能生效。
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyLoginAgreementDocuments})
	if err != nil {
		return nil, err
	}
	for _, doc := range parseLoginAgreementDocuments(values[SettingKeyLoginAgreementDocuments]) {
		if normalizeLoginAgreementDocumentID(doc.ID) == wanted {
			found := doc
			return &found, nil
		}
	}
	return nil, nil
}
