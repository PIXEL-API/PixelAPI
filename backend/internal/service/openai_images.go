package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/imroc/req/v3"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	openAIImagesGenerationsEndpoint = "/v1/images/generations"
	openAIImagesEditsEndpoint       = "/v1/images/edits"

	openAIImagesGenerationsURL = "https://api.openai.com/v1/images/generations"
	openAIImagesEditsURL       = "https://api.openai.com/v1/images/edits"

	openAIChatGPTStartURL          = "https://chatgpt.com/"
	openAIChatGPTFilesURL          = "https://chatgpt.com/backend-api/files"
	openAIImageBackendUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	openAIImageMaxDownloadBytes    = 20 << 20 // 20MB per image download
	openAIImageMaxUploadPartSize   = 20 << 20 // 20MB per multipart upload part
	openAIImagesResponsesMainModel = "gpt-5.4-mini"
)

type OpenAIImagesCapability string

const (
	OpenAIImagesCapabilityBasic  OpenAIImagesCapability = "images-basic"
	OpenAIImagesCapabilityNative OpenAIImagesCapability = "images-native"
)

type OpenAIImagesUpload struct {
	FieldName   string
	FileName    string
	ContentType string
	Data        []byte
	Width       int
	Height      int
}

func (u OpenAIImagesUpload) ModerationDataURL() string {
	if len(u.Data) == 0 {
		return ""
	}
	contentType := strings.TrimSpace(u.ContentType)
	if contentType == "" {
		contentType = http.DetectContentType(u.Data)
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return ""
	}
	return fmt.Sprintf("data:%s;base64,%s", contentType, base64.StdEncoding.EncodeToString(u.Data))
}

type OpenAIImagesRequest struct {
	Endpoint           string
	ContentType        string
	Multipart          bool
	Model              string
	ExplicitModel      bool
	Prompt             string
	Stream             bool
	N                  int
	Size               string
	ExplicitSize       bool
	SizeTier           string
	ResponseFormat     string
	Quality            string
	Background         string
	OutputFormat       string
	Moderation         string
	InputFidelity      string
	Style              string
	OutputCompression  *int
	PartialImages      *int
	HasMask            bool
	HasNativeOptions   bool
	RequiredCapability OpenAIImagesCapability
	InputImageURLs     []string
	MaskImageURL       string
	Uploads            []OpenAIImagesUpload
	MaskUpload         *OpenAIImagesUpload
	Body               []byte
	bodyHash           string
}

func (r *OpenAIImagesRequest) IsEdits() bool {
	return r != nil && r.Endpoint == openAIImagesEditsEndpoint
}

func (r *OpenAIImagesRequest) StickySessionSeed() string {
	if r == nil {
		return ""
	}
	parts := []string{
		"openai-images",
		strings.TrimSpace(r.Endpoint),
		strings.TrimSpace(r.Model),
		strings.TrimSpace(r.Size),
		strings.TrimSpace(r.Prompt),
	}
	seed := strings.Join(parts, "|")
	if strings.TrimSpace(r.Prompt) == "" && r.bodyHash != "" {
		seed += "|body=" + r.bodyHash
	}
	return seed
}

func (s *OpenAIGatewayService) ParseOpenAIImagesRequest(c *gin.Context, body []byte) (*OpenAIImagesRequest, error) {
	if c == nil || c.Request == nil {
		return nil, fmt.Errorf("missing request context")
	}
	endpoint := normalizeOpenAIImagesEndpointPath(c.Request.URL.Path)
	if endpoint == "" {
		return nil, fmt.Errorf("unsupported images endpoint")
	}

	contentType := strings.TrimSpace(c.GetHeader("Content-Type"))
	req := &OpenAIImagesRequest{
		Endpoint:    endpoint,
		ContentType: contentType,
		N:           1,
		Body:        body,
	}
	if len(body) > 0 {
		sum := sha256.Sum256(body)
		req.bodyHash = hex.EncodeToString(sum[:8])
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && strings.EqualFold(mediaType, "multipart/form-data") {
		req.Multipart = true
		if parseErr := parseOpenAIImagesMultipartRequest(body, contentType, req); parseErr != nil {
			return nil, parseErr
		}
	} else {
		if len(body) == 0 {
			return nil, fmt.Errorf("request body is empty")
		}
		if !gjson.ValidBytes(body) {
			return nil, fmt.Errorf("failed to parse request body")
		}
		if parseErr := parseOpenAIImagesJSONRequest(body, req); parseErr != nil {
			return nil, parseErr
		}
	}

	applyOpenAIImagesDefaults(req)
	if err := validateOpenAIImagesModel(req.Model); err != nil {
		return nil, err
	}
	if err := validateOpenAIImagesOptions(req); err != nil {
		return nil, err
	}
	req.SizeTier = normalizeOpenAIImageSizeTier(req.Size)
	req.RequiredCapability = classifyOpenAIImagesCapability(req)
	return req, nil
}

func parseOpenAIImagesJSONRequest(body []byte, req *OpenAIImagesRequest) error {
	if modelResult := gjson.GetBytes(body, "model"); modelResult.Exists() {
		req.Model = strings.TrimSpace(modelResult.String())
		req.ExplicitModel = req.Model != ""
	}
	req.Prompt = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())

	if streamResult := gjson.GetBytes(body, "stream"); streamResult.Exists() {
		if streamResult.Type != gjson.True && streamResult.Type != gjson.False {
			return fmt.Errorf("invalid stream field type")
		}
		req.Stream = streamResult.Bool()
	}

	if nResult := gjson.GetBytes(body, "n"); nResult.Exists() {
		if nResult.Type != gjson.Number {
			return fmt.Errorf("invalid n field type")
		}
		n := nResult.Float()
		if n != math.Trunc(n) {
			return fmt.Errorf("n must be an integer")
		}
		req.N = int(n)
	}

	if sizeResult := gjson.GetBytes(body, "size"); sizeResult.Exists() {
		req.Size = strings.TrimSpace(sizeResult.String())
		req.ExplicitSize = req.Size != ""
	}
	req.ResponseFormat = strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "response_format").String()))
	req.Quality = strings.TrimSpace(gjson.GetBytes(body, "quality").String())
	req.Background = strings.TrimSpace(gjson.GetBytes(body, "background").String())
	req.OutputFormat = strings.TrimSpace(gjson.GetBytes(body, "output_format").String())
	req.Moderation = strings.TrimSpace(gjson.GetBytes(body, "moderation").String())
	req.InputFidelity = strings.TrimSpace(gjson.GetBytes(body, "input_fidelity").String())
	req.Style = strings.TrimSpace(gjson.GetBytes(body, "style").String())
	req.HasMask = gjson.GetBytes(body, "mask").Exists()
	if outputCompression := gjson.GetBytes(body, "output_compression"); outputCompression.Exists() {
		if outputCompression.Type != gjson.Number {
			return fmt.Errorf("invalid output_compression field type")
		}
		value := outputCompression.Float()
		if value != math.Trunc(value) {
			return fmt.Errorf("output_compression must be an integer")
		}
		v := int(value)
		req.OutputCompression = &v
	}
	if partialImages := gjson.GetBytes(body, "partial_images"); partialImages.Exists() {
		if partialImages.Type != gjson.Number {
			return fmt.Errorf("invalid partial_images field type")
		}
		value := partialImages.Float()
		if value != math.Trunc(value) {
			return fmt.Errorf("partial_images must be an integer")
		}
		v := int(value)
		req.PartialImages = &v
	}
	if req.IsEdits() {
		images := gjson.GetBytes(body, "images")
		if images.Exists() {
			if !images.IsArray() {
				return fmt.Errorf("invalid images field type")
			}
			for _, item := range images.Array() {
				if imageURL := strings.TrimSpace(item.Get("image_url").String()); imageURL != "" {
					req.InputImageURLs = append(req.InputImageURLs, imageURL)
					continue
				}
				if item.Get("file_id").Exists() {
					return fmt.Errorf("images[].file_id is not supported (use images[].image_url instead)")
				}
			}
		}
		if maskImageURL := strings.TrimSpace(gjson.GetBytes(body, "mask.image_url").String()); maskImageURL != "" {
			req.MaskImageURL = maskImageURL
			req.HasMask = true
		}
		if gjson.GetBytes(body, "mask.file_id").Exists() {
			return fmt.Errorf("mask.file_id is not supported (use mask.image_url instead)")
		}
		if len(req.InputImageURLs) == 0 {
			return fmt.Errorf("images[].image_url is required")
		}
	}
	req.HasNativeOptions = hasOpenAINativeImageOptions(func(path string) bool {
		return gjson.GetBytes(body, path).Exists()
	})
	return nil
}

func parseOpenAIImagesMultipartRequest(body []byte, contentType string, req *OpenAIImagesRequest) error {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("invalid multipart content-type: %w", err)
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return fmt.Errorf("multipart boundary is required")
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read multipart body: %w", err)
		}
		name := strings.TrimSpace(part.FormName())
		if name == "" {
			_ = part.Close()
			continue
		}

		data, err := io.ReadAll(io.LimitReader(part, openAIImageMaxUploadPartSize+1))
		_ = part.Close()
		if err != nil {
			return fmt.Errorf("read multipart field %s: %w", name, err)
		}
		if len(data) > openAIImageMaxUploadPartSize {
			return fmt.Errorf("multipart field %s exceeds the 20MB per-part limit", name)
		}

		fileName := strings.TrimSpace(part.FileName())
		if fileName != "" {
			partContentType := strings.TrimSpace(part.Header.Get("Content-Type"))
			if name == "mask" && len(data) > 0 {
				req.HasMask = true
				width, height := parseOpenAIImageDimensions(part.Header)
				maskUpload := OpenAIImagesUpload{
					FieldName:   name,
					FileName:    fileName,
					ContentType: partContentType,
					Data:        data,
					Width:       width,
					Height:      height,
				}
				req.MaskUpload = &maskUpload
			}
			if name == "image" || strings.HasPrefix(name, "image[") {
				width, height := parseOpenAIImageDimensions(part.Header)
				req.Uploads = append(req.Uploads, OpenAIImagesUpload{
					FieldName:   name,
					FileName:    fileName,
					ContentType: partContentType,
					Data:        data,
					Width:       width,
					Height:      height,
				})
			}
			continue
		}

		value := strings.TrimSpace(string(data))
		switch name {
		case "model":
			req.Model = value
			req.ExplicitModel = value != ""
		case "prompt":
			req.Prompt = value
		case "size":
			req.Size = value
			req.ExplicitSize = value != ""
		case "response_format":
			req.ResponseFormat = strings.ToLower(value)
		case "stream":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("invalid stream field value")
			}
			req.Stream = parsed
		case "n":
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return fmt.Errorf("n must be a positive integer")
			}
			req.N = n
		case "quality":
			req.Quality = value
			req.HasNativeOptions = true
		case "background":
			req.Background = value
			req.HasNativeOptions = true
		case "output_format":
			req.OutputFormat = value
			req.HasNativeOptions = true
		case "moderation":
			req.Moderation = value
			req.HasNativeOptions = true
		case "input_fidelity":
			req.InputFidelity = value
			req.HasNativeOptions = true
		case "style":
			req.Style = value
			req.HasNativeOptions = true
		case "output_compression":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid output_compression field value")
			}
			req.OutputCompression = &n
			req.HasNativeOptions = true
		case "partial_images":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid partial_images field value")
			}
			req.PartialImages = &n
			req.HasNativeOptions = true
		default:
			if isOpenAINativeImageOption(name) && value != "" {
				req.HasNativeOptions = true
			}
		}
	}

	if len(req.Uploads) == 0 && req.IsEdits() {
		return fmt.Errorf("image file is required")
	}
	return nil
}

func parseOpenAIImageDimensions(_ textproto.MIMEHeader) (int, int) {
	return 0, 0
}

func applyOpenAIImagesDefaults(req *OpenAIImagesRequest) {
	if req == nil {
		return
	}
	req.Model = strings.TrimSpace(req.Model)
}

func isOpenAIImageGenerationModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gpt-image-") || isGrokImageGenerationModel(model)
}

func isGrokImageGenerationModel(model string) bool {
	model = strings.ToLower(xai.StripGrokProviderPrefix(model))
	return model == "grok-imagine" ||
		model == "grok-imagine-edit" ||
		strings.HasPrefix(model, "grok-imagine-image")
}

func validateOpenAIImagesModel(model string) error {
	model = strings.TrimSpace(model)
	if isOpenAIImageGenerationModel(model) {
		return nil
	}
	if model == "" {
		return fmt.Errorf("images endpoint requires an image model")
	}
	return fmt.Errorf("images endpoint requires an image model, got %q", model)
}

func validateOpenAIImagesOptions(req *OpenAIImagesRequest) error {
	if req == nil {
		return fmt.Errorf("images request is required")
	}
	if req.N < 1 || req.N > 10 {
		return fmt.Errorf("n must be between 1 and 10")
	}
	if req.PartialImages != nil && (*req.PartialImages < 0 || *req.PartialImages > 3) {
		return fmt.Errorf("partial_images must be between 0 and 3")
	}
	if req.OutputCompression != nil && (*req.OutputCompression < 0 || *req.OutputCompression > 100) {
		return fmt.Errorf("output_compression must be between 0 and 100")
	}
	return validateOpenAIImagesOptionsForModel(req, req.Model)
}

func validateOpenAIImagesOptionsForModel(req *OpenAIImagesRequest, model string) error {
	if req == nil {
		return fmt.Errorf("images request is required")
	}
	if strings.EqualFold(strings.TrimSpace(model), "gpt-image-2") &&
		strings.EqualFold(strings.TrimSpace(req.Background), "transparent") {
		return fmt.Errorf("background transparent is not supported by gpt-image-2")
	}
	return nil
}

func normalizeOpenAIImagesEndpointPath(path string) string {
	trimmed := strings.TrimSpace(path)
	switch {
	case strings.Contains(trimmed, "/images/generations"):
		return openAIImagesGenerationsEndpoint
	case strings.Contains(trimmed, "/images/edits"):
		return openAIImagesEditsEndpoint
	default:
		return ""
	}
}

func classifyOpenAIImagesCapability(req *OpenAIImagesRequest) OpenAIImagesCapability {
	if req == nil {
		return OpenAIImagesCapabilityNative
	}
	if req.ExplicitModel || req.ExplicitSize {
		return OpenAIImagesCapabilityNative
	}
	model := strings.ToLower(strings.TrimSpace(req.Model))
	if !strings.HasPrefix(model, "gpt-image-") {
		return OpenAIImagesCapabilityNative
	}
	if req.Stream || req.N != 1 || req.HasMask || req.HasNativeOptions {
		return OpenAIImagesCapabilityNative
	}
	if req.IsEdits() && !req.Multipart {
		return OpenAIImagesCapabilityNative
	}
	if req.ResponseFormat != "" && req.ResponseFormat != "b64_json" {
		return OpenAIImagesCapabilityNative
	}
	return OpenAIImagesCapabilityBasic
}

func hasOpenAINativeImageOptions(exists func(path string) bool) bool {
	for _, path := range []string{
		"background",
		"quality",
		"style",
		"output_format",
		"output_compression",
		"moderation",
		"input_fidelity",
		"partial_images",
	} {
		if exists(path) {
			return true
		}
	}
	return false
}

func isOpenAINativeImageOption(name string) bool {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "background", "quality", "style", "output_format", "output_compression", "moderation", "input_fidelity", "partial_images":
		return true
	default:
		return false
	}
}

func normalizeOpenAIImageSizeTier(size string) string {
	return NormalizeImageBillingTierOrDefault(size)
}

func (s *OpenAIGatewayService) ForwardImages(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	parsed *OpenAIImagesRequest,
	channelMappedModel string,
) (*OpenAIForwardResult, error) {
	if parsed == nil {
		return nil, fmt.Errorf("parsed images request is required")
	}
	switch account.Type {
	case AccountTypeAPIKey:
		return s.forwardOpenAIImagesAPIKey(ctx, c, account, body, parsed, channelMappedModel)
	case AccountTypeOAuth:
		return s.forwardOpenAIImagesOAuth(ctx, c, account, parsed, channelMappedModel)
	default:
		return nil, fmt.Errorf("unsupported account type: %s", account.Type)
	}
}

func (s *OpenAIGatewayService) forwardOpenAIImagesAPIKey(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
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
	forwardResult := &OpenAIForwardResult{
		Model:         requestModel,
		UpstreamModel: upstreamModel,
		Stream:        parsed.Stream,
		ImageSize:     parsed.SizeTier,
	}
	ctx = withOpenAIForwardResultBillingState(ctx, c, forwardResult, startTime, openAIResponseImageBillingConfig{
		Intent: true,
		Model:  requestModel,
		Size:   parsed.SizeTier,
	})
	logger.LegacyPrintf(
		"service.openai_gateway",
		"[OpenAI] Images request routing request_model=%s upstream_model=%s endpoint=%s account_type=%s",
		strings.TrimSpace(parsed.Model),
		upstreamModel,
		parsed.Endpoint,
		account.Type,
	)
	forwardBody, forwardContentType, err := rewriteOpenAIImagesModel(body, parsed.ContentType, upstreamModel)
	if err != nil {
		return nil, err
	}
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
	upstreamReq, err := s.buildOpenAIImagesRequest(upstreamCtx, c, account, forwardBody, forwardContentType, token, parsed.Endpoint)
	if err != nil {
		return nil, err
	}

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
	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		if !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(resp, c, account, false, writeOpenAICompactAwareJSONError)
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
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
			s.handleFailoverSideEffectsForModel(upstreamCtx, resp, account, requestModel)
			return nil, newOpenAIUpstreamFailoverError(
				resp.StatusCode,
				resp.Header,
				respBody,
				upstreamMsg,
				shouldRetryOpenAIOnSamePoolAccount(account, resp.StatusCode, upstreamMsg, respBody),
			)
		}
		if resp.StatusCode == http.StatusBadRequest {
			return nil, newOpenAIImagesStreamFailoverError(resp, resp.StatusCode, upstreamMsg, false)
		}
		return s.handleErrorResponse(upstreamCtx, resp, c, account, forwardBody, requestModel)
	}
	defer func() { _ = resp.Body.Close() }()

	var usage OpenAIUsage
	imageCount := parsed.N
	var firstTokenMs *int
	if parsed.Stream {
		streamUsage, streamCount, ttft, err := s.handleOpenAIImagesStreamingResponse(ctx, resp, c, startTime)
		usage = streamUsage
		imageCount = streamCount
		firstTokenMs = ttft
		if err != nil {
			result := updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
				requestID:       resp.Header.Get("x-request-id"),
				usage:           &usage,
				firstTokenMs:    firstTokenMs,
				responseHeaders: resp.Header,
				imageCount:      imageCount,
				imageSize:       parsed.SizeTier,
			})
			if OpenAIForwardResultHasBillableUsage(result) {
				return result, err
			}
			return nil, err
		}
	} else {
		nonStreamUsage, nonStreamCount, err := s.handleOpenAIImagesNonStreamingResponse(upstreamCtx, resp, c)
		usage = nonStreamUsage
		if nonStreamCount > 0 {
			imageCount = nonStreamCount
		}
		if err != nil {
			result := updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
				requestID:       resp.Header.Get("x-request-id"),
				usage:           &usage,
				responseHeaders: resp.Header,
				imageCount:      imageCount,
				imageSize:       parsed.SizeTier,
			})
			if OpenAIForwardResultHasBillableUsage(result) {
				return result, err
			}
			if ctx != nil && ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, newOpenAIImagesStreamFailoverError(resp, http.StatusBadGateway, err.Error(), false)
		}
	}
	return updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
		requestID:       resp.Header.Get("x-request-id"),
		usage:           &usage,
		firstTokenMs:    firstTokenMs,
		responseHeaders: resp.Header,
		imageCount:      imageCount,
		imageSize:       parsed.SizeTier,
	}), nil
}

func (s *OpenAIGatewayService) openAIImagesNonstreamTotalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.cfg == nil || s.cfg.Gateway.ImageNonstreamTotalTimeoutSeconds <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(s.cfg.Gateway.ImageNonstreamTotalTimeoutSeconds)*time.Second)
}

func (s *OpenAIGatewayService) buildOpenAIImagesRequest(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	contentType string,
	token string,
	endpoint string,
) (*http.Request, error) {
	targetURL := openAIImagesGenerationsURL
	if endpoint == openAIImagesEditsEndpoint {
		targetURL = openAIImagesEditsURL
	}
	baseURL := account.GetOpenAIBaseURL()
	if baseURL != "" {
		validatedURL, err := s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return nil, err
		}
		targetURL = buildOpenAIImagesURL(validatedURL, endpoint)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for key, values := range c.Request.Header {
		if !openaiPassthroughAllowedHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	customUA := account.GetOpenAIUserAgent()
	if customUA != "" {
		req.Header.Set("User-Agent", customUA)
	}
	if strings.TrimSpace(contentType) != "" {
		req.Header.Set("Content-Type", contentType)
	}
	account.ApplyHeaderOverrides(req.Header)
	return req, nil
}

func buildOpenAIImagesURL(base string, endpoint string) string {
	normalized := strings.TrimRight(strings.TrimSpace(base), "/")
	relative := strings.TrimPrefix(strings.TrimSpace(endpoint), "/v1")
	if strings.HasSuffix(normalized, endpoint) || strings.HasSuffix(normalized, relative) {
		return normalized
	}
	if strings.HasSuffix(normalized, "/v1") {
		return normalized + relative
	}
	return normalized + endpoint
}

func rewriteOpenAIImagesModel(body []byte, contentType string, model string) ([]byte, string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return body, contentType, nil
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && strings.EqualFold(mediaType, "multipart/form-data") {
		rewrittenBody, rewrittenType, rewriteErr := rewriteOpenAIImagesMultipartModel(body, contentType, model)
		return rewrittenBody, rewrittenType, rewriteErr
	}
	rewritten, err := sjson.SetBytes(body, "model", model)
	if err != nil {
		return nil, "", fmt.Errorf("rewrite image request model: %w", err)
	}
	return rewritten, contentType, nil
}

func rewriteOpenAIImagesMultipartModel(body []byte, contentType string, model string) ([]byte, string, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, "", fmt.Errorf("parse multipart content-type: %w", err)
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return nil, "", fmt.Errorf("multipart boundary is required")
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	modelWritten := false

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("read multipart body: %w", err)
		}

		formName := strings.TrimSpace(part.FormName())
		partHeader := cloneMultipartHeader(part.Header)
		target, err := writer.CreatePart(partHeader)
		if err != nil {
			_ = part.Close()
			return nil, "", fmt.Errorf("create multipart part: %w", err)
		}

		if formName == "model" && part.FileName() == "" {
			if _, err := target.Write([]byte(model)); err != nil {
				_ = part.Close()
				return nil, "", fmt.Errorf("rewrite multipart model: %w", err)
			}
			modelWritten = true
			_ = part.Close()
			continue
		}
		if _, err := io.Copy(target, part); err != nil {
			_ = part.Close()
			return nil, "", fmt.Errorf("copy multipart part: %w", err)
		}
		_ = part.Close()
	}

	if !modelWritten {
		if err := writer.WriteField("model", model); err != nil {
			return nil, "", fmt.Errorf("append multipart model field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("finalize multipart body: %w", err)
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func cloneMultipartHeader(src textproto.MIMEHeader) textproto.MIMEHeader {
	dst := make(textproto.MIMEHeader, len(src))
	for key, values := range src {
		copied := make([]string, len(values))
		copy(copied, values)
		dst[key] = copied
	}
	return dst
}

func (s *OpenAIGatewayService) handleOpenAIImagesNonStreamingResponse(ctx context.Context, resp *http.Response, c *gin.Context) (OpenAIUsage, int, error) {
	body, err := ReadUpstreamResponseBodyWithContext(ctx, resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return OpenAIUsage{}, 0, err
	}
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	contentType := "application/json"
	if s.cfg != nil && !s.cfg.Security.ResponseHeaders.Enabled {
		if upstreamType := resp.Header.Get("Content-Type"); upstreamType != "" {
			contentType = upstreamType
		}
	}
	usage, _ := extractOpenAIUsageFromJSONBytes(body)
	imageCount := extractOpenAIImageCountFromJSONBytes(body)
	c.Data(resp.StatusCode, contentType, body)
	return usage, imageCount, nil
}

var errOpenAIImagesStreamIdleTimeout = errors.New("image stream data interval timeout")

type openAIImagesStreamReadEvent struct {
	line []byte
	err  error
}

type openAIImagesStreamPump struct {
	service              *OpenAIGatewayService
	ctx                  context.Context
	resp                 *http.Response
	c                    *gin.Context
	flusher              http.Flusher
	events               <-chan openAIImagesStreamReadEvent
	stopReader           chan struct{}
	idleTimer            *time.Timer
	idleCh               <-chan time.Time
	idleTimeout          time.Duration
	keepaliveTicker      *time.Ticker
	keepaliveCh          <-chan time.Time
	keepaliveInterval    time.Duration
	clientDone           <-chan struct{}
	clientDisconnected   bool
	semanticOutput       bool
	nonSemanticOutput    bool
	lastDownstreamWrite  time.Time
	cancelDrainDeadline  context.CancelFunc
	disconnectLogMessage string
	beforeSemanticWrite  func()
	semanticHeadersSet   bool
}

func newOpenAIImagesStreamPump(
	s *OpenAIGatewayService,
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	flusher http.Flusher,
	disconnectLogMessage string,
	beforeSemanticWrite func(),
) *openAIImagesStreamPump {
	if ctx == nil {
		ctx = context.Background()
	}
	stopReader := make(chan struct{})
	events := make(chan openAIImagesStreamReadEvent, 1)
	go func() {
		defer close(events)
		reader := bufio.NewReader(resp.Body)
		send := func(event openAIImagesStreamReadEvent) bool {
			select {
			case events <- event:
				return true
			case <-stopReader:
				return false
			}
		}
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 && !send(openAIImagesStreamReadEvent{line: line}) {
				return
			}
			if err != nil {
				if err != io.EOF {
					_ = send(openAIImagesStreamReadEvent{err: err})
				}
				return
			}
		}
	}()

	clientDone := ctx.Done()
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		clientDone = c.Request.Context().Done()
	}
	pump := &openAIImagesStreamPump{
		service:              s,
		ctx:                  ctx,
		resp:                 resp,
		c:                    c,
		flusher:              flusher,
		events:               events,
		stopReader:           stopReader,
		clientDone:           clientDone,
		lastDownstreamWrite:  time.Now(),
		disconnectLogMessage: disconnectLogMessage,
		beforeSemanticWrite:  beforeSemanticWrite,
	}
	if s != nil && s.cfg != nil && s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
		pump.idleTimeout = time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
		pump.idleTimer = time.NewTimer(pump.idleTimeout)
		pump.idleCh = pump.idleTimer.C
	}
	if s != nil && s.cfg != nil && s.cfg.Gateway.StreamKeepaliveInterval > 0 {
		pump.keepaliveInterval = time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second
		pump.keepaliveTicker = time.NewTicker(pump.keepaliveInterval)
		pump.keepaliveCh = pump.keepaliveTicker.C
	}
	return pump
}

func (p *openAIImagesStreamPump) Close() {
	if p == nil {
		return
	}
	if p.idleTimer != nil {
		p.idleTimer.Stop()
	}
	if p.keepaliveTicker != nil {
		p.keepaliveTicker.Stop()
	}
	if p.cancelDrainDeadline != nil {
		p.cancelDrainDeadline()
	}
	select {
	case <-p.stopReader:
	default:
		close(p.stopReader)
	}
	if p.resp != nil && p.resp.Body != nil {
		_ = p.resp.Body.Close()
	}
}

func (p *openAIImagesStreamPump) resetIdleTimer() {
	if p == nil || p.idleTimer == nil {
		return
	}
	if !p.idleTimer.Stop() {
		select {
		case <-p.idleTimer.C:
		default:
		}
	}
	p.idleTimer.Reset(p.idleTimeout)
}

func (p *openAIImagesStreamPump) startClientDisconnectDrain() {
	if p == nil || p.clientDisconnected {
		return
	}
	p.clientDisconnected = true
	p.clientDone = nil
	if p.cancelDrainDeadline == nil {
		p.cancelDrainDeadline = p.service.startDisconnectedStreamDrainDeadline(p.ctx, p.resp.Body, p.resp.Header.Get("x-request-id"))
	}
	if strings.Contains(p.disconnectLogMessage, "OAuth") {
		p.service.legacyLogClientDisconnectDrainDecision(p.ctx, "[OpenAI images OAuth] Client disconnected during streaming, continuing to drain upstream for usage")
	} else {
		p.service.legacyLogClientDisconnectDrainDecision(p.ctx, "[OpenAI images] Client disconnected during streaming, continuing to drain upstream for usage")
	}
}

func (p *openAIImagesStreamPump) markDownstreamWrite(semantic bool) {
	if p == nil {
		return
	}
	p.lastDownstreamWrite = time.Now()
	if semantic {
		p.semanticOutput = true
	} else {
		p.nonSemanticOutput = true
	}
}

func (p *openAIImagesStreamPump) write(data []byte, semantic bool) bool {
	if p == nil || p.clientDisconnected {
		return false
	}
	if p.clientDone != nil {
		select {
		case <-p.clientDone:
			p.startClientDisconnectDrain()
			return false
		default:
		}
	}
	if semantic && !p.semanticHeadersSet {
		if p.beforeSemanticWrite != nil {
			p.beforeSemanticWrite()
		}
		p.semanticHeadersSet = true
	}
	if _, err := p.c.Writer.Write(data); err != nil {
		p.startClientDisconnectDrain()
		return false
	}
	p.flusher.Flush()
	p.markDownstreamWrite(semantic)
	return true
}

func (p *openAIImagesStreamPump) Next() ([]byte, error) {
	for {
		select {
		case event, ok := <-p.events:
			if !ok {
				return nil, io.EOF
			}
			if len(event.line) > 0 {
				p.resetIdleTimer()
				return event.line, nil
			}
			if event.err != nil {
				return nil, event.err
			}
		case <-p.idleCh:
			_ = p.resp.Body.Close()
			return nil, errOpenAIImagesStreamIdleTimeout
		case <-p.clientDone:
			p.startClientDisconnectDrain()
		case <-p.keepaliveCh:
			if p.clientDisconnected || time.Since(p.lastDownstreamWrite) < p.keepaliveInterval {
				continue
			}
			p.write([]byte(":\n\n"), false)
		}
	}
}

func (p *openAIImagesStreamPump) ClientDisconnected() bool {
	return p != nil && p.clientDisconnected
}

func (p *openAIImagesStreamPump) SemanticOutputWritten() bool {
	return p != nil && p.semanticOutput
}

func (p *openAIImagesStreamPump) SafeToFailoverAfterWrite() bool {
	return p != nil && p.nonSemanticOutput && !p.semanticOutput
}

func parseOpenAIImagesSSEEvent(event []byte) (string, []byte, bool) {
	var eventName string
	dataLines := make([]string, 0, 1)
	for _, rawLine := range bytes.Split(event, []byte("\n")) {
		line := strings.TrimRight(string(rawLine), "\r")
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if data, ok := extractOpenAISSEDataLine(line); ok {
			dataLines = append(dataLines, data)
		}
	}
	if len(dataLines) == 0 {
		return eventName, nil, false
	}
	return eventName, []byte(strings.Join(dataLines, "\n")), true
}

func openAIImagesDirectStreamFailure(eventName string, payload []byte) (int, string, bool) {
	eventType := strings.ToLower(strings.TrimSpace(eventName))
	if gjson.ValidBytes(payload) {
		if value := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "type").String())); value != "" {
			eventType = value
		}
	}
	failed := eventType == "error" || strings.HasSuffix(eventType, ".error") || strings.HasSuffix(eventType, ".failed")
	if !failed && gjson.ValidBytes(payload) {
		errorValue := gjson.GetBytes(payload, "error")
		failed = errorValue.Exists() && errorValue.Type != gjson.Null &&
			(errorValue.IsObject() || errorValue.Type == gjson.String)
	}
	if !failed {
		return 0, "", false
	}
	status := 0
	message := ""
	if gjson.ValidBytes(payload) {
		for _, path := range []string{"error.status", "error.status_code", "status", "status_code"} {
			if value := int(gjson.GetBytes(payload, path).Int()); value > 0 {
				status = value
				break
			}
		}
		for _, path := range []string{"error.message", "message"} {
			if value := strings.TrimSpace(gjson.GetBytes(payload, path).String()); value != "" {
				message = value
				break
			}
		}
	}
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusBadGateway
	}
	if message == "" {
		message = "OpenAI image generation failed"
	}
	return status, sanitizeUpstreamErrorMessage(message), true
}

func newOpenAIImagesStreamFailoverError(resp *http.Response, status int, message string, safeAfterWrite bool) *UpstreamFailoverError {
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusBadGateway
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "upstream image request failed"
	}
	failoverErr := &UpstreamFailoverError{
		StatusCode:               status,
		ResponseBody:             openAIImagesFailoverBody(message),
		SafeToFailoverAfterWrite: safeAfterWrite,
	}
	if resp != nil {
		failoverErr.ResponseHeaders = resp.Header.Clone()
	}
	if status == http.StatusBadRequest {
		if isOpenAIImagesAccountCapabilityFailure(message) {
			failoverErr.Scope = GatewayFailureScopeAccount
		} else {
			failoverErr.Scope = GatewayFailureScopeRequest
			failoverErr.NextAccountAction = NextAccountStop
			failoverErr.ClientStatusCode = http.StatusBadRequest
			failoverErr.ClientMessage = strings.TrimSpace(message)
		}
	}
	return failoverErr
}

func isOpenAIImagesAccountCapabilityFailure(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	for _, marker := range []string{
		"unsupported image model",
		"image generation tool is not available",
		"image_generation tool is not available",
		"image tool is not available",
		"not eligible for image generation",
		"does not have access to image generation",
		"does not have access to model",
		"do not have access to model",
		"you do not have access to it",
		"account does not support",
		"account is not supported",
		"model is not supported for this account",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func newOpenAIImagesRequestError(status int, message string) *UpstreamFailoverError {
	if status < http.StatusBadRequest || status >= http.StatusInternalServerError {
		status = http.StatusBadRequest
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "invalid image request"
	}
	return &UpstreamFailoverError{
		StatusCode:        status,
		ResponseBody:      openAIImagesFailoverBody(message),
		Scope:             GatewayFailureScopeRequest,
		NextAccountAction: NextAccountStop,
		ClientStatusCode:  status,
		ClientMessage:     message,
	}
}

func (s *OpenAIGatewayService) handleOpenAIImagesStreamingResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	startTime time.Time,
) (OpenAIUsage, int, *int, error) {
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "text/event-stream"
	}
	c.Header("Content-Type", contentType)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return OpenAIUsage{}, 0, nil, fmt.Errorf("streaming is not supported by response writer")
	}

	usage := OpenAIUsage{}
	imageCount := 0
	var firstTokenMs *int
	sawTerminal := false
	var billingUsageObservation openAIResponsesBillingUsageObservation
	pump := newOpenAIImagesStreamPump(
		s,
		ctx,
		resp,
		c,
		flusher,
		"[OpenAI images] Client disconnected during streaming, continuing to drain upstream for usage",
		func() { responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter) },
	)
	defer pump.Close()
	var eventBuffer []byte

	processEvent := func(event []byte) error {
		eventName, data, hasData := parseOpenAIImagesSSEEvent(event)
		trimmedData := strings.TrimSpace(string(data))
		meaningfulData := hasData && trimmedData != "" && trimmedData != "[DONE]"
		eventType := ""
		if meaningfulData {
			billingUsageObservation.observePayload(data)
			if status, message, failed := openAIImagesDirectStreamFailure(eventName, data); failed {
				if !pump.SemanticOutputWritten() && !pump.ClientDisconnected() {
					return newOpenAIImagesStreamFailoverError(resp, status, message, pump.SafeToFailoverAfterWrite())
				}
				pump.write(event, false)
				return fmt.Errorf("upstream image generation failed: %s", message)
			}
			mergeOpenAIUsage(&usage, data)
			if count := extractOpenAIImageCountFromJSONBytes(data); count > imageCount {
				imageCount = count
			}
			eventType = strings.TrimSpace(gjson.GetBytes(data, "type").String())
		}
		if trimmedData == "[DONE]" ||
			eventType == "image_generation.completed" ||
			eventType == "response.completed" ||
			strings.EqualFold(strings.TrimSpace(eventName), "image_generation.completed") {
			sawTerminal = true
		}
		written := pump.write(event, meaningfulData)
		if written && meaningfulData && firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		return nil
	}

	for {
		line, err := pump.Next()
		if len(line) > 0 {
			eventBuffer = append(eventBuffer, line...)
			if len(bytes.TrimRight(line, "\r\n")) == 0 {
				if processErr := processEvent(eventBuffer); processErr != nil {
					return usage, imageCount, firstTokenMs, processErr
				}
				eventBuffer = eventBuffer[:0]
			}
		}
		if err == io.EOF {
			if len(eventBuffer) > 0 {
				if processErr := processEvent(eventBuffer); processErr != nil {
					return usage, imageCount, firstTokenMs, processErr
				}
			}
			break
		}
		if err != nil {
			if pump.ClientDisconnected() || (ctx != nil && ctx.Err() != nil) {
				return usage, imageCount, firstTokenMs, fmt.Errorf("stream usage incomplete after disconnect: %w", err)
			}
			if !pump.SemanticOutputWritten() {
				return usage, imageCount, firstTokenMs, newOpenAIImagesStreamFailoverError(resp, http.StatusBadGateway, err.Error(), pump.SafeToFailoverAfterWrite())
			}
			_ = pump.write(append([]byte("event: error\ndata: "), append(buildOpenAIImagesStreamErrorBody(err.Error()), []byte("\n\n")...)...), false)
			return usage, imageCount, firstTokenMs, err
		}
	}
	if pump.ClientDisconnected() {
		if streamErr := s.clientDisconnectIncompleteUsageError(ctx); streamErr != nil {
			return usage, imageCount, firstTokenMs, streamErr
		}
		if !openAIImageStreamHasBillableResult(usage, imageCount) {
			return usage, imageCount, firstTokenMs, errors.New("stream usage incomplete after disconnect: missing image usage")
		}
	}
	if !sawTerminal {
		err := errors.New("upstream image stream ended without a terminal event")
		if !pump.SemanticOutputWritten() && !pump.ClientDisconnected() {
			return usage, imageCount, firstTokenMs, newOpenAIImagesStreamFailoverError(
				resp,
				http.StatusBadGateway,
				err.Error(),
				pump.SafeToFailoverAfterWrite(),
			)
		}
		return usage, imageCount, firstTokenMs, err
	}
	return usage, imageCount, firstTokenMs, nil
}

func openAIImageStreamHasBillableResult(usage OpenAIUsage, imageCount int) bool {
	tokens, _ := openAIUsageTokens(usage)
	return imageCount > 0 || usage.ImageCount > 0 || hasBillableOpenAITokens(tokens)
}

func mergeOpenAIUsage(dst *OpenAIUsage, body []byte) {
	if dst == nil {
		return
	}
	if parsed, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		if parsed.InputTokens > 0 {
			dst.InputTokens = parsed.InputTokens
		}
		if parsed.TextInputTokens > 0 {
			dst.TextInputTokens = parsed.TextInputTokens
		}
		if parsed.ImageInputTokens > 0 {
			dst.ImageInputTokens = parsed.ImageInputTokens
		}
		if parsed.OutputTokens > 0 {
			dst.OutputTokens = parsed.OutputTokens
		}
		if parsed.TextOutputTokens > 0 {
			dst.TextOutputTokens = parsed.TextOutputTokens
		}
		if parsed.CacheReadInputTokens > 0 {
			dst.CacheReadInputTokens = parsed.CacheReadInputTokens
		}
		if parsed.TextCacheReadInputTokens > 0 {
			dst.TextCacheReadInputTokens = parsed.TextCacheReadInputTokens
		}
		if parsed.ImageCacheReadInputTokens > 0 {
			dst.ImageCacheReadInputTokens = parsed.ImageCacheReadInputTokens
		}
		if parsed.ImageOutputTokens > 0 {
			dst.ImageOutputTokens = parsed.ImageOutputTokens
		}
	}
}

func extractOpenAIImageCountFromJSONBytes(body []byte) int {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return 0
	}
	data := gjson.GetBytes(body, "data")
	if data.Exists() && data.IsArray() {
		return len(data.Array())
	}
	return 0
}

type openAIImagePointerInfo struct {
	Pointer     string
	DownloadURL string
	B64JSON     string
	MimeType    string
	Prompt      string
}

func collectOpenAIImagePointers(body []byte) []openAIImagePointerInfo {
	if len(body) == 0 {
		return nil
	}
	prompt := ""
	for _, path := range []string{
		"message.metadata.dalle.prompt",
		"metadata.dalle.prompt",
		"revised_prompt",
	} {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			prompt = value
			break
		}
	}
	matches := openAIImagePointerMatches(body)
	out := make([]openAIImagePointerInfo, 0, len(matches))
	for _, pointer := range matches {
		out = append(out, openAIImagePointerInfo{Pointer: pointer, Prompt: prompt})
	}
	return mergeOpenAIImagePointerInfos(out, collectOpenAIImageInlineAssets(body, prompt))
}

func openAIImagePointerMatches(body []byte) []string {
	raw := string(body)
	matches := make([]string, 0, 4)
	for _, prefix := range []string{"file-service://", "sediment://"} {
		start := 0
		for {
			idx := strings.Index(raw[start:], prefix)
			if idx < 0 {
				break
			}
			idx += start
			end := idx + len(prefix)
			for end < len(raw) {
				ch := raw[end]
				if ch != '-' && ch != '_' &&
					(ch < '0' || ch > '9') &&
					(ch < 'a' || ch > 'z') &&
					(ch < 'A' || ch > 'Z') {
					break
				}
				end++
			}
			matches = append(matches, raw[idx:end])
			start = end
		}
	}
	return dedupeStrings(matches)
}

func mergeOpenAIImagePointerInfos(existing []openAIImagePointerInfo, next []openAIImagePointerInfo) []openAIImagePointerInfo {
	if len(next) == 0 {
		return existing
	}
	seen := make(map[string]openAIImagePointerInfo, len(existing)+len(next))
	out := make([]openAIImagePointerInfo, 0, len(existing)+len(next))
	for _, item := range existing {
		if key := item.identityKey(); key != "" {
			seen[key] = item
		}
		out = append(out, item)
	}
	for _, item := range next {
		key := item.identityKey()
		if key == "" {
			continue
		}
		if existingItem, ok := seen[key]; ok {
			merged := mergeOpenAIImagePointerInfo(existingItem, item)
			if merged != existingItem {
				for i := range out {
					if out[i].identityKey() == key {
						out[i] = merged
						break
					}
				}
				seen[key] = merged
			}
			continue
		}
		seen[key] = item
		out = append(out, item)
	}
	return out
}

func (i openAIImagePointerInfo) identityKey() string {
	switch {
	case strings.TrimSpace(i.Pointer) != "":
		return "pointer:" + strings.TrimSpace(i.Pointer)
	case strings.TrimSpace(i.DownloadURL) != "":
		return "download:" + strings.TrimSpace(i.DownloadURL)
	case strings.TrimSpace(i.B64JSON) != "":
		b64 := strings.TrimSpace(i.B64JSON)
		if len(b64) > 64 {
			b64 = b64[:64]
		}
		return "b64:" + b64
	default:
		return ""
	}
}

func mergeOpenAIImagePointerInfo(existing, next openAIImagePointerInfo) openAIImagePointerInfo {
	merged := existing
	if strings.TrimSpace(merged.Pointer) == "" {
		merged.Pointer = next.Pointer
	}
	if strings.TrimSpace(merged.DownloadURL) == "" {
		merged.DownloadURL = next.DownloadURL
	}
	if strings.TrimSpace(merged.B64JSON) == "" {
		merged.B64JSON = next.B64JSON
	}
	if strings.TrimSpace(merged.MimeType) == "" {
		merged.MimeType = next.MimeType
	}
	if strings.TrimSpace(merged.Prompt) == "" {
		merged.Prompt = next.Prompt
	}
	return merged
}

func resolveOpenAIImageBytes(
	ctx context.Context,
	client *req.Client,
	headers http.Header,
	conversationID string,
	pointer openAIImagePointerInfo,
) ([]byte, error) {
	if normalized := normalizeOpenAIImageBase64(pointer.B64JSON); normalized != "" {
		return base64.StdEncoding.DecodeString(normalized)
	}
	if downloadURL := strings.TrimSpace(pointer.DownloadURL); downloadURL != "" {
		return downloadOpenAIImageBytes(ctx, client, headers, downloadURL)
	}
	if strings.TrimSpace(pointer.Pointer) == "" {
		return nil, fmt.Errorf("image asset is missing pointer, url, and base64 data")
	}
	downloadURL, err := fetchOpenAIImageDownloadURL(ctx, client, headers, conversationID, pointer.Pointer)
	if err != nil {
		return nil, err
	}
	return downloadOpenAIImageBytes(ctx, client, headers, downloadURL)
}

func normalizeOpenAIImageBase64(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "data:") {
		if idx := strings.Index(raw, ","); idx >= 0 && idx+1 < len(raw) {
			raw = raw[idx+1:]
		}
	}
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "=") + strings.Repeat("=", (4-len(raw)%4)%4)
	if raw == "" {
		return ""
	}
	if _, err := base64.StdEncoding.DecodeString(raw); err != nil {
		return ""
	}
	return raw
}

func collectOpenAIImageInlineAssets(body []byte, fallbackPrompt string) []openAIImagePointerInfo {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil
	}
	var out []openAIImagePointerInfo
	walkOpenAIImageInlineAssets(decoded, strings.TrimSpace(fallbackPrompt), &out)
	return out
}

func walkOpenAIImageInlineAssets(node any, prompt string, out *[]openAIImagePointerInfo) {
	switch value := node.(type) {
	case map[string]any:
		localPrompt := prompt
		for _, key := range []string{"revised_prompt", "image_gen_title", "prompt"} {
			if v, ok := value[key].(string); ok && strings.TrimSpace(v) != "" {
				localPrompt = strings.TrimSpace(v)
				break
			}
		}
		item := openAIImagePointerInfo{
			Prompt:      localPrompt,
			Pointer:     firstNonEmptyString(value["asset_pointer"], value["pointer"]),
			DownloadURL: firstNonEmptyString(value["download_url"], value["url"], value["image_url"]),
			B64JSON:     firstNonEmptyString(value["b64_json"], value["base64"], value["image_base64"]),
			MimeType:    firstNonEmptyString(value["mime_type"], value["mimeType"], value["content_type"]),
		}
		switch {
		case strings.HasPrefix(strings.TrimSpace(item.Pointer), "file-service://"),
			strings.HasPrefix(strings.TrimSpace(item.Pointer), "sediment://"),
			isLikelyOpenAIImageDownloadURL(item.DownloadURL),
			normalizeOpenAIImageBase64(item.B64JSON) != "":
			*out = append(*out, item)
		}
		for _, child := range value {
			walkOpenAIImageInlineAssets(child, localPrompt, out)
		}
	case []any:
		for _, child := range value {
			walkOpenAIImageInlineAssets(child, prompt, out)
		}
	}
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func isLikelyOpenAIImageDownloadURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(raw), "data:image/") {
		return true
	}
	if !strings.HasPrefix(strings.ToLower(raw), "http://") && !strings.HasPrefix(strings.ToLower(raw), "https://") {
		return false
	}
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "/download") ||
		strings.Contains(lower, ".png") ||
		strings.Contains(lower, ".jpg") ||
		strings.Contains(lower, ".jpeg") ||
		strings.Contains(lower, ".webp")
}

func fetchOpenAIImageDownloadURL(
	ctx context.Context,
	client *req.Client,
	headers http.Header,
	conversationID string,
	pointer string,
) (string, error) {
	url := ""
	allowConversationRetry := false
	switch {
	case strings.HasPrefix(pointer, "file-service://"):
		fileID := strings.TrimPrefix(pointer, "file-service://")
		url = fmt.Sprintf("%s/%s/download", openAIChatGPTFilesURL, fileID)
	case strings.HasPrefix(pointer, "sediment://"):
		attachmentID := strings.TrimPrefix(pointer, "sediment://")
		url = fmt.Sprintf("https://chatgpt.com/backend-api/conversation/%s/attachment/%s/download", conversationID, attachmentID)
		allowConversationRetry = true
	default:
		return "", fmt.Errorf("unsupported image pointer: %s", pointer)
	}

	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		var result struct {
			DownloadURL string `json:"download_url"`
		}
		resp, err := client.R().
			SetContext(ctx).
			SetHeaders(headerToMap(headers)).
			SetSuccessResult(&result).
			Get(url)
		if err != nil {
			lastErr = err
		} else if resp.IsSuccessState() && strings.TrimSpace(result.DownloadURL) != "" {
			return strings.TrimSpace(result.DownloadURL), nil
		} else {
			statusErr := newOpenAIImageStatusError(resp, "fetch image download url failed")
			if !allowConversationRetry || !isOpenAIImageTransientConversationNotFoundError(statusErr) {
				return "", statusErr
			}
			lastErr = statusErr
		}
		if attempt == 7 {
			break
		}
		timer := time.NewTimer(750 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("fetch image download url failed")
	}
	return "", lastErr
}

func downloadOpenAIImageBytes(ctx context.Context, client *req.Client, headers http.Header, downloadURL string) ([]byte, error) {
	request := client.R().
		SetContext(ctx).
		DisableAutoReadResponse()

	if strings.HasPrefix(downloadURL, openAIChatGPTStartURL) {
		downloadHeaders := cloneHTTPHeader(headers)
		downloadHeaders.Set("Accept", "image/*,*/*;q=0.8")
		downloadHeaders.Del("Content-Type")
		request.SetHeaders(headerToMap(downloadHeaders))
	} else {
		userAgent := strings.TrimSpace(headers.Get("User-Agent"))
		if userAgent == "" {
			userAgent = openAIImageBackendUserAgent
		}
		request.SetHeader("User-Agent", userAgent)
	}

	resp, err := request.Get(downloadURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newOpenAIImageStatusError(resp, "download image bytes failed")
	}
	return io.ReadAll(io.LimitReader(resp.Body, openAIImageMaxDownloadBytes))
}

type openAIImageStatusError struct {
	StatusCode      int
	Message         string
	ResponseBody    []byte
	ResponseHeaders http.Header
	RequestID       string
	URL             string
}

func (e *openAIImageStatusError) Error() string {
	if e == nil {
		return "openai image backend request failed"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("openai image backend request failed: status %d", e.StatusCode)
	}
	return "openai image backend request failed"
}

func newOpenAIImageStatusError(resp *req.Response, fallback string) error {
	if resp == nil {
		if strings.TrimSpace(fallback) == "" {
			fallback = "openai image backend request failed"
		}
		return fmt.Errorf("%s", fallback)
	}

	statusCode := resp.StatusCode
	headers := http.Header(nil)
	requestID := ""
	requestURL := ""
	body := []byte(nil)

	if resp.Response != nil {
		headers = resp.Header.Clone()
		requestID = strings.TrimSpace(resp.Header.Get("x-request-id"))
		if resp.Request != nil && resp.Request.URL != nil {
			requestURL = resp.Request.URL.String()
		}
		if resp.Body != nil {
			body, _ = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			_ = resp.Body.Close()
		}
	}

	message := sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(body))
	if message == "" {
		prefix := strings.TrimSpace(fallback)
		if prefix == "" {
			prefix = "openai image backend request failed"
		}
		message = fmt.Sprintf("%s: status %d", prefix, statusCode)
	}

	return &openAIImageStatusError{
		StatusCode:      statusCode,
		Message:         message,
		ResponseBody:    body,
		ResponseHeaders: headers,
		RequestID:       requestID,
		URL:             requestURL,
	}
}

func isOpenAIImageTransientConversationNotFoundError(err error) bool {
	statusErr, ok := err.(*openAIImageStatusError)
	if !ok || statusErr == nil || statusErr.StatusCode != http.StatusNotFound {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(statusErr.Message))
	if strings.Contains(msg, "conversation_not_found") {
		return true
	}
	if strings.Contains(msg, "conversation") && strings.Contains(msg, "not found") {
		return true
	}
	bodyMsg := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(statusErr.ResponseBody)))
	if strings.Contains(bodyMsg, "conversation_not_found") {
		return true
	}
	return strings.Contains(bodyMsg, "conversation") && strings.Contains(bodyMsg, "not found")
}

func cloneHTTPHeader(src http.Header) http.Header {
	dst := make(http.Header, len(src))
	for key, values := range src {
		copied := make([]string, len(values))
		copy(copied, values)
		dst[key] = copied
	}
	return dst
}

func headerToMap(header http.Header) map[string]string {
	if len(header) == 0 {
		return nil
	}
	result := make(map[string]string, len(header))
	for key, values := range header {
		if len(values) == 0 {
			continue
		}
		result[key] = values[0]
	}
	return result
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
