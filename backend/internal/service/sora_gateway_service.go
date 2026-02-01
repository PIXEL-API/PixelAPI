package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

var soraSSEDataRe = regexp.MustCompile(`^data:\s*`)
var soraImageMarkdownRe = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
var soraVideoHTMLRe = regexp.MustCompile(`(?i)<video[^>]+src=['"]([^'"]+)['"]`)

const soraRewriteBufferLimit = 2048

var soraImageSizeMap = map[string]string{
	"gpt-image":           "360",
	"gpt-image-landscape": "540",
	"gpt-image-portrait":  "540",
}

type soraStreamingResult struct {
	mediaType    string
	mediaURLs    []string
	imageCount   int
	imageSize    string
	firstTokenMs *int
}

// SoraGatewayService handles forwarding requests to Sora upstream.
type SoraGatewayService struct {
	soraClient       SoraClient
	mediaStorage     *SoraMediaStorage
	rateLimitService *RateLimitService
	cfg              *config.Config
}

func NewSoraGatewayService(
	soraClient SoraClient,
	mediaStorage *SoraMediaStorage,
	rateLimitService *RateLimitService,
	cfg *config.Config,
) *SoraGatewayService {
	return &SoraGatewayService{
		soraClient:       soraClient,
		mediaStorage:     mediaStorage,
		rateLimitService: rateLimitService,
		cfg:              cfg,
	}
}

func (s *SoraGatewayService) Forward(ctx context.Context, c *gin.Context, account *Account, body []byte, clientStream bool) (*ForwardResult, error) {
	startTime := time.Now()

	if s.soraClient == nil || !s.soraClient.Enabled() {
		if c != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{
					"type":    "api_error",
					"message": "Sora 上游未配置",
				},
			})
		}
		return nil, errors.New("sora upstream not configured")
	}

	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body", clientStream)
		return nil, fmt.Errorf("parse request: %w", err)
	}
	reqModel, _ := reqBody["model"].(string)
	reqStream, _ := reqBody["stream"].(bool)
	if strings.TrimSpace(reqModel) == "" {
		s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", "model is required", clientStream)
		return nil, errors.New("model is required")
	}

	mappedModel := account.GetMappedModel(reqModel)
	if mappedModel != "" && mappedModel != reqModel {
		reqModel = mappedModel
	}

	modelCfg, ok := GetSoraModelConfig(reqModel)
	if !ok {
		s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", "Unsupported Sora model", clientStream)
		return nil, fmt.Errorf("unsupported model: %s", reqModel)
	}
	if modelCfg.Type == "prompt_enhance" {
		s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", "Prompt-enhance 模型暂未支持", clientStream)
		return nil, fmt.Errorf("prompt-enhance not supported")
	}

	prompt, imageInput, videoInput, remixTargetID := extractSoraInput(reqBody)
	if strings.TrimSpace(prompt) == "" {
		s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", "prompt is required", clientStream)
		return nil, errors.New("prompt is required")
	}
	if strings.TrimSpace(videoInput) != "" {
		s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", "Video input is not supported yet", clientStream)
		return nil, errors.New("video input not supported")
	}

	reqCtx, cancel := s.withSoraTimeout(ctx, reqStream)
	if cancel != nil {
		defer cancel()
	}

	var imageData []byte
	imageFilename := ""
	if strings.TrimSpace(imageInput) != "" {
		decoded, filename, err := decodeSoraImageInput(reqCtx, imageInput)
		if err != nil {
			s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", err.Error(), clientStream)
			return nil, err
		}
		imageData = decoded
		imageFilename = filename
	}

	mediaID := ""
	if len(imageData) > 0 {
		uploadID, err := s.soraClient.UploadImage(reqCtx, account, imageData, imageFilename)
		if err != nil {
			return nil, s.handleSoraRequestError(ctx, account, err, reqModel, c, clientStream)
		}
		mediaID = uploadID
	}

	taskID := ""
	var err error
	switch modelCfg.Type {
	case "image":
		taskID, err = s.soraClient.CreateImageTask(reqCtx, account, SoraImageRequest{
			Prompt:  prompt,
			Width:   modelCfg.Width,
			Height:  modelCfg.Height,
			MediaID: mediaID,
		})
	case "video":
		taskID, err = s.soraClient.CreateVideoTask(reqCtx, account, SoraVideoRequest{
			Prompt:        prompt,
			Orientation:   modelCfg.Orientation,
			Frames:        modelCfg.Frames,
			Model:         modelCfg.Model,
			Size:          modelCfg.Size,
			MediaID:       mediaID,
			RemixTargetID: remixTargetID,
		})
	default:
		err = fmt.Errorf("unsupported model type: %s", modelCfg.Type)
	}
	if err != nil {
		return nil, s.handleSoraRequestError(ctx, account, err, reqModel, c, clientStream)
	}

	if clientStream && c != nil {
		s.prepareSoraStream(c, taskID)
	}

	var mediaURLs []string
	mediaType := modelCfg.Type
	imageCount := 0
	imageSize := ""
	if modelCfg.Type == "image" {
		urls, pollErr := s.pollImageTask(reqCtx, c, account, taskID, clientStream)
		if pollErr != nil {
			return nil, s.handleSoraRequestError(ctx, account, pollErr, reqModel, c, clientStream)
		}
		mediaURLs = urls
		imageCount = len(urls)
		imageSize = soraImageSizeFromModel(reqModel)
	} else if modelCfg.Type == "video" {
		urls, pollErr := s.pollVideoTask(reqCtx, c, account, taskID, clientStream)
		if pollErr != nil {
			return nil, s.handleSoraRequestError(ctx, account, pollErr, reqModel, c, clientStream)
		}
		mediaURLs = urls
	} else {
		mediaType = "prompt"
	}

	finalURLs := mediaURLs
	if len(mediaURLs) > 0 && s.mediaStorage != nil && s.mediaStorage.Enabled() {
		stored, storeErr := s.mediaStorage.StoreFromURLs(reqCtx, mediaType, mediaURLs)
		if storeErr != nil {
			return nil, s.handleSoraRequestError(ctx, account, storeErr, reqModel, c, clientStream)
		}
		finalURLs = s.normalizeSoraMediaURLs(stored)
	} else {
		finalURLs = s.normalizeSoraMediaURLs(mediaURLs)
	}

	content := buildSoraContent(mediaType, finalURLs)
	var firstTokenMs *int
	if clientStream {
		ms, streamErr := s.writeSoraStream(c, reqModel, content, startTime)
		if streamErr != nil {
			return nil, streamErr
		}
		firstTokenMs = ms
	} else if c != nil {
		response := buildSoraNonStreamResponse(content, reqModel)
		if len(finalURLs) > 0 {
			response["media_url"] = finalURLs[0]
			if len(finalURLs) > 1 {
				response["media_urls"] = finalURLs
			}
		}
		c.JSON(http.StatusOK, response)
	}

	return &ForwardResult{
		RequestID:    taskID,
		Model:        reqModel,
		Stream:       clientStream,
		Duration:     time.Since(startTime),
		FirstTokenMs: firstTokenMs,
		Usage:        ClaudeUsage{},
		MediaType:    mediaType,
		MediaURL:     firstMediaURL(finalURLs),
		ImageCount:   imageCount,
		ImageSize:    imageSize,
	}, nil
}

func (s *SoraGatewayService) withSoraTimeout(ctx context.Context, stream bool) (context.Context, context.CancelFunc) {
	if s == nil || s.cfg == nil {
		return ctx, nil
	}
	timeoutSeconds := s.cfg.Gateway.SoraRequestTimeoutSeconds
	if stream {
		timeoutSeconds = s.cfg.Gateway.SoraStreamTimeoutSeconds
	}
	if timeoutSeconds <= 0 {
		return ctx, nil
	}
	return context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
}

func (s *SoraGatewayService) setUpstreamRequestError(c *gin.Context, account *Account, err error) {
	safeErr := sanitizeUpstreamErrorMessage(err.Error())
	setOpsUpstreamError(c, 0, safeErr, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: 0,
		Kind:               "request_error",
		Message:            safeErr,
	})
	if c != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"type":    "upstream_error",
				"message": "Upstream request failed",
			},
		})
	}
}

func (s *SoraGatewayService) shouldFailoverUpstreamError(statusCode int) bool {
	switch statusCode {
	case 401, 402, 403, 429, 529:
		return true
	default:
		return statusCode >= 500
	}
}

func (s *SoraGatewayService) handleFailoverSideEffects(ctx context.Context, resp *http.Response, account *Account) {
	if s.rateLimitService == nil || account == nil || resp == nil {
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, body)
}

func (s *SoraGatewayService) handleErrorResponse(ctx context.Context, resp *http.Response, c *gin.Context, account *Account, reqModel string) (*ForwardResult, error) {
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	if msg := soraProErrorMessage(reqModel, upstreamMsg); msg != "" {
		upstreamMsg = msg
	}

	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(respBody), maxBytes)
	}
	setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: resp.StatusCode,
		UpstreamRequestID:  resp.Header.Get("x-request-id"),
		Kind:               "http_error",
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})

	if c != nil {
		responsePayload := s.buildErrorPayload(respBody, upstreamMsg)
		c.JSON(resp.StatusCode, responsePayload)
	}
	if upstreamMsg == "" {
		return nil, fmt.Errorf("upstream error: %d", resp.StatusCode)
	}
	return nil, fmt.Errorf("upstream error: %d message=%s", resp.StatusCode, upstreamMsg)
}

func (s *SoraGatewayService) buildErrorPayload(respBody []byte, overrideMessage string) map[string]any {
	if len(respBody) > 0 {
		var payload map[string]any
		if err := json.Unmarshal(respBody, &payload); err == nil {
			if errObj, ok := payload["error"].(map[string]any); ok {
				if overrideMessage != "" {
					errObj["message"] = overrideMessage
				}
				payload["error"] = errObj
				return payload
			}
		}
	}
	return map[string]any{
		"error": map[string]any{
			"type":    "upstream_error",
			"message": overrideMessage,
		},
	}
}

func (s *SoraGatewayService) handleStreamingResponse(ctx context.Context, resp *http.Response, c *gin.Context, account *Account, startTime time.Time, originalModel string, clientStream bool) (*soraStreamingResult, error) {
	if resp == nil {
		return nil, errors.New("empty response")
	}

	if clientStream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		if v := resp.Header.Get("x-request-id"); v != "" {
			c.Header("x-request-id", v)
		}
	}

	w := c.Writer
	flusher, _ := w.(http.Flusher)

	contentBuilder := strings.Builder{}
	var firstTokenMs *int
	var upstreamError error
	rewriteBuffer := ""

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanner.Buffer(make([]byte, 64*1024), maxLineSize)

	sendLine := func(line string) error {
		if !clientStream {
			return nil
		}
		if _, err := fmt.Fprintf(w, "%s\n", line); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if soraSSEDataRe.MatchString(line) {
			data := soraSSEDataRe.ReplaceAllString(line, "")
			if data == "[DONE]" {
				if rewriteBuffer != "" {
					flushLine, flushContent, err := s.flushSoraRewriteBuffer(rewriteBuffer, originalModel)
					if err != nil {
						return nil, err
					}
					if flushLine != "" {
						if flushContent != "" {
							if _, err := contentBuilder.WriteString(flushContent); err != nil {
								return nil, err
							}
						}
						if err := sendLine(flushLine); err != nil {
							return nil, err
						}
					}
					rewriteBuffer = ""
				}
				if err := sendLine("data: [DONE]"); err != nil {
					return nil, err
				}
				break
			}
			updatedLine, contentDelta, errEvent := s.processSoraSSEData(data, originalModel, &rewriteBuffer)
			if errEvent != nil && upstreamError == nil {
				upstreamError = errEvent
			}
			if contentDelta != "" {
				if firstTokenMs == nil {
					ms := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &ms
				}
				if _, err := contentBuilder.WriteString(contentDelta); err != nil {
					return nil, err
				}
			}
			if err := sendLine(updatedLine); err != nil {
				return nil, err
			}
			continue
		}
		if err := sendLine(line); err != nil {
			return nil, err
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			if clientStream {
				_, _ = fmt.Fprintf(w, "event: error\ndata: {\"error\":\"response_too_large\"}\n\n")
				if flusher != nil {
					flusher.Flush()
				}
			}
			return nil, err
		}
		if ctx.Err() == context.DeadlineExceeded && s.rateLimitService != nil && account != nil {
			s.rateLimitService.HandleStreamTimeout(ctx, account, originalModel)
		}
		if clientStream {
			_, _ = fmt.Fprintf(w, "event: error\ndata: {\"error\":\"stream_read_error\"}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
		return nil, err
	}

	content := contentBuilder.String()
	mediaType, mediaURLs := s.extractSoraMedia(content)
	if mediaType == "" && isSoraPromptEnhanceModel(originalModel) {
		mediaType = "prompt"
	}
	imageSize := ""
	imageCount := 0
	if mediaType == "image" {
		imageSize = soraImageSizeFromModel(originalModel)
		imageCount = len(mediaURLs)
	}

	if upstreamError != nil && !clientStream {
		if c != nil {
			c.JSON(http.StatusBadGateway, map[string]any{
				"error": map[string]any{
					"type":    "upstream_error",
					"message": upstreamError.Error(),
				},
			})
		}
		return nil, upstreamError
	}

	if !clientStream {
		response := buildSoraNonStreamResponse(content, originalModel)
		if len(mediaURLs) > 0 {
			response["media_url"] = mediaURLs[0]
			if len(mediaURLs) > 1 {
				response["media_urls"] = mediaURLs
			}
		}
		c.JSON(http.StatusOK, response)
	}

	return &soraStreamingResult{
		mediaType:    mediaType,
		mediaURLs:    mediaURLs,
		imageCount:   imageCount,
		imageSize:    imageSize,
		firstTokenMs: firstTokenMs,
	}, nil
}

func (s *SoraGatewayService) processSoraSSEData(data string, originalModel string, rewriteBuffer *string) (string, string, error) {
	if strings.TrimSpace(data) == "" {
		return "data: ", "", nil
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return "data: " + data, "", nil
	}

	if errObj, ok := payload["error"].(map[string]any); ok {
		if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return "data: " + data, "", errors.New(msg)
		}
	}

	if model, ok := payload["model"].(string); ok && model != "" && originalModel != "" {
		payload["model"] = originalModel
	}

	contentDelta, updated := extractSoraContent(payload)
	if updated {
		var rewritten string
		if rewriteBuffer != nil {
			rewritten = s.rewriteSoraContentWithBuffer(contentDelta, rewriteBuffer)
		} else {
			rewritten = s.rewriteSoraContent(contentDelta)
		}
		if rewritten != contentDelta {
			applySoraContent(payload, rewritten)
			contentDelta = rewritten
		}
	}

	updatedData, err := json.Marshal(payload)
	if err != nil {
		return "data: " + data, contentDelta, nil
	}
	return "data: " + string(updatedData), contentDelta, nil
}

func extractSoraContent(payload map[string]any) (string, bool) {
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return "", false
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return "", false
	}
	if delta, ok := choice["delta"].(map[string]any); ok {
		if content, ok := delta["content"].(string); ok {
			return content, true
		}
	}
	if message, ok := choice["message"].(map[string]any); ok {
		if content, ok := message["content"].(string); ok {
			return content, true
		}
	}
	return "", false
}

func applySoraContent(payload map[string]any, content string) {
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return
	}
	if delta, ok := choice["delta"].(map[string]any); ok {
		delta["content"] = content
		choice["delta"] = delta
		return
	}
	if message, ok := choice["message"].(map[string]any); ok {
		message["content"] = content
		choice["message"] = message
	}
}

func (s *SoraGatewayService) rewriteSoraContentWithBuffer(contentDelta string, buffer *string) string {
	if buffer == nil {
		return s.rewriteSoraContent(contentDelta)
	}
	if contentDelta == "" && *buffer == "" {
		return ""
	}
	combined := *buffer + contentDelta
	rewritten := s.rewriteSoraContent(combined)
	bufferStart := s.findSoraRewriteBufferStart(rewritten)
	if bufferStart < 0 {
		*buffer = ""
		return rewritten
	}
	if len(rewritten)-bufferStart > soraRewriteBufferLimit {
		bufferStart = len(rewritten) - soraRewriteBufferLimit
	}
	output := rewritten[:bufferStart]
	*buffer = rewritten[bufferStart:]
	return output
}

func (s *SoraGatewayService) findSoraRewriteBufferStart(content string) int {
	minIndex := -1
	start := 0
	for {
		idx := strings.Index(content[start:], "![")
		if idx < 0 {
			break
		}
		idx += start
		if !hasSoraImageMatchAt(content, idx) {
			if minIndex == -1 || idx < minIndex {
				minIndex = idx
			}
		}
		start = idx + 2
	}
	lower := strings.ToLower(content)
	start = 0
	for {
		idx := strings.Index(lower[start:], "<video")
		if idx < 0 {
			break
		}
		idx += start
		if !hasSoraVideoMatchAt(content, idx) {
			if minIndex == -1 || idx < minIndex {
				minIndex = idx
			}
		}
		start = idx + len("<video")
	}
	return minIndex
}

func hasSoraImageMatchAt(content string, idx int) bool {
	if idx < 0 || idx >= len(content) {
		return false
	}
	loc := soraImageMarkdownRe.FindStringIndex(content[idx:])
	return loc != nil && loc[0] == 0
}

func hasSoraVideoMatchAt(content string, idx int) bool {
	if idx < 0 || idx >= len(content) {
		return false
	}
	loc := soraVideoHTMLRe.FindStringIndex(content[idx:])
	return loc != nil && loc[0] == 0
}

func (s *SoraGatewayService) rewriteSoraContent(content string) string {
	if content == "" {
		return content
	}
	content = soraImageMarkdownRe.ReplaceAllStringFunc(content, func(match string) string {
		sub := soraImageMarkdownRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		rewritten := s.rewriteSoraURL(sub[1])
		if rewritten == sub[1] {
			return match
		}
		return strings.Replace(match, sub[1], rewritten, 1)
	})
	content = soraVideoHTMLRe.ReplaceAllStringFunc(content, func(match string) string {
		sub := soraVideoHTMLRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		rewritten := s.rewriteSoraURL(sub[1])
		if rewritten == sub[1] {
			return match
		}
		return strings.Replace(match, sub[1], rewritten, 1)
	})
	return content
}

func (s *SoraGatewayService) flushSoraRewriteBuffer(buffer string, originalModel string) (string, string, error) {
	if buffer == "" {
		return "", "", nil
	}
	rewritten := s.rewriteSoraContent(buffer)
	payload := map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"content": rewritten,
				},
				"index": 0,
			},
		},
	}
	if originalModel != "" {
		payload["model"] = originalModel
	}
	updatedData, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	return "data: " + string(updatedData), rewritten, nil
}

func (s *SoraGatewayService) rewriteSoraURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	path := parsed.Path
	if !strings.HasPrefix(path, "/tmp/") && !strings.HasPrefix(path, "/static/") {
		return raw
	}
	return s.buildSoraMediaURL(path, parsed.RawQuery)
}

func (s *SoraGatewayService) extractSoraMedia(content string) (string, []string) {
	if content == "" {
		return "", nil
	}
	if match := soraVideoHTMLRe.FindStringSubmatch(content); len(match) > 1 {
		return "video", []string{match[1]}
	}
	imageMatches := soraImageMarkdownRe.FindAllStringSubmatch(content, -1)
	if len(imageMatches) == 0 {
		return "", nil
	}
	urls := make([]string, 0, len(imageMatches))
	for _, match := range imageMatches {
		if len(match) > 1 {
			urls = append(urls, match[1])
		}
	}
	return "image", urls
}

func buildSoraNonStreamResponse(content, model string) map[string]any {
	return map[string]any{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
	}
}

func soraImageSizeFromModel(model string) string {
	modelLower := strings.ToLower(model)
	if size, ok := soraImageSizeMap[modelLower]; ok {
		return size
	}
	if strings.Contains(modelLower, "landscape") || strings.Contains(modelLower, "portrait") {
		return "540"
	}
	return "360"
}

func isSoraPromptEnhanceModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "prompt-enhance")
}

func soraProErrorMessage(model, upstreamMsg string) string {
	modelLower := strings.ToLower(model)
	if strings.Contains(modelLower, "sora2pro-hd") {
		return "当前账号无法使用 Sora Pro-HD 模型，请更换模型或账号"
	}
	if strings.Contains(modelLower, "sora2pro") {
		return "当前账号无法使用 Sora Pro 模型，请更换模型或账号"
	}
	return ""
}

func firstMediaURL(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func (s *SoraGatewayService) buildSoraMediaURL(path string, rawQuery string) string {
	if path == "" {
		return path
	}
	prefix := "/sora/media"
	values := url.Values{}
	if rawQuery != "" {
		if parsed, err := url.ParseQuery(rawQuery); err == nil {
			values = parsed
		}
	}

	signKey := ""
	ttlSeconds := 0
	if s != nil && s.cfg != nil {
		signKey = strings.TrimSpace(s.cfg.Gateway.SoraMediaSigningKey)
		ttlSeconds = s.cfg.Gateway.SoraMediaSignedURLTTLSeconds
	}
	values.Del("sig")
	values.Del("expires")
	signingQuery := values.Encode()
	if signKey != "" && ttlSeconds > 0 {
		expires := time.Now().Add(time.Duration(ttlSeconds) * time.Second).Unix()
		signature := SignSoraMediaURL(path, signingQuery, expires, signKey)
		if signature != "" {
			values.Set("expires", strconv.FormatInt(expires, 10))
			values.Set("sig", signature)
			prefix = "/sora/media-signed"
		}
	}

	encoded := values.Encode()
	if encoded == "" {
		return prefix + path
	}
	return prefix + path + "?" + encoded
}

func (s *SoraGatewayService) prepareSoraStream(c *gin.Context, requestID string) {
	if c == nil {
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	if strings.TrimSpace(requestID) != "" {
		c.Header("x-request-id", requestID)
	}
}

func (s *SoraGatewayService) writeSoraStream(c *gin.Context, model, content string, startTime time.Time) (*int, error) {
	if c == nil {
		return nil, nil
	}
	writer := c.Writer
	flusher, _ := writer.(http.Flusher)

	chunk := map[string]any{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{
			map[string]any{
				"index": 0,
				"delta": map[string]any{
					"content": content,
				},
			},
		},
	}
	encoded, _ := json.Marshal(chunk)
	if _, err := fmt.Fprintf(writer, "data: %s\n\n", encoded); err != nil {
		return nil, err
	}
	if flusher != nil {
		flusher.Flush()
	}
	ms := int(time.Since(startTime).Milliseconds())
	finalChunk := map[string]any{
		"id":      chunk["id"],
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{
			map[string]any{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": "stop",
			},
		},
	}
	finalEncoded, _ := json.Marshal(finalChunk)
	if _, err := fmt.Fprintf(writer, "data: %s\n\n", finalEncoded); err != nil {
		return &ms, err
	}
	if _, err := fmt.Fprint(writer, "data: [DONE]\n\n"); err != nil {
		return &ms, err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return &ms, nil
}

func (s *SoraGatewayService) writeSoraError(c *gin.Context, status int, errType, message string, stream bool) {
	if c == nil {
		return
	}
	if stream {
		flusher, _ := c.Writer.(http.Flusher)
		errorEvent := fmt.Sprintf(`event: error`+"\n"+`data: {"error": {"type": "%s", "message": "%s"}}`+"\n\n", errType, message)
		_, _ = fmt.Fprint(c.Writer, errorEvent)
		_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		return
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

func (s *SoraGatewayService) handleSoraRequestError(ctx context.Context, account *Account, err error, model string, c *gin.Context, stream bool) error {
	if err == nil {
		return nil
	}
	var upstreamErr *SoraUpstreamError
	if errors.As(err, &upstreamErr) {
		if s.rateLimitService != nil && account != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, upstreamErr.StatusCode, upstreamErr.Headers, upstreamErr.Body)
		}
		if s.shouldFailoverUpstreamError(upstreamErr.StatusCode) {
			return &UpstreamFailoverError{StatusCode: upstreamErr.StatusCode}
		}
		msg := upstreamErr.Message
		if override := soraProErrorMessage(model, msg); override != "" {
			msg = override
		}
		s.writeSoraError(c, upstreamErr.StatusCode, "upstream_error", msg, stream)
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		s.writeSoraError(c, http.StatusGatewayTimeout, "timeout_error", "Sora generation timeout", stream)
		return err
	}
	s.writeSoraError(c, http.StatusBadGateway, "api_error", err.Error(), stream)
	return err
}

func (s *SoraGatewayService) pollImageTask(ctx context.Context, c *gin.Context, account *Account, taskID string, stream bool) ([]string, error) {
	interval := s.pollInterval()
	maxAttempts := s.pollMaxAttempts()
	lastPing := time.Now()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		status, err := s.soraClient.GetImageTask(ctx, account, taskID)
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(status.Status) {
		case "succeeded", "completed":
			return status.URLs, nil
		case "failed":
			if status.ErrorMsg != "" {
				return nil, errors.New(status.ErrorMsg)
			}
			return nil, errors.New("Sora image generation failed")
		}
		if stream {
			s.maybeSendPing(c, &lastPing)
		}
		if err := sleepWithContext(ctx, interval); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("Sora image generation timeout")
}

func (s *SoraGatewayService) pollVideoTask(ctx context.Context, c *gin.Context, account *Account, taskID string, stream bool) ([]string, error) {
	interval := s.pollInterval()
	maxAttempts := s.pollMaxAttempts()
	lastPing := time.Now()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		status, err := s.soraClient.GetVideoTask(ctx, account, taskID)
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(status.Status) {
		case "completed", "succeeded":
			return status.URLs, nil
		case "failed":
			if status.ErrorMsg != "" {
				return nil, errors.New(status.ErrorMsg)
			}
			return nil, errors.New("Sora video generation failed")
		}
		if stream {
			s.maybeSendPing(c, &lastPing)
		}
		if err := sleepWithContext(ctx, interval); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("Sora video generation timeout")
}

func (s *SoraGatewayService) pollInterval() time.Duration {
	if s == nil || s.cfg == nil {
		return 2 * time.Second
	}
	interval := s.cfg.Sora.Client.PollIntervalSeconds
	if interval <= 0 {
		interval = 2
	}
	return time.Duration(interval) * time.Second
}

func (s *SoraGatewayService) pollMaxAttempts() int {
	if s == nil || s.cfg == nil {
		return 600
	}
	maxAttempts := s.cfg.Sora.Client.MaxPollAttempts
	if maxAttempts <= 0 {
		maxAttempts = 600
	}
	return maxAttempts
}

func (s *SoraGatewayService) maybeSendPing(c *gin.Context, lastPing *time.Time) {
	if c == nil {
		return
	}
	interval := 10 * time.Second
	if s != nil && s.cfg != nil && s.cfg.Concurrency.PingInterval > 0 {
		interval = time.Duration(s.cfg.Concurrency.PingInterval) * time.Second
	}
	if time.Since(*lastPing) < interval {
		return
	}
	if _, err := fmt.Fprint(c.Writer, ":\n\n"); err == nil {
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush()
		}
		*lastPing = time.Now()
	}
}

func (s *SoraGatewayService) normalizeSoraMediaURLs(urls []string) []string {
	if len(urls) == 0 {
		return urls
	}
	output := make([]string, 0, len(urls))
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
			output = append(output, raw)
			continue
		}
		pathVal := raw
		if !strings.HasPrefix(pathVal, "/") {
			pathVal = "/" + pathVal
		}
		output = append(output, s.buildSoraMediaURL(pathVal, ""))
	}
	return output
}

func buildSoraContent(mediaType string, urls []string) string {
	switch mediaType {
	case "image":
		parts := make([]string, 0, len(urls))
		for _, u := range urls {
			parts = append(parts, fmt.Sprintf("![image](%s)", u))
		}
		return strings.Join(parts, "\n")
	case "video":
		if len(urls) == 0 {
			return ""
		}
		return fmt.Sprintf("```html\n<video src='%s' controls></video>\n```", urls[0])
	default:
		return ""
	}
}

func extractSoraInput(body map[string]any) (prompt, imageInput, videoInput, remixTargetID string) {
	if body == nil {
		return "", "", "", ""
	}
	if v, ok := body["remix_target_id"].(string); ok {
		remixTargetID = v
	}
	if v, ok := body["image"].(string); ok {
		imageInput = v
	}
	if v, ok := body["video"].(string); ok {
		videoInput = v
	}
	if v, ok := body["prompt"].(string); ok && strings.TrimSpace(v) != "" {
		prompt = v
	}
	if messages, ok := body["messages"].([]any); ok {
		builder := strings.Builder{}
		for _, raw := range messages {
			msg, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			role, _ := msg["role"].(string)
			if role != "" && role != "user" {
				continue
			}
			content := msg["content"]
			text, img, vid := parseSoraMessageContent(content)
			if text != "" {
				if builder.Len() > 0 {
					builder.WriteString("\n")
				}
				builder.WriteString(text)
			}
			if imageInput == "" && img != "" {
				imageInput = img
			}
			if videoInput == "" && vid != "" {
				videoInput = vid
			}
		}
		if prompt == "" {
			prompt = builder.String()
		}
	}
	return prompt, imageInput, videoInput, remixTargetID
}

func parseSoraMessageContent(content any) (text, imageInput, videoInput string) {
	switch val := content.(type) {
	case string:
		return val, "", ""
	case []any:
		builder := strings.Builder{}
		for _, item := range val {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t, _ := itemMap["type"].(string)
			switch t {
			case "text":
				if txt, ok := itemMap["text"].(string); ok && strings.TrimSpace(txt) != "" {
					if builder.Len() > 0 {
						builder.WriteString("\n")
					}
					builder.WriteString(txt)
				}
			case "image_url":
				if imageInput == "" {
					if urlVal, ok := itemMap["image_url"].(map[string]any); ok {
						imageInput = fmt.Sprintf("%v", urlVal["url"])
					} else if urlStr, ok := itemMap["image_url"].(string); ok {
						imageInput = urlStr
					}
				}
			case "video_url":
				if videoInput == "" {
					if urlVal, ok := itemMap["video_url"].(map[string]any); ok {
						videoInput = fmt.Sprintf("%v", urlVal["url"])
					} else if urlStr, ok := itemMap["video_url"].(string); ok {
						videoInput = urlStr
					}
				}
			}
		}
		return builder.String(), imageInput, videoInput
	default:
		return "", "", ""
	}
}

func decodeSoraImageInput(ctx context.Context, input string) ([]byte, string, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return nil, "", errors.New("empty image input")
	}
	if strings.HasPrefix(raw, "data:") {
		parts := strings.SplitN(raw, ",", 2)
		if len(parts) != 2 {
			return nil, "", errors.New("invalid data url")
		}
		meta := parts[0]
		payload := parts[1]
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", err
		}
		ext := ""
		if strings.HasPrefix(meta, "data:") {
			metaParts := strings.SplitN(meta[5:], ";", 2)
			if len(metaParts) > 0 {
				if exts, err := mime.ExtensionsByType(metaParts[0]); err == nil && len(exts) > 0 {
					ext = exts[0]
				}
			}
		}
		filename := "image" + ext
		return decoded, filename, nil
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return downloadSoraImageInput(ctx, raw)
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, "", errors.New("invalid base64 image")
	}
	return decoded, "image.png", nil
}

func downloadSoraImageInput(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download image failed: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, "", err
	}
	ext := fileExtFromURL(rawURL)
	if ext == "" {
		ext = fileExtFromContentType(resp.Header.Get("Content-Type"))
	}
	filename := "image" + ext
	return data, filename, nil
}
