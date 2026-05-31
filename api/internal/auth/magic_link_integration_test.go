//go:build integration

// Run integration tests with:
//
//	TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/auth/...
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/db"
)

// cleanupUserAndTokens deletes a test user, their auth tokens, invitations,
// and company so the DB stays clean between runs.
func cleanupUserAndTokens(t *testing.T, pool *pgxpool.Pool, email string) {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	u, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		return // already gone
	}
	pool.Exec(ctx, `DELETE FROM auth_tokens WHERE user_id = $1`, u.ID)
	pool.Exec(ctx, `DELETE FROM invitations WHERE company_id = $1`, u.CompanyID)
	pool.Exec(ctx, `DELETE FROM users WHERE company_id = $1`, u.CompanyID)
	pool.Exec(ctx, `DELETE FROM companies WHERE id = $1`, u.CompanyID)
}

func newMagicLinkIntegrationRouter(pool *pgxpool.Pool, m *mockMailer) *gin.Engine {
	r := gin.New()
	r.POST("/v1/auth/magic-link", MagicLinkHandler(pool, m, "http://localhost:3001"))
	return r
}

func newVerifyIntegrationRouter(pool *pgxpool.Pool, secret string) *gin.Engine {
	r := gin.New()
	r.POST("/v1/auth/verify", VerifyHandler(pool, secret))
	return r
}

func doPost(t *testing.T, router *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

// registerTestUser creates a company+user via the register endpoint and returns
// the user from the DB.
func registerTestUser(t *testing.T, pool *pgxpool.Pool, email string) db.User {
	t.Helper()
	m := &mockMailer{}
	r := gin.New()
	r.POST("/v1/auth/register", RegisterHandler(pool, m, "http://localhost:3001"))
	w := doPost(t, r, "/v1/auth/register", map[string]string{
		"company_name": fmt.Sprintf("Test Co %d", randomInt()),
		"owner_name":   "Test User",
		"email":        email,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("registerTestUser: status = %d, want 201; body = %s", w.Code, w.Body.String())
	}
	q := db.New(pool)
	u, err := q.GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("registerTestUser: GetUserByEmail: %v", err)
	}
	return u
}

// TestIntegration_MagicLink_HappyPath verifies that a valid registered email
// triggers token storage in the DB and an email send via the mailer.
func TestIntegration_MagicLink_HappyPath(t *testing.T) {
	pool := integrationPool(t)
	email := fmt.Sprintf("magiclink-happy-%d@example.com", randomInt())
	t.Cleanup(func() { cleanupUserAndTokens(t, pool, email) })

	_ = registerTestUser(t, pool, email)

	m := &mockMailer{}
	router := newMagicLinkIntegrationRouter(pool, m)

	w := doPost(t, router, "/v1/auth/magic-link", map[string]string{"email": email})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	// The response body should always be the same regardless of whether the
	// email is registered (enumeration protection).
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["message"] == "" {
		t.Error("expected non-empty 'message' field in response")
	}

	// Wait briefly for the background goroutine to complete.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := m.sent(); len(s) > 0 {
			if s[0] != email {
				t.Errorf("sent to %q, want %q", s[0], email)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if s := m.sent(); len(s) == 0 {
		t.Error("timed out waiting for magic-link email to be sent")
	}

	// Verify that an auth_token row was created in the DB.
	q := db.New(pool)
	u, err := q.GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	var count int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM auth_tokens WHERE user_id = $1`, u.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count auth_tokens: %v", err)
	}
	if count == 0 {
		t.Error("expected at least one auth_token row for the user, got 0")
	}
}

// TestIntegration_MagicLink_UnknownEmail verifies that a request for an
// unregistered email still returns 200 (enumeration protection) and does not
// send any email.
func TestIntegration_MagicLink_UnknownEmail(t *testing.T) {
	pool := integrationPool(t)

	m := &mockMailer{}
	router := newMagicLinkIntegrationRouter(pool, m)

	w := doPost(t, router, "/v1/auth/magic-link", map[string]string{
		"email": fmt.Sprintf("nobody-%d@example.com", randomInt()),
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	// Give the goroutine time to finish (it should be a no-op for unknown email).
	time.Sleep(200 * time.Millisecond)
	if s := m.sent(); len(s) != 0 {
		t.Errorf("expected no emails sent for unknown address, got %v", s)
	}
}

const testJWTSecret = "integration-test-jwt-secret-32ch!!"

// TestIntegration_Verify_InvalidToken ensures POST /auth/verify returns 401
// for a token that was never issued.
func TestIntegration_Verify_InvalidToken(t *testing.T) {
	pool := integrationPool(t)
	router := newVerifyIntegrationRouter(pool, testJWTSecret)

	w := doPost(t, router, "/v1/auth/verify", map[string]string{
		"token": "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body = %s", w.Code, w.Body.String())
	}
}

// TestIntegration_Verify_ExpiredToken ensures POST /auth/verify returns 401
// for a token whose expires_at is in the past.
func TestIntegration_Verify_ExpiredToken(t *testing.T) {
	pool := integrationPool(t)
	email := fmt.Sprintf("verify-expired-%d@example.com", randomInt())
	t.Cleanup(func() { cleanupUserAndTokens(t, pool, email) })

	u := registerTestUser(t, pool, email)

	rawToken, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	tokenHash := sha256Hex(rawToken)

	q := db.New(pool)
	_, err = q.CreateAuthToken(context.Background(), db.CreateAuthTokenParams{
		UserID:    u.ID,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Minute), Valid: true}, // already expired
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	router := newVerifyIntegrationRouter(pool, testJWTSecret)
	w := doPost(t, router, "/v1/auth/verify", map[string]string{"token": rawToken})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body = %s", w.Code, w.Body.String())
	}
}

// TestIntegration_Verify_HappyPath exercises the full verify flow:
// a valid, unexpired, unused token is exchanged for a JWT.
func TestIntegration_Verify_HappyPath(t *testing.T) {
	pool := integrationPool(t)
	email := fmt.Sprintf("verify-happy-%d@example.com", randomInt())
	t.Cleanup(func() { cleanupUserAndTokens(t, pool, email) })

	u := registerTestUser(t, pool, email)

	rawToken, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	tokenHash := sha256Hex(rawToken)

	q := db.New(pool)
	_, err = q.CreateAuthToken(context.Background(), db.CreateAuthTokenParams{
		UserID:    u.ID,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(15 * time.Minute), Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	router := newVerifyIntegrationRouter(pool, testJWTSecret)
	w := doPost(t, router, "/v1/auth/verify", map[string]string{"token": rawToken})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	var resp verifyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected non-empty JWT token in response")
	}
	if resp.User.Email != email {
		t.Errorf("user.email = %q, want %q", resp.User.Email, email)
	}
	if resp.User.ID == "" {
		t.Error("expected non-empty user.id in response")
	}
}

// TestIntegration_Verify_AlreadyUsedToken ensures a token cannot be reused.
func TestIntegration_Verify_AlreadyUsedToken(t *testing.T) {
	pool := integrationPool(t)
	email := fmt.Sprintf("verify-reuse-%d@example.com", randomInt())
	t.Cleanup(func() { cleanupUserAndTokens(t, pool, email) })

	u := registerTestUser(t, pool, email)

	rawToken, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	tokenHash := sha256Hex(rawToken)

	q := db.New(pool)
	_, err = q.CreateAuthToken(context.Background(), db.CreateAuthTokenParams{
		UserID:    u.ID,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(15 * time.Minute), Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	router := newVerifyIntegrationRouter(pool, testJWTSecret)
	body := map[string]string{"token": rawToken}

	// First use — should succeed.
	w1 := doPost(t, router, "/v1/auth/verify", body)
	if w1.Code != http.StatusOK {
		t.Fatalf("first verify: status = %d, want 200; body = %s", w1.Code, w1.Body.String())
	}

	// Second use — token already consumed, must be rejected.
	w2 := doPost(t, router, "/v1/auth/verify", body)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("second verify: status = %d, want 401; body = %s", w2.Code, w2.Body.String())
	}
}
