package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
)

var errOpenAIRequestBodyReplayReleased = errors.New("openai upstream request body replay has been released")

// openAIRequestBodyReplay owns the source used by net/http redirects and
// transport retries until Client.Do returns. Readers hold independent views so
// an early response cannot invalidate an upload still running in the transport.
type openAIRequestBodyReplay struct {
	mu       sync.Mutex
	body     []byte
	released bool
}

func (r *openAIRequestBodyReplay) newReader() (io.ReadCloser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.released {
		return nil, errOpenAIRequestBodyReplayReleased
	}
	return &openAIRequestBodyReader{replay: r, reader: bytes.NewReader(r.body)}, nil
}

func (r *openAIRequestBodyReplay) release() {
	r.mu.Lock()
	r.body = nil
	r.released = true
	r.mu.Unlock()
}

// net/http may retain its internal request throughout a long response. An
// exhausted or closed reader must therefore release its payload even when the
// Request itself remains reachable. Close may run concurrently with Read.
type openAIRequestBodyReader struct {
	mu     sync.Mutex
	reader *bytes.Reader
	replay *openAIRequestBodyReplay
}

func (r *openAIRequestBodyReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reader == nil {
		return 0, io.EOF
	}
	n, err := r.reader.Read(p)
	if r.reader.Len() == 0 {
		r.reader = nil
	}
	return n, err
}

func (r *openAIRequestBodyReader) Close() error {
	r.mu.Lock()
	r.reader = nil
	r.mu.Unlock()
	return nil
}

func newOpenAIUpstreamRequestWithBody(ctx context.Context, method, targetURL string, body []byte) (*http.Request, error) {
	if len(body) == 0 {
		return http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(nil))
	}
	replay := &openAIRequestBodyReplay{body: body}
	reader, err := replay.newReader()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, reader)
	if err != nil {
		_ = reader.Close()
		replay.release()
		return nil, err
	}
	req.ContentLength = int64(len(body))
	req.GetBody = replay.newReader
	return req, nil
}

// releaseOpenAIRequestBodyReplay runs after Do returns, including errors. It
// never mutates the shared http.Request or any payload bytes. Already-created
// transport readers keep their data until they reach EOF or Close.
func releaseOpenAIRequestBodyReplay(req *http.Request) {
	if req == nil {
		return
	}
	if reader, ok := req.Body.(*openAIRequestBodyReader); ok {
		reader.replay.release()
	}
}
