//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type stubAllower struct {
	calls   int
	allowed bool
	err     error
	lastKey string
}

func (s *stubAllower) Allow(_ context.Context, key string, _ int, _ time.Duration) (middleware.AllowResult, error) {
	s.calls++
	s.lastKey = key
	if s.err != nil {
		return middleware.AllowResult{}, s.err
	}
	return middleware.AllowResult{Allowed: s.allowed, RetryAfter: time.Minute}, nil
}

func TestIsPubliclyRoutableClientIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"1.1.1.1", true},
		{"159.195.12.14", true},
		{"2001:4860:4860::8888", true},
		{"127.0.0.1", false}, // 回环：反代内部地址，按它计数会合并所有用户
		{"::1", false},       // IPv6 回环
		{"10.0.0.1", false},  // RFC1918
		{"192.168.1.5", false},
		{"172.16.0.9", false},
		{"169.254.1.1", false}, // 链路本地
		{"0.0.0.0", false},     // 未指定
		{"", false},
		{"not-an-ip", false},
	}
	for _, tt := range tests {
		if got := isPubliclyRoutableClientIP(tt.ip); got != tt.want {
			t.Fatalf("isPubliclyRoutableClientIP(%q) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestAbortPanelRateLimitedSetsRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage", nil)

	abortPanelRateLimited(c, 1500*time.Millisecond)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	// 1.5s 必须向上取整为 2，否则客户端会在窗口结束前就重试
	if got := w.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want %q", got, "2")
	}
	if !c.IsAborted() {
		t.Fatal("expected context to be aborted")
	}
}

// 限流器未装配（Redis 未启用等）时必须直接放行，绝不能把面板打挂。
func TestPanelRateLimiterNilDependenciesPassThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, p := range map[string]*PanelRateLimiter{
		"nil receiver":   nil,
		"empty":          {},
		"no setting svc": {limiter: &stubAllower{allowed: true}},
	} {
		for handlerName, h := range map[string]gin.HandlerFunc{
			"global": p.Global(),
			"heavy":  p.Heavy(),
			"public": p.PublicIP(),
		} {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage", nil)
			h(c)
			if c.IsAborted() {
				t.Fatalf("%s/%s: request must pass through when limiter is not wired", name, handlerName)
			}
		}
	}
	_ = service.PanelRateLimitSettings{}
}
