package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"heaprag-proxy/config"
	"heaprag-proxy/middleware"
)

// Proxy wraps the httputil.ReverseProxy to provide extra configuration and logging.
type Proxy struct {
	reverseProxy *httputil.ReverseProxy
	targetHost   string
	authToken    string
	logger       *slog.Logger
}

// ProxyErrorResponse represents the JSON structure for proxy-level errors.
type ProxyErrorResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// NewProxy creates and configures a new ReverseProxy instance.
func NewProxy(cfg *config.Config, logger *slog.Logger) *Proxy {
	target := cfg.TargetURL

	// Configure the HTTP transport for the reverse proxy with sensible timeouts
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	p := &Proxy{
		targetHost: target.Host,
		authToken:  cfg.AuthToken,
		logger:     logger,
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Set the destination URL scheme, host, and path
			pr.SetURL(target)

			// Clean up and set forwarding headers
			pr.SetXForwarded()

			// Propagate the Request ID header to backend
			reqID := middleware.GetRequestID(pr.In.Context())
			if reqID != "" {
				pr.Out.Header.Set("X-Request-ID", reqID)
			}

			// Override/inject Authorization header if configured
			if p.authToken != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+p.authToken)
			}

			// Ensure the Host header matches the target server host
			pr.Out.Host = target.Host
		},
		Transport:    transport,
		ErrorHandler: p.errorHandler,
	}

	p.reverseProxy = rp
	return p
}

// ServeHTTP implements http.Handler, forwarding requests to the target.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.reverseProxy.ServeHTTP(w, r)
}

// errorHandler intercepts proxying failures (e.g. backend unreachable) and writes JSON.
func (p *Proxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	reqID := middleware.GetRequestID(r.Context())

	// Determine the error type to provide an appropriate HTTP status
	statusCode := http.StatusBadGateway
	if errors.Is(err, context.Canceled) {
		statusCode = 499 // Client Closed Request
		p.logger.Warn("Client closed request prematurely",
			slog.String("request_id", reqID),
			slog.String("path", r.URL.Path),
		)
	} else {
		p.logger.Error("Proxy forwarding error",
			slog.String("request_id", reqID),
			slog.String("path", r.URL.Path),
			slog.String("target", p.targetHost),
			slog.Any("error", err),
		)
	}

	// Respond with structured JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorText := http.StatusText(statusCode)
	if errorText == "" {
		errorText = "Client Closed Request"
	}

	resp := ProxyErrorResponse{
		Error:     errorText,
		Message:   err.Error(),
		RequestID: reqID,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		p.logger.Error("Failed to write proxy error response",
			slog.String("request_id", reqID),
			slog.Any("error", err),
		)
	}
}
