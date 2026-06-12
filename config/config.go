package config

import (
	"flag"
	"net/url"
	"os"
	"strings"
)

// Config holds the configuration options for the proxy server.
type Config struct {
	Port           string
	TargetURL      *url.URL
	LogLevel       string
	AllowedOrigins []string
	AuthToken      string
}

// LoadConfig merges CLI flags and environment variables to construct the Config.
func LoadConfig() (*Config, error) {
	// CLI Flags take precedence
	portFlag := flag.String("port", "", "Port to run the proxy server on (default: :8080)")
	targetFlag := flag.String("target", "", "Target URL to proxy requests to (default: https://httpbin.org)")
	logLevelFlag := flag.String("log-level", "", "Log level: debug, info, warn, error (default: info)")
	allowedOriginsFlag := flag.String("allowed-origins", "", "Comma-separated allowed CORS origins (default: *)")
	authTokenFlag := flag.String("auth-token", "", "Optional authorization bearer token to inject into backend requests")
	flag.Parse()

	getEnv := func(key, fallback string) string {
		if value, ok := os.LookupEnv(key); ok {
			return value
		}
		return fallback
	}

	port := *portFlag
	if port == "" {
		port = getEnv("PROXY_PORT", ":8080")
	}
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	rawTarget := *targetFlag
	if rawTarget == "" {
		rawTarget = getEnv("PROXY_TARGET_URL", "http://localhost:11434")
	}
	targetURL, err := url.Parse(rawTarget)
	if err != nil {
		return nil, err
	}

	logLevel := *logLevelFlag
	if logLevel == "" {
		logLevel = getEnv("PROXY_LOG_LEVEL", "info")
	}

	rawOrigins := *allowedOriginsFlag
	if rawOrigins == "" {
		rawOrigins = getEnv("PROXY_ALLOWED_ORIGINS", "*")
	}
	var allowedOrigins []string
	for _, o := range strings.Split(rawOrigins, ",") {
		trimmed := strings.TrimSpace(o)
		if trimmed != "" {
			allowedOrigins = append(allowedOrigins, trimmed)
		}
	}

	authToken := *authTokenFlag
	if authToken == "" {
		authToken = getEnv("PROXY_AUTH_TOKEN", "")
	}

	return &Config{
		Port:           port,
		TargetURL:      targetURL,
		LogLevel:       strings.ToLower(logLevel),
		AllowedOrigins: allowedOrigins,
		AuthToken:      authToken,
	}, nil
}
