package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Imported account settings take precedence over form defaults. A form URL
// belongs to the form's mode/protocol and must not leak into a different one.
func cnCredentialImportCredentials(source map[string]any, defaults importUserAccountCredentialsRequest) map[string]any {
	credentials := make(map[string]any, len(source)+6)
	for key, value := range source {
		credentials[key] = value
	}
	mode := strings.ToLower(strings.TrimSpace(defaults.AccountMode))
	if mode == "" {
		mode = service.AccountModePayG
	}
	protocol := strings.ToLower(strings.TrimSpace(defaults.APIProtocol))
	if protocol == "" {
		protocol = service.APIProtocolChatCompletions
	}
	if _, exists := credentials["account_mode"]; !exists {
		credentials["account_mode"] = mode
	}
	if _, exists := credentials["api_protocol"]; !exists {
		credentials["api_protocol"] = protocol
	}
	accountMode, _ := credentials["account_mode"].(string)
	accountProtocol, _ := credentials["api_protocol"].(string)
	if accountMode == mode && accountProtocol == protocol {
		if _, exists := credentials["base_url"]; !exists && strings.TrimSpace(defaults.BaseURL) != "" {
			credentials["base_url"] = strings.TrimSpace(defaults.BaseURL)
		}
		if _, exists := credentials["api_base_urls"]; !exists && len(defaults.APIBaseURLs) > 0 {
			urls := map[string]any{}
			for key, value := range defaults.APIBaseURLs {
				if value = strings.TrimSpace(value); value != "" {
					urls[key] = value
				}
			}
			if len(urls) > 0 {
				credentials["api_base_urls"] = urls
			}
		}
	}
	for key, value := range map[string]string{"zhipu_organization": defaults.ZhipuOrganization, "zhipu_project": defaults.ZhipuProject} {
		if _, exists := credentials[key]; !exists && strings.TrimSpace(value) != "" {
			credentials[key] = strings.TrimSpace(value)
		}
	}
	return credentials
}
