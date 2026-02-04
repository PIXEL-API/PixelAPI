//nolint:unused
package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var soraSSEDataRe = regexp.MustCompile(`^data:\s*`)
var soraImageMarkdownRe = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
var soraVideoHTMLRe = regexp.MustCompile(`(?i)<video[^>]+src=['"]([^'"]+)['"]`)

const soraRewriteBufferLimit = 2048

type soraStreamingResult struct {
	mediaType    string
	mediaURLs    []string
	imageCount   int
	imageSize    string
	firstTokenMs *int
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

func isSoraPromptEnhanceModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "prompt-enhance")
}
