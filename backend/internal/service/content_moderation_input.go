package service

import (
	"crypto/sha256"
	"encoding/binary"
	"io"
	randv2 "math/rand/v2"
	"strings"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"github.com/tidwall/gjson"
)

const maxModerationImageDigestEntries = 1024

type moderationInputCollector struct {
	text                     strings.Builder
	runeCount                int
	image                    string
	imageCount               int
	imageDigests             [][sha256.Size]byte
	seenImageDigests         map[[sha256.Size]byte]struct{}
	imageDigestOverflow      [sha256.Size]byte
	imageDigestOverflowCount uint64
}

func newModerationInputCollector() *moderationInputCollector {
	return &moderationInputCollector{}
}

func (c *moderationInputCollector) IsEmpty() bool {
	return c == nil || (c.runeCount == 0 && c.image == "")
}

func (c *moderationInputCollector) Input() ContentModerationInput {
	if c == nil {
		return ContentModerationInput{}
	}
	out := ContentModerationInput{
		Text:                     c.text.String(),
		allImageDigests:          append([][sha256.Size]byte(nil), c.imageDigests...),
		imageDigestOverflow:      c.imageDigestOverflow,
		imageDigestOverflowCount: c.imageDigestOverflowCount,
	}
	if c.image != "" {
		out.Images = []string{c.image}
	}
	return out
}

// AppendInput merges an already bounded input into the collector. Exact image
// digests stay bounded and any overflow is folded into the fixed-size summary;
// only the reservoir-selected image data itself is retained. This is used by
// the compatibility fallback that combines several protocol-shaped fields.
func (c *moderationInputCollector) AppendInput(in ContentModerationInput) {
	if c == nil {
		return
	}
	c.AddText(in.Text)
	if len(in.allImageDigests) == 0 {
		for _, image := range in.Images {
			c.AddImage(image)
		}
		return
	}
	var selectedImage string
	if len(in.Images) > 0 {
		selectedImage = in.Images[0]
	}
	if c.image == "" && selectedImage != "" {
		// The source input exposes only its reservoir-selected image. Keep it as
		// a safe fallback in case the merged reservoir chooses a digest whose
		// original image bytes are intentionally not retained by that source.
		c.image = strings.Clone(selectedImage)
	}
	selectedDigest := sha256.Sum256([]byte(selectedImage))
	for _, digest := range in.allImageDigests {
		if !c.shouldSelectImage(digest) {
			continue
		}
		if selectedImage != "" && digest == selectedDigest {
			c.image = strings.Clone(selectedImage)
		}
	}
	if in.imageDigestOverflowCount > 0 {
		c.addImageDigestOverflow(in.imageDigestOverflow, in.imageDigestOverflowCount)
	}
}

// AddText 收录全部文本，不对 <system-reminder> 之类的标记做任何排除。
// 客户端注入的提醒块与用户自己输入的同名标记在请求体里无法区分，任何基于标记的
// 排除规则都可被伪造：曾经的实现只要正文出现 "<system-reminder>" 就丢弃整段，
// 于是加上这一个标记即可让请求完全绕过内容审计。
func (c *moderationInputCollector) AddText(text string) {
	if c == nil || c.runeCount >= maxModerationInputRunes {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	pendingSpace := c.runeCount > 0
	for len(text) > 0 && c.runeCount < maxModerationInputRunes {
		r, size := utf8.DecodeRuneInString(text)
		text = text[size:]
		if unicode.IsSpace(r) {
			pendingSpace = c.runeCount > 0
			continue
		}
		if pendingSpace {
			_ = c.text.WriteByte(' ')
			c.runeCount++
			if c.runeCount >= maxModerationInputRunes {
				return
			}
			pendingSpace = false
		}
		_, _ = c.text.WriteRune(r)
		c.runeCount++
	}
}

func (c *moderationInputCollector) AddImage(image string) {
	if c == nil {
		return
	}
	image = strings.TrimSpace(image)
	if image == "" || (!strings.HasPrefix(image, "data:") && !strings.HasPrefix(image, "http://") && !strings.HasPrefix(image, "https://")) {
		return
	}
	digest := sha256.Sum256([]byte(image))
	if c.shouldSelectImage(digest) {
		// gjson results may be zero-copy views over the complete request body.
		// Clone the one retained image so an async audit task cannot keep the
		// entire body alive through a short substring.
		c.image = strings.Clone(image)
	}
}

func (c *moderationInputCollector) AddImageData(mimeType, data string) {
	if c == nil {
		return
	}
	mimeType = strings.TrimSpace(mimeType)
	data = strings.TrimSpace(data)
	if mimeType == "" || data == "" {
		return
	}
	h := sha256.New()
	_, _ = io.WriteString(h, "data:")
	_, _ = io.WriteString(h, mimeType)
	_, _ = io.WriteString(h, ";base64,")
	_, _ = io.WriteString(h, data)
	var digest [sha256.Size]byte
	h.Sum(digest[:0])
	if c.shouldSelectImage(digest) {
		c.image = "data:" + mimeType + ";base64," + data
	}
}

// shouldSelectImage tracks a bounded exact digest prefix. Once the prefix is
// full, later digests are folded into a fixed-size rolling summary. Exact
// deduplication is intentionally limited to the prefix: maintaining an
// attacker-controlled set for every image would merely move the leak from
// image strings to the digest map. Reservoir sampling still retains at most
// one image body for the moderation API.
func (c *moderationInputCollector) shouldSelectImage(digest [sha256.Size]byte) bool {
	if c == nil {
		return false
	}
	if c.seenImageDigests == nil {
		c.seenImageDigests = make(map[[sha256.Size]byte]struct{}, maxModerationImageDigestEntries)
	}
	if _, exists := c.seenImageDigests[digest]; exists {
		return false
	}
	if len(c.imageDigests) < maxModerationImageDigestEntries {
		c.seenImageDigests[digest] = struct{}{}
		c.imageDigests = append(c.imageDigests, digest)
		c.imageCount++
	} else {
		c.addImageDigestOverflow(digest, 1)
	}
	return c.imageCount == 1 || randv2.IntN(c.imageCount) == 0
}

func (c *moderationInputCollector) addImageDigestOverflow(digest [sha256.Size]byte, count uint64) {
	if c == nil || count == 0 {
		return
	}
	h := sha256.New()
	_, _ = h.Write([]byte("content-moderation-image-overflow-v1:"))
	_, _ = h.Write(c.imageDigestOverflow[:])
	var encodedCount [8]byte
	binary.BigEndian.PutUint64(encodedCount[:], count)
	_, _ = h.Write(encodedCount[:])
	_, _ = h.Write(digest[:])
	h.Sum(c.imageDigestOverflow[:0])
	c.imageDigestOverflowCount += count
	if count > uint64(^uint(0)>>1)-uint64(c.imageCount) {
		c.imageCount = int(^uint(0) >> 1)
	} else {
		c.imageCount += int(count)
	}
}

func ExtractContentModerationText(protocol string, body []byte) string {
	return ExtractContentModerationInput(protocol, body).Text
}

func ExtractContentModerationInput(protocol string, body []byte) ContentModerationInput {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ContentModerationInput{}
	}
	return extractContentModerationInputFromValidJSON(protocol, body)
}

func extractContentModerationInputFromValidJSON(protocol string, body []byte) ContentModerationInput {
	// Parse a zero-copy view of the already-buffered body. gjson.GetBytes
	// deliberately copies matched JSON subtrees; messages/input/contents may be
	// almost as large as the request itself. Retained text is copied into the
	// bounded builder and retained images are explicitly cloned by AddImage.
	root := gjson.Parse(unsafe.String(unsafe.SliceData(body), len(body)))
	if protocol == ContentModerationProtocolOpenAIResponses {
		return extractResponsesContentModerationInput(root.Get("input"))
	}
	switch protocol {
	case ContentModerationProtocolAnthropicMessages:
		return extractLastRoleMessageBounded(root.Get("messages"), "user", true)
	case ContentModerationProtocolOpenAIChat:
		return extractLastRoleMessageBounded(root.Get("messages"), "user", false)
	case ContentModerationProtocolGemini:
		return extractLastGeminiContentBounded(root.Get("contents"))
	case ContentModerationProtocolOpenAIImages:
		collector := newModerationInputCollector()
		collector.AddText(root.Get("prompt").String())
		collectContentValueBounded(root.Get("images"), collector)
		return collector.Input()
	default:
		// Unknown/legacy callers may send more than one protocol-shaped field.
		// Merge each bounded result instead of building unbounded parts/images
		// slices and joining them after the fact.
		collector := newModerationInputCollector()
		collector.AppendInput(extractResponsesContentModerationInput(root.Get("input")))
		collector.AppendInput(extractLastRoleMessageBounded(root.Get("messages"), "user", false))
		collector.AppendInput(extractLastGeminiContentBounded(root.Get("contents")))
		return collector.Input()
	}
}

func extractResponsesContentModerationInput(input gjson.Result) ContentModerationInput {
	switch {
	case !input.Exists():
		return ContentModerationInput{}
	case input.Type == gjson.String:
		collector := newModerationInputCollector()
		collector.AddText(input.String())
		return collector.Input()
	case input.IsArray():
		var latest *moderationInputCollector
		input.ForEach(func(_, item gjson.Result) bool {
			if !isResponsesModerationCandidate(item) {
				return true
			}
			collector := newModerationInputCollector()
			collectResponsesItemModerationContentBounded(item, collector)
			if !collector.IsEmpty() {
				latest = collector
			}
			return true
		})
		if latest != nil {
			return latest.Input()
		}
	case input.IsObject() && isResponsesModerationCandidate(input):
		collector := newModerationInputCollector()
		collectResponsesItemModerationContentBounded(input, collector)
		return collector.Input()
	}
	return ContentModerationInput{}
}

// extractLastRoleMessageBounded returns the last non-empty message for role.
// It keeps only one bounded collector (12k runes, one image plus fixed-size
// digests) instead of materializing every text/image value in the message
// history before selecting the last candidate.
func extractLastRoleMessageBounded(messages gjson.Result, role string, anthropic bool) ContentModerationInput {
	if !messages.IsArray() {
		return ContentModerationInput{}
	}
	var latest *moderationInputCollector
	messages.ForEach(func(_, msg gjson.Result) bool {
		if strings.ToLower(strings.TrimSpace(msg.Get("role").String())) != role {
			return true
		}
		collector := newModerationInputCollector()
		if anthropic {
			collectAnthropicUserContentValueBounded(msg.Get("content"), collector)
		} else {
			collectContentValueBounded(msg.Get("content"), collector)
		}
		if !collector.IsEmpty() {
			latest = collector
		}
		return true
	})
	if latest == nil {
		return ContentModerationInput{}
	}
	return latest.Input()
}

func extractLastGeminiContentBounded(contents gjson.Result) ContentModerationInput {
	if !contents.IsArray() {
		return ContentModerationInput{}
	}
	var latest *moderationInputCollector
	contents.ForEach(func(_, content gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(content.Get("role").String()))
		if role != "" && role != "user" {
			return true
		}
		collector := newModerationInputCollector()
		if arr := content.Get("parts"); arr.IsArray() {
			arr.ForEach(func(_, part gjson.Result) bool {
				collector.AddText(part.Get("text").String())
				addGeminiModerationImageBounded(part, collector)
				return true
			})
		}
		if !collector.IsEmpty() {
			latest = collector
		}
		return true
	})
	if latest == nil {
		return ContentModerationInput{}
	}
	return latest.Input()
}

func isResponsesModerationCandidate(item gjson.Result) bool {
	role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
	return role == "" || role == "user"
}

func collectResponsesItemModerationContentBounded(item gjson.Result, collector *moderationInputCollector) {
	collectContentValueBounded(item.Get("content"), collector)
	if item.Get("type").String() == "input_text" || item.Get("text").Exists() {
		collectContentValueBounded(item, collector)
	}
}

func collectContentValueBounded(value gjson.Result, collector *moderationInputCollector) {
	if collector == nil {
		return
	}
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		if collector.runeCount < maxModerationInputRunes {
			collector.AddText(value.String())
		}
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectContentValueBounded(item, collector)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		collector.AddImage(value.Get("image_url.url").String())
		collector.AddImage(value.Get("image_url").String())
		collector.AddImage(value.Get("url").String())
		collector.AddImageData(value.Get("source.media_type").String(), value.Get("source.data").String())
		collector.AddImageData(value.Get("source.mediaType").String(), value.Get("source.data").String())
		collector.AddImageData(value.Get("media_type").String(), value.Get("data").String())
		collector.AddImageData(value.Get("mime_type").String(), value.Get("data").String())
		collector.AddImageData(value.Get("mimeType").String(), value.Get("data").String())
		collector.AddImage(value.Get("source.data").String())
		collector.AddImage(value.Get("data").String())
		collector.AddImage(value.Get("base64").String())
		switch typ {
		// output_text 也必须收录：本 fork 的三处协议转换器（openai_codex_transform、
		// chatcompletions_responses_bridge、apicompat/responses_to_anthropic_request）
		// 都会把 output_text 的 text 透传给上游模型，审计端漏收就是一条静默绕过。
		case "", "text", "input_text", "output_text", "message":
			if text := value.Get("text"); text.Exists() && collector.runeCount < maxModerationInputRunes {
				collector.AddText(text.String())
			}
			if content := value.Get("content"); content.Exists() {
				collectContentValueBounded(content, collector)
			}
		case "image_url", "input_image", "image":
		}
	}
}

// collectAnthropicUserContentValueBounded mirrors the Anthropic-specific
// content rules while writing directly into a bounded collector. In
// particular, text attached to image blocks is not treated as prompt text.
func collectAnthropicUserContentValueBounded(value gjson.Result, collector *moderationInputCollector) {
	if collector == nil {
		return
	}
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		collector.AddText(value.String())
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectAnthropicUserContentValueBounded(item, collector)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		switch typ {
		case "", "text", "input_text", "output_text", "message":
			if text := value.Get("text"); text.Exists() {
				collector.AddText(text.String())
			}
			if content := value.Get("content"); content.Exists() {
				collectAnthropicUserContentValueBounded(content, collector)
			}
		case "image_url", "input_image", "image":
			collectContentValueBounded(value, collector)
		}
	}
}

func addGeminiModerationImageBounded(part gjson.Result, collector *moderationInputCollector) {
	if collector == nil {
		return
	}
	if inlineData := part.Get("inline_data"); inlineData.IsObject() {
		collector.AddImageData(inlineData.Get("mime_type").String(), inlineData.Get("data").String())
	}
	if inlineData := part.Get("inlineData"); inlineData.IsObject() {
		collector.AddImageData(inlineData.Get("mimeType").String(), inlineData.Get("data").String())
	}
	collector.AddImage(part.Get("file_data.file_uri").String())
	collector.AddImage(part.Get("fileData.fileUri").String())
}

func normalizeModerationImages(images []string) []string {
	out := make([]string, 0, len(images))
	seen := make(map[string]struct{}, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if _, ok := seen[image]; ok {
			continue
		}
		seen[image] = struct{}{}
		out = append(out, image)
	}
	return out
}

func limitContentModerationImages(images []string) []string {
	if len(images) <= maxContentModerationInputImages {
		return images
	}
	return []string{images[randv2.IntN(len(images))]}
}

func normalizeContentModerationText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}
