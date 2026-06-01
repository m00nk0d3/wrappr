// Package config loads and validates application configuration from environment
// variables. Call Load() once at startup and inject the returned Config into
// all components that need it. Load() fails fast: if a required variable is
// absent or malformed the process exits with a descriptive error rather than
// silently using a bad value.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration for the server.
type Config struct {
	// Port is the TCP port the HTTP server listens on (e.g. "8080").
	Port string

	// DatabaseURL is the PostgreSQL connection string (required).
	DatabaseURL string

	// AppURL is the public base URL of the frontend app (required).
	// Used to build magic-link URLs like {AppURL}/auth/magic?token={token}.
	AppURL string

	// ResendAPIKey is the API key for the Resend email service (required).
	ResendAPIKey string

	// JWTSecret is the HMAC-SHA256 signing secret for issued JWTs (required).
	JWTSecret string

	// RedisURL is the Redis connection string used by Asynq (required).
	// e.g. "redis://localhost:6379"
	RedisURL string

	// R2AccountID is the Cloudflare account ID for the R2 storage bucket.
	R2AccountID string

	// R2AccessKeyID is the R2 API access key ID.
	R2AccessKeyID string

	// R2SecretAccessKey is the R2 API secret access key.
	R2SecretAccessKey string

	// R2Bucket is the name of the R2 bucket used to store job files.
	R2Bucket string

	// GroqAPIKey is the API key for the Groq Whisper transcription API.
	// Optional at config load time; cmd/worker validates it is non-empty at startup.
	GroqAPIKey string
}

// Load reads configuration from environment variables and returns a validated
// Config. It returns an error for any value that is present but malformed;
// absent optional variables fall back to documented defaults.
func Load() (*Config, error) {
	port, err := loadPort()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	databaseURL, err := requireEnv("DATABASE_URL")
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	appURL, err := requireEnv("APP_URL")
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	resendAPIKey, err := requireEnv("RESEND_API_KEY")
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	jwtSecret, err := requireEnv("JWT_SECRET")
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if len(jwtSecret) < 32 {
		return nil, fmt.Errorf("config: JWT_SECRET must be at least 32 characters")
	}

	redisURL, err := requireEnv("REDIS_URL")
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	r2AccountID, err := requireEnv("R2_ACCOUNT_ID")
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	r2AccessKeyID, err := requireEnv("R2_ACCESS_KEY_ID")
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	r2SecretAccessKey, err := requireEnv("R2_SECRET_ACCESS_KEY")
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	r2Bucket, err := requireEnv("R2_BUCKET")
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	groqAPIKey := os.Getenv("GROQ_API_KEY")

	return &Config{
		Port:              port,
		DatabaseURL:       databaseURL,
		AppURL:            appURL,
		ResendAPIKey:      resendAPIKey,
		JWTSecret:         jwtSecret,
		RedisURL:          redisURL,
		R2AccountID:       r2AccountID,
		R2AccessKeyID:     r2AccessKeyID,
		R2SecretAccessKey: r2SecretAccessKey,
		R2Bucket:          r2Bucket,
		GroqAPIKey:        groqAPIKey,
	}, nil
}

// Addr returns the network address string suitable for http.Server.Addr.
func (c *Config) Addr() string {
	return ":" + c.Port
}

// requireEnv reads a required environment variable and returns an error when
// it is absent or empty.
func requireEnv(key string) (string, error) {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return "", fmt.Errorf("%s is required but not set", key)
	}
	return val, nil
}

// loadPort reads the PORT environment variable. It defaults to "8080" when
// absent. It returns an error when PORT is present but not a valid port number.
func loadPort() (string, error) {
	raw, ok := os.LookupEnv("PORT")
	if !ok || raw == "" {
		return "8080", nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("PORT %q is not a valid port number (1-65535)", raw)
	}

	return raw, nil
}
