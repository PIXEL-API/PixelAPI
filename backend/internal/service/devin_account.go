package service

import (
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// validateDevinAccountConfiguration 校验 Devin 账号形状：一期仅支持 apikey
// （session token）账号，拒绝 OAuth/上游透传等其他类型。
func validateDevinAccountConfiguration(platform, accountType string, credentials map[string]any) error {
	if platform != PlatformDevin {
		return nil
	}
	if accountType != AccountTypeAPIKey {
		return infraerrors.BadRequest("INVALID_DEVIN_ACCOUNT_TYPE", "Devin requires an API key account")
	}
	if !hasNonEmptyStringField(credentials, "api_key") {
		return infraerrors.BadRequest("INVALID_DEVIN_CREDENTIALS", "Devin requires credentials.api_key (session token)")
	}
	return nil
}
