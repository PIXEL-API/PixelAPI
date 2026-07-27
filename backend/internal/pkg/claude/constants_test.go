package claude

import (
	"slices"
	"testing"
)

func TestClaudeCodeFingerprintVersionsStayAligned(t *testing.T) {
	if got, want := DefaultHeaders["User-Agent"], "claude-cli/"+CLICurrentVersion+" (external, cli)"; got != want {
		t.Fatalf("User-Agent = %q, want %q", got, want)
	}
	if got, want := DefaultHeaders["X-Stainless-Package-Version"], "0.94.0"; got != want {
		t.Fatalf("X-Stainless-Package-Version = %q, want %q", got, want)
	}
}

func TestFullClaudeCodeMimicryBetasDoesNotRedactThinkingByDefault(t *testing.T) {
	betas := FullClaudeCodeMimicryBetas()
	if slices.Contains(betas, BetaRedactThinking) {
		t.Fatalf("default mimicry betas must not contain %q", BetaRedactThinking)
	}
	if !slices.Contains(betas, BetaContextManagement) {
		t.Fatalf("default mimicry betas must contain %q", BetaContextManagement)
	}
}
