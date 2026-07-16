package service

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitOpenAIConcatenatedJSONDocumentsRequiresMultipleTypedEvents(t *testing.T) {
	payload := []byte(`{"type":"response.created","response":{"id":"r1"}} {"type":"response.completed","response":{"id":"r1"}}`)
	documents, repaired := splitOpenAIConcatenatedJSONDocuments(payload)
	require.True(t, repaired)
	require.Len(t, documents, 2)
	require.JSONEq(t, `{"type":"response.created","response":{"id":"r1"}}`, string(documents[0]))
	require.JSONEq(t, `{"type":"response.completed","response":{"id":"r1"}}`, string(documents[1]))

	for _, malformed := range [][]byte{
		[]byte(`{"type":"response.created"}`),
		[]byte(`{"type":"response.created"}{"response":{"id":"r1"}}`),
		[]byte(`{"type":"response.created"}{`),
	} {
		_, repaired = splitOpenAIConcatenatedJSONDocuments(malformed)
		require.False(t, repaired)
	}
}

func TestOpenAISSEJSONDocumentScannerExpandsOnlyAtCompleteEventBoundaries(t *testing.T) {
	input := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"r1"}}{"type":"response.completed","response":{"id":"r1"}}`,
		"",
	}, "\n")
	documentScanner := newOpenAISSEJSONDocumentScanner(bufio.NewScanner(strings.NewReader(input)))

	var lines []string
	for documentScanner.Scan() {
		lines = append(lines, documentScanner.Text())
	}
	require.NoError(t, documentScanner.Err())
	require.Equal(t, []string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"r1"}}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"r1"}}`,
		"",
	}, lines)
}
