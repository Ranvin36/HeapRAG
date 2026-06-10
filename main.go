package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"heaprag-proxy/config"
	"heaprag-proxy/middleware"
	"heaprag-proxy/proxy"
)

func main() {
	// 1. Load configuration from env/flags
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("Failed to load configuration", slog.Any("error", err))
		os.Exit(1)
	}

	// 2. Set up structured logging (slog)
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))

	// Set as default logger so other standard logs use it too
	slog.SetDefault(logger)

	// 3. Initialize reverse proxy
	proxyHandler := proxy.NewProxy(cfg, logger)

	// 4. Construct middleware chain
	// Order of execution (outermost to innermost):
	// Recovery -> RequestID -> CORS -> Logger -> Proxy Handler
	var handler http.Handler = proxyHandler
	handler = middleware.Logger(logger)(handler)
	handler = middleware.CORS(cfg.AllowedOrigins)(handler)
	handler = middleware.RequestID(handler)
	handler = middleware.Recovery(logger)(handler)

	// 5. Configure HTTP server
	server := &http.Server{
		Addr:         cfg.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 6. Start server in a background goroutine
	go func() {
		logger.Info("Starting HeapRAG proxy server",
			slog.String("port", cfg.Port),
			slog.String("target", cfg.TargetURL.String()),
			slog.String("log_level", cfg.LogLevel),
		)

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed to listen/serve", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// 7. Graceful shutdown listening to interrupt signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	<-quit
	logger.Info("Shutdown signal received, shutting down gracefully...")

	// Allow pending requests up to 10 seconds to finish
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", slog.Any("error", err))
		os.Exit(1)
	}

	logger.Info("HeapRAG proxy server stopped clean")
}
