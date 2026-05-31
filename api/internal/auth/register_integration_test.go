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
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/db"
)

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// cleanupEmail removes any test rows created during a test run so the DB stays
// clean between runs. Errors are logged but not fatal — the next run may still
// work if constraints allow it.
func cleanupEmail(t *testing.T, pool *pgxpool.Pool, email string) {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	u, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		return // already gone
	}
	pool.Exec(ctx, `DELETE FROM invitations WHERE company_id = $1`, u.CompanyID)
	pool.Exec(ctx, `DELETE FROM users WHERE company_id = $1`, u.CompanyID)
	pool.Exec(ctx, `DELETE FROM companies WHERE id = $1`, u.CompanyID)
}

func newIntegrationRouter(pool *pgxpool.Pool, m *mockMailer) *gin.Engine {
	r := gin.New()
	r.POST("/v1/auth/register", RegisterHandler(pool, m, "http://localhost:3001"))
	return r
}

func doRegisterRaw(t *testing.T, router *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

// TestIntegration_NewRegistration exercises the full happy path against a real DB.
func TestIntegration_NewRegistration(t *testing.T) {
	pool := integrationPool(t)
	email := fmt.Sprintf("integration-new-%d@example.com", randomInt())
	t.Cleanup(func() { cleanupEmail(t, pool, email) })

	m := &mockMailer{}
	router := newIntegrationRouter(pool, m)

	w := doRegisterRaw(t, router, map[string]string{
		"company_name": "Integration Test Co",
		"owner_name":   "Test Owner",
		"email":        email,
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", w.Code, w.Body.String())
	}

	// Magic link should have been sent.
	if s := m.sent(); len(s) != 1 || s[0] != email {
		t.Errorf("expected magic link sent to %q, got sentTo=%v", email, s)
	}

	// User and company should exist in DB.
	q := db.New(pool)
	user, err := q.GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if user.Role != roleOwner {
		t.Errorf("user.Role = %q, want %q", user.Role, roleOwner)
	}
}

// TestIntegration_ExistingEmailSilentResend verifies that a second registration
// attempt with the same email returns 201 and issues a new magic link.
func TestIntegration_ExistingEmailSilentResend(t *testing.T) {
	pool := integrationPool(t)
	email := fmt.Sprintf("integration-resend-%d@example.com", randomInt())
	t.Cleanup(func() { cleanupEmail(t, pool, email) })

	m := &mockMailer{}
	router := newIntegrationRouter(pool, m)

	payload := map[string]string{
		"company_name": "Resend Test Co",
		"owner_name":   "Resend Owner",
		"email":        email,
	}

	// First registration.
	w1 := doRegisterRaw(t, router, payload)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first registration: status = %d, want 201; body = %s", w1.Code, w1.Body.String())
	}

	// Second registration with same email — should silently re-issue magic link.
	w2 := doRegisterRaw(t, router, payload)
	if w2.Code != http.StatusCreated {
		t.Fatalf("second registration: status = %d, want 201; body = %s", w2.Code, w2.Body.String())
	}

	// Both calls should have triggered an email send.
	if s := m.sent(); len(s) != 2 {
		t.Errorf("expected 2 magic links sent, got %d", len(s))
	}
}

// TestIntegration_EmailSendFailureNonFatal checks that a mailer error does not
// cause the registration to fail — the transaction has already committed.
func TestIntegration_EmailSendFailureNonFatal(t *testing.T) {
	pool := integrationPool(t)
	email := fmt.Sprintf("integration-mailfail-%d@example.com", randomInt())
	t.Cleanup(func() { cleanupEmail(t, pool, email) })

	m := &mockMailer{sendErr: fmt.Errorf("smtp timeout")}
	router := newIntegrationRouter(pool, m)

	w := doRegisterRaw(t, router, map[string]string{
		"company_name": "Mail Fail Co",
		"owner_name":   "Fail Owner",
		"email":        email,
	})

	// Should still return 201 even though email sending failed.
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", w.Code, w.Body.String())
	}

	// User should still be in the DB (transaction was committed before email send).
	q := db.New(pool)
	if _, err := q.GetUserByEmail(context.Background(), email); err != nil {
		t.Errorf("user not found after email-failure registration: %v", err)
	}
}

// randomInt returns a pseudo-random int for unique test email addresses.
// Uses generateToken internally so we don't pull in a new dependency.
func randomInt() int64 {
	tok, _ := generateToken()
	var n int64
	for i := 0; i < 8; i++ {
		n = n*256 + int64(tok[i])
	}
	if n < 0 {
		n = -n
	}
	return n
}
