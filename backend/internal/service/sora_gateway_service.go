package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

const soraImageInputMaxBytes = 20 << 20
const soraImageInputMaxRedirects = 3
const soraImageInputTimeout = 20 * time.Second
const soraVideoInputMaxBytes = 200 << 20
const soraVideoInputMaxRedirects = 3
const soraVideoInputTimeout = 60 * time.Second

var soraImageSizeMap = map[string]string{
	"gpt-image":           "360",
	"gpt-image-landscape": "540",
	"gpt-image-portrait":  "540",
}

var soraBlockedHostnames = map[string]struct{}{
	"localhost":                 {},
	"localhost.localdomain":     {},
	"metadata.google.internal":  {},
	"metadata.google.internal.": {},
}

var soraBlockedCIDRs = mustParseCIDRs([]string{
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"fc00::/7",
	"fe80::/10",
})

// SoraGatewayService handles forwarding requests to Sora upstream.
type SoraGatewayService struct {
	soraClient       SoraClient
	rateLimitService *RateLimitService
	httpUpstream     HTTPUpstream // 用于 apikey 类型账号的 HTTP 透传
	cfg              *config.Config
}

type soraWatermarkOptions struct {
	Enabled           bool
	ParseMethod       string
	ParseURL          string
	ParseToken        string
	FallbackOnFailure bool
	DeletePost        bool
}

type soraCharacterOptions struct {
	SetPublic           bool
	DeleteAfterGenerate bool
}

type soraCharacterFlowResult struct {
	CameoID     string
	CharacterID string
	Username    string
	DisplayName string
}

var soraStoryboardPattern = regexp.MustCompile(`\[\d+(?:\.\d+)?s\]`)
var soraStoryboardShotPattern = regexp.MustCompile(`\[(\d+(?:\.\d+)?)s\]\s*([^\[]+)`)
var soraRemixTargetPattern = regexp.MustCompile(`s_[a-f0-9]{32}`)
var soraRemixTargetInURLPattern = regexp.MustCompile(`https://sora\.chatgpt\.com/p/s_[a-f0-9]{32}`)

type soraPreflightChecker interface {
	PreflightCheck(ctx context.Context, account *Account, requestedModel string, modelCfg SoraModelConfig) error
}

func NewSoraGatewayService(
	soraClient SoraClient,
	rateLimitService *RateLimitService,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
) *SoraGatewayService {
	return &SoraGatewayService{
		soraClient:       soraClient,
		rateLimitService: rateLimitService,
		httpUpstream:     httpUpstream,
		cfg:              cfg,
	}
}

func (s *SoraGatewayService) Forward(ctx context.Context, c *gin.Context, account *Account, body []byte, clientStream bool) (*ForwardResult, error) {
	startTime := time.Now()

	// apikey 类型账号：HTTP 透传到上游，不走 SoraSDKClient
	if account.Type == AccountTypeAPIKey && account.GetBaseURL() != "" {
		if s.httpUpstream == nil {
			s.writeSoraError(c, http.StatusInternalServerError, "api_error", "HTTP upstream client not configured", clientStream)
			return nil, errors.New("httpUpstream not configured for sora apikey forwarding")
		}
		return s.forwardToUpstream(ctx, c, account, body, clientStream, startTime)
	}

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
	prompt, imageInput, videoInput, remixTargetID := extractSoraInput(reqBody)
	prompt = strings.TrimSpace(prompt)
	imageInput = strings.TrimSpace(imageInput)
	videoInput = strings.TrimSpace(videoInput)
	remixTargetID = strings.TrimSpace(remixTargetID)

	if videoInput != "" && modelCfg.Type != "video" {
		s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", "video input only supports video models", clientStream)
		return nil, errors.New("video input only supports video models")
	}
	if videoInput != "" && imageInput != "" {
		s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", "image input and video input cannot be used together", clientStream)
		return nil, errors.New("image input and video input cannot be used together")
	}
	characterOnly := videoInput != "" && prompt == ""
	if modelCfg.Type == "prompt_enhance" && prompt == "" {
		s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", "prompt is required", clientStream)
		return nil, errors.New("prompt is required")
	}
	if modelCfg.Type != "prompt_enhance" && prompt == "" && !characterOnly {
		s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", "prompt is required", clientStream)
		return nil, errors.New("prompt is required")
	}

	reqCtx, cancel := s.withSoraTimeout(ctx, reqStream)
	if cancel != nil {
		defer cancel()
	}
	if checker, ok := s.soraClient.(soraPreflightChecker); ok && !characterOnly {
		if err := checker.PreflightCheck(reqCtx, account, reqModel, modelCfg); err != nil {
			return nil, s.handleSoraRequestError(ctx, account, err, reqModel, c, clientStream)
		}
	}

	if modelCfg.Type == "prompt_enhance" {
		enhancedPrompt, err := s.soraClient.EnhancePrompt(reqCtx, account, prompt, modelCfg.ExpansionLevel, modelCfg.DurationS)
		if err != nil {
			return nil, s.handleSoraRequestError(ctx, account, err, reqModel, c, clientStream)
		}
		content := strings.TrimSpace(enhancedPrompt)
		if content == "" {
			content = prompt
		}
		var firstTokenMs *int
		if clientStream {
			ms, streamErr := s.writeSoraStream(c, reqModel, content, startTime)
			if streamErr != nil {
				return nil, streamErr
			}
			firstTokenMs = ms
		} else if c != nil {
			c.JSON(http.StatusOK, buildSoraNonStreamResponse(content, reqModel))
		}
		return &ForwardResult{
			RequestID:    "",
			Model:        reqModel,
			Stream:       clientStream,
			Duration:     time.Since(startTime),
			FirstTokenMs: firstTokenMs,
			Usage:        ClaudeUsage{},
			MediaType:    "prompt",
		}, nil
	}

	characterOpts := parseSoraCharacterOptions(reqBody)
	watermarkOpts := parseSoraWatermarkOptions(reqBody)
	var characterResult *soraCharacterFlowResult
	if videoInput != "" {
		videoData, videoErr := decodeSoraVideoInput(reqCtx, videoInput)
		if videoErr != nil {
			s.writeSoraError(c, http.StatusBadRequest, "invalid_request_error", videoErr.Error(), clientStream)
			return nil, videoErr
		}
		characterResult, videoErr = s.createCharacterFromVideo(reqCtx, account, videoData, characterOpts)
		if videoErr != nil {
			return nil, s.handleSoraRequestError(ctx, account, videoErr, reqModel, c, clientStream)
		}
		if characterResult != nil && characterOpts.DeleteAfterGenerate && strings.TrimSpace(characterResult.CharacterID) != "" && !characterOnly {
			characterID := strings.TrimSpace(characterResult.CharacterID)
			defer func() {
				cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancelCleanup()
				if err := s.soraClient.DeleteCharacter(cleanupCtx, account, characterID); err != nil {
					log.Printf("[Sora] cleanup character failed, character_id=%s err=%v", characterID, err)
				}
			}()
		}
		if characterOnly {
			content := "角色创建成功"
			if characterResult != nil && strings.TrimSpace(characterResult.Username) != "" {
				content = fmt.Sprintf("角色创建成功，角色名@%s", strings.TrimSpace(characterResult.Username))
			}
			var firstTokenMs *int
			if clientStream {
				ms, streamErr := s.writeSoraStream(c, reqModel, content, startTime)
				if streamErr != nil {
					return nil, streamErr
				}
				firstTokenMs = ms
			} else if c != nil {
				resp := buildSoraNonStreamResponse(content, reqModel)
				if characterResult != nil {
					resp["character_id"] = characterResult.CharacterID
					resp["cameo_id"] = characterResult.CameoID
					resp["character_username"] = characterResult.Username
					resp["character_display_name"] = characterResult.DisplayName
				}
				c.JSON(http.StatusOK, resp)
			}
			return &ForwardResult{
				RequestID:    "",
				Model:        reqModel,
				Stream:       clientStream,
				Duration:     time.Since(startTime),
				FirstTokenMs: firstTokenMs,
				Usage:        ClaudeUsage{},
				MediaType:    "prompt",
			}, nil
		}
		if characterResult != nil && strings.TrimSpace(characterResult.Username) != "" {
			prompt = fmt.Sprintf("@%s %s", characterResult.Username, prompt)
		}
	}

	var imageData []byte
	imageFilename := ""
	if imageInput != "" {
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
	videoCount := parseSoraVideoCount(reqBody)
	switch modelCfg.Type {
	case "image":
		taskID, err = s.soraClient.CreateImageTask(reqCtx, account, SoraImageRequest{
			Prompt:  prompt,
			Width:   modelCfg.Width,
			Height:  modelCfg.Height,
			MediaID: mediaID,
		})
	case "video":
		if remixTargetID == "" && isSoraStoryboardPrompt(prompt) {
			taskID, err = s.soraClient.CreateStoryboardTask(reqCtx, account, SoraStoryboardRequest{
				Prompt:      formatSoraStoryboardPrompt(prompt),
				Orientation: modelCfg.Orientation,
				Frames:      modelCfg.Frames,
				Model:       modelCfg.Model,
				Size:        modelCfg.Size,
				MediaID:     mediaID,
			})
		} else {
			taskID, err = s.soraClient.CreateVideoTask(reqCtx, account, SoraVideoRequest{
				Prompt:        prompt,
				Orientation:   modelCfg.Orientation,
				Frames:        modelCfg.Frames,
				Model:         modelCfg.Model,
				Size:          modelCfg.Size,
				VideoCount:    videoCount,
				MediaID:       mediaID,
				RemixTargetID: remixTargetID,
				CameoIDs:      extractSoraCameoIDs(reqBody),
			})
		}
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
	videoGenerationID := ""
	mediaType := modelCfg.Type
	imageCount := 0
	imageSize := ""
	switch modelCfg.Type {
	case "image":
		urls, pollErr := s.pollImageTask(reqCtx, c, account, taskID, clientStream)
		if pollErr != nil {
			return nil, s.handleSoraRequestError(ctx, account, pollErr, reqModel, c, clientStream)
		}
		mediaURLs = urls
		imageCount = len(urls)
		imageSize = soraImageSizeFromModel(reqModel)
	case "video":
		videoStatus, pollErr := s.pollVideoTaskDetailed(reqCtx, c, account, taskID, clientStream)
		if pollErr != nil {
			return nil, s.handleSoraRequestError(ctx, account, pollErr, reqModel, c, clientStream)
		}
		if videoStatus != nil {
			mediaURLs = videoStatus.URLs
			videoGenerationID = strings.TrimSpace(videoStatus.GenerationID)
		}
	default:
		mediaType = "prompt"
	}

	watermarkPostID := ""
	if modelCfg.Type == "video" && watermarkOpts.Enabled {
		watermarkURL, postID, watermarkErr := s.resolveWatermarkFreeURL(reqCtx, account, videoGenerationID, watermarkOpts)
		if watermarkErr != nil {
			if !watermarkOpts.FallbackOnFailure {
				return nil, s.handleSoraRequestError(ctx, account, watermarkErr, reqModel, c, clientStream)
			}
			log.Printf("[Sora] watermark-free fallback to original URL, task_id=%s err=%v", taskID, watermarkErr)
		} else if strings.TrimSpace(watermarkURL) != "" {
			mediaURLs = []string{strings.TrimSpace(watermarkURL)}
			watermarkPostID = strings.TrimSpace(postID)
		}
	}

	// 直调路径（/sora/v1/chat/completions）保持纯透传，不执行本地/S3 媒体落盘。
	// 媒体存储由客户端 API 路径（/api/v1/sora/generate）的异步流程负责。
	finalURLs := s.normalizeSoraMediaURLs(mediaURLs)
	if watermarkPostID != "" && watermarkOpts.DeletePost {
		if deleteErr := s.soraClient.DeletePost(reqCtx, account, watermarkPostID); deleteErr != nil {
			log.Printf("[Sora] delete post failed, post_id=%s err=%v", watermarkPostID, deleteErr)
		}
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

func parseSoraWatermarkOptions(body map[string]any) soraWatermarkOptions {
	opts := soraWatermarkOptions{
		Enabled:           parseBoolWithDefault(body, "watermark_free", false),
		ParseMethod:       strings.ToLower(strings.TrimSpace(parseStringWithDefault(body, "watermark_parse_method", "third_party"))),
		ParseURL:          strings.TrimSpace(parseStringWithDefault(body, "watermark_parse_url", "")),
		ParseToken:        strings.TrimSpace(parseStringWithDefault(body, "watermark_parse_token", "")),
		FallbackOnFailure: parseBoolWithDefault(body, "watermark_fallback_on_failure", true),
		DeletePost:        parseBoolWithDefault(body, "watermark_delete_post", false),
	}
	if opts.ParseMethod == "" {
		opts.ParseMethod = "third_party"
	}
	return opts
}

func parseSoraCharacterOptions(body map[string]any) soraCharacterOptions {
	return soraCharacterOptions{
		SetPublic:           parseBoolWithDefault(body, "character_set_public", true),
		DeleteAfterGenerate: parseBoolWithDefault(body, "character_delete_after_generate", true),
	}
}

func parseSoraVideoCount(body map[string]any) int {
	if body == nil {
		return 1
	}
	keys := []string{"video_count", "videos", "n_variants"}
	for _, key := range keys {
		count := parseIntWithDefault(body, key, 0)
		if count > 0 {
			return clampInt(count, 1, 3)
		}
	}
	return 1
}

func parseBoolWithDefault(body map[string]any, key string, def bool) bool {
	if body == nil {
		return def
	}
	val, ok := body[key]
	if !ok {
		return def
	}
	switch typed := val.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int32:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		typed = strings.ToLower(strings.TrimSpace(typed))
		if typed == "true" || typed == "1" || typed == "yes" {
			return true
		}
		if typed == "false" || typed == "0" || typed == "no" {
			return false
		}
	}
	return def
}

func parseStringWithDefault(body map[string]any, key, def string) string {
	if body == nil {
		return def
	}
	val, ok := body[key]
	if !ok {
		return def
	}
	if str, ok := val.(string); ok {
		return str
	}
	return def
}

func parseIntWithDefault(body map[string]any, key string, def int) int {
	if body == nil {
		return def
	}
	val, ok := body[key]
	if !ok {
		return def
	}
	switch typed := val.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return def
}

func clampInt(v, minVal, maxVal int) int {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

func extractSoraCameoIDs(body map[string]any) []string {
	if body == nil {
		return nil
	}
	raw, ok := body["cameo_ids"]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			str, ok := item.(string)
			if !ok {
				continue
			}
			str = strings.TrimSpace(str)
			if str != "" {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func (s *SoraGatewayService) createCharacterFromVideo(ctx context.Context, account *Account, videoData []byte, opts soraCharacterOptions) (*soraCharacterFlowResult, error) {
	cameoID, err := s.soraClient.UploadCharacterVideo(ctx, account, videoData)
	if err != nil {
		return nil, err
	}

	cameoStatus, err := s.pollCameoStatus(ctx, account, cameoID)
	if err != nil {
		return nil, err
	}
	username := processSoraCharacterUsername(cameoStatus.UsernameHint)
	displayName := strings.TrimSpace(cameoStatus.DisplayNameHint)
	if displayName == "" {
		displayName = "Character"
	}
	profileAssetURL := strings.TrimSpace(cameoStatus.ProfileAssetURL)
	if profileAssetURL == "" {
		return nil, errors.New("profile asset url not found in cameo status")
	}

	avatarData, err := s.soraClient.DownloadCharacterImage(ctx, account, profileAssetURL)
	if err != nil {
		return nil, err
	}
	assetPointer, err := s.soraClient.UploadCharacterImage(ctx, account, avatarData)
	if err != nil {
		return nil, err
	}
	instructionSet := cameoStatus.InstructionSetHint
	if instructionSet == nil {
		instructionSet = cameoStatus.InstructionSet
	}

	characterID, err := s.soraClient.FinalizeCharacter(ctx, account, SoraCharacterFinalizeRequest{
		CameoID:             strings.TrimSpace(cameoID),
		Username:            username,
		DisplayName:         displayName,
		ProfileAssetPointer: assetPointer,
		InstructionSet:      instructionSet,
	})
	if err != nil {
		return nil, err
	}

	if opts.SetPublic {
		if err := s.soraClient.SetCharacterPublic(ctx, account, cameoID); err != nil {
			return nil, err
		}
	}

	return &soraCharacterFlowResult{
		CameoID:     strings.TrimSpace(cameoID),
		CharacterID: strings.TrimSpace(characterID),
		Username:    strings.TrimSpace(username),
		DisplayName: displayName,
	}, nil
}

func (s *SoraGatewayService) pollCameoStatus(ctx context.Context, account *Account, cameoID string) (*SoraCameoStatus, error) {
	timeout := 10 * time.Minute
	interval := 5 * time.Second
	maxAttempts := int(math.Ceil(timeout.Seconds() / interval.Seconds()))
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	consecutiveErrors := 0
	for attempt := 0; attempt < maxAttempts; attempt++ {
		status, err := s.soraClient.GetCameoStatus(ctx, account, cameoID)
		if err != nil {
			lastErr = err
			consecutiveErrors++
			if consecutiveErrors >= 3 {
				break
			}
			if attempt < maxAttempts-1 {
				if sleepErr := sleepWithContext(ctx, interval); sleepErr != nil {
					return nil, sleepErr
				}
			}
			continue
		}
		consecutiveErrors = 0
		if status == nil {
			if attempt < maxAttempts-1 {
				if sleepErr := sleepWithContext(ctx, interval); sleepErr != nil {
					return nil, sleepErr
				}
			}
			continue
		}
		currentStatus := strings.ToLower(strings.TrimSpace(status.Status))
		statusMessage := strings.TrimSpace(status.StatusMessage)
		if currentStatus == "failed" {
			if statusMessage == "" {
				statusMessage = "character creation failed"
			}
			return nil, errors.New(statusMessage)
		}
		if strings.EqualFold(statusMessage, "Completed") || currentStatus == "finalized" {
			return status, nil
		}
		if attempt < maxAttempts-1 {
			if sleepErr := sleepWithContext(ctx, interval); sleepErr != nil {
				return nil, sleepErr
			}
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("poll cameo status failed: %w", lastErr)
	}
	return nil, errors.New("cameo processing timeout")
}

func processSoraCharacterUsername(usernameHint string) string {
	usernameHint = strings.TrimSpace(usernameHint)
	if usernameHint == "" {
		usernameHint = "character"
	}
	if strings.Contains(usernameHint, ".") {
		parts := strings.Split(usernameHint, ".")
		usernameHint = strings.TrimSpace(parts[len(parts)-1])
	}
	if usernameHint == "" {
		usernameHint = "character"
	}
	return fmt.Sprintf("%s%d", usernameHint, rand.Intn(900)+100)
}

func (s *SoraGatewayService) resolveWatermarkFreeURL(ctx context.Context, account *Account, generationID string, opts soraWatermarkOptions) (string, string, error) {
	generationID = strings.TrimSpace(generationID)
	if generationID == "" {
		return "", "", errors.New("generation id is required for watermark-free mode")
	}
	postID, err := s.soraClient.PostVideoForWatermarkFree(ctx, account, generationID)
	if err != nil {
		return "", "", err
	}
	postID = strings.TrimSpace(postID)
	if postID == "" {
		return "", "", errors.New("watermark-free publish returned empty post id")
	}

	switch opts.ParseMethod {
	case "custom":
		urlVal, parseErr := s.soraClient.GetWatermarkFreeURLCustom(ctx, account, opts.ParseURL, opts.ParseToken, postID)
		if parseErr != nil {
			return "", postID, parseErr
		}
		return strings.TrimSpace(urlVal), postID, nil
	case "", "third_party":
		return fmt.Sprintf("https://oscdn2.dyysy.com/MP4/%s.mp4", postID), postID, nil
	default:
		return "", postID, fmt.Errorf("unsupported watermark parse method: %s", opts.ParseMethod)
	}
}

func (s *SoraGatewayService) shouldFailoverUpstreamError(statusCode int) bool {
	switch statusCode {
	case 401, 402, 403, 404, 429, 529:
		return true
	default:
		return statusCode >= 500
	}
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
	encoded, _ := jsonMarshalRaw(chunk)
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
	finalEncoded, _ := jsonMarshalRaw(finalChunk)
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
		errorData := map[string]any{
			"error": map[string]string{
				"type":    errType,
				"message": message,
			},
		}
		jsonBytes, err := json.Marshal(errorData)
		if err != nil {
			_ = c.Error(err)
			return
		}
		errorEvent := fmt.Sprintf("event: error\ndata: %s\n\n", string(jsonBytes))
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
		accountID := int64(0)
		if account != nil {
			accountID = account.ID
		}
		logger.LegacyPrintf(
			"service.sora",
			"[SoraRawError] account_id=%d model=%s status=%d request_id=%s cf_ray=%s message=%s raw_body=%s",
			accountID,
			model,
			upstreamErr.StatusCode,
			strings.TrimSpace(upstreamErr.Headers.Get("x-request-id")),
			strings.TrimSpace(upstreamErr.Headers.Get("cf-ray")),
			strings.TrimSpace(upstreamErr.Message),
			truncateForLog(upstreamErr.Body, 1024),
		)
		if s.rateLimitService != nil && account != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, upstreamErr.StatusCode, upstreamErr.Headers, upstreamErr.Body)
		}
		if s.shouldFailoverUpstreamError(upstreamErr.StatusCode) {
			var responseHeaders http.Header
			if upstreamErr.Headers != nil {
				responseHeaders = upstreamErr.Headers.Clone()
			}
			return &UpstreamFailoverError{
				StatusCode:      upstreamErr.StatusCode,
				ResponseBody:    upstreamErr.Body,
				ResponseHeaders: responseHeaders,
			}
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
			return nil, errors.New("sora image generation failed")
		}
		if stream {
			s.maybeSendPing(c, &lastPing)
		}
		if err := sleepWithContext(ctx, interval); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("sora image generation timeout")
}

func (s *SoraGatewayService) pollVideoTaskDetailed(ctx context.Context, c *gin.Context, account *Account, taskID string, stream bool) (*SoraVideoTaskStatus, error) {
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
			return status, nil
		case "failed":
			if status.ErrorMsg != "" {
				return nil, errors.New(status.ErrorMsg)
			}
			return nil, errors.New("sora video generation failed")
		}
		if stream {
			s.maybeSendPing(c, &lastPing)
		}
		if err := sleepWithContext(ctx, interval); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("sora video generation timeout")
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

// jsonMarshalRaw 序列化 JSON，不转义 &、<、> 等 HTML 字符，
// 避免 URL 中的 & 被转义为 \u0026 导致客户端无法直接使用。
func jsonMarshalRaw(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encode 会追加换行符，去掉它
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b, nil
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
		remixTargetID = strings.TrimSpace(v)
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
					_, _ = builder.WriteString("\n")
				}
				_, _ = builder.WriteString(text)
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
	if remixTargetID == "" {
		remixTargetID = extractRemixTargetIDFromPrompt(prompt)
	}
	prompt = cleanRemixLinkFromPrompt(prompt)
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
						_, _ = builder.WriteString("\n")
					}
					_, _ = builder.WriteString(txt)
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

func isSoraStoryboardPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	return len(soraStoryboardPattern.FindAllString(prompt, -1)) >= 1
}

func formatSoraStoryboardPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	matches := soraStoryboardShotPattern.FindAllStringSubmatch(prompt, -1)
	if len(matches) == 0 {
		return prompt
	}
	firstBracketPos := strings.Index(prompt, "[")
	instructions := ""
	if firstBracketPos > 0 {
		instructions = strings.TrimSpace(prompt[:firstBracketPos])
	}
	shots := make([]string, 0, len(matches))
	for i, match := range matches {
		if len(match) < 3 {
			continue
		}
		duration := strings.TrimSpace(match[1])
		scene := strings.TrimSpace(match[2])
		if scene == "" {
			continue
		}
		shots = append(shots, fmt.Sprintf("Shot %d:\nduration: %ssec\nScene: %s", i+1, duration, scene))
	}
	if len(shots) == 0 {
		return prompt
	}
	timeline := strings.Join(shots, "\n\n")
	if instructions == "" {
		return timeline
	}
	return fmt.Sprintf("current timeline:\n%s\n\ninstructions:\n%s", timeline, instructions)
}

func extractRemixTargetIDFromPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	return strings.TrimSpace(soraRemixTargetPattern.FindString(prompt))
}

func cleanRemixLinkFromPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return prompt
	}
	cleaned := soraRemixTargetInURLPattern.ReplaceAllString(prompt, "")
	cleaned = soraRemixTargetPattern.ReplaceAllString(cleaned, "")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	return strings.TrimSpace(cleaned)
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
		decoded, err := decodeBase64WithLimit(payload, soraImageInputMaxBytes)
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
	decoded, err := decodeBase64WithLimit(raw, soraImageInputMaxBytes)
	if err != nil {
		return nil, "", errors.New("invalid base64 image")
	}
	return decoded, "image.png", nil
}

func decodeSoraVideoInput(ctx context.Context, input string) ([]byte, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return nil, errors.New("empty video input")
	}
	if strings.HasPrefix(raw, "data:") {
		parts := strings.SplitN(raw, ",", 2)
		if len(parts) != 2 {
			return nil, errors.New("invalid video data url")
		}
		decoded, err := decodeBase64WithLimit(parts[1], soraVideoInputMaxBytes)
		if err != nil {
			return nil, errors.New("invalid base64 video")
		}
		if len(decoded) == 0 {
			return nil, errors.New("empty video data")
		}
		return decoded, nil
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return downloadSoraVideoInput(ctx, raw)
	}
	decoded, err := decodeBase64WithLimit(raw, soraVideoInputMaxBytes)
	if err != nil {
		return nil, errors.New("invalid base64 video")
	}
	if len(decoded) == 0 {
		return nil, errors.New("empty video data")
	}
	return decoded, nil
}

func downloadSoraImageInput(ctx context.Context, rawURL string) ([]byte, string, error) {
	parsed, err := validateSoraRemoteURL(rawURL)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{
		Timeout: soraImageInputTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= soraImageInputMaxRedirects {
				return errors.New("too many redirects")
			}
			return validateSoraRemoteURLValue(req.URL)
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download image failed: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, soraImageInputMaxBytes))
	if err != nil {
		return nil, "", err
	}
	ext := fileExtFromURL(parsed.String())
	if ext == "" {
		ext = fileExtFromContentType(resp.Header.Get("Content-Type"))
	}
	filename := "image" + ext
	return data, filename, nil
}

func downloadSoraVideoInput(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := validateSoraRemoteURL(rawURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: soraVideoInputTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= soraVideoInputMaxRedirects {
				return errors.New("too many redirects")
			}
			return validateSoraRemoteURLValue(req.URL)
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download video failed: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, soraVideoInputMaxBytes))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("empty video content")
	}
	return data, nil
}

func decodeBase64WithLimit(encoded string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errors.New("invalid max bytes limit")
	}
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))
	limited := io.LimitReader(decoder, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("input exceeds %d bytes limit", maxBytes)
	}
	return data, nil
}

func validateSoraRemoteURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("empty remote url")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid remote url: %w", err)
	}
	if err := validateSoraRemoteURLValue(parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func validateSoraRemoteURLValue(parsed *url.URL) error {
	if parsed == nil {
		return errors.New("invalid remote url")
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return errors.New("only http/https remote url is allowed")
	}
	if parsed.User != nil {
		return errors.New("remote url cannot contain userinfo")
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return errors.New("remote url missing host")
	}
	if _, blocked := soraBlockedHostnames[host]; blocked {
		return errors.New("remote url is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isSoraBlockedIP(ip) {
			return errors.New("remote url is not allowed")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve remote url failed: %w", err)
	}
	for _, ip := range ips {
		if isSoraBlockedIP(ip) {
			return errors.New("remote url is not allowed")
		}
	}
	return nil
}

func isSoraBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	for _, cidr := range soraBlockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func mustParseCIDRs(values []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(values))
	for _, val := range values {
		_, cidr, err := net.ParseCIDR(val)
		if err != nil {
			continue
		}
		out = append(out, cidr)
	}
	return out
}
