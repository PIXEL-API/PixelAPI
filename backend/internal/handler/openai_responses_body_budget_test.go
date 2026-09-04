package handler

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesBodyReservation(t *testing.T) {
	const maxBody = int64(256 << 20)

	req, err := http.NewRequest(http.MethodPost, "http://example.test/v1/responses", bytes.NewReader(make([]byte, 4096)))
	require.NoError(t, err)
	reservation, err := openAIResponsesBodyReservation(req, maxBody)
	require.NoError(t, err)
	require.Equal(t, int64(4096), reservation)

	req.Header.Set("Content-Encoding", "gzip")
	reservation, err = openAIResponsesBodyReservation(req, maxBody)
	require.NoError(t, err)
	require.Equal(t, pkghttputil.MaxCompressedRequestBodyMemoryReservation, reservation)

	req.Header.Set("Content-Encoding", "zstd")
	reservation, err = openAIResponsesBodyReservation(req, maxBody)
	require.NoError(t, err)
	require.Equal(t, pkghttputil.MaxZstdRequestBodyMemoryReservation, reservation)
	req.ContentLength = maxBody + 1
	_, err = openAIResponsesBodyReservation(req, maxBody)
	var compressedMaxErr *http.MaxBytesError
	require.ErrorAs(t, err, &compressedMaxErr)
	require.Equal(t, maxBody, compressedMaxErr.Limit)

	req.Header.Del("Content-Encoding")
	req.ContentLength = -1
	reservation, err = openAIResponsesBodyReservation(req, maxBody)
	require.NoError(t, err)
	require.Equal(t, pkghttputil.BoundedRequestBodyMemoryReservation(maxBody), reservation)

	req.ContentLength = maxBody + 1
	_, err = openAIResponsesBodyReservation(req, maxBody)
	var maxErr *http.MaxBytesError
	require.ErrorAs(t, err, &maxErr)
	require.Equal(t, maxBody, maxErr.Limit)
}

func TestReadResponsesRequestBodyTimesOutSlowHTTPUpload(t *testing.T) {
	budget, err := service.NewOpenAIResponsesBodyBudget(config.GatewayOpenAIResponsesBodyBudgetConfig{
		Enabled:            true,
		CapacityBytes:      1,
		WaitTimeoutSeconds: 1,
		ReadTimeoutSeconds: 1,
		RetryAfterSeconds:  1,
	})
	require.NoError(t, err)
	h := &OpenAIGatewayHandler{responsesBodyBudget: budget}
	readResult := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_, readErr := h.readResponsesRequestBody(writer, req)
		readResult <- readErr
		writer.WriteHeader(http.StatusRequestTimeout)
	}))
	defer server.Close()

	address := strings.TrimPrefix(server.URL, "http://")
	conn, err := net.DialTimeout("tcp", address, time.Second)
	require.NoError(t, err)
	defer conn.Close()
	_, err = io.WriteString(conn, "POST /v1/responses HTTP/1.1\r\nHost: "+address+"\r\nContent-Length: 32\r\nConnection: close\r\n\r\n{")
	require.NoError(t, err)

	startedAt := time.Now()
	select {
	case readErr := <-readResult:
		require.ErrorIs(t, readErr, errOpenAIResponsesBodyReadTimeout)
		require.Less(t, time.Since(startedAt), 2500*time.Millisecond)
	case <-time.After(3 * time.Second):
		t.Fatal("slow HTTP request body was not interrupted by the read deadline")
	}
}

type readDeadlineGinWriter struct {
	gin.ResponseWriter
	deadlines []time.Time
}

func (w *readDeadlineGinWriter) SetReadDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func TestReadResponsesRequestBodySetsDeadlineThroughOpsCaptureWriter(t *testing.T) {
	budget, err := service.NewOpenAIResponsesBodyBudget(config.GatewayOpenAIResponsesBodyBudgetConfig{
		Enabled:            true,
		CapacityBytes:      1,
		WaitTimeoutSeconds: 1,
		ReadTimeoutSeconds: 1,
		RetryAfterSeconds:  1,
	})
	require.NoError(t, err)
	h := &OpenAIGatewayHandler{responsesBodyBudget: budget}
	underlying := &readDeadlineGinWriter{}
	writer := &opsCaptureWriter{ResponseWriter: underlying}
	req, err := http.NewRequest(http.MethodPost, "http://example.test/v1/responses", strings.NewReader(`{"model":"gpt-5"}`))
	require.NoError(t, err)

	body, err := h.readResponsesRequestBody(writer, req)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-5"}`, string(body))
	require.Len(t, underlying.deadlines, 2)
	require.False(t, underlying.deadlines[0].IsZero())
	require.True(t, underlying.deadlines[1].IsZero())
}

func TestReadResponsesRequestBodyKeepsDeadlineAfterDecodeError(t *testing.T) {
	budget, err := service.NewOpenAIResponsesBodyBudget(config.GatewayOpenAIResponsesBodyBudgetConfig{
		Enabled:            true,
		CapacityBytes:      1,
		WaitTimeoutSeconds: 1,
		ReadTimeoutSeconds: 1,
		RetryAfterSeconds:  1,
	})
	require.NoError(t, err)
	cfg := &config.Config{}
	cfg.Gateway.MaxBodySize = 32
	h := &OpenAIGatewayHandler{cfg: cfg, responsesBodyBudget: budget}
	underlying := &readDeadlineGinWriter{}
	writer := &opsCaptureWriter{ResponseWriter: underlying}
	req, err := http.NewRequest(http.MethodPost, "http://example.test/v1/responses", strings.NewReader("{"))
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "br")

	beforeRead := time.Now()
	_, err = h.readResponsesRequestBody(writer, req)

	require.Error(t, err)
	require.Len(t, underlying.deadlines, 2)
	require.False(t, underlying.deadlines[0].IsZero())
	require.True(t, underlying.deadlines[1].Before(beforeRead), "a partial-body error must immediately expire the read deadline")
}

func TestReadResponsesRequestBodyExpiresDeadlineAfterDecodeErrorWithoutBudget(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.MaxBodySize = 32
	h := &OpenAIGatewayHandler{cfg: cfg}
	underlying := &readDeadlineGinWriter{}
	writer := &opsCaptureWriter{ResponseWriter: underlying}
	req, err := http.NewRequest(http.MethodPost, "http://example.test/v1/responses", strings.NewReader("{"))
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "br")

	beforeRead := time.Now()
	_, err = h.readResponsesRequestBody(writer, req)

	require.Error(t, err)
	require.Len(t, underlying.deadlines, 1)
	require.True(t, underlying.deadlines[0].Before(beforeRead))
}

func TestReadResponsesRequestBodyUnsupportedEncodingClosesSlowConnectionAndReleasesLease(t *testing.T) {
	budget, err := service.NewOpenAIResponsesBodyBudget(config.GatewayOpenAIResponsesBodyBudgetConfig{
		Enabled:            true,
		CapacityBytes:      1,
		WaitTimeoutSeconds: 1,
		ReadTimeoutSeconds: 30,
		RetryAfterSeconds:  1,
	})
	require.NoError(t, err)
	cfg := &config.Config{}
	cfg.Gateway.MaxBodySize = 32
	h := &OpenAIGatewayHandler{cfg: cfg, responsesBodyBudget: budget}
	readResult := make(chan error, 1)
	handlerReturned := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		defer close(handlerReturned)
		lease, acquireErr := budget.Acquire(req.Context(), 1)
		if acquireErr != nil {
			readResult <- acquireErr
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer lease.Release()

		_, readErr := h.readResponsesRequestBody(writer, req)
		readResult <- readErr
		if readErr == nil {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		markOpenAIResponsesRequestConnectionUnusable(writer, req)
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	address := strings.TrimPrefix(server.URL, "http://")
	conn, err := net.DialTimeout("tcp", address, time.Second)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(3*time.Second)))
	_, err = io.WriteString(conn, "POST /v1/responses HTTP/1.1\r\nHost: "+address+"\r\nContent-Length: 32\r\nContent-Encoding: br\r\n\r\n{")
	require.NoError(t, err)

	startedAt := time.Now()
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.True(t, response.Close)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Less(t, time.Since(startedAt), time.Second)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, err = reader.Peek(1)
	require.Error(t, err, "the rejected HTTP/1 connection must be physically closed")
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Fatalf("connection remained open after the body read failed: %v", err)
	}

	select {
	case readErr := <-readResult:
		require.Error(t, readErr)
	case <-time.After(3 * time.Second):
		t.Fatal("unsupported body decoder did not return")
	}
	select {
	case <-handlerReturned:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not return after rejecting the partial request body")
	}
	require.Zero(t, budget.Snapshot().InUseBytes)
}

func TestMarkOpenAIResponsesRequestConnectionUnusableDoesNotEmitHTTP2ConnectionHeader(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		protoMajor int
		wantHeader string
	}{
		{name: "http1", protoMajor: 1, wantHeader: "close"},
		{name: "http2", protoMajor: 2, wantHeader: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("{"))
			req.ProtoMajor = testCase.protoMajor

			markOpenAIResponsesRequestConnectionUnusable(recorder, req)

			require.True(t, req.Close)
			require.Equal(t, testCase.wantHeader, recorder.Header().Get("Connection"))
		})
	}
}

func TestReadResponsesRequestBodyPreservesStreamingDecompression(t *testing.T) {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, err := io.WriteString(zw, `{"model":"gpt-5","input":"hello"}`)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	req, err := http.NewRequest(http.MethodPost, "http://example.test/v1/responses", bytes.NewReader(compressed.Bytes()))
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "gzip")
	h := &OpenAIGatewayHandler{}
	got, err := h.readResponsesRequestBody(nil, req)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-5","input":"hello"}`, string(got))
}
