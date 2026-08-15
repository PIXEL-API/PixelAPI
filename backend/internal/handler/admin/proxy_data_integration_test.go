package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type dataImportAdminService struct {
	*stubAdminService
	nextProxyID   int64
	proxyState    map[int64]service.Proxy
	proxyIDByName map[string]int64
}

func newDataImportAdminService(existing []service.Proxy) *dataImportAdminService {
	stub := newStubAdminService()
	stub.proxies = append([]service.Proxy(nil), existing...)
	state := make(map[int64]service.Proxy, len(existing))
	idsByName := make(map[string]int64, len(existing))
	for i := range existing {
		state[existing[i].ID] = existing[i]
		idsByName[existing[i].Name] = existing[i].ID
	}
	return &dataImportAdminService{
		stubAdminService: stub,
		nextProxyID:      100,
		proxyState:       state,
		proxyIDByName:    idsByName,
	}
}

func (s *dataImportAdminService) CreateProxy(_ context.Context, input *service.CreateProxyInput) (*service.Proxy, error) {
	s.nextProxyID++
	proxy := service.Proxy{
		ID:                   s.nextProxyID,
		Name:                 input.Name,
		Protocol:             input.Protocol,
		Host:                 input.Host,
		Port:                 input.Port,
		Username:             input.Username,
		Password:             input.Password,
		Platform:             input.Platform,
		RequiredAccountLevel: input.RequiredAccountLevel,
		Status:               service.StatusActive,
		MaxAccounts:          input.MaxAccounts,
		ExpiresAt:            input.ExpiresAt,
		FallbackMode:         input.FallbackMode,
		BackupProxyID:        input.BackupProxyID,
		ExpiryWarnDays:       input.ExpiryWarnDays,
	}
	s.createdProxies = append(s.createdProxies, input)
	s.proxyState[proxy.ID] = proxy
	s.proxyIDByName[proxy.Name] = proxy.ID
	return &proxy, nil
}

func (s *dataImportAdminService) UpdateProxy(_ context.Context, id int64, input *service.UpdateProxyInput) (*service.Proxy, error) {
	copied := *input
	s.updatedProxyIDs = append(s.updatedProxyIDs, id)
	s.updatedProxies = append(s.updatedProxies, &copied)

	proxy := s.proxyState[id]
	if input.Status != "" {
		proxy.Status = input.Status
	}
	if input.Platform != nil {
		proxy.Platform = *input.Platform
	}
	if input.RequiredAccountLevel != nil {
		proxy.RequiredAccountLevel = *input.RequiredAccountLevel
	}
	if input.MaxAccounts != nil {
		proxy.MaxAccounts = *input.MaxAccounts
	}
	if input.ExpiresAtProvided {
		proxy.ExpiresAt = input.ExpiresAt
	}
	if input.FallbackMode != nil {
		proxy.FallbackMode = *input.FallbackMode
	}
	if input.BackupProxyIDProvided {
		proxy.BackupProxyID = input.BackupProxyID
	}
	if input.ExpiryWarnDays != nil {
		proxy.ExpiryWarnDays = *input.ExpiryWarnDays
	}
	s.proxyState[id] = proxy
	return &proxy, nil
}

func TestDataImportHandlersResolveForwardBackupReference(t *testing.T) {
	for _, test := range []struct {
		name  string
		route string
	}{
		{name: "accounts data", route: "/api/v1/admin/accounts/data"},
		{name: "proxies data", route: "/api/v1/admin/proxies/data"},
	} {
		t.Run(test.name, func(t *testing.T) {
			adminSvc := newDataImportAdminService(nil)
			router := setupDataImportRouter(test.route, adminSvc)
			payload := dataImportPayload([]map[string]any{
				{
					"name":              "primary",
					"protocol":          "http",
					"host":              "primary.example",
					"port":              8080,
					"fallback_mode":     service.FallbackModeProxy,
					"backup_proxy_name": "backup",
					"expiry_warn_days":  0,
				},
				{
					"name":     "backup",
					"protocol": "http",
					"host":     "backup.example",
					"port":     8081,
				},
			})

			result := postDataImport(t, router, test.route, payload)

			require.Equal(t, 2, result.ProxyCreated)
			require.Empty(t, result.Errors)
			primaryID := adminSvc.proxyIDByName["primary"]
			backupID := adminSvc.proxyIDByName["backup"]
			require.NotZero(t, primaryID)
			require.NotZero(t, backupID)

			input := findDataProxyUpdate(t, adminSvc, primaryID)
			require.NotNil(t, input.FallbackMode)
			require.Equal(t, service.FallbackModeProxy, *input.FallbackMode)
			require.True(t, input.BackupProxyIDProvided)
			require.NotNil(t, input.BackupProxyID)
			require.Equal(t, backupID, *input.BackupProxyID)
			require.NotNil(t, input.ExpiryWarnDays)
			require.Zero(t, *input.ExpiryWarnDays, "explicit zero must not be treated as omitted")
		})
	}
}

func TestProxyDataImportRejectsAmbiguousBackupAndPreservesOmittedLocalScope(t *testing.T) {
	existing := []service.Proxy{
		{ID: 1, Name: "primary", Protocol: "http", Host: "primary.example", Port: 8080, Status: service.StatusActive, Platform: service.PlatformOpenAI, RequiredAccountLevel: "plus"},
		{ID: 2, Name: "duplicate", Protocol: "http", Host: "backup-one.example", Port: 8081, Status: service.StatusActive},
		{ID: 3, Name: "duplicate", Protocol: "http", Host: "backup-two.example", Port: 8082, Status: service.StatusActive},
	}
	adminSvc := newDataImportAdminService(existing)
	route := "/api/v1/admin/proxies/data"
	router := setupDataImportRouter(route, adminSvc)
	payload := dataImportPayload([]map[string]any{
		{
			"name":              "primary",
			"protocol":          "http",
			"host":              "primary.example",
			"port":              8080,
			"fallback_mode":     service.FallbackModeProxy,
			"backup_proxy_name": "duplicate",
		},
	})

	result := postDataImport(t, router, route, payload)

	require.Equal(t, 1, result.ProxyReused)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "ambiguous")
	input := findDataProxyUpdate(t, adminSvc, 1)
	require.NotNil(t, input.FallbackMode)
	require.Equal(t, service.FallbackModeNone, *input.FallbackMode)
	require.True(t, input.BackupProxyIDProvided)
	require.Nil(t, input.BackupProxyID)
	require.Nil(t, input.Platform, "omitted platform must preserve the local scope")
	require.Nil(t, input.RequiredAccountLevel, "omitted required_account_level must preserve the local scope")
	require.Equal(t, service.PlatformOpenAI, adminSvc.proxyState[1].Platform)
	require.Equal(t, "plus", adminSvc.proxyState[1].RequiredAccountLevel)
}

func TestProxyDataImportLeavesOmittedLifecycleUntouched(t *testing.T) {
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	existing := []service.Proxy{{
		ID:             1,
		Name:           "primary",
		Protocol:       "http",
		Host:           "primary.example",
		Port:           8080,
		Status:         service.StatusActive,
		ExpiresAt:      &expiresAt,
		FallbackMode:   service.FallbackModeDirect,
		ExpiryWarnDays: 7,
	}}
	adminSvc := newDataImportAdminService(existing)
	route := "/api/v1/admin/proxies/data"
	router := setupDataImportRouter(route, adminSvc)
	payload := dataImportPayload([]map[string]any{
		{
			"name":     "primary",
			"protocol": "http",
			"host":     "primary.example",
			"port":     8080,
		},
	})

	result := postDataImport(t, router, route, payload)

	require.Equal(t, 1, result.ProxyReused)
	require.Empty(t, result.Errors)
	require.Empty(t, adminSvc.updatedProxies, "an upstream payload with omitted optional fields must not overwrite familiar local behavior")
}

func setupDataImportRouter(route string, adminSvc service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if route == "/api/v1/admin/accounts/data" {
		handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		router.POST(route, handler.ImportData)
		return router
	}
	handler := NewProxyHandler(adminSvc)
	router.POST(route, handler.ImportData)
	return router
}

func dataImportPayload(proxies []map[string]any) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"type":     dataType,
			"version":  dataVersion,
			"proxies":  proxies,
			"accounts": []map[string]any{},
		},
	}
}

func postDataImport(t *testing.T, router *gin.Engine, route string, payload map[string]any) DataImportResult {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, route, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response struct {
		Code int              `json:"code"`
		Data DataImportResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Zero(t, response.Code)
	return response.Data
}

func findDataProxyUpdate(t *testing.T, adminSvc *dataImportAdminService, proxyID int64) *service.UpdateProxyInput {
	t.Helper()
	for i := len(adminSvc.updatedProxyIDs) - 1; i >= 0; i-- {
		if adminSvc.updatedProxyIDs[i] == proxyID {
			return adminSvc.updatedProxies[i]
		}
	}
	require.FailNow(t, "proxy update not found", "proxy_id=%d", proxyID)
	return nil
}
