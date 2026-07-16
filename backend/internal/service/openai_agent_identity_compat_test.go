package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type agentIdentityCompatOutcome struct {
	response *http.Response
	err      error
}

type agentIdentityCompatUpstream struct {
	mu       sync.Mutex
	outcomes []agentIdentityCompatOutcome
	requests []*http.Request
}

func (u *agentIdentityCompatUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.requests = append(u.requests, cloneAgentIdentityCompatRequest(req))
	if len(u.outcomes) == 0 {
		return nil, errors.New("no Agent Identity compatibility response configured")
	}
	outcome := u.outcomes[0]
	u.outcomes = u.outcomes[1:]
	return outcome.response, outcome.err
}

func (u *agentIdentityCompatUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *agentIdentityCompatUpstream) requestSnapshot() []*http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]*http.Request, len(u.requests))
	copy(out, u.requests)
	return out
}

func cloneAgentIdentityCompatRequest(req *http.Request) *http.Request {
	if req == nil {
		return nil
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	return clone
}

type agentIdentityCompatRepo struct {
	AccountRepository
	mu               sync.Mutex
	account          *Account
	credentialWrites int
	setErrorCalls    int
}

func (r *agentIdentityCompatRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	return cloneAgentIdentityAccountForTest(r.account), nil
}

func (r *agentIdentityCompatRepo) UpdateCredentials(_ context.Context, id int64, credentials map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account == nil || r.account.ID != id {
		return ErrAccountNotFound
	}
	r.account.Credentials = cloneCredentials(credentials)
	r.credentialWrites++
	return nil
}

func (r *agentIdentityCompatRepo) UpdateExtra(_ context.Context, _ int64, _ map[string]any) error {
	return nil
}

func (r *agentIdentityCompatRepo) SetError(_ context.Context, _ int64, _ string) error {
	r.mu.Lock()
	r.setErrorCalls++
	r.mu.Unlock()
	return nil
}

func (r *agentIdentityCompatRepo) credentialWriteCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.credentialWrites
}

type agentIdentityCompatInvalidator struct {
	accountIDs []int64
	mu         sync.Mutex
}

func (r *agentIdentityCompatInvalidator) InvalidateAgentIdentityWSConnections(accountID int64) {
	r.mu.Lock()
	r.accountIDs = append(r.accountIDs, accountID)
	r.mu.Unlock()
}

func (r *agentIdentityCompatInvalidator) snapshot() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int64, len(r.accountIDs))
	copy(out, r.accountIDs)
	return out
}

func newAgentIdentityCompatResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newAgentIdentityCompatGinContext(body []byte) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", codexCLIUserAgent)
	c.Request.Header.Set("originator", "codex_cli_rs")
	return c
}

func installAgentIdentityCompatRegistrationServer(t *testing.T, taskID string) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"task_id":`+quoteAgentIdentityCompatString(taskID)+`}`)
	}))
	previous := openAIAgentIdentityAuthAPIBaseURL
	openAIAgentIdentityAuthAPIBaseURL = server.URL
	t.Cleanup(func() {
		openAIAgentIdentityAuthAPIBaseURL = previous
		server.Close()
	})
	return &calls
}

func quoteAgentIdentityCompatString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func decodeAgentIdentityCompatAssertion(t *testing.T, authorization string) (runtimeID, taskID string) {
	t.Helper()
	const prefix = "AgentAssertion "
	require.True(t, strings.HasPrefix(authorization, prefix), "authorization must use AgentAssertion")
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(authorization, prefix))
	require.NoError(t, err)
	var envelope struct {
		RuntimeID string `json:"agent_runtime_id"`
		TaskID    string `json:"task_id"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope))
	return envelope.RuntimeID, envelope.TaskID
}

func TestOpenAIAgentIdentityHTTPInvalidTaskRecoversExactlyOnce(t *testing.T) {
	const (
		runtimeID = "runtime-http-secret"
		oldTaskID = "task-http-old"
		newTaskID = "task-http-new"
	)
	registrationCalls := installAgentIdentityCompatRegistrationServer(t, newTaskID)
	account := newAgentIdentityTestAccount(t, 2301, runtimeID, oldTaskID)
	account.Name = "agent-http-recovery"
	account.Credentials["chatgpt_account_id"] = "account-http-recovery"
	repo := &agentIdentityCompatRepo{account: cloneAgentIdentityAccountForTest(account)}
	privateKey := account.GetCredential("agent_private_key")
	successBody := `{"id":"resp-agent","object":"response","model":"gpt-5.4","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
	upstream := &agentIdentityCompatUpstream{outcomes: []agentIdentityCompatOutcome{
		{response: newAgentIdentityCompatResponse(http.StatusUnauthorized, `{"error":{"code":"invalid_task_id"}}`)},
		{response: newAgentIdentityCompatResponse(http.StatusOK, successBody)},
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		accountRepo:  repo,
		httpUpstream: upstream,
	}
	requestBody := []byte(`{"model":"gpt-5.4","instructions":"Reply OK","input":[],"stream":false}`)

	result, err := svc.Forward(context.Background(), newAgentIdentityCompatGinContext(requestBody), account, requestBody)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.EqualValues(t, 1, registrationCalls.Load())
	require.Equal(t, 1, repo.credentialWriteCount())
	require.Equal(t, newTaskID, account.GetCredential("task_id"))
	requests := upstream.requestSnapshot()
	require.Len(t, requests, 2)
	firstRuntime, firstTask := decodeAgentIdentityCompatAssertion(t, requests[0].Header.Get("Authorization"))
	secondRuntime, secondTask := decodeAgentIdentityCompatAssertion(t, requests[1].Header.Get("Authorization"))
	require.Equal(t, runtimeID, firstRuntime)
	require.Equal(t, runtimeID, secondRuntime)
	require.Equal(t, oldTaskID, firstTask)
	require.Equal(t, newTaskID, secondTask)
	require.NotEqual(t, requests[0].Header.Get("Authorization"), requests[1].Header.Get("Authorization"))
	require.NotContains(t, requests[1].Header.Get("Authorization"), privateKey)

	account.Credentials["task_id"] = "task-http-old-second-request"
	repo.mu.Lock()
	repo.account.Credentials["task_id"] = "task-http-old-second-request"
	repo.mu.Unlock()
	secondOldTaskID := account.GetCredential("task_id")
	upstream.mu.Lock()
	upstream.outcomes = []agentIdentityCompatOutcome{
		{response: newAgentIdentityCompatResponse(http.StatusUnauthorized, `{"error":{"code":"invalid_task_id"}}`)},
		{response: newAgentIdentityCompatResponse(http.StatusUnauthorized, `{"error":{"code":"invalid_task_id","message":"`+runtimeID+` `+secondOldTaskID+` AgentAssertion leaked-assertion"}}`)},
	}
	upstream.mu.Unlock()

	_, err = svc.Forward(context.Background(), newAgentIdentityCompatGinContext(requestBody), account, requestBody)
	require.Error(t, err)
	require.EqualValues(t, 2, registrationCalls.Load(), "one client request may register at most one replacement task")
	require.Len(t, upstream.requestSnapshot(), 4)
	require.NotContains(t, err.Error(), runtimeID)
	require.NotContains(t, err.Error(), secondOldTaskID)
	require.NotContains(t, err.Error(), privateKey)
	require.NotContains(t, err.Error(), "leaked-assertion")
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.NotContains(t, string(failoverErr.ResponseBody), runtimeID)
	require.NotContains(t, string(failoverErr.ResponseBody), secondOldTaskID)
	require.NotContains(t, string(failoverErr.ResponseBody), "leaked-assertion")
}

func TestOpenAIAgentIdentityNonTaskFailuresDoNotRegisterTask(t *testing.T) {
	tests := []struct {
		name    string
		outcome agentIdentityCompatOutcome
	}{
		{
			name:    "ordinary 401",
			outcome: agentIdentityCompatOutcome{response: newAgentIdentityCompatResponse(http.StatusUnauthorized, `{"error":{"code":"invalid_token"}}`)},
		},
		{
			name:    "5xx with task marker",
			outcome: agentIdentityCompatOutcome{response: newAgentIdentityCompatResponse(http.StatusInternalServerError, `{"error":{"code":"invalid_task_id"}}`)},
		},
		{
			name:    "network error",
			outcome: agentIdentityCompatOutcome{err: errors.New("controlled upstream network failure")},
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registrationCalls := installAgentIdentityCompatRegistrationServer(t, "task-must-not-be-created")
			account := newAgentIdentityTestAccount(t, int64(2400+index), "runtime-non-task", "task-existing")
			account.Name = "agent-non-task-failure"
			account.Credentials["chatgpt_account_id"] = "account-non-task"
			repo := &agentIdentityCompatRepo{account: cloneAgentIdentityAccountForTest(account)}
			upstream := &agentIdentityCompatUpstream{outcomes: []agentIdentityCompatOutcome{test.outcome}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream}
			body := []byte(`{"model":"gpt-5.4","instructions":"Reply OK","input":[],"stream":false}`)

			_, err := svc.Forward(context.Background(), newAgentIdentityCompatGinContext(body), account, body)
			require.Error(t, err)
			require.EqualValues(t, 0, registrationCalls.Load())
			require.Equal(t, 0, repo.credentialWriteCount())
			require.Len(t, upstream.requestSnapshot(), 1)
		})
	}
}

func TestOpenAIAgentIdentityCompatibilityRoutesRecoverOnlyOnce(t *testing.T) {
	tests := []struct {
		name string
		path string
		body []byte
		call func(*OpenAIGatewayService, context.Context, *gin.Context, *Account, []byte) (*OpenAIForwardResult, error)
	}{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			body: []byte(`{"model":"gpt-5.4","stream":false,"messages":[{"role":"user","content":"hi"}]}`),
			call: func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return s.ForwardAsChatCompletions(ctx, c, account, body, "", "gpt-5.4")
			},
		},
		{
			name: "anthropic messages",
			path: "/v1/messages",
			body: []byte(`{"model":"gpt-5.4","stream":false,"max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`),
			call: func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return s.ForwardAsAnthropic(ctx, c, account, body, "", "gpt-5.4")
			},
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const newTaskID = "task-compat-new"
			registrationCalls := installAgentIdentityCompatRegistrationServer(t, newTaskID)
			account := newAgentIdentityTestAccount(t, int64(2450+index), "runtime-compat", "task-compat-old")
			account.Name = "agent-compat-route"
			account.Credentials["chatgpt_account_id"] = "account-compat-route"
			repo := &agentIdentityCompatRepo{account: cloneAgentIdentityAccountForTest(account)}
			upstream := &agentIdentityCompatUpstream{outcomes: []agentIdentityCompatOutcome{
				{response: newAgentIdentityCompatResponse(http.StatusUnauthorized, `{"error":{"code":"invalid_task_id"}}`)},
				{response: newAgentIdentityCompatResponse(http.StatusUnauthorized, `{"error":{"code":"invalid_task_id"}}`)},
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, test.path, bytes.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")

			_, err := test.call(svc, context.Background(), c, account, test.body)
			require.Error(t, err)
			require.EqualValues(t, 1, registrationCalls.Load())
			require.Equal(t, 1, repo.credentialWriteCount())
			requests := upstream.requestSnapshot()
			require.Len(t, requests, 2)
			_, secondTaskID := decodeAgentIdentityCompatAssertion(t, requests[1].Header.Get("Authorization"))
			require.Equal(t, newTaskID, secondTaskID)
		})
	}
}

func TestAccountTestCompactAgentIdentityRecoversAndInvalidatesWS(t *testing.T) {
	const newTaskID = "task-account-test-new"
	registrationCalls := installAgentIdentityCompatRegistrationServer(t, newTaskID)
	account := newAgentIdentityTestAccount(t, 2481, "runtime-account-test", "task-account-test-old")
	account.Credentials["chatgpt_account_id"] = "account-test-agent"
	repo := &agentIdentityCompatRepo{account: cloneAgentIdentityAccountForTest(account)}
	invalidator := &agentIdentityCompatInvalidator{}
	upstream := &agentIdentityCompatUpstream{outcomes: []agentIdentityCompatOutcome{
		{response: newAgentIdentityCompatResponse(http.StatusUnauthorized, `{"error":{"code":"invalid_task_id"}}`)},
		{response: newAgentIdentityCompatResponse(http.StatusOK, `{"id":"compact-agent","status":"completed"}`)},
	}}
	svc := &AccountTestService{
		accountRepo:                repo,
		httpUpstream:               upstream,
		cfg:                        &config.Config{},
		agentIdentityWSInvalidator: invalidator,
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/2481/test", nil)

	err := svc.testOpenAICompactConnection(c, account, "gpt-5.4")
	require.NoError(t, err)
	require.EqualValues(t, 1, registrationCalls.Load())
	require.Equal(t, 1, repo.credentialWriteCount())
	require.Equal(t, []int64{account.ID}, invalidator.snapshot())
	requests := upstream.requestSnapshot()
	require.Len(t, requests, 2)
	_, firstTaskID := decodeAgentIdentityCompatAssertion(t, requests[0].Header.Get("Authorization"))
	_, secondTaskID := decodeAgentIdentityCompatAssertion(t, requests[1].Header.Get("Authorization"))
	require.Equal(t, "task-account-test-old", firstTaskID)
	require.Equal(t, newTaskID, secondTaskID)
}

func TestOpenAIQuotaResetRejectsAgentIdentityBeforeQuotaPrerequisites(t *testing.T) {
	account := newAgentIdentityTestAccount(t, 2501, "runtime-reset", "task-reset")
	delete(account.Credentials, "chatgpt_account_id")
	repo := &agentIdentityCompatRepo{account: cloneAgentIdentityAccountForTest(account)}
	svc := NewOpenAIQuotaService(repo, nil, nil, nil)

	_, err := svc.ResetCredit(context.Background(), account.ID)
	require.Error(t, err)
	require.ErrorContains(t, err, "reset credits are not supported for Agent Identity accounts")
	require.NotContains(t, err.Error(), "chatgpt_account_id")
	require.Equal(t, 0, repo.credentialWriteCount())
}

func TestOpenAIWSIngressAllowsAgentIdentityWithoutBearerToken(t *testing.T) {
	account := newAgentIdentityTestAccount(t, 2601, "runtime-ingress", "task-ingress")
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeOff
	svc := &OpenAIGatewayService{cfg: cfg}
	c := newAgentIdentityCompatGinContext([]byte(`{"type":"response.create","model":"gpt-5.4","input":[]}`))

	err := svc.ProxyResponsesWebSocketFromClient(
		context.Background(),
		c,
		new(coderws.Conn),
		account,
		"",
		[]byte(`{"type":"response.create","model":"gpt-5.4","input":[]}`),
		nil,
	)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "token is empty")
	require.Contains(t, err.Error(), "websocket mode is disabled")

	regularOAuth := &Account{ID: 2602, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	err = svc.ProxyResponsesWebSocketFromClient(
		context.Background(),
		c,
		new(coderws.Conn),
		regularOAuth,
		"",
		[]byte(`{"type":"response.create","model":"gpt-5.4","input":[]}`),
		nil,
	)
	require.EqualError(t, err, "token is empty")
}

type agentIdentityCompatWSDialer struct {
	mu             sync.Mutex
	responseBody   []byte
	responseBodies [][]byte
	authorizations []string
}

func (d *agentIdentityCompatWSDialer) Dial(_ context.Context, _ string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	d.authorizations = append(d.authorizations, headers.Get("Authorization"))
	responseBody := d.responseBody
	if len(d.responseBodies) > 0 {
		responseBody = d.responseBodies[0]
		d.responseBodies = d.responseBodies[1:]
	}
	d.mu.Unlock()
	return nil, http.StatusUnauthorized, http.Header{"X-Request-Id": []string{"ws-malformed"}}, &openAIWSHandshakeError{
		Body: append([]byte(nil), responseBody...),
		Err:  errors.New("controlled websocket handshake rejection"),
	}
}

func (d *agentIdentityCompatWSDialer) snapshot() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.authorizations))
	copy(out, d.authorizations)
	return out
}

func TestOpenAIWSV2PassthroughAgentIdentityUsesDynamicAssertionAndRejectsMalformedRecovery(t *testing.T) {
	t.Run("valid invalid task reloads assertion once", func(t *testing.T) {
		const newTaskID = "task-ws-new"
		registrationCalls := installAgentIdentityCompatRegistrationServer(t, newTaskID)
		account := newAgentIdentityTestAccount(t, 2699, "runtime-ws", "task-ws-old")
		repo := &agentIdentityCompatRepo{account: cloneAgentIdentityAccountForTest(account)}
		dialer := &agentIdentityCompatWSDialer{responseBodies: [][]byte{
			[]byte(`{"error":{"code":"invalid_task_id"}}`),
			[]byte(`{"error":{"code":"invalid_token"}}`),
		}}
		svc := &OpenAIGatewayService{
			cfg:                       &config.Config{},
			accountRepo:               repo,
			openaiWSPassthroughDialer: dialer,
		}
		c := newAgentIdentityCompatGinContext([]byte(`{"type":"response.create","model":"gpt-5.4","input":[]}`))

		err := svc.proxyResponsesWebSocketV2Passthrough(
			context.Background(),
			c,
			new(coderws.Conn),
			account,
			"",
			[]byte(`{"type":"response.create","model":"gpt-5.4","input":[]}`),
			nil,
			OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
		)
		require.Error(t, err)
		require.EqualValues(t, 1, registrationCalls.Load())
		require.Equal(t, 1, repo.credentialWriteCount())
		authorizations := dialer.snapshot()
		require.Len(t, authorizations, 2)
		firstRuntimeID, firstTaskID := decodeAgentIdentityCompatAssertion(t, authorizations[0])
		secondRuntimeID, secondTaskID := decodeAgentIdentityCompatAssertion(t, authorizations[1])
		require.Equal(t, "runtime-ws", firstRuntimeID)
		require.Equal(t, "runtime-ws", secondRuntimeID)
		require.Equal(t, "task-ws-old", firstTaskID)
		require.Equal(t, newTaskID, secondTaskID)
		require.NotEqual(t, authorizations[0], authorizations[1])
		require.NotContains(t, err.Error(), authorizations[0])
		require.NotContains(t, err.Error(), authorizations[1])
	})

	tests := []struct {
		name string
		body []byte
	}{
		{name: "truncated json", body: []byte(`{"error":{"code":"invalid_task_id"`)},
		{name: "plain marker", body: []byte(`invalid_task_id`)},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registrationCalls := installAgentIdentityCompatRegistrationServer(t, "task-ws-unexpected")
			account := newAgentIdentityTestAccount(t, int64(2700+index), "runtime-ws", "task-ws-current")
			repo := &agentIdentityCompatRepo{account: cloneAgentIdentityAccountForTest(account)}
			dialer := &agentIdentityCompatWSDialer{responseBody: test.body}
			svc := &OpenAIGatewayService{
				cfg:                       &config.Config{},
				accountRepo:               repo,
				openaiWSPassthroughDialer: dialer,
			}
			c := newAgentIdentityCompatGinContext([]byte(`{"type":"response.create","model":"gpt-5.4","input":[]}`))

			err := svc.proxyResponsesWebSocketV2Passthrough(
				context.Background(),
				c,
				new(coderws.Conn),
				account,
				"",
				[]byte(`{"type":"response.create","model":"gpt-5.4","input":[]}`),
				nil,
				OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
			)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "token is empty")
			require.EqualValues(t, 0, registrationCalls.Load(), "malformed websocket 401 must not rotate task")
			require.Equal(t, 0, repo.credentialWriteCount())
			authorizations := dialer.snapshot()
			require.Len(t, authorizations, 1)
			runtimeID, taskID := decodeAgentIdentityCompatAssertion(t, authorizations[0])
			require.Equal(t, "runtime-ws", runtimeID)
			require.Equal(t, "task-ws-current", taskID)
			require.NotContains(t, err.Error(), authorizations[0])
		})
	}
}
