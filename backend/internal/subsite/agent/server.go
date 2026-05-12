package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	masterclient "github.com/Wei-Shaw/sub2api/internal/subsite/client"
	"github.com/Wei-Shaw/sub2api/internal/subsite/queue"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type Server struct {
	cfg         *Config
	master      *masterclient.MasterClient
	queue       *queue.UsageQueue
	credentials *CredentialCache
	engine      *gin.Engine
}

func NewServer(cfg *Config, master *masterclient.MasterClient, usageQueue *queue.UsageQueue) *Server {
	s := &Server{
		cfg:         cfg,
		master:      master,
		queue:       usageQueue,
		credentials: NewCredentialCache(2 * time.Minute),
		engine:      gin.New(),
	}
	configureTrustedProxies(s.engine, cfg.TrustedProxies)
	s.registerRoutes()
	return s
}

func configureTrustedProxies(engine *gin.Engine, trustedProxies []string) {
	if engine == nil {
		return
	}
	engine.TrustedPlatform = ""
	engine.RemoteIPHeaders = []string{"CF-Connecting-IP", "X-Forwarded-For", "X-Real-IP"}
	if len(trustedProxies) == 0 {
		_ = engine.SetTrustedProxies(nil)
		return
	}
	if err := engine.SetTrustedProxies(trustedProxies); err != nil {
		panic(fmt.Sprintf("invalid trusted_proxies: %v", err))
	}
}

func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           s.engine,
		ReadHeaderTimeout: 15 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	go s.heartbeatLoop(ctx)
	go s.usageFlushLoop(ctx)
	go s.credentials.StartCleanup(ctx)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
}

func (s *Server) registerRoutes() {
	s.engine.Use(gin.Recovery())
	s.engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	s.engine.GET("/readyz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ready", "subsite_id": s.cfg.Subsite.ID})
	})

	dataPlane := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/models"},
		{http.MethodGet, "/models"},
		{http.MethodPost, "/v1/messages"},
		{http.MethodPost, "/v1/responses"},
		{http.MethodPost, "/responses"},
		{http.MethodGet, "/v1/responses"},
		{http.MethodGet, "/responses"},
		{http.MethodPost, "/backend-api/codex/responses"},
		{http.MethodPost, "/v1/chat/completions"},
		{http.MethodPost, "/chat/completions"},
		{http.MethodPost, "/v1/images/generations"},
		{http.MethodPost, "/images/generations"},
		{http.MethodPost, "/v1/images/edits"},
		{http.MethodPost, "/images/edits"},
		{http.MethodPost, "/v1beta/models/*path"},
		{http.MethodGet, "/v1beta/models/*path"},
	}
	for _, route := range dataPlane {
		s.engine.Handle(route.method, route.path, s.handleDataPlane)
	}
}

func (s *Server) handleDataPlane(c *gin.Context) {
	if isOpenAIModelsEndpoint(c.Request.URL.Path) {
		s.handleOpenAIModels(c)
		return
	}
	if isWebSocketRequest(c) {
		s.handleResponsesWebSocket(c)
		return
	}
	authorization, body, err := s.authorizeRequest(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"code":    "SUBSITE_AUTHORIZE_FAILED",
				"message": err.Error(),
			},
		})
		return
	}
	s.credentials.Set(authorization)

	result, err := s.proxyAuthorizedRequest(c, authorization, body)
	if err != nil {
		retryAuthorization, retryErr := s.retryAuthorizedRequest(c, authorization, body, err)
		if retryErr != nil {
			err = retryErr
		} else if retryAuthorization != nil {
			s.credentials.Set(retryAuthorization)
			result, err = s.proxyAuthorizedRequest(c, retryAuthorization, body)
			authorization = retryAuthorization
			if err == nil {
				goto enqueue
			}
		}
		if authorization.RequestID != "" {
			_ = s.cancelReservation(c.Request.Context(), authorization.RequestID)
		}
		s.credentials.Delete(authorization.RequestID)
		if upstreamErr := (*upstreamError)(nil); errors.As(err, &upstreamErr) && upstreamErr != nil && !c.Writer.Written() {
			contentType := upstreamErr.contentType
			if contentType == "" {
				contentType = "application/json"
			}
			c.Data(upstreamErr.statusCode, contentType, upstreamErr.body)
			return
		}
		if !c.Writer.Written() {
			c.JSON(http.StatusBadGateway, gin.H{
				"error": gin.H{
					"code":    "SUBSITE_PROXY_FAILED",
					"message": err.Error(),
				},
			})
		}
		return
	}
enqueue:
	defer s.credentials.Delete(authorization.RequestID)
	if result != nil && hasUsagePayload(result.Usage) {
		if err := s.queue.Enqueue(c.Request.Context(), result.Usage); err != nil {
			if authorization.RequestID != "" {
				_ = s.cancelReservation(c.Request.Context(), authorization.RequestID)
			}
			s.credentials.Delete(authorization.RequestID)
			if !c.Writer.Written() {
				c.JSON(http.StatusBadGateway, gin.H{
					"error": gin.H{
						"code":    "SUBSITE_USAGE_QUEUE_FAILED",
						"message": err.Error(),
					},
				})
			}
			return
		}
	}
}

func (s *Server) authorizeRequest(c *gin.Context) (*service.AuthorizeSubsiteResponse, []byte, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read request body: %w", err)
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	apiKey := extractClientAPIKey(c)
	if apiKey == "" {
		return nil, nil, fmt.Errorf("api key is required")
	}
	requestedModel := extractRequestedModel(c, body)
	input := service.AuthorizeSubsiteRequestInput{
		SubsiteID:          s.cfg.Subsite.ID,
		APIKey:             apiKey,
		Platform:           platformForPath(c.Request.URL.Path),
		RequestedModel:     requestedModel,
		MappedModel:        requestedModel,
		RequestFingerprint: requestFingerprint(c.Request.Method, c.Request.URL.Path, body),
		ClientIP:           clientIP(c),
		UserAgent:          c.GetHeader("User-Agent"),
		InboundEndpoint:    c.Request.URL.Path,
	}
	if err := populateAuthorizeCostHints(&input, c, body); err != nil {
		return nil, nil, err
	}
	authorization, err := s.master.Authorize(c.Request.Context(), input)
	if err != nil {
		return nil, nil, err
	}
	return authorization, body, nil
}

func (s *Server) cancelReservation(ctx context.Context, requestID string) error {
	payload := map[string]string{"request_id": requestID}
	return s.master.PostRaw(ctx, "/api/internal/requests/cancel", payload, nil)
}

func (s *Server) retryAuthorizedRequest(c *gin.Context, authorization *service.AuthorizeSubsiteResponse, body []byte, proxyErr error) (*service.AuthorizeSubsiteResponse, error) {
	if s == nil || authorization == nil {
		return nil, proxyErr
	}
	if !shouldFailoverForUpstreamError(proxyErr) {
		return nil, proxyErr
	}
	if authorization.RequestID != "" {
		_ = s.cancelReservation(c.Request.Context(), authorization.RequestID)
	}
	s.credentials.Delete(authorization.RequestID)

	apiKey := extractClientAPIKey(c)
	if apiKey == "" {
		return nil, proxyErr
	}
	requestedModel := extractRequestedModel(c, body)
	input := service.AuthorizeSubsiteRequestInput{
		SubsiteID:          s.cfg.Subsite.ID,
		APIKey:             apiKey,
		Platform:           platformForPath(c.Request.URL.Path),
		RequestedModel:     requestedModel,
		MappedModel:        requestedModel,
		RequestFingerprint: requestFingerprint(c.Request.Method, c.Request.URL.Path, body),
		ClientIP:           clientIP(c),
		UserAgent:          c.GetHeader("User-Agent"),
		InboundEndpoint:    c.Request.URL.Path,
		ExcludedLeaseIDs:   []string{authorization.LeaseID},
		ExcludedAccountIDs: []int64{authorization.AccountID},
	}
	if err := populateAuthorizeCostHints(&input, c, body); err != nil {
		return nil, err
	}
	return s.master.Authorize(c.Request.Context(), input)
}

func extractClientAPIKey(c *gin.Context) string {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}
	for _, header := range []string{"x-api-key", "x-goog-api-key"} {
		if value := strings.TrimSpace(c.GetHeader(header)); value != "" {
			return value
		}
	}
	return ""
}

func populateAuthorizeCostHints(input *service.AuthorizeSubsiteRequestInput, c *gin.Context, body []byte) error {
	if input == nil {
		return fmt.Errorf("authorize input is required")
	}
	input.EstimatedCost = 0
	input.EstimatedInputTokens = estimateInputTokensFromBody(body)
	input.EstimatedOutputTokens = extractMaxOutputTokens(body)
	input.ServiceTier = extractStringFromJSON(body, "service_tier")
	input.ReasoningEffort = extractReasoningEffort(body)
	if isImageEndpoint(c.Request.URL.Path) {
		count, size := parseImageRequest(c, body)
		input.EstimatedImageCount = count
		input.EstimatedImageSize = normalizeAuthorizeImageSize(size)
	} else if input.EstimatedOutputTokens <= 0 {
		input.EstimatedOutputTokens = service.DefaultSubsiteEstimatedOutputTokens
		if strings.Contains(strings.ToLower(c.Request.URL.Path), "responses") {
			input.EstimatedOutputTokens = service.DefaultSubsiteEstimatedUnboundedOutputTokens
		}
	}
	return nil
}

func estimateInputTokensFromBody(body []byte) int {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return service.DefaultSubsiteEstimatedInputTokens
	}
	estimated := len(trimmed)/3 + 512
	if estimated < 1 {
		return 1
	}
	return estimated
}

func extractMaxOutputTokens(body []byte) int {
	if len(bytes.TrimSpace(body)) == 0 || !gjson.ValidBytes(body) {
		return 0
	}
	paths := []string{
		"max_output_tokens",
		"max_completion_tokens",
		"max_tokens",
		"generationConfig.maxOutputTokens",
		"generation_config.max_output_tokens",
	}
	for _, path := range paths {
		if value := int(gjson.GetBytes(body, path).Int()); value > 0 {
			return value
		}
	}
	return 0
}

func extractStringFromJSON(body []byte, path string) string {
	if len(bytes.TrimSpace(body)) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	return strings.TrimSpace(gjson.GetBytes(body, path).String())
}

func extractReasoningEffort(body []byte) string {
	if value := extractStringFromJSON(body, "reasoning.effort"); value != "" {
		return value
	}
	return extractStringFromJSON(body, "reasoning_effort")
}

func normalizeAuthorizeImageSize(size string) string {
	value := strings.ToLower(strings.TrimSpace(size))
	switch value {
	case "256x256", "512x512", "1024x1024", "1k":
		return "1K"
	case "1024x1536", "1536x1024", "1024x1792", "1792x1024", "2k":
		return "2K"
	case "2048x2048", "4k":
		return "4K"
	case "hd":
		return "HD"
	default:
		return ""
	}
}

func extractRequestedModel(c *gin.Context, body []byte) string {
	var payload struct {
		Model string `json:"model"`
	}
	if len(body) > 0 && json.Unmarshal(body, &payload) == nil && strings.TrimSpace(payload.Model) != "" {
		return strings.TrimSpace(payload.Model)
	}
	if isMultipartRequest(c) {
		if model := strings.TrimSpace(extractMultipartField(c.GetHeader("Content-Type"), body, "model")); model != "" {
			return model
		}
	}
	if model := extractGeminiModelFromPath(c.Request.URL.Path); model != "" {
		return model
	}
	if value := strings.TrimSpace(c.Query("model")); value != "" {
		return value
	}
	return ""
}

func extractGeminiModelFromPath(path string) string {
	if !strings.HasPrefix(path, "/v1beta/models/") {
		return ""
	}
	modelAction := strings.TrimPrefix(path, "/v1beta/models/")
	if idx := strings.Index(modelAction, ":"); idx >= 0 {
		modelAction = modelAction[:idx]
	}
	return strings.TrimSpace(modelAction)
}

func requestFingerprint(method, path string, body []byte) string {
	sum := sha256.Sum256(body)
	raw := strings.ToUpper(strings.TrimSpace(method)) + "|" + strings.TrimSpace(path) + "|" + hex.EncodeToString(sum[:])
	final := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(final[:])
}

func platformForPath(path string) string {
	switch {
	case isOpenAIModelsEndpoint(path):
		return service.PlatformOpenAI
	case strings.HasPrefix(path, "/v1beta/"):
		return service.PlatformGemini
	case strings.Contains(path, "/chat/completions"), strings.Contains(path, "/responses"), strings.Contains(path, "/images/"):
		return service.PlatformOpenAI
	default:
		return service.PlatformAnthropic
	}
}

func clientIP(c *gin.Context) string {
	return c.ClientIP()
}

func isWebSocketRequest(c *gin.Context) bool {
	if strings.EqualFold(c.Request.Method, http.MethodGet) && strings.Contains(c.Request.URL.Path, "/responses") {
		return true
	}
	return strings.Contains(strings.ToLower(c.GetHeader("Connection")), "upgrade") ||
		strings.Contains(strings.ToLower(c.GetHeader("Upgrade")), "websocket")
}

func isStreamRequest(c *gin.Context, body []byte) bool {
	if strings.Contains(strings.ToLower(c.GetHeader("Accept")), "text/event-stream") {
		return true
	}
	if strings.EqualFold(c.Query("stream"), "true") {
		return true
	}
	var payload struct {
		Stream bool `json:"stream"`
	}
	return len(body) > 0 && json.Unmarshal(body, &payload) == nil && payload.Stream
}

func isImageEndpoint(path string) bool {
	return strings.Contains(path, "/images/")
}

func isOpenAIModelsEndpoint(path string) bool {
	normalized := strings.TrimRight(strings.TrimSpace(path), "/")
	return normalized == "/v1/models" || normalized == "/models"
}

func isGeminiEndpoint(path string) bool {
	return strings.HasPrefix(path, "/v1beta/")
}

func (s *Server) handleOpenAIModels(c *gin.Context) {
	if extractClientAPIKey(c) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"type":    "authentication_error",
				"message": "Invalid API key",
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   openai.DefaultModels,
	})
}

func hasUsagePayload(item service.UsageIngestItem) bool {
	return strings.TrimSpace(item.RequestID) != "" &&
		strings.TrimSpace(item.ReservationID) != "" &&
		strings.TrimSpace(item.RequestFingerprint) != ""
}

func (s *Server) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.sendHeartbeat(ctx); err != nil {
			fmt.Printf("subsite heartbeat failed: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) sendHeartbeat(ctx context.Context) error {
	depth, err := s.queue.Depth(ctx)
	if err != nil {
		return err
	}
	return s.master.Heartbeat(ctx, serviceHeartbeatInput(s.cfg, depth, s.credentials.ActiveCount()))
}

func (s *Server) usageFlushLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.flushUsage(ctx); err != nil {
			fmt.Printf("subsite usage flush failed: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) flushUsage(ctx context.Context) error {
	items, err := s.queue.DequeueBatch(ctx, 100)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	payloads := make([]service.UsageIngestItem, 0, len(items))
	for _, item := range items {
		payloads = append(payloads, item.Payload)
	}
	result, err := s.master.UsageBatch(ctx, service.UsageIngestBatch{
		SubsiteID: s.cfg.Subsite.ID,
		Items:     payloads,
	})
	if err != nil {
		return err
	}
	if result == nil || len(result.Items) == 0 {
		return fmt.Errorf("usage batch missing item results")
	}
	if len(result.Items) != len(items) {
		return fmt.Errorf("usage batch result length mismatch: accepted=%d total=%d", len(result.Items), len(items))
	}
	ackIDs := make([]int64, 0, len(items))
	failed := 0
	for i, itemResult := range result.Items {
		if itemResult.Applied || itemResult.Duplicate {
			ackIDs = append(ackIDs, items[i].ID)
			continue
		}
		failed++
	}
	if len(ackIDs) > 0 {
		if err := s.queue.Ack(ctx, ackIDs); err != nil {
			return err
		}
	}
	if failed > 0 {
		return fmt.Errorf("usage batch partially accepted: applied=%d duplicate=%d failed=%d total=%d", result.Applied, result.Duplicate, failed, len(items))
	}
	return nil
}

func Run(ctx context.Context, cfg *Config) error {
	master := masterclient.NewMasterClient(cfg.Master.BaseURL, cfg.Subsite.ID, cfg.Master.Secret)
	usageQueue, err := queue.Open(cfg.Queue.Path)
	if err != nil {
		return err
	}
	defer usageQueue.Close()
	server := NewServer(cfg, master, usageQueue)
	return server.Run(ctx)
}

func serviceHeartbeatInput(cfg *Config, queuedUsage, activeRequests int) service.SubsiteHeartbeatInput {
	return service.SubsiteHeartbeatInput{
		SubsiteID:      cfg.Subsite.ID,
		Status:         service.SubsiteStatusActive,
		Version:        cfg.Version,
		QueuedUsage:    queuedUsage,
		RemoteIP:       cfg.Subsite.PublicURL,
		ReportedAt:     time.Now(),
		ActiveRequests: activeRequests,
		Metadata: map[string]any{
			"public_url": cfg.Subsite.PublicURL,
		},
	}
}
