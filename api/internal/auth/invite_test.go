package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/m00nk0d3/wrappr/api/internal/middleware"
)

// newInviteRouter returns a minimal Gin router with a pre-authenticated JWT
// so InviteHandler's validation path is reachable without a real DB.
func newInviteRouter(t *testing.T, m *mockMailer) *gin.Engine {
	t.Helper()
	r := gin.New()

	// Inject a fake owner JWT into every request so the JWT middleware passes.
	r.Use(fakeOwnerJWT())

	// nil pool — safe for validation-failure paths that never touch the DB.
	r.POST("/v1/team/invite", InviteHandler(nil, m, "http://localhost:3001"))
	return r
}

// fakeOwnerJWT injects hardcoded JWT claims into the Gin context, simulating
// the middleware.JWT + middleware.RequireOwner chain without a real token.
func fakeOwnerJWT() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("companyID", "fedcba98-7654-3210-fedc-ba9876543210")
		c.Set("userID", "01234567-89ab-cdef-0123-456789abcdef")
		c.Set("role", "owner")
		c.Next()
	}
}

// fakeJWT signs and returns a JWT for the given user claims using HS256 and
// the provided secret. Used in tests that verify JWT output.
func fakeJWT(t *testing.T, secret, sub, companyID, role string) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":        sub,
		"company_id": companyID,
		"role":       role,
		"iat":        now.Unix(),
		"exp":        now.Add(7 * 24 * time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return signed
}

func doInvite(t *testing.T, router *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/team/invite", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func TestInviteHandler_ValidationErrors(t *testing.T) {
	m := &mockMailer{}
	router := newInviteRouter(t, m)

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
			w := doInvite(t, router, tc.body)
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

func TestInviteHandler_NonJSONBody(t *testing.T) {
	m := &mockMailer{}
	router := newInviteRouter(t, m)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/team/invite", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestRequireOwner_Rejects403 verifies that a non-owner JWT is blocked.
func TestRequireOwner_Rejects403(t *testing.T) {
	const jwtSecret = "test-secret"

	r := gin.New()
	r.Use(middleware.JWT(jwtSecret))
	r.POST("/v1/team/invite", middleware.RequireOwner(), InviteHandler(nil, &mockMailer{}, "http://localhost:3001"))

	// Build a JWT with role=technician (not owner).
	techUser := fakeUser()
	tok, err := issueJWT(techUser, jwtSecret)
	if err != nil {
		t.Fatalf("issue JWT: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"email": "new@example.com"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/team/invite", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
}

// TestParseUUID ensures parseUUID works with valid and invalid strings.
func TestParseUUID(t *testing.T) {
	validUUID := "01234567-89ab-cdef-0123-456789abcdef"
	u, err := parseUUID(validUUID)
	if err != nil {
		t.Fatalf("parseUUID(%q) unexpected error: %v", validUUID, err)
	}
	if !u.Valid {
		t.Errorf("parseUUID(%q) Valid = false, want true", validUUID)
	}

	// Verify round-trip.
	got := pgtype.UUID(u).String()
	if got != validUUID {
		t.Errorf("round-trip UUID = %q, want %q", got, validUUID)
	}

	_, err = parseUUID("not-a-uuid")
	if err == nil {
		t.Error("parseUUID(\"not-a-uuid\") expected error, got nil")
	}
}
