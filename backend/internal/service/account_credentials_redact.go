package service

// SensitiveCredentialKeys lists Account.Credentials keys that must never be
// returned to frontend responses. DTO redaction and update merge logic share
// this list so new credential types stay consistent.
var SensitiveCredentialKeys = []string{
	// OAuth tokens
	"access_token",
	"refresh_token",
	"id_token",
	// API key and browser-session credentials
	"api_key",
	"session_key",
	"session_token",
	"claude_session_key",
	"cookie",
	"cookies",
	// Cloud credentials
	"aws_secret_access_key",
	"aws_session_token",
	"service_account_json",
	"service_account",
	"private_key",
}

var sensitiveCredentialKeySet = func() map[string]struct{} {
	keys := make(map[string]struct{}, len(SensitiveCredentialKeys))
	for _, key := range SensitiveCredentialKeys {
		keys[key] = struct{}{}
	}
	return keys
}()

func IsSensitiveCredentialKey(key string) bool {
	_, ok := sensitiveCredentialKeySet[key]
	return ok
}

// MergePreservingSensitiveCreds applies incoming over existing, but preserves
// existing sensitive values when the incoming payload omits them. This protects
// full-object edit flows after response DTOs stop returning raw secrets.
func MergePreservingSensitiveCreds(existing, incoming map[string]any) map[string]any {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	out := make(map[string]any, len(incoming)+len(SensitiveCredentialKeys))
	for key, value := range incoming {
		out[key] = value
	}
	for _, key := range SensitiveCredentialKeys {
		if _, ok := incoming[key]; ok {
			continue
		}
		if value, ok := existing[key]; ok {
			out[key] = value
		}
	}
	return out
}
