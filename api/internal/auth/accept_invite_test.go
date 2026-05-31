package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/m00nk0d3/wrappr/api/internal/db"
)

// mockAcceptInviteExecutor stubs AcceptInvite for unit tests.
type mockAcceptInviteExecutor struct {
	user db.User
	err  error
}

func (m *mockAcceptInviteExecutor) AcceptInvite(_ context.Context, _, _ string) (db.User, error) {
	return m.user, m.err
}

func newAcceptInviteRouter(jwtSecret string) *gin.Engine {
	r := gin.New()
	// nil pool — safe for validation-failure paths that never reach the executor.
	r.POST("/v1/auth/accept-invite", AcceptInviteHandler(nil, jwtSecret))
	return r
}

func newAcceptInviteRouterWithExec(exec acceptInviteExecutor, jwtSecret string) *gin.Engine {
	r := gin.New()
	r.POST("/v1/auth/accept-invite", acceptInviteHandlerWithExecutor(exec, jwtSecret))
	return r
}

func doAcceptInvite(t *testing.T, router *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/accept-invite", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func TestAcceptInviteHandler_ValidationErrors(t *testing.T) {
	router := newAcceptInviteRouter("test-secret")

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
			name:       "missing token",
			body:       map[string]string{"name": "John Smith"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing name",
			body:       map[string]string{"token": "abc123"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doAcceptInvite(t, router, tc.body)
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

func TestAcceptInviteHandler_NonJSONBody(t *testing.T) {
	router := newAcceptInviteRouter("test-secret")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/accept-invite", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAcceptInviteHandler_InvalidToken(t *testing.T) {
	exec := &mockAcceptInviteExecutor{err: db.ErrNotFound}
	router := newAcceptInviteRouterWithExec(exec, "test-secret")

	w := doAcceptInvite(t, router, map[string]string{"token": "bad-token", "name": "John"})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("expected non-empty 'error' field in response")
	}
}

func TestAcceptInviteHandler_DuplicateUser(t *testing.T) {
	exec := &mockAcceptInviteExecutor{err: db.ErrConflict}
	router := newAcceptInviteRouterWithExec(exec, "test-secret")

	w := doAcceptInvite(t, router, map[string]string{"token": "valid-token", "name": "John"})
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("expected non-empty 'error' field in response")
	}
}

func TestAcceptInviteHandler_Success(t *testing.T) {
	const jwtSecret = "test-secret"
	user := fakeUser()
	exec := &mockAcceptInviteExecutor{user: user}
	router := newAcceptInviteRouterWithExec(exec, jwtSecret)

	w := doAcceptInvite(t, router, map[string]string{"token": "valid-token", "name": user.Name})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	var resp verifyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected non-empty JWT token in response")
	}
	if resp.User.Email != user.Email {
		t.Errorf("user.Email = %q, want %q", resp.User.Email, user.Email)
	}
	if resp.User.Role != user.Role {
		t.Errorf("user.Role = %q, want %q", resp.User.Role, user.Role)
	}
}

