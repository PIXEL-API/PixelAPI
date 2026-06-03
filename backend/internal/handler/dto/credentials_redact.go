package dto

import "github.com/Wei-Shaw/sub2api/internal/service"

// RedactCredentials returns a copy of credentials without sensitive keys plus
// has_<key> status flags so the UI can tell configured secrets from missing ones.
func RedactCredentials(in map[string]any) (map[string]any, map[string]bool) {
	if in == nil {
		return nil, nil
	}
	out := make(map[string]any, len(in))
	var status map[string]bool
	for key, value := range in {
		if service.IsSensitiveCredentialKey(key) {
			if isCredentialValuePresent(value) {
				if status == nil {
					status = make(map[string]bool, 4)
				}
				status["has_"+key] = true
			}
			continue
		}
		out[key] = value
	}
	return out, status
}

func isCredentialValuePresent(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return v != ""
	case bool:
		return v
	default:
		return true
	}
}
