package xai

import (
	"os"
	"strings"

	"golang.org/x/mod/semver"
)

const (
	// CLIStableVersion is the oldest supported operator override. The default
	// identity remains CLIClientVersion, which is the repository-wide pin.
	CLIStableVersion = "0.2.93"
	CLIVersionEnv    = "XAI_GROK_CLI_VERSION"

	CLIProxyHost        = "cli-chat-proxy.grok.com"
	CLIClientIdentifier = "grok-shell"
)

func ResolveCLIVersion() string {
	version := strings.TrimSpace(os.Getenv(CLIVersionEnv))
	if !IsSupportedCLIVersion(version) {
		return CLIClientVersion
	}
	return version
}

func IsSupportedCLIVersion(version string) bool {
	canonical := "v" + version
	minimum := "v" + CLIStableVersion
	return semver.IsValid(canonical) &&
		semver.Canonical(canonical) == canonical &&
		semver.Compare(canonical, minimum) >= 0
}

func CLIUserAgentForVersion(version string) string {
	if strings.TrimSpace(version) == "" {
		version = CLIClientVersion
	}
	return "xai-grok-workspace/" + version
}
