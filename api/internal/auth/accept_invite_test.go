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

func newAcceptInviteRouter(jwtSecret string) *gin.Engine {
	r := gin.New()
	// nil pool — safe for validation-failure paths that never hit the DB.
	r.POST("/v1/auth/accept-invite", AcceptInviteHandler(nil, jwtSecret))
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
