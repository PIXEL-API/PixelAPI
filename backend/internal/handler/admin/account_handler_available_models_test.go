package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type availableModelsAdminService struct {
	*stubAdminService
	account service.Account
}

func (s *availableModelsAdminService) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	if s.account.ID == id {
		acc := s.account
		return &acc, nil
	}
	return s.stubAdminService.GetAccount(context.Background(), id)
}

// availableModelsCatalogStub 实现 service.PricedModelCatalog 窄接口。
type availableModelsCatalogStub struct {
	models []string
}

func (s *availableModelsCatalogStub) ListPricedModelIDs(_ context.Context, _ []string) ([]string, error) {
	return nil, nil
}

func (s *availableModelsCatalogStub) ListSelectablePricedModelIDs(_ context.Context, _ service.PricedModelQuery) ([]string, error) {
	return s.models, nil
}

func (s *availableModelsCatalogStub) IsModelPriced(_ context.Context, _ service.PricedModelQuery, modelID string) (bool, error) {
	for _, m := range s.models {
		if m == modelID {
			return true, nil
		}
	}
	return false, nil
}

func setupAvailableModelsRouter(adminSvc service.AdminService, catalog service.PricedModelCatalog) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	testSvc := &service.AccountTestService{}
	testSvc.SetModelResolver(service.NewAccountTestModelResolver(catalog))
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, testSvc, nil, nil, nil, nil, nil)
	router.GET("/api/v1/admin/accounts/:id/models", handler.GetAvailableModels)
	return router
}

func TestAccountHandlerGetAvailableModels_OwnedAccountReturnsPricedIntersection(t *testing.T) {
	ownerUserID := int64(7)
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:          42,
			Name:        "openai-oauth",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			OwnerUserID: &ownerUserID,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5": "gpt-5.1",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc, &availableModelsCatalogStub{models: []string{"gpt-5", "gpt-6"}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "gpt-5", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_PlatformAccountNoMappingUsesCatalog(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       43,
			Name:     "openai-platform",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
		},
	}
	router := setupAvailableModelsRouter(svc, &availableModelsCatalogStub{models: []string{"gpt-5", "gpt-6"}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/43/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 2)
}

func TestAccountHandlerGetAvailableModels_PlatformAccountExplicitMappingFilters(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       44,
			Name:     "openai-platform-mapping",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"gpt-5": "gpt-5.1"},
			},
		},
	}
	router := setupAvailableModelsRouter(svc, &availableModelsCatalogStub{models: []string{"gpt-5", "gpt-6"}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/44/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "gpt-5", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_OwnedAccountEmptyWhitelistReturnsWhitelistMissing(t *testing.T) {
	ownerUserID := int64(7)
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:          46,
			Name:        "grok-owned-legacy",
			Platform:    service.PlatformGrok,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			OwnerUserID: &ownerUserID,
			Credentials: map[string]any{},
		},
	}
	router := setupAvailableModelsRouter(svc, &availableModelsCatalogStub{models: []string{"grok-4.5"}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/46/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "ACCOUNT_TEST_MODEL_WHITELIST_MISSING")
}

func TestAccountHandlerGetAvailableModels_OwnedAccountNoPricedIntersection(t *testing.T) {
	ownerUserID := int64(7)
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:          47,
			Name:        "openai-owned-unpriced",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			OwnerUserID: &ownerUserID,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"gpt-9": "gpt-9"},
			},
		},
	}
	router := setupAvailableModelsRouter(svc, &availableModelsCatalogStub{models: []string{"gpt-5"}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/47/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "ACCOUNT_TEST_MODEL_NO_PRICED_INTERSECTION")
}

func TestAccountHandlerGetAvailableModels_RejectsUnsupportedPlatform(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       48,
			Name:     "unsupported",
			Platform: "unsupported",
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
		},
	}
	router := setupAvailableModelsRouter(svc, &availableModelsCatalogStub{models: []string{"model-a"}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/48/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "ACCOUNT_TEST_UNSUPPORTED_PLATFORM")
	require.NotContains(t, rec.Body.String(), "claude-sonnet")
}
