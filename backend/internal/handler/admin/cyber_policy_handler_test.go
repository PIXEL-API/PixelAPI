package admin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cyberPolicyRestrictionServiceStub struct {
	state        service.CyberPolicyBlockState
	getErr       error
	clearRemoved bool
	clearErr     error
	getCalls     [][2]int64
	clearCalls   [][2]int64
}

type cyberPolicyRequestServiceStub struct {
	listResult  *service.CyberPolicyRequestList
	listErr     error
	detail      *service.CyberPolicyRequestDetail
	detailErr   error
	exportItems []*service.CyberPolicyRequestDetail
	truncated   bool
	exportErr   error
	listFilters []service.CyberPolicyRequestFilter
	exportCalls []service.CyberPolicyRequestFilter
	detailIDs   []int64
}

func (s *cyberPolicyRequestServiceStub) ListCyberPolicyRequests(
	_ context.Context,
	filter service.CyberPolicyRequestFilter,
) (*service.CyberPolicyRequestList, error) {
	s.listFilters = append(s.listFilters, filter)
	return s.listResult, s.listErr
}

func (s *cyberPolicyRequestServiceStub) GetCyberPolicyRequestByID(
	_ context.Context,
	id int64,
) (*service.CyberPolicyRequestDetail, error) {
	s.detailIDs = append(s.detailIDs, id)
	return s.detail, s.detailErr
}

func (s *cyberPolicyRequestServiceStub) ExportCyberPolicyRequests(
	_ context.Context,
	filter service.CyberPolicyRequestFilter,
) ([]*service.CyberPolicyRequestDetail, bool, error) {
	s.exportCalls = append(s.exportCalls, filter)
	return s.exportItems, s.truncated, s.exportErr
}

func (s *cyberPolicyRestrictionServiceStub) GetCyberPolicyRestriction(
	_ context.Context,
	userID, groupID int64,
) (service.CyberPolicyBlockState, error) {
	s.getCalls = append(s.getCalls, [2]int64{userID, groupID})
	return s.state, s.getErr
}

func (s *cyberPolicyRestrictionServiceStub) ClearCyberPolicyRestriction(
	_ context.Context,
	userID, groupID int64,
) (bool, error) {
	s.clearCalls = append(s.clearCalls, [2]int64{userID, groupID})
	return s.clearRemoved, s.clearErr
}

func newCyberPolicyHandlerTestRouter(stub cyberPolicyRestrictionService, requestServices ...cyberPolicyRequestService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &CyberPolicyHandler{service: stub}
	if len(requestServices) > 0 {
		h.opsService = requestServices[0]
	}
	router := gin.New()
	router.GET("/restrictions/users/:user_id/groups/:group_id", h.GetRestriction)
	router.DELETE("/restrictions/users/:user_id/groups/:group_id", h.ClearRestriction)
	router.GET("/requests", h.ListRequests)
	router.GET("/requests/export", h.ExportRequests)
	router.GET("/requests/:id", h.GetRequest)
	return router
}

func decodeCyberPolicyHandlerResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

func TestCyberPolicyHandlerGetRestriction(t *testing.T) {
	blockedUntil := time.Date(2026, 8, 12, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	stub := &cyberPolicyRestrictionServiceStub{state: service.CyberPolicyBlockState{
		Blocked:      true,
		Scope:        service.CyberPolicyBlockScopeUserGroupDay,
		RetryAfter:   90*time.Second + time.Millisecond,
		BlockedUntil: blockedUntil,
	}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/restrictions/users/445/groups/1198", nil)
	newCyberPolicyHandlerTestRouter(stub).ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, [][2]int64{{445, 1198}}, stub.getCalls)
	body := decodeCyberPolicyHandlerResponse(t, recorder)
	data := body["data"].(map[string]any)
	require.Equal(t, float64(445), data["user_id"])
	require.Equal(t, float64(1198), data["group_id"])
	require.Equal(t, true, data["blocked"])
	require.Equal(t, "user_group_day", data["scope"])
	require.Equal(t, float64(91), data["retry_after_seconds"])
	require.Equal(t, blockedUntil.Format(time.RFC3339), data["blocked_until"])
}

func TestCyberPolicyHandlerGetRestrictionReturnsUnblockedShape(t *testing.T) {
	stub := &cyberPolicyRestrictionServiceStub{}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/restrictions/users/445/groups/1198", nil)
	newCyberPolicyHandlerTestRouter(stub).ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	data := decodeCyberPolicyHandlerResponse(t, recorder)["data"].(map[string]any)
	require.Equal(t, false, data["blocked"])
	require.Equal(t, "", data["scope"])
	require.Nil(t, data["blocked_until"])
	require.Equal(t, float64(0), data["retry_after_seconds"])
}

func TestCyberPolicyHandlerClearRestrictionIsIdempotent(t *testing.T) {
	stub := &cyberPolicyRestrictionServiceStub{clearRemoved: true}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/restrictions/users/445/groups/1198", nil)
	newCyberPolicyHandlerTestRouter(stub).ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, [][2]int64{{445, 1198}}, stub.clearCalls)
	data := decodeCyberPolicyHandlerResponse(t, recorder)["data"].(map[string]any)
	require.Equal(t, true, data["removed"])
}

func TestCyberPolicyHandlerValidatesIDs(t *testing.T) {
	stub := &cyberPolicyRestrictionServiceStub{}
	tests := []string{
		"/restrictions/users/0/groups/1198",
		"/restrictions/users/not-a-number/groups/1198",
		"/restrictions/users/445/groups/-1",
	}
	for _, path := range tests {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		newCyberPolicyHandlerTestRouter(stub).ServeHTTP(recorder, req)
		require.Equal(t, http.StatusBadRequest, recorder.Code, path)
	}
	require.Empty(t, stub.getCalls)
}

func TestCyberPolicyHandlerSurfacesStoreErrors(t *testing.T) {
	stub := &cyberPolicyRestrictionServiceStub{
		getErr:   errors.New("redis unavailable"),
		clearErr: errors.New("redis unavailable"),
	}
	router := newCyberPolicyHandlerTestRouter(stub)

	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/restrictions/users/445/groups/1198", nil))
	require.Equal(t, http.StatusInternalServerError, getRecorder.Code)

	clearRecorder := httptest.NewRecorder()
	router.ServeHTTP(clearRecorder, httptest.NewRequest(http.MethodDelete, "/restrictions/users/445/groups/1198", nil))
	require.Equal(t, http.StatusInternalServerError, clearRecorder.Code)
}

func TestCyberPolicyHandlerListRequestsPassesAllFilters(t *testing.T) {
	stub := &cyberPolicyRequestServiceStub{listResult: &service.CyberPolicyRequestList{
		Items:    []*service.CyberPolicyRequest{{ID: 9, UserName: "alice", UserEmail: "alice@example.com", GroupName: "研发一组"}},
		Total:    1,
		Page:     2,
		PageSize: 50,
	}}
	query := url.Values{
		"from":        {"2026-08-01T00:00:00Z"},
		"to":          {"2026-08-02"},
		"group_query": {" 研发一组 "},
		"user_query":  {" alice@example.com "},
		"model":       {" gpt-5 "},
		"endpoint":    {" /v1/responses "},
		"page":        {"2"},
		"page_size":   {"50"},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/requests?"+query.Encode(), nil)

	newCyberPolicyHandlerTestRouter(nil, stub).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, stub.listFilters, 1)
	filter := stub.listFilters[0]
	require.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), *filter.StartTime)
	require.Equal(t, time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), *filter.EndTime)
	require.Equal(t, "研发一组", filter.GroupQuery)
	require.Equal(t, "alice@example.com", filter.UserQuery)
	require.Equal(t, "gpt-5", filter.Model)
	require.Equal(t, "/v1/responses", filter.Endpoint)
	require.Equal(t, 2, filter.Page)
	require.Equal(t, 50, filter.PageSize)
	data := decodeCyberPolicyHandlerResponse(t, recorder)["data"].(map[string]any)
	require.Equal(t, float64(1), data["total"])
	require.Equal(t, float64(2), data["page"])
	require.Len(t, data["items"].([]any), 1)
}

func TestCyberPolicyHandlerRequestTimeRangeValidation(t *testing.T) {
	stub := &cyberPolicyRequestServiceStub{}
	router := newCyberPolicyHandlerTestRouter(nil, stub)
	tests := []string{
		"/requests?from=2026-08-02T00:00:00Z&to=2026-08-02T00:00:00Z",
		"/requests?from=2026-06-01T00:00:00Z&to=2026-08-02T00:00:00Z",
		"/requests?from=not-a-date",
	}

	for _, path := range tests {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusBadRequest, recorder.Code, path)
	}
	require.Empty(t, stub.listFilters)
}

func TestCyberPolicyHandlerGetRequestValidationAndNotFound(t *testing.T) {
	stub := &cyberPolicyRequestServiceStub{
		detailErr: infraerrors.NotFound("OPS_CYBER_POLICY_REQUEST_NOT_FOUND", "Cyber Policy request not found"),
	}
	router := newCyberPolicyHandlerTestRouter(nil, stub)

	invalidRecorder := httptest.NewRecorder()
	router.ServeHTTP(invalidRecorder, httptest.NewRequest(http.MethodGet, "/requests/not-a-number", nil))
	require.Equal(t, http.StatusBadRequest, invalidRecorder.Code)
	require.Empty(t, stub.detailIDs)

	notFoundRecorder := httptest.NewRecorder()
	router.ServeHTTP(notFoundRecorder, httptest.NewRequest(http.MethodGet, "/requests/77", nil))
	require.Equal(t, http.StatusNotFound, notFoundRecorder.Code)
	require.Equal(t, []int64{77}, stub.detailIDs)
	require.Equal(t, "OPS_CYBER_POLICY_REQUEST_NOT_FOUND", decodeCyberPolicyHandlerResponse(t, notFoundRecorder)["reason"])
}

func TestCyberPolicyHandlerGetRequestReturnsDetail(t *testing.T) {
	stub := &cyberPolicyRequestServiceStub{detail: &service.CyberPolicyRequestDetail{
		CyberPolicyRequest: service.CyberPolicyRequest{ID: 9, UserName: "alice", GroupName: "研发一组"},
		RequestContent:     `{"input":"hello"}`,
	}}
	recorder := httptest.NewRecorder()

	newCyberPolicyHandlerTestRouter(nil, stub).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/requests/9", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int64{9}, stub.detailIDs)
	data := decodeCyberPolicyHandlerResponse(t, recorder)["data"].(map[string]any)
	require.Equal(t, float64(9), data["id"])
	require.Equal(t, `{"input":"hello"}`, data["request_content"])
}

func TestCyberPolicyHandlerExportRequestsWritesSafeCSV(t *testing.T) {
	groupID, userID, apiKeyID, accountID := int64(1198), int64(445), int64(21), int64(88)
	upstreamStatus, requestBytes := 403, 300000
	stub := &cyberPolicyRequestServiceStub{
		truncated: true,
		exportItems: []*service.CyberPolicyRequestDetail{{
			CyberPolicyRequest: service.CyberPolicyRequest{
				ID: 9, CreatedAt: time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC), RequestID: "=request",
				GroupID: &groupID, GroupName: "研发一组", UserID: &userID, UserName: " =SUM(1,1)", UserEmail: "+evil@example.com",
				APIKeyID: &apiKeyID, APIKeyName: "@key", AccountID: &accountID, AccountName: "account-a",
				RequestedModel: "-model", UpstreamModel: "gpt-5", InboundEndpoint: "/v1/responses", UpstreamEndpoint: "/v1/responses",
				StatusCode: 200, UpstreamStatusCode: &upstreamStatus, UpstreamErrorMessage: "cyber_policy: blocked",
				RequestContentTruncated: true, RequestContentBytes: &requestBytes,
			},
			RequestContent: " @cmd",
		}},
	}
	query := url.Values{
		"from":        {"2026-08-01T00:00:00Z"},
		"to":          {"2026-08-02T00:00:00Z"},
		"group_query": {"研发一组"},
		"user_query":  {"alice@example.com"},
	}
	recorder := httptest.NewRecorder()

	newCyberPolicyHandlerTestRouter(nil, stub).ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/requests/export?"+query.Encode(), nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "text/csv; charset=utf-8", recorder.Header().Get("Content-Type"))
	require.Contains(t, recorder.Header().Get("Content-Disposition"), "cyber-policy-requests-")
	require.Equal(t, "1000", recorder.Header().Get("X-Export-Limit"))
	require.Equal(t, "true", recorder.Header().Get("X-Export-Truncated"))
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	require.True(t, strings.HasPrefix(recorder.Body.String(), "\xEF\xBB\xBF"))
	require.Len(t, stub.exportCalls, 1)
	require.Equal(t, "研发一组", stub.exportCalls[0].GroupQuery)
	require.Equal(t, "alice@example.com", stub.exportCalls[0].UserQuery)

	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(recorder.Body.String(), "\xEF\xBB\xBF")))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Len(t, records[0], 21)
	require.Len(t, records[1], len(records[0]))
	row := records[1]
	require.Equal(t, "'=request", row[1])
	require.Equal(t, "' =SUM(1,1)", row[4])
	require.Equal(t, "'+evil@example.com", row[5])
	require.Equal(t, "'@key", row[7])
	require.Equal(t, "'-model", row[11])
	require.Equal(t, "' @cmd", row[20])
}

func TestCyberPolicyHandlerExportRequestsPropagatesServiceError(t *testing.T) {
	stub := &cyberPolicyRequestServiceStub{exportErr: errors.New("export failed")}
	recorder := httptest.NewRecorder()

	newCyberPolicyHandlerTestRouter(nil, stub).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/requests/export", nil))

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Empty(t, recorder.Header().Get("Content-Disposition"))
	_, err := io.ReadAll(recorder.Body)
	require.NoError(t, err)
}
