package config

import (
	"testing"
)

func TestLoad_DefaultPort(t *testing.T) {
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
			t.Setenv("PORT", tc.port)

			_, err := Load()
			if err == nil {
				t.Errorf("expected error for PORT=%q, got nil", tc.port)
			}
		})
	}
}
