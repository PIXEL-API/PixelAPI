package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// 显式列出已接入的模型，不把未来模型或未知快照自动送到直调端点。
func usesCodexDirectImages(model string) bool {
	switch strings.TrimSpace(model) {
	case "gpt-image-1.5", "gpt-image-2",
		"gpt-image-2.5-flare", "gpt-image-2.5-sunburst",
		"gpt-image-2.5-flare-2026-09-08", "gpt-image-2.5-sunburst-2026-09-08":
		return true
	default:
		return false
	}
}

// 正式转发与后台测试共用同一份端点选择和请求构造。
func buildOpenAIImagesOAuthPayload(parsed *OpenAIImagesRequest, model string) ([]byte, string, error) {
	if parsed == nil {
		return nil, "", fmt.Errorf("parsed images request is required")
	}
	if !usesCodexDirectImages(model) {
		body, err := buildOpenAIImagesResponsesRequest(parsed, model)
		return body, chatgptCodexURL, err
	}
	if strings.TrimSpace(parsed.Prompt) == "" {
		return nil, "", fmt.Errorf("prompt is required")
	}
	// 只把已解析并校验的图片字段发送给上游，避免客户端注入任意 JSON 字段。
	payload := make(map[string]any, 16)
	payload["model"] = model
	prompt := parsed.Prompt
	if !parsed.Multipart && gjson.ValidBytes(parsed.Body) {
		if rawPrompt := gjson.GetBytes(parsed.Body, "prompt").String(); rawPrompt != "" {
			prompt = rawPrompt
		}
	}
	payload["prompt"] = prompt
	for _, field := range []struct {
		key   string
		value string
	}{
		{"size", parsed.Size}, {"quality", parsed.Quality},
		{"background", parsed.Background}, {"output_format", parsed.OutputFormat},
		{"moderation", parsed.Moderation}, {"input_fidelity", parsed.InputFidelity},
		{"style", parsed.Style},
	} {
		if value := strings.TrimSpace(field.value); value != "" {
			payload[field.key] = value
		}
	}
	if parsed.N > 1 {
		payload["n"] = parsed.N
	}
	if parsed.OutputCompression != nil {
		payload["output_compression"] = *parsed.OutputCompression
	}
	if parsed.PartialImages != nil {
		payload["partial_images"] = *parsed.PartialImages
	}
	if parsed.Stream {
		payload["stream"] = true
	}

	endpoint := "/images/generations"
	if parsed.IsEdits() {
		endpoint = "/images/edits"
		images := make([]map[string]string, 0, len(parsed.InputImageURLs)+len(parsed.Uploads))
		for _, imageURL := range parsed.InputImageURLs {
			if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
				images = append(images, map[string]string{"image_url": imageURL})
			}
		}
		for _, upload := range parsed.Uploads {
			imageURL, err := openAIImageUploadToDataURL(upload)
			if err != nil {
				return nil, "", err
			}
			images = append(images, map[string]string{"image_url": imageURL})
		}
		if len(images) == 0 {
			return nil, "", fmt.Errorf("image input is required")
		}
		payload["images"] = images
		mask := strings.TrimSpace(parsed.MaskImageURL)
		if parsed.MaskUpload != nil {
			var err error
			mask, err = openAIImageUploadToDataURL(*parsed.MaskUpload)
			if err != nil {
				return nil, "", err
			}
		}
		if mask != "" {
			payload["mask"] = map[string]string{"image_url": mask}
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("marshal Codex Images request: %w", err)
	}
	return body, strings.TrimSuffix(chatgptCodexURL, "/responses") + endpoint, nil
}

// 原生 JSON 响应与后台图片预览使用同一份校验，防止空 data 被计为成功。
func parseCodexDirectImagesResponse(body []byte) ([]openAIResponsesImageResult, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("invalid Images API JSON response")
	}
	if _, message, failed := openAIImagesDirectStreamFailure("", body); failed {
		return nil, fmt.Errorf("upstream image generation failed: %s", message)
	}
	root := gjson.ParseBytes(body)
	results := make([]openAIResponsesImageResult, 0, len(root.Get("data").Array()))
	for _, item := range root.Get("data").Array() {
		result := strings.TrimSpace(item.Get("b64_json").String())
		if result == "" {
			continue
		}
		meta := func(key string) string {
			if value := item.Get(key).String(); value != "" {
				return value
			}
			return root.Get(key).String()
		}
		results = append(results, openAIResponsesImageResult{
			Result: result, RevisedPrompt: item.Get("revised_prompt").String(),
			OutputFormat: meta("output_format"), Size: meta("size"),
			Quality: meta("quality"), Background: meta("background"), Model: meta("model"),
		})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("Images API returned no image output")
	}
	reconcileOpenAIResponsesImageResultSizes(results, nil)
	return results, nil
}

func codexDirectImageURL(body []byte, path, outputFormat string) []byte {
	if result := gjson.GetBytes(body, path+"b64_json").String(); result != "" {
		body, _ = sjson.SetBytes(body, path+"url", "data:"+openAIImageOutputMIMEType(outputFormat)+";base64,"+result)
		body, _ = sjson.DeleteBytes(body, path+"b64_json")
	}
	return body
}

// Images 端点只输出图片；未提供输出分类时，output_tokens 全部是图片 token。
// 缓存图片数量只采信明确明细，不根据总缓存量猜测图文占比。
func codexDirectImagesUsage(body []byte) (OpenAIUsage, bool) {
	value := gjson.GetBytes(body, "usage")
	usage, ok := openAIUsageFromGJSON(value)
	if !ok {
		return usage, false
	}
	if !value.Get("output_tokens_details.image_tokens").Exists() {
		usage.ImageOutputTokens = max(usage.OutputTokens-usage.TextOutputTokens, 0)
	}
	cached := value.Get("input_tokens_details.cached_tokens_details")
	if !value.Get("input_tokens_details.cached_tokens").Exists() && cached.IsObject() {
		imageTokens := max(int(cached.Get("image_tokens").Int()), 0)
		textTokens := max(int(cached.Get("text_tokens").Int()), 0)
		usage.CacheReadInputTokens = min(imageTokens, max(usage.InputTokens, 0))
		usage.CacheReadInputTokens += min(textTokens, max(usage.InputTokens-usage.CacheReadInputTokens, 0))
	}
	imageCached := max(int(cached.Get("image_tokens").Int()), 0)
	textCached := max(int(cached.Get("text_tokens").Int()), 0)
	usage.ImageCacheReadInputTokens = min(imageCached, max(usage.ImageInputTokens, 0), max(usage.CacheReadInputTokens, 0))
	usage.TextCacheReadInputTokens = min(textCached, max(usage.CacheReadInputTokens-usage.ImageCacheReadInputTokens, 0))
	return usage, true
}

func (s *OpenAIGatewayService) handleCodexDirectImagesNonStreamingResponse(ctx context.Context, resp *http.Response, c *gin.Context, parsed *OpenAIImagesRequest) (OpenAIUsage, int, []string, error) {
	body, err := ReadUpstreamResponseBodyWithContext(ctx, resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return OpenAIUsage{}, 0, nil, err
	}
	results, err := parseCodexDirectImagesResponse(body)
	if err != nil {
		return OpenAIUsage{}, 0, nil, err
	}
	usage, _ := codexDirectImagesUsage(body)
	if observer := upstreamResponseModelObserverFromContext(c); observer != nil {
		observer.Observe(gjson.GetBytes(body, "model").String(), true)
		for _, result := range results {
			observer.Observe(result.Model, true)
		}
	}
	clientModel := strings.TrimSpace(parsed.Model)
	if clientModel != "" {
		for i := range gjson.GetBytes(body, "data").Array() {
			body, _ = sjson.SetBytes(body, fmt.Sprintf("data.%d.model", i), clientModel)
		}
	}
	for i, item := range gjson.GetBytes(body, "data").Array() {
		if actualSize := detectOpenAIImageResultSize(item.Get("b64_json").String()); actualSize != "" {
			body, _ = sjson.SetBytes(body, fmt.Sprintf("data.%d.size", i), actualSize)
			if i == 0 {
				body, _ = sjson.SetBytes(body, "size", actualSize)
			}
		}
	}
	if parsed.ResponseFormat == "url" {
		for i, item := range gjson.GetBytes(body, "data").Array() {
			format := item.Get("output_format").String()
			if format == "" {
				format = gjson.GetBytes(body, "output_format").String()
			}
			if format == "" {
				format = parsed.OutputFormat
			}
			body = codexDirectImageURL(body, fmt.Sprintf("data.%d.", i), format)
		}
	}
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	contentType := "application/json"
	if s.cfg != nil && !s.cfg.Security.ResponseHeaders.Enabled {
		if upstreamType := strings.TrimSpace(resp.Header.Get("Content-Type")); upstreamType != "" {
			contentType = upstreamType
		}
	}
	c.Data(resp.StatusCode, contentType, body)
	return usage, len(results), openAIResponsesImageResultSizes(results), nil
}

type codexDirectImagesStream struct {
	request *OpenAIImagesRequest
	seen    map[[sha256.Size]byte]struct{}
	sizes   []string
	count   int
}

func (s *codexDirectImagesStream) transform(eventName string, data []byte) (string, []byte, error) {
	if !gjson.ValidBytes(data) {
		return "", nil, fmt.Errorf("invalid Images API stream JSON")
	}
	eventType := strings.TrimSpace(gjson.GetBytes(data, "type").String())
	if eventType == "" {
		eventType = strings.TrimSpace(eventName)
	}
	if s.request != nil && s.request.IsEdits() && strings.HasPrefix(eventType, "image_generation.") {
		eventType = strings.Replace(eventType, "image_generation.", "image_edit.", 1)
	}
	data, err := sjson.SetBytes(data, "type", eventType)
	if err != nil {
		return "", nil, err
	}
	if s.request != nil && strings.TrimSpace(s.request.Model) != "" {
		data, err = sjson.SetBytes(data, "model", strings.TrimSpace(s.request.Model))
		if err != nil {
			return "", nil, err
		}
	}
	encoded := gjson.GetBytes(data, "b64_json").String()
	if strings.HasSuffix(eventType, ".completed") && encoded != "" {
		key := sha256.Sum256([]byte(encoded))
		if s.seen == nil {
			s.seen = make(map[[sha256.Size]byte]struct{})
		}
		if _, exists := s.seen[key]; !exists {
			s.seen[key] = struct{}{}
			s.count++
			size := detectOpenAIImageResultSize(encoded)
			if size == "" {
				size = strings.TrimSpace(gjson.GetBytes(data, "size").String())
			}
			if size != "" {
				s.sizes = append(s.sizes, size)
				data, err = sjson.SetBytes(data, "size", size)
				if err != nil {
					return "", nil, err
				}
			}
		}
	}
	if s.request != nil && s.request.ResponseFormat == "url" && encoded != "" {
		format := strings.TrimSpace(gjson.GetBytes(data, "output_format").String())
		if format == "" {
			format = s.request.OutputFormat
		}
		data = codexDirectImageURL(data, "", format)
	}
	return eventType, data, nil
}
