package xai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveCLIVersion(t *testing.T) {
	t.Setenv(CLIVersionEnv, "")
	require.Equal(t, CLIClientVersion, ResolveCLIVersion())
	require.Equal(t, "xai-grok-workspace/"+CLIClientVersion, CLIUserAgentForVersion(""))

	t.Setenv(CLIVersionEnv, "0.2.95-alpha.1")
	require.Equal(t, "0.2.95-alpha.1", ResolveCLIVersion())

	for _, invalid := range []string{"0.2.92", "0.2.93-beta.1", "0.3", "0.2.95\r\nX-Injected: true"} {
		t.Run(invalid, func(t *testing.T) {
			t.Setenv(CLIVersionEnv, invalid)
			require.Equal(t, CLIClientVersion, ResolveCLIVersion())
		})
	}
}
