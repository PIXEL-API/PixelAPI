package repository

import (
	"strings"
	"testing"
	"time"
)

func TestBuildOAuthRefreshCandidatesQueryFiltersHealthyAccountsInSQL(t *testing.T) {
	query, args := buildOAuthRefreshCandidatesQuery(2 * time.Hour)
	normalized := strings.Join(strings.Fields(query), " ")

	if len(args) != 1 || args[0] != int64(7200) {
		t.Fatalf("args = %#v, want single refresh window seconds arg", args)
	}
	required := []string{
		"WITH candidates AS",
		"NULLIF(btrim(credentials->>'expires_at'), '') AS expires_at_raw",
		"type IN ('oauth', 'setup-token')",
		"platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok')",
		"WHEN expires_at_raw ~ '^[0-9]+$' THEN to_timestamp(expires_at_raw::double precision)",
		"needs_go_time_parse",
		"platform = 'openai'",
		"rate_limit_reset_at > NOW()",
		"platform IN ('anthropic', 'gemini')",
		"INTERVAL '15 minutes'",
		"platform = 'grok'",
		"GREATEST($1::bigint, 3600) * INTERVAL '1 second'",
		"credential_expires_at <= NOW() + ($1::bigint * INTERVAL '1 second')",
	}
	for _, fragment := range required {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("query missing fragment %q\n%s", fragment, normalized)
		}
	}
}

func TestBuildOAuthRefreshCandidatesQueryClampsNegativeWindow(t *testing.T) {
	_, args := buildOAuthRefreshCandidatesQuery(-time.Hour)
	if len(args) != 1 || args[0] != int64(0) {
		t.Fatalf("args = %#v, want clamped zero refresh window", args)
	}
}
