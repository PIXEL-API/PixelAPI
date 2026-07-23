//go:build unit

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConfigureClientIPResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		serverConfig   config.ServerConfig
		securityConfig config.SecurityConfig
		remoteAddr     string
		headers        map[string]string
		want           string
	}{
		{
			name:         "untrusted peer cannot spoof standard or custom headers",
			remoteAddr:   "9.9.9.9:12345",
			serverConfig: config.ServerConfig{},
			securityConfig: config.SecurityConfig{
				ForwardedClientIPHeaders: []string{"True-Client-IP"},
			},
			headers: map[string]string{
				"True-Client-IP":  "1.1.1.1",
				"X-Forwarded-For": "2.2.2.2",
				"X-Real-IP":       "3.3.3.3",
			},
			want: "9.9.9.9",
		},
		{
			name:       "trusted proxy accepts explicit custom header",
			remoteAddr: "10.0.0.5:12345",
			serverConfig: config.ServerConfig{
				TrustedProxies: []string{"10.0.0.0/8"},
			},
			securityConfig: config.SecurityConfig{
				ForwardedClientIPHeaders: []string{"True-Client-IP"},
			},
			headers: map[string]string{
				"True-Client-IP":  "1.1.1.1",
				"X-Forwarded-For": "2.2.2.2",
			},
			want: "1.1.1.1",
		},
		{
			name:       "trusted multi-hop xff skips trusted proxy hops",
			remoteAddr: "10.0.0.5:12345",
			serverConfig: config.ServerConfig{
				TrustedProxies: []string{"10.0.0.0/8"},
			},
			headers: map[string]string{
				"X-Forwarded-For": "198.51.100.10, 10.0.0.6",
			},
			want: "198.51.100.10",
		},
		{
			name:       "invalid trusted proxy configuration fails closed",
			remoteAddr: "9.9.9.9:12345",
			serverConfig: config.ServerConfig{
				TrustedProxies: []string{"not-a-cidr"},
			},
			headers: map[string]string{
				"X-Forwarded-For": "1.1.1.1",
			},
			want: "9.9.9.9",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			configureClientIPResolution(router, test.serverConfig, test.securityConfig)
			router.GET("/client-ip", func(c *gin.Context) {
				c.String(http.StatusOK, ip.GetSecurityClientIP(c))
			})

			request := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
			request.RemoteAddr = test.remoteAddr
			for name, value := range test.headers {
				request.Header.Set(name, value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, test.want, response.Body.String())
		})
	}
}
