package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newMagicLinkRouter(m *mockMailer) *gin.Engine {
	r := gin.New()
	// nil pool — safe for validation-failure paths that never touch the DB.
	r.POST("/v1/auth/magic-link", MagicLinkHandler(nil, m, "http://localhost:3001"))
	return r
}

func doMagicLink(t *testing.T, router *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/magic-link", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func TestMagicLinkHandler_ValidationErrors(t *testing.T) {
	m := &mockMailer{}
	router := newMagicLinkRouter(m)

	cases := []struct {
		name       string
		body       any
		wantStatus int
	}{
		{
			name:       "empty body",
			body:       map[string]string{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing email",
			body:       map[string]string{"foo": "bar"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid email format",
			body:       map[string]string{"email": "not-an-email"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "email missing domain",
			body:       map[string]string{"email": "user@"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doMagicLink(t, router, tc.body)
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tc.wantStatus, w.Body.String())
			}
			ct := w.Header().Get("Content-Type")
			if !strings.Contains(ct, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var resp map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if resp["error"] == "" {
				t.Errorf("expected non-empty 'error' field in response")
			}
		})
	}
}

func TestMagicLinkHandler_NonJSONBody(t *testing.T) {
	m := &mockMailer{}
	router := newMagicLinkRouter(m)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/magic-link", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestSha256Hex verifies sha256Hex produces a consistent 64-char hex string.
func TestSha256Hex(t *testing.T) {
	input := "hello"
	got := sha256Hex(input)
	// SHA-256("hello") is well-known:
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Errorf("sha256Hex(%q) = %q, want %q", input, got, want)
	}

	// Must be 64 hex chars.
	if len(got) != 64 {
		t.Errorf("sha256Hex result len = %d, want 64", len(got))
	}

	// Must be deterministic.
	if sha256Hex(input) != got {
		t.Error("sha256Hex is not deterministic")
	}
}
