package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type GrokMediaEndpoint string

const (
	GrokMediaEndpointImagesGenerations GrokMediaEndpoint = "images_generations"
	GrokMediaEndpointImagesEdits       GrokMediaEndpoint = "images_edits"
	GrokMediaEndpointVideosGenerations GrokMediaEndpoint = "videos_generations"
	GrokMediaEndpointVideosEdits       GrokMediaEndpoint = "videos_edits"
	GrokMediaEndpointVideosExtensions  GrokMediaEndpoint = "videos_extensions"
	GrokMediaEndpointVideoStatus       GrokMediaEndpoint = "video_status"
	GrokMediaEndpointVideoContent      GrokMediaEndpoint = "video_content"

	// xAI Imagine image edits accept at most three source images.
	grokMediaMaxEditSourceImages = 3
	// Async video tasks commonly outlive the normal one-hour sticky session.
	grokVideoPendingBillingTTL = 24 * time.Hour
	grokVideoBilledClaimTTL    = 48 * time.Hour
)

func (e GrokMediaEndpoint) RequiresRequestBody() bool {
	return !e.IsVideoLookupRequest()
}

func (e GrokMediaEndpoint) IsVideoLookupRequest() bool {
	return e == GrokMediaEndpointVideoStatus || e == GrokMediaEndpointVideoContent
}

func (e GrokMediaEndpoint) IsGenerationRequest() bool {
	switch e {
	case GrokMediaEndpointImagesGenerations,
		GrokMediaEndpointImagesEdits,
		GrokMediaEndpointVideosGenerations,
		GrokMediaEndpointVideosEdits,
		GrokMediaEndpointVideosExtensions:
		return true
	default:
		return false
	}
}

func (e GrokMediaEndpoint) IsVideoMutationRequest() bool {
	switch e {
	case GrokMediaEndpointVideosGenerations,
		GrokMediaEndpointVideosEdits,
		GrokMediaEndpointVideosExtensions:
		return true
	default:
		return false
	}
}

type GrokMediaRequestInfo struct {
	Model           string
	Prompt          string
	N               int
	Size            string
	SizeTier        string
	Resolution      string
	DurationSeconds int
	InputImageURLs  []string
	MaskImageURL    string
	Uploads         []OpenAIImagesUpload
	MaskUpload      *OpenAIImagesUpload
}

func (r GrokMediaRequestInfo) ModerationBody() []byte {
	payload := map[string]any{}
	if prompt := strings.TrimSpace(r.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}

	images := make([]map[string]string, 0, len(r.InputImageURLs)+len(r.Uploads)+1)
	for _, imageURL := range r.InputImageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			images = append(images, map[string]string{"image_url": imageURL})
		}
	}
	for _, upload := range r.Uploads {
		if dataURL := upload.ModerationDataURL(); dataURL != "" {
			images = append(images, map[string]string{"image_url": dataURL})
		}
	}
	if maskURL := strings.TrimSpace(r.MaskImageURL); maskURL != "" {
		images = append(images, map[string]string{"image_url": maskURL})
	}
	if r.MaskUpload != nil {
		if dataURL := r.MaskUpload.ModerationDataURL(); dataURL != "" {
			images = append(images, map[string]string{"image_url": dataURL})
		}
	}
	if len(images) > 0 {
		payload["images"] = images
	}
	if len(payload) == 0 {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}

func (e GrokMediaEndpoint) httpMethod() string {
	if e.IsVideoLookupRequest() {
		return http.MethodGet
	}
	return http.MethodPost
}

func ExtractGrokMediaModel(contentType string, body []byte) string {
	return ParseGrokMediaRequest(contentType, body).Model
}

func ParseGrokMediaRequest(contentType string, body []byte) GrokMediaRequestInfo {
	info := GrokMediaRequestInfo{N: 1}
	if gjson.ValidBytes(body) {
		parseGrokMediaJSONRequest(body, &info)
	} else {
		parseGrokMediaMultipartRequest(contentType, body, &info)
	}
	info.Model = strings.TrimSpace(info.Model)
	info.Prompt = strings.TrimSpace(info.Prompt)
	info.Size = strings.TrimSpace(info.Size)
	info.SizeTier = NormalizeImageBillingTierOrDefault(info.Size)
	info.Resolution = NormalizeVideoBillingResolutionOrDefault(info.Resolution)
	info.DurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(info.DurationSeconds)
	if info.N <= 0 {
		info.N = 1
	}
	return info
}

func parseGrokMediaJSONRequest(body []byte, info *GrokMediaRequestInfo) {
	if info == nil {
		return
	}
	info.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	info.Prompt = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
	info.Size = strings.TrimSpace(gjson.GetBytes(body, "size").String())
	info.Resolution = strings.TrimSpace(gjson.GetBytes(body, "resolution").String())
	if duration := gjson.GetBytes(body, "duration"); duration.Exists() && duration.Type == gjson.Number {
		info.DurationSeconds = int(duration.Int())
	}
	if n := gjson.GetBytes(body, "n"); n.Exists() && n.Type == gjson.Number {
		info.N = int(n.Int())
	}
	appendJSONImageURLs := func(value gjson.Result) {
		if !value.Exists() {
			return
		}
		switch {
		case value.IsArray():
			for _, item := range value.Array() {
				if imageURL := extractGrokMediaImageURL(item); imageURL != "" {
					info.InputImageURLs = append(info.InputImageURLs, imageURL)
				}
			}
		default:
			if imageURL := extractGrokMediaImageURL(value); imageURL != "" {
				info.InputImageURLs = append(info.InputImageURLs, imageURL)
			}
		}
	}
	appendJSONImageURLs(gjson.GetBytes(body, "image"))
	appendJSONImageURLs(gjson.GetBytes(body, "images"))
	appendJSONImageURLs(gjson.GetBytes(body, "reference_images"))
	info.MaskImageURL = extractGrokMediaImageURL(gjson.GetBytes(body, "mask"))
}

func extractGrokMediaImageURL(value gjson.Result) string {
	if !value.Exists() {
		return ""
	}
	if value.Type == gjson.String {
		return strings.TrimSpace(value.String())
	}
	if imageURL := strings.TrimSpace(value.Get("url").String()); imageURL != "" {
		return imageURL
	}
	if nested := value.Get("image_url"); nested.Exists() {
		if nested.Type == gjson.String {
			return strings.TrimSpace(nested.String())
		}
		if imageURL := strings.TrimSpace(nested.Get("url").String()); imageURL != "" {
			return imageURL
		}
	}
	return ""
}

func grokMediaImageObject(imageURL string) map[string]string {
	return map[string]string{"url": imageURL, "type": "image_url"}
}

func parseGrokMediaMultipartRequest(contentType string, body []byte, info *GrokMediaRequestInfo) {
	if info == nil {
		return
	}
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
		name := strings.TrimSpace(part.FormName())
		if name == "" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, openAIImageMaxUploadPartSize))
		_ = part.Close()
		if err != nil {
			return
		}
		fileName := strings.TrimSpace(part.FileName())
		partContentType := strings.TrimSpace(part.Header.Get("Content-Type"))
		if fileName != "" {
			upload := OpenAIImagesUpload{
				FieldName:   name,
				FileName:    fileName,
				ContentType: partContentType,
				Data:        data,
			}
			if name == "mask" {
				info.MaskUpload = &upload
				continue
			}
			if name == "image" || strings.HasPrefix(name, "image[") ||
				name == "reference_images" || strings.HasPrefix(name, "reference_images[") {
				info.Uploads = append(info.Uploads, upload)
			}
			continue
		}

		value := strings.TrimSpace(string(data))
		switch name {
		case "model":
			info.Model = value
		case "prompt":
			info.Prompt = value
		case "size":
			info.Size = value
		case "resolution":
			info.Resolution = value
		case "duration":
			if duration, err := strconv.Atoi(value); err == nil {
				info.DurationSeconds = duration
			}
		case "n":
			if n, err := strconv.Atoi(value); err == nil {
				info.N = n
			}
		case "image", "image_url", "reference_images":
			if value != "" {
				info.InputImageURLs = append(info.InputImageURLs, value)
			}
		case "mask", "mask_image_url":
			info.MaskImageURL = value
		}
	}
}

func GrokMediaVideoRequestSessionHash(requestID string, userID, apiKeyID int64) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	ownerSeed := fmt.Sprintf("%d:%d:%s", userID, apiKeyID, requestID)
	return "grok-video:" + DeriveSessionHashFromSeed(ownerSeed)
}

func GrokMediaSessionHash(sessionHash string) string {
	sessionHash = strings.TrimSpace(sessionHash)
	if sessionHash == "" {
		return ""
	}
	return "grok-media:" + sessionHash
}

const grokMediaVideoOwnerBindingPrefix = "grok-video-owner:"

func grokMediaVideoOwnerBindingKey(requestID string, userID, apiKeyID int64) string {
	sessionHash := GrokMediaVideoRequestSessionHash(requestID, userID, apiKeyID)
	if sessionHash == "" {
		return ""
	}
	return grokMediaVideoOwnerBindingPrefix + sessionHash
}

func (s *OpenAIGatewayService) grokMediaVideoBindingTTL() time.Duration {
	ttl := grokVideoPendingBillingTTL
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds > 0 {
		if stickyTTL := time.Duration(s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds) * time.Second; stickyTTL > ttl {
			ttl = stickyTTL
		}
	}
	return ttl
}

func (s *OpenAIGatewayService) BindGrokMediaVideoRequestAccount(
	ctx context.Context,
	groupID *int64,
	requestID string,
	userID, apiKeyID, accountID int64,
) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("grok video request binding cache is unavailable")
	}
	ownerKey := grokMediaVideoOwnerBindingKey(requestID, userID, apiKeyID)
	routingKey := s.openAISessionCacheKey(GrokMediaVideoRequestSessionHash(requestID, userID, apiKeyID))
	if ownerKey == "" || routingKey == "" || accountID <= 0 {
		return fmt.Errorf("grok video request binding is invalid")
	}
	ttl := s.grokMediaVideoBindingTTL()
	group := derefGroupID(groupID)
	if err := s.cache.SetSessionString(ctx, group, ownerKey, strconv.FormatInt(accountID, 10), ttl); err != nil {
		return fmt.Errorf("store grok video owner binding: %w", err)
	}
	if err := s.cache.SetSessionAccountID(ctx, group, routingKey, accountID, ttl); err != nil {
		return fmt.Errorf("store grok video routing binding: %w", err)
	}
	return nil
}

func (s *OpenAIGatewayService) ResolveGrokMediaVideoRequestAccount(
	ctx context.Context,
	groupID *int64,
	requestID string,
	userID, apiKeyID int64,
) (int64, error) {
	if s == nil || s.cache == nil {
		return 0, fmt.Errorf("grok video request binding cache is unavailable")
	}
	ownerKey := grokMediaVideoOwnerBindingKey(requestID, userID, apiKeyID)
	routingKey := s.openAISessionCacheKey(GrokMediaVideoRequestSessionHash(requestID, userID, apiKeyID))
	if ownerKey == "" || routingKey == "" {
		return 0, fmt.Errorf("grok video request binding is invalid")
	}
	group := derefGroupID(groupID)
	ttl := s.grokMediaVideoBindingTTL()
	rawAccountID, err := s.cache.GetSessionString(ctx, group, ownerKey)
	if err != nil && !errors.Is(err, ErrGatewaySessionStringNotFound) {
		return 0, fmt.Errorf("load grok video owner binding: %w", err)
	}
	if errors.Is(err, ErrGatewaySessionStringNotFound) {
		// Compatibility for in-flight tasks created before owner bindings used
		// their own namespace. Migrate the old routing record on first lookup.
		legacyAccountID, legacyErr := s.cache.GetSessionAccountID(ctx, group, routingKey)
		if legacyErr != nil {
			return 0, legacyErr
		}
		rawAccountID = strconv.FormatInt(legacyAccountID, 10)
	}
	accountID, parseErr := strconv.ParseInt(strings.TrimSpace(rawAccountID), 10, 64)
	if parseErr != nil || accountID <= 0 {
		return 0, fmt.Errorf("grok video owner binding is invalid")
	}
	if err := s.cache.SetSessionString(ctx, group, ownerKey, strconv.FormatInt(accountID, 10), ttl); err != nil {
		return 0, fmt.Errorf("refresh grok video owner binding: %w", err)
	}
	if err := s.cache.SetSessionAccountID(ctx, group, routingKey, accountID, ttl); err != nil {
		return 0, fmt.Errorf("restore grok video routing binding: %w", err)
	}
	return accountID, nil
}

// grokVideoBillingCache is deliberately narrower than GatewayCache. This keeps
// unrelated cache test doubles source-compatible while requiring the concrete
// Redis cache to expose atomic video billing operations at runtime.
type grokVideoBillingCache interface {
	SetGrokVideoPendingBilling(ctx context.Context, key string, payload []byte, ttl time.Duration) error
	GetGrokVideoPendingBilling(ctx context.Context, key string) ([]byte, error)
	ClaimGrokVideoBilled(ctx context.Context, key string, ttl time.Duration) (bool, error)
	ReleaseGrokVideoBilled(ctx context.Context, key string) error
}

// GrokVideoPendingBilling is the create-time snapshot used when the first
// official status=done + video.url response is observed. xAI omits resolution
// from status responses, so billing must retain the original request values.
type GrokVideoPendingBilling struct {
	Model                string `json:"model"`
	BillingModel         string `json:"billing_model,omitempty"`
	UpstreamModel        string `json:"upstream_model,omitempty"`
	VideoResolution      string `json:"video_resolution,omitempty"`
	VideoDurationSeconds int    `json:"video_duration_seconds,omitempty"`
	OriginalModel        string `json:"original_model,omitempty"`
	CreatedAt            string `json:"created_at,omitempty"`
}

func GrokVideoPendingCreatedAtNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// GrokVideoE2EDuration measures async job latency from create acceptance to the
// first completed observation. Invalid or future timestamps fail closed to 0.
func GrokVideoE2EDuration(createdAt string, discoveredAt time.Time) time.Duration {
	createdAt = strings.TrimSpace(createdAt)
	if createdAt == "" {
		return 0
	}
	if discoveredAt.IsZero() {
		discoveredAt = time.Now()
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		created, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return 0
		}
	}
	duration := discoveredAt.Sub(created)
	if duration < 0 {
		return 0
	}
	return duration
}

func grokVideoPendingBillingKey(requestID string, userID, apiKeyID int64) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", userID, apiKeyID, requestID)
}

func (s *OpenAIGatewayService) requireGrokVideoBillingCache() (grokVideoBillingCache, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("grok video billing cache is unavailable")
	}
	cache, ok := s.cache.(grokVideoBillingCache)
	if !ok || cache == nil {
		return nil, fmt.Errorf("grok video billing cache does not support atomic billing state")
	}
	return cache, nil
}

func (s *OpenAIGatewayService) StoreGrokVideoPendingBilling(
	ctx context.Context,
	requestID string,
	userID, apiKeyID int64,
	pending GrokVideoPendingBilling,
) error {
	cache, err := s.requireGrokVideoBillingCache()
	if err != nil {
		return err
	}
	key := grokVideoPendingBillingKey(requestID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("grok video pending billing key is invalid")
	}
	pending.Model = strings.TrimSpace(pending.Model)
	pending.BillingModel = strings.TrimSpace(pending.BillingModel)
	pending.UpstreamModel = strings.TrimSpace(pending.UpstreamModel)
	pending.OriginalModel = strings.TrimSpace(pending.OriginalModel)
	if pending.VideoResolution != "" {
		pending.VideoResolution = NormalizeVideoBillingResolutionOrDefault(pending.VideoResolution)
	}
	if pending.VideoDurationSeconds > 0 {
		pending.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(pending.VideoDurationSeconds)
	}
	if strings.TrimSpace(pending.CreatedAt) == "" {
		pending.CreatedAt = GrokVideoPendingCreatedAtNow()
	} else {
		pending.CreatedAt = strings.TrimSpace(pending.CreatedAt)
	}
	payload, err := json.Marshal(pending)
	if err != nil {
		return fmt.Errorf("marshal grok video pending billing: %w", err)
	}
	return cache.SetGrokVideoPendingBilling(ctx, key, payload, grokVideoPendingBillingTTL)
}

func (s *OpenAIGatewayService) LoadGrokVideoPendingBilling(
	ctx context.Context,
	requestID string,
	userID, apiKeyID int64,
) (*GrokVideoPendingBilling, error) {
	cache, err := s.requireGrokVideoBillingCache()
	if err != nil {
		return nil, err
	}
	key := grokVideoPendingBillingKey(requestID, userID, apiKeyID)
	if key == "" {
		return nil, fmt.Errorf("grok video pending billing key is invalid")
	}
	payload, err := cache.GetGrokVideoPendingBilling(ctx, key)
	if err != nil || len(payload) == 0 {
		return nil, err
	}
	var pending GrokVideoPendingBilling
	if err := json.Unmarshal(payload, &pending); err != nil {
		return nil, fmt.Errorf("decode grok video pending billing: %w", err)
	}
	return &pending, nil
}

func (s *OpenAIGatewayService) ClaimGrokVideoBilling(
	ctx context.Context,
	requestID string,
	userID, apiKeyID int64,
) (bool, error) {
	cache, err := s.requireGrokVideoBillingCache()
	if err != nil {
		return false, err
	}
	key := grokVideoPendingBillingKey(requestID, userID, apiKeyID)
	if key == "" {
		return false, fmt.Errorf("grok video billing claim key is invalid")
	}
	return cache.ClaimGrokVideoBilled(ctx, key, grokVideoBilledClaimTTL)
}

func (s *OpenAIGatewayService) ReleaseGrokVideoBilling(
	ctx context.Context,
	requestID string,
	userID, apiKeyID int64,
) error {
	cache, err := s.requireGrokVideoBillingCache()
	if err != nil {
		return err
	}
	key := grokVideoPendingBillingKey(requestID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("grok video billing claim key is invalid")
	}
	return cache.ReleaseGrokVideoBilled(ctx, key)
}

func StableGrokVideoBillingRequestID(taskRequestID string) string {
	taskRequestID = strings.TrimSpace(taskRequestID)
	if taskRequestID == "" {
		return ""
	}
	if strings.HasPrefix(taskRequestID, "grok-video:") {
		return taskRequestID
	}
	return "grok-video:" + taskRequestID
}

// IsGrokVideoStatusBillable accepts only the official success shape. A URL in
// any other state must never trigger a charge.
func IsGrokVideoStatusBillable(statusBody []byte) bool {
	if len(statusBody) == 0 || !gjson.ValidBytes(statusBody) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(gjson.GetBytes(statusBody, "status").String()), "done") &&
		strings.TrimSpace(gjson.GetBytes(statusBody, "video.url").String()) != ""
}

func ExtractGrokVideoBillingFromStatusBody(
	statusBody []byte,
	pending *GrokVideoPendingBilling,
	requestID string,
) *OpenAIForwardResult {
	if !IsGrokVideoStatusBillable(statusBody) {
		return nil
	}
	model := strings.TrimSpace(gjson.GetBytes(statusBody, "model").String())
	billingModel := ""
	upstreamModel := ""
	resolution := ""
	durationSeconds := 0
	if value := gjson.GetBytes(statusBody, "video.duration"); value.Exists() && value.Type == gjson.Number {
		durationSeconds = int(value.Int())
		if durationSeconds == 0 && value.Float() >= 1 {
			durationSeconds = int(value.Float())
		}
	}
	if pending != nil {
		if model == "" {
			model = firstNonEmpty(pending.BillingModel, pending.Model, pending.OriginalModel)
		}
		billingModel = firstNonEmpty(pending.BillingModel, pending.Model)
		upstreamModel = pending.UpstreamModel
		resolution = pending.VideoResolution
		if durationSeconds <= 0 {
			durationSeconds = pending.VideoDurationSeconds
		}
	}
	if model == "" {
		model = xai.DefaultImagineVideoModel
	}
	if billingModel == "" {
		billingModel = model
	}
	if resolution != "" {
		resolution = NormalizeVideoBillingResolutionOrDefault(resolution)
	}
	if durationSeconds > 0 {
		durationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(durationSeconds)
	}
	responseID := extractGrokMediaVideoRequestID(statusBody)
	if responseID == "" {
		responseID = strings.TrimSpace(requestID)
	}
	return &OpenAIForwardResult{
		ResponseID:           responseID,
		Model:                model,
		BillingModel:         billingModel,
		UpstreamModel:        upstreamModel,
		VideoCount:           1,
		VideoResolution:      resolution,
		VideoDurationSeconds: durationSeconds,
	}
}

func (s *OpenAIGatewayService) ForwardGrokMedia(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	endpoint GrokMediaEndpoint,
	requestID string,
	body []byte,
	contentType string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil {
		return nil, fmt.Errorf("grok account is required")
	}
	if account.Platform != PlatformGrok {
		return nil, fmt.Errorf("account platform %s is not supported for grok media", account.Platform)
	}

	token, _, err := s.GetRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if endpoint == GrokMediaEndpointVideoContent {
		return s.forwardGrokMediaVideoContent(ctx, c, account, token, requestID, startTime)
	}
	targetURL, err := buildGrokMediaURL(ctx, account, s.cfg, s.settingService, endpoint, requestID)
	if err != nil {
		return nil, err
	}
	targetURL, err = appendGrokMediaRequestQuery(targetURL, c)
	if err != nil {
		return nil, err
	}

	body, contentType, err = prepareGrokMediaForwardBody(endpoint, body, contentType)
	if err != nil {
		return nil, err
	}
	body, contentType, err = normalizeGrokMediaForwardBody(endpoint, body, contentType)
	if err != nil {
		return nil, err
	}
	requestInfo := ParseGrokMediaRequest(contentType, body)
	upstreamModel := requestInfo.Model
	if endpoint.RequiresRequestBody() && gjson.ValidBytes(body) {
		if mappedModel := strings.TrimSpace(account.GetMappedModel(requestInfo.Model)); mappedModel != "" {
			upstreamModel = mappedModel
		}
		if upstreamModel != requestInfo.Model {
			body, err = sjson.SetBytes(body, "model", upstreamModel)
			if err != nil {
				return nil, fmt.Errorf("rewrite grok media account mapped model: %w", err)
			}
		}
	}
	body, contentType, err = sanitizeGrokMediaForwardBody(endpoint, body, contentType)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if endpoint.RequiresRequestBody() {
		bodyReader = bytes.NewReader(body)
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	upstreamReq, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(upstreamCtx),
		endpoint.httpMethod(),
		targetURL,
		bodyReader,
	)
	if err != nil {
		return nil, err
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	upstreamReq.Header.Set("Accept", "application/json")
	if account.IsGrokOAuth() && isGrokCLIProxyTarget(targetURL) {
		applyGrokCLIHeaders(upstreamReq.Header)
	} else {
		upstreamReq.Header.Set("User-Agent", "sub2api-grok/1.0")
	}
	if endpoint.RequiresRequestBody() {
		contentType = strings.TrimSpace(contentType)
		if contentType == "" {
			contentType = "application/json"
		}
		upstreamReq.Header.Set("Content-Type", contentType)
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)

	proxyURL, err := grokAccountProxyURL(account)
	if err != nil {
		return nil, err
	}
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	requestIDHeader := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id"))
	requestModel := requestInfo.Model
	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		if !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(
				resp,
				c,
				account,
				false,
				writeGrokMediaErrorResponse,
			)
		}
		return s.handleGrokMediaErrorResponse(ctx, resp, c, account, requestIDHeader, requestModel)
	}

	s.updateGrokUsageFromResponse(ctx, account, resp.Header, resp.StatusCode)
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	usage, err := grokMediaUsageFromResponse(endpoint, requestInfo, respBody)
	if err != nil {
		safeMessage := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, resp.StatusCode, safeMessage, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  requestIDHeader,
			Kind:               "invalid_response",
			Message:            safeMessage,
		})
		writeGrokMediaErrorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return nil, err
	}
	if endpoint == GrokMediaEndpointVideoStatus {
		respBody = rewriteGrokMediaVideoContentURLs(
			respBody,
			requestID,
			grokMediaContentProxyURL(c, requestID),
		)
	}
	resultModel := requestModel
	resultBillingModel := requestModel
	if endpoint == GrokMediaEndpointVideoStatus {
		if strings.TrimSpace(usage.Model) != "" {
			resultModel = usage.Model
		}
		if strings.TrimSpace(usage.BillingModel) != "" {
			resultBillingModel = usage.BillingModel
		}
	}
	result := &OpenAIForwardResult{
		RequestID:            requestIDHeader,
		ResponseID:           usage.ResponseID,
		Usage:                usage.Usage,
		Model:                resultModel,
		BillingModel:         resultBillingModel,
		UpstreamModel:        upstreamModel,
		ResponseHeaders:      resp.Header.Clone(),
		Duration:             time.Since(startTime),
		ImageCount:           usage.ImageCount,
		ImageSize:            usage.ImageSize,
		ImageInputSize:       usage.ImageInputSize,
		ImageOutputSizes:     usage.ImageOutputSizes,
		VideoCount:           usage.VideoCount,
		VideoResolution:      usage.VideoResolution,
		VideoDurationSeconds: usage.VideoDurationSeconds,
	}
	writeGrokMediaResponse(c, resp, respBody, s.responseHeaderFilter)
	return result, nil
}

func (s *OpenAIGatewayService) forwardGrokMediaVideoContent(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token, requestID string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	statusURL, err := buildGrokMediaURL(
		ctx, account, s.cfg, s.settingService, GrokMediaEndpointVideoStatus, requestID,
	)
	if err != nil {
		return nil, err
	}

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	statusReq, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(upstreamCtx),
		http.MethodGet,
		statusURL,
		nil,
	)
	if err != nil {
		return nil, err
	}
	statusReq.Header.Set("Authorization", "Bearer "+token)
	statusReq.Header.Set("Accept", "application/json")
	if account.IsGrokOAuth() && isGrokCLIProxyTarget(statusURL) {
		applyGrokCLIHeaders(statusReq.Header)
	}
	account.ApplyHeaderOverrides(statusReq.Header)

	proxyURL, err := grokAccountProxyURL(account)
	if err != nil {
		return nil, err
	}
	upstreamStart := time.Now()
	statusResp, err := s.httpUpstream.Do(statusReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	statusRequestID := firstNonEmpty(statusResp.Header.Get("x-request-id"), statusResp.Header.Get("xai-request-id"))
	if !isOpenAIUpstreamSuccessStatus(statusResp.StatusCode) {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		if !isOpenAIUpstreamErrorStatus(statusResp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(
				statusResp,
				c,
				account,
				false,
				writeGrokMediaErrorResponse,
			)
		}
		defer func() { _ = statusResp.Body.Close() }()
		return s.handleGrokMediaErrorResponse(ctx, statusResp, c, account, statusRequestID, "")
	}
	statusBody, err := ReadUpstreamResponseBody(statusResp.Body, s.cfg, c, openAITooLargeError)
	_ = statusResp.Body.Close()
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, err
	}
	s.updateGrokUsageFromResponse(ctx, account, statusResp.Header, statusResp.StatusCode)

	contentURL, err := grokMediaSignedVideoContentURL(statusBody, requestID)
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, err
	}
	signedContent := contentURL != ""
	if !signedContent {
		contentURL, err = buildGrokMediaURL(
			ctx, account, s.cfg, s.settingService, GrokMediaEndpointVideoContent, requestID,
		)
		if err != nil {
			SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
			return nil, err
		}
	}

	contentReq, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(upstreamCtx),
		http.MethodGet,
		contentURL,
		nil,
	)
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, err
	}
	contentReq.Header.Set("Accept", "*/*")
	if c != nil {
		if rangeHeader := strings.TrimSpace(c.GetHeader("Range")); rangeHeader != "" {
			contentReq.Header.Set("Range", rangeHeader)
		}
	}
	if !signedContent {
		contentReq.Header.Set("Authorization", "Bearer "+token)
		if account.IsGrokOAuth() && isGrokCLIProxyTarget(contentURL) {
			applyGrokCLIHeaders(contentReq.Header)
		}
		account.ApplyHeaderOverrides(contentReq.Header)
	}

	contentResp, err := s.httpUpstream.Do(contentReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = contentResp.Body.Close() }()
	contentRequestID := firstNonEmpty(
		contentResp.Header.Get("x-request-id"),
		contentResp.Header.Get("xai-request-id"),
		statusRequestID,
	)
	if !isOpenAIUpstreamSuccessStatus(contentResp.StatusCode) &&
		contentResp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		if !isOpenAIUpstreamErrorStatus(contentResp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(
				contentResp,
				c,
				account,
				false,
				writeGrokMediaErrorResponse,
			)
		}
		return s.handleGrokMediaErrorResponse(ctx, contentResp, c, account, contentRequestID, "")
	}

	s.updateGrokUsageFromResponse(ctx, account, contentResp.Header, contentResp.StatusCode)
	if err := writeGrokMediaContentResponse(c, contentResp); err != nil {
		return nil, err
	}
	result := &OpenAIForwardResult{
		RequestID:       contentRequestID,
		ResponseHeaders: contentResp.Header.Clone(),
		Duration:        time.Since(startTime),
	}
	if billed := ExtractGrokVideoBillingFromStatusBody(statusBody, nil, requestID); billed != nil {
		result.ResponseID = firstNonEmpty(billed.ResponseID, strings.TrimSpace(requestID))
		result.Model = billed.Model
		result.BillingModel = billed.BillingModel
		result.UpstreamModel = billed.UpstreamModel
		result.VideoCount = billed.VideoCount
		result.VideoResolution = billed.VideoResolution
		result.VideoDurationSeconds = billed.VideoDurationSeconds
	}
	return result, nil
}

func grokMediaSignedVideoContentURL(body []byte, requestID string) (string, error) {
	rawURL := strings.TrimSpace(gjson.GetBytes(body, "video.url").String())
	if rawURL == "" {
		return "", nil
	}
	if isGrokMediaVideoContentURL(rawURL, requestID) {
		return "", nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") ||
		!strings.EqualFold(parsed.Hostname(), "vidgen.x.ai") ||
		(parsed.Port() != "" && parsed.Port() != "443") || parsed.User != nil {
		return "", fmt.Errorf("grok media status returned an unsupported video content URL")
	}
	return parsed.String(), nil
}

func isGrokCLIProxyTarget(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && strings.EqualFold(parsed.Hostname(), "cli-chat-proxy.grok.com")
}

func grokAccountProxyURL(account *Account) (string, error) {
	if account == nil || account.ProxyID == nil {
		return "", nil
	}
	if account.Proxy == nil {
		return "", fmt.Errorf("grok account proxy binding is unavailable")
	}
	proxyURL := strings.TrimSpace(account.Proxy.URL())
	if proxyURL == "" {
		return "", fmt.Errorf("grok account proxy binding is invalid")
	}
	return proxyURL, nil
}

func prepareGrokMediaForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	if endpoint != GrokMediaEndpointImagesEdits {
		return body, contentType, nil
	}
	if gjson.ValidBytes(body) {
		out, err := normalizeGrokMediaJSONImageRefs(body)
		return out, contentType, err
	}
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return body, contentType, nil
	}

	info := ParseGrokMediaRequest(contentType, body)
	payload := make(map[string]any)
	if info.Model != "" {
		payload["model"] = info.Model
	}
	if info.Prompt != "" {
		payload["prompt"] = info.Prompt
	}
	if info.N > 1 {
		payload["n"] = info.N
	}
	if info.Size != "" {
		payload["size"] = info.Size
	}

	images := make([]map[string]string, 0, len(info.InputImageURLs)+len(info.Uploads))
	for _, imageURL := range info.InputImageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			images = append(images, grokMediaImageObject(imageURL))
		}
	}
	for _, upload := range info.Uploads {
		dataURL, err := openAIImageUploadToDataURL(upload)
		if err != nil {
			return nil, "", err
		}
		images = append(images, grokMediaImageObject(dataURL))
	}
	if len(images) > grokMediaMaxEditSourceImages {
		return nil, "", fmt.Errorf("a maximum of %d source images is supported for image edits", grokMediaMaxEditSourceImages)
	}
	if len(images) > 0 {
		payload["image"] = images[0]
		if len(images) > 1 {
			payload["images"] = images
		}
	}

	maskImageURL := strings.TrimSpace(info.MaskImageURL)
	if info.MaskUpload != nil {
		dataURL, err := openAIImageUploadToDataURL(*info.MaskUpload)
		if err != nil {
			return nil, "", err
		}
		maskImageURL = dataURL
	}
	if maskImageURL != "" {
		payload["mask"] = grokMediaImageObject(maskImageURL)
	}

	out, err := marshalOpenAIUpstreamJSON(payload)
	if err != nil {
		return nil, "", err
	}
	return out, "application/json", nil
}

func normalizeGrokMediaJSONImageRefs(body []byte) ([]byte, error) {
	info := ParseGrokMediaRequest("application/json", body)
	if len(info.InputImageURLs) > grokMediaMaxEditSourceImages {
		return nil, fmt.Errorf("a maximum of %d source images is supported for image edits", grokMediaMaxEditSourceImages)
	}
	out := body
	var err error
	for _, field := range []string{"image", "images", "mask"} {
		out, err = rewriteGrokMediaJSONImageField(out, field)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func rewriteGrokMediaJSONImageField(body []byte, path string) ([]byte, error) {
	value := gjson.GetBytes(body, path)
	if !value.Exists() {
		return body, nil
	}
	if value.IsArray() {
		rewritten := make([]map[string]string, 0, len(value.Array()))
		for _, item := range value.Array() {
			imageURL := extractGrokMediaImageURL(item)
			if imageURL == "" {
				return nil, fmt.Errorf("invalid grok media %s image reference", path)
			}
			rewritten = append(rewritten, grokMediaImageObject(imageURL))
		}
		out, err := sjson.SetBytes(body, path, rewritten)
		if err != nil {
			return nil, fmt.Errorf("rewrite grok media %s: %w", path, err)
		}
		return out, nil
	}
	imageURL := extractGrokMediaImageURL(value)
	if imageURL == "" {
		return nil, fmt.Errorf("invalid grok media %s image reference", path)
	}
	out, err := sjson.SetBytes(body, path, grokMediaImageObject(imageURL))
	if err != nil {
		return nil, fmt.Errorf("rewrite grok media %s: %w", path, err)
	}
	return out, nil
}

func normalizeGrokMediaForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	if !endpoint.RequiresRequestBody() || !gjson.ValidBytes(body) {
		return body, contentType, nil
	}
	var imageFields []string
	switch endpoint {
	case GrokMediaEndpointImagesEdits:
		imageFields = []string{"image", "images", "mask"}
	case GrokMediaEndpointVideosGenerations, GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions:
		imageFields = []string{"image", "images", "reference_images"}
	}
	var err error
	body, err = canonicalizeGrokMediaImageURLFields(body, imageFields...)
	if err != nil {
		return nil, "", err
	}
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	upstreamModel := NormalizeGrokMediaModelForEndpoint(endpoint, model)
	if upstreamModel == "" || upstreamModel == model {
		return body, contentType, nil
	}
	out, err := sjson.SetBytes(body, "model", upstreamModel)
	if err != nil {
		return nil, "", fmt.Errorf("rewrite grok media model: %w", err)
	}
	return out, contentType, nil
}

func canonicalizeGrokMediaImageURLFields(body []byte, fields ...string) ([]byte, error) {
	out := body
	for _, field := range fields {
		value := gjson.GetBytes(out, field)
		if !value.Exists() {
			continue
		}
		if value.IsArray() {
			for index := range value.Array() {
				var err error
				out, err = canonicalizeGrokMediaImageURLObject(out, fmt.Sprintf("%s.%d", field, index))
				if err != nil {
					return nil, err
				}
			}
			continue
		}
		var err error
		out, err = canonicalizeGrokMediaImageURLObject(out, field)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func canonicalizeGrokMediaImageURLObject(body []byte, path string) ([]byte, error) {
	legacyPath := path + ".image_url"
	legacy := gjson.GetBytes(body, legacyPath)
	if !legacy.Exists() {
		return body, nil
	}
	out := body
	if strings.TrimSpace(gjson.GetBytes(out, path+".url").String()) == "" {
		var err error
		out, err = sjson.SetBytes(out, path+".url", legacy.Value())
		if err != nil {
			return nil, fmt.Errorf("normalize grok media image url: %w", err)
		}
	}
	out, err := sjson.DeleteBytes(out, legacyPath)
	if err != nil {
		return nil, fmt.Errorf("remove legacy grok media image url: %w", err)
	}
	return out, nil
}

func sanitizeGrokMediaForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	if !endpoint.RequiresRequestBody() || !gjson.ValidBytes(body) {
		return body, contentType, nil
	}
	switch endpoint {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		if !gjson.GetBytes(body, "size").Exists() {
			return body, contentType, nil
		}
		out, err := sjson.DeleteBytes(body, "size")
		if err != nil {
			return nil, "", fmt.Errorf("sanitize grok media size: %w", err)
		}
		return out, contentType, nil
	default:
		return body, contentType, nil
	}
}

func appendGrokMediaRequestQuery(targetURL string, c *gin.Context) (string, error) {
	if c == nil || c.Request == nil || c.Request.URL == nil || strings.TrimSpace(c.Request.URL.RawQuery) == "" {
		return targetURL, nil
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return "", fmt.Errorf("invalid grok media query: %w", err)
	}
	for key := range query {
		if isGrokCredentialQueryKey(key) {
			return "", fmt.Errorf("grok media query contains a forbidden credential parameter")
		}
	}
	target, err := url.Parse(targetURL)
	if err != nil {
		return "", fmt.Errorf("invalid grok media target URL: %w", err)
	}
	// Only the query component is copied. The allowlisted target scheme, host,
	// and endpoint path remain immutable.
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func isGrokCredentialQueryKey(key string) bool {
	switch normalizeCredentialSafetyKey(key) {
	case "api_key", "apikey", "x_api_key", "xapikey",
		"authorization", "authorization_header", "authorizationheader",
		"access_token", "accesstoken", "refresh_token", "refreshtoken",
		"token", "credential", "credentials":
		return true
	default:
		return false
	}
}

// NormalizeGrokMediaModelForEndpoint resolves built-in media aliases before
// scheduler capability checks and account-level model mapping.
func NormalizeGrokMediaModelForEndpoint(endpoint GrokMediaEndpoint, model string) string {
	model = strings.TrimSpace(model)
	switch endpoint {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		if model == "grok-imagine" || model == "grok-imagine-edit" {
			return xai.DefaultImagineImageQualityModel
		}
	}
	return model
}

type grokMediaUsageMetadata struct {
	ResponseID           string
	Usage                OpenAIUsage
	Model                string
	BillingModel         string
	ImageCount           int
	ImageSize            string
	ImageInputSize       string
	ImageOutputSizes     []string
	VideoCount           int
	VideoResolution      string
	VideoDurationSeconds int
}

func grokMediaUsageFromResponse(
	endpoint GrokMediaEndpoint,
	requestInfo GrokMediaRequestInfo,
	responseBody []byte,
) (grokMediaUsageMetadata, error) {
	usage, _ := extractOpenAIUsageFromJSONBytes(responseBody)
	meta := grokMediaUsageMetadata{Usage: usage}
	switch endpoint {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		imageCount := countOpenAIResponseImageOutputsFromJSONBytes(responseBody)
		if imageCount <= 0 {
			return meta, fmt.Errorf("grok media upstream returned a successful response without image output")
		}
		meta.ImageCount = imageCount
		meta.ImageSize = requestInfo.SizeTier
		meta.ImageInputSize = requestInfo.Size
		meta.ImageOutputSizes = collectOpenAIResponseImageOutputSizesFromJSONBytes(responseBody)
	case GrokMediaEndpointVideosGenerations, GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions:
		meta.ResponseID = extractGrokMediaVideoRequestID(responseBody)
		if meta.ResponseID == "" {
			return meta, fmt.Errorf("grok media upstream returned a successful response without a video request id")
		}
		// Video creation is asynchronous. Keep pricing inputs but do not expose
		// billable units until status=done and video.url are both observed.
		meta.VideoResolution = requestInfo.Resolution
		meta.VideoDurationSeconds = requestInfo.DurationSeconds
	case GrokMediaEndpointVideoStatus:
		if billed := ExtractGrokVideoBillingFromStatusBody(responseBody, nil, ""); billed != nil {
			meta.ResponseID = billed.ResponseID
			meta.Model = billed.Model
			meta.BillingModel = billed.BillingModel
			meta.VideoCount = billed.VideoCount
			meta.VideoResolution = billed.VideoResolution
			meta.VideoDurationSeconds = billed.VideoDurationSeconds
		}
	}
	return meta, nil
}

func extractGrokMediaVideoRequestID(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	for _, path := range []string{
		"request_id", "id", "data.request_id", "data.id", "video.request_id", "video.id",
		"task_id", "data.task_id", "video.task_id",
	} {
		if id := strings.TrimSpace(gjson.GetBytes(body, path).String()); id != "" {
			return id
		}
	}
	return ""
}

func (s *OpenAIGatewayService) handleGrokMediaErrorResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	requestIDHeader string,
	requestedModel string,
) (*OpenAIForwardResult, error) {
	body := s.readUpstreamErrorBody(resp)
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if upstreamMsg == "" {
		upstreamMsg = fmt.Sprintf("xAI upstream returned status %d", resp.StatusCode)
	}

	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(body), maxBytes)
	}
	setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)
	if isGrokContentPolicyRejection(resp.StatusCode, body) {
		clientMsg := grokContentPolicyClientMessage(body)
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  requestIDHeader,
			Kind:               "http_error",
			Message:            clientMsg,
			Detail:             upstreamDetail,
		})
		MarkResponseCommitted(c)
		writeGrokMediaErrorResponse(c, http.StatusForbidden, "invalid_request_error", clientMsg)
		return nil, fmt.Errorf("grok content policy rejection: %s", clientMsg)
	}
	// Account health must be updated before any branch commits and returns an
	// error response; otherwise matched passthrough rules can leave a 401/429
	// account eligible for immediate rescheduling.
	s.handleGrokAccountUpstreamError(withGrokRequestedModel(ctx, requestedModel), account, resp.StatusCode, resp.Header, body)

	if status, errType, errMsg, matched := applyErrorPassthroughRule(
		c,
		account.Platform,
		resp.StatusCode,
		body,
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	); matched {
		MarkResponseCommitted(c)
		writeGrokMediaErrorResponse(c, status, errType, errMsg)
		return nil, fmt.Errorf("upstream error: %d (passthrough rule matched) message=%s", resp.StatusCode, upstreamMsg)
	}

	if !account.ShouldHandleErrorCode(resp.StatusCode) {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  requestIDHeader,
			Kind:               "http_error",
			Message:            upstreamMsg,
			Detail:             upstreamDetail,
		})
		MarkResponseCommitted(c)
		writeGrokMediaErrorResponse(c, http.StatusInternalServerError, "upstream_error", "Upstream gateway error")
		return nil, fmt.Errorf("upstream error: %d (not in custom error codes) message=%s", resp.StatusCode, upstreamMsg)
	}

	kind := "http_error"
	if s.shouldFailoverGrokUpstreamError(resp.StatusCode, body) {
		kind = "failover"
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: resp.StatusCode,
		UpstreamRequestID:  requestIDHeader,
		Kind:               kind,
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})
	if kind == "failover" {
		return nil, &UpstreamFailoverError{
			StatusCode:             resp.StatusCode,
			ResponseBody:           body,
			RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
		}
	}

	MarkResponseCommitted(c)
	writeGrokMediaErrorResponse(c, resp.StatusCode, grokMediaErrorType(resp.StatusCode), upstreamMsg)
	return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
}

func grokMediaErrorType(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		return "upstream_error"
	}
}

func writeGrokMediaErrorResponse(c *gin.Context, statusCode int, errType, message string) {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return
	}
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    strings.TrimSpace(errType),
			"message": strings.TrimSpace(message),
		},
	})
}

func writeGrokMediaResponse(c *gin.Context, resp *http.Response, body []byte, filter *responseheaders.CompiledHeaderFilter) {
	if c == nil || resp == nil {
		return
	}
	writeOpenAIPassthroughResponseHeaders(c.Writer.Header(), resp.Header, filter)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(resp.StatusCode, contentType, body)
}

func writeGrokMediaContentResponse(c *gin.Context, resp *http.Response) error {
	if c == nil || resp == nil || resp.Body == nil {
		return fmt.Errorf("grok media content response is incomplete")
	}

	for _, name := range []string{
		"Content-Type",
		"Content-Length",
		"Content-Range",
		"Accept-Ranges",
		"Content-Disposition",
	} {
		if value := strings.TrimSpace(resp.Header.Get(name)); value != "" {
			c.Header(name, value)
		}
	}
	if strings.TrimSpace(c.Writer.Header().Get("Content-Length")) == "" && resp.ContentLength >= 0 {
		c.Header("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	if strings.TrimSpace(c.Writer.Header().Get("Content-Type")) == "" {
		c.Header("Content-Type", "application/octet-stream")
	}
	c.Status(resp.StatusCode)
	MarkResponseCommitted(c)
	_, err := io.Copy(c.Writer, resp.Body)
	return err
}

func rewriteGrokMediaVideoContentURLs(body []byte, requestID, proxyURL string) []byte {
	if len(body) == 0 || strings.TrimSpace(requestID) == "" || strings.TrimSpace(proxyURL) == "" || !gjson.ValidBytes(body) {
		return body
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return body
	}
	changed := rewriteGrokMediaKnownVideoURL(&value, proxyURL)
	if rewriteGrokMediaVideoContentURLValue(&value, requestID, proxyURL) {
		changed = true
	}
	if !changed {
		return body
	}
	rewritten, err := json.Marshal(value)
	if err != nil {
		return body
	}
	return rewritten
}

func rewriteGrokMediaKnownVideoURL(value *any, proxyURL string) bool {
	if value == nil {
		return false
	}
	root, ok := (*value).(map[string]any)
	if !ok {
		return false
	}
	video, ok := root["video"].(map[string]any)
	if !ok {
		return false
	}
	rawURL, ok := video["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return false
	}
	video["url"] = proxyURL
	return true
}

func rewriteGrokMediaVideoContentURLValue(value *any, requestID, proxyURL string) bool {
	if value == nil {
		return false
	}
	switch typed := (*value).(type) {
	case map[string]any:
		changed := false
		for key, child := range typed {
			childValue := child
			if rewriteGrokMediaVideoContentURLValue(&childValue, requestID, proxyURL) {
				typed[key] = childValue
				changed = true
			}
		}
		return changed
	case []any:
		changed := false
		for index, child := range typed {
			childValue := child
			if rewriteGrokMediaVideoContentURLValue(&childValue, requestID, proxyURL) {
				typed[index] = childValue
				changed = true
			}
		}
		return changed
	case string:
		if isGrokMediaVideoContentURL(typed, requestID) {
			*value = proxyURL
			return true
		}
	}
	return false
}

func isGrokMediaVideoContentURL(rawURL, requestID string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Path == "" {
		return false
	}
	segments := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(segments) < 3 {
		return false
	}
	requestID = strings.Trim(requestID, "/")
	decodedID, err := url.PathUnescape(segments[len(segments)-2])
	if err != nil {
		return false
	}
	return segments[len(segments)-3] == "videos" &&
		decodedID == requestID &&
		segments[len(segments)-1] == "content"
}

func grokMediaContentProxyURL(c *gin.Context, requestID string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil || strings.TrimSpace(requestID) == "" {
		return ""
	}
	pathPrefix := ""
	if strings.HasPrefix(c.Request.URL.Path, "/v1/") {
		pathPrefix = "/v1"
	}
	return pathPrefix + "/videos/" + url.PathEscape(strings.Trim(requestID, "/")) + "/content"
}
