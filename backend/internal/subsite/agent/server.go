package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	masterclient "github.com/Wei-Shaw/sub2api/internal/subsite/client"
	"github.com/Wei-Shaw/sub2api/internal/subsite/queue"
	"github.com/gin-gonic/gin"
)

type Server struct {
	cfg    *Config
	master *masterclient.MasterClient
	queue  *queue.UsageQueue
	engine *gin.Engine
}

func NewServer(cfg *Config, master *masterclient.MasterClient, usageQueue *queue.UsageQueue) *Server {
	s := &Server{
		cfg:    cfg,
		master: master,
		queue:  usageQueue,
		engine: gin.New(),
	}
	s.registerRoutes()
	return s
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
	authorization, err := s.authorizeRequest(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"code":    "SUBSITE_AUTHORIZE_FAILED",
				"message": err.Error(),
			},
		})
		return
	}
	if authorization != nil && authorization.RequestID != "" {
		_ = s.cancelReservation(c.Request.Context(), authorization.RequestID)
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": gin.H{
			"code":    "SUBSITE_PROXY_NOT_WIRED",
			"message": "subsite request was authorized, but upstream proxy execution is not wired yet",
		},
	})
}

func (s *Server) authorizeRequest(c *gin.Context) (*service.AuthorizeSubsiteResponse, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	apiKey := extractClientAPIKey(c)
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}
	estimatedCost, err := parseEstimatedCost(c)
	if err != nil {
		return nil, err
	}
	requestedModel := extractRequestedModel(c, body)
	input := service.AuthorizeSubsiteRequestInput{
		SubsiteID:          s.cfg.Subsite.ID,
		APIKey:             apiKey,
		Platform:           platformForPath(c.Request.URL.Path),
		RequestedModel:     requestedModel,
		MappedModel:        requestedModel,
		EstimatedCost:      estimatedCost,
		RequestFingerprint: requestFingerprint(c.Request.Method, c.Request.URL.Path, body),
		ClientIP:           clientIP(c),
		UserAgent:          c.GetHeader("User-Agent"),
		InboundEndpoint:    c.Request.URL.Path,
	}
	return s.master.Authorize(c.Request.Context(), input)
}

func (s *Server) cancelReservation(ctx context.Context, requestID string) error {
	payload := map[string]string{"request_id": requestID}
	return s.master.PostRaw(ctx, "/api/internal/requests/cancel", payload, nil)
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

func parseEstimatedCost(c *gin.Context) (float64, error) {
	value := strings.TrimSpace(c.GetHeader("X-Sub2API-Estimated-Cost"))
	if value == "" {
		return 0, fmt.Errorf("X-Sub2API-Estimated-Cost is required until subsite pricing estimator is wired")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("X-Sub2API-Estimated-Cost must be greater than zero")
	}
	return parsed, nil
}

func extractRequestedModel(c *gin.Context, body []byte) string {
	var payload struct {
		Model string `json:"model"`
	}
	if len(body) > 0 && json.Unmarshal(body, &payload) == nil && strings.TrimSpace(payload.Model) != "" {
		return strings.TrimSpace(payload.Model)
	}
	if value := strings.TrimSpace(c.Query("model")); value != "" {
		return value
	}
	return ""
}

func requestFingerprint(method, path string, body []byte) string {
	sum := sha256.Sum256(body)
	raw := strings.ToUpper(strings.TrimSpace(method)) + "|" + strings.TrimSpace(path) + "|" + hex.EncodeToString(sum[:])
	final := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(final[:])
}

func platformForPath(path string) string {
	switch {
	case strings.HasPrefix(path, "/v1beta/"):
		return service.PlatformGemini
	case strings.Contains(path, "/chat/completions"), strings.Contains(path, "/responses"), strings.Contains(path, "/images/"):
		return service.PlatformOpenAI
	default:
		return service.PlatformAnthropic
	}
}

func clientIP(c *gin.Context) string {
	if value := strings.TrimSpace(c.GetHeader("CF-Connecting-IP")); value != "" {
		return value
	}
	if value := strings.TrimSpace(c.GetHeader("X-Real-IP")); value != "" {
		return value
	}
	if value := strings.TrimSpace(c.GetHeader("X-Forwarded-For")); value != "" {
		parts := strings.Split(value, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	return c.ClientIP()
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
	return s.master.Heartbeat(ctx, serviceHeartbeatInput(s.cfg, depth))
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
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		payloads = append(payloads, item.Payload)
		ids = append(ids, item.ID)
	}
	result, err := s.master.UsageBatch(ctx, service.UsageIngestBatch{
		SubsiteID: s.cfg.Subsite.ID,
		Items:     payloads,
	})
	if err != nil {
		return err
	}
	if result.Applied+result.Duplicate == len(items) {
		return s.queue.Ack(ctx, ids)
	}
	return fmt.Errorf("usage batch partially accepted: applied=%d duplicate=%d total=%d", result.Applied, result.Duplicate, len(items))
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

func serviceHeartbeatInput(cfg *Config, queuedUsage int) service.SubsiteHeartbeatInput {
	return service.SubsiteHeartbeatInput{
		SubsiteID:      cfg.Subsite.ID,
		Status:         service.SubsiteStatusActive,
		Version:        cfg.Version,
		QueuedUsage:    queuedUsage,
		RemoteIP:       cfg.Subsite.PublicURL,
		ReportedAt:     time.Now(),
		ActiveRequests: 0,
		Metadata: map[string]any{
			"public_url": cfg.Subsite.PublicURL,
		},
	}
}
