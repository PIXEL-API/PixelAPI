package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// batchTestAdminService 覆盖 GetAccountsByIDs，返回带平台/白名单的账号。
type batchTestAdminService struct {
	*stubAdminService
	accounts []*service.Account
}

func (s *batchTestAdminService) GetAccountsByIDs(_ context.Context, _ []int64) ([]*service.Account, error) {
	return s.accounts, nil
}

func setupBatchTestModelsRouter(adminSvc service.AdminService, catalog service.PricedModelCatalog) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	testSvc := &service.AccountTestService{}
	testSvc.SetModelResolver(service.NewAccountTestModelResolver(catalog))
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, testSvc, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts/batch-test/model-options", handler.GetBatchTestModelOptions)
	return router
}

func ownedGrokAccountForBatchTest(id int64, mapping map[string]any) *service.Account {
	ownerID := int64(7)
	credentials := map[string]any{}
	if mapping != nil {
		credentials["model_mapping"] = mapping
	}
	return &service.Account{
		ID:          id,
		Platform:    service.PlatformGrok,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		OwnerUserID: &ownerID,
		Credentials: credentials,
	}
}

func postBatchModelOptions(router *gin.Engine, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/batch-test/model-options", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func TestAccountHandlerGetBatchTestModelOptions_CommonIntersection(t *testing.T) {
	svc := &batchTestAdminService{
		stubAdminService: newStubAdminService(),
		accounts: []*service.Account{
			ownedGrokAccountForBatchTest(1, map[string]any{"grok-4.5": "grok-4.5", "grok-4.6": "grok-4.6"}),
			ownedGrokAccountForBatchTest(2, map[string]any{"grok-4.5": "grok-4.5"}),
		},
	}
	router := setupBatchTestModelsRouter(svc, &availableModelsCatalogStub{models: []string{"grok-4.5", "grok-4.6"}})

	rec := postBatchModelOptions(router, `{"account_ids":[1,2]}`)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "grok-4.5", resp.Data[0].ID)
}

func TestAccountHandlerGetBatchTestModelOptions_AccountNotFound(t *testing.T) {
	svc := &batchTestAdminService{
		stubAdminService: newStubAdminService(),
		accounts: []*service.Account{
			ownedGrokAccountForBatchTest(1, map[string]any{"grok-4.5": "grok-4.5"}),
		},
	}
	router := setupBatchTestModelsRouter(svc, &availableModelsCatalogStub{models: []string{"grok-4.5"}})

	rec := postBatchModelOptions(router, `{"account_ids":[1,999]}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAccountHandlerGetBatchTestModelOptions_MixedPlatforms(t *testing.T) {
	ownerID := int64(7)
	svc := &batchTestAdminService{
		stubAdminService: newStubAdminService(),
		accounts: []*service.Account{
			ownedGrokAccountForBatchTest(1, map[string]any{"grok-4.5": "grok-4.5"}),
			{
				ID:          2,
				Platform:    service.PlatformOpenAI,
				Type:        service.AccountTypeOAuth,
				Status:      service.StatusActive,
				OwnerUserID: &ownerID,
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}},
			},
		},
	}
	router := setupBatchTestModelsRouter(svc, &availableModelsCatalogStub{models: []string{"grok-4.5", "gpt-5"}})

	rec := postBatchModelOptions(router, `{"account_ids":[1,2]}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "all accounts must use the same platform")
}

func TestAccountHandlerGetBatchTestModelOptions_EmptyAccountIDs(t *testing.T) {
	router := setupBatchTestModelsRouter(newStubAdminService(), &availableModelsCatalogStub{})

	rec := postBatchModelOptions(router, `{"account_ids":[]}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}
