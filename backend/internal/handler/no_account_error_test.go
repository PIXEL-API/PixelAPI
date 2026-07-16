//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type noAccountFakeDiagnoser struct {
	calls []noAccountFakeDiagnoseCall
	resp  service.ModelAvailabilityDiagnosis
}

type noAccountFakeDiagnoseCall struct {
	GroupID  *int64
	Model    string
	Platform string
}

func (f *noAccountFakeDiagnoser) DiagnoseModelAvailabilityForPlatform(
	_ context.Context,
	groupID *int64,
	model string,
	platform string,
) service.ModelAvailabilityDiagnosis {
	f.calls = append(f.calls, noAccountFakeDiagnoseCall{
		GroupID:  groupID,
		Model:    model,
		Platform: platform,
	})
	return f.resp
}

func ptrInt64(v int64) *int64 { return &v }

func newNoAccountTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
	return c
}

func TestClassifyNoAccountError_ModelNotSupported_Returns404(t *testing.T) {
	c := newNoAccountTestContext()
	fd := &noAccountFakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(42)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5.1-codex-mini", "gpt-5.1-codex-mini", service.PlatformOpenAI)

	require.Equal(t, http.StatusNotFound, cls.Status)
	require.Equal(t, "model_not_found", cls.ErrType)
	require.True(t, cls.ModelNotFound)
	require.Contains(t, cls.Message, "gpt-5.1-codex-mini")
	require.Len(t, fd.calls, 1)
	require.Equal(t, "gpt-5.1-codex-mini", fd.calls[0].Model)
	require.Equal(t, service.PlatformOpenAI, fd.calls[0].Platform)
	require.NotNil(t, fd.calls[0].GroupID)
	require.Equal(t, int64(42), *fd.calls[0].GroupID)
}

func TestClassifyNoAccountError_NoAccountsInPool_Stays503(t *testing.T) {
	c := newNoAccountTestContext()
	fd := &noAccountFakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: false, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.False(t, cls.ModelNotFound)
}

func TestClassifyNoAccountError_HasModelSupport_Stays503(t *testing.T) {
	c := newNoAccountTestContext()
	fd := &noAccountFakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Equal(t, "api_error", cls.ErrType)
	require.False(t, cls.ModelNotFound)
}

func TestClassifyNoAccountError_MissingInputs_Stays503(t *testing.T) {
	c := newNoAccountTestContext()
	fd := &noAccountFakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}

	tests := []struct {
		name   string
		apiKey *service.APIKey
		model  string
	}{
		{name: "nil api key", apiKey: nil, model: "gpt-5"},
		{name: "nil group", apiKey: &service.APIKey{}, model: "gpt-5"},
		{name: "empty model", apiKey: &service.APIKey{GroupID: ptrInt64(7)}, model: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fd.calls = nil

			cls := classifyNoAccountErrorFromGin(c, fd, tt.apiKey, tt.model, tt.model, service.PlatformOpenAI)

			require.Equal(t, http.StatusServiceUnavailable, cls.Status)
			require.False(t, cls.ModelNotFound)
			require.Empty(t, fd.calls)
		})
	}
}
