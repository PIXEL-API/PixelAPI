//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// failingAdminService 嵌入 stubAdminService，记录 BulkUpdateAccounts 的调用次数，
// 并可配置某个账号 ID 在批量更新中失败（由服务层统一汇总为部分成功）。
type failingAdminService struct {
	*stubAdminService
	failOnAccountID int64
	bulkCallCount   atomic.Int64
}

func (f *failingAdminService) BulkUpdateAccounts(ctx context.Context, input *service.BulkUpdateAccountsInput) (*service.BulkUpdateAccountsResult, error) {
	f.bulkCallCount.Add(1)
	result := &service.BulkUpdateAccountsResult{
		SuccessIDs: make([]int64, 0, len(input.AccountIDs)),
		FailedIDs:  make([]int64, 0),
	}
	for _, id := range input.AccountIDs {
		if id == f.failOnAccountID {
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, id)
			continue
		}
		result.Success++
		result.SuccessIDs = append(result.SuccessIDs, id)
	}
	return result, nil
}

func setupAccountHandlerWithService(adminSvc service.AdminService) (*gin.Engine, *AccountHandler) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts/batch-update-credentials", handler.BatchUpdateCredentials)
	return router, handler
}

func TestBatchUpdateCredentials_AllSuccess(t *testing.T) {
	svc := &failingAdminService{stubAdminService: newStubAdminService()}
	router, _ := setupAccountHandlerWithService(svc)

	body, _ := json.Marshal(BatchUpdateCredentialsRequest{
		AccountIDs: []int64{1, 2, 3},
		Field:      "account_uuid",
		Value:      "test-uuid",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "全部成功时应返回 200")
	require.Equal(t, int64(1), svc.bulkCallCount.Load(), "应调用一次 BulkUpdateAccounts")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	require.Equal(t, float64(3), data["success"], "应有 3 个成功")
	require.Equal(t, float64(0), data["failed"], "应有 0 个失败")
}

func TestBatchUpdateCredentials_PartialFailure(t *testing.T) {
	// 让第 2 个账号（ID=2）更新时失败
	svc := &failingAdminService{
		stubAdminService: newStubAdminService(),
		failOnAccountID:  2,
	}
	router, _ := setupAccountHandlerWithService(svc)

	body, _ := json.Marshal(BatchUpdateCredentialsRequest{
		AccountIDs: []int64{1, 2, 3},
		Field:      "org_uuid",
		Value:      "test-org",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// 实现采用"部分成功"模式：总是返回 200 + 成功/失败明细
	require.Equal(t, http.StatusOK, w.Code, "批量更新返回 200 + 成功/失败明细")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	require.Equal(t, float64(2), data["success"], "应有 2 个成功")
	require.Equal(t, float64(1), data["failed"], "应有 1 个失败")

	// 服务层统一处理批量，handler 只调用一次 BulkUpdateAccounts
	require.Equal(t, int64(1), svc.bulkCallCount.Load(),
		"应调用一次 BulkUpdateAccounts（部分成功由服务层汇总）")
}

func TestBatchUpdateCredentials_ServiceNotFoundMapsTo404(t *testing.T) {
	// 服务层校验目标账号不存在时返回 NotFound，handler 应透传为 404。
	svc := newStubAdminService()
	svc.bulkUpdateAccountErr = infraerrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")
	router, _ := setupAccountHandlerWithService(svc)

	body, _ := json.Marshal(BatchUpdateCredentialsRequest{
		AccountIDs: []int64{1, 2, 3},
		Field:      "account_uuid",
		Value:      "test",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code, "服务层 NotFound 应映射为 404")
}

func TestBatchUpdateCredentials_InterceptWarmupRequests_NonBool(t *testing.T) {
	svc := &failingAdminService{stubAdminService: newStubAdminService()}
	router, _ := setupAccountHandlerWithService(svc)

	// intercept_warmup_requests 传入非 bool 类型（string），应返回 400
	body, _ := json.Marshal(map[string]any{
		"account_ids": []int64{1},
		"field":       "intercept_warmup_requests",
		"value":       "not-a-bool",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code,
		"intercept_warmup_requests 传入非 bool 值应返回 400")
}

func TestBatchUpdateCredentials_InterceptWarmupRequests_ValidBool(t *testing.T) {
	svc := &failingAdminService{stubAdminService: newStubAdminService()}
	router, _ := setupAccountHandlerWithService(svc)

	body, _ := json.Marshal(map[string]any{
		"account_ids": []int64{1},
		"field":       "intercept_warmup_requests",
		"value":       true,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"intercept_warmup_requests 传入合法 bool 值应返回 200")
}

func TestBatchUpdateCredentials_AccountUUID_NonString(t *testing.T) {
	svc := &failingAdminService{stubAdminService: newStubAdminService()}
	router, _ := setupAccountHandlerWithService(svc)

	// account_uuid 传入非 string 类型（number），应返回 400
	body, _ := json.Marshal(map[string]any{
		"account_ids": []int64{1},
		"field":       "account_uuid",
		"value":       12345,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code,
		"account_uuid 传入非 string 值应返回 400")
}

func TestBatchUpdateCredentials_AccountUUID_NullValue(t *testing.T) {
	svc := &failingAdminService{stubAdminService: newStubAdminService()}
	router, _ := setupAccountHandlerWithService(svc)

	// account_uuid 传入 null（设置为空），应正常通过
	body, _ := json.Marshal(map[string]any{
		"account_ids": []int64{1},
		"field":       "account_uuid",
		"value":       nil,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"account_uuid 传入 null 应返回 200")
}
