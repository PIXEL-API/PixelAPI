package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/gin-gonic/gin"
)

func runServerTimingRequest(
	t *testing.T,
	enabled bool,
	path string,
	adminMarker string,
	userMarker string,
	role string,
	handler gin.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(ServerTiming(enabled))
	engine.Any("/*path", func(c *gin.Context) {
		if role != "" {
			c.Set(string(ContextKeyUserRole), role)
		}
		handler(c)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if adminMarker != "" {
		request.Header.Set(servertiming.AdminUIHeader, adminMarker)
	}
	if userMarker != "" {
		request.Header.Set(servertiming.UserUIHeader, userMarker)
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestServerTimingScopesRoleGateAndPublicExclusions(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		path        string
		adminMarker string
		userMarker  string
		role        string
		wantHeader  bool
		wantActive  bool
	}{
		{name: "disabled", path: "/api/v1/admin/users", role: "admin"},
		{name: "admin route", enabled: true, path: "/api/v1/admin/users", role: "admin", wantHeader: true, wantActive: true},
		{name: "admin marker on shared user route", enabled: true, path: "/api/v1/groups/available", adminMarker: "1", role: "admin", wantHeader: true, wantActive: true},
		{name: "authenticated user route", enabled: true, path: "/api/v1/accounts", role: "user", wantHeader: true, wantActive: true},
		{name: "user marker on allowlist", enabled: true, path: "/api/v1/keys", userMarker: "1", role: "user", wantHeader: true, wantActive: true},
		{name: "user cannot use admin marker to expand scope", enabled: true, path: "/api/v1/settings/public", adminMarker: "1", role: "user", wantActive: true},
		{name: "user marker cannot expand scope", enabled: true, path: "/api/v1/settings/public", userMarker: "1", role: "user"},
		{name: "unauthenticated user path", enabled: true, path: "/api/v1/keys", wantActive: true},
		{name: "public settings excluded", enabled: true, path: "/api/v1/settings/public"},
		{name: "login excluded", enabled: true, path: "/api/v1/auth/login", userMarker: "1"},
		{name: "public usage excluded", enabled: true, path: "/api/v1/public/usage/today", userMarker: "1"},
		{name: "public shop excluded", enabled: true, path: "/api/v1/shop/products", userMarker: "1"},
		{name: "authenticated shop order", enabled: true, path: "/api/v1/shop/orders/2", role: "user", wantHeader: true, wantActive: true},
		{name: "payment user route", enabled: true, path: "/api/v1/payment/orders/my", role: "user", wantHeader: true, wantActive: true},
		{name: "payment public excluded", enabled: true, path: "/api/v1/payment/public/orders/verify", userMarker: "1", role: "user"},
		{name: "payment webhook excluded", enabled: true, path: "/api/v1/payment/webhook/stripe", userMarker: "1", role: "admin"},
		{name: "admin boundary", enabled: true, path: "/api/v1/administrator", role: "admin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active := false
			recorder := runServerTimingRequest(t, tt.enabled, tt.path, tt.adminMarker, tt.userMarker, tt.role, func(c *gin.Context) {
				active = servertiming.Active(c.Request.Context())
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})
			header := recorder.Header().Get(servertiming.HeaderName)
			if active != tt.wantActive {
				t.Fatalf("collector active = %v, want %v", active, tt.wantActive)
			}
			if (header != "") != tt.wantHeader {
				t.Fatalf("Server-Timing header = %q, want present %v", header, tt.wantHeader)
			}
			if header != "" && (!strings.Contains(header, "total;dur=") || !strings.Contains(header, `cache;desc="bypass"`)) {
				t.Fatalf("incomplete timing header: %q", header)
			}
		})
	}
}

func TestIsUserTimingPathMatchesLocalAuthenticatedRoutes(t *testing.T) {
	tests := map[string]bool{
		"/api/v1/auth/me":                       true,
		"/api/v1/auth/login":                    false,
		"/api/v1/oidc/authorize/complete":       true,
		"/api/v1/oidc/token":                    false,
		"/api/v1/user/profile":                  true,
		"/api/v1/keys/12":                       true,
		"/api/v1/accounts/12/usage":             true,
		"/api/v1/account-oauth/openai/auth-url": true,
		"/api/v1/account-share/listings":        true,
		"/api/v1/groups/available":              true,
		"/api/v1/groups":                        false,
		"/api/v1/channels/available":            true,
		"/api/v1/channels":                      false,
		"/api/v1/usage/dashboard/stats":         true,
		"/api/v1/public/usage/today":            false,
		"/api/v1/announcements":                 true,
		"/api/v1/conversations/unread-count":    true,
		"/api/v1/redeem/history":                true,
		"/api/v1/subscriptions/active":          true,
		"/api/v1/channel-monitors/1/status":     true,
		"/api/v1/activities/winners":            true,
		"/api/v1/shop/draw-progress":            true,
		"/api/v1/shop/orders/1":                 true,
		"/api/v1/shop/products":                 false,
		"/api/v1/payment/config":                true,
		"/api/v1/payment/public/orders/verify":  false,
		"/api/v1/payment/webhook/easypay":       false,
		"/api/v1/settings/public":               false,
		"/api/v1/admin/users":                   false,
		"/api/v1evil/keys":                      false,
		"/v1/keys":                              false,
	}
	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			if got := isUserTimingPath(path); got != want {
				t.Fatalf("isUserTimingPath(%q) = %v, want %v", path, got, want)
			}
		})
	}
}

func TestServerTimingFinalizesBeforeEarlyCommitAndFlush(t *testing.T) {
	for _, mode := range []string{"write-header-now", "flush", "read-from", "status-only"} {
		t.Run(mode, func(t *testing.T) {
			recorder := runServerTimingRequest(t, true, "/api/v1/admin/export", "", "", "admin", func(c *gin.Context) {
				switch mode {
				case "write-header-now":
					c.Status(http.StatusAccepted)
					c.Writer.WriteHeaderNow()
				case "flush":
					c.Writer.Flush()
				case "read-from":
					if _, err := io.Copy(c.Writer, strings.NewReader("body")); err != nil {
						t.Fatal(err)
					}
				default:
					c.Status(http.StatusNotModified)
				}
			})
			if got := recorder.Header().Get(servertiming.HeaderName); got == "" {
				t.Fatal("timing header missing before response commit")
			}
		})
	}
}

func TestServerTimingResponseWriterUnwrapAndCacheStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	baseWriter := c.Writer
	writer := &serverTimingResponseWriter{ResponseWriter: baseWriter}
	if got := writer.Unwrap(); got != baseWriter {
		t.Fatalf("Unwrap() = %T, want original Gin writer", got)
	}

	recorder = runServerTimingRequest(t, true, "/api/v1/admin/dashboard", "", "", "admin", func(c *gin.Context) {
		c.Header(snapshotCacheHeader, "HIT")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	if got := recorder.Header().Get(servertiming.HeaderName); !strings.Contains(got, `cache;desc="hit"`) {
		t.Fatalf("cache status missing from %q", got)
	}
}

func TestServerTimingResponseHeaderForWebSocketRoleGate(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/ops/ws/qps", nil)
	c.Request = c.Request.WithContext(servertiming.WithCollector(c.Request.Context(), servertiming.New(time.Now())))
	c.Set(string(ContextKeyUserRole), "admin")
	if header := ServerTimingResponseHeader(c); header.Get(servertiming.HeaderName) == "" {
		t.Fatal("admin WebSocket response header missing")
	}
	c.Set(string(ContextKeyUserRole), "user")
	if header := ServerTimingResponseHeader(c); header != nil {
		t.Fatalf("non-admin WebSocket received timing header: %#v", header)
	}
}
