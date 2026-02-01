//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSoraDirectClient_DoRequestSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{BaseURL: server.URL},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)

	body, _, err := client.doRequest(context.Background(), &Account{ID: 1}, http.MethodGet, server.URL, http.Header{}, nil, false)
	require.NoError(t, err)
	require.Contains(t, string(body), "ok")
}

func TestSoraDirectClient_BuildBaseHeaders(t *testing.T) {
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				Headers: map[string]string{
					"X-Test":                "yes",
					"Authorization":         "should-ignore",
					"openai-sentinel-token": "skip",
				},
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)

	headers := client.buildBaseHeaders("token-123", "UA")
	require.Equal(t, "Bearer token-123", headers.Get("Authorization"))
	require.Equal(t, "UA", headers.Get("User-Agent"))
	require.Equal(t, "yes", headers.Get("X-Test"))
	require.Empty(t, headers.Get("openai-sentinel-token"))
}

func TestSoraDirectClient_GetImageTaskFallbackLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		switch limit {
		case "1":
			_, _ = w.Write([]byte(`{"task_responses":[]}`))
		case "2":
			_, _ = w.Write([]byte(`{"task_responses":[{"id":"task-1","status":"completed","progress_pct":1,"generations":[{"url":"https://example.com/a.png"}]}]}`))
		default:
			_, _ = w.Write([]byte(`{"task_responses":[]}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL:            server.URL,
				RecentTaskLimit:    1,
				RecentTaskLimitMax: 2,
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)
	account := &Account{Credentials: map[string]any{"access_token": "token"}}

	status, err := client.GetImageTask(context.Background(), account, "task-1")
	require.NoError(t, err)
	require.Equal(t, "completed", status.Status)
	require.Equal(t, []string{"https://example.com/a.png"}, status.URLs)
}
