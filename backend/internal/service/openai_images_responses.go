package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type openAIResponsesImageResult struct {
	Result        string
	RevisedPrompt string
	OutputFormat  string
	Size          string
	Background    string
	Quality       string
	Model         string
}

func openAIResponsesImageResultKey(itemID string, result openAIResponsesImageResult) string {
	if strings.TrimSpace(result.Result) != "" {
		return strings.TrimSpace(result.OutputFormat) + "|" + strings.TrimSpace(result.Result)
	}
	return "item:" + strings.TrimSpace(itemID)
}

func openAIResponsesImageResultSizes(results []openAIResponsesImageResult) []string {
	if len(results) == 0 {
		return nil
	}
	sizes := make([]string, 0, len(results))
	for _, result := range results {
		if size := strings.TrimSpace(result.Size); size != "" {
			sizes = append(sizes, size)
		}
	}
	if len(sizes) == 0 {
		return nil
	}
	return sizes
}

func appendOpenAIResponsesImageResultDedup(results *[]openAIResponsesImageResult, seen map[string]struct{}, itemID string, result openAIResponsesImageResult) bool {
	if results == nil {
		return false
	}
	key := openAIResponsesImageResultKey(itemID, result)
	if key != "" {
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	*results = append(*results, result)
	return true
}

func mergeOpenAIResponsesImageMeta(dst *openAIResponsesImageResult, src openAIResponsesImageResult) {
	if dst == nil {
		return
	}
	if trimmed := strings.TrimSpace(src.OutputFormat); trimmed != "" {
		dst.OutputFormat = trimmed
	}
	if trimmed := strings.TrimSpace(src.Size); trimmed != "" {
		dst.Size = trimmed
	}
	if trimmed := strings.TrimSpace(src.Background); trimmed != "" {
		dst.Background = trimmed
	}
	if trimmed := strings.TrimSpace(src.Quality); trimmed != "" {
		dst.Quality = trimmed
	}
	if trimmed := strings.TrimSpace(src.Model); trimmed != "" {
		dst.Model = trimmed
	}
}

func extractOpenAIResponsesImageMetaFromLifecycleEvent(payload []byte) (openAIResponsesImageResult, int64, bool) {
	switch gjson.GetBytes(payload, "type").String() {
	case "response.created", "response.in_progress", "response.completed":
	default:
		return openAIResponsesImageResult{}, 0, false
	}

	response := gjson.GetBytes(payload, "response")
	if !response.Exists() {
		return openAIResponsesImageResult{}, 0, false
	}

	meta := openAIResponsesImageResult{
		OutputFormat: strings.TrimSpace(response.Get("tools.0.output_format").String()),
		Size:         strings.TrimSpace(response.Get("tools.0.size").String()),
		Background:   strings.TrimSpace(response.Get("tools.0.background").String()),
		Quality:      strings.TrimSpace(response.Get("tools.0.quality").String()),
		Model:        strings.TrimSpace(response.Get("tools.0.model").String()),
	}
	return meta, response.Get("created_at").Int(), true
}

func buildOpenAIImagesStreamPartialPayload(
	eventType string,
	b64 string,
	partialImageIndex int64,
	responseFormat string,
	createdAt int64,
	meta openAIResponsesImageResult,
) []byte {
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}

	payload := []byte(`{"type":"","created_at":0,"partial_image_index":0,"b64_json":""}`)
	payload, _ = sjson.SetBytes(payload, "type", eventType)
	payload, _ = sjson.SetBytes(payload, "created_at", createdAt)
	payload, _ = sjson.SetBytes(payload, "partial_image_index", partialImageIndex)
	payload, _ = sjson.SetBytes(payload, "b64_json", b64)
	if strings.EqualFold(strings.TrimSpace(responseFormat), "url") {
		payload, _ = sjson.SetBytes(payload, "url", "data:"+openAIImageOutputMIMEType(meta.OutputFormat)+";base64,"+b64)
	}
	if meta.Background != "" {
		payload, _ = sjson.SetBytes(payload, "background", meta.Background)
	}
	if meta.OutputFormat != "" {
		payload, _ = sjson.SetBytes(payload, "output_format", meta.OutputFormat)
	}
	if meta.Quality != "" {
		payload, _ = sjson.SetBytes(payload, "quality", meta.Quality)
	}
	if meta.Size != "" {
		payload, _ = sjson.SetBytes(payload, "size", meta.Size)
	}
	if meta.Model != "" {
		payload, _ = sjson.SetBytes(payload, "model", meta.Model)
	}
	return payload
}

func buildOpenAIImagesStreamCompletedPayload(
	eventType string,
	img openAIResponsesImageResult,
	responseFormat string,
	createdAt int64,
	usageRaw []byte,
) []byte {
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}

	payload := []byte(`{"type":"","created_at":0,"b64_json":""}`)
	payload, _ = sjson.SetBytes(payload, "type", eventType)
	payload, _ = sjson.SetBytes(payload, "created_at", createdAt)
	payload, _ = sjson.SetBytes(payload, "b64_json", img.Result)
	if strings.EqualFold(strings.TrimSpace(responseFormat), "url") {
		payload, _ = sjson.SetBytes(payload, "url", "data:"+openAIImageOutputMIMEType(img.OutputFormat)+";base64,"+img.Result)
	}
	if img.Background != "" {
		payload, _ = sjson.SetBytes(payload, "background", img.Background)
	}
	if img.OutputFormat != "" {
		payload, _ = sjson.SetBytes(payload, "output_format", img.OutputFormat)
	}
	if img.Quality != "" {
		payload, _ = sjson.SetBytes(payload, "quality", img.Quality)
	}
	if img.Size != "" {
		payload, _ = sjson.SetBytes(payload, "size", img.Size)
	}
	if img.Model != "" {
		payload, _ = sjson.SetBytes(payload, "model", img.Model)
	}
	if len(usageRaw) > 0 && gjson.ValidBytes(usageRaw) {
		payload, _ = sjson.SetRawBytes(payload, "usage", usageRaw)
	}
	return payload
}

func openAIImageOutputMIMEType(outputFormat string) string {
	if outputFormat == "" {
		return "image/png"
	}
	if strings.Contains(outputFormat, "/") {
		return outputFormat
	}
	switch strings.ToLower(strings.TrimSpace(outputFormat)) {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

func openAIImageUploadToDataURL(upload OpenAIImagesUpload) (string, error) {
	if len(upload.Data) == 0 {
		return "", fmt.Errorf("upload %q is empty", strings.TrimSpace(upload.FileName))
	}
	contentType := strings.TrimSpace(upload.ContentType)
	if contentType == "" {
		contentType = http.DetectContentType(upload.Data)
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(upload.Data), nil
}

func buildOpenAIImagesResponsesRequest(parsed *OpenAIImagesRequest, toolModel string) ([]byte, error) {
	if parsed == nil {
		return nil, fmt.Errorf("parsed images request is required")
	}
	prompt := strings.TrimSpace(parsed.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if parsed.N > 1 {
		return nil, fmt.Errorf("n greater than 1 is not supported for OAuth image accounts")
	}

	inputImages := make([]string, 0, len(parsed.InputImageURLs)+len(parsed.Uploads))
	for _, imageURL := range parsed.InputImageURLs {
		if trimmed := strings.TrimSpace(imageURL); trimmed != "" {
			inputImages = append(inputImages, trimmed)
		}
	}
	for _, upload := range parsed.Uploads {
		dataURL, err := openAIImageUploadToDataURL(upload)
		if err != nil {
			return nil, err
		}
		inputImages = append(inputImages, dataURL)
	}
	if parsed.IsEdits() && len(inputImages) == 0 {
		return nil, fmt.Errorf("image input is required")
	}

	req := []byte(`{"instructions":"","stream":true,"reasoning":{"effort":"medium","summary":"auto"},"parallel_tool_calls":true,"include":["reasoning.encrypted_content"],"model":"","store":false,"tool_choice":{"type":"image_generation"}}`)
	req, _ = sjson.SetBytes(req, "model", openAIImagesResponsesMainModel)

	input := []byte(`[{"type":"message","role":"user","content":[{"type":"input_text","text":""}]}]`)
	input, _ = sjson.SetBytes(input, "0.content.0.text", prompt)
	for index, imageURL := range inputImages {
		part := []byte(`{"type":"input_image","image_url":""}`)
		part, _ = sjson.SetBytes(part, "image_url", imageURL)
		input, _ = sjson.SetRawBytes(input, fmt.Sprintf("0.content.%d", index+1), part)
	}
	req, _ = sjson.SetRawBytes(req, "input", input)

	action := "generate"
	if parsed.IsEdits() {
		action = "edit"
	}
	tool := []byte(`{"type":"image_generation","action":"","model":""}`)
	tool, _ = sjson.SetBytes(tool, "action", action)
	tool, _ = sjson.SetBytes(tool, "model", strings.TrimSpace(toolModel))

	for _, field := range []struct {
		path  string
		value string
	}{
		{path: "size", value: parsed.Size},
		{path: "quality", value: parsed.Quality},
		{path: "background", value: parsed.Background},
		{path: "output_format", value: parsed.OutputFormat},
		{path: "moderation", value: parsed.Moderation},
		{path: "style", value: parsed.Style},
	} {
		if trimmed := strings.TrimSpace(field.value); trimmed != "" {
			tool, _ = sjson.SetBytes(tool, field.path, trimmed)
		}
	}
	if parsed.OutputCompression != nil {
		tool, _ = sjson.SetBytes(tool, "output_compression", *parsed.OutputCompression)
	}
	if parsed.PartialImages != nil {
		tool, _ = sjson.SetBytes(tool, "partial_images", *parsed.PartialImages)
	}

	maskImageURL := strings.TrimSpace(parsed.MaskImageURL)
	if parsed.MaskUpload != nil {
		dataURL, err := openAIImageUploadToDataURL(*parsed.MaskUpload)
		if err != nil {
			return nil, err
		}
		maskImageURL = dataURL
	}
	if maskImageURL != "" {
		tool, _ = sjson.SetBytes(tool, "input_image_mask.image_url", maskImageURL)
	}

	req, _ = sjson.SetRawBytes(req, "tools", []byte(`[]`))
	req, _ = sjson.SetRawBytes(req, "tools.-1", tool)
	return req, nil
}

func extractOpenAIImagesFromResponsesCompleted(payload []byte) ([]openAIResponsesImageResult, int64, []byte, openAIResponsesImageResult, error) {
	if gjson.GetBytes(payload, "type").String() != "response.completed" {
		return nil, 0, nil, openAIResponsesImageResult{}, fmt.Errorf("unexpected event type")
	}

	createdAt := gjson.GetBytes(payload, "response.created_at").Int()
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}

	var (
		results   []openAIResponsesImageResult
		firstMeta openAIResponsesImageResult
	)
	output := gjson.GetBytes(payload, "response.output")
	if output.IsArray() {
		for _, item := range output.Array() {
			if item.Get("type").String() != "image_generation_call" {
				continue
			}
			result := strings.TrimSpace(item.Get("result").String())
			if result == "" {
				continue
			}
			entry := openAIResponsesImageResult{
				Result:        result,
				RevisedPrompt: strings.TrimSpace(item.Get("revised_prompt").String()),
				OutputFormat:  strings.TrimSpace(item.Get("output_format").String()),
				Size:          strings.TrimSpace(item.Get("size").String()),
				Background:    strings.TrimSpace(item.Get("background").String()),
				Quality:       strings.TrimSpace(item.Get("quality").String()),
			}
			if len(results) == 0 {
				firstMeta = entry
			}
			results = append(results, entry)
		}
	}

	var usageRaw []byte
	if usage := gjson.GetBytes(payload, "response.tool_usage.image_gen"); usage.Exists() && usage.IsObject() {
		usageRaw = []byte(usage.Raw)
	}
	return results, createdAt, usageRaw, firstMeta, nil
}

func extractOpenAIImageFromResponsesOutputItemDone(payload []byte) (openAIResponsesImageResult, string, bool, error) {
	if gjson.GetBytes(payload, "type").String() != "response.output_item.done" {
		return openAIResponsesImageResult{}, "", false, fmt.Errorf("unexpected event type")
	}

	item := gjson.GetBytes(payload, "item")
	if !item.Exists() || item.Get("type").String() != "image_generation_call" {
		return openAIResponsesImageResult{}, "", false, nil
	}

	result := strings.TrimSpace(item.Get("result").String())
	if result == "" {
		return openAIResponsesImageResult{}, "", false, nil
	}

	entry := openAIResponsesImageResult{
		Result:        result,
		RevisedPrompt: strings.TrimSpace(item.Get("revised_prompt").String()),
		OutputFormat:  strings.TrimSpace(item.Get("output_format").String()),
		Size:          strings.TrimSpace(item.Get("size").String()),
		Background:    strings.TrimSpace(item.Get("background").String()),
		Quality:       strings.TrimSpace(item.Get("quality").String()),
	}
	return entry, strings.TrimSpace(item.Get("id").String()), true, nil
}

func collectOpenAIImagesFromResponsesBody(body []byte) ([]openAIResponsesImageResult, int64, []byte, openAIResponsesImageResult, bool, error) {
	var (
		fallbackResults []openAIResponsesImageResult
		fallbackSeen    = make(map[string]struct{})
		createdAt       int64
		usageRaw        []byte
		foundFinal      bool
		responseMeta    openAIResponsesImageResult
	)

	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		data, ok := extractOpenAISSEDataLine(string(line))
		if !ok || data == "" || data == "[DONE]" {
			continue
		}
		payload := []byte(data)
		if !gjson.ValidBytes(payload) {
			continue
		}
		if meta, eventCreatedAt, ok := extractOpenAIResponsesImageMetaFromLifecycleEvent(payload); ok {
			mergeOpenAIResponsesImageMeta(&responseMeta, meta)
			if eventCreatedAt > 0 {
				createdAt = eventCreatedAt
			}
		}

		switch gjson.GetBytes(payload, "type").String() {
		case "response.output_item.done":
			result, itemID, ok, err := extractOpenAIImageFromResponsesOutputItemDone(payload)
			if err != nil {
				return nil, 0, nil, openAIResponsesImageResult{}, false, err
			}
			if ok {
				mergeOpenAIResponsesImageMeta(&result, responseMeta)
				appendOpenAIResponsesImageResultDedup(&fallbackResults, fallbackSeen, itemID, result)
			}
		case "response.completed":
			results, completedAt, completedUsageRaw, firstMeta, err := extractOpenAIImagesFromResponsesCompleted(payload)
			if err != nil {
				return nil, 0, nil, openAIResponsesImageResult{}, false, err
			}
			foundFinal = true
			if completedAt > 0 {
				createdAt = completedAt
			}
			if len(completedUsageRaw) > 0 {
				usageRaw = completedUsageRaw
			}
			if len(results) > 0 {
				mergeOpenAIResponsesImageMeta(&firstMeta, responseMeta)
				reconcileOpenAIResponsesImageResultSizes(results, &firstMeta)
				return results, createdAt, usageRaw, firstMeta, true, nil
			}
			if len(fallbackResults) > 0 {
				firstMeta = fallbackResults[0]
				mergeOpenAIResponsesImageMeta(&firstMeta, responseMeta)
				reconcileOpenAIResponsesImageResultSizes(fallbackResults, &firstMeta)
				return fallbackResults, createdAt, usageRaw, firstMeta, true, nil
			}
		}
	}

	if len(fallbackResults) > 0 {
		firstMeta := fallbackResults[0]
		mergeOpenAIResponsesImageMeta(&firstMeta, responseMeta)
		reconcileOpenAIResponsesImageResultSizes(fallbackResults, &firstMeta)
		return fallbackResults, createdAt, usageRaw, firstMeta, foundFinal, nil
	}
	return nil, createdAt, usageRaw, openAIResponsesImageResult{}, foundFinal, nil
}

func buildOpenAIImagesAPIResponse(
	results []openAIResponsesImageResult,
	createdAt int64,
	usageRaw []byte,
	firstMeta openAIResponsesImageResult,
	responseFormat string,
) ([]byte, error) {
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}
	out := []byte(`{"created":0,"data":[]}`)
	out, _ = sjson.SetBytes(out, "created", createdAt)

	format := strings.ToLower(strings.TrimSpace(responseFormat))
	if format == "" {
		format = "b64_json"
	}
	for _, img := range results {
		item := []byte(`{}`)
		if format == "url" {
			item, _ = sjson.SetBytes(item, "url", "data:"+openAIImageOutputMIMEType(img.OutputFormat)+";base64,"+img.Result)
		} else {
			item, _ = sjson.SetBytes(item, "b64_json", img.Result)
		}
		if img.RevisedPrompt != "" {
			item, _ = sjson.SetBytes(item, "revised_prompt", img.RevisedPrompt)
		}
		out, _ = sjson.SetRawBytes(out, "data.-1", item)
	}
	if firstMeta.Background != "" {
		out, _ = sjson.SetBytes(out, "background", firstMeta.Background)
	}
	if firstMeta.OutputFormat != "" {
		out, _ = sjson.SetBytes(out, "output_format", firstMeta.OutputFormat)
	}
	if firstMeta.Quality != "" {
		out, _ = sjson.SetBytes(out, "quality", firstMeta.Quality)
	}
	if firstMeta.Size != "" {
		out, _ = sjson.SetBytes(out, "size", firstMeta.Size)
	}
	if firstMeta.Model != "" {
		out, _ = sjson.SetBytes(out, "model", firstMeta.Model)
	}
	if len(usageRaw) > 0 && gjson.ValidBytes(usageRaw) {
		out, _ = sjson.SetRawBytes(out, "usage", usageRaw)
	}
	return out, nil
}

func openAIImagesStreamPrefix(parsed *OpenAIImagesRequest) string {
	if parsed != nil && parsed.IsEdits() {
		return "image_edit"
	}
	return "image_generation"
}

func buildOpenAIImagesStreamErrorBody(message string) []byte {
	body := []byte(`{"type":"error","error":{"type":"upstream_error","message":""}}`)
	if strings.TrimSpace(message) == "" {
		message = "upstream request failed"
	}
	body, _ = sjson.SetBytes(body, "error.message", message)
	return body
}

func openAIImagesResponsesFailure(body []byte) (int, string, bool) {
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		data, ok := extractOpenAISSEDataLine(string(line))
		if !ok || data == "" || data == "[DONE]" {
			continue
		}
		payload := []byte(data)
		if status, message, failed := openAIImagesResponsesEventFailure(payload); failed {
			return status, message, true
		}
	}
	return 0, "", false
}

func openAIImagesResponsesEventFailure(payload []byte) (int, string, bool) {
	if !gjson.ValidBytes(payload) {
		return 0, "", false
	}
	eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	responseStatus := strings.TrimSpace(gjson.GetBytes(payload, "response.status").String())
	if eventType != "response.failed" &&
		eventType != "error" &&
		(eventType != "response.completed" || responseStatus != "failed") {
		return 0, "", false
	}

	message := strings.TrimSpace(gjson.GetBytes(payload, "response.error.message").String())
	if message == "" {
		message = strings.TrimSpace(gjson.GetBytes(payload, "error.message").String())
	}
	if message == "" {
		message = strings.TrimSpace(gjson.GetBytes(payload, "message").String())
	}
	if message == "" {
		message = "OpenAI image generation failed"
	}

	status := int(gjson.GetBytes(payload, "response.error.status").Int())
	if status <= 0 {
		status = int(gjson.GetBytes(payload, "error.status").Int())
	}
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusBadGateway
	}
	return status, sanitizeUpstreamErrorMessage(message), true
}

func openAIImagesFailoverBody(message string) []byte {
	body := []byte(`{"error":{"message":""}}`)
	if strings.TrimSpace(message) == "" {
		message = "upstream image generation failed"
	}
	body, _ = sjson.SetBytes(body, "error.message", truncateString(message, 1024))
	return body
}

func (s *OpenAIGatewayService) writeOpenAIImagesStreamEvent(c *gin.Context, flusher http.Flusher, eventName string, payload []byte) error {
	if strings.TrimSpace(eventName) != "" {
		if _, err := fmt.Fprintf(c.Writer, "event: %s\n", eventName); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func (s *OpenAIGatewayService) readOpenAIImagesOAuthNonStreamingSSE(ctx context.Context, resp *http.Response, c *gin.Context) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("upstream response body is missing")
	}
	type readEvent struct {
		line []byte
		err  error
	}
	events := make(chan readEvent, 1)
	done := make(chan struct{})
	go func() {
		defer close(events)
		reader := bufio.NewReader(resp.Body)
		send := func(event readEvent) bool {
			select {
			case events <- event:
				return true
			case <-done:
				return false
			}
		}
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 && !send(readEvent{line: line}) {
				return
			}
			if err != nil {
				if err != io.EOF {
					_ = send(readEvent{err: err})
				}
				return
			}
		}
	}()
	defer close(done)

	var idleTimer *time.Timer
	var idleCh <-chan time.Time
	idleTimeout := time.Duration(0)
	if s != nil && s.cfg != nil && s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
		idleTimeout = time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
		idleTimer = time.NewTimer(idleTimeout)
		idleCh = idleTimer.C
		defer idleTimer.Stop()
	}
	resetIdleTimer := func() {
		if idleTimer == nil {
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(idleTimeout)
	}

	maxBytes := resolveUpstreamResponseReadLimit(s.cfg)
	body := make([]byte, 0, 64<<10)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return body, nil
			}
			if event.err != nil {
				return nil, event.err
			}
			if len(event.line) == 0 {
				continue
			}
			resetIdleTimer()
			if int64(len(body))+int64(len(event.line)) > maxBytes {
				_ = resp.Body.Close()
				setOpsUpstreamError(c, http.StatusBadGateway, "upstream response too large", "")
				return nil, fmt.Errorf("%w: limit=%d", ErrUpstreamResponseBodyTooLarge, maxBytes)
			}
			body = append(body, event.line...)
		case <-idleCh:
			_ = resp.Body.Close()
			setOpsUpstreamError(c, http.StatusBadGateway, "upstream response read idle timeout", "")
			return nil, errOpenAIImagesStreamIdleTimeout
		case <-ctx.Done():
			_ = resp.Body.Close()
			setOpsUpstreamError(c, http.StatusBadGateway, "upstream response total timeout", "")
			return nil, ctx.Err()
		}
	}
}

func (s *OpenAIGatewayService) handleOpenAIImagesOAuthNonStreamingResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	responseFormat string,
	fallbackModel string,
) (OpenAIUsage, int, []string, error) {
	body, err := s.readOpenAIImagesOAuthNonStreamingSSE(ctx, resp, c)
	if err != nil {
		return OpenAIUsage{}, 0, nil, err
	}
	if status, message, failed := openAIImagesResponsesFailure(body); failed {
		return OpenAIUsage{}, 0, nil, newOpenAIImagesStreamFailoverError(resp, status, message, false)
	}

	var usage OpenAIUsage
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		data, ok := extractOpenAISSEDataLine(string(line))
		if !ok || data == "" || data == "[DONE]" {
			continue
		}
		dataBytes := []byte(data)
		s.parseSSEUsageBytes(dataBytes, &usage)
	}
	results, createdAt, usageRaw, firstMeta, _, err := collectOpenAIImagesFromResponsesBody(body)
	if err != nil {
		return OpenAIUsage{}, 0, nil, err
	}
	if len(results) == 0 {
		return OpenAIUsage{}, 0, nil, newOpenAIImagesStreamFailoverError(resp, http.StatusBadGateway, "upstream did not return image output", false)
	}
	if strings.TrimSpace(firstMeta.Model) == "" {
		firstMeta.Model = strings.TrimSpace(fallbackModel)
	}

	responseBody, err := buildOpenAIImagesAPIResponse(results, createdAt, usageRaw, firstMeta, responseFormat)
	if err != nil {
		return OpenAIUsage{}, 0, nil, err
	}
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	c.Data(resp.StatusCode, "application/json; charset=utf-8", responseBody)
	return usage, len(results), openAIResponsesImageResultSizes(results), nil
}

func (s *OpenAIGatewayService) handleOpenAIImagesOAuthStreamingResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	startTime time.Time,
	responseFormat string,
	streamPrefix string,
	fallbackModel string,
) (OpenAIUsage, int, []string, *int, error) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return OpenAIUsage{}, 0, nil, nil, fmt.Errorf("streaming is not supported by response writer")
	}

	format := strings.ToLower(strings.TrimSpace(responseFormat))
	if format == "" {
		format = "b64_json"
	}

	usage := OpenAIUsage{}
	imageCount := 0
	var imageOutputSizes []string
	var firstTokenMs *int
	emitted := make(map[string]struct{})
	pendingResults := make([]openAIResponsesImageResult, 0, 1)
	pendingSeen := make(map[string]struct{})
	streamMeta := openAIResponsesImageResult{Model: strings.TrimSpace(fallbackModel)}
	var createdAt int64
	pump := newOpenAIImagesStreamPump(
		s,
		ctx,
		resp,
		c,
		flusher,
		"[OpenAI images OAuth] Client disconnected during streaming, continuing to drain upstream for usage",
		func() { responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter) },
	)
	defer pump.Close()
	emitEvent := func(eventName string, payload []byte) error {
		if pump.ClientDisconnected() {
			return nil
		}
		var event bytes.Buffer
		if strings.TrimSpace(eventName) != "" {
			_, _ = fmt.Fprintf(&event, "event: %s\n", eventName)
		}
		_, _ = fmt.Fprintf(&event, "data: %s\n\n", payload)
		semantic := !strings.EqualFold(strings.TrimSpace(eventName), "error")
		if pump.write(event.Bytes(), semantic) && semantic && firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		return nil
	}
	streamFailure := func(status int, message string, cause error) error {
		if cause == nil {
			cause = fmt.Errorf("upstream image generation failed: %s", message)
		}
		if pump.ClientDisconnected() || (ctx != nil && ctx.Err() != nil) {
			return cause
		}
		if !pump.SemanticOutputWritten() {
			return newOpenAIImagesStreamFailoverError(resp, status, message, pump.SafeToFailoverAfterWrite())
		}
		_ = emitEvent("error", buildOpenAIImagesStreamErrorBody(message))
		return cause
	}

	for {
		line, err := pump.Next()
		if len(line) > 0 {
			trimmedLine := strings.TrimRight(string(line), "\r\n")
			data, ok := extractOpenAISSEDataLine(trimmedLine)
			if ok && data != "" && data != "[DONE]" {
				dataBytes := []byte(data)
				s.parseSSEUsageBytes(dataBytes, &usage)
				if gjson.ValidBytes(dataBytes) {
					if status, message, failed := openAIImagesResponsesEventFailure(dataBytes); failed {
						return usage, imageCount, imageOutputSizes, firstTokenMs, streamFailure(status, message, nil)
					}
					if meta, eventCreatedAt, ok := extractOpenAIResponsesImageMetaFromLifecycleEvent(dataBytes); ok {
						mergeOpenAIResponsesImageMeta(&streamMeta, meta)
						if eventCreatedAt > 0 {
							createdAt = eventCreatedAt
						}
					}
					switch gjson.GetBytes(dataBytes, "type").String() {
					case "response.image_generation_call.partial_image":
						b64 := strings.TrimSpace(gjson.GetBytes(dataBytes, "partial_image_b64").String())
						if b64 != "" {
							eventName := streamPrefix + ".partial_image"
							partialMeta := streamMeta
							mergeOpenAIResponsesImageMeta(&partialMeta, openAIResponsesImageResult{
								OutputFormat: strings.TrimSpace(gjson.GetBytes(dataBytes, "output_format").String()),
								Background:   strings.TrimSpace(gjson.GetBytes(dataBytes, "background").String()),
							})
							payload := buildOpenAIImagesStreamPartialPayload(
								eventName,
								b64,
								gjson.GetBytes(dataBytes, "partial_image_index").Int(),
								format,
								createdAt,
								partialMeta,
							)
							_ = emitEvent(eventName, payload)
						}
					case "response.output_item.done":
						img, itemID, ok, extractErr := extractOpenAIImageFromResponsesOutputItemDone(dataBytes)
						if extractErr != nil {
							return usage, imageCount, imageOutputSizes, firstTokenMs, streamFailure(http.StatusBadGateway, extractErr.Error(), extractErr)
						}
						if !ok {
							break
						}
						mergeOpenAIResponsesImageMeta(&streamMeta, img)
						mergeOpenAIResponsesImageMeta(&img, streamMeta)
						key := openAIResponsesImageResultKey(itemID, img)
						if _, exists := emitted[key]; exists {
							break
						}
						if _, exists := pendingSeen[key]; exists {
							break
						}
						pendingSeen[key] = struct{}{}
						pendingResults = append(pendingResults, img)
					case "response.completed":
						results, _, usageRaw, firstMeta, extractErr := extractOpenAIImagesFromResponsesCompleted(dataBytes)
						if extractErr != nil {
							return usage, imageCount, imageOutputSizes, firstTokenMs, streamFailure(http.StatusBadGateway, extractErr.Error(), extractErr)
						}
						mergeOpenAIResponsesImageMeta(&streamMeta, firstMeta)
						finalResults := make([]openAIResponsesImageResult, 0, len(results)+len(pendingResults))
						finalSeen := make(map[string]struct{})
						for _, img := range results {
							mergeOpenAIResponsesImageMeta(&img, streamMeta)
							appendOpenAIResponsesImageResultDedup(&finalResults, finalSeen, "", img)
						}
						for _, img := range pendingResults {
							mergeOpenAIResponsesImageMeta(&img, streamMeta)
							appendOpenAIResponsesImageResultDedup(&finalResults, finalSeen, "", img)
						}
						reconcileOpenAIResponsesImageResultSizes(finalResults, nil)
						if len(finalResults) == 0 {
							err = fmt.Errorf("upstream did not return image output")
							return usage, imageCount, imageOutputSizes, firstTokenMs, streamFailure(http.StatusBadGateway, err.Error(), err)
						}
						eventName := streamPrefix + ".completed"
						for _, img := range finalResults {
							key := openAIResponsesImageResultKey("", img)
							if _, exists := emitted[key]; exists {
								continue
							}
							payload := buildOpenAIImagesStreamCompletedPayload(eventName, img, format, createdAt, usageRaw)
							_ = emitEvent(eventName, payload)
							emitted[key] = struct{}{}
						}
						imageCount = len(emitted)
						imageOutputSizes = openAIResponsesImageResultSizes(finalResults)
						if pump.ClientDisconnected() {
							if streamErr := s.clientDisconnectIncompleteUsageError(ctx); streamErr != nil {
								return usage, imageCount, imageOutputSizes, firstTokenMs, streamErr
							}
							if !openAIImageStreamHasBillableResult(usage, imageCount) {
								return usage, imageCount, imageOutputSizes, firstTokenMs, errors.New("stream usage incomplete after disconnect: missing image usage")
							}
						}
						return usage, imageCount, imageOutputSizes, firstTokenMs, nil
					}
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return usage, imageCount, imageOutputSizes, firstTokenMs, streamFailure(http.StatusBadGateway, err.Error(), err)
		}
	}

	if imageCount > 0 {
		if pump.ClientDisconnected() {
			if streamErr := s.clientDisconnectIncompleteUsageError(ctx); streamErr != nil {
				return usage, imageCount, imageOutputSizes, firstTokenMs, streamErr
			}
			if !openAIImageStreamHasBillableResult(usage, imageCount) {
				return usage, imageCount, imageOutputSizes, firstTokenMs, errors.New("stream usage incomplete after disconnect: missing image usage")
			}
		}
		return usage, imageCount, imageOutputSizes, firstTokenMs, nil
	}
	if len(pendingResults) > 0 {
		eventName := streamPrefix + ".completed"
		finalResults := append([]openAIResponsesImageResult(nil), pendingResults...)
		for i := range finalResults {
			mergeOpenAIResponsesImageMeta(&finalResults[i], streamMeta)
		}
		reconcileOpenAIResponsesImageResultSizes(finalResults, nil)
		for _, img := range finalResults {
			key := openAIResponsesImageResultKey("", img)
			if _, exists := emitted[key]; exists {
				continue
			}
			payload := buildOpenAIImagesStreamCompletedPayload(eventName, img, format, createdAt, nil)
			_ = emitEvent(eventName, payload)
			emitted[key] = struct{}{}
		}
		imageCount = len(emitted)
		imageOutputSizes = openAIResponsesImageResultSizes(finalResults)
		if pump.ClientDisconnected() {
			if streamErr := s.clientDisconnectIncompleteUsageError(ctx); streamErr != nil {
				return usage, imageCount, imageOutputSizes, firstTokenMs, streamErr
			}
			if !openAIImageStreamHasBillableResult(usage, imageCount) {
				return usage, imageCount, imageOutputSizes, firstTokenMs, errors.New("stream usage incomplete after disconnect: missing image usage")
			}
		}
		return usage, imageCount, imageOutputSizes, firstTokenMs, nil
	}

	streamErr := fmt.Errorf("stream disconnected before image generation completed")
	return usage, imageCount, imageOutputSizes, firstTokenMs, streamFailure(http.StatusBadGateway, streamErr.Error(), streamErr)
}

func (s *OpenAIGatewayService) forwardOpenAIImagesOAuth(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	parsed *OpenAIImagesRequest,
	channelMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	requestModel := strings.TrimSpace(parsed.Model)
	if mapped := strings.TrimSpace(channelMappedModel); mapped != "" {
		requestModel = mapped
	}
	if err := validateOpenAIImagesModel(requestModel); err != nil {
		return nil, newOpenAIImagesRequestError(http.StatusBadRequest, err.Error())
	}
	upstreamModel := account.GetMappedModel(requestModel)
	if err := validateOpenAIImagesModel(upstreamModel); err != nil {
		return nil, newOpenAIImagesRequestError(http.StatusBadRequest, err.Error())
	}
	if err := validateOpenAIImagesOptionsForModel(parsed, upstreamModel); err != nil {
		return nil, newOpenAIImagesRequestError(http.StatusBadRequest, err.Error())
	}
	if parsed.N > 1 {
		return nil, newOpenAIImagesRequestError(http.StatusBadRequest, "n greater than 1 is not supported for OAuth image accounts")
	}
	logger.LegacyPrintf(
		"service.openai_gateway",
		"[OpenAI] Images request routing request_model=%s upstream_model=%s endpoint=%s account_type=%s uploads=%d",
		requestModel,
		upstreamModel,
		parsed.Endpoint,
		account.Type,
		len(parsed.Uploads),
	)
	upstreamCtx, releaseUpstreamCtx := s.detachOpenAIUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	if !parsed.Stream {
		var cancelTotalTimeout context.CancelFunc
		upstreamCtx, cancelTotalTimeout = s.openAIImagesNonstreamTotalContext(upstreamCtx)
		defer cancelTotalTimeout()
	}

	token, _, err := s.GetAccessToken(upstreamCtx, account)
	if err != nil {
		return nil, err
	}

	responsesBody, err := buildOpenAIImagesResponsesRequest(parsed, upstreamModel)
	if err != nil {
		return nil, newOpenAIImagesRequestError(http.StatusBadRequest, err.Error())
	}
	upstreamReq, err := s.buildUpstreamRequest(upstreamCtx, c, account, responsesBody, token, true, parsed.StickySessionSeed(), false)
	if err != nil {
		return nil, err
	}
	expectedAgentIdentityTaskID := strings.TrimSpace(account.GetCredential("task_id"))
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Accept", "text/event-stream")

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: 0,
			UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
			Kind:               "request_error",
			Message:            safeErr,
		})
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, newOpenAIImagesStreamFailoverError(nil, http.StatusBadGateway, safeErr, false)
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		if account.IsOpenAIAgentIdentity() && !agentIdentityTaskRecoveryWasTried(ctx) &&
			isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, respBody) {
			if err := s.recoverAgentIdentityTask(ctx, account, expectedAgentIdentityTaskID); err != nil {
				return nil, fmt.Errorf("recover Agent Identity task: %w", err)
			}
			retryCtx := withAgentIdentitySensitiveValues(markAgentIdentityTaskRecoveryTried(ctx), expectedAgentIdentityTaskID)
			return s.forwardOpenAIImagesOAuth(retryCtx, c, account, parsed, channelMappedModel)
		}
		respBody = s.redactAgentIdentitySensitiveBody(ctx, account, respBody)
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  resp.Header.Get("x-request-id"),
				UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
				Kind:               "failover",
				Message:            upstreamMsg,
			})
			s.handleFailoverSideEffects(upstreamCtx, resp, account)
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				ResponseBody:           respBody,
				RetryableOnSameAccount: shouldRetryOpenAIOnSamePoolAccount(account, resp.StatusCode, upstreamMsg, respBody),
			}
		}
		if resp.StatusCode == http.StatusBadRequest {
			return nil, newOpenAIImagesStreamFailoverError(resp, resp.StatusCode, upstreamMsg, false)
		}
		return s.handleErrorResponse(upstreamCtx, resp, c, account, responsesBody, requestModel)
	}
	defer func() { _ = resp.Body.Close() }()

	var (
		usage            OpenAIUsage
		imageCount       int
		imageOutputSizes []string
		firstTokenMs     *int
	)
	if parsed.Stream {
		usage, imageCount, imageOutputSizes, firstTokenMs, err = s.handleOpenAIImagesOAuthStreamingResponse(ctx, resp, c, startTime, parsed.ResponseFormat, openAIImagesStreamPrefix(parsed), requestModel)
		if err != nil {
			return nil, err
		}
	} else {
		usage, imageCount, imageOutputSizes, err = s.handleOpenAIImagesOAuthNonStreamingResponse(upstreamCtx, resp, c, parsed.ResponseFormat, requestModel)
		if err != nil {
			if ctx != nil && ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var failoverErr *UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				return nil, failoverErr
			}
			return nil, newOpenAIImagesStreamFailoverError(resp, http.StatusBadGateway, err.Error(), false)
		}
	}
	if imageCount <= 0 {
		imageCount = parsed.N
	}
	return &OpenAIForwardResult{
		RequestID:        resp.Header.Get("x-request-id"),
		Usage:            usage,
		Model:            requestModel,
		UpstreamModel:    upstreamModel,
		Stream:           parsed.Stream,
		ResponseHeaders:  resp.Header.Clone(),
		Duration:         time.Since(startTime),
		FirstTokenMs:     firstTokenMs,
		ImageCount:       imageCount,
		ImageSize:        parsed.SizeTier,
		ImageOutputSizes: imageOutputSizes,
	}, nil
}
