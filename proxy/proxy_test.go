package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"heaprag-proxy/config"
	"heaprag-proxy/middleware"
)

func TestProxySuccess(t *testing.T) {
	// 1. Create a mock backend server
	backendReceivedHeaders := make(http.Header)
	backendReceivedHost := ""
	backendReceivedPath := ""

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendReceivedHost = r.Host
		backendReceivedPath = r.URL.Path
		// Capture headers
		for k, v := range r.Header {
			backendReceivedHeaders[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("failed to parse backend URL: %v", err)
	}

	// 2. Set up config and Proxy
	cfg := &config.Config{
		TargetURL: backendURL,
		AuthToken: "test-auth-token-123",
	}
	
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	p := NewProxy(cfg, logger)

	// Wrap proxy in RequestID middleware to test tracing headers
	handler := middleware.RequestID(p)

	// 3. Make client request to proxy
	req := httptest.NewRequest("GET", "http://localhost:8080/foo/bar?baz=qux", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// 4. Assertions
	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Verify target host rewriting
	if backendReceivedHost != backendURL.Host {
		t.Errorf("expected backend host %q, got %q", backendURL.Host, backendReceivedHost)
	}

	// Verify path preservation
	if backendReceivedPath != "/foo/bar" {
		t.Errorf("expected backend path %q, got %q", "/foo/bar", backendReceivedPath)
	}

	// Verify headers injected/propagated
	if backendReceivedHeaders.Get("Authorization") != "Bearer test-auth-token-123" {
		t.Errorf("expected Authorization header to be injected, got %q", backendReceivedHeaders.Get("Authorization"))
	}

	reqID := resp.Header.Get("X-Request-ID")
	if reqID == "" {
		t.Error("expected X-Request-ID response header to be set")
	}

	if backendReceivedHeaders.Get("X-Request-ID") != reqID {
		t.Errorf("expected backend request ID %q, got %q", reqID, backendReceivedHeaders.Get("X-Request-ID"))
	}

	if backendReceivedHeaders.Get("X-Forwarded-For") == "" {
		t.Error("expected X-Forwarded-For header to be set")
	}
}

func TestProxyBackendUnreachable(t *testing.T) {
	// Set up config pointing to a port that's not listening
	badURL, _ := url.Parse("http://127.0.0.1:54321")
	cfg := &config.Config{
		TargetURL: badURL,
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	p := NewProxy(cfg, logger)
	handler := middleware.RequestID(p)

	req := httptest.NewRequest("GET", "http://localhost:8080/some-path", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	// Verify HTTP status is 502 Bad Gateway
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", resp.StatusCode)
	}

	// Verify JSON error response structure
	var errorResp ProxyErrorResponse
	err := json.NewDecoder(resp.Body).Decode(&errorResp)
	if err != nil {
		t.Fatalf("failed to decode JSON error response: %v", err)
	}

	if errorResp.Error != "Bad Gateway" {
		t.Errorf("expected error field to be 'Bad Gateway', got %q", errorResp.Error)
	}

	if !strings.Contains(errorResp.Message, "dial tcp") {
		t.Errorf("expected message to mention connection failure, got %q", errorResp.Message)
	}

	if errorResp.RequestID == "" {
		t.Error("expected request_id field in JSON response to be set")
	}
}
