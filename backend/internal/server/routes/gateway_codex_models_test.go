package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexModelsRouteAccountRepo struct {
	service.AccountRepository
}

func (codexModelsRouteAccountRepo) ListSchedulableByGroupID(context.Context, int64) ([]service.Account, error) {
	return nil, nil
}

func (codexModelsRouteAccountRepo) ListSchedulableByPlatform(context.Context, string) ([]service.Account, error) {
	return nil, nil
}

func TestGatewayRoutesCodexModelsManifestPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	registered := make(map[string]string)
	for _, route := range router.Routes() {
		if route.Method == http.MethodGet {
			registered[route.Path] = route.Handler
		}
	}

	require.NotEmpty(t, registered["/backend-api/codex/models"])
	require.NotEmpty(t, registered["/v1/models"])
	require.NotEmpty(t, registered["/models"])
	require.Equal(t, registered["/v1/models"], registered["/models"], "root alias must reuse the platform-aware models handler")
}

func TestGatewayRoutesCodexModelsManifestRuntimeGate(t *testing.T) {
	tests := []struct {
		name          string
		platform      string
		clientVersion string
		wantManifest  bool
	}{
		{name: "OpenAI Codex client", platform: service.PlatformOpenAI, clientVersion: "0.144.0", wantManifest: true},
		{name: "OpenAI client without version", platform: service.PlatformOpenAI},
		{name: "non-OpenAI client with version", platform: service.PlatformGrok, clientVersion: "0.144.0"},
	}

	for _, path := range []string{"/v1/models", "/models"} {
		for _, test := range tests {
			t.Run(path+"/"+test.name, func(t *testing.T) {
				router := newCodexModelsRuntimeGateRouter(test.platform)
				request := httptest.NewRequest(http.MethodGet, path, nil)
				if test.clientVersion != "" {
					query := request.URL.Query()
					query.Set("client_version", test.clientVersion)
					request.URL.RawQuery = query.Encode()
				}
				recorder := httptest.NewRecorder()

				router.ServeHTTP(recorder, request)

				if test.wantManifest {
					require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
					require.Contains(t, recorder.Body.String(), "No available OpenAI accounts")
					return
				}
				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				var response map[string]any
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
				require.Equal(t, "list", response["object"])
				_, hasData := response["data"].([]any)
				require.True(t, hasData, "ordinary models response must contain a data array")
			})
		}
	}
}

func newCodexModelsRuntimeGateRouter(platform string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	repo := codexModelsRouteAccountRepo{}
	settingService := service.NewSettingService(&gatewayRouteSettingRepo{values: map[string]string{}}, cfg)

	gatewayService := service.NewGatewayService(
		repo,
		nil,
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
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		settingService,
		nil,
		nil,
		nil,
		nil,
	)
	openAIGatewayService := service.NewOpenAIGatewayService(
		repo,
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
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	handlers := &handler.Handlers{
		Gateway: handler.NewGatewayHandler(
			gatewayService,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			cfg,
			settingService,
		),
		OpenAIGateway: handler.NewOpenAIGatewayHandler(
			openAIGatewayService,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			cfg,
		),
	}

	router := gin.New()
	RegisterGatewayRoutes(
		router,
		handlers,
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupID := int64(1)
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				GroupID: &groupID,
				Group:   &service.Group{ID: groupID, Platform: platform},
			})
			c.Next()
		}),
		nil,
		nil,
		nil,
		settingService,
		cfg,
	)
	return router
}
