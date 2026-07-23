// Package server provides HTTP server initialization and configuration.
package server

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/http2"
)

// ProviderSet 提供服务器层的依赖
var ProviderSet = wire.NewSet(
	ProvideRouter,
	ProvideHTTPServer,
)

// ProvideRouter 提供路由器
func ProvideRouter(
	cfg *config.Config,
	handlers *handler.Handlers,
	jwtAuth middleware2.JWTAuthMiddleware,
	adminAuth middleware2.AdminAuthMiddleware,
	apiKeyAuth middleware2.APIKeyAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	clusterRuntime *service.ClusterRuntime,
	redisClient *redis.Client,
) *gin.Engine {
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(middleware2.Recovery())
	configureClientIPResolution(r, cfg.Server, cfg.Security)

	// Wire up websearch Manager builder so it initializes on startup and rebuilds on config save.
	settingService.SetWebSearchManagerBuilder(context.Background(), func(cfg *service.WebSearchEmulationConfig, proxyURLs map[int64]string) {
		if cfg == nil || !cfg.Enabled || len(cfg.Providers) == 0 {
			service.SetWebSearchManager(nil)
			return
		}
		configs := make([]websearch.ProviderConfig, 0, len(cfg.Providers))
		for _, p := range cfg.Providers {
			if p.APIKey == "" {
				continue
			}
			pc := websearch.ProviderConfig{
				Type:       p.Type,
				APIKey:     p.APIKey,
				QuotaLimit: derefInt64(p.QuotaLimit),
				ExpiresAt:  p.ExpiresAt,
			}
			if p.SubscribedAt != nil {
				pc.SubscribedAt = p.SubscribedAt
			}
			if p.ProxyID != nil {
				pc.ProxyID = *p.ProxyID
				if u, ok := proxyURLs[*p.ProxyID]; ok {
					pc.ProxyURL = u
				} else {
					// Proxy configured but not found — skip this provider to prevent direct connection.
					slog.Warn("websearch: proxy not found for provider, skipping",
						"provider", p.Type, "proxy_id", *p.ProxyID)
					continue
				}
			}
			configs = append(configs, pc)
		}
		service.SetWebSearchManager(websearch.NewManager(configs, redisClient))
	})

	return SetupRouter(r, handlers, jwtAuth, adminAuth, apiKeyAuth, apiKeyService, subscriptionService, opsService, settingService, clusterRuntime, cfg, redisClient)
}

var standardForwardedClientIPHeaders = []string{
	"CF-Connecting-IP",
	"X-Forwarded-For",
	"X-Real-IP",
}

func configureClientIPResolution(r *gin.Engine, serverCfg config.ServerConfig, securityCfg config.SecurityConfig) {
	customHeaders, err := config.NormalizeForwardedClientIPHeaders(securityCfg.ForwardedClientIPHeaders)
	if err != nil {
		// Config.Load 会提前拒绝非法配置；这里仍然关闭自定义头，避免直接构造 Config
		// 的测试或嵌入调用意外扩大信任边界。
		log.Printf("Ignoring invalid security.forwarded_client_ip_headers: %v", err)
		customHeaders = nil
	}
	r.RemoteIPHeaders = mergeForwardedClientIPHeaders(customHeaders, standardForwardedClientIPHeaders)

	if len(serverCfg.TrustedProxies) == 0 {
		if err := r.SetTrustedProxies(nil); err != nil {
			log.Printf("Failed to disable trusted proxies: %v", err)
		}
		if serverCfg.Mode == "release" {
			log.Printf("Warning: server.trusted_proxies is empty in release mode; forwarded client IP headers are disabled")
		}
		return
	}

	if err := r.SetTrustedProxies(serverCfg.TrustedProxies); err != nil {
		log.Printf("Failed to set trusted proxies, disabling forwarded client IP headers: %v", err)
		if disableErr := r.SetTrustedProxies(nil); disableErr != nil {
			log.Printf("Failed to disable trusted proxies after invalid configuration: %v", disableErr)
		}
	}
}

func mergeForwardedClientIPHeaders(groups ...[]string) []string {
	count := 0
	for _, group := range groups {
		count += len(group)
	}
	merged := make([]string, 0, count)
	seen := make(map[string]struct{}, count)
	for _, group := range groups {
		for _, header := range group {
			key := strings.ToLower(header)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, header)
		}
	}
	return merged
}

// ProvideHTTPServer 提供 HTTP 服务器
func ProvideHTTPServer(
	cfg *config.Config,
	router *gin.Engine,
	connectionTracker *service.ClusterConnectionTracker,
) *http.Server {
	httpHandler := http.Handler(router)
	httpHandler = newClusterConnectionTrackingHandler(httpHandler, connectionTracker)
	server := &http.Server{
		Addr:    cfg.Server.Address(),
		Handler: httpHandler,
		// ReadHeaderTimeout: 读取请求头的超时时间，防止慢速请求头攻击
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeout) * time.Second,
		// IdleTimeout: 空闲连接超时时间，释放不活跃的连接资源
		IdleTimeout: time.Duration(cfg.Server.IdleTimeout) * time.Second,
		// 注意：不设置 WriteTimeout，因为流式响应可能持续十几分钟
		// 不设置 ReadTimeout，因为大请求体可能需要较长时间读取
	}

	globalMaxSize := cfg.Server.MaxRequestBodySize
	if globalMaxSize <= 0 {
		globalMaxSize = cfg.Gateway.MaxBodySize
	}
	if globalMaxSize > 0 {
		httpHandler = http.MaxBytesHandler(httpHandler, globalMaxSize)
		log.Printf("Global max request body size: %d bytes (%.2f MB)", globalMaxSize, float64(globalMaxSize)/(1<<20))
	}

	// 根据配置决定是否启用 H2C
	if cfg.Server.H2C.Enabled {
		h2cConfig := cfg.Server.H2C
		if err := http2.ConfigureServer(server, &http2.Server{
			MaxConcurrentStreams:         h2cConfig.MaxConcurrentStreams,
			IdleTimeout:                  time.Duration(h2cConfig.IdleTimeout) * time.Second,
			MaxReadFrameSize:             uint32(h2cConfig.MaxReadFrameSize),
			MaxUploadBufferPerConnection: int32(h2cConfig.MaxUploadBufferPerConnection),
			MaxUploadBufferPerStream:     int32(h2cConfig.MaxUploadBufferPerStream),
		}); err != nil {
			log.Printf("Failed to configure HTTP/2 Cleartext (h2c): %v", err)
		} else {
			protocols := new(http.Protocols)
			protocols.SetHTTP1(true)
			protocols.SetUnencryptedHTTP2(true)
			server.Protocols = protocols
			log.Printf("HTTP/2 Cleartext (h2c) enabled: max_concurrent_streams=%d, idle_timeout=%ds, max_read_frame_size=%d, max_upload_buffer_per_connection=%d, max_upload_buffer_per_stream=%d",
				h2cConfig.MaxConcurrentStreams,
				h2cConfig.IdleTimeout,
				h2cConfig.MaxReadFrameSize,
				h2cConfig.MaxUploadBufferPerConnection,
				h2cConfig.MaxUploadBufferPerStream,
			)
		}
	}

	server.Handler = httpHandler
	return server
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
