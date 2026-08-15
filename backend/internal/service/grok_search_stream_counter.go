package service

import (
	"bytes"
	"io"
	"sync"
)

// grokSearchCountingReadCloser observes Responses SSE without changing the
// bytes consumed by the existing Responses-to-Chat bridge. It keeps the Grok
// entrypoint responsible for search accounting while avoiding a second copy of
// the full stream in memory.
type grokSearchCountingReadCloser struct {
	source io.ReadCloser

	mu        sync.Mutex
	pending   []byte
	frameData [][]byte
	seen      map[string]struct{}
	count     int
	closed    bool
}

func newGrokSearchCountingReadCloser(source io.ReadCloser) *grokSearchCountingReadCloser {
	return &grokSearchCountingReadCloser{
		source: source,
		seen:   make(map[string]struct{}),
	}
}

func (r *grokSearchCountingReadCloser) Read(p []byte) (int, error) {
	if r == nil || r.source == nil {
		return 0, io.EOF
	}
	n, err := r.source.Read(p)
	r.mu.Lock()
	if n > 0 {
		r.observeLocked(p[:n], false)
	}
	if err == io.EOF {
		r.observeLocked(nil, true)
	}
	r.mu.Unlock()
	return n, err
}

func (r *grokSearchCountingReadCloser) Close() error {
	if r == nil || r.source == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	return r.source.Close()
}

func (r *grokSearchCountingReadCloser) Count() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

func (r *grokSearchCountingReadCloser) observeLocked(chunk []byte, atEOF bool) {
	if len(chunk) > 0 {
		r.pending = append(r.pending, chunk...)
	}
	for len(r.pending) > 0 {
		advance, line, _ := scanSSELinesPreservingEndings(r.pending, atEOF)
		if advance == 0 {
			return
		}
		r.observeLineLocked(line)
		r.pending = r.pending[advance:]
	}
	if atEOF {
		r.flushFrameLocked()
		r.pending = nil
	}
}

func (r *grokSearchCountingReadCloser) observeLineLocked(rawLine []byte) {
	line := trimSSELineEnding(rawLine)
	if len(line) == 0 {
		r.flushFrameLocked()
		return
	}
	data, ok := extractOpenAISSEDataLine(string(line))
	if !ok {
		return
	}
	r.frameData = append(r.frameData, []byte(data))
}

func (r *grokSearchCountingReadCloser) flushFrameLocked() {
	if len(r.frameData) == 0 {
		return
	}
	payload := bytes.Join(r.frameData, []byte("\n"))
	r.count += countGrokNativeSearchCallsInSSEDataDedup(payload, r.seen)
	for index := range r.frameData {
		r.frameData[index] = nil
	}
	r.frameData = r.frameData[:0]
}
