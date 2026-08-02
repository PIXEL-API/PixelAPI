package service

import (
	"crypto/sha256"
	"fmt"
	"io"
	randv2 "math/rand/v2"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

type moderationInputCollector struct {
	text             strings.Builder
	runeCount        int
	image            string
	imageDigests     [][sha256.Size]byte
	seenImageDigests map[[sha256.Size]byte]struct{}
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
		Text:            c.text.String(),
		allImageDigests: append([][sha256.Size]byte(nil), c.imageDigests...),
	}
	if c.image != "" {
		out.Images = []string{c.image}
	}
	return out
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
			c.text.WriteByte(' ')
			c.runeCount++
			if c.runeCount >= maxModerationInputRunes {
				return
			}
			pendingSpace = false
		}
		c.text.WriteRune(r)
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
		c.image = image
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

// shouldSelectImage tracks every unique image by a fixed-size digest so Hash
// keeps its previous all-image semantics without retaining every data URI.
// Reservoir sampling preserves the previous uniform one-image audit behavior.
func (c *moderationInputCollector) shouldSelectImage(digest [sha256.Size]byte) bool {
	if c == nil {
		return false
	}
	if c.seenImageDigests == nil {
		c.seenImageDigests = make(map[[sha256.Size]byte]struct{})
	}
	if _, exists := c.seenImageDigests[digest]; exists {
		return false
	}
	c.seenImageDigests[digest] = struct{}{}
	c.imageDigests = append(c.imageDigests, digest)
	imageCount := len(c.imageDigests)
	return imageCount == 1 || randv2.IntN(imageCount) == 0
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
	if protocol == ContentModerationProtocolOpenAIResponses {
		return extractResponsesContentModerationInput(gjson.GetBytes(body, "input"))
	}
	var parts []string
	var images []string
	switch protocol {
	case ContentModerationProtocolAnthropicMessages:
		collectLastAnthropicUserMessage(gjson.GetBytes(body, "messages"), &parts, &images)
	case ContentModerationProtocolOpenAIChat:
		collectLastRoleMessage(gjson.GetBytes(body, "messages"), "user", &parts, &images)
	case ContentModerationProtocolGemini:
		collectLastGeminiContent(gjson.GetBytes(body, "contents"), &parts, &images)
	case ContentModerationProtocolOpenAIImages:
		addModerationText(&parts, gjson.GetBytes(body, "prompt").String())
		collectContentValue(gjson.GetBytes(body, "images"), &parts, &images)
	default:
		collectLastResponsesInput(gjson.GetBytes(body, "input"), &parts, &images)
		collectLastRoleMessage(gjson.GetBytes(body, "messages"), "user", &parts, &images)
		collectLastGeminiContent(gjson.GetBytes(body, "contents"), &parts, &images)
	}
	out := ContentModerationInput{
		Text:   normalizeContentModerationText(strings.Join(parts, "\n")),
		Images: normalizeModerationImages(images),
	}
	out.Normalize()
	return out
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
		items := input.Array()
		for i := len(items) - 1; i >= 0; i-- {
			item := items[i]
			if !isResponsesModerationCandidate(item) {
				continue
			}
			collector := newModerationInputCollector()
			collectResponsesItemModerationContentBounded(item, collector)
			if !collector.IsEmpty() {
				return collector.Input()
			}
		}
	case input.IsObject() && isResponsesModerationCandidate(input):
		collector := newModerationInputCollector()
		collectResponsesItemModerationContentBounded(input, collector)
		return collector.Input()
	}
	return ContentModerationInput{}
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
		case "", "text", "input_text", "message":
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

func collectLastRoleMessage(messages gjson.Result, role string, parts *[]string, images *[]string) {
	if !messages.IsArray() {
		return
	}
	var lastParts []string
	var lastImages []string
	messages.ForEach(func(_, msg gjson.Result) bool {
		if strings.ToLower(strings.TrimSpace(msg.Get("role").String())) == role {
			var candidate []string
			var candidateImages []string
			collectContentValue(msg.Get("content"), &candidate, &candidateImages)
			if normalizeContentModerationText(strings.Join(candidate, "\n")) != "" || len(candidateImages) > 0 {
				lastParts = candidate
				lastImages = candidateImages
			}
		}
		return true
	})
	*parts = append(*parts, lastParts...)
	*images = append(*images, lastImages...)
}

func collectLastAnthropicUserMessage(messages gjson.Result, parts *[]string, images *[]string) {
	if !messages.IsArray() {
		return
	}
	var lastParts []string
	var lastImages []string
	messages.ForEach(func(_, msg gjson.Result) bool {
		if strings.ToLower(strings.TrimSpace(msg.Get("role").String())) == "user" {
			var candidate []string
			var candidateImages []string
			collectAnthropicUserContentValue(msg.Get("content"), &candidate, &candidateImages)
			if normalizeContentModerationText(strings.Join(candidate, "\n")) != "" || len(candidateImages) > 0 {
				lastParts = candidate
				lastImages = candidateImages
			}
		}
		return true
	})
	*parts = append(*parts, lastParts...)
	*images = append(*images, lastImages...)
}

func collectAnthropicUserContentValue(value gjson.Result, parts *[]string, images *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		addModerationText(parts, value.String())
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectAnthropicUserContentValue(item, parts, images)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		switch typ {
		case "", "text", "input_text", "message":
			if value.Get("text").Exists() {
				addModerationText(parts, value.Get("text").String())
			}
			if value.Get("content").Exists() {
				collectAnthropicUserContentValue(value.Get("content"), parts, images)
			}
		case "image_url", "input_image", "image":
			collectContentValue(value, parts, images)
		}
	}
}

func collectLastResponsesInput(input gjson.Result, parts *[]string, images *[]string) {
	switch {
	case !input.Exists():
		return
	case input.Type == gjson.String:
		addModerationText(parts, input.String())
	case input.IsArray():
		var lastParts []string
		var lastImages []string
		input.ForEach(func(_, item gjson.Result) bool {
			if candidateParts, candidateImages, ok := collectResponsesUserTextItem(item); ok {
				lastParts = candidateParts
				lastImages = candidateImages
			}
			return true
		})
		*parts = append(*parts, lastParts...)
		*images = append(*images, lastImages...)
	case input.IsObject():
		if candidateParts, candidateImages, ok := collectResponsesUserTextItem(input); ok {
			*parts = append(*parts, candidateParts...)
			*images = append(*images, candidateImages...)
		}
	}
}

func collectResponsesUserTextItem(item gjson.Result) ([]string, []string, bool) {
	if isResponsesUserTextItem(item) {
		return collectResponsesItemModerationContent(item)
	}
	role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
	if role != "" {
		return nil, nil, false
	}
	return collectResponsesItemModerationContent(item)
}

func isResponsesUserTextItem(item gjson.Result) bool {
	return strings.ToLower(strings.TrimSpace(item.Get("role").String())) == "user"
}

func collectResponsesItemModerationContent(item gjson.Result) ([]string, []string, bool) {
	var parts []string
	var images []string
	collectContentValue(item.Get("content"), &parts, &images)
	if item.Get("type").String() == "input_text" || item.Get("text").Exists() {
		collectContentValue(item, &parts, &images)
	}
	if normalizeContentModerationText(strings.Join(parts, "\n")) == "" && len(images) == 0 {
		return nil, nil, false
	}
	return parts, images, true
}

func collectLastGeminiContent(contents gjson.Result, parts *[]string, images *[]string) {
	if !contents.IsArray() {
		return
	}
	var lastParts []string
	var lastImages []string
	contents.ForEach(func(_, content gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(content.Get("role").String()))
		if role == "" || role == "user" {
			var candidate []string
			var candidateImages []string
			if arr := content.Get("parts"); arr.IsArray() {
				arr.ForEach(func(_, part gjson.Result) bool {
					addModerationText(&candidate, part.Get("text").String())
					addGeminiModerationImage(&candidateImages, part)
					return true
				})
			}
			if normalizeContentModerationText(strings.Join(candidate, "\n")) != "" || len(candidateImages) > 0 {
				lastParts = candidate
				lastImages = candidateImages
			}
		}
		return true
	})
	*parts = append(*parts, lastParts...)
	*images = append(*images, lastImages...)
}

func collectContentValue(value gjson.Result, parts *[]string, images *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		addModerationText(parts, value.String())
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectContentValue(item, parts, images)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		addModerationImage(images, value.Get("image_url.url").String())
		addModerationImage(images, value.Get("image_url").String())
		addModerationImage(images, value.Get("url").String())
		addModerationImageData(images, value.Get("source.media_type").String(), value.Get("source.data").String())
		addModerationImageData(images, value.Get("source.mediaType").String(), value.Get("source.data").String())
		addModerationImageData(images, value.Get("media_type").String(), value.Get("data").String())
		addModerationImageData(images, value.Get("mime_type").String(), value.Get("data").String())
		addModerationImageData(images, value.Get("mimeType").String(), value.Get("data").String())
		addModerationImage(images, value.Get("source.data").String())
		addModerationImage(images, value.Get("data").String())
		addModerationImage(images, value.Get("base64").String())
		switch typ {
		case "", "text", "input_text", "message":
			if value.Get("text").Exists() {
				addModerationText(parts, value.Get("text").String())
			}
			if value.Get("content").Exists() {
				collectContentValue(value.Get("content"), parts, images)
			}
		case "image_url", "input_image", "image":
		}
	}
}

func addGeminiModerationImage(images *[]string, part gjson.Result) {
	if inlineData := part.Get("inline_data"); inlineData.IsObject() {
		mimeType := strings.TrimSpace(inlineData.Get("mime_type").String())
		data := strings.TrimSpace(inlineData.Get("data").String())
		if mimeType != "" && data != "" {
			addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
		}
	}
	if inlineData := part.Get("inlineData"); inlineData.IsObject() {
		mimeType := strings.TrimSpace(inlineData.Get("mimeType").String())
		data := strings.TrimSpace(inlineData.Get("data").String())
		if mimeType != "" && data != "" {
			addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
		}
	}
	addModerationImage(images, part.Get("file_data.file_uri").String())
	addModerationImage(images, part.Get("fileData.fileUri").String())
}

func addModerationImageData(images *[]string, mimeType string, data string) {
	mimeType = strings.TrimSpace(mimeType)
	data = strings.TrimSpace(data)
	if mimeType == "" || data == "" {
		return
	}
	addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
}

func addModerationImage(images *[]string, image string) {
	image = strings.TrimSpace(image)
	if image == "" {
		return
	}
	if strings.HasPrefix(image, "data:") || strings.HasPrefix(image, "http://") || strings.HasPrefix(image, "https://") {
		*images = append(*images, image)
	}
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

// addModerationText 收录全部文本；排除规则见 moderationInputCollector.AddText 的说明。
func addModerationText(parts *[]string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	*parts = append(*parts, text)
}

func normalizeContentModerationText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}
