package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockMailer implements mailer.Mailer for tests.
// It is goroutine-safe so it can be called from background goroutines
// (e.g. MagicLinkHandler's async token-send path).
type mockMailer struct {
	mu      sync.Mutex
	sentTo  []string
	sendErr error
}

func (m *mockMailer) SendMagicLink(_ context.Context, to, _, _ string) error {
	m.mu.Lock()
	m.sentTo = append(m.sentTo, to)
	m.mu.Unlock()
	return m.sendErr
}

func (m *mockMailer) SendInvitation(_ context.Context, to, _ string) error {
	m.mu.Lock()
	m.sentTo = append(m.sentTo, to)
	m.mu.Unlock()
	return m.sendErr
}

// sent returns a snapshot of all addresses the mock has sent to.
func (m *mockMailer) sent() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.sentTo))
	copy(out, m.sentTo)
	return out
}

// --- slugify ---

func TestSlugify(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Smith Electrical LLC", "smith-electrical-llc"},
		{"  Leading & Trailing  ", "leading-trailing"},
		{"Café Bistro", "caf-bistro"},
		{"ACME", "acme"},
		{"hello---world", "hello-world"},
		{"123 Main St.", "123-main-st"},
	}

	for _, tc := range cases {
		got := slugify(tc.input)
		if got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --- randomSuffix ---

func TestRandomSuffix(t *testing.T) {
	for _, n := range []int{4, 6, 8} {
		s, err := randomSuffix(n)
		if err != nil {
			t.Fatalf("randomSuffix(%d) error: %v", n, err)
		}
		if len(s) != n {
			t.Errorf("randomSuffix(%d) = %q, want len=%d", n, s, n)
		}
		matched, _ := regexp.MatchString(`^[0-9a-f]+$`, s)
		if !matched {
			t.Errorf("randomSuffix(%d) = %q is not lowercase hex", n, s)
		}
	}

	// Two suffixes should be distinct with overwhelming probability.
	a, _ := randomSuffix(4)
	b, _ := randomSuffix(4)
	if a == b {
		t.Error("two consecutive randomSuffix(4) values are identical — RNG may be broken")
	}
}

// --- generateToken ---

func TestGenerateToken(t *testing.T) {
	tok, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken() error: %v", err)
	}
	if len(tok) != 64 {
		t.Errorf("expected 64-char token, got len=%d", len(tok))
	}
	matched, _ := regexp.MatchString(`^[0-9a-f]{64}$`, tok)
	if !matched {
		t.Errorf("token %q is not lowercase hex", tok)
	}

	// Two tokens should be distinct (collision probability is negligible).
	tok2, _ := generateToken()
	if tok == tok2 {
		t.Error("two consecutive tokens are identical — RNG may be broken")
	}
}

// --- RegisterHandler ---
//
// We test the handler in isolation by wiring it into a minimal Gin router.
// The pool-dependent paths require a real DB, so those are skipped here; we
// focus on validation and response shape using a nil pool (unreachable for
// validation-error paths).

func newTestRouter(m *mockMailer) *gin.Engine {
	r := gin.New()
	// nil pool — safe for validation-failure tests that never hit the DB.
	r.POST("/v1/auth/register", RegisterHandler(nil, m, "http://localhost:3001"))
	return r
}

func doRegister(t *testing.T, router *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func TestRegisterHandler_ValidationErrors(t *testing.T) {
	m := &mockMailer{}
	router := newTestRouter(m)

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
			name:       "missing company_name",
			body:       map[string]string{"owner_name": "John", "email": "john@example.com"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing owner_name",
			body:       map[string]string{"company_name": "ACME", "email": "john@example.com"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing email",
			body:       map[string]string{"company_name": "ACME", "owner_name": "John"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid email format",
			body:       map[string]string{"company_name": "ACME", "owner_name": "John", "email": "not-an-email"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "email missing domain",
			body:       map[string]string{"company_name": "ACME", "owner_name": "John", "email": "john@"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doRegister(t, router, tc.body)
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tc.wantStatus, w.Body.String())
			}
			// Response must be JSON with an "error" key.
			ct := w.Header().Get("Content-Type")
			if !strings.Contains(ct, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var resp map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal body: %v", err)
			}
			if resp["error"] == "" {
				t.Errorf("expected non-empty 'error' field in response")
			}
		})
	}
}

func TestRegisterHandler_NonJSONBody(t *testing.T) {
	m := &mockMailer{}
	router := newTestRouter(m)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
