package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

func grokBaseURLValidator(
	ctx context.Context,
	account *Account,
	cfg *config.Config,
	settingService *SettingService,
) (xai.BaseURLValidator, error) {
	if account == nil || !account.IsGrok() {
		return nil, fmt.Errorf("grok account is required")
	}
	switch account.Type {
	case AccountTypeOAuth:
		return redactedGrokBaseURLValidator(xai.ValidateTrustedBaseURL), nil
	case AccountTypeAPIKey:
		if cfg == nil {
			return nil, fmt.Errorf("grok API key URL security configuration is required")
		}
		if !cfg.Security.URLAllowlist.Enabled {
			return redactedGrokBaseURLValidator(func(raw string) (string, error) {
				return urlvalidator.ValidateURLFormat(raw, cfg.Security.URLAllowlist.AllowInsecureHTTP)
			}), nil
		}
		if ctx == nil {
			ctx = context.Background()
		}
		allowedHosts, err := upstreamAllowlistHosts(ctx, cfg, settingService)
		if err != nil {
			return nil, fmt.Errorf("load grok upstream URL allowlist: %w", err)
		}
		return redactedGrokBaseURLValidator(func(raw string) (string, error) {
			return urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
				AllowedHosts:     allowedHosts,
				RequireAllowlist: true,
				AllowPrivate:     cfg.Security.URLAllowlist.AllowPrivateHosts,
			})
		}), nil
	default:
		return nil, fmt.Errorf("unsupported grok account type: %s", account.Type)
	}
}

func redactedGrokBaseURLValidator(validator xai.BaseURLValidator) xai.BaseURLValidator {
	return func(raw string) (string, error) {
		validated, err := validator(raw)
		if err != nil {
			return "", errors.New("base URL rejected by URL security policy")
		}
		return validated, nil
	}
}

func buildGrokResponsesURL(ctx context.Context, account *Account, cfg *config.Config, settingService *SettingService) (string, error) {
	validator, err := grokBaseURLValidator(ctx, account, cfg, settingService)
	if err != nil {
		return "", err
	}
	return xai.BuildResponsesURLWithValidator(account.GetGrokBaseURL(), validator)
}

func buildGrokChatCompletionsURL(ctx context.Context, account *Account, cfg *config.Config, settingService *SettingService) (string, error) {
	validator, err := grokBaseURLValidator(ctx, account, cfg, settingService)
	if err != nil {
		return "", err
	}
	return xai.BuildChatCompletionsURLWithValidator(account.GetGrokBaseURL(), validator)
}

func buildGrokMediaURL(
	ctx context.Context,
	account *Account,
	cfg *config.Config,
	settingService *SettingService,
	endpoint GrokMediaEndpoint,
	requestID string,
) (string, error) {
	validator, err := grokBaseURLValidator(ctx, account, cfg, settingService)
	if err != nil {
		return "", err
	}
	baseURL := account.GetGrokMediaBaseURL()
	switch endpoint {
	case GrokMediaEndpointImagesGenerations:
		return xai.BuildImagesGenerationsURLWithValidator(baseURL, validator)
	case GrokMediaEndpointImagesEdits:
		return xai.BuildImagesEditsURLWithValidator(baseURL, validator)
	case GrokMediaEndpointVideosGenerations:
		return xai.BuildVideosGenerationsURLWithValidator(baseURL, validator)
	case GrokMediaEndpointVideosEdits:
		return xai.BuildVideosEditsURLWithValidator(baseURL, validator)
	case GrokMediaEndpointVideosExtensions:
		return xai.BuildVideosExtensionsURLWithValidator(baseURL, validator)
	case GrokMediaEndpointVideoStatus:
		return xai.BuildVideoURLWithValidator(baseURL, requestID, validator)
	case GrokMediaEndpointVideoContent:
		videoURL, err := xai.BuildVideoURLWithValidator(baseURL, requestID, validator)
		if err != nil {
			return "", err
		}
		return videoURL + "/content", nil
	default:
		return "", fmt.Errorf("unsupported grok media endpoint: %s", endpoint)
	}
}
