package config

import (
	"testing"
)

// setRequiredEnvs sets all required environment variables so that
// tests focused on specific behaviour are not blocked by missing vars.
func setRequiredEnvs(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/test")
	t.Setenv("APP_URL", "http://localhost:3001")
	t.Setenv("RESEND_API_KEY", "re_test_key")
	t.Setenv("JWT_SECRET", "test-jwt-secret-32-chars-minimum!!")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("R2_ACCOUNT_ID", "test-account-id")
	t.Setenv("R2_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("R2_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("R2_BUCKET", "test-bucket")
}

func TestLoad_DefaultPort(t *testing.T) {
	setRequiredEnvs(t)
	t.Setenv("PORT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("expected default port %q, got %q", "8080", cfg.Port)
	}
	if cfg.Addr() != ":8080" {
		t.Errorf("expected addr %q, got %q", ":8080", cfg.Addr())
	}
}

func TestLoad_CustomPort(t *testing.T) {
	setRequiredEnvs(t)
	t.Setenv("PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("expected port %q, got %q", "9090", cfg.Port)
	}
	if cfg.Addr() != ":9090" {
		t.Errorf("expected addr %q, got %q", ":9090", cfg.Addr())
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	cases := []struct {
		name string
		port string
	}{
		{"not a number", "abc"},
		{"zero", "0"},
		{"above max", "65536"},
		{"negative", "-1"},
		{"empty with spaces", "  "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnvs(t)
			t.Setenv("PORT", tc.port)

			_, err := Load()
			if err == nil {
				t.Errorf("expected error for PORT=%q, got nil", tc.port)
			}
		})
	}
}

func TestLoad_RequiredVars(t *testing.T) {
	cases := []struct {
		name  string
		unset string
	}{
		{"missing DATABASE_URL", "DATABASE_URL"},
		{"missing APP_URL", "APP_URL"},
		{"missing RESEND_API_KEY", "RESEND_API_KEY"},
		{"missing JWT_SECRET", "JWT_SECRET"},
		{"missing REDIS_URL", "REDIS_URL"},
		{"missing R2_ACCOUNT_ID", "R2_ACCOUNT_ID"},
		{"missing R2_ACCESS_KEY_ID", "R2_ACCESS_KEY_ID"},
		{"missing R2_SECRET_ACCESS_KEY", "R2_SECRET_ACCESS_KEY"},
		{"missing R2_BUCKET", "R2_BUCKET"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnvs(t)
			t.Setenv(tc.unset, "") // override to empty

			_, err := Load()
			if err == nil {
				t.Errorf("expected error when %s is unset, got nil", tc.unset)
			}
		})
	}
}

func TestLoad_GroqAPIKeyOptional(t *testing.T) {
	setRequiredEnvs(t)
	// GROQ_API_KEY is intentionally not set here.

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() should not fail when GROQ_API_KEY is absent, got: %v", err)
	}
	if cfg.GroqAPIKey != "" {
		t.Errorf("expected empty GroqAPIKey, got %q", cfg.GroqAPIKey)
	}
}

func TestLoad_ShortJWTSecret(t *testing.T) {
	setRequiredEnvs(t)
	t.Setenv("JWT_SECRET", "tooshort") // < 32 chars

	_, err := Load()
	if err == nil {
		t.Error("expected error for JWT_SECRET shorter than 32 chars, got nil")
	}
}

func TestLoad_WorkerConcurrencyDefault(t *testing.T) {
	setRequiredEnvs(t)
	t.Setenv("WORKER_CONCURRENCY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WorkerConcurrency != 5 {
		t.Errorf("want default concurrency 5, got %d", cfg.WorkerConcurrency)
	}
}

func TestLoad_WorkerConcurrencyCustom(t *testing.T) {
	setRequiredEnvs(t)
	t.Setenv("WORKER_CONCURRENCY", "20")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WorkerConcurrency != 20 {
		t.Errorf("want concurrency 20, got %d", cfg.WorkerConcurrency)
	}
}

func TestLoad_WorkerConcurrencyInvalid(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"not a number", "abc"},
		{"zero", "0"},
		{"negative", "-1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnvs(t)
			t.Setenv("WORKER_CONCURRENCY", tc.value)

			_, err := Load()
			if err == nil {
				t.Errorf("expected error for WORKER_CONCURRENCY=%q, got nil", tc.value)
			}
		})
	}
}

func TestLoad_AllVarsSet(t *testing.T) {
	setRequiredEnvs(t)
	t.Setenv("PORT", "4000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/test" {
		t.Errorf("unexpected DatabaseURL: %q", cfg.DatabaseURL)
	}
	if cfg.AppURL != "http://localhost:3001" {
		t.Errorf("unexpected AppURL: %q", cfg.AppURL)
	}
	if cfg.ResendAPIKey != "re_test_key" {
		t.Errorf("unexpected ResendAPIKey: %q", cfg.ResendAPIKey)
	}
	if cfg.JWTSecret != "test-jwt-secret-32-chars-minimum!!" {
		t.Errorf("unexpected JWTSecret: %q", cfg.JWTSecret)
	}
}
