package agent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	masterclient "github.com/Wei-Shaw/sub2api/internal/subsite/client"
	"github.com/Wei-Shaw/sub2api/internal/subsite/queue"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSubsiteAgentProxy_AuthorizesForwardsAndQueuesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "subsite-secret"
	var upstreamAuth string
	var upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		upstreamBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"gpt-5.4","usage":{"input_tokens":12,"output_tokens":3,"input_tokens_details":{"cached_tokens":2}}}`))
	}))
	defer upstream.Close()

	var authorizeBody service.AuthorizeSubsiteRequestInput
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSubsiteSignature(t, r, secret)
		switch r.URL.Path {
		case "/api/internal/requests/authorize":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&authorizeBody))
			writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
				RequestID:      "subreq_1",
				ReservationID:  "qres_1",
				SubsiteID:      "site_1",
				LeaseID:        "lease_1",
				AccountID:      100,
				APIKeyID:       200,
				UserID:         300,
				Platform:       service.PlatformOpenAI,
				RequestedModel: "gpt-5.4",
				MappedModel:    "gpt-5.4",
				MaxCost:        1,
				ExpiresAt:      time.Now().Add(time.Minute),
				BillingType:    service.BillingTypeBalance,
				Credential: service.CredentialSnapshot{
					AccountType: service.AccountTypeAPIKey,
					Credentials: map[string]any{
						"api_key":  "sk-upstream",
						"base_url": upstream.URL,
					},
					ExpiresAt: time.Now().Add(time.Minute),
				},
			})
		case "/api/internal/requests/cancel":
			t.Fatalf("reservation should not be canceled on successful proxy")
		default:
			t.Fatalf("unexpected master path: %s", r.URL.Path)
		}
	}))
	defer master.Close()

	usageQueue, err := queue.Open(filepath.Join(t.TempDir(), "usage.db"))
	require.NoError(t, err)
	defer func() { _ = usageQueue.Close() }()

	server := NewServer(&Config{
		ListenAddr: ":0",
		Subsite:    SubsiteConfig{ID: "site_1"},
		Master:     MasterConfig{BaseURL: master.URL, Secret: secret},
		Queue:      QueueConfig{Path: filepath.Join(t.TempDir(), "unused.db")},
	}, masterclient.NewMasterClient(master.URL, "site_1", secret), usageQueue)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"hello"}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("CF-Connecting-IP", "203.0.113.99")
	req.Header.Set("X-Real-IP", "203.0.113.98")
	req.Header.Set("X-Forwarded-For", "203.0.113.97")
	req.Header.Set("X-Sub2API-Estimated-Cost", "0.000001")
	rec := httptest.NewRecorder()

	server.engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "Bearer sk-upstream", upstreamAuth)
	require.JSONEq(t, `{"model":"gpt-5.4","input":"hello"}`, upstreamBody)
	require.Equal(t, "client-key", authorizeBody.APIKey)
	require.Equal(t, "gpt-5.4", authorizeBody.RequestedModel)
	require.Zero(t, authorizeBody.EstimatedCost)
	require.NotEqual(t, "203.0.113.99", authorizeBody.ClientIP)
	require.NotEqual(t, "203.0.113.98", authorizeBody.ClientIP)
	require.NotEqual(t, "203.0.113.97", authorizeBody.ClientIP)
	require.Greater(t, authorizeBody.EstimatedInputTokens, 0)
	require.Equal(t, service.DefaultSubsiteEstimatedUnboundedOutputTokens, authorizeBody.EstimatedOutputTokens)

	items, err := usageQueue.DequeueBatch(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	item := items[0].Payload
	require.Equal(t, "subreq_1", item.RequestID)
	require.Equal(t, int64(200), item.APIKeyID)
	require.Equal(t, 10, item.InputTokens)
	require.Equal(t, 3, item.OutputTokens)
	require.Equal(t, 2, item.CacheReadTokens)
	require.Equal(t, int16(service.RequestTypeSync), item.RequestType)
	require.Equal(t, requestFingerprint(http.MethodPost, "/v1/responses", []byte(`{"model":"gpt-5.4","input":"hello"}`)), item.RequestFingerprint)
	require.NotNil(t, item.DurationMs)
	require.Nil(t, item.FirstTokenMs)
}

func TestSubsiteAgentProxy_WebSocketQueuesEachCompletedTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "subsite-secret"
	var upstreamAuth string
	var upstreamMessages []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth = r.Header.Get("Authorization")
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{InsecureSkipVerify: true})
		require.NoError(t, err)
		defer func() { _ = conn.CloseNow() }()
		for i := 1; i <= 2; i++ {
			readCtx, cancel := context.WithTimeout(r.Context(), time.Second)
			_, payload, err := conn.Read(readCtx)
			cancel()
			require.NoError(t, err)
			upstreamMessages = append(upstreamMessages, string(payload))
			respID := "resp_turn_1"
			inputTokens := 11
			outputTokens := 3
			if i == 2 {
				respID = "resp_turn_2"
				inputTokens = 7
				outputTokens = 2
			}
			writeCtx, writeCancel := context.WithTimeout(r.Context(), time.Second)
			err = conn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"`+respID+`","model":"gpt-5.4-upstream","usage":{"input_tokens":`+strconv.Itoa(inputTokens)+`,"output_tokens":`+strconv.Itoa(outputTokens)+`,"input_tokens_details":{"cached_tokens":1}}}}`))
			writeCancel()
			require.NoError(t, err)
		}
	}))
	defer upstream.Close()

	var authorizeBodies []service.AuthorizeSubsiteRequestInput
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSubsiteSignature(t, r, secret)
		switch r.URL.Path {
		case "/api/internal/requests/authorize":
			var input service.AuthorizeSubsiteRequestInput
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			authorizeBodies = append(authorizeBodies, input)
			idx := len(authorizeBodies)
			writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
				RequestID:      "subreq_ws_" + strconv.Itoa(idx),
				ReservationID:  "qres_ws_" + strconv.Itoa(idx),
				SubsiteID:      "site_1",
				LeaseID:        "lease_1",
				AccountID:      100,
				APIKeyID:       200,
				UserID:         300,
				Platform:       service.PlatformOpenAI,
				RequestedModel: "gpt-5.4",
				MappedModel:    "gpt-5.4-upstream",
				MaxCost:        1,
				ExpiresAt:      time.Now().Add(time.Minute),
				BillingType:    service.BillingTypeBalance,
				Credential: service.CredentialSnapshot{
					AccountType: service.AccountTypeAPIKey,
					Credentials: map[string]any{
						"api_key":  "sk-upstream",
						"base_url": strings.Replace(upstream.URL, "http://", "ws://", 1),
					},
					ExpiresAt: time.Now().Add(time.Minute),
				},
			})
		case "/api/internal/requests/cancel":
			t.Fatalf("reservation should not be canceled on successful websocket turn")
		default:
			t.Fatalf("unexpected master path: %s", r.URL.Path)
		}
	}))
	defer master.Close()

	usageQueue, err := queue.Open(filepath.Join(t.TempDir(), "usage.db"))
	require.NoError(t, err)
	defer func() { _ = usageQueue.Close() }()

	server := NewServer(&Config{
		ListenAddr: ":0",
		Subsite:    SubsiteConfig{ID: "site_1"},
		Master:     MasterConfig{BaseURL: master.URL, Secret: secret},
		Queue:      QueueConfig{Path: filepath.Join(t.TempDir(), "unused.db")},
	}, masterclient.NewMasterClient(master.URL, "site_1", secret), usageQueue)

	subsite := httptest.NewServer(server.engine)
	defer subsite.Close()
	wsURL := strings.Replace(subsite.URL, "http://", "ws://", 1) + "/v1/responses"
	client, _, err := coderws.Dial(context.Background(), wsURL, &coderws.DialOptions{
		HTTPHeader: http.Header{
			"Authorization":          []string{"Bearer client-key"},
			"OpenAI-Conversation-ID": []string{"conv_1"},
			"OpenAI-Session-ID":      []string{"sess_1"},
			"Accept-Language":        []string{"zh-CN"},
		},
	})
	require.NoError(t, err)
	defer func() { _ = client.CloseNow() }()

	writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4","input":"one"}`)))
	cancel()
	readCtx, readCancel := context.WithTimeout(context.Background(), time.Second)
	_, firstResponse, err := client.Read(readCtx)
	readCancel()
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"response.completed","response":{"id":"resp_turn_1","model":"gpt-5.4","usage":{"input_tokens":11,"output_tokens":3,"input_tokens_details":{"cached_tokens":1}}}}`, string(firstResponse))

	writeCtx, cancel = context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4","previous_response_id":"resp_turn_1","input":"two"}`)))
	cancel()
	readCtx, readCancel = context.WithTimeout(context.Background(), time.Second)
	_, secondResponse, err := client.Read(readCtx)
	readCancel()
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"response.completed","response":{"id":"resp_turn_2","model":"gpt-5.4","usage":{"input_tokens":7,"output_tokens":2,"input_tokens_details":{"cached_tokens":1}}}}`, string(secondResponse))

	require.Equal(t, "Bearer sk-upstream", upstreamAuth)
	require.Len(t, upstreamMessages, 2)
	require.JSONEq(t, `{"type":"response.create","model":"gpt-5.4-upstream","input":"one"}`, upstreamMessages[0])
	require.JSONEq(t, `{"type":"response.create","model":"gpt-5.4-upstream","previous_response_id":"resp_turn_1","input":"two"}`, upstreamMessages[1])

	require.Len(t, authorizeBodies, 2)
	require.Equal(t, "client-key", authorizeBodies[0].APIKey)
	require.Zero(t, authorizeBodies[0].EstimatedCost)
	require.Greater(t, authorizeBodies[0].EstimatedInputTokens, 0)
	require.Equal(t, service.DefaultSubsiteEstimatedUnboundedOutputTokens, authorizeBodies[0].EstimatedOutputTokens)
	require.Empty(t, authorizeBodies[0].PreferredLeaseID)
	require.Equal(t, "lease_1", authorizeBodies[1].PreferredLeaseID)
	require.Equal(t, int64(100), authorizeBodies[1].PreferredAccountID)

	var items []queue.UsageQueueItem
	require.Eventually(t, func() bool {
		var dequeueErr error
		items, dequeueErr = usageQueue.DequeueBatch(context.Background(), 10)
		require.NoError(t, dequeueErr)
		return len(items) == 2
	}, 2*time.Second, 20*time.Millisecond)
	require.Len(t, items, 2)
	require.Equal(t, "subreq_ws_1", items[0].Payload.RequestID)
	require.Equal(t, int16(service.RequestTypeWSV2), items[0].Payload.RequestType)
	require.Equal(t, 10, items[0].Payload.InputTokens)
	require.Equal(t, 3, items[0].Payload.OutputTokens)
	require.Equal(t, 1, items[0].Payload.CacheReadTokens)
	require.Equal(t, "subreq_ws_2", items[1].Payload.RequestID)
	require.Equal(t, 6, items[1].Payload.InputTokens)
	require.Equal(t, 2, items[1].Payload.OutputTokens)
	require.Equal(t, 1, items[1].Payload.CacheReadTokens)
	require.Equal(t, requestFingerprint(http.MethodGet, "/v1/responses", []byte(`{"type":"response.create","model":"gpt-5.4","input":"one"}`)), items[0].Payload.RequestFingerprint)
	require.Equal(t, requestFingerprint(http.MethodGet, "/v1/responses", []byte(`{"type":"response.create","model":"gpt-5.4","previous_response_id":"resp_turn_1","input":"two"}`)), items[1].Payload.RequestFingerprint)
}

func TestSubsiteAgentProxy_OpenAIOAuthResponsesUsesCodexUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "subsite-secret"
	groupID := int64(18)
	var upstreamAuth string
	var upstreamHost string
	var upstreamBody map[string]any
	var upstreamOriginator string
	var upstreamBeta string
	var upstreamUserAgent string
	var upstreamAccountID string

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth = r.Header.Get("Authorization")
		upstreamHost = r.Host
		upstreamOriginator = r.Header.Get("originator")
		upstreamBeta = r.Header.Get("OpenAI-Beta")
		upstreamUserAgent = r.Header.Get("User-Agent")
		upstreamAccountID = r.Header.Get("chatgpt-account-id")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&upstreamBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"gpt-5.4","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":12,"output_tokens":3}}`))
	}))
	defer upstream.Close()

	originalTransport := http.DefaultTransport
	http.DefaultTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Test transport connects only to httptest.NewTLSServer.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == "chatgpt.com:443" {
				return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "https://"))
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	defer func() { http.DefaultTransport = originalTransport }()

	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSubsiteSignature(t, r, secret)
		switch r.URL.Path {
		case "/api/internal/requests/authorize":
			writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
				RequestID:      "subreq_oauth_1",
				ReservationID:  "qres_oauth_1",
				SubsiteID:      "site_1",
				LeaseID:        "lease_1",
				AccountID:      100,
				APIKeyID:       200,
				UserID:         300,
				GroupID:        &groupID,
				Platform:       service.PlatformOpenAI,
				RequestedModel: "gpt-5.4",
				MappedModel:    "gpt-5.4",
				MaxCost:        1,
				ExpiresAt:      time.Now().Add(time.Minute),
				BillingType:    service.BillingTypeBalance,
				Credential: service.CredentialSnapshot{
					AccountType:  service.AccountTypeOAuth,
					AccountLevel: service.AccountLevelPlus,
					Credentials: map[string]any{
						"access_token":       "oauth-token",
						"chatgpt_account_id": "acct_123",
					},
					ExpiresAt: time.Now().Add(time.Minute),
				},
			})
		default:
			t.Fatalf("unexpected master path: %s", r.URL.Path)
		}
	}))
	defer master.Close()

	usageQueue, err := queue.Open(filepath.Join(t.TempDir(), "usage.db"))
	require.NoError(t, err)
	defer func() { _ = usageQueue.Close() }()

	server := NewServer(&Config{
		ListenAddr: ":0",
		Subsite:    SubsiteConfig{ID: "site_1"},
		Master:     MasterConfig{BaseURL: master.URL, Secret: secret},
		Queue:      QueueConfig{Path: filepath.Join(t.TempDir(), "unused.db")},
	}, masterclient.NewMasterClient(master.URL, "site_1", secret), usageQueue)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"hello","metadata":{"trace":"1"}}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "Bearer oauth-token", upstreamAuth)
	require.Equal(t, "chatgpt.com", upstreamHost)
	require.Equal(t, "codex_cli_rs", upstreamOriginator)
	require.Equal(t, "responses=experimental", upstreamBeta)
	require.Equal(t, "acct_123", upstreamAccountID)
	require.Equal(t, subsiteCodexCLIUserAgent, upstreamUserAgent)
	require.False(t, gjson.GetBytes([]byte(mustJSON(t, upstreamBody)), "metadata").Exists())
	require.Equal(t, "You are a helpful coding assistant.", gjson.GetBytes([]byte(mustJSON(t, upstreamBody)), "instructions").String())
	require.Equal(t, "message", gjson.GetBytes([]byte(mustJSON(t, upstreamBody)), "input.0.type").String())
	require.Equal(t, "user", gjson.GetBytes([]byte(mustJSON(t, upstreamBody)), "input.0.role").String())
	require.Equal(t, "hello", gjson.GetBytes([]byte(mustJSON(t, upstreamBody)), "input.0.content").String())
}

func TestSubsiteAgentProxy_OpenAIChatCompletionsViaResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "subsite-secret"
	groupID := int64(18)
	var upstreamPath string
	var upstreamBody map[string]any
	var authorizeBody service.AuthorizeSubsiteRequestInput

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&upstreamBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"gpt-5.4","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello back"}]}],"usage":{"input_tokens":8,"output_tokens":5}}`))
	}))
	defer upstream.Close()

	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSubsiteSignature(t, r, secret)
		switch r.URL.Path {
		case "/api/internal/requests/authorize":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&authorizeBody))
			writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
				RequestID:      "subreq_cc_1",
				ReservationID:  "qres_cc_1",
				SubsiteID:      "site_1",
				LeaseID:        "lease_1",
				AccountID:      100,
				APIKeyID:       200,
				UserID:         300,
				GroupID:        &groupID,
				Platform:       service.PlatformOpenAI,
				RequestedModel: "gpt-5.4",
				MappedModel:    "gpt-5.4",
				MaxCost:        1,
				ExpiresAt:      time.Now().Add(time.Minute),
				BillingType:    service.BillingTypeBalance,
				Credential: service.CredentialSnapshot{
					AccountType: service.AccountTypeAPIKey,
					Credentials: map[string]any{
						"api_key":  "sk-upstream",
						"base_url": upstream.URL,
					},
					ExpiresAt: time.Now().Add(time.Minute),
				},
			})
		default:
			t.Fatalf("unexpected master path: %s", r.URL.Path)
		}
	}))
	defer master.Close()

	usageQueue, err := queue.Open(filepath.Join(t.TempDir(), "usage.db"))
	require.NoError(t, err)
	defer func() { _ = usageQueue.Close() }()

	server := NewServer(&Config{
		ListenAddr: ":0",
		Subsite:    SubsiteConfig{ID: "site_1"},
		Master:     MasterConfig{BaseURL: master.URL, Secret: secret},
		Queue:      QueueConfig{Path: filepath.Join(t.TempDir(), "unused.db")},
	}, masterclient.NewMasterClient(master.URL, "site_1", secret), usageQueue)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "/v1/responses", upstreamPath)
	require.Equal(t, "gpt-5.4", gjson.GetBytes([]byte(mustJSON(t, upstreamBody)), "model").String())
	require.True(t, gjson.GetBytes([]byte(mustJSON(t, upstreamBody)), "input").Exists())
	require.Contains(t, rec.Body.String(), `"chat.completion"`)
	require.Contains(t, rec.Body.String(), `"hello back"`)

	items, err := usageQueue.DequeueBatch(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, authorizeBody.RequestFingerprint, items[0].Payload.RequestFingerprint)
	require.Equal(t, requestFingerprint(http.MethodPost, "/v1/chat/completions", []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`)), items[0].Payload.RequestFingerprint)
}

func TestSubsiteAgentProxy_OpenAIChatCompletionsViaResponsesCapturesLatencyAndReasoning(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "subsite-secret"
	groupID := int64(18)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"))
		flusher.Flush()
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.4\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello back\"}]}],\"usage\":{\"input_tokens\":8,\"output_tokens\":5}}}\n\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSubsiteSignature(t, r, secret)
		switch r.URL.Path {
		case "/api/internal/requests/authorize":
			writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
				RequestID:      "subreq_cc_latency_1",
				ReservationID:  "qres_cc_latency_1",
				SubsiteID:      "site_1",
				LeaseID:        "lease_1",
				AccountID:      100,
				APIKeyID:       200,
				UserID:         300,
				GroupID:        &groupID,
				Platform:       service.PlatformOpenAI,
				RequestedModel: "gpt-5.4",
				MappedModel:    "gpt-5.4",
				MaxCost:        1,
				ExpiresAt:      time.Now().Add(time.Minute),
				BillingType:    service.BillingTypeBalance,
				Credential: service.CredentialSnapshot{
					AccountType: service.AccountTypeAPIKey,
					Credentials: map[string]any{
						"api_key":  "sk-upstream",
						"base_url": upstream.URL,
					},
					ExpiresAt: time.Now().Add(time.Minute),
				},
			})
		default:
			t.Fatalf("unexpected master path: %s", r.URL.Path)
		}
	}))
	defer master.Close()

	usageQueue, err := queue.Open(filepath.Join(t.TempDir(), "usage.db"))
	require.NoError(t, err)
	defer func() { _ = usageQueue.Close() }()

	server := NewServer(&Config{
		ListenAddr: ":0",
		Subsite:    SubsiteConfig{ID: "site_1"},
		Master:     MasterConfig{BaseURL: master.URL, Secret: secret},
		Queue:      QueueConfig{Path: filepath.Join(t.TempDir(), "unused.db")},
	}, masterclient.NewMasterClient(master.URL, "site_1", secret), usageQueue)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"chat.completion.chunk"`)

	items, err := usageQueue.DequeueBatch(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	item := items[0].Payload
	require.Equal(t, "high", item.ReasoningEffort)
	require.Equal(t, int16(service.RequestTypeStream), item.RequestType)
	require.NotNil(t, item.DurationMs)
	require.NotNil(t, item.FirstTokenMs)
	require.GreaterOrEqual(t, *item.DurationMs, 20)
	require.GreaterOrEqual(t, *item.FirstTokenMs, 0)
	require.LessOrEqual(t, *item.FirstTokenMs, *item.DurationMs)
}

func TestSubsiteAgentProxy_OpenAIOAuthChatCompletionsUseCodexResponsesUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "subsite-secret"
	groupID := int64(18)
	var upstreamHost string
	var upstreamPath string
	var upstreamOriginator string
	var upstreamBeta string
	var upstreamUserAgent string
	var upstreamSessionID string
	var upstreamConversationID string
	var upstreamBody map[string]any

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHost = r.Host
		upstreamPath = r.URL.Path
		upstreamOriginator = r.Header.Get("originator")
		upstreamBeta = r.Header.Get("OpenAI-Beta")
		upstreamUserAgent = r.Header.Get("User-Agent")
		upstreamSessionID = r.Header.Get("session_id")
		upstreamConversationID = r.Header.Get("conversation_id")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&upstreamBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"gpt-5.4","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":8,"output_tokens":2}}`))
	}))
	defer upstream.Close()

	originalTransport := http.DefaultTransport
	http.DefaultTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Test transport connects only to httptest.NewTLSServer.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == "chatgpt.com:443" {
				return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "https://"))
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	defer func() { http.DefaultTransport = originalTransport }()

	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSubsiteSignature(t, r, secret)
		switch r.URL.Path {
		case "/api/internal/requests/authorize":
			writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
				RequestID:      "subreq_oauth_cc_1",
				ReservationID:  "qres_oauth_cc_1",
				SubsiteID:      "site_1",
				LeaseID:        "lease_1",
				AccountID:      100,
				APIKeyID:       200,
				UserID:         300,
				GroupID:        &groupID,
				Platform:       service.PlatformOpenAI,
				RequestedModel: "gpt-5.4",
				MappedModel:    "gpt-5.4",
				MaxCost:        1,
				ExpiresAt:      time.Now().Add(time.Minute),
				BillingType:    service.BillingTypeBalance,
				Credential: service.CredentialSnapshot{
					AccountType:  service.AccountTypeOAuth,
					AccountLevel: service.AccountLevelPlus,
					Credentials: map[string]any{
						"access_token":       "oauth-token",
						"chatgpt_account_id": "acct_123",
					},
					ExpiresAt: time.Now().Add(time.Minute),
				},
			})
		default:
			t.Fatalf("unexpected master path: %s", r.URL.Path)
		}
	}))
	defer master.Close()

	usageQueue, err := queue.Open(filepath.Join(t.TempDir(), "usage.db"))
	require.NoError(t, err)
	defer func() { _ = usageQueue.Close() }()

	server := NewServer(&Config{
		ListenAddr: ":0",
		Subsite:    SubsiteConfig{ID: "site_1"},
		Master:     MasterConfig{BaseURL: master.URL, Secret: secret},
		Queue:      QueueConfig{Path: filepath.Join(t.TempDir(), "unused.db")},
	}, masterclient.NewMasterClient(master.URL, "site_1", secret), usageQueue)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Session-ID", "sess_123")
	req.Header.Set("OpenAI-Conversation-ID", "conv_456")
	rec := httptest.NewRecorder()

	server.engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "chatgpt.com", upstreamHost)
	require.Equal(t, "/backend-api/codex/responses", upstreamPath)
	require.Equal(t, "responses=experimental", upstreamBeta)
	require.Equal(t, "codex_cli_rs", upstreamOriginator)
	require.Equal(t, subsiteCodexCLIUserAgent, upstreamUserAgent)
	require.NotEmpty(t, upstreamSessionID)
	require.NotEmpty(t, upstreamConversationID)
	require.NotEqual(t, "sess_123", upstreamSessionID)
	require.NotEqual(t, "conv_456", upstreamConversationID)
	require.True(t, gjson.GetBytes([]byte(mustJSON(t, upstreamBody)), "input").Exists())
	require.Contains(t, rec.Body.String(), `"chat.completion"`)
	require.Contains(t, rec.Body.String(), `"ok"`)
}

func TestSubsiteAgentProxy_FailoverOnOpenAIResponsesScopeError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "subsite-secret"
	var authorizeBodies []service.AuthorizeSubsiteRequestInput
	var upstreamAuths []string
	var upstreamPaths []string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuths = append(upstreamAuths, r.Header.Get("Authorization"))
		upstreamPaths = append(upstreamPaths, r.URL.Path)
		if r.Header.Get("Authorization") == "Bearer bad-token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"You have insufficient permissions for this operation. Missing scopes: api.responses.write.","type":"invalid_request_error","code":null}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"gpt-5.4","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":8,"output_tokens":2}}`))
	}))
	defer upstream.Close()

	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSubsiteSignature(t, r, secret)
		switch r.URL.Path {
		case "/api/internal/requests/authorize":
			var input service.AuthorizeSubsiteRequestInput
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			authorizeBodies = append(authorizeBodies, input)
			if len(authorizeBodies) == 1 {
				require.Empty(t, input.ExcludedLeaseIDs)
				require.Empty(t, input.ExcludedAccountIDs)
				writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
					RequestID:      "subreq_1",
					ReservationID:  "qres_1",
					SubsiteID:      "site_1",
					LeaseID:        "lease_bad",
					AccountID:      101,
					APIKeyID:       200,
					UserID:         300,
					Platform:       service.PlatformOpenAI,
					RequestedModel: "gpt-5.4",
					MappedModel:    "gpt-5.4",
					MaxCost:        1,
					ExpiresAt:      time.Now().Add(time.Minute),
					BillingType:    service.BillingTypeBalance,
					Credential: service.CredentialSnapshot{
						AccountType: service.AccountTypeAPIKey,
						Credentials: map[string]any{
							"api_key":  "bad-token",
							"base_url": upstream.URL,
						},
						ExpiresAt: time.Now().Add(time.Minute),
					},
				})
				return
			}
			require.Equal(t, []string{"lease_bad"}, input.ExcludedLeaseIDs)
			require.Equal(t, []int64{101}, input.ExcludedAccountIDs)
			writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
				RequestID:      "subreq_2",
				ReservationID:  "qres_2",
				SubsiteID:      "site_1",
				LeaseID:        "lease_good",
				AccountID:      102,
				APIKeyID:       200,
				UserID:         300,
				Platform:       service.PlatformOpenAI,
				RequestedModel: "gpt-5.4",
				MappedModel:    "gpt-5.4",
				MaxCost:        1,
				ExpiresAt:      time.Now().Add(time.Minute),
				BillingType:    service.BillingTypeBalance,
				Credential: service.CredentialSnapshot{
					AccountType: service.AccountTypeAPIKey,
					Credentials: map[string]any{
						"api_key":  "good-token",
						"base_url": upstream.URL,
					},
					ExpiresAt: time.Now().Add(time.Minute),
				},
			})
		case "/api/internal/requests/cancel":
			writeMasterEnvelope(t, w, gin.H{"status": service.QuotaReservationStatusCanceled})
		default:
			t.Fatalf("unexpected master path: %s", r.URL.Path)
		}
	}))
	defer master.Close()

	usageQueue, err := queue.Open(filepath.Join(t.TempDir(), "usage.db"))
	require.NoError(t, err)
	defer func() { _ = usageQueue.Close() }()

	server := NewServer(&Config{
		ListenAddr: ":0",
		Subsite:    SubsiteConfig{ID: "site_1"},
		Master:     MasterConfig{BaseURL: master.URL, Secret: secret},
		Queue:      QueueConfig{Path: filepath.Join(t.TempDir(), "unused.db")},
	}, masterclient.NewMasterClient(master.URL, "site_1", secret), usageQueue)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, authorizeBodies, 2)
	require.Equal(t, []string{"Bearer bad-token", "Bearer good-token"}, upstreamAuths)
	require.Equal(t, []string{"/v1/responses", "/v1/responses"}, upstreamPaths)
	require.Contains(t, rec.Body.String(), `"ok"`)
	require.Contains(t, rec.Body.String(), `"chat.completion"`)

	items, err := usageQueue.DequeueBatch(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "subreq_2", items[0].Payload.RequestID)
	require.Equal(t, int64(102), items[0].Payload.AccountID)
}

func TestSubsiteAgentProxy_FailoverOnUpstreamServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "subsite-secret"
	var authorizeBodies []service.AuthorizeSubsiteRequestInput
	var upstreamAuths []string
	var upstreamPaths []string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuths = append(upstreamAuths, r.Header.Get("Authorization"))
		upstreamPaths = append(upstreamPaths, r.URL.Path)
		if r.Header.Get("Authorization") == "Bearer bad-token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"temporary upstream outage"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"gpt-5.4","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":8,"output_tokens":2}}`))
	}))
	defer upstream.Close()

	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSubsiteSignature(t, r, secret)
		switch r.URL.Path {
		case "/api/internal/requests/authorize":
			var input service.AuthorizeSubsiteRequestInput
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			authorizeBodies = append(authorizeBodies, input)
			if len(authorizeBodies) == 1 {
				writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
					RequestID:      "subreq_1",
					ReservationID:  "qres_1",
					SubsiteID:      "site_1",
					LeaseID:        "lease_bad",
					AccountID:      101,
					APIKeyID:       200,
					UserID:         300,
					Platform:       service.PlatformOpenAI,
					RequestedModel: "gpt-5.4",
					MappedModel:    "gpt-5.4",
					MaxCost:        1,
					ExpiresAt:      time.Now().Add(time.Minute),
					BillingType:    service.BillingTypeBalance,
					Credential: service.CredentialSnapshot{
						AccountType: service.AccountTypeAPIKey,
						Credentials: map[string]any{"api_key": "bad-token", "base_url": upstream.URL},
						ExpiresAt:   time.Now().Add(time.Minute),
					},
				})
				return
			}
			require.Equal(t, []string{"lease_bad"}, input.ExcludedLeaseIDs)
			require.Equal(t, []int64{101}, input.ExcludedAccountIDs)
			writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
				RequestID:      "subreq_2",
				ReservationID:  "qres_2",
				SubsiteID:      "site_1",
				LeaseID:        "lease_good",
				AccountID:      102,
				APIKeyID:       200,
				UserID:         300,
				Platform:       service.PlatformOpenAI,
				RequestedModel: "gpt-5.4",
				MappedModel:    "gpt-5.4",
				MaxCost:        1,
				ExpiresAt:      time.Now().Add(time.Minute),
				BillingType:    service.BillingTypeBalance,
				Credential: service.CredentialSnapshot{
					AccountType: service.AccountTypeAPIKey,
					Credentials: map[string]any{"api_key": "good-token", "base_url": upstream.URL},
					ExpiresAt:   time.Now().Add(time.Minute),
				},
			})
		case "/api/internal/requests/cancel":
			writeMasterEnvelope(t, w, gin.H{"status": service.QuotaReservationStatusCanceled})
		default:
			t.Fatalf("unexpected master path: %s", r.URL.Path)
		}
	}))
	defer master.Close()

	usageQueue, err := queue.Open(filepath.Join(t.TempDir(), "usage.db"))
	require.NoError(t, err)
	defer func() { _ = usageQueue.Close() }()

	server := NewServer(&Config{
		ListenAddr: ":0",
		Subsite:    SubsiteConfig{ID: "site_1"},
		Master:     MasterConfig{BaseURL: master.URL, Secret: secret},
		Queue:      QueueConfig{Path: filepath.Join(t.TempDir(), "unused.db")},
	}, masterclient.NewMasterClient(master.URL, "site_1", secret), usageQueue)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, authorizeBodies, 2)
	require.Equal(t, []string{"Bearer bad-token", "Bearer good-token"}, upstreamAuths)
	require.Equal(t, []string{"/v1/responses", "/v1/responses"}, upstreamPaths)
	require.Contains(t, rec.Body.String(), `"ok"`)
	require.Contains(t, rec.Body.String(), `"chat.completion"`)
}

func TestSubsiteAgentProxy_ReturnsRetryAuthorizeError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "subsite-secret"
	var authorizeBodies []service.AuthorizeSubsiteRequestInput

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"You have insufficient permissions for this operation. Missing scopes: api.responses.write.","type":"invalid_request_error","code":null}}`))
	}))
	defer upstream.Close()

	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSubsiteSignature(t, r, secret)
		switch r.URL.Path {
		case "/api/internal/requests/authorize":
			var input service.AuthorizeSubsiteRequestInput
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			authorizeBodies = append(authorizeBodies, input)
			if len(authorizeBodies) == 1 {
				writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
					RequestID:      "subreq_1",
					ReservationID:  "qres_1",
					SubsiteID:      "site_1",
					LeaseID:        "lease_bad",
					AccountID:      101,
					APIKeyID:       200,
					UserID:         300,
					Platform:       service.PlatformOpenAI,
					RequestedModel: "gpt-5.4",
					MappedModel:    "gpt-5.4",
					MaxCost:        1,
					ExpiresAt:      time.Now().Add(time.Minute),
					BillingType:    service.BillingTypeBalance,
					Credential: service.CredentialSnapshot{
						AccountType: service.AccountTypeAPIKey,
						Credentials: map[string]any{"api_key": "bad-token", "base_url": upstream.URL},
						ExpiresAt:   time.Now().Add(time.Minute),
					},
				})
				return
			}
			http.Error(w, "master overloaded", http.StatusServiceUnavailable)
		case "/api/internal/requests/cancel":
			writeMasterEnvelope(t, w, gin.H{"status": service.QuotaReservationStatusCanceled})
		default:
			t.Fatalf("unexpected master path: %s", r.URL.Path)
		}
	}))
	defer master.Close()

	usageQueue, err := queue.Open(filepath.Join(t.TempDir(), "usage.db"))
	require.NoError(t, err)
	defer func() { _ = usageQueue.Close() }()

	server := NewServer(&Config{
		ListenAddr: ":0",
		Subsite:    SubsiteConfig{ID: "site_1"},
		Master:     MasterConfig{BaseURL: master.URL, Secret: secret},
		Queue:      QueueConfig{Path: filepath.Join(t.TempDir(), "unused.db")},
	}, masterclient.NewMasterClient(master.URL, "site_1", secret), usageQueue)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Len(t, authorizeBodies, 2)
	require.Contains(t, rec.Body.String(), `master returned 503`)
	require.NotContains(t, rec.Body.String(), `api.responses.write`)
}

func TestSubsiteAgentProxy_WebSocketFailoverOnInitialDialError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "subsite-secret"
	var authorizeBodies []service.AuthorizeSubsiteRequestInput
	var upstreamMessages []string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{InsecureSkipVerify: true})
		require.NoError(t, err)
		defer func() { _ = conn.CloseNow() }()
		readCtx, cancel := context.WithTimeout(r.Context(), time.Second)
		_, payload, err := conn.Read(readCtx)
		cancel()
		require.NoError(t, err)
		upstreamMessages = append(upstreamMessages, string(payload))
		writeCtx, writeCancel := context.WithTimeout(r.Context(), time.Second)
		err = conn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_turn_1","model":"gpt-5.4-upstream","usage":{"input_tokens":11,"output_tokens":3,"input_tokens_details":{"cached_tokens":1}}}}`))
		writeCancel()
		require.NoError(t, err)
	}))
	defer upstream.Close()

	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSubsiteSignature(t, r, secret)
		switch r.URL.Path {
		case "/api/internal/requests/authorize":
			var input service.AuthorizeSubsiteRequestInput
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			authorizeBodies = append(authorizeBodies, input)
			if len(authorizeBodies) == 1 {
				writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
					RequestID:      "subreq_ws_1",
					ReservationID:  "qres_ws_1",
					SubsiteID:      "site_1",
					LeaseID:        "lease_bad",
					AccountID:      100,
					APIKeyID:       200,
					UserID:         300,
					Platform:       service.PlatformOpenAI,
					RequestedModel: "gpt-5.4",
					MappedModel:    "gpt-5.4-upstream",
					MaxCost:        1,
					ExpiresAt:      time.Now().Add(time.Minute),
					BillingType:    service.BillingTypeBalance,
					Credential: service.CredentialSnapshot{
						AccountType: service.AccountTypeAPIKey,
						Credentials: map[string]any{"api_key": "bad-token", "base_url": "ws://127.0.0.1:1"},
						ExpiresAt:   time.Now().Add(time.Minute),
					},
				})
				return
			}
			require.Equal(t, []string{"lease_bad"}, input.ExcludedLeaseIDs)
			require.Equal(t, []int64{100}, input.ExcludedAccountIDs)
			writeMasterEnvelope(t, w, service.AuthorizeSubsiteResponse{
				RequestID:      "subreq_ws_2",
				ReservationID:  "qres_ws_2",
				SubsiteID:      "site_1",
				LeaseID:        "lease_good",
				AccountID:      101,
				APIKeyID:       200,
				UserID:         300,
				Platform:       service.PlatformOpenAI,
				RequestedModel: "gpt-5.4",
				MappedModel:    "gpt-5.4-upstream",
				MaxCost:        1,
				ExpiresAt:      time.Now().Add(time.Minute),
				BillingType:    service.BillingTypeBalance,
				Credential: service.CredentialSnapshot{
					AccountType: service.AccountTypeAPIKey,
					Credentials: map[string]any{
						"api_key":  "good-token",
						"base_url": strings.Replace(upstream.URL, "http://", "ws://", 1),
					},
					ExpiresAt: time.Now().Add(time.Minute),
				},
			})
		case "/api/internal/requests/cancel":
			writeMasterEnvelope(t, w, gin.H{"status": service.QuotaReservationStatusCanceled})
		default:
			t.Fatalf("unexpected master path: %s", r.URL.Path)
		}
	}))
	defer master.Close()

	usageQueue, err := queue.Open(filepath.Join(t.TempDir(), "usage.db"))
	require.NoError(t, err)
	defer func() { _ = usageQueue.Close() }()

	server := NewServer(&Config{
		ListenAddr: ":0",
		Subsite:    SubsiteConfig{ID: "site_1"},
		Master:     MasterConfig{BaseURL: master.URL, Secret: secret},
		Queue:      QueueConfig{Path: filepath.Join(t.TempDir(), "unused.db")},
	}, masterclient.NewMasterClient(master.URL, "site_1", secret), usageQueue)

	subsite := httptest.NewServer(server.engine)
	defer subsite.Close()
	wsURL := strings.Replace(subsite.URL, "http://", "ws://", 1) + "/v1/responses"
	client, _, err := coderws.Dial(context.Background(), wsURL, &coderws.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer client-key"},
		},
	})
	require.NoError(t, err)
	defer func() { _ = client.CloseNow() }()

	writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4","input":"one"}`)))
	cancel()
	readCtx, readCancel := context.WithTimeout(context.Background(), time.Second)
	_, firstResponse, err := client.Read(readCtx)
	readCancel()
	require.NoError(t, err)

	require.Len(t, authorizeBodies, 2)
	require.Len(t, upstreamMessages, 1)
	require.JSONEq(t, `{"type":"response.completed","response":{"id":"resp_turn_1","model":"gpt-5.4","usage":{"input_tokens":11,"output_tokens":3,"input_tokens_details":{"cached_tokens":1}}}}`, string(firstResponse))

	var items []queue.UsageQueueItem
	require.Eventually(t, func() bool {
		var dequeueErr error
		items, dequeueErr = usageQueue.DequeueBatch(context.Background(), 10)
		require.NoError(t, dequeueErr)
		return len(items) == 1
	}, 2*time.Second, 20*time.Millisecond)
	require.Len(t, items, 1)
	require.Equal(t, "subreq_ws_2", items[0].Payload.RequestID)
	require.Equal(t, int64(101), items[0].Payload.AccountID)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

func requireSubsiteSignature(t *testing.T, r *http.Request, secret string) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	sum := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(sum[:])
	require.Equal(t, bodyHash, r.Header.Get(service.SubsiteAuthHeaderBodySHA))
	expected := service.SignSubsiteRequest(
		secret,
		r.Method,
		r.URL.Path,
		r.Header.Get(service.SubsiteAuthHeaderTimestamp),
		r.Header.Get(service.SubsiteAuthHeaderNonce),
		bodyHash,
	)
	require.Equal(t, expected, r.Header.Get(service.SubsiteAuthHeaderSignature))
}

func writeMasterEnvelope(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
		"code": 0,
		"data": data,
	}))
}

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}
