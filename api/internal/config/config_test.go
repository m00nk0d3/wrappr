package config

import (
	"testing"
)

// setRequiredEnvs sets the three new required environment variables so that
// tests focused on PORT behaviour are not blocked by missing vars.
func setRequiredEnvs(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/test")
	t.Setenv("APP_URL", "http://localhost:3001")
	t.Setenv("RESEND_API_KEY", "re_test_key")
	t.Setenv("JWT_SECRET", "test-jwt-secret-32-chars-minimum!!")
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

func TestLoad_ShortJWTSecret(t *testing.T) {
	setRequiredEnvs(t)
	t.Setenv("JWT_SECRET", "tooshort") // < 32 chars

	_, err := Load()
	if err == nil {
		t.Error("expected error for JWT_SECRET shorter than 32 chars, got nil")
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
