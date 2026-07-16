package servertiming

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackingBody struct{ read bool }

func (b *trackingBody) Read(_ []byte) (int, error) {
	b.read = true
	return 0, io.EOF
}

func (b *trackingBody) Close() error { return nil }

type closeTrackingRoundTripper struct {
	roundTripFunc
	closed bool
}

func (t *closeTrackingRoundTripper) CloseIdleConnections() { t.closed = true }

func TestWrapRoundTripperRecordsOnlyResponseHeaderLatency(t *testing.T) {
	collector := New(time.Now())
	body := &trackingBody{}
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header), Request: req}, nil
	})
	req, err := http.NewRequestWithContext(WithCollector(context.Background(), collector), http.MethodGet, "https://api.github.com/repos/example/project?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := WrapRoundTripper(base).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if body.read {
		t.Fatal("instrumentation read the response body")
	}
	header := collector.HeaderValue(time.Now(), "bypass")
	if !strings.Contains(header, `dep_github;dur=`) || strings.Contains(header, "token") || strings.Contains(header, "secret") {
		t.Fatalf("unexpected dependency header: %q", header)
	}
}

func TestWrapRoundTripperRejectsUnknownModuleOverride(t *testing.T) {
	collector := New(time.Now())
	ctx := WithDependencyModule(WithCollector(context.Background(), collector), "data-managementd\r\nsecret")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://private.example.test/path", nil)
	if err != nil {
		t.Fatal(err)
	}
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
	})

	if _, err := WrapRoundTripper(base).RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	header := collector.HeaderValue(time.Now(), "bypass")
	if !strings.Contains(header, "dep_http") || strings.ContainsAny(header, "\r\n") || strings.Contains(header, "private.example") || strings.Contains(header, "secret") {
		t.Fatalf("module sanitization failed: %q", header)
	}
}

func TestInactiveHTTPInstrumentationPreservesClientAndTransportSemantics(t *testing.T) {
	called := false
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
	})
	original := &http.Client{Transport: base, Timeout: time.Second}
	instrumented := InstrumentClient(original)
	if instrumented == original || instrumented.Timeout != original.Timeout {
		t.Fatal("InstrumentClient did not return a settings-preserving shallow copy")
	}
	if _, ok := original.Transport.(roundTripFunc); !ok {
		t.Fatalf("original transport mutated to %T", original.Transport)
	}
	if WrapRoundTripper(instrumented.Transport) != instrumented.Transport {
		t.Fatal("transport was wrapped twice")
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instrumented.Do(req); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("inactive request did not reach base transport")
	}
}

func TestWrapRoundTripperPreservesIdleConnectionCleanup(t *testing.T) {
	base := &closeTrackingRoundTripper{roundTripFunc: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
	}}
	client := &http.Client{Transport: WrapRoundTripper(base)}
	client.CloseIdleConnections()
	if !base.closed {
		t.Fatal("CloseIdleConnections was not delegated to the base transport")
	}
}

func TestDoRecordsWithoutChangingTransportType(t *testing.T) {
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
	})
	client := &http.Client{Transport: base}
	collector := New(time.Now())
	req, err := http.NewRequestWithContext(WithCollector(context.Background(), collector), http.MethodGet, "https://api.openai.com/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Do(client, req); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.Transport.(roundTripFunc); !ok {
		t.Fatalf("Do changed client transport type to %T", client.Transport)
	}
	if header := collector.HeaderValue(time.Now(), "bypass"); !strings.Contains(header, "dep_openai;dur=") {
		t.Fatalf("dependency metric missing: %q", header)
	}
}
