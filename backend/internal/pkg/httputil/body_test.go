package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestReadBodyUpToLimitRejectsOversizedInput(t *testing.T) {
	t.Parallel()

	_, err := readBodyUpToLimit(strings.NewReader("123456789"), 8)
	if err == nil {
		t.Fatal("expected oversized body error")
	}
	var maxErr *http.MaxBytesError
	if !errors.As(err, &maxErr) {
		t.Fatalf("error = %T %v, want *http.MaxBytesError", err, err)
	}
	if maxErr.Limit != 8 {
		t.Fatalf("limit = %d, want 8", maxErr.Limit)
	}
}

func TestReadBodyUpToLimitAcceptsExactLimit(t *testing.T) {
	t.Parallel()

	got, err := readBodyUpToLimit(strings.NewReader("12345678"), 8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "12345678" {
		t.Fatalf("body = %q, want exact input", got)
	}
}

const samplePayload = `{"model":"gpt-5.5","input":"hi","stream":false}`

func newRequestWithBody(t *testing.T, body []byte, encoding string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	req.ContentLength = int64(len(body))
	return req
}

func TestReadRequestBodyWithPrealloc_PassesThroughIdentity(t *testing.T) {
	req := newRequestWithBody(t, []byte(samplePayload), "")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}

func TestReadRequestBodyWithLimitUsesExactKnownLengthAllocation(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), requestBodyReadMaxInitCap+123)
	req := newRequestWithBody(t, payload, "")

	got, err := ReadRequestBodyWithLimit(req, int64(len(payload)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("body mismatch")
	}
	if cap(got) != len(payload) {
		t.Fatalf("body capacity = %d, want exact %d", cap(got), len(payload))
	}
}

func TestReadRequestBodyWithLimitBoundsUnknownLengthGrowth(t *testing.T) {
	const limit = int64((2 << 20) + 17)
	payload := bytes.Repeat([]byte("x"), int(limit))
	req := newRequestWithBody(t, payload, "")
	req.ContentLength = -1

	got, err := ReadRequestBodyWithLimit(req, limit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != int(limit) || cap(got) > int(limit) {
		t.Fatalf("body len/cap = %d/%d, want len %d and cap <= limit", len(got), cap(got), limit)
	}

	oversized := newRequestWithBody(t, append(payload, 'x'), "")
	oversized.ContentLength = -1
	_, err = ReadRequestBodyWithLimit(oversized, limit)
	var maxErr *http.MaxBytesError
	if !errors.As(err, &maxErr) || maxErr.Limit != limit {
		t.Fatalf("error = %v, want MaxBytesError(%d)", err, limit)
	}
}

func TestBoundedRequestBodyMemoryReservation(t *testing.T) {
	if got, want := BoundedRequestBodyMemoryReservation(256<<20), int64(384<<20); got != want {
		t.Fatalf("reservation = %d, want %d", got, want)
	}
	if got := BoundedRequestBodyMemoryReservation(math.MaxInt64); got != math.MaxInt64 {
		t.Fatalf("overflow reservation = %d, want MaxInt64", got)
	}
}

func TestBoundedRequestBodyGrowthKeepsPreviousAllocationWithinHalfLimit(t *testing.T) {
	for _, limit := range []int64{513, (1 << 20) + 1, (129 << 20) + 7, 256 << 20} {
		halfLimit := limit/2 + limit%2
		current := int64(requestBodyReadInitCap)
		if current > halfLimit {
			current = halfLimit
		}
		for current < limit {
			previous := current
			current = nextBoundedRequestBodyCapacity(current, limit)
			if current <= previous || current > limit {
				t.Fatalf("limit=%d invalid growth %d -> %d", limit, previous, current)
			}
			if current == limit && previous > halfLimit {
				t.Fatalf("limit=%d final allocation retains %d bytes, greater than half limit %d", limit, previous, halfLimit)
			}
		}
	}
}

func TestReadRequestBodyWithPrealloc_DecodesZstd(t *testing.T) {
	enc, _ := zstd.NewWriter(nil)
	compressed := enc.EncodeAll([]byte(samplePayload), nil)
	_ = enc.Close()

	req := newRequestWithBody(t, compressed, "zstd")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
	if req.Header.Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding should be cleared after decoding")
	}
	if req.ContentLength != int64(len(samplePayload)) {
		t.Fatalf("ContentLength not updated: %d", req.ContentLength)
	}
}

func TestReadRequestBodyWithPrealloc_DecodesGzip(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte(samplePayload)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	req := newRequestWithBody(t, buf.Bytes(), "gzip")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}

func TestReadRequestBodyWithPrealloc_DecodesDeflate(t *testing.T) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write([]byte(samplePayload)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	req := newRequestWithBody(t, buf.Bytes(), "deflate")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}

func TestReadRequestBodyWithPrealloc_RejectsUnsupportedEncoding(t *testing.T) {
	req := newRequestWithBody(t, []byte(samplePayload), "br")
	_, err := ReadRequestBodyWithPrealloc(req)
	if err == nil {
		t.Fatal("expected error for unsupported encoding, got nil")
	}
	if !strings.Contains(err.Error(), "br") {
		t.Fatalf("error should mention encoding, got %v", err)
	}
}

func TestReadRequestBodyWithPrealloc_RejectsCorruptZstd(t *testing.T) {
	req := newRequestWithBody(t, []byte("not actually zstd"), "zstd")
	_, err := ReadRequestBodyWithPrealloc(req)
	if err == nil {
		t.Fatal("expected error for corrupt zstd body, got nil")
	}
}

func TestReadRequestBodyWithPrealloc_RejectsZstdWindowAboveLimit(t *testing.T) {
	header := zstd.Header{WindowSize: maxZstdDecoderWindowSize * 2}
	frame, err := header.AppendTo(nil)
	if err != nil {
		t.Fatalf("build zstd frame header: %v", err)
	}
	// Last raw block with an empty payload. The decoder must reject the frame
	// header before allocating history for the advertised window.
	frame = append(frame, 0x01, 0x00, 0x00)

	req := newRequestWithBody(t, frame, "zstd")
	_, err = ReadRequestBodyWithPrealloc(req)
	if !errors.Is(err, zstd.ErrWindowSizeExceeded) {
		t.Fatalf("error = %v, want ErrWindowSizeExceeded", err)
	}
}

func TestReadRequestBodyWithPrealloc_NilBody(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/v1/responses", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil body, got %q", got)
	}
}

func TestReadRequestBodyWithPrealloc_RespectsIdentityEncoding(t *testing.T) {
	req := newRequestWithBody(t, []byte(samplePayload), "identity")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}
