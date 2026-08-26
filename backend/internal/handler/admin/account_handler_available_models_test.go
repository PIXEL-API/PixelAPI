package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func setupAvailableModelsRouter(adminSvc service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.GET("/api/v1/admin/accounts/:id/models", handler.GetAvailableModels)
	return router
}

func TestAccountHandlerGetAvailableModels_OpenAIOAuthUsesExplicitModelMapping(t *testing.T) {
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
	router := setupAvailableModelsRouter(svc)

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

func TestAccountHandlerGetAvailableModels_OpenAIOAuthPassthroughFallsBackToDefaults(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       43,
			Name:     "openai-oauth-passthrough",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5": "gpt-5.1",
				},
			},
			Extra: map[string]any{
				"openai_passthrough": true,
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

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
	require.NotEmpty(t, resp.Data)
	require.NotEqual(t, "gpt-5", resp.Data[0].ID)
	var foundCodexAutoReview bool
	for _, model := range resp.Data {
		if model.ID == "codex-auto-review" {
			foundCodexAutoReview = true
			break
		}
	}
	require.True(t, foundCodexAutoReview)
}

func TestAccountHandlerGetAvailableModels_GeminiGoogleOneRespectsMappingPrecedence(t *testing.T) {
	ownerUserID := int64(7)
	tests := []struct {
		name        string
		ownerUserID *int64
		credentials map[string]any
		wantIDs     []string
	}{
		{
			name: "platform account uses conservative defaults",
			credentials: map[string]any{
				"oauth_type": "google_one",
			},
			wantIDs: []string{"gemini-2.0-flash", "gemini-2.5-flash", "gemini-2.5-pro"},
		},
		{
			name: "platform account preserves explicit mapping",
			credentials: map[string]any{
				"oauth_type": "google_one",
				"model_mapping": map[string]any{
					"custom-model": "gemini-2.5-flash",
				},
			},
			wantIDs: []string{"custom-model"},
		},
		{
			name:        "owned account uses strict whitelist",
			ownerUserID: &ownerUserID,
			credentials: map[string]any{
				"oauth_type": "google_one",
				"model_mapping": map[string]any{
					"owner-model": "gemini-2.5-pro",
				},
			},
			wantIDs: []string{"owner-model"},
		},
		{
			name:        "owned account with empty whitelist returns no models",
			ownerUserID: &ownerUserID,
			credentials: map[string]any{
				"oauth_type":    "google_one",
				"model_mapping": map[string]any{},
			},
			wantIDs: []string{},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &availableModelsAdminService{
				stubAdminService: newStubAdminService(),
				account: service.Account{
					ID:          int64(45 + i),
					Name:        "gemini-google-one",
					Platform:    service.PlatformGemini,
					Type:        service.AccountTypeOAuth,
					Status:      service.StatusActive,
					OwnerUserID: tt.ownerUserID,
					Credentials: tt.credentials,
				},
			}
			router := setupAvailableModelsRouter(svc)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/"+strconv.Itoa(45+i)+"/models", nil)
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			var resp struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			ids := make([]string, 0, len(resp.Data))
			for _, model := range resp.Data {
				ids = append(ids, model.ID)
			}
			require.ElementsMatch(t, tt.wantIDs, ids)
		})
	}
}

func TestAccountHandlerGetAvailableModels_OwnedOpenAIEmptyWhitelistReturnsNoModels(t *testing.T) {
	ownerUserID := int64(7)
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:          46,
			Name:        "openai-owned-legacy",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			OwnerUserID: &ownerUserID,
			Credentials: map[string]any{"model_mapping": map[string]any{}},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/46/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Empty(t, resp.Data)
}

func TestAccountHandlerGetAvailableModels_RejectsUnsupportedPlatform(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       44,
			Name:     "unsupported",
			Platform: "unsupported",
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/44/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Unsupported account platform")
	require.NotContains(t, rec.Body.String(), "claude-sonnet")
}
