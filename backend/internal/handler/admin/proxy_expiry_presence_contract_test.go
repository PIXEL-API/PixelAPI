package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func updateProxyWithLifecyclePayload(t *testing.T, payload map[string]any) *service.UpdateProxyInput {
	t.Helper()
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	router := gin.New()
	router.PUT("/api/v1/admin/proxies/:id", NewProxyHandler(adminSvc).Update)

	body, err := json.Marshal(payload)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/proxies/4", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(t, adminSvc.updatedProxies, 1)
	return adminSvc.updatedProxies[0]
}

func TestUpdateProxyLifecycleFieldsOmittedPreserveExistingValues(t *testing.T) {
	input := updateProxyWithLifecyclePayload(t, map[string]any{"name": "renamed"})
	value := reflect.ValueOf(input).Elem()

	require.False(t, requiredBoolField(t, value, "ExpiresAtProvided"))
	require.True(t, requiredField(t, value, "ExpiresAt").IsNil())
	require.True(t, requiredField(t, value, "FallbackMode").IsNil())
	require.False(t, requiredBoolField(t, value, "BackupProxyIDProvided"))
	require.True(t, requiredField(t, value, "BackupProxyID").IsNil())
	require.True(t, requiredField(t, value, "ExpiryWarnDays").IsNil())
}

func TestUpdateProxyLifecycleFieldsExplicitNullAndZeroRemainPresent(t *testing.T) {
	input := updateProxyWithLifecyclePayload(t, map[string]any{
		"expires_at":       nil,
		"fallback_mode":    "none",
		"backup_proxy_id":  nil,
		"expiry_warn_days": 0,
	})
	value := reflect.ValueOf(input).Elem()

	require.True(t, requiredBoolField(t, value, "ExpiresAtProvided"))
	require.True(t, requiredField(t, value, "ExpiresAt").IsNil(), "explicit null must clear expires_at")
	require.Equal(t, "none", requiredField(t, value, "FallbackMode").Elem().String())
	require.True(t, requiredBoolField(t, value, "BackupProxyIDProvided"))
	require.True(t, requiredField(t, value, "BackupProxyID").IsNil(), "explicit null must clear backup_proxy_id")
	require.Equal(t, int64(0), requiredField(t, value, "ExpiryWarnDays").Elem().Int(), "explicit zero must not be treated as omitted")
}

func requiredField(t *testing.T, value reflect.Value, name string) reflect.Value {
	t.Helper()
	field := value.FieldByName(name)
	require.True(t, field.IsValid(), "service update input must expose %s", name)
	return field
}

func requiredBoolField(t *testing.T, value reflect.Value, name string) bool {
	t.Helper()
	field := requiredField(t, value, name)
	require.Equal(t, reflect.Bool, field.Kind(), "%s must be a presence boolean", name)
	return field.Bool()
}
