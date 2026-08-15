//go:build unit

package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExtractGrokTTSInputText(t *testing.T) {
	t.Parallel()
	require.Equal(t, "你好 Grok", extractGrokTTSInputText([]byte(`{"input":" 你好 Grok "}`)))
	require.Equal(t, "fallback", extractGrokTTSInputText([]byte(`{"text":"fallback"}`)))
	require.Empty(t, extractGrokTTSInputText([]byte(`{"input":42}`)))
	require.Empty(t, extractGrokTTSInputText([]byte(`not-json`)))
}

func TestGrokRealtimeBillingRequiresObservedAudio(t *testing.T) {
	t.Parallel()
	require.Nil(t, grokRealtimeBillingResult("grok-voice-latest", time.Second, false))
	require.Nil(t, grokRealtimeBillingResult("grok-voice-latest", 0, true))

	first := grokRealtimeBillingResult("grok-voice-latest", 90*time.Second, true)
	second := grokRealtimeBillingResult("grok-voice-latest", 90*time.Second, true)
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.NotEqual(t, first.RequestID, second.RequestID)
	require.Equal(t, "realtime", first.AudioUsage.Mode)
	require.Equal(t, 1.5, first.AudioUsage.DurationOrUnits)
}

func TestIsExpectedGrokRealtimeClose(t *testing.T) {
	t.Parallel()
	for _, status := range []coderws.StatusCode{
		coderws.StatusNormalClosure,
		coderws.StatusGoingAway,
		coderws.StatusNoStatusRcvd,
		coderws.StatusAbnormalClosure,
	} {
		require.True(t, isExpectedGrokRealtimeClose(coderws.CloseError{Code: status}), status)
	}
	require.False(t, isExpectedGrokRealtimeClose(coderws.CloseError{Code: coderws.StatusPolicyViolation}))
}

func TestGrokRealtimePreAcceptSwitchesCredentialAccountBeforeAccept(t *testing.T) {
	accounts := []*service.Account{{ID: 1}, {ID: 2}}
	selectCalls := 0
	releaseCalls := map[int64]int{}
	reportedFailures := make([]int64, 0, 1)
	acceptCalls := 0

	prepared, err := prepareGrokRealtimeClient(1, grokRealtimePreAcceptOps{
		selectAccount: func(failedAccountIDs map[int64]struct{}) (*service.AccountSelectionResult, error) {
			selectCalls++
			switch selectCalls {
			case 1:
				require.Empty(t, failedAccountIDs)
				return &service.AccountSelectionResult{Account: accounts[0]}, nil
			case 2:
				require.Contains(t, failedAccountIDs, accounts[0].ID)
				require.NotContains(t, failedAccountIDs, accounts[1].ID)
				return &service.AccountSelectionResult{Account: accounts[1]}, nil
			default:
				t.Fatalf("unexpected account selection call %d", selectCalls)
				return nil, nil
			}
		},
		acquireAccount: func(selection *service.AccountSelectionResult) (*service.Account, func(), bool) {
			account := selection.Account
			return account, func() { releaseCalls[account.ID]++ }, true
		},
		getCredential: func(account *service.Account) (string, error) {
			if account.ID == accounts[0].ID {
				return "", newGrokRealtimeCredentialFailoverError()
			}
			return "token-2", nil
		},
		reportFailure: func(accountID int64, _ *service.UpstreamFailoverError) {
			reportedFailures = append(reportedFailures, accountID)
		},
		accept: func() (*coderws.Conn, error) {
			acceptCalls++
			require.Equal(t, 1, releaseCalls[accounts[0].ID], "failed account slot must be released before websocket accept")
			require.Zero(t, releaseCalls[accounts[1].ID], "selected account slot must remain held through websocket accept")
			return new(coderws.Conn), nil
		},
	})

	require.NoError(t, err)
	require.NotNil(t, prepared)
	require.Nil(t, prepared.exhausted)
	require.Same(t, accounts[1], prepared.account)
	require.Equal(t, "token-2", prepared.token)
	require.NotNil(t, prepared.client)
	require.Equal(t, 2, selectCalls)
	require.Equal(t, 1, acceptCalls)
	require.Equal(t, []int64{accounts[0].ID}, reportedFailures)
	require.Equal(t, 1, releaseCalls[accounts[0].ID])
	require.Zero(t, releaseCalls[accounts[1].ID])

	require.NotNil(t, prepared.release)
	prepared.release()
	require.Equal(t, 1, releaseCalls[accounts[1].ID])
}

func TestGrokRealtimeCredentialExhaustionReturns503(t *testing.T) {
	accounts := []*service.Account{{ID: 1}, {ID: 2}}
	selectCalls := 0
	releaseCalls := map[int64]int{}
	acceptCalls := 0

	prepared, err := prepareGrokRealtimeClient(1, grokRealtimePreAcceptOps{
		selectAccount: func(failedAccountIDs map[int64]struct{}) (*service.AccountSelectionResult, error) {
			selectCalls++
			if selectCalls > len(accounts) {
				return nil, nil
			}
			account := accounts[selectCalls-1]
			if selectCalls == 2 {
				require.Contains(t, failedAccountIDs, accounts[0].ID)
			}
			return &service.AccountSelectionResult{Account: account}, nil
		},
		acquireAccount: func(selection *service.AccountSelectionResult) (*service.Account, func(), bool) {
			account := selection.Account
			return account, func() { releaseCalls[account.ID]++ }, true
		},
		getCredential: func(*service.Account) (string, error) {
			return "", newGrokRealtimeCredentialFailoverError()
		},
		accept: func() (*coderws.Conn, error) {
			acceptCalls++
			return new(coderws.Conn), nil
		},
	})

	require.NoError(t, err)
	require.NotNil(t, prepared)
	require.NotNil(t, prepared.exhausted)
	require.True(t, prepared.exhausted.IsCredentialFailure())
	require.Equal(t, http.StatusServiceUnavailable, prepared.exhausted.ClientStatusCode)
	require.Equal(t, 2, selectCalls)
	require.Zero(t, acceptCalls, "credential exhaustion must be reported before websocket accept")
	require.Equal(t, 1, releaseCalls[accounts[0].ID])
	require.Equal(t, 1, releaseCalls[accounts[1].ID])

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, prepared.exhausted, false)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotEqual(t, http.StatusBadGateway, recorder.Code)
}

func TestGrokRealtimeAcceptFailureDoesNotSwitchAccounts(t *testing.T) {
	account := &service.Account{ID: 1}
	acceptErr := errors.New("accept failed")
	selectCalls := 0
	releaseCalls := 0

	prepared, err := prepareGrokRealtimeClient(3, grokRealtimePreAcceptOps{
		selectAccount: func(map[int64]struct{}) (*service.AccountSelectionResult, error) {
			selectCalls++
			return &service.AccountSelectionResult{Account: account}, nil
		},
		acquireAccount: func(selection *service.AccountSelectionResult) (*service.Account, func(), bool) {
			return selection.Account, func() { releaseCalls++ }, true
		},
		getCredential: func(*service.Account) (string, error) {
			return "token-1", nil
		},
		accept: func() (*coderws.Conn, error) {
			return nil, acceptErr
		},
	})

	require.ErrorIs(t, err, acceptErr)
	require.Nil(t, prepared)
	require.Equal(t, 1, selectCalls, "websocket accept failure must not enter account failover")
	require.Equal(t, 1, releaseCalls)
}

func TestGrokRealtimePreAcceptNonFailoverCredentialErrorReleasesSlot(t *testing.T) {
	account := &service.Account{ID: 1}
	credentialErr := errors.New("credential provider failed")
	releaseCalls := 0
	acceptCalls := 0

	prepared, err := prepareGrokRealtimeClient(3, grokRealtimePreAcceptOps{
		selectAccount: func(map[int64]struct{}) (*service.AccountSelectionResult, error) {
			return &service.AccountSelectionResult{Account: account}, nil
		},
		acquireAccount: func(selection *service.AccountSelectionResult) (*service.Account, func(), bool) {
			return selection.Account, func() { releaseCalls++ }, true
		},
		getCredential: func(*service.Account) (string, error) {
			return "", credentialErr
		},
		accept: func() (*coderws.Conn, error) {
			acceptCalls++
			return new(coderws.Conn), nil
		},
	})

	require.ErrorIs(t, err, credentialErr)
	require.Nil(t, prepared)
	require.Equal(t, 1, releaseCalls)
	require.Zero(t, acceptCalls, "non-failover credential errors must stop before websocket accept")
}

func newGrokRealtimeCredentialFailoverError() *service.UpstreamFailoverError {
	return &service.UpstreamFailoverError{
		StatusCode:        http.StatusServiceUnavailable,
		Stage:             service.GatewayFailureStageAccountAuth,
		Scope:             service.GatewayFailureScopeAccount,
		NextAccountAction: service.NextAccountRetry,
		ClientStatusCode:  http.StatusServiceUnavailable,
		ClientMessage:     service.GrokCredentialUnavailableClientMessage,
	}
}
