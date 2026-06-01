package worker

import (
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func TestParseRedisURL(t *testing.T) {
	cases := []struct {
		name      string
		url       string
		wantAddr  string
		wantPw    string
		wantTLS   bool
		wantDB    int
		wantError bool
	}{
		{
			name:     "plain redis",
			url:      "redis://localhost:6379",
			wantAddr: "localhost:6379",
		},
		{
			name:     "rediss TLS",
			url:      "rediss://localhost:6380",
			wantAddr: "localhost:6380",
			wantTLS:  true,
		},
		{
			name:     "with password",
			url:      "redis://:secret@localhost:6379",
			wantAddr: "localhost:6379",
			wantPw:   "secret",
		},
		{
			name:     "rediss with password",
			url:      "rediss://:mypassword@redis.example.com:6380",
			wantAddr: "redis.example.com:6380",
			wantPw:   "mypassword",
			wantTLS:  true,
		},
		{
			name:      "missing host",
			url:       "redis:///",
			wantError: true,
		},
		{
			name:      "invalid URL",
			url:       "://bad",
			wantError: true,
		},
		{
			name:     "with database number",
			url:      "redis://localhost:6379/2",
			wantAddr: "localhost:6379",
			wantDB:   2,
		},
		{
			name:     "default database (no path)",
			url:      "redis://localhost:6379",
			wantAddr: "localhost:6379",
			wantDB:   0,
		},
		{
			name:      "invalid database number",
			url:       "redis://localhost:6379/notanumber",
			wantError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opt, err := ParseRedisURL(tc.url)

			if tc.wantError {
				if err == nil {
					t.Errorf("expected error for URL %q, got nil", tc.url)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if opt.Addr != tc.wantAddr {
				t.Errorf("Addr: want %q, got %q", tc.wantAddr, opt.Addr)
			}
			if opt.Password != tc.wantPw {
				t.Errorf("Password: want %q, got %q", tc.wantPw, opt.Password)
			}

			if opt.DB != tc.wantDB {
				t.Errorf("DB: want %d, got %d", tc.wantDB, opt.DB)
			}

			hasTLS := opt.TLSConfig != nil
			if hasTLS != tc.wantTLS {
				t.Errorf("TLSConfig presence: want %v, got %v", tc.wantTLS, hasTLS)
			}
		})
	}
}

func TestRetryDelay(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 30 * time.Second},
		{2, 2 * time.Minute},
		{3, 10 * time.Minute},
		{4, 10 * time.Minute},
		{99, 10 * time.Minute},
	}

	for _, tc := range cases {
		got := retryDelay(tc.attempt, nil, nil)
		if got != tc.want {
			t.Errorf("retryDelay(%d): want %v, got %v", tc.attempt, tc.want, got)
		}
	}
}

// Compile-time check: ParseRedisURL must return an asynq.RedisClientOpt.
var _ asynq.RedisClientOpt = func() asynq.RedisClientOpt {
	opt, _ := ParseRedisURL("redis://localhost:6379")
	return opt
}()
