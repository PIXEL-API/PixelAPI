package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// CodexModels serves the schema-agnostic Codex models manifest. Codex clients
// call either /models or /backend-api/codex/models depending on provider mode.
func (h *OpenAIGatewayHandler) CodexModels(c *gin.Context) {
	if c.Request.Context().Err() != nil {
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "invalid_request_error", "API key group is required")
		return
	}
	if apiKey.Group.Platform != service.PlatformOpenAI {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex models manifest is only available for OpenAI groups")
		return
	}
	roomModels, err := h.gatewayService.GetAccountShareModels(c.Request.Context(), apiKey)
	if err != nil {
		if c.Request.Context().Err() != nil {
			return
		}
		if h.handleAccountShareModeSelectionError(c, err, false) {
			return
		}
		logger.LegacyPrintf("handler.openai_codex_models", "resolve account share models failed: %v", err)
		h.errorResponse(c, http.StatusServiceUnavailable, "upstream_error", "Unable to load account share models")
		return
	}
	if roomModels != nil {
		c.Header("Cache-Control", "private, no-cache")
		// The client ETag belongs to the filtered response, not the upstream
		// manifest. Fetch a body before applying the current room permissions.
		manifest := &service.CodexModelsManifest{Body: []byte(`{"models":[]}`)}
		if len(roomModels.Models) > 0 {
			manifest, err = h.gatewayService.FetchCodexModelsManifest(c.Request.Context(), roomModels.Account, c.Query("client_version"), "")
		}
		if c.Request.Context().Err() != nil {
			return
		}
		if err != nil {
			h.errorResponse(c, infraerrors.Code(err), "upstream_error", infraerrors.Message(err))
			return
		}
		manifest, err = service.FilterAccountShareCodexModelsManifest(manifest, roomModels.Models, c.GetHeader("If-None-Match"))
		if c.Request.Context().Err() != nil {
			return
		}
		if err != nil {
			logger.LegacyPrintf("handler.openai_codex_models", "filter account share models manifest failed: %v", err)
			h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Unable to load account share Codex models")
			return
		}
		writeCodexModelsManifest(c, manifest)
		return
	}

	maxAccountSwitches := h.maxAccountSwitches
	if maxAccountSwitches <= 0 {
		maxAccountSwitches = 3
	}
	failedAccountIDs := make(map[int64]struct{})
	switchCount := 0
	var lastUpstreamErr error
	selectionCtx := openAIAccountShareModeRequestContext(c, apiKey)

	for {
		account, err := h.gatewayService.SelectAccountForModelWithExclusions(selectionCtx, apiKey.GroupID, "", "", failedAccountIDs)
		if err != nil {
			if c.Request.Context().Err() != nil {
				return
			}
			if lastUpstreamErr != nil {
				h.errorResponse(c, infraerrors.Code(lastUpstreamErr), "upstream_error", infraerrors.Message(lastUpstreamErr))
				return
			}
			h.errorResponse(c, http.StatusServiceUnavailable, "upstream_error", "No available OpenAI accounts")
			return
		}

		manifest, err := h.gatewayService.FetchCodexModelsManifest(selectionCtx, account, c.Query("client_version"), c.GetHeader("If-None-Match"))
		if err != nil {
			if c.Request.Context().Err() != nil {
				return
			}
			if service.IsRetryableCodexModelsManifestError(err) && switchCount < maxAccountSwitches {
				failedAccountIDs[account.ID] = struct{}{}
				switchCount++
				lastUpstreamErr = err
				continue
			}
			h.errorResponse(c, infraerrors.Code(err), "upstream_error", infraerrors.Message(err))
			return
		}
		if c.Request.Context().Err() != nil {
			return
		}

		writeCodexModelsManifest(c, manifest)
		return
	}
}

func writeCodexModelsManifest(c *gin.Context, manifest *service.CodexModelsManifest) {
	if manifest.ETag != "" {
		c.Header("ETag", manifest.ETag)
	}
	if manifest.NotModified {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, "application/json", manifest.Body)
}
