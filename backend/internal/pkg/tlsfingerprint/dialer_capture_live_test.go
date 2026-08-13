//go:build integration && tlslive

package tlsfingerprint

import "testing"

// TestDialerAgainstCaptureServer is an explicit live smoke test. Connection,
// protocol, and fingerprint mismatches are real failures; this test must never
// skip when the configured capture service is unavailable.
func TestDialerAgainstCaptureServer(t *testing.T) {
	runDialerAgainstCaptureServer(t)
}
