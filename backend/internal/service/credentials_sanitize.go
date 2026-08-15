package service

// SanitizeStoredCredentials removes ephemeral authentication material that
// must not be persisted after it has been exchanged for durable credentials.
//
// The platform parameter documents the call-site boundary and leaves room for
// platform-specific rules. The current secrets are unsafe for every platform,
// so they are always removed, including when a bulk caller has no platform.
func SanitizeStoredCredentials(platform string, credentials map[string]any) map[string]any {
	if credentials == nil {
		return nil
	}
	_ = platform

	for _, key := range []string{
		"password",
		"sso_token",
		"sso",
		"sso-rw",
		"clearTextPassword",
		"cookie",
	} {
		delete(credentials, key)
	}
	return credentials
}
