//go:build unit

package service

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type grokSearchChunkReader struct {
	chunks [][]byte
	index  int
	closed bool
}

func (r *grokSearchChunkReader) Read(p []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[r.index])
	if n == len(r.chunks[r.index]) {
		r.index++
	} else {
		r.chunks[r.index] = r.chunks[r.index][n:]
	}
	return n, nil
}

func (r *grokSearchChunkReader) Close() error {
	r.closed = true
	return nil
}

func TestGrokSearchCountingReadCloserPreservesBytesAndCountsAcrossChunks(t *testing.T) {
	t.Parallel()
	want := []byte("event: response.output_item.done\r\n" +
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"web_search_call\",\"call_id\":\"c1\"}}\r\n\r\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"web_search_call\",\"call_id\":\"c1\"},{\"type\":\"x_search_call\",\"call_id\":\"c2\"}]}}\n\n")
	source := &grokSearchChunkReader{chunks: [][]byte{
		want[:17], want[17:63], want[63:129], want[129:],
	}}
	counter := newGrokSearchCountingReadCloser(source)
	got, err := io.ReadAll(counter)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, 2, counter.Count())
	require.NoError(t, counter.Close())
	require.NoError(t, counter.Close())
	require.True(t, source.closed)
}

func TestGrokSearchCountingReadCloserFlushesFinalFrameAtEOF(t *testing.T) {
	t.Parallel()
	source := io.NopCloser(bytes.NewBufferString(`data: {"type":"response.output_item.done","item":{"type":"tool_search_call","id":"s1"}}`))
	counter := newGrokSearchCountingReadCloser(source)
	_, err := io.ReadAll(counter)
	require.NoError(t, err)
	require.Equal(t, 1, counter.Count())
}
