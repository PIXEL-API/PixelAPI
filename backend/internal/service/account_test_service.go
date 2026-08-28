package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// sseDataPrefix matches SSE data lines with optional whitespace after colon.
// Some upstream APIs return non-standard "data:" without space (should be "data: ").
var sseDataPrefix = regexp.MustCompile(`^data:\s*`)

const (
	testClaudeAPIURL   = "https://api.anthropic.com/v1/messages?beta=true"
	chatgptCodexAPIURL = "https://chatgpt.com/backend-api/codex/responses"
)

// TestEvent represents a SSE event for account testing
type TestEvent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Model    string `json:"model,omitempty"`
	Status   string `json:"status,omitempty"`
	Code     string `json:"code,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Data     any    `json:"data,omitempty"`
	Success  bool   `json:"success,omitempty"`
	Error    string `json:"error,omitempty"`
}

const (
	defaultGeminiTextTestPrompt  = "hi"
	defaultOpenAIImageTestPrompt = "Generate a cute orange cat astronaut sticker on a clean pastel background."
	defaultGrokTestModel         = "grok-4.5"
	defaultOpencodeTestModel     = "deepseek-v4-flash"
)

// opencodeTestModelFallbacks 是 opencode 校验测试的备选模型。
// 首选 defaultOpencodeTestModel（deepseek-v4-flash 最新版在 opencode 仅中国区托管），
// 国际区账号访问会返回 403 RegionError。fallback 用 opencode 国际通用的裸 slug，
// 当首选模型因「模型不可用」类错误（区域限制/上游端点不可用）失败时按序重试，
// 避免把 key 有效但模型区域不匹配的账号误判为校验失败。
var opencodeTestModelFallbacks = []string{"gpt-5.6-luna", "grok-4.5"}

// isOpenAIImageModel checks if the model is an OpenAI image generation model (e.g. gpt-image-2).
func isOpenAIImageModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(model), "gpt-image-")
}

// AccountTestService handles account testing operations
type AccountTestService struct {
	accountRepo                AccountRepository
	geminiTokenProvider        *GeminiTokenProvider
	claudeTokenProvider        *ClaudeTokenProvider
	grokTokenProvider          *GrokTokenProvider
	antigravityGatewayService  *AntigravityGatewayService
	httpUpstream               HTTPUpstream
	cfg                        *config.Config
	tlsFPProfileService        *TLSFingerprintProfileService
	settingService             *SettingService
	agentIdentityTaskMu        sync.Mutex
	agentIdentityWSInvalidator agentIdentityWSConnectionInvalidator
	modelResolver              *AccountTestModelResolver
}

// NewAccountTestService creates a new AccountTestService
func NewAccountTestService(
	accountRepo AccountRepository,
	geminiTokenProvider *GeminiTokenProvider,
	claudeTokenProvider *ClaudeTokenProvider,
	antigravityGatewayService *AntigravityGatewayService,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	tlsFPProfileService *TLSFingerprintProfileService,
	settingService *SettingService,
	agentIdentityWSInvalidator *AgentIdentityWSInvalidatorProxy,
) *AccountTestService {
	return &AccountTestService{
		accountRepo:                accountRepo,
		geminiTokenProvider:        geminiTokenProvider,
		claudeTokenProvider:        claudeTokenProvider,
		antigravityGatewayService:  antigravityGatewayService,
		httpUpstream:               httpUpstream,
		cfg:                        cfg,
		tlsFPProfileService:        tlsFPProfileService,
		settingService:             settingService,
		agentIdentityWSInvalidator: agentIdentityWSInvalidator,
	}
}

func (s *AccountTestService) buildOpenAIAuthenticationHeaders(ctx context.Context, account *Account, token string) (http.Header, error) {
	if account == nil {
		return nil, errors.New("account is nil")
	}
	if account.IsOpenAIAgentIdentity() {
		if s.agentIdentityWSInvalidator == nil {
			return nil, errors.New("agent identity WS invalidator is not configured")
		}
		return buildAgentIdentityAuthenticationHeaders(ctx, s.accountRepo, s.agentIdentityWSInvalidator, &s.agentIdentityTaskMu, account)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("OpenAI authentication token is missing")
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	return headers, nil
}

func (s *AccountTestService) recoverAgentIdentityTask(ctx context.Context, account *Account, expectedTaskID string) error {
	if s.agentIdentityWSInvalidator == nil {
		return errors.New("agent identity WS invalidator is not configured")
	}
	return ensureAgentIdentityTaskForAccount(ctx, s.accountRepo, s.agentIdentityWSInvalidator, &s.agentIdentityTaskMu, account, expectedTaskID)
}

func (s *AccountTestService) SetGrokTokenProvider(grokTokenProvider *GrokTokenProvider) {
	s.grokTokenProvider = grokTokenProvider
}

// SetModelResolver 注入账号「测试连接」模型 resolver。
func (s *AccountTestService) SetModelResolver(resolver *AccountTestModelResolver) {
	s.modelResolver = resolver
}

// ResolveAvailableTestModels 委托 resolver 解析账号可测试模型列表。
// 返回统一结构的 []claude.Model；非 ready 状态返回业务错误而非空数组。
func (s *AccountTestService) ResolveAvailableTestModels(ctx context.Context, account *Account) ([]claude.Model, error) {
	if s == nil || s.modelResolver == nil {
		return nil, ErrOwnedAccountModelCatalogUnavailable
	}
	return s.modelResolver.ResolveTestModels(ctx, account)
}

// ResolveBatchTestModels 委托 resolver 解析多个账号共同可测试模型列表。
func (s *AccountTestService) ResolveBatchTestModels(ctx context.Context, accounts []*Account) ([]claude.Model, error) {
	if s == nil || s.modelResolver == nil {
		return nil, ErrOwnedAccountModelCatalogUnavailable
	}
	return s.modelResolver.ResolveBatchTestModels(ctx, accounts)
}

// ValidateTestModel 校验模型是否仍在账号可测试集合中（供计划测试/runner 服务端校验）。
func (s *AccountTestService) ValidateTestModel(ctx context.Context, accountID int64, modelID string) error {
	if s == nil || s.modelResolver == nil {
		return ErrOwnedAccountModelCatalogUnavailable
	}
	if s.accountRepo == nil {
		return ErrOwnedAccountModelCatalogUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	models, err := s.modelResolver.ResolveTestModels(ctx, account)
	if err != nil {
		return err
	}
	for _, model := range models {
		if model.ID == modelID {
			return nil
		}
	}
	return ErrAccountTestModelNotAvailable
}

func (s *AccountTestService) validateUpstreamBaseURL(raw string) (string, error) {
	if s.cfg == nil {
		return "", errors.New("config is not available")
	}
	if !s.cfg.Security.URLAllowlist.Enabled {
		return urlvalidator.ValidateURLFormat(raw, s.cfg.Security.URLAllowlist.AllowInsecureHTTP)
	}
	allowedHosts, err := upstreamAllowlistHosts(context.Background(), s.cfg, s.settingService)
	if err != nil {
		return "", err
	}
	normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     allowedHosts,
		RequireAllowlist: true,
		AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", err
	}
	return normalized, nil
}

// generateSessionString generates a Claude Code style session string.
// The output format is determined by the UA version in claude.DefaultHeaders,
// ensuring consistency between the user_id format and the UA sent to upstream.
func generateSessionString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	hex64 := hex.EncodeToString(b)
	sessionUUID := uuid.New().String()
	uaVersion := ExtractCLIVersion(claude.DefaultHeaders["User-Agent"])
	return FormatMetadataUserID(hex64, "", sessionUUID, uaVersion), nil
}

// createTestPayload creates a Claude Code style test request payload
func createTestPayload(modelID string) (map[string]any, error) {
	sessionID, err := generateSessionString()
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"model": modelID,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "text",
						"text": "hi",
						"cache_control": map[string]string{
							"type": "ephemeral",
						},
					},
				},
			},
		},
		"system": []map[string]any{
			{
				"type": "text",
				"text": claudeCodeSystemPrompt,
				"cache_control": map[string]string{
					"type": "ephemeral",
				},
			},
		},
		"metadata": map[string]string{
			"user_id": sessionID,
		},
		"max_tokens":  1024,
		"temperature": 1,
		"stream":      true,
	}, nil
}

// TestAccountConnection tests an account's connection by sending a test request
// All account types use full Claude Code client characteristics, only auth header differs
// modelID is optional - if empty, defaults to claude.DefaultTestModel
// mode is optional - "compact" routes OpenAI accounts to the /responses/compact probe path
func (s *AccountTestService) TestAccountConnection(c *gin.Context, accountID int64, modelID string, prompt string, mode string) error {
	ctx := c.Request.Context()

	// Get account
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return s.sendErrorAndEnd(c, "Account not found")
	}

	// Route to platform-specific test method
	if account.IsOpenAI() {
		return s.testOpenAIAccountConnection(c, account, modelID, prompt, normalizeAccountTestMode(mode))
	}

	if account.IsGrok() {
		return s.testGrokAccountConnection(c, account, modelID, prompt)
	}

	if account.IsGemini() {
		return s.testGeminiAccountConnection(c, account, modelID, prompt)
	}

	if account.Platform == PlatformAntigravity {
		return s.routeAntigravityTest(c, account, modelID, prompt)
	}

	if account.IsOpencode() {
		return s.testOpencodeAccountConnection(c, account, modelID)
	}

	if account.IsAnthropic() {
		return s.testClaudeAccountConnection(c, account, modelID)
	}

	return s.sendErrorAndEnd(c, fmt.Sprintf("Unsupported account platform: %s", account.Platform))
}

// testOpencodeAccountConnection tests an OpenCode Go subscription account's connection.
// opencode 走 /chat/completions（Authorization Bearer），非流式探测一次即可确认 api_key 有效。
// 首选模型（默认 deepseek-v4-flash）在 opencode 国际区会返回 403 RegionError，故在「模型不可用」
// 类错误时按序 fallback 到 opencodeTestModelFallbacks，避免把 key 有效但模型区域不匹配的账号误判为校验失败。
func (s *AccountTestService) testOpencodeAccountConnection(c *gin.Context, account *Account, modelID string) error {
	ctx := c.Request.Context()
	if s.httpUpstream == nil {
		return s.sendErrorAndEnd(c, "HTTP upstream is not configured")
	}

	testModelID := strings.TrimSpace(modelID)
	if testModelID == "" {
		testModelID = defaultOpencodeTestModel
	}
	testModelID = account.GetMappedModel(testModelID)
	if testModelID == "" {
		testModelID = defaultOpencodeTestModel
	}

	authToken := account.GetOpencodeApiKey()
	if authToken == "" {
		return s.sendErrorAndEnd(c, "OpenCode API key is missing")
	}

	normalizedBaseURL, err := s.validateUpstreamBaseURL(account.GetOpencodeBaseURL())
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Invalid OpenCode base URL: %s", err.Error()))
	}
	apiURL := buildOpenAIChatCompletionsURL(normalizedBaseURL)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	// 候选模型：首选 testModelID，遇模型级错误时按序 fallback。
	candidates := append([]string{testModelID}, opencodeTestModelFallbacks...)
	var lastStatus int
	var lastBody string
	for _, candidate := range candidates {
		s.sendEvent(c, TestEvent{Type: "test_start", Model: candidate})

		status, body, probeErr := s.probeOpencodeChatCompletions(ctx, account, apiURL, authToken, candidate)
		if probeErr != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("OpenCode request failed: %s", probeErr.Error()))
		}
		lastStatus, lastBody = status, string(body)

		if status == http.StatusOK {
			// 非流式响应：choices 有内容即视为连接成功。
			if opencodeChatCompletionsHasContent(body) {
				s.sendEvent(c, TestEvent{Type: "test_complete", Success: true, Text: "Connection test succeeded"})
				return nil
			}
			return s.sendErrorAndEnd(c, "OpenCode returned an unexpected response")
		}
		if opencodeTestErrorRetryableWithOtherModel(status, lastBody) {
			continue
		}
		break
	}
	return s.sendErrorAndEnd(c, fmt.Sprintf("OpenCode API returned %d: %s", lastStatus, lastBody))
}

// probeOpencodeChatCompletions 对 opencode 发起一次非流式 chat/completions 探测。
// 返回 HTTP 状态码与响应体（body 截断到 2MB）；网络/请求错误通过 error 返回。
func (s *AccountTestService) probeOpencodeChatCompletions(ctx context.Context, account *Account, apiURL, authToken, model string) (int, []byte, error) {
	payload := map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": "Reply with the single word: OK"}},
		"stream":   false,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), http.MethodPost, apiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))

	var resp *http.Response
	if s.tlsFPProfileService == nil {
		resp, err = s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, nil)
	} else {
		resp, err = s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	}
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return resp.StatusCode, body, nil
}

// opencodeChatCompletionsHasContent 判断非流式探测响应是否包含有效 choices（即连接成功）。
func opencodeChatCompletionsHasContent(body []byte) bool {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	return json.Unmarshal(body, &parsed) == nil && len(parsed.Choices) > 0
}

// opencodeTestErrorRetryableWithOtherModel 判断 opencode 探测失败是否属于「换一个模型可能成功」的
// 模型级错误（区域限制 / 上游 provider 端点不可用 / 模型不存在），而非账号级硬错误（认证/计费/用量）。
func opencodeTestErrorRetryableWithOtherModel(statusCode int, body string) bool {
	if statusCode < 400 {
		return false
	}
	// 401（认证/计费）、402（支付）、429（用量）是账号级硬错误，换模型无意义。
	switch statusCode {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusTooManyRequests:
		return false
	}
	lower := strings.ToLower(body)
	for _, marker := range []string{
		"regionerror",
		"server_error",
		"endpoint is unavailable",
		"model not found",
		"model does not exist",
		"not supported for format",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// testClaudeAccountConnection tests an Anthropic Claude account's connection
func (s *AccountTestService) testClaudeAccountConnection(c *gin.Context, account *Account, modelID string) error {
	ctx := c.Request.Context()

	// Determine the model to use
	testModelID := modelID
	if testModelID == "" {
		testModelID = claude.DefaultTestModel
	}

	// API Key 账号测试连接时也需要应用通配符模型映射。
	if account.Type == "apikey" {
		testModelID = account.GetMappedModel(testModelID)
	}

	// Bedrock accounts use a separate test path
	if account.IsBedrock() {
		return s.testBedrockAccountConnection(c, ctx, account, testModelID)
	}
	if account.Type == AccountTypeServiceAccount {
		return s.testClaudeVertexServiceAccountConnection(c, ctx, account, testModelID)
	}

	// Determine authentication method and API URL
	var authToken string
	var useBearer bool
	var apiURL string

	if account.IsOAuth() {
		// OAuth or Setup Token - use Bearer token
		useBearer = true
		apiURL = testClaudeAPIURL
		authToken = account.GetCredential("access_token")
		if authToken == "" {
			return s.sendErrorAndEnd(c, "No access token available")
		}
	} else if account.Type == "apikey" {
		// API Key - use x-api-key header
		useBearer = false
		authToken = account.GetCredential("api_key")
		if authToken == "" {
			return s.sendErrorAndEnd(c, "No API key available")
		}

		baseURL := account.GetBaseURL()
		if baseURL == "" {
			baseURL = "https://api.anthropic.com"
		}
		normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Invalid base URL: %s", err.Error()))
		}
		apiURL = strings.TrimSuffix(normalizedBaseURL, "/") + "/v1/messages?beta=true"
	} else {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Unsupported account type: %s", account.Type))
	}

	// Set SSE headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	// Create Claude Code style payload (same for all account types)
	payload, err := createTestPayload(testModelID)
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to create test payload")
	}
	payloadBytes, _ := json.Marshal(payload)

	// Send test_start event
	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	req, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(ctx),
		"POST",
		apiURL,
		bytes.NewReader(payloadBytes),
	)
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to create request")
	}

	// Set common headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	// Apply Claude Code client headers
	for key, value := range claude.DefaultHeaders {
		req.Header.Set(key, value)
	}

	// Set authentication header
	if useBearer {
		req.Header.Set("anthropic-beta", claude.DefaultBetaHeader)
		req.Header.Set("Authorization", "Bearer "+authToken)
	} else {
		req.Header.Set("anthropic-beta", claude.APIKeyBetaHeader)
		req.Header.Set("x-api-key", authToken)
	}
	account.ApplyHeaderOverrides(req.Header)

	// Get proxy URL
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) && !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned unexpected HTTP status %d", resp.StatusCode))
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body))

		// 403 表示账号被上游封禁，标记为 error 状态
		if resp.StatusCode == http.StatusForbidden {
			_ = s.accountRepo.SetError(ctx, account.ID, errMsg)
		}

		return s.sendErrorAndEnd(c, errMsg)
	}

	// Process SSE stream
	return s.processClaudeStream(c, resp.Body)
}

func (s *AccountTestService) testClaudeVertexServiceAccountConnection(c *gin.Context, ctx context.Context, account *Account, testModelID string) error {
	if mappedModel, matched := account.ResolveMappedModel(testModelID); matched {
		testModelID = mappedModel
	} else {
		testModelID = normalizeVertexAnthropicModelID(claude.NormalizeModelID(testModelID))
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	payload, err := createTestPayload(testModelID)
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to create test payload")
	}
	payloadBytes, _ := json.Marshal(payload)
	vertexBody, err := buildVertexAnthropicRequestBody(payloadBytes)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to create Vertex request body: %s", err.Error()))
	}

	if s.claudeTokenProvider == nil {
		return s.sendErrorAndEnd(c, "Claude token provider not configured")
	}
	accessToken, err := s.claudeTokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to get service account access token: %s", err.Error()))
	}

	fullURL, err := buildVertexAnthropicURL(account.VertexProjectID(), account.VertexLocation(testModelID), testModelID, true)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to build Vertex URL: %s", err.Error()))
	}

	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	req, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(ctx),
		http.MethodPost,
		fullURL,
		bytes.NewReader(vertexBody),
	)
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) && !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned unexpected HTTP status %d", resp.StatusCode))
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body))
		if resp.StatusCode == http.StatusForbidden {
			_ = s.accountRepo.SetError(ctx, account.ID, errMsg)
		}
		return s.sendErrorAndEnd(c, errMsg)
	}

	return s.processClaudeStream(c, resp.Body)
}

// testBedrockAccountConnection tests a Bedrock (SigV4 or API Key) account using non-streaming invoke
func (s *AccountTestService) testBedrockAccountConnection(c *gin.Context, ctx context.Context, account *Account, testModelID string) error {
	region := bedrockRuntimeRegion(account)
	resolvedModelID, ok := ResolveBedrockModelID(account, testModelID)
	if !ok {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Unsupported Bedrock model: %s", testModelID))
	}
	testModelID = resolvedModelID

	// Set SSE headers (test UI expects SSE)
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	// Create a minimal Bedrock-compatible payload (no stream, no cache_control)
	bedrockPayload := map[string]any{
		"anthropic_version": "bedrock-2023-05-31",
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "text",
						"text": "hi",
					},
				},
			},
		},
		"max_tokens":  256,
		"temperature": 1,
	}
	bedrockBody, _ := json.Marshal(bedrockPayload)

	// Use non-streaming endpoint (response is standard Claude JSON)
	apiURL := BuildBedrockURL(region, testModelID, false)

	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	req, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(ctx),
		"POST",
		apiURL,
		bytes.NewReader(bedrockBody),
	)
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")

	// Sign or set auth based on account type
	if account.IsBedrockAPIKey() {
		apiKey := account.GetCredential("api_key")
		if apiKey == "" {
			return s.sendErrorAndEnd(c, "No API key available")
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		signer, err := NewBedrockSignerFromAccount(account)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to create Bedrock signer: %s", err.Error()))
		}
		if err := signer.SignRequest(ctx, req, bedrockBody); err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to sign request: %s", err.Error()))
		}
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, nil)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) && !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned unexpected HTTP status %d", resp.StatusCode))
	}

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body)))
	}

	// Bedrock non-streaming response is standard Claude JSON, extract the text
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to parse response: %s", err.Error()))
	}

	text := ""
	if len(result.Content) > 0 {
		text = result.Content[0].Text
	}
	if text == "" {
		text = "(empty response)"
	}

	s.sendEvent(c, TestEvent{Type: "content", Text: text})
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}

// testOpenAIAccountConnection tests an OpenAI account's connection
func (s *AccountTestService) testOpenAIAccountConnection(c *gin.Context, account *Account, modelID string, prompt string, mode string) error {
	ctx := c.Request.Context()
	_ = prompt
	mode = normalizeAccountTestMode(mode)

	// Default to openai.DefaultTestModel for OpenAI testing
	testModelID := modelID
	if testModelID == "" {
		testModelID = openai.DefaultTestModel
	}

	// Align test routing with gateway behavior: OpenAI accounts apply normal
	// account model mapping, and compact mode applies compact-only mapping on top.
	testModelID = account.GetMappedModel(testModelID)
	if mode == AccountTestModeCompact {
		testModelID = resolveOpenAICompactForwardModel(account, testModelID)
		return s.testOpenAICompactConnection(c, account, testModelID)
	}

	// Route to image generation test if an image model is selected
	if isOpenAIImageModel(testModelID) {
		imagePrompt := strings.TrimSpace(prompt)
		if imagePrompt == "" {
			imagePrompt = defaultOpenAIImageTestPrompt
		}
		if account.Type == "apikey" {
			return s.testOpenAIImageAPIKey(c, ctx, account, testModelID, imagePrompt)
		}
		return s.testOpenAIImageOAuth(c, ctx, account, testModelID, imagePrompt)
	}

	// Determine authentication method and API URL
	var authToken string
	var apiURL string
	var isOAuth bool

	if account.IsOAuth() {
		isOAuth = true
		// OAuth - use Bearer token with ChatGPT internal API
		authToken = account.GetOpenAIAccessToken()
		if authToken == "" && !account.IsOpenAIAgentIdentity() {
			return s.sendErrorAndEnd(c, "No access token available")
		}

		// OAuth uses ChatGPT internal API
		apiURL = chatgptCodexAPIURL
	} else if account.Type == "apikey" {
		// API Key - use Platform API
		authToken = account.GetOpenAIApiKey()
		if authToken == "" {
			return s.sendErrorAndEnd(c, "No API key available")
		}

		baseURL := account.GetOpenAIBaseURL()
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Invalid base URL: %s", err.Error()))
		}
		apiURL = buildOpenAIResponsesURL(normalizedBaseURL)
	} else {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Unsupported account type: %s", account.Type))
	}

	// Set SSE headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	// Create OpenAI Responses API payload
	payload := createOpenAITestPayload(testModelID, isOAuth)
	payloadBytes, _ := json.Marshal(payload)

	// Send test_start event
	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	agentIdentityTaskRecoveryTried := false
	for {
		req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(payloadBytes))
		if err != nil {
			return s.sendErrorAndEnd(c, "Failed to create request")
		}

		// Set common headers
		req.Header.Set("Content-Type", "application/json")
		authHeaders, err := s.buildOpenAIAuthenticationHeaders(ctx, account, authToken)
		if err != nil {
			return s.sendErrorAndEnd(c, err.Error())
		}
		for key, values := range authHeaders {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
		expectedAgentIdentityTaskID := strings.TrimSpace(account.GetCredential("task_id"))

		// Set OAuth-specific headers for ChatGPT internal API
		if isOAuth {
			req.Host = "chatgpt.com"
			applyOpenAITestCodexHeaders(req.Header, account, "text/event-stream")
			setOpenAIChatGPTAccountHeaders(req.Header, account)
		}
		account.ApplyHeaderOverrides(req.Header)
		if account.IsOpenAIAgentIdentity() {
			req.Header.Set("Authorization", authHeaders.Get("Authorization"))
		}

		// Get proxy URL
		proxyURL := ""
		if account.ProxyID != nil && account.Proxy != nil {
			proxyURL = account.Proxy.URL()
		}

		req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
		resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
		}
		defer func() { _ = resp.Body.Close() }()

		if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) && !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return s.sendErrorAndEnd(c, fmt.Sprintf("API returned unexpected HTTP status %d", resp.StatusCode))
		}

		if isOAuth && s.accountRepo != nil {
			if updates, err := extractOpenAICodexProbeUpdates(resp); err == nil && len(updates) > 0 {
				_ = s.accountRepo.UpdateExtra(ctx, account.ID, updates)
				mergeAccountExtra(account, updates)
			}
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			if account.IsOpenAIAgentIdentity() && !agentIdentityTaskRecoveryTried &&
				isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, body) {
				_ = resp.Body.Close()
				if err := s.recoverAgentIdentityTask(ctx, account, expectedAgentIdentityTaskID); err != nil {
					return s.sendErrorAndEnd(c, fmt.Sprintf("Agent Identity task recovery failed: %s", err.Error()))
				}
				ctx = withAgentIdentitySensitiveValues(ctx, expectedAgentIdentityTaskID)
				agentIdentityTaskRecoveryTried = true
				continue
			}
			body = redactAgentIdentitySensitiveBodyForAccount(account, body, agentIdentitySensitiveValuesFromContext(ctx)...)
			if resp.StatusCode == http.StatusTooManyRequests {
				s.reconcileOpenAI429State(ctx, account, resp.Header, body)
			}
			// 401 Unauthorized: 标记账号为永久错误
			if resp.StatusCode == http.StatusUnauthorized && s.accountRepo != nil {
				errMsg := fmt.Sprintf("Authentication failed (401): %s", string(body))
				_ = s.accountRepo.SetError(ctx, account.ID, errMsg)
			}
			return s.sendErrorAndEnd(c, fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body)))
		}

		// Process SSE stream
		return s.processOpenAIStream(c, resp.Body)
	}
}

func (s *AccountTestService) testGrokAccountConnection(c *gin.Context, account *Account, modelID string, prompt string) error {
	ctx := c.Request.Context()
	if s.httpUpstream == nil {
		return s.sendErrorAndEnd(c, "HTTP upstream is not configured")
	}

	testModelID := strings.TrimSpace(modelID)
	if testModelID == "" {
		testModelID = defaultGrokTestModel
	}
	testModelID = account.GetMappedModel(testModelID)

	var authToken string
	switch account.Type {
	case AccountTypeOAuth:
		if s.grokTokenProvider == nil {
			return s.sendErrorAndEnd(c, "Grok token provider is not configured")
		}
		var err error
		authToken, err = s.grokTokenProvider.GetAccessTokenForManualTest(ctx, account)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Grok token refresh failed: %s", err.Error()))
		}
	case AccountTypeAPIKey:
		authToken = strings.TrimSpace(account.GetCredential("api_key"))
		if authToken == "" {
			return s.sendErrorAndEnd(c, "Grok API key is missing")
		}
	default:
		return s.sendErrorAndEnd(c, fmt.Sprintf("Unsupported Grok account type: %s", account.Type))
	}
	if strings.TrimSpace(authToken) == "" {
		return s.sendErrorAndEnd(c, "No Grok access token available")
	}

	normalizedBaseURL, err := s.validateUpstreamBaseURL(account.GetGrokBaseURL())
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Invalid Grok base URL: %s", err.Error()))
	}
	apiURL, err := xai.BuildResponsesURL(normalizedBaseURL)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Invalid Grok responses URL: %s", err.Error()))
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	payloadBytes, _ := json.Marshal(createGrokTestPayload(testModelID, prompt))
	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	req, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(ctx),
		http.MethodPost,
		apiURL,
		bytes.NewReader(payloadBytes),
	)
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to create Grok request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+authToken)
	if account.IsGrokOAuth() {
		applyGrokCLIHeaders(req.Header)
	}
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	var resp *http.Response
	if s.tlsFPProfileService == nil {
		resp, err = s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, nil)
	} else {
		resp, err = s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	}
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Grok request failed: %s", err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) && !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Grok API returned unexpected HTTP status %d", resp.StatusCode))
	}

	if s.accountRepo != nil {
		if snapshot := xai.ParseQuotaHeaders(resp.Header, resp.StatusCode); snapshot != nil {
			_ = s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
				grokQuotaSnapshotExtraKey: snapshot,
			})
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusPaymentRequired {
			_, stateErr := setGrokPaymentRequiredErrorIfMatch(ctx, s.accountRepo, account)
			if stateErr != nil {
				return s.sendErrorAndEnd(
					c,
					fmt.Sprintf("Grok API returned 402, but the account could not be marked as error: %s", stateErr),
				)
			}
		}
		return s.sendErrorAndEnd(c, fmt.Sprintf("Grok API returned %d: %s", resp.StatusCode, string(body)))
	}

	return s.processOpenAIStream(c, resp.Body)
}

// testOpenAICompactConnection probes /responses/compact and persists the
// resulting capability state on the account.
func (s *AccountTestService) testOpenAICompactConnection(c *gin.Context, account *Account, testModelID string) error {
	ctx := c.Request.Context()

	authToken := ""
	apiURL := ""
	isOAuth := false

	switch {
	case account.IsOAuth():
		isOAuth = true
		authToken = account.GetOpenAIAccessToken()
		if authToken == "" && !account.IsOpenAIAgentIdentity() {
			return s.sendErrorAndEnd(c, "No access token available")
		}
		apiURL = chatgptCodexAPIURL + "/compact"
	case account.Type == AccountTypeAPIKey:
		authToken = account.GetOpenAIApiKey()
		if authToken == "" {
			return s.sendErrorAndEnd(c, "No API key available")
		}
		baseURL := account.GetOpenAIBaseURL()
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Invalid base URL: %s", err.Error()))
		}
		apiURL = appendOpenAIResponsesRequestPathSuffix(buildOpenAIResponsesURL(normalizedBaseURL), "/compact")
	default:
		return s.sendErrorAndEnd(c, fmt.Sprintf("Unsupported account type: %s", account.Type))
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	payloadBytes, _ := json.Marshal(createOpenAICompactProbePayload(testModelID))
	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	agentIdentityTaskRecoveryTried := false
	for {
		req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(payloadBytes))
		if err != nil {
			return s.sendErrorAndEnd(c, "Failed to create request")
		}

		req.Header.Set("Content-Type", "application/json")
		authHeaders, err := s.buildOpenAIAuthenticationHeaders(ctx, account, authToken)
		if err != nil {
			return s.sendErrorAndEnd(c, err.Error())
		}
		for key, values := range authHeaders {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
		expectedAgentIdentityTaskID := strings.TrimSpace(account.GetCredential("task_id"))
		applyOpenAITestCodexHeaders(req.Header, account, "application/json")
		probeSessionID := compactProbeSessionID(account.ID)
		req.Header.Set("Session_ID", probeSessionID)
		req.Header.Set("Conversation_ID", probeSessionID)

		if isOAuth {
			req.Host = "chatgpt.com"
			setOpenAIChatGPTAccountHeaders(req.Header, account)
		}

		proxyURL := ""
		if account.ProxyID != nil && account.Proxy != nil {
			proxyURL = account.Proxy.URL()
		}

		req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
		resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
		if err != nil {
			if s.accountRepo != nil {
				updates := buildOpenAICompactProbeExtraUpdates(nil, nil, err, time.Now())
				_ = s.accountRepo.UpdateExtra(ctx, account.ID, updates)
				mergeAccountExtra(account, updates)
			}
			return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
		}
		defer func() { _ = resp.Body.Close() }()

		if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) && !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return s.sendErrorAndEnd(c, fmt.Sprintf("API returned unexpected HTTP status %d", resp.StatusCode))
		}

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if account.IsOpenAIAgentIdentity() && !agentIdentityTaskRecoveryTried &&
			isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, body) {
			_ = resp.Body.Close()
			if err := s.recoverAgentIdentityTask(ctx, account, expectedAgentIdentityTaskID); err != nil {
				return s.sendErrorAndEnd(c, fmt.Sprintf("Agent Identity task recovery failed: %s", err.Error()))
			}
			ctx = withAgentIdentitySensitiveValues(ctx, expectedAgentIdentityTaskID)
			agentIdentityTaskRecoveryTried = true
			continue
		}
		body = redactAgentIdentitySensitiveBodyForAccount(account, body, agentIdentitySensitiveValuesFromContext(ctx)...)

		if s.accountRepo != nil {
			updates := buildOpenAICompactProbeExtraUpdates(resp, body, nil, time.Now())
			if codexUpdates, err := extractOpenAICodexProbeUpdates(resp); err == nil && len(codexUpdates) > 0 {
				updates = mergeExtraUpdates(updates, codexUpdates)
			}
			if len(updates) > 0 {
				_ = s.accountRepo.UpdateExtra(ctx, account.ID, updates)
				mergeAccountExtra(account, updates)
			}
			// 探测如返回 429,主动同步限流状态,避免后续短时间内继续选中。
			if resp.StatusCode == http.StatusTooManyRequests {
				s.reconcileOpenAI429State(ctx, account, resp.Header, body)
			}
		}

		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusUnauthorized && s.accountRepo != nil {
				errMsg := fmt.Sprintf("Authentication failed (401): %s", string(body))
				_ = s.accountRepo.SetError(ctx, account.ID, errMsg)
			}
			return s.sendErrorAndEnd(c, fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body)))
		}

		s.sendEvent(c, TestEvent{Type: "content", Text: "Compact probe succeeded"})
		s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
		return nil
	}
}

func applyOpenAITestCodexHeaders(header http.Header, account *Account, accept string) {
	if header == nil {
		return
	}
	if strings.TrimSpace(accept) != "" {
		header.Set("Accept", accept)
	}
	header.Set("OpenAI-Beta", "responses=experimental")
	header.Set("Originator", "codex_cli_rs")
	header.Set("Version", codexCLIVersion)
	customUA := ""
	if account != nil {
		customUA = strings.TrimSpace(account.GetOpenAIUserAgent())
	}
	if customUA != "" {
		header.Set("User-Agent", customUA)
	} else {
		header.Set("User-Agent", codexCLIUserAgent)
	}
	if account != nil && account.IsOpenAIOAuth() {
		enforceCodexIdentityHeaders(header)
	}
}

func (s *AccountTestService) reconcileOpenAI429State(ctx context.Context, account *Account, headers http.Header, body []byte) {
	if s == nil || s.accountRepo == nil || account == nil {
		return
	}

	var resetAt *time.Time
	if calculated := calculateOpenAI429ResetTime(headers); calculated != nil {
		resetAt = calculated
	} else if unixTs := parseOpenAIRateLimitResetTime(body); unixTs != nil {
		t := time.Unix(*unixTs, 0)
		resetAt = &t
	}
	if resetAt == nil {
		return
	}

	if err := s.accountRepo.SetRateLimited(ctx, account.ID, *resetAt); err != nil {
		return
	}

	now := time.Now()
	account.RateLimitedAt = &now
	account.RateLimitResetAt = resetAt

	if account.Status == StatusError {
		if err := s.accountRepo.ClearError(ctx, account.ID); err != nil {
			return
		}
		account.Status = StatusActive
		account.ErrorMessage = ""
	}
}

// testGeminiAccountConnection tests a Gemini account's connection
func (s *AccountTestService) testGeminiAccountConnection(c *gin.Context, account *Account, modelID string, prompt string) error {
	ctx := c.Request.Context()

	// Determine the model to use
	testModelID := modelID
	if testModelID == "" {
		testModelID = geminicli.DefaultTestModel
	}

	// For static upstream credentials with model mapping, map the model
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mapping := account.GetModelMapping()
		if len(mapping) > 0 {
			if mappedModel, exists := mapping[testModelID]; exists {
				testModelID = mappedModel
			}
		}
	}

	// Set SSE headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	// Create test payload (Gemini format)
	payload := createGeminiTestPayload(testModelID, prompt)

	// Build request based on account type
	var req *http.Request
	var err error

	switch account.Type {
	case AccountTypeAPIKey:
		req, err = s.buildGeminiAPIKeyRequest(ctx, account, testModelID, payload)
	case AccountTypeOAuth:
		req, err = s.buildGeminiOAuthRequest(ctx, account, testModelID, payload)
	case AccountTypeServiceAccount:
		req, err = s.buildGeminiServiceAccountRequest(ctx, account, testModelID, payload)
	default:
		return s.sendErrorAndEnd(c, fmt.Sprintf("Unsupported account type: %s", account.Type))
	}

	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to build request: %s", err.Error()))
	}

	// Send test_start event
	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	// Get proxy and execute request
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) && !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned unexpected HTTP status %d", resp.StatusCode))
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body))
		// 与 Claude/OpenAI 测试路径以及运行时网关保持一致：
		// 认证失败(401)/被封禁或权限不足(403) 标记账号为 error，
		// 限流(429) 标记为 rate-limited，避免失效账号仍显示 active 被继续调度。
		s.reconcileGeminiTestErrorState(ctx, account, resp.StatusCode, resp.Header, body)
		return s.sendErrorAndEnd(c, errMsg)
	}

	// Process SSE stream
	return s.processGeminiStream(c, resp.Body)
}

// reconcileGeminiTestErrorState 在 Gemini 账号测试返回错误状态码时同步账号状态，
// 使管理端"测试连接"的结果能真正下线失效账号（对齐 Claude 403 / OpenAI 401 的行为）。
func (s *AccountTestService) reconcileGeminiTestErrorState(ctx context.Context, account *Account, statusCode int, headers http.Header, body []byte) {
	if s == nil || s.accountRepo == nil || account == nil {
		return
	}
	// 遵守账号自定义错误码策略：未命中则不处理，避免与运行时策略冲突。
	if !account.ShouldHandleErrorCode(statusCode) {
		return
	}

	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		errMsg := fmt.Sprintf("Account test failed (%d): %s", statusCode, string(body))
		_ = s.accountRepo.SetError(ctx, account.ID, errMsg)
	case http.StatusTooManyRequests:
		resetTime := time.Now().Add(5 * time.Minute)
		if ts := ParseGeminiRateLimitResetTime(body); ts != nil {
			resetTime = time.Unix(*ts, 0)
		}
		_ = s.accountRepo.SetRateLimited(ctx, account.ID, resetTime)
	}
	_ = headers
}

// routeAntigravityTest 路由 Antigravity 账号的测试请求。
// APIKey 类型走原生协议（与 gateway_handler 路由一致），OAuth/Upstream 走 CRS 中转。
func (s *AccountTestService) routeAntigravityTest(c *gin.Context, account *Account, modelID string, prompt string) error {
	if account.Type == AccountTypeAPIKey {
		if strings.HasPrefix(modelID, "gemini-") {
			return s.testGeminiAccountConnection(c, account, modelID, prompt)
		}
		return s.testClaudeAccountConnection(c, account, modelID)
	}
	return s.testAntigravityAccountConnection(c, account, modelID)
}

// testAntigravityAccountConnection tests an Antigravity account's connection
// 支持 Claude 和 Gemini 两种协议，使用非流式请求
func (s *AccountTestService) testAntigravityAccountConnection(c *gin.Context, account *Account, modelID string) error {
	ctx := c.Request.Context()

	// 默认模型：Claude 使用 claude-sonnet-4-5，Gemini 使用 gemini-3-pro-preview
	testModelID := modelID
	if testModelID == "" {
		testModelID = "claude-sonnet-4-5"
	}

	if s.antigravityGatewayService == nil {
		return s.sendErrorAndEnd(c, "Antigravity gateway service not configured")
	}

	// Set SSE headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	// Send test_start event
	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	// 调用 AntigravityGatewayService.TestConnection（复用协议转换逻辑）
	result, err := s.antigravityGatewayService.TestConnection(ctx, account, testModelID)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}

	// 发送响应内容
	if result.Text != "" {
		s.sendEvent(c, TestEvent{Type: "content", Text: result.Text})
	}

	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}

// buildGeminiAPIKeyRequest builds request for Gemini API Key accounts
func (s *AccountTestService) buildGeminiAPIKeyRequest(ctx context.Context, account *Account, modelID string, payload []byte) (*http.Request, error) {
	apiKey := account.GetCredential("api_key")
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("no API key available")
	}

	baseURL := account.GetCredential("base_url")
	if baseURL == "" {
		baseURL = geminicli.AIStudioBaseURL
	}
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}

	// Use streamGenerateContent for real-time feedback
	fullURL, err := buildGeminiAIStudioModelActionURL(normalizedBaseURL, modelID, "streamGenerateContent", true)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	return req, nil
}

// buildGeminiOAuthRequest builds request for Gemini OAuth accounts
func (s *AccountTestService) buildGeminiOAuthRequest(ctx context.Context, account *Account, modelID string, payload []byte) (*http.Request, error) {
	if s.geminiTokenProvider == nil {
		return nil, fmt.Errorf("gemini token provider not configured")
	}

	// Get access token (auto-refreshes if needed)
	accessToken, err := s.geminiTokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	projectID := strings.TrimSpace(account.GetCredential("project_id"))
	if projectID == "" {
		// AI Studio OAuth mode (no project_id): call generativelanguage API directly with Bearer token.
		baseURL := account.GetCredential("base_url")
		if strings.TrimSpace(baseURL) == "" {
			baseURL = geminicli.AIStudioBaseURL
		}
		normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return nil, err
		}
		fullURL, err := buildGeminiAIStudioModelActionURL(normalizedBaseURL, modelID, "streamGenerateContent", true)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)
		return req, nil
	}

	// Code Assist mode (with project_id)
	return s.buildCodeAssistRequest(ctx, accessToken, projectID, modelID, payload)
}

func (s *AccountTestService) buildGeminiServiceAccountRequest(ctx context.Context, account *Account, modelID string, payload []byte) (*http.Request, error) {
	if s.geminiTokenProvider == nil {
		return nil, fmt.Errorf("gemini token provider not configured")
	}
	accessToken, err := s.geminiTokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("failed to get service account access token: %w", err)
	}
	fullURL, err := buildVertexGeminiURL(account.VertexProjectID(), account.VertexLocation(modelID), modelID, "streamGenerateContent", true)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return req, nil
}

// buildCodeAssistRequest builds request for Google Code Assist API (used by Gemini CLI and Antigravity)
func (s *AccountTestService) buildCodeAssistRequest(ctx context.Context, accessToken, projectID, modelID string, payload []byte) (*http.Request, error) {
	var inner map[string]any
	if err := json.Unmarshal(payload, &inner); err != nil {
		return nil, err
	}

	wrapped := map[string]any{
		"model":   modelID,
		"project": projectID,
		"request": inner,
	}
	wrappedBytes, _ := json.Marshal(wrapped)

	normalizedBaseURL, err := s.validateUpstreamBaseURL(geminicli.GeminiCliBaseURL)
	if err != nil {
		return nil, err
	}
	fullURL := fmt.Sprintf("%s/v1internal:streamGenerateContent?alt=sse", normalizedBaseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewReader(wrappedBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", geminicli.GeminiCLIUserAgent)

	return req, nil
}

// createGeminiTestPayload creates a minimal text-only test payload for Gemini API.
func createGeminiTestPayload(modelID string, prompt string) []byte {
	_ = modelID

	textPrompt := strings.TrimSpace(prompt)
	if textPrompt == "" {
		textPrompt = defaultGeminiTextTestPrompt
	}

	payload := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": textPrompt},
				},
			},
		},
		"systemInstruction": map[string]any{
			"parts": []map[string]any{
				{"text": "You are a helpful AI assistant."},
			},
		},
	}
	bytes, _ := json.Marshal(payload)
	return bytes
}

// processGeminiStream processes SSE stream from Gemini API
func (s *AccountTestService) processGeminiStream(c *gin.Context, body io.Reader) error {
	reader := bufio.NewReader(body)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
				return nil
			}
			return s.sendErrorAndEnd(c, fmt.Sprintf("Stream read error: %s", err.Error()))
		}

		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		jsonStr := strings.TrimPrefix(line, "data: ")
		if jsonStr == "[DONE]" {
			s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
			return nil
		}

		var data map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			continue
		}

		// Support two Gemini response formats:
		// - AI Studio: {"candidates": [...]}
		// - Gemini CLI: {"response": {"candidates": [...]}}
		if resp, ok := data["response"].(map[string]any); ok && resp != nil {
			data = resp
		}
		if candidates, ok := data["candidates"].([]any); ok && len(candidates) > 0 {
			if candidate, ok := candidates[0].(map[string]any); ok {
				// Extract content first (before checking completion)
				if content, ok := candidate["content"].(map[string]any); ok {
					if parts, ok := content["parts"].([]any); ok {
						for _, part := range parts {
							if partMap, ok := part.(map[string]any); ok {
								if text, ok := partMap["text"].(string); ok && text != "" {
									s.sendEvent(c, TestEvent{Type: "content", Text: text})
								}
								if inlineData, ok := partMap["inlineData"].(map[string]any); ok {
									mimeType, _ := inlineData["mimeType"].(string)
									data, _ := inlineData["data"].(string)
									if strings.HasPrefix(strings.ToLower(mimeType), "image/") && data != "" {
										s.sendEvent(c, TestEvent{
											Type:     "image",
											ImageURL: fmt.Sprintf("data:%s;base64,%s", mimeType, data),
											MimeType: mimeType,
										})
									}
								}
							}
						}
					}
				}

				// Check for completion after extracting content
				if finishReason, ok := candidate["finishReason"].(string); ok && finishReason != "" {
					s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
					return nil
				}
			}
		}

		// Handle errors
		if errData, ok := data["error"].(map[string]any); ok {
			errorMsg := "Unknown error"
			if msg, ok := errData["message"].(string); ok {
				errorMsg = msg
			}
			return s.sendErrorAndEnd(c, errorMsg)
		}
	}
}

// createOpenAITestPayload creates a test payload for OpenAI Responses API
func createOpenAITestPayload(modelID string, isOAuth bool) map[string]any {
	payload := map[string]any{
		"model": modelID,
		"input": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "input_text",
						"text": "hi",
					},
				},
			},
		},
		"stream": true,
	}

	// OAuth accounts using ChatGPT internal API require store=false.
	if isOAuth {
		payload["store"] = false
	}

	// All accounts require instructions for Responses API
	payload["instructions"] = openai.DefaultInstructions

	return payload
}

func createGrokTestPayload(modelID string, prompt string) map[string]any {
	return createGrokProbePayload(modelID, prompt)
}

// processClaudeStream processes the SSE stream from Claude API
func (s *AccountTestService) processClaudeStream(c *gin.Context, body io.Reader) error {
	reader := bufio.NewReader(body)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
				return nil
			}
			return s.sendErrorAndEnd(c, fmt.Sprintf("Stream read error: %s", err.Error()))
		}

		line = strings.TrimSpace(line)
		if line == "" || !sseDataPrefix.MatchString(line) {
			continue
		}

		jsonStr := sseDataPrefix.ReplaceAllString(line, "")
		if jsonStr == "[DONE]" {
			s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
			return nil
		}

		var data map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			continue
		}

		eventType, _ := data["type"].(string)

		switch eventType {
		case "content_block_delta":
			if delta, ok := data["delta"].(map[string]any); ok {
				if text, ok := delta["text"].(string); ok {
					s.sendEvent(c, TestEvent{Type: "content", Text: text})
				}
			}
		case "message_stop":
			s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
			return nil
		case "error":
			errorMsg := "Unknown error"
			if errData, ok := data["error"].(map[string]any); ok {
				if msg, ok := errData["message"].(string); ok {
					errorMsg = msg
				}
			}
			return s.sendErrorAndEnd(c, errorMsg)
		}
	}
}

// processOpenAIStream processes the SSE stream from OpenAI Responses API
func (s *AccountTestService) processOpenAIStream(c *gin.Context, body io.Reader) error {
	reader := bufio.NewReader(body)
	seenCompleted := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if seenCompleted {
					s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
					return nil
				}
				return s.sendErrorAndEnd(c, "Stream ended before response.completed")
			}
			return s.sendErrorAndEnd(c, fmt.Sprintf("Stream read error: %s", err.Error()))
		}

		line = strings.TrimSpace(line)
		if line == "" || !sseDataPrefix.MatchString(line) {
			continue
		}

		jsonStr := sseDataPrefix.ReplaceAllString(line, "")
		if jsonStr == "[DONE]" {
			if seenCompleted {
				s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
				return nil
			}
			return s.sendErrorAndEnd(c, "Stream ended before response.completed")
		}

		var data map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			continue
		}

		eventType, _ := data["type"].(string)

		switch eventType {
		case "response.output_text.delta":
			// OpenAI Responses API uses "delta" field for text content
			if delta, ok := data["delta"].(string); ok && delta != "" {
				s.sendEvent(c, TestEvent{Type: "content", Text: delta})
			}
		case "response.completed", "response.done":
			s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
			return nil
		case "response.incomplete":
			// Defensive: upstream may end a probe with response.incomplete
			// instead of response.completed (e.g. reasoning models hitting an
			// output token cap). Surface the reason so the failure is legible.
			// Per the Responses API wire shape the details are nested inside
			// the `response` object (same place response.failed reads its error);
			// fall back to the top-level field to tolerate odd placements.
			reason := ""
			if responseData, ok := data["response"].(map[string]any); ok {
				if incompleteDetails, ok := responseData["incomplete_details"].(map[string]any); ok {
					reason, _ = incompleteDetails["reason"].(string)
				}
			}
			if reason == "" {
				if incompleteDetails, ok := data["incomplete_details"].(map[string]any); ok {
					reason, _ = incompleteDetails["reason"].(string)
				}
			}
			if reason == "" {
				reason = "unknown"
			}
			return s.sendErrorAndEnd(c, fmt.Sprintf("OpenAI response incomplete (reason: %s)", reason))
		case "response.failed":
			errorMsg := "OpenAI response failed"
			if responseData, ok := data["response"].(map[string]any); ok {
				if errData, ok := responseData["error"].(map[string]any); ok {
					if msg, ok := errData["message"].(string); ok && msg != "" {
						errorMsg = msg
					}
				}
			}
			return s.sendErrorAndEnd(c, errorMsg)
		case "error":
			errorMsg := "Unknown error"
			if errData, ok := data["error"].(map[string]any); ok {
				if msg, ok := errData["message"].(string); ok {
					errorMsg = msg
				}
			}
			return s.sendErrorAndEnd(c, errorMsg)
		}
	}
}

// testOpenAIImageAPIKey tests OpenAI image generation using an API Key account.
func (s *AccountTestService) testOpenAIImageAPIKey(c *gin.Context, ctx context.Context, account *Account, modelID, prompt string) error {
	authToken := account.GetOpenAIApiKey()
	if authToken == "" {
		return s.sendErrorAndEnd(c, "No API key available")
	}

	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Invalid base URL: %s", err.Error()))
	}
	apiURL := buildOpenAIImagesURL(normalizedBaseURL, openAIImagesGenerationsEndpoint)

	// Set SSE headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	s.sendEvent(c, TestEvent{Type: "test_start", Model: modelID})

	payload := map[string]any{
		"model":  modelID,
		"prompt": prompt,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) && !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned unexpected HTTP status %d", resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to read response: %s", err.Error()))
	}

	if resp.StatusCode != http.StatusOK {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body)))
	}

	// Parse {"data": [{"b64_json": "...", "revised_prompt": "..."}]}
	var result struct {
		Data []struct {
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to parse response: %s", err.Error()))
	}

	if len(result.Data) == 0 {
		return s.sendErrorAndEnd(c, "No images returned from API")
	}

	for _, item := range result.Data {
		if item.RevisedPrompt != "" {
			s.sendEvent(c, TestEvent{Type: "content", Text: item.RevisedPrompt})
		}
		if item.B64JSON != "" {
			s.sendEvent(c, TestEvent{
				Type:     "image",
				ImageURL: "data:image/png;base64," + item.B64JSON,
				MimeType: "image/png",
			})
		}
	}

	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}

// testOpenAIImageOAuth tests OpenAI image generation using an OAuth account via Codex /responses API.
func (s *AccountTestService) testOpenAIImageOAuth(c *gin.Context, ctx context.Context, account *Account, modelID, prompt string) error {
	authToken := account.GetOpenAIAccessToken()
	if authToken == "" && !account.IsOpenAIAgentIdentity() {
		return s.sendErrorAndEnd(c, "No access token available")
	}

	// Set SSE headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	s.sendEvent(c, TestEvent{Type: "test_start", Model: modelID})
	s.sendEvent(c, TestEvent{Type: "content", Text: "Calling Codex /responses image tool...\n"})

	parsed := &OpenAIImagesRequest{
		Endpoint: openAIImagesGenerationsEndpoint,
		Model:    strings.TrimSpace(modelID),
		Prompt:   prompt,
	}
	applyOpenAIImagesDefaults(parsed)

	responsesBody, err := buildOpenAIImagesResponsesRequest(parsed, parsed.Model)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to build image request: %s", err.Error()))
	}

	agentIdentityTaskRecoveryTried := false
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexAPIURL, bytes.NewReader(responsesBody))
		if err != nil {
			return s.sendErrorAndEnd(c, "Failed to create request")
		}
		req.Host = "chatgpt.com"
		authHeaders, err := s.buildOpenAIAuthenticationHeaders(ctx, account, authToken)
		if err != nil {
			return s.sendErrorAndEnd(c, err.Error())
		}
		for key, values := range authHeaders {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
		expectedAgentIdentityTaskID := strings.TrimSpace(account.GetCredential("task_id"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		req.Header.Set("originator", "opencode")
		if customUA := strings.TrimSpace(account.GetOpenAIUserAgent()); customUA != "" {
			req.Header.Set("User-Agent", customUA)
		} else {
			req.Header.Set("User-Agent", codexCLIUserAgent)
		}
		setOpenAIChatGPTAccountHeaders(req.Header, account)
		account.ApplyHeaderOverrides(req.Header)
		if account.IsOpenAIAgentIdentity() {
			req.Header.Set("Authorization", authHeaders.Get("Authorization"))
		}
		enforceCodexIdentityHeaders(req.Header)

		proxyURL := ""
		if account.ProxyID != nil && account.Proxy != nil {
			proxyURL = account.Proxy.URL()
		}
		req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
		resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Responses API request failed: %s", err.Error()))
		}
		defer func() {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
		}()
		if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) && !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Responses API returned unexpected HTTP status %d", resp.StatusCode))
		}
		if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			if account.IsOpenAIAgentIdentity() && !agentIdentityTaskRecoveryTried &&
				isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, body) {
				_ = resp.Body.Close()
				if err := s.recoverAgentIdentityTask(ctx, account, expectedAgentIdentityTaskID); err != nil {
					return s.sendErrorAndEnd(c, fmt.Sprintf("Agent Identity task recovery failed: %s", err.Error()))
				}
				ctx = withAgentIdentitySensitiveValues(ctx, expectedAgentIdentityTaskID)
				agentIdentityTaskRecoveryTried = true
				continue
			}
			body = redactAgentIdentitySensitiveBodyForAccount(account, body, agentIdentitySensitiveValuesFromContext(ctx)...)
			message := strings.TrimSpace(extractUpstreamErrorMessage(body))
			if message == "" {
				message = fmt.Sprintf("Responses API returned %d", resp.StatusCode)
			}
			return s.sendErrorAndEnd(c, message)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to read image response: %s", err.Error()))
		}
		results, _, _, _, _, err := collectOpenAIImagesFromResponsesBody(body)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to parse image response: %s", err.Error()))
		}
		if len(results) == 0 {
			return s.sendErrorAndEnd(c, "No images returned from responses API")
		}
		for _, item := range results {
			if item.RevisedPrompt != "" {
				s.sendEvent(c, TestEvent{Type: "content", Text: item.RevisedPrompt})
			}
			mimeType := openAIImageOutputMIMEType(item.OutputFormat)
			s.sendEvent(c, TestEvent{
				Type:     "image",
				ImageURL: "data:" + mimeType + ";base64," + item.Result,
				MimeType: mimeType,
			})
		}
		s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
		return nil
	}
}

func (s *AccountTestService) sendEvent(c *gin.Context, event TestEvent) {
	eventJSON, _ := json.Marshal(event)
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", eventJSON); err != nil {
		log.Printf("failed to write SSE event: %v", err)
		return
	}
	c.Writer.Flush()
}

// sendErrorAndEnd sends an error event and ends the stream
func (s *AccountTestService) sendErrorAndEnd(c *gin.Context, errorMsg string) error {
	log.Printf("Account test error: %s", errorMsg)
	s.sendEvent(c, TestEvent{Type: "error", Error: errorMsg})
	return fmt.Errorf("%s", errorMsg)
}

// RunTestBackground executes an account test in-memory (no real HTTP client),
// capturing SSE output via httptest.NewRecorder, then parses the result.
func (s *AccountTestService) RunTestBackground(ctx context.Context, accountID int64, modelID string) (*ScheduledTestResult, error) {
	startedAt := time.Now()

	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = (&http.Request{}).WithContext(ctx)

	testErr := s.TestAccountConnection(ginCtx, accountID, modelID, "", AccountTestModeDefault)

	finishedAt := time.Now()
	body := w.Body.String()
	responseText, errMsg := parseTestSSEOutput(body)

	status := "success"
	if testErr != nil || errMsg != "" {
		status = "failed"
		if errMsg == "" && testErr != nil {
			errMsg = testErr.Error()
		}
	}

	return &ScheduledTestResult{
		Status:       status,
		ResponseText: responseText,
		ErrorMessage: errMsg,
		LatencyMs:    finishedAt.Sub(startedAt).Milliseconds(),
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
	}, nil
}

// parseTestSSEOutput extracts response text and error message from captured SSE output.
func parseTestSSEOutput(body string) (responseText, errMsg string) {
	var texts []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonStr := strings.TrimPrefix(line, "data: ")
		var event TestEvent
		if err := json.Unmarshal([]byte(jsonStr), &event); err != nil {
			continue
		}
		switch event.Type {
		case "content":
			if event.Text != "" {
				texts = append(texts, event.Text)
			}
		case "error":
			errMsg = event.Error
		}
	}
	responseText = strings.Join(texts, "")
	return
}
