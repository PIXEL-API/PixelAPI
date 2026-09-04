package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	requestBodyReadInitCap    = 512
	requestBodyReadMaxInitCap = 1 << 20
	// MaxDecompressedRequestBodySize is the maximum decoded request body size.
	// Callers that reserve memory before decoding compressed requests must use
	// this same value so admission and decoding cannot drift apart.
	MaxDecompressedRequestBodySize int64 = 64 << 20
	// MaxCompressedRequestBodyMemoryReservation covers the bounded decoded
	// buffer's growth peak plus gzip/deflate decoder state and framing slack.
	MaxCompressedRequestBodyMemoryReservation int64 = 3 * MaxDecompressedRequestBodySize
	// MaxZstdRequestBodyMemoryReservation additionally covers the bounded zstd
	// history window and decoder overhead. It is the largest compressed-body
	// reservation and therefore the minimum capacity for an enabled body gate.
	MaxZstdRequestBodyMemoryReservation int64 = MaxCompressedRequestBodyMemoryReservation + int64(maxZstdDecoderWindowSize) + (4 << 20)
	// maxZstdDecoderWindowSize preserves support for every payload within the
	// library encoder's normal maximum window while rejecting custom frames
	// that advertise the decoder default's much larger (512 MiB) window.
	maxZstdDecoderWindowSize uint64 = 8 << 20
	// maxDecompressedBodySize limits the decompressed request body to 64 MB
	// to prevent decompression bomb attacks.
	maxDecompressedBodySize = MaxDecompressedRequestBodySize
)

// ReadRequestBodyWithPrealloc reads request body with preallocated buffer based
// on content length, transparently decoding any Content-Encoding the upstream
// client used to compress the body (zstd, gzip, deflate).
func ReadRequestBodyWithPrealloc(req *http.Request) ([]byte, error) {
	return readRequestBody(req, 0)
}

// ReadRequestBodyWithLimit reads a request with a caller-provided hard limit.
// Identity bodies with a trustworthy, in-range Content-Length are allocated
// exactly once; unknown-length bodies use bounded growth that never doubles
// merely to probe EOF.
func ReadRequestBodyWithLimit(req *http.Request, maxBodySize int64) ([]byte, error) {
	if maxBodySize <= 0 {
		return nil, errors.New("request body limit must be positive")
	}
	return readRequestBody(req, maxBodySize)
}

func readRequestBody(req *http.Request, maxBodySize int64) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, nil
	}
	if maxBodySize > 0 && req.ContentLength > maxBodySize {
		return nil, &http.MaxBytesError{Limit: maxBodySize}
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
	if maxBodySize > 0 {
		if req.ContentLength > 0 && len(req.TransferEncoding) == 0 {
			return readBodyWithExactLength(req.Body, req.ContentLength)
		}
		return readBodyUpToLimit(req.Body, maxBodySize)
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

func readBodyWithExactLength(reader io.Reader, length int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("body reader is nil")
	}
	if length < 0 || uint64(length) > uint64(math.MaxInt) {
		return nil, errors.New("request body length cannot be represented in memory")
	}
	body := make([]byte, int(length))
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	return body, nil
}

// BoundedRequestBodyMemoryReservation returns a conservative upper bound for
// the live byte slices used while a bounded buffer doubles. The largest growth
// retains the previous half-sized slice while allocating the final slice.
func BoundedRequestBodyMemoryReservation(limit int64) int64 {
	if limit <= 0 {
		return 0
	}
	half := limit/2 + limit%2
	if limit > math.MaxInt64-half {
		return math.MaxInt64
	}
	return limit + half
}

func decompressRequestBody(encoding string, source io.Reader) ([]byte, error) {
	if source == nil {
		return nil, errors.New("compressed request body is nil")
	}

	var reader io.Reader
	var closeReader func()
	switch encoding {
	case "zstd":
		dec, err := zstd.NewReader(
			source,
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderLowmem(true),
			zstd.WithDecoderMaxMemory(uint64(MaxDecompressedRequestBodySize)),
			zstd.WithDecoderMaxWindow(maxZstdDecoderWindowSize),
			zstd.WithDecodeBuffersBelow(0),
		)
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
	if uint64(limit) > uint64(math.MaxInt) {
		return nil, errors.New("body limit cannot be represented in memory")
	}
	if limit == 0 {
		var probe [1]byte
		n, err := reader.Read(probe[:])
		if n > 0 {
			return nil, &http.MaxBytesError{Limit: limit}
		}
		if err == nil || errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}

	halfLimit := limit/2 + limit%2
	initialCapacity := int64(requestBodyReadInitCap)
	if initialCapacity > halfLimit {
		initialCapacity = halfLimit
	}
	body := make([]byte, 0, int(initialCapacity))
	for {
		if len(body) == cap(body) {
			if int64(len(body)) == limit {
				var probe [1]byte
				n, err := reader.Read(probe[:])
				if n > 0 {
					return nil, &http.MaxBytesError{Limit: limit}
				}
				if errors.Is(err, io.EOF) {
					return body, nil
				}
				if err != nil {
					return nil, err
				}
				continue
			}

			newCapacity := nextBoundedRequestBodyCapacity(int64(cap(body)), limit)
			grown := make([]byte, len(body), int(newCapacity))
			copy(grown, body)
			body = grown
		}

		n, err := reader.Read(body[len(body):cap(body)])
		if n < 0 || n > cap(body)-len(body) {
			return nil, errors.New("invalid body reader result")
		}
		body = body[:len(body)+n]
		if errors.Is(err, io.EOF) {
			return body, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func nextBoundedRequestBodyCapacity(current, limit int64) int64 {
	if current >= limit {
		return limit
	}
	halfLimit := limit/2 + limit%2
	if current >= halfLimit {
		return limit
	}
	next := current * 2
	if next > halfLimit {
		return halfLimit
	}
	return next
}
