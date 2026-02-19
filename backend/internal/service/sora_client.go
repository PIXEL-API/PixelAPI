package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"math/rand"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	openaioauth "github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/Wei-Shaw/sub2api/internal/util/soraerror"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"golang.org/x/crypto/sha3"
)

const (
	soraChatGPTBaseURL   = "https://chatgpt.com"
	soraSentinelFlow     = "sora_2_create_task"
	soraDefaultUserAgent = "Sora/1.2026.007 (Android 15; 24122RKC7C; build 2600700)"
)

var (
	soraSessionAuthURL = "https://sora.chatgpt.com/api/auth/session"
	soraOAuthTokenURL  = "https://auth.openai.com/oauth/token"
)

const (
	soraPowMaxIteration = 500000
)

var soraPowCores = []int{8, 16, 24, 32}

var soraPowScripts = []string{
	"https://cdn.oaistatic.com/_next/static/cXh69klOLzS0Gy2joLDRS/_ssgManifest.js?dpl=453ebaec0d44c2decab71692e1bfe39be35a24b3",
}

var soraPowDPL = []string{
	"prod-f501fe933b3edf57aea882da888e1a544df99840",
}

var soraPowNavigatorKeys = []string{
	"registerProtocolHandler−function registerProtocolHandler() { [native code] }",
	"storage−[object StorageManager]",
	"locks−[object LockManager]",
	"appCodeName−Mozilla",
	"permissions−[object Permissions]",
	"webdriver−false",
	"vendor−Google Inc.",
	"mediaDevices−[object MediaDevices]",
	"cookieEnabled−true",
	"product−Gecko",
	"productSub−20030107",
	"hardwareConcurrency−32",
	"onLine−true",
}

var soraPowDocumentKeys = []string{
	"_reactListeningo743lnnpvdg",
	"location",
}

var soraPowWindowKeys = []string{
	"0", "window", "self", "document", "name", "location",
	"navigator", "screen", "innerWidth", "innerHeight",
	"localStorage", "sessionStorage", "crypto", "performance",
	"fetch", "setTimeout", "setInterval", "console",
}

var soraDesktopUserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 11.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
}

var soraMobileUserAgents = []string{
	"Sora/1.2026.007 (Android 15; 24122RKC7C; build 2600700)",
	"Sora/1.2026.007 (Android 14; SM-G998B; build 2600700)",
	"Sora/1.2026.007 (Android 15; Pixel 8 Pro; build 2600700)",
	"Sora/1.2026.007 (Android 14; Pixel 7; build 2600700)",
	"Sora/1.2026.007 (Android 15; 2211133C; build 2600700)",
	"Sora/1.2026.007 (Android 14; SM-S918B; build 2600700)",
	"Sora/1.2026.007 (Android 15; OnePlus 12; build 2600700)",
}

var soraRand = rand.New(rand.NewSource(time.Now().UnixNano()))
var soraRandMu sync.Mutex
var soraPerfStart = time.Now()
var soraPowTokenGenerator = soraGetPowToken

// SoraClient 定义直连 Sora 的任务操作接口。
type SoraClient interface {
	Enabled() bool
	UploadImage(ctx context.Context, account *Account, data []byte, filename string) (string, error)
	CreateImageTask(ctx context.Context, account *Account, req SoraImageRequest) (string, error)
	CreateVideoTask(ctx context.Context, account *Account, req SoraVideoRequest) (string, error)
	CreateStoryboardTask(ctx context.Context, account *Account, req SoraStoryboardRequest) (string, error)
	UploadCharacterVideo(ctx context.Context, account *Account, data []byte) (string, error)
	GetCameoStatus(ctx context.Context, account *Account, cameoID string) (*SoraCameoStatus, error)
	DownloadCharacterImage(ctx context.Context, account *Account, imageURL string) ([]byte, error)
	UploadCharacterImage(ctx context.Context, account *Account, data []byte) (string, error)
	FinalizeCharacter(ctx context.Context, account *Account, req SoraCharacterFinalizeRequest) (string, error)
	SetCharacterPublic(ctx context.Context, account *Account, cameoID string) error
	DeleteCharacter(ctx context.Context, account *Account, characterID string) error
	PostVideoForWatermarkFree(ctx context.Context, account *Account, generationID string) (string, error)
	DeletePost(ctx context.Context, account *Account, postID string) error
	GetWatermarkFreeURLCustom(ctx context.Context, account *Account, parseURL, parseToken, postID string) (string, error)
	EnhancePrompt(ctx context.Context, account *Account, prompt, expansionLevel string, durationS int) (string, error)
	GetImageTask(ctx context.Context, account *Account, taskID string) (*SoraImageTaskStatus, error)
	GetVideoTask(ctx context.Context, account *Account, taskID string) (*SoraVideoTaskStatus, error)
}

// SoraImageRequest 图片生成请求参数
type SoraImageRequest struct {
	Prompt  string
	Width   int
	Height  int
	MediaID string
}

// SoraVideoRequest 视频生成请求参数
type SoraVideoRequest struct {
	Prompt        string
	Orientation   string
	Frames        int
	Model         string
	Size          string
	MediaID       string
	RemixTargetID string
	CameoIDs      []string
}

// SoraStoryboardRequest 分镜视频生成请求参数
type SoraStoryboardRequest struct {
	Prompt      string
	Orientation string
	Frames      int
	Model       string
	Size        string
	MediaID     string
}

// SoraImageTaskStatus 图片任务状态
type SoraImageTaskStatus struct {
	ID          string
	Status      string
	ProgressPct float64
	URLs        []string
	ErrorMsg    string
}

// SoraVideoTaskStatus 视频任务状态
type SoraVideoTaskStatus struct {
	ID           string
	Status       string
	ProgressPct  int
	URLs         []string
	GenerationID string
	ErrorMsg     string
}

// SoraCameoStatus 角色处理中间态
type SoraCameoStatus struct {
	Status             string
	StatusMessage      string
	DisplayNameHint    string
	UsernameHint       string
	ProfileAssetURL    string
	InstructionSetHint any
	InstructionSet     any
}

// SoraCharacterFinalizeRequest 角色定稿请求参数
type SoraCharacterFinalizeRequest struct {
	CameoID             string
	Username            string
	DisplayName         string
	ProfileAssetPointer string
	InstructionSet      any
}

// SoraUpstreamError 上游错误
type SoraUpstreamError struct {
	StatusCode int
	Message    string
	Headers    http.Header
	Body       []byte
}

func (e *SoraUpstreamError) Error() string {
	if e == nil {
		return "sora upstream error"
	}
	if e.Message != "" {
		return fmt.Sprintf("sora upstream error: %d %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("sora upstream error: %d", e.StatusCode)
}

// SoraDirectClient 直连 Sora 实现
type SoraDirectClient struct {
	cfg                 *config.Config
	httpUpstream        HTTPUpstream
	tokenProvider       *OpenAITokenProvider
	accountRepo         AccountRepository
	soraAccountRepo     SoraAccountRepository
	baseURL             string
	challengeCooldownMu sync.RWMutex
	challengeCooldowns  map[string]soraChallengeCooldownEntry
	sidecarSessionMu    sync.RWMutex
	sidecarSessions     map[string]soraSidecarSessionEntry
}

// NewSoraDirectClient 创建 Sora 直连客户端
func NewSoraDirectClient(cfg *config.Config, httpUpstream HTTPUpstream, tokenProvider *OpenAITokenProvider) *SoraDirectClient {
	baseURL := ""
	if cfg != nil {
		rawBaseURL := strings.TrimRight(strings.TrimSpace(cfg.Sora.Client.BaseURL), "/")
		baseURL = normalizeSoraBaseURL(rawBaseURL)
		if rawBaseURL != "" && baseURL != rawBaseURL {
			log.Printf("[SoraClient] normalized base_url from %s to %s", sanitizeSoraLogURL(rawBaseURL), sanitizeSoraLogURL(baseURL))
		}
	}
	return &SoraDirectClient{
		cfg:                cfg,
		httpUpstream:       httpUpstream,
		tokenProvider:      tokenProvider,
		baseURL:            baseURL,
		challengeCooldowns: make(map[string]soraChallengeCooldownEntry),
		sidecarSessions:    make(map[string]soraSidecarSessionEntry),
	}
}

func (c *SoraDirectClient) SetAccountRepositories(accountRepo AccountRepository, soraAccountRepo SoraAccountRepository) {
	if c == nil {
		return
	}
	c.accountRepo = accountRepo
	c.soraAccountRepo = soraAccountRepo
}

// Enabled 判断是否启用 Sora 直连
func (c *SoraDirectClient) Enabled() bool {
	if c == nil {
		return false
	}
	if strings.TrimSpace(c.baseURL) != "" {
		return true
	}
	if c.cfg == nil {
		return false
	}
	return strings.TrimSpace(normalizeSoraBaseURL(c.cfg.Sora.Client.BaseURL)) != ""
}

// PreflightCheck 在创建任务前执行账号能力预检。
// 当前仅对视频模型执行 /nf/check 预检，用于提前识别额度耗尽或能力缺失。
func (c *SoraDirectClient) PreflightCheck(ctx context.Context, account *Account, requestedModel string, modelCfg SoraModelConfig) error {
	if modelCfg.Type != "video" {
		return nil
	}
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Accept", "application/json")
	body, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodGet, c.buildURL("/nf/check"), headers, nil, false)
	if err != nil {
		var upstreamErr *SoraUpstreamError
		if errors.As(err, &upstreamErr) && upstreamErr.StatusCode == http.StatusNotFound {
			return &SoraUpstreamError{
				StatusCode: http.StatusForbidden,
				Message:    "当前账号未开通 Sora2 能力或无可用配额",
				Headers:    upstreamErr.Headers,
				Body:       upstreamErr.Body,
			}
		}
		return err
	}

	rateLimitReached := gjson.GetBytes(body, "rate_limit_and_credit_balance.rate_limit_reached").Bool()
	remaining := gjson.GetBytes(body, "rate_limit_and_credit_balance.estimated_num_videos_remaining")
	if rateLimitReached || (remaining.Exists() && remaining.Int() <= 0) {
		msg := "当前账号 Sora2 可用配额不足"
		if requestedModel != "" {
			msg = fmt.Sprintf("当前账号 %s 可用配额不足", requestedModel)
		}
		return &SoraUpstreamError{
			StatusCode: http.StatusTooManyRequests,
			Message:    msg,
			Headers:    http.Header{},
		}
	}
	return nil
}

func (c *SoraDirectClient) UploadImage(ctx context.Context, account *Account, data []byte, filename string) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty image data")
	}
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return "", err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	if filename == "" {
		filename = "image.png"
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	contentType := mime.TypeByExtension(path.Ext(filename))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	partHeader.Set("Content-Type", contentType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.WriteField("file_name", filename); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Content-Type", writer.FormDataContentType())

	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodPost, c.buildURL("/uploads"), headers, &body, false)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(gjson.GetBytes(respBody, "id").String())
	if id == "" {
		return "", errors.New("upload response missing id")
	}
	return id, nil
}

func (c *SoraDirectClient) CreateImageTask(ctx context.Context, account *Account, req SoraImageRequest) (string, error) {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return "", err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	operation := "simple_compose"
	inpaintItems := []map[string]any{}
	if strings.TrimSpace(req.MediaID) != "" {
		operation = "remix"
		inpaintItems = append(inpaintItems, map[string]any{
			"type":            "image",
			"frame_index":     0,
			"upload_media_id": req.MediaID,
		})
	}
	payload := map[string]any{
		"type":          "image_gen",
		"operation":     operation,
		"prompt":        req.Prompt,
		"width":         req.Width,
		"height":        req.Height,
		"n_variants":    1,
		"n_frames":      1,
		"inpaint_items": inpaintItems,
	}
	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Content-Type", "application/json")
	headers.Set("Origin", "https://sora.chatgpt.com")
	headers.Set("Referer", "https://sora.chatgpt.com/")

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sentinel, err := c.generateSentinelToken(ctx, account, token, userAgent, proxyURL)
	if err != nil {
		return "", err
	}
	headers.Set("openai-sentinel-token", sentinel)

	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodPost, c.buildURL("/video_gen"), headers, bytes.NewReader(body), true)
	if err != nil {
		return "", err
	}
	taskID := strings.TrimSpace(gjson.GetBytes(respBody, "id").String())
	if taskID == "" {
		return "", errors.New("image task response missing id")
	}
	return taskID, nil
}

func (c *SoraDirectClient) CreateVideoTask(ctx context.Context, account *Account, req SoraVideoRequest) (string, error) {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return "", err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	orientation := req.Orientation
	if orientation == "" {
		orientation = "landscape"
	}
	nFrames := req.Frames
	if nFrames <= 0 {
		nFrames = 450
	}
	model := req.Model
	if model == "" {
		model = "sy_8"
	}
	size := req.Size
	if size == "" {
		size = "small"
	}

	inpaintItems := []map[string]any{}
	if strings.TrimSpace(req.MediaID) != "" {
		inpaintItems = append(inpaintItems, map[string]any{
			"kind":      "upload",
			"upload_id": req.MediaID,
		})
	}
	payload := map[string]any{
		"kind":          "video",
		"prompt":        req.Prompt,
		"orientation":   orientation,
		"size":          size,
		"n_frames":      nFrames,
		"model":         model,
		"inpaint_items": inpaintItems,
	}
	if strings.TrimSpace(req.RemixTargetID) != "" {
		payload["remix_target_id"] = req.RemixTargetID
		payload["cameo_ids"] = []string{}
		payload["cameo_replacements"] = map[string]any{}
	} else if len(req.CameoIDs) > 0 {
		payload["cameo_ids"] = req.CameoIDs
		payload["cameo_replacements"] = map[string]any{}
	}

	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Content-Type", "application/json")
	headers.Set("Origin", "https://sora.chatgpt.com")
	headers.Set("Referer", "https://sora.chatgpt.com/")
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sentinel, err := c.generateSentinelToken(ctx, account, token, userAgent, proxyURL)
	if err != nil {
		return "", err
	}
	headers.Set("openai-sentinel-token", sentinel)

	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodPost, c.buildURL("/nf/create"), headers, bytes.NewReader(body), true)
	if err != nil {
		return "", err
	}
	taskID := strings.TrimSpace(gjson.GetBytes(respBody, "id").String())
	if taskID == "" {
		return "", errors.New("video task response missing id")
	}
	return taskID, nil
}

func (c *SoraDirectClient) CreateStoryboardTask(ctx context.Context, account *Account, req SoraStoryboardRequest) (string, error) {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return "", err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	orientation := req.Orientation
	if orientation == "" {
		orientation = "landscape"
	}
	nFrames := req.Frames
	if nFrames <= 0 {
		nFrames = 450
	}
	model := req.Model
	if model == "" {
		model = "sy_8"
	}
	size := req.Size
	if size == "" {
		size = "small"
	}

	inpaintItems := []map[string]any{}
	if strings.TrimSpace(req.MediaID) != "" {
		inpaintItems = append(inpaintItems, map[string]any{
			"kind":      "upload",
			"upload_id": req.MediaID,
		})
	}
	payload := map[string]any{
		"kind":               "video",
		"prompt":             req.Prompt,
		"title":              "Draft your video",
		"orientation":        orientation,
		"size":               size,
		"n_frames":           nFrames,
		"storyboard_id":      nil,
		"inpaint_items":      inpaintItems,
		"remix_target_id":    nil,
		"model":              model,
		"metadata":           nil,
		"style_id":           nil,
		"cameo_ids":          nil,
		"cameo_replacements": nil,
		"audio_caption":      nil,
		"audio_transcript":   nil,
		"video_caption":      nil,
	}

	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Content-Type", "application/json")
	headers.Set("Origin", "https://sora.chatgpt.com")
	headers.Set("Referer", "https://sora.chatgpt.com/")
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sentinel, err := c.generateSentinelToken(ctx, account, token, userAgent, proxyURL)
	if err != nil {
		return "", err
	}
	headers.Set("openai-sentinel-token", sentinel)

	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodPost, c.buildURL("/nf/create/storyboard"), headers, bytes.NewReader(body), true)
	if err != nil {
		return "", err
	}
	taskID := strings.TrimSpace(gjson.GetBytes(respBody, "id").String())
	if taskID == "" {
		return "", errors.New("storyboard task response missing id")
	}
	return taskID, nil
}

func (c *SoraDirectClient) UploadCharacterVideo(ctx context.Context, account *Account, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty video data")
	}
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return "", err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", `form-data; name="file"; filename="video.mp4"`)
	partHeader.Set("Content-Type", "video/mp4")
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.WriteField("timestamps", "0,3"); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Content-Type", writer.FormDataContentType())
	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodPost, c.buildURL("/characters/upload"), headers, &body, false)
	if err != nil {
		return "", err
	}
	cameoID := strings.TrimSpace(gjson.GetBytes(respBody, "id").String())
	if cameoID == "" {
		return "", errors.New("character upload response missing id")
	}
	return cameoID, nil
}

func (c *SoraDirectClient) GetCameoStatus(ctx context.Context, account *Account, cameoID string) (*SoraCameoStatus, error) {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	headers := c.buildBaseHeaders(token, userAgent)
	respBody, _, err := c.doRequestWithProxy(
		ctx,
		account,
		proxyURL,
		http.MethodGet,
		c.buildURL("/project_y/cameos/in_progress/"+strings.TrimSpace(cameoID)),
		headers,
		nil,
		false,
	)
	if err != nil {
		return nil, err
	}
	return &SoraCameoStatus{
		Status:             strings.TrimSpace(gjson.GetBytes(respBody, "status").String()),
		StatusMessage:      strings.TrimSpace(gjson.GetBytes(respBody, "status_message").String()),
		DisplayNameHint:    strings.TrimSpace(gjson.GetBytes(respBody, "display_name_hint").String()),
		UsernameHint:       strings.TrimSpace(gjson.GetBytes(respBody, "username_hint").String()),
		ProfileAssetURL:    strings.TrimSpace(gjson.GetBytes(respBody, "profile_asset_url").String()),
		InstructionSetHint: gjson.GetBytes(respBody, "instruction_set_hint").Value(),
		InstructionSet:     gjson.GetBytes(respBody, "instruction_set").Value(),
	}, nil
}

func (c *SoraDirectClient) DownloadCharacterImage(ctx context.Context, account *Account, imageURL string) ([]byte, error) {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Accept", "image/*,*/*;q=0.8")

	respBody, _, err := c.doRequestWithProxy(
		ctx,
		account,
		proxyURL,
		http.MethodGet,
		strings.TrimSpace(imageURL),
		headers,
		nil,
		false,
	)
	if err != nil {
		return nil, err
	}
	return respBody, nil
}

func (c *SoraDirectClient) UploadCharacterImage(ctx context.Context, account *Account, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty character image")
	}
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return "", err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", `form-data; name="file"; filename="profile.webp"`)
	partHeader.Set("Content-Type", "image/webp")
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.WriteField("use_case", "profile"); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Content-Type", writer.FormDataContentType())
	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodPost, c.buildURL("/project_y/file/upload"), headers, &body, false)
	if err != nil {
		return "", err
	}
	assetPointer := strings.TrimSpace(gjson.GetBytes(respBody, "asset_pointer").String())
	if assetPointer == "" {
		return "", errors.New("character image upload response missing asset_pointer")
	}
	return assetPointer, nil
}

func (c *SoraDirectClient) FinalizeCharacter(ctx context.Context, account *Account, req SoraCharacterFinalizeRequest) (string, error) {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return "", err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	payload := map[string]any{
		"cameo_id":               req.CameoID,
		"username":               req.Username,
		"display_name":           req.DisplayName,
		"profile_asset_pointer":  req.ProfileAssetPointer,
		"instruction_set":        nil,
		"safety_instruction_set": nil,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Content-Type", "application/json")
	headers.Set("Origin", "https://sora.chatgpt.com")
	headers.Set("Referer", "https://sora.chatgpt.com/")
	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodPost, c.buildURL("/characters/finalize"), headers, bytes.NewReader(body), false)
	if err != nil {
		return "", err
	}
	characterID := strings.TrimSpace(gjson.GetBytes(respBody, "character.character_id").String())
	if characterID == "" {
		return "", errors.New("character finalize response missing character_id")
	}
	return characterID, nil
}

func (c *SoraDirectClient) SetCharacterPublic(ctx context.Context, account *Account, cameoID string) error {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	payload := map[string]any{"visibility": "public"}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Content-Type", "application/json")
	headers.Set("Origin", "https://sora.chatgpt.com")
	headers.Set("Referer", "https://sora.chatgpt.com/")
	_, _, err = c.doRequestWithProxy(
		ctx,
		account,
		proxyURL,
		http.MethodPost,
		c.buildURL("/project_y/cameos/by_id/"+strings.TrimSpace(cameoID)+"/update_v2"),
		headers,
		bytes.NewReader(body),
		false,
	)
	return err
}

func (c *SoraDirectClient) DeleteCharacter(ctx context.Context, account *Account, characterID string) error {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	headers := c.buildBaseHeaders(token, userAgent)
	_, _, err = c.doRequestWithProxy(
		ctx,
		account,
		proxyURL,
		http.MethodDelete,
		c.buildURL("/project_y/characters/"+strings.TrimSpace(characterID)),
		headers,
		nil,
		false,
	)
	return err
}

func (c *SoraDirectClient) PostVideoForWatermarkFree(ctx context.Context, account *Account, generationID string) (string, error) {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return "", err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	payload := map[string]any{
		"attachments_to_create": []map[string]any{
			{
				"generation_id": generationID,
				"kind":          "sora",
			},
		},
		"post_text": "",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Content-Type", "application/json")
	headers.Set("Origin", "https://sora.chatgpt.com")
	headers.Set("Referer", "https://sora.chatgpt.com/")
	sentinel, err := c.generateSentinelToken(ctx, account, token, userAgent, proxyURL)
	if err != nil {
		return "", err
	}
	headers.Set("openai-sentinel-token", sentinel)
	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodPost, c.buildURL("/project_y/post"), headers, bytes.NewReader(body), true)
	if err != nil {
		return "", err
	}
	postID := strings.TrimSpace(gjson.GetBytes(respBody, "post.id").String())
	if postID == "" {
		return "", errors.New("watermark-free publish response missing post.id")
	}
	return postID, nil
}

func (c *SoraDirectClient) DeletePost(ctx context.Context, account *Account, postID string) error {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	headers := c.buildBaseHeaders(token, userAgent)
	_, _, err = c.doRequestWithProxy(
		ctx,
		account,
		proxyURL,
		http.MethodDelete,
		c.buildURL("/project_y/post/"+strings.TrimSpace(postID)),
		headers,
		nil,
		false,
	)
	return err
}

func (c *SoraDirectClient) GetWatermarkFreeURLCustom(ctx context.Context, account *Account, parseURL, parseToken, postID string) (string, error) {
	parseURL = strings.TrimRight(strings.TrimSpace(parseURL), "/")
	if parseURL == "" {
		return "", errors.New("custom parse url is required")
	}
	if strings.TrimSpace(parseToken) == "" {
		return "", errors.New("custom parse token is required")
	}
	shareURL := "https://sora.chatgpt.com/p/" + strings.TrimSpace(postID)
	payload := map[string]any{
		"url":   shareURL,
		"token": strings.TrimSpace(parseToken),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parseURL+"/get-sora-link", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	proxyURL := c.resolveProxyURL(account)
	accountID := int64(0)
	accountConcurrency := 0
	if account != nil {
		accountID = account.ID
		accountConcurrency = account.Concurrency
	}
	var resp *http.Response
	if c.httpUpstream != nil {
		resp, err = c.httpUpstream.Do(req, proxyURL, accountID, accountConcurrency)
	} else {
		resp, err = http.DefaultClient.Do(req)
	}
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("custom parse failed: %d %s", resp.StatusCode, truncateForLog(raw, 256))
	}
	downloadLink := strings.TrimSpace(gjson.GetBytes(raw, "download_link").String())
	if downloadLink == "" {
		return "", errors.New("custom parse response missing download_link")
	}
	return downloadLink, nil
}

func (c *SoraDirectClient) EnhancePrompt(ctx context.Context, account *Account, prompt, expansionLevel string, durationS int) (string, error) {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return "", err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	if strings.TrimSpace(expansionLevel) == "" {
		expansionLevel = "medium"
	}
	if durationS <= 0 {
		durationS = 10
	}

	payload := map[string]any{
		"prompt":          prompt,
		"expansion_level": expansionLevel,
		"duration_s":      durationS,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	headers := c.buildBaseHeaders(token, userAgent)
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	headers.Set("Origin", "https://sora.chatgpt.com")
	headers.Set("Referer", "https://sora.chatgpt.com/")

	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodPost, c.buildURL("/editor/enhance_prompt"), headers, bytes.NewReader(body), false)
	if err != nil {
		return "", err
	}
	enhancedPrompt := strings.TrimSpace(gjson.GetBytes(respBody, "enhanced_prompt").String())
	if enhancedPrompt == "" {
		return "", errors.New("enhance_prompt response missing enhanced_prompt")
	}
	return enhancedPrompt, nil
}

func (c *SoraDirectClient) GetImageTask(ctx context.Context, account *Account, taskID string) (*SoraImageTaskStatus, error) {
	status, found, err := c.fetchRecentImageTask(ctx, account, taskID, c.recentTaskLimit())
	if err != nil {
		return nil, err
	}
	if found {
		return status, nil
	}
	maxLimit := c.recentTaskLimitMax()
	if maxLimit > 0 && maxLimit != c.recentTaskLimit() {
		status, found, err = c.fetchRecentImageTask(ctx, account, taskID, maxLimit)
		if err != nil {
			return nil, err
		}
		if found {
			return status, nil
		}
	}
	return &SoraImageTaskStatus{ID: taskID, Status: "processing"}, nil
}

func (c *SoraDirectClient) fetchRecentImageTask(ctx context.Context, account *Account, taskID string, limit int) (*SoraImageTaskStatus, bool, error) {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return nil, false, err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	headers := c.buildBaseHeaders(token, userAgent)
	if limit <= 0 {
		limit = 20
	}
	endpoint := fmt.Sprintf("/v2/recent_tasks?limit=%d", limit)
	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodGet, c.buildURL(endpoint), headers, nil, false)
	if err != nil {
		return nil, false, err
	}
	var found *SoraImageTaskStatus
	gjson.GetBytes(respBody, "task_responses").ForEach(func(_, item gjson.Result) bool {
		if item.Get("id").String() != taskID {
			return true // continue
		}
		status := strings.TrimSpace(item.Get("status").String())
		progress := item.Get("progress_pct").Float()
		var urls []string
		item.Get("generations").ForEach(func(_, gen gjson.Result) bool {
			if u := strings.TrimSpace(gen.Get("url").String()); u != "" {
				urls = append(urls, u)
			}
			return true
		})
		found = &SoraImageTaskStatus{
			ID:          taskID,
			Status:      status,
			ProgressPct: progress,
			URLs:        urls,
		}
		return false // break
	})
	if found != nil {
		return found, true, nil
	}
	return &SoraImageTaskStatus{ID: taskID, Status: "processing"}, false, nil
}

func (c *SoraDirectClient) recentTaskLimit() int {
	if c == nil || c.cfg == nil {
		return 20
	}
	if c.cfg.Sora.Client.RecentTaskLimit > 0 {
		return c.cfg.Sora.Client.RecentTaskLimit
	}
	return 20
}

func (c *SoraDirectClient) recentTaskLimitMax() int {
	if c == nil || c.cfg == nil {
		return 0
	}
	if c.cfg.Sora.Client.RecentTaskLimitMax > 0 {
		return c.cfg.Sora.Client.RecentTaskLimitMax
	}
	return 0
}

func (c *SoraDirectClient) GetVideoTask(ctx context.Context, account *Account, taskID string) (*SoraVideoTaskStatus, error) {
	token, err := c.getAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	userAgent := c.taskUserAgent()
	proxyURL := c.resolveProxyURL(account)
	headers := c.buildBaseHeaders(token, userAgent)

	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodGet, c.buildURL("/nf/pending/v2"), headers, nil, false)
	if err != nil {
		return nil, err
	}
	// 搜索 pending 列表（JSON 数组）
	pendingResult := gjson.ParseBytes(respBody)
	if pendingResult.IsArray() {
		var pendingFound *SoraVideoTaskStatus
		pendingResult.ForEach(func(_, task gjson.Result) bool {
			if task.Get("id").String() != taskID {
				return true
			}
			progress := 0
			if v := task.Get("progress_pct"); v.Exists() {
				progress = int(v.Float() * 100)
			}
			status := strings.TrimSpace(task.Get("status").String())
			pendingFound = &SoraVideoTaskStatus{
				ID:          taskID,
				Status:      status,
				ProgressPct: progress,
			}
			return false
		})
		if pendingFound != nil {
			return pendingFound, nil
		}
	}

	respBody, _, err = c.doRequestWithProxy(ctx, account, proxyURL, http.MethodGet, c.buildURL("/project_y/profile/drafts?limit=15"), headers, nil, false)
	if err != nil {
		return nil, err
	}
	var draftFound *SoraVideoTaskStatus
	gjson.GetBytes(respBody, "items").ForEach(func(_, draft gjson.Result) bool {
		if draft.Get("task_id").String() != taskID {
			return true
		}
		generationID := strings.TrimSpace(draft.Get("id").String())
		kind := strings.TrimSpace(draft.Get("kind").String())
		reason := strings.TrimSpace(draft.Get("reason_str").String())
		if reason == "" {
			reason = strings.TrimSpace(draft.Get("markdown_reason_str").String())
		}
		urlStr := strings.TrimSpace(draft.Get("downloadable_url").String())
		if urlStr == "" {
			urlStr = strings.TrimSpace(draft.Get("url").String())
		}

		if kind == "sora_content_violation" || reason != "" || urlStr == "" {
			msg := reason
			if msg == "" {
				msg = "Content violates guardrails"
			}
			draftFound = &SoraVideoTaskStatus{
				ID:           taskID,
				Status:       "failed",
				GenerationID: generationID,
				ErrorMsg:     msg,
			}
		} else {
			draftFound = &SoraVideoTaskStatus{
				ID:           taskID,
				Status:       "completed",
				GenerationID: generationID,
				URLs:         []string{urlStr},
			}
		}
		return false
	})
	if draftFound != nil {
		return draftFound, nil
	}

	return &SoraVideoTaskStatus{ID: taskID, Status: "processing"}, nil
}

func (c *SoraDirectClient) buildURL(endpoint string) string {
	base := strings.TrimRight(strings.TrimSpace(c.baseURL), "/")
	if base == "" && c != nil && c.cfg != nil {
		base = normalizeSoraBaseURL(c.cfg.Sora.Client.BaseURL)
		c.baseURL = base
	}
	if base == "" {
		return endpoint
	}
	if strings.HasPrefix(endpoint, "/") {
		return base + endpoint
	}
	return base + "/" + endpoint
}

func (c *SoraDirectClient) defaultUserAgent() string {
	if c == nil || c.cfg == nil {
		return soraDefaultUserAgent
	}
	ua := strings.TrimSpace(c.cfg.Sora.Client.UserAgent)
	if ua == "" {
		return soraDefaultUserAgent
	}
	return ua
}

func (c *SoraDirectClient) taskUserAgent() string {
	if c != nil && c.cfg != nil {
		if ua := strings.TrimSpace(c.cfg.Sora.Client.UserAgent); ua != "" {
			return ua
		}
	}
	if len(soraMobileUserAgents) > 0 {
		return soraMobileUserAgents[soraRandInt(len(soraMobileUserAgents))]
	}
	if len(soraDesktopUserAgents) > 0 {
		return soraDesktopUserAgents[soraRandInt(len(soraDesktopUserAgents))]
	}
	return soraDefaultUserAgent
}

func (c *SoraDirectClient) resolveProxyURL(account *Account) string {
	if account == nil || account.ProxyID == nil || account.Proxy == nil {
		return ""
	}
	return strings.TrimSpace(account.Proxy.URL())
}

func (c *SoraDirectClient) getAccessToken(ctx context.Context, account *Account) (string, error) {
	if account == nil {
		return "", errors.New("account is nil")
	}

	allowProvider := c.allowOpenAITokenProvider(account)
	var providerErr error
	if allowProvider && c.tokenProvider != nil {
		token, err := c.tokenProvider.GetAccessToken(ctx, account)
		if err == nil && strings.TrimSpace(token) != "" {
			c.logTokenSource(account, "openai_token_provider")
			return token, nil
		}
		providerErr = err
		if err != nil && c.debugEnabled() {
			c.debugLogf(
				"token_provider_failed account_id=%d platform=%s err=%s",
				account.ID,
				account.Platform,
				logredact.RedactText(err.Error()),
			)
		}
	}
	token := strings.TrimSpace(account.GetCredential("access_token"))
	if token != "" {
		expiresAt := account.GetCredentialAsTime("expires_at")
		if expiresAt != nil && time.Until(*expiresAt) <= 2*time.Minute {
			refreshed, refreshErr := c.recoverAccessToken(ctx, account, "access_token_expiring")
			if refreshErr == nil && strings.TrimSpace(refreshed) != "" {
				c.logTokenSource(account, "refresh_token_recovered")
				return refreshed, nil
			}
			if refreshErr != nil && c.debugEnabled() {
				c.debugLogf("token_refresh_before_use_failed account_id=%d err=%s", account.ID, logredact.RedactText(refreshErr.Error()))
			}
		}
		c.logTokenSource(account, "account_credentials")
		return token, nil
	}

	recovered, recoverErr := c.recoverAccessToken(ctx, account, "access_token_missing")
	if recoverErr == nil && strings.TrimSpace(recovered) != "" {
		c.logTokenSource(account, "session_or_refresh_recovered")
		return recovered, nil
	}
	if recoverErr != nil && c.debugEnabled() {
		c.debugLogf("token_recover_failed account_id=%d platform=%s err=%s", account.ID, account.Platform, logredact.RedactText(recoverErr.Error()))
	}
	if providerErr != nil {
		return "", providerErr
	}
	if c.tokenProvider != nil && !allowProvider {
		c.logTokenSource(account, "account_credentials(provider_disabled)")
	}
	return "", errors.New("access_token not found")
}

func (c *SoraDirectClient) recoverAccessToken(ctx context.Context, account *Account, reason string) (string, error) {
	if account == nil {
		return "", errors.New("account is nil")
	}

	if sessionToken := strings.TrimSpace(account.GetCredential("session_token")); sessionToken != "" {
		accessToken, expiresAt, err := c.exchangeSessionToken(ctx, account, sessionToken)
		if err == nil && strings.TrimSpace(accessToken) != "" {
			c.applyRecoveredToken(ctx, account, accessToken, "", expiresAt, sessionToken)
			c.logTokenRecover(account, "session_token", reason, true, nil)
			return accessToken, nil
		}
		c.logTokenRecover(account, "session_token", reason, false, err)
	}

	refreshToken := strings.TrimSpace(account.GetCredential("refresh_token"))
	if refreshToken == "" {
		return "", errors.New("session_token/refresh_token not found")
	}
	accessToken, newRefreshToken, expiresAt, err := c.exchangeRefreshToken(ctx, account, refreshToken)
	if err != nil {
		c.logTokenRecover(account, "refresh_token", reason, false, err)
		return "", err
	}
	if strings.TrimSpace(accessToken) == "" {
		return "", errors.New("refreshed access_token is empty")
	}
	c.applyRecoveredToken(ctx, account, accessToken, newRefreshToken, expiresAt, "")
	c.logTokenRecover(account, "refresh_token", reason, true, nil)
	return accessToken, nil
}

func (c *SoraDirectClient) exchangeSessionToken(ctx context.Context, account *Account, sessionToken string) (string, string, error) {
	headers := http.Header{}
	headers.Set("Cookie", "__Secure-next-auth.session-token="+sessionToken)
	headers.Set("Accept", "application/json")
	headers.Set("Origin", "https://sora.chatgpt.com")
	headers.Set("Referer", "https://sora.chatgpt.com/")
	headers.Set("User-Agent", c.defaultUserAgent())
	body, _, err := c.doRequest(ctx, account, http.MethodGet, soraSessionAuthURL, headers, nil, false)
	if err != nil {
		return "", "", err
	}
	accessToken := strings.TrimSpace(gjson.GetBytes(body, "accessToken").String())
	if accessToken == "" {
		return "", "", errors.New("session exchange missing accessToken")
	}
	expiresAt := strings.TrimSpace(gjson.GetBytes(body, "expires").String())
	return accessToken, expiresAt, nil
}

func (c *SoraDirectClient) exchangeRefreshToken(ctx context.Context, account *Account, refreshToken string) (string, string, string, error) {
	clientIDs := []string{
		strings.TrimSpace(account.GetCredential("client_id")),
		openaioauth.SoraClientID,
		openaioauth.ClientID,
	}
	tried := make(map[string]struct{}, len(clientIDs))
	var lastErr error

	for _, clientID := range clientIDs {
		if clientID == "" {
			continue
		}
		if _, ok := tried[clientID]; ok {
			continue
		}
		tried[clientID] = struct{}{}

		formData := url.Values{}
		formData.Set("client_id", clientID)
		formData.Set("grant_type", "refresh_token")
		formData.Set("refresh_token", refreshToken)
		formData.Set("redirect_uri", "com.openai.chat://auth0.openai.com/ios/com.openai.chat/callback")
		headers := http.Header{}
		headers.Set("Accept", "application/json")
		headers.Set("Content-Type", "application/x-www-form-urlencoded")
		headers.Set("User-Agent", c.defaultUserAgent())

		respBody, _, err := c.doRequest(ctx, account, http.MethodPost, soraOAuthTokenURL, headers, strings.NewReader(formData.Encode()), false)
		if err != nil {
			lastErr = err
			if c.debugEnabled() {
				c.debugLogf("refresh_token_exchange_failed account_id=%d client_id=%s err=%s", account.ID, clientID, logredact.RedactText(err.Error()))
			}
			continue
		}
		accessToken := strings.TrimSpace(gjson.GetBytes(respBody, "access_token").String())
		if accessToken == "" {
			lastErr = errors.New("oauth refresh response missing access_token")
			continue
		}
		newRefreshToken := strings.TrimSpace(gjson.GetBytes(respBody, "refresh_token").String())
		expiresIn := gjson.GetBytes(respBody, "expires_in").Int()
		expiresAt := ""
		if expiresIn > 0 {
			expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
		}
		return accessToken, newRefreshToken, expiresAt, nil
	}

	if lastErr != nil {
		return "", "", "", lastErr
	}
	return "", "", "", errors.New("no available client_id for refresh_token exchange")
}

func (c *SoraDirectClient) applyRecoveredToken(ctx context.Context, account *Account, accessToken, refreshToken, expiresAt, sessionToken string) {
	if account == nil {
		return
	}
	if account.Credentials == nil {
		account.Credentials = make(map[string]any)
	}
	if strings.TrimSpace(accessToken) != "" {
		account.Credentials["access_token"] = accessToken
	}
	if strings.TrimSpace(refreshToken) != "" {
		account.Credentials["refresh_token"] = refreshToken
	}
	if strings.TrimSpace(expiresAt) != "" {
		account.Credentials["expires_at"] = expiresAt
	}
	if strings.TrimSpace(sessionToken) != "" {
		account.Credentials["session_token"] = sessionToken
	}

	if c.accountRepo != nil {
		if err := c.accountRepo.Update(ctx, account); err != nil {
			if c.debugEnabled() {
				c.debugLogf("persist_recovered_token_failed account_id=%d err=%s", account.ID, logredact.RedactText(err.Error()))
			}
		}
	}
	c.updateSoraAccountExtension(ctx, account, accessToken, refreshToken, sessionToken)
}

func (c *SoraDirectClient) updateSoraAccountExtension(ctx context.Context, account *Account, accessToken, refreshToken, sessionToken string) {
	if c == nil || c.soraAccountRepo == nil || account == nil || account.ID <= 0 {
		return
	}
	updates := make(map[string]any)
	if strings.TrimSpace(accessToken) != "" && strings.TrimSpace(refreshToken) != "" {
		updates["access_token"] = accessToken
		updates["refresh_token"] = refreshToken
	}
	if strings.TrimSpace(sessionToken) != "" {
		updates["session_token"] = sessionToken
	}
	if len(updates) == 0 {
		return
	}
	if err := c.soraAccountRepo.Upsert(ctx, account.ID, updates); err != nil && c.debugEnabled() {
		c.debugLogf("persist_sora_extension_failed account_id=%d err=%s", account.ID, logredact.RedactText(err.Error()))
	}
}

func (c *SoraDirectClient) logTokenRecover(account *Account, source, reason string, success bool, err error) {
	if !c.debugEnabled() || account == nil {
		return
	}
	if success {
		c.debugLogf("token_recover_success account_id=%d platform=%s source=%s reason=%s", account.ID, account.Platform, source, reason)
		return
	}
	if err == nil {
		c.debugLogf("token_recover_failed account_id=%d platform=%s source=%s reason=%s", account.ID, account.Platform, source, reason)
		return
	}
	c.debugLogf("token_recover_failed account_id=%d platform=%s source=%s reason=%s err=%s", account.ID, account.Platform, source, reason, logredact.RedactText(err.Error()))
}

func (c *SoraDirectClient) allowOpenAITokenProvider(account *Account) bool {
	if c == nil || c.tokenProvider == nil {
		return false
	}
	if account != nil && account.Platform == PlatformSora {
		return c.cfg != nil && c.cfg.Sora.Client.UseOpenAITokenProvider
	}
	return true
}

func (c *SoraDirectClient) logTokenSource(account *Account, source string) {
	if !c.debugEnabled() || account == nil {
		return
	}
	c.debugLogf(
		"token_selected account_id=%d platform=%s account_type=%s source=%s",
		account.ID,
		account.Platform,
		account.Type,
		source,
	)
}

func (c *SoraDirectClient) buildBaseHeaders(token, userAgent string) http.Header {
	headers := http.Header{}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	if userAgent != "" {
		headers.Set("User-Agent", userAgent)
	}
	if c != nil && c.cfg != nil {
		for key, value := range c.cfg.Sora.Client.Headers {
			if strings.EqualFold(key, "authorization") || strings.EqualFold(key, "openai-sentinel-token") {
				continue
			}
			headers.Set(key, value)
		}
	}
	return headers
}

func (c *SoraDirectClient) doRequest(ctx context.Context, account *Account, method, urlStr string, headers http.Header, body io.Reader, allowRetry bool) ([]byte, http.Header, error) {
	return c.doRequestWithProxy(ctx, account, c.resolveProxyURL(account), method, urlStr, headers, body, allowRetry)
}

func (c *SoraDirectClient) doRequestWithProxy(
	ctx context.Context,
	account *Account,
	proxyURL string,
	method,
	urlStr string,
	headers http.Header,
	body io.Reader,
	allowRetry bool,
) ([]byte, http.Header, error) {
	if strings.TrimSpace(urlStr) == "" {
		return nil, nil, errors.New("empty upstream url")
	}
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		proxyURL = c.resolveProxyURL(account)
	}
	if cooldownErr := c.checkCloudflareChallengeCooldown(account, proxyURL); cooldownErr != nil {
		return nil, nil, cooldownErr
	}
	timeout := 0
	if c != nil && c.cfg != nil {
		timeout = c.cfg.Sora.Client.TimeoutSeconds
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}
	maxRetries := 0
	if allowRetry && c != nil && c.cfg != nil {
		maxRetries = c.cfg.Sora.Client.MaxRetries
	}
	if maxRetries < 0 {
		maxRetries = 0
	}

	var bodyBytes []byte
	if body != nil {
		b, err := io.ReadAll(body)
		if err != nil {
			return nil, nil, err
		}
		bodyBytes = b
	}

	attempts := maxRetries + 1
	authRecovered := false
	authRecoverExtraAttemptGranted := false
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if c.debugEnabled() {
			c.debugLogf(
				"request_start method=%s url=%s attempt=%d/%d timeout_s=%d body_bytes=%d proxy_bound=%t headers=%s",
				method,
				sanitizeSoraLogURL(urlStr),
				attempt,
				attempts,
				timeout,
				len(bodyBytes),
				proxyURL != "",
				formatSoraHeaders(headers),
			)
		}

		var reader io.Reader
		if bodyBytes != nil {
			reader = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, urlStr, reader)
		if err != nil {
			return nil, nil, err
		}
		req.Header = headers.Clone()
		start := time.Now()

		resp, err := c.doHTTP(req, proxyURL, account)
		if err != nil {
			lastErr = err
			if c.debugEnabled() {
				c.debugLogf(
					"request_transport_error method=%s url=%s attempt=%d/%d err=%s",
					method,
					sanitizeSoraLogURL(urlStr),
					attempt,
					attempts,
					logredact.RedactText(err.Error()),
				)
			}
			if attempt < attempts && allowRetry {
				if c.debugEnabled() {
					c.debugLogf("request_retry_scheduled method=%s url=%s reason=transport_error next_attempt=%d/%d", method, sanitizeSoraLogURL(urlStr), attempt+1, attempts)
				}
				c.sleepRetry(attempt)
				continue
			}
			return nil, nil, err
		}

		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, resp.Header, readErr
		}

		if c.cfg != nil && c.cfg.Sora.Client.Debug {
			c.debugLogf(
				"response_received method=%s url=%s attempt=%d/%d status=%d cost=%s resp_bytes=%d resp_headers=%s",
				method,
				sanitizeSoraLogURL(urlStr),
				attempt,
				attempts,
				resp.StatusCode,
				time.Since(start),
				len(respBody),
				formatSoraHeaders(resp.Header),
			)
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			isCFChallenge := soraerror.IsCloudflareChallengeResponse(resp.StatusCode, resp.Header, respBody)
			if isCFChallenge {
				c.recordCloudflareChallengeCooldown(account, proxyURL, resp.StatusCode, resp.Header, respBody)
			}
			if !isCFChallenge && !authRecovered && shouldAttemptSoraTokenRecover(resp.StatusCode, urlStr) && account != nil {
				if recovered, recoverErr := c.recoverAccessToken(ctx, account, fmt.Sprintf("upstream_status_%d", resp.StatusCode)); recoverErr == nil && strings.TrimSpace(recovered) != "" {
					headers.Set("Authorization", "Bearer "+recovered)
					authRecovered = true
					if attempt == attempts && !authRecoverExtraAttemptGranted {
						attempts++
						authRecoverExtraAttemptGranted = true
					}
					if c.debugEnabled() {
						c.debugLogf("request_retry_with_recovered_token method=%s url=%s status=%d", method, sanitizeSoraLogURL(urlStr), resp.StatusCode)
					}
					continue
				} else if recoverErr != nil && c.debugEnabled() {
					c.debugLogf("request_recover_token_failed method=%s url=%s status=%d err=%s", method, sanitizeSoraLogURL(urlStr), resp.StatusCode, logredact.RedactText(recoverErr.Error()))
				}
			}
			if c.debugEnabled() {
				c.debugLogf(
					"response_non_success method=%s url=%s attempt=%d/%d status=%d body=%s",
					method,
					sanitizeSoraLogURL(urlStr),
					attempt,
					attempts,
					resp.StatusCode,
					summarizeSoraResponseBody(respBody, 512),
				)
			}
			upstreamErr := c.buildUpstreamError(resp.StatusCode, resp.Header, respBody, urlStr)
			lastErr = upstreamErr
			if isCFChallenge {
				return nil, resp.Header, upstreamErr
			}
			if allowRetry && attempt < attempts && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) {
				if c.debugEnabled() {
					c.debugLogf("request_retry_scheduled method=%s url=%s reason=status_%d next_attempt=%d/%d", method, sanitizeSoraLogURL(urlStr), resp.StatusCode, attempt+1, attempts)
				}
				c.sleepRetry(attempt)
				continue
			}
			return nil, resp.Header, upstreamErr
		}
		return respBody, resp.Header, nil
	}
	if lastErr != nil {
		return nil, nil, lastErr
	}
	return nil, nil, errors.New("upstream retries exhausted")
}

func shouldAttemptSoraTokenRecover(statusCode int, rawURL string) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		parsed, err := url.Parse(strings.TrimSpace(rawURL))
		if err != nil {
			return false
		}
		host := strings.ToLower(parsed.Hostname())
		if host != "sora.chatgpt.com" && host != "chatgpt.com" {
			return false
		}
		// 避免在 ST->AT 转换接口上递归触发 token 恢复导致死循环。
		path := strings.ToLower(strings.TrimSpace(parsed.Path))
		if path == "/api/auth/session" {
			return false
		}
		return true
	default:
		return false
	}
}

func (c *SoraDirectClient) doHTTP(req *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if c != nil && c.cfg != nil && c.cfg.Sora.Client.CurlCFFISidecar.Enabled {
		resp, err := c.doHTTPViaCurlCFFISidecar(req, proxyURL, account)
		if err != nil {
			return nil, err
		}
		return resp, nil
	}

	enableTLS := c == nil || c.cfg == nil || !c.cfg.Sora.Client.DisableTLSFingerprint
	if c.httpUpstream != nil {
		accountID := int64(0)
		accountConcurrency := 0
		if account != nil {
			accountID = account.ID
			accountConcurrency = account.Concurrency
		}
		return c.httpUpstream.DoWithTLS(req, proxyURL, accountID, accountConcurrency, enableTLS)
	}
	return http.DefaultClient.Do(req)
}

func (c *SoraDirectClient) sleepRetry(attempt int) {
	backoff := time.Duration(attempt*attempt) * time.Second
	if backoff > 10*time.Second {
		backoff = 10 * time.Second
	}
	time.Sleep(backoff)
}

func (c *SoraDirectClient) buildUpstreamError(status int, headers http.Header, body []byte, requestURL string) error {
	msg := strings.TrimSpace(extractUpstreamErrorMessage(body))
	msg = sanitizeUpstreamErrorMessage(msg)
	if status == http.StatusNotFound && strings.Contains(strings.ToLower(msg), "not found") {
		if hint := soraBaseURLNotFoundHint(requestURL); hint != "" {
			msg = strings.TrimSpace(msg + " " + hint)
		}
	}
	if msg == "" {
		msg = truncateForLog(body, 256)
	}
	return &SoraUpstreamError{
		StatusCode: status,
		Message:    msg,
		Headers:    headers,
		Body:       body,
	}
}

func normalizeSoraBaseURL(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return trimmed
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "sora.chatgpt.com" && host != "chatgpt.com" {
		return trimmed
	}
	pathVal := strings.TrimRight(strings.TrimSpace(parsed.Path), "/")
	switch pathVal {
	case "", "/":
		parsed.Path = "/backend"
	case "/backend-api":
		parsed.Path = "/backend"
	}
	return strings.TrimRight(parsed.String(), "/")
}

func soraBaseURLNotFoundHint(requestURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(requestURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "sora.chatgpt.com" && host != "chatgpt.com" {
		return ""
	}
	pathVal := strings.TrimSpace(parsed.Path)
	if strings.HasPrefix(pathVal, "/backend/") || pathVal == "/backend" {
		return ""
	}
	return "(请检查 sora.client.base_url，建议配置为 https://sora.chatgpt.com/backend)"
}

func (c *SoraDirectClient) generateSentinelToken(ctx context.Context, account *Account, accessToken, userAgent, proxyURL string) (string, error) {
	reqID := uuid.NewString()
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		userAgent = c.taskUserAgent()
	}
	powToken := soraPowTokenGenerator(userAgent)
	payload := map[string]any{
		"p":    powToken,
		"flow": soraSentinelFlow,
		"id":   reqID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	headers := http.Header{}
	headers.Set("Accept", "application/json, text/plain, */*")
	headers.Set("Content-Type", "application/json")
	headers.Set("Origin", "https://sora.chatgpt.com")
	headers.Set("Referer", "https://sora.chatgpt.com/")
	headers.Set("User-Agent", userAgent)
	if accessToken != "" {
		headers.Set("Authorization", "Bearer "+accessToken)
	}

	urlStr := soraChatGPTBaseURL + "/backend-api/sentinel/req"
	respBody, _, err := c.doRequestWithProxy(ctx, account, proxyURL, http.MethodPost, urlStr, headers, bytes.NewReader(body), true)
	if err != nil {
		return "", err
	}
	var resp map[string]any
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", err
	}

	sentinel := soraBuildSentinelToken(soraSentinelFlow, reqID, powToken, resp, userAgent)
	if sentinel == "" {
		return "", errors.New("failed to build sentinel token")
	}
	return sentinel, nil
}

func soraGetPowToken(userAgent string) string {
	configList := soraBuildPowConfig(userAgent)
	seed := strconv.FormatFloat(soraRandFloat(), 'f', -1, 64)
	difficulty := "0fffff"
	solution, _ := soraSolvePow(seed, difficulty, configList)
	return "gAAAAAC" + solution
}

func soraRandFloat() float64 {
	soraRandMu.Lock()
	defer soraRandMu.Unlock()
	return soraRand.Float64()
}

func soraRandInt(max int) int {
	if max <= 1 {
		return 0
	}
	soraRandMu.Lock()
	defer soraRandMu.Unlock()
	return soraRand.Intn(max)
}

func soraBuildPowConfig(userAgent string) []any {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" && len(soraDesktopUserAgents) > 0 {
		userAgent = soraDesktopUserAgents[0]
	}
	screenVal := soraStableChoiceInt([]int{
		1920 + 1080,
		2560 + 1440,
		1920 + 1200,
		2560 + 1600,
	}, userAgent+"|screen")
	perfMs := float64(time.Since(soraPerfStart).Milliseconds())
	wallMs := float64(time.Now().UnixNano()) / 1e6
	diff := wallMs - perfMs
	return []any{
		screenVal,
		soraPowParseTime(),
		4294705152,
		0,
		userAgent,
		soraStableChoice(soraPowScripts, userAgent+"|script"),
		soraStableChoice(soraPowDPL, userAgent+"|dpl"),
		"en-US",
		"en-US,es-US,en,es",
		0,
		soraStableChoice(soraPowNavigatorKeys, userAgent+"|navigator"),
		soraStableChoice(soraPowDocumentKeys, userAgent+"|document"),
		soraStableChoice(soraPowWindowKeys, userAgent+"|window"),
		perfMs,
		uuid.NewString(),
		"",
		soraStableChoiceInt(soraPowCores, userAgent+"|cores"),
		diff,
	}
}

func soraStableChoice(items []string, seed string) string {
	if len(items) == 0 {
		return ""
	}
	idx := soraStableIndex(seed, len(items))
	return items[idx]
}

func soraStableChoiceInt(items []int, seed string) int {
	if len(items) == 0 {
		return 0
	}
	idx := soraStableIndex(seed, len(items))
	return items[idx]
}

func soraStableIndex(seed string, size int) int {
	if size <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return int(h.Sum32() % uint32(size))
}

func soraPowParseTime() string {
	loc := time.FixedZone("EST", -5*3600)
	return time.Now().In(loc).Format("Mon Jan 02 2006 15:04:05 GMT-0700 (Eastern Standard Time)")
}

func soraSolvePow(seed, difficulty string, configList []any) (string, bool) {
	diffLen := len(difficulty) / 2
	target, err := hexDecodeString(difficulty)
	if err != nil {
		return "", false
	}
	seedBytes := []byte(seed)

	part1 := mustMarshalJSON(configList[:3])
	part2 := mustMarshalJSON(configList[4:9])
	part3 := mustMarshalJSON(configList[10:])

	staticPart1 := append(part1[:len(part1)-1], ',')
	staticPart2 := append([]byte(","), append(part2[1:len(part2)-1], ',')...)
	staticPart3 := append([]byte(","), part3[1:]...)

	for i := 0; i < soraPowMaxIteration; i++ {
		dynamicI := []byte(strconv.Itoa(i))
		dynamicJ := []byte(strconv.Itoa(i >> 1))
		finalJSON := make([]byte, 0, len(staticPart1)+len(dynamicI)+len(staticPart2)+len(dynamicJ)+len(staticPart3))
		finalJSON = append(finalJSON, staticPart1...)
		finalJSON = append(finalJSON, dynamicI...)
		finalJSON = append(finalJSON, staticPart2...)
		finalJSON = append(finalJSON, dynamicJ...)
		finalJSON = append(finalJSON, staticPart3...)

		b64 := base64.StdEncoding.EncodeToString(finalJSON)
		hash := sha3.Sum512(append(seedBytes, []byte(b64)...))
		if bytes.Compare(hash[:diffLen], target[:diffLen]) <= 0 {
			return b64, true
		}
	}

	errorToken := "wQ8Lk5FbGpA2NcR9dShT6gYjU7VxZ4D" + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("\"%s\"", seed)))
	return errorToken, false
}

func soraBuildSentinelToken(flow, reqID, powToken string, resp map[string]any, userAgent string) string {
	finalPow := powToken
	proof, _ := resp["proofofwork"].(map[string]any)
	if required, _ := proof["required"].(bool); required {
		seed, _ := proof["seed"].(string)
		difficulty, _ := proof["difficulty"].(string)
		if seed != "" && difficulty != "" {
			configList := soraBuildPowConfig(userAgent)
			solution, _ := soraSolvePow(seed, difficulty, configList)
			finalPow = "gAAAAAB" + solution
		}
	}
	if !strings.HasSuffix(finalPow, "~S") {
		finalPow += "~S"
	}
	turnstile, _ := resp["turnstile"].(map[string]any)
	tokenPayload := map[string]any{
		"p":    finalPow,
		"t":    safeMapString(turnstile, "dx"),
		"c":    safeString(resp["token"]),
		"id":   reqID,
		"flow": flow,
	}
	encoded, _ := json.Marshal(tokenPayload)
	return string(encoded)
}

func safeMapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		return safeString(v)
	}
	return ""
}

func safeString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

func mustMarshalJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func hexDecodeString(s string) ([]byte, error) {
	dst := make([]byte, len(s)/2)
	_, err := hex.Decode(dst, []byte(s))
	return dst, err
}

func sanitizeSoraLogURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := parsed.Query()
	q.Del("sig")
	q.Del("expires")
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

func (c *SoraDirectClient) debugEnabled() bool {
	return c != nil && c.cfg != nil && c.cfg.Sora.Client.Debug
}

func (c *SoraDirectClient) debugLogf(format string, args ...any) {
	if !c.debugEnabled() {
		return
	}
	log.Printf("[SoraClient] "+format, args...)
}

func formatSoraHeaders(headers http.Header) string {
	if len(headers) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		values := headers.Values(key)
		if len(values) == 0 {
			continue
		}
		val := strings.Join(values, ",")
		if isSensitiveHeader(key) {
			out[key] = "***"
			continue
		}
		out[key] = truncateForLog([]byte(logredact.RedactText(val)), 160)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func isSensitiveHeader(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	switch k {
	case "authorization", "openai-sentinel-token", "cookie", "set-cookie", "x-api-key":
		return true
	default:
		return false
	}
}

func summarizeSoraResponseBody(body []byte, maxLen int) string {
	if len(body) == 0 {
		return ""
	}
	var text string
	if json.Valid(body) {
		text = logredact.RedactJSON(body)
	} else {
		text = logredact.RedactText(string(body))
	}
	text = strings.TrimSpace(text)
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "...(truncated)"
}
