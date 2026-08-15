//go:build unit

package service

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func readFilteredGrokSSE(t *testing.T, input string, account *Account, maxLineSize int) string {
	t.Helper()
	body := newGrokResponsesBillingPingFilterBody(io.NopCloser(strings.NewReader(input)), account, maxLineSize)
	output, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	return string(output)
}

func TestGrokResponsesBillingPingFilterConvertsPingAndPreservesTerminal(t *testing.T) {
	input := "event: ping\r\ndata: {\"type\":\"ping\",\"cost\":1}\r\n\r\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"total_tokens\":3}}}\n\n"

	output := readFilteredGrokSSE(t, input, &Account{Platform: PlatformGrok}, defaultMaxLineSize)

	require.NotContains(t, output, "event: ping")
	require.Contains(t, output, ": ping\n\n")
	require.Contains(t, output, "event: response.completed")
	require.Contains(t, output, `"total_tokens":3`)
}

func TestGrokResponsesBillingPingFilterPreservesConflictingEventType(t *testing.T) {
	input := "event: ping\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"

	output := readFilteredGrokSSE(t, input, &Account{Platform: PlatformGrok}, defaultMaxLineSize)

	require.Equal(t, input, output)
}

func TestGrokResponsesBillingPingFilterPreservesUnknownOrOversizedCandidates(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		input := "event: ping\nid: must-pass-through\ndata: {}\n\n"
		require.Equal(t, input, readFilteredGrokSSE(t, input, &Account{Platform: PlatformGrok}, defaultMaxLineSize))
	})

	t.Run("candidate cap", func(t *testing.T) {
		var input strings.Builder
		input.WriteString("event: ping\n")
		for range grokResponsesPingFrameMaxLines {
			input.WriteString(": comment\n")
		}
		input.WriteString("\n")
		want := input.String()
		require.Equal(t, want, readFilteredGrokSSE(t, want, &Account{Platform: PlatformGrok}, defaultMaxLineSize))
	})
}

func TestGrokResponsesBillingPingFilterPassesThroughNonGrok(t *testing.T) {
	input := "event: ping\ndata: {\"type\":\"ping\"}\n\n"
	require.Equal(t, input, readFilteredGrokSSE(t, input, &Account{Platform: PlatformOpenAI}, defaultMaxLineSize))
}

type grokPingFilterErrorReader struct {
	read bool
}

func (r *grokPingFilterErrorReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(p, "event: ping\n"), nil
	}
	return 0, errors.New("source failed")
}

func (r *grokPingFilterErrorReader) Close() error { return nil }

func TestGrokResponsesBillingPingFilterPropagatesSourceError(t *testing.T) {
	body := newGrokResponsesBillingPingFilterBody(&grokPingFilterErrorReader{}, &Account{Platform: PlatformGrok}, defaultMaxLineSize)
	_, err := io.ReadAll(body)
	require.ErrorContains(t, err, "source failed")
	require.NoError(t, body.Close())
}
