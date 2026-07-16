package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexModelsFailoverAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r codexModelsFailoverAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for index := range r.accounts {
		if r.accounts[index].ID == id {
			account := r.accounts[index]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r codexModelsFailoverAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	accounts := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

type codexModelsFailoverHTTPUpstream struct {
	service.HTTPUpstream
	mu          sync.Mutex
	accountIDs  []int64
	firstErr    error
	firstStatus int
	statuses    map[int64]int
}

func (u *codexModelsFailoverHTTPUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.accountIDs = append(u.accountIDs, accountID)
	u.mu.Unlock()

	status, hasStatus := u.statuses[accountID]
	if accountID == 1 || hasStatus {
		if u.firstErr != nil {
			return nil, u.firstErr
		}
		if !hasStatus {
			status = u.firstStatus
		}
		if status == 0 {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"upstream unavailable"}}`)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"models":[{"slug":"gpt-5.6-sol"}]}`)),
	}, nil
}

func (u *codexModelsFailoverHTTPUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func TestCodexModelsCanceledRequestDoesNotWriteResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil).WithContext(ctx)

	(&OpenAIGatewayHandler{}).CodexModels(c)
	require.False(t, c.Writer.Written())
}

func TestCodexModelsRejectsNonOpenAIGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.144.0", nil)
	groupID := int64(42)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformGrok},
	})

	(&OpenAIGatewayHandler{}).CodexModels(c)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestCodexModelsFailsOverFromRetryableFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		err    error
	}{
		{name: "429", status: http.StatusTooManyRequests},
		{name: "500", status: http.StatusInternalServerError},
		{name: "502", status: http.StatusBadGateway},
		{name: "503", status: http.StatusServiceUnavailable},
		{name: "504", status: http.StatusGatewayTimeout},
		{name: "transport", status: http.StatusServiceUnavailable, err: &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, upstream, groupID := newCodexModelsFailoverTestHandler(test.status)
			upstream.firstErr = test.err
			recorder := performCodexModelsRequest(t, handler, groupID)

			require.Equal(t, []int64{1, 2}, upstream.calls())
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.JSONEq(t, `{"models":[{"slug":"gpt-5.6-sol"}]}`, recorder.Body.String())
		})
	}
}

func TestCodexModelsDoesNotFailOverFromPermanentFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		err    error
	}{
		{name: "400", status: http.StatusBadRequest},
		{name: "401", status: http.StatusUnauthorized},
		{name: "403", status: http.StatusForbidden},
		{name: "404", status: http.StatusNotFound},
		{name: "600", status: 600},
		{name: "configuration", status: http.StatusServiceUnavailable, err: errors.New("invalid proxy URL")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, upstream, groupID := newCodexModelsFailoverTestHandler(test.status)
			upstream.firstErr = test.err
			recorder := performCodexModelsRequest(t, handler, groupID)

			require.Equal(t, []int64{1}, upstream.calls())
			require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
		})
	}
}

func TestCodexModelsReturnsLastErrorWhenAccountsExhausted(t *testing.T) {
	handler, upstream, groupID := newCodexModelsFailoverTestHandler(http.StatusServiceUnavailable)
	upstream.statuses = map[int64]int{1: http.StatusServiceUnavailable, 2: http.StatusGatewayTimeout}
	recorder := performCodexModelsRequest(t, handler, groupID)

	require.Equal(t, []int64{1, 2}, upstream.calls())
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Body.String(), "upstream error 504")
}

func TestCodexModelsHonorsAccountSwitchLimit(t *testing.T) {
	handler, upstream, groupID := newCodexModelsFailoverTestHandlerWithAccountCount(http.StatusServiceUnavailable, 4, 2)
	upstream.statuses = map[int64]int{
		1: http.StatusServiceUnavailable,
		2: http.StatusBadGateway,
		3: http.StatusGatewayTimeout,
		4: http.StatusInternalServerError,
	}
	recorder := performCodexModelsRequest(t, handler, groupID)

	require.Equal(t, []int64{1, 2, 3}, upstream.calls())
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Body.String(), "upstream error 504")
}

func newCodexModelsFailoverTestHandler(firstStatus int) (*OpenAIGatewayHandler, *codexModelsFailoverHTTPUpstream, int64) {
	return newCodexModelsFailoverTestHandlerWithAccountCount(firstStatus, 2, 3)
}

func newCodexModelsFailoverTestHandlerWithAccountCount(firstStatus, accountCount, maxSwitches int) (*OpenAIGatewayHandler, *codexModelsFailoverHTTPUpstream, int64) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	accounts := make([]service.Account, 0, accountCount)
	for index := 1; index <= accountCount; index++ {
		accounts = append(accounts, service.Account{
			ID:          int64(index),
			Name:        fmt.Sprintf("upstream-%d", index),
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Status:      service.StatusActive,
			Schedulable: true,
			Priority:    index - 1,
			Concurrency: 1,
			Credentials: map[string]any{
				"api_key":  fmt.Sprintf("sk-%d", index),
				"base_url": fmt.Sprintf("https://upstream-%d.example/v1", index),
			},
		})
	}
	upstream := &codexModelsFailoverHTTPUpstream{firstStatus: firstStatus}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	gatewayService := service.NewOpenAIGatewayService(
		codexModelsFailoverAccountRepo{accounts: accounts},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		nil,
		nil,
		nil,
		upstream,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	return &OpenAIGatewayHandler{gatewayService: gatewayService, maxAccountSwitches: maxSwitches}, upstream, groupID
}

func performCodexModelsRequest(t *testing.T, handler *OpenAIGatewayHandler, groupID int64) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.144.0", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI},
	})
	handler.CodexModels(c)
	return recorder
}
