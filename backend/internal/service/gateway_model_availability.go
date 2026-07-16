package service

import (
	"context"
	"strings"
)

// ModelAvailabilityDiagnosis describes whether a configured account in the
// pool can serve the requested model, ignoring transient availability state.
type ModelAvailabilityDiagnosis struct {
	HasAccountsInPool bool
	HasModelSupport   bool
}

// ModelAvailabilityDiagnoser is implemented by gateway services that can
// diagnose model support for a no-account selection result.
type ModelAvailabilityDiagnoser interface {
	DiagnoseModelAvailabilityForPlatform(
		ctx context.Context,
		groupID *int64,
		requestedModel string,
		platform string,
	) ModelAvailabilityDiagnosis
}

func modelAvailabilityConservativeFallback() ModelAvailabilityDiagnosis {
	return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
}

// DiagnoseModelAvailabilityForPlatform inspects configured accounts without
// considering transient rate limits, quotas, runtime blocks, or slot pressure.
func (s *GatewayService) DiagnoseModelAvailabilityForPlatform(
	ctx context.Context,
	groupID *int64,
	requestedModel string,
	platform string,
) ModelAvailabilityDiagnosis {
	if s == nil || s.accountRepo == nil {
		return modelAvailabilityConservativeFallback()
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" || strings.TrimSpace(platform) == "" {
		return modelAvailabilityConservativeFallback()
	}

	accounts, _, err := s.listSchedulableAccounts(ctx, groupID, platform, false)
	if err != nil {
		return modelAvailabilityConservativeFallback()
	}

	diag := ModelAvailabilityDiagnosis{}
	for i := range accounts {
		diag.HasAccountsInPool = true
		if s.isModelSupportedByAccountWithContext(ctx, &accounts[i], requestedModel) {
			diag.HasModelSupport = true
			return diag
		}
	}
	return diag
}

// DiagnoseModelAvailabilityForPlatform reports model support for the current
// OpenAI-compatible local implementation. This project has not pulled in the
// upstream Grok platform split, so the platform parameter is intentionally
// conservative here and the local OpenAI account pool remains the source.
func (s *OpenAIGatewayService) DiagnoseModelAvailabilityForPlatform(
	ctx context.Context,
	groupID *int64,
	requestedModel string,
	platform string,
) ModelAvailabilityDiagnosis {
	if s == nil || s.accountRepo == nil {
		return modelAvailabilityConservativeFallback()
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" || strings.TrimSpace(platform) == "" {
		return modelAvailabilityConservativeFallback()
	}

	accounts, err := s.listSchedulableAccounts(ctx, groupID)
	if err != nil {
		return modelAvailabilityConservativeFallback()
	}

	diag := ModelAvailabilityDiagnosis{}
	for i := range accounts {
		diag.HasAccountsInPool = true
		if accounts[i].IsModelSupported(requestedModel) {
			diag.HasModelSupport = true
			return diag
		}
	}
	return diag
}
