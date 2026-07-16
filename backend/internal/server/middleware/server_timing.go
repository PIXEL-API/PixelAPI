package middleware

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/gin-gonic/gin"
)

const (
	snapshotCacheHeader = "X-Snapshot-Cache"
	usageCacheHeader    = "X-Usage-Stats-Cache"
)

type serverTimingResponseWriter struct {
	gin.ResponseWriter
	context *gin.Context
	once    sync.Once
}

var (
	_ gin.ResponseWriter = (*serverTimingResponseWriter)(nil)
	_ http.Hijacker      = (*serverTimingResponseWriter)(nil)
	_ http.Flusher       = (*serverTimingResponseWriter)(nil)
	_ http.CloseNotifier = (*serverTimingResponseWriter)(nil) //nolint:staticcheck // Gin compatibility contract.
	_ io.ReaderFrom      = (*serverTimingResponseWriter)(nil)
)

func (w *serverTimingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// ServerTiming collects timing only for explicitly scoped Admin/User Web requests.
func ServerTiming(enabled bool) gin.HandlerFunc {
	if !enabled {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		if c.Request == nil || !shouldCollectServerTiming(c) {
			c.Next()
			return
		}

		collector := servertiming.New(time.Now())
		c.Request = c.Request.WithContext(servertiming.WithCollector(c.Request.Context(), collector))
		writer := &serverTimingResponseWriter{ResponseWriter: c.Writer, context: c}
		c.Writer = writer
		c.Next()
		writer.finalize()
	}
}

func (w *serverTimingResponseWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *serverTimingResponseWriter) WriteHeaderNow() {
	w.finalize()
	w.ResponseWriter.WriteHeaderNow()
}

func (w *serverTimingResponseWriter) Write(data []byte) (int, error) {
	w.finalize()
	return w.ResponseWriter.Write(data)
}

func (w *serverTimingResponseWriter) WriteString(data string) (int, error) {
	w.finalize()
	return w.ResponseWriter.WriteString(data)
}

func (w *serverTimingResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	w.finalize()
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(reader)
	}
	// Hide this wrapper's ReaderFrom method to prevent io.Copy recursion.
	return io.Copy(struct{ io.Writer }{Writer: w.ResponseWriter}, reader)
}

func (w *serverTimingResponseWriter) Flush() {
	w.finalize()
	w.ResponseWriter.Flush()
}

func (w *serverTimingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.finalize()
	return w.ResponseWriter.Hijack()
}

func (w *serverTimingResponseWriter) Pusher() http.Pusher {
	return w.ResponseWriter.Pusher()
}

func (w *serverTimingResponseWriter) finalize() {
	if w == nil || w.ResponseWriter == nil {
		return
	}
	w.once.Do(func() {
		if value := ServerTimingHeaderValue(w.context); value != "" {
			w.ResponseWriter.Header().Set(servertiming.HeaderName, value)
		}
	})
}

// ServerTimingHeaderValue emits metrics only after a trusted auth middleware
// has populated the role. Marker headers are collection signals, not authorization.
func ServerTimingHeaderValue(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	role, ok := GetUserRoleFromContext(c)
	if !ok || role == "" {
		return ""
	}
	if role != "admin" && !isUserTimingPath(c.Request.URL.Path) {
		return ""
	}
	return servertiming.HeaderValue(c.Request.Context(), time.Now(), responseCacheStatus(c.Writer.Header()))
}

// ServerTimingResponseHeader returns the extra response header for a WebSocket 101 handshake.
func ServerTimingResponseHeader(c *gin.Context) http.Header {
	value := ServerTimingHeaderValue(c)
	if value == "" {
		return nil
	}
	return http.Header{servertiming.HeaderName: []string{value}}
}

func shouldCollectServerTiming(c *gin.Context) bool {
	return isAdminUIRequest(c) || isUserUIRequest(c)
}

func isAdminUIRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	if strings.TrimSpace(c.GetHeader(servertiming.AdminUIHeader)) == "1" {
		return true
	}
	path := strings.TrimSpace(c.Request.URL.Path)
	return path == "/api/v1/admin" || strings.HasPrefix(path, "/api/v1/admin/")
}

func isUserUIRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	// The marker documents frontend intent but cannot expand the backend
	// allowlist; an attacker-controlled header must not enable collection on
	// public or unrelated endpoints.
	return isUserTimingPath(c.Request.URL.Path)
}

// isUserTimingPath is the explicit authenticated browser API allowlist.
// Public settings, auth entry points, shops, payment recovery and webhooks are excluded.
func isUserTimingPath(path string) bool {
	path = strings.TrimSpace(path)
	const prefix = "/api/v1"
	if path == "" || !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" {
		return false
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}

	switch {
	case rest == "/auth/me",
		rest == "/auth/revoke-all-sessions",
		rest == "/auth/oauth/bind-token",
		rest == "/oidc/authorize/complete":
		return true
	case rest == "/user", strings.HasPrefix(rest, "/user/"):
		return true
	case rest == "/keys", strings.HasPrefix(rest, "/keys/"):
		return true
	case rest == "/accounts", strings.HasPrefix(rest, "/accounts/"):
		return true
	case rest == "/account-oauth", strings.HasPrefix(rest, "/account-oauth/"):
		return true
	case rest == "/account-share", strings.HasPrefix(rest, "/account-share/"):
		return true
	case rest == "/groups/available", rest == "/groups/rates":
		return true
	case rest == "/channels/available":
		return true
	case rest == "/usage", strings.HasPrefix(rest, "/usage/"):
		return true
	case rest == "/announcements", strings.HasPrefix(rest, "/announcements/"):
		return true
	case rest == "/conversations", strings.HasPrefix(rest, "/conversations/"):
		return true
	case rest == "/redeem", strings.HasPrefix(rest, "/redeem/"):
		return true
	case rest == "/subscriptions", strings.HasPrefix(rest, "/subscriptions/"):
		return true
	case rest == "/channel-monitors", strings.HasPrefix(rest, "/channel-monitors/"):
		return true
	case rest == "/activities", strings.HasPrefix(rest, "/activities/"):
		return true
	case rest == "/shop/draw-progress", rest == "/shop/orders", strings.HasPrefix(rest, "/shop/orders/"):
		return true
	case strings.HasPrefix(rest, "/payment/"):
		return rest != "/payment/public" && !strings.HasPrefix(rest, "/payment/public/") &&
			rest != "/payment/webhook" && !strings.HasPrefix(rest, "/payment/webhook/")
	default:
		return false
	}
}

func responseCacheStatus(header http.Header) string {
	for _, name := range []string{snapshotCacheHeader, usageCacheHeader} {
		switch strings.ToLower(strings.TrimSpace(header.Get(name))) {
		case "hit":
			return "hit"
		case "miss":
			return "miss"
		case "bypass":
			return "bypass"
		}
	}
	return "bypass"
}
