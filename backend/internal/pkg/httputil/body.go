package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	requestBodyReadInitCap    = 512
	requestBodyReadMaxInitCap = 1 << 20
	// maxDecompressedBodySize limits the decompressed request body to 64 MB
	// to prevent decompression bomb attacks.
	maxDecompressedBodySize = 64 << 20
)

// ReadRequestBodyWithPrealloc reads request body with preallocated buffer based
// on content length, transparently decoding any Content-Encoding the upstream
// client used to compress the body (zstd, gzip, deflate).
func ReadRequestBodyWithPrealloc(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, nil
	}

	enc := strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Encoding")))
	if enc != "" && enc != "identity" {
		// Decode directly from the request stream. The previous implementation
		// first buffered the complete compressed body and then allocated a second
		// buffer for the decoded body, so a single request could retain both
		// copies until the function returned. Streaming the decoder keeps the
		// peak close to the decoded limit instead of compressed+decoded size.
		decoded, err := decompressRequestBody(enc, req.Body)
		if err != nil {
			return nil, fmt.Errorf("decode Content-Encoding %q: %w", enc, err)
		}
		req.Header.Del("Content-Encoding")
		req.Header.Del("Content-Length")
		req.ContentLength = int64(len(decoded))
		return decoded, nil
	}

	capHint := requestBodyReadInitCap
	if req.ContentLength > 0 {
		switch {
		case req.ContentLength < int64(requestBodyReadInitCap):
			capHint = requestBodyReadInitCap
		case req.ContentLength > int64(requestBodyReadMaxInitCap):
			capHint = requestBodyReadMaxInitCap
		default:
			capHint = int(req.ContentLength)
		}
	}

	buf := bytes.NewBuffer(make([]byte, 0, capHint))
	if _, err := io.Copy(buf, req.Body); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decompressRequestBody(encoding string, source io.Reader) ([]byte, error) {
	if source == nil {
		return nil, errors.New("compressed request body is nil")
	}

	var reader io.Reader
	var closeReader func()
	switch encoding {
	case "zstd":
		dec, err := zstd.NewReader(source)
		if err != nil {
			return nil, err
		}
		reader = dec
		closeReader = dec.Close
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(source)
		if err != nil {
			return nil, err
		}
		reader = gr
		closeReader = func() { _ = gr.Close() }
	case "deflate":
		zr, err := zlib.NewReader(source)
		if err != nil {
			return nil, err
		}
		reader = zr
		closeReader = func() { _ = zr.Close() }
	default:
		return nil, errors.New("unsupported Content-Encoding")
	}
	if closeReader != nil {
		defer closeReader()
	}

	return readBodyUpToLimit(reader, maxDecompressedBodySize)
}

func readBodyUpToLimit(reader io.Reader, limit int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("body reader is nil")
	}
	if limit < 0 {
		return nil, errors.New("body limit is negative")
	}
	// Read one byte beyond the limit so an oversized decoded payload is
	// rejected instead of silently truncated into invalid JSON. Returning the
	// standard error type lets gateway handlers consistently answer 413.
	decoded, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(decoded)) > limit {
		return nil, &http.MaxBytesError{Limit: limit}
	}
	return decoded, nil
}
