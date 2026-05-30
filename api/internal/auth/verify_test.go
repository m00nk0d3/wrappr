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
)

func newVerifyRouter(jwtSecret string) *gin.Engine {
	r := gin.New()
	// nil pool — safe for validation-failure paths that never touch the DB.
	r.POST("/v1/auth/verify", VerifyHandler(nil, jwtSecret))
	return r
}

func doVerify(t *testing.T, router *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/verify", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func TestVerifyHandler_ValidationErrors(t *testing.T) {
	router := newVerifyRouter("test-secret")

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
			name:       "missing token field",
			body:       map[string]string{"foo": "bar"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doVerify(t, router, tc.body)
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tc.wantStatus, w.Body.String())
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

func TestVerifyHandler_NonJSONBody(t *testing.T) {
	router := newVerifyRouter("test-secret")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/verify", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestIssueJWT verifies that issueJWT returns a parseable, correctly-claimed JWT.
func TestIssueJWT(t *testing.T) {
	user := fakeUser()
	secret := "test-secret-for-unit-tests"

	tokenStr, err := issueJWT(user, secret)
	if err != nil {
		t.Fatalf("issueJWT() error: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("issueJWT() returned empty string")
	}

	// Parse and verify claims.
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil {
		t.Fatalf("jwt.Parse() error: %v", err)
	}
	if !tok.Valid {
		t.Fatal("parsed JWT is not valid")
	}

	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("claims are not MapClaims")
	}

	if claims["sub"] != user.ID.String() {
		t.Errorf("sub = %v, want %v", claims["sub"], user.ID.String())
	}
	if claims["company_id"] != user.CompanyID.String() {
		t.Errorf("company_id = %v, want %v", claims["company_id"], user.CompanyID.String())
	}
	if claims["role"] != user.Role {
		t.Errorf("role = %v, want %v", claims["role"], user.Role)
	}

	// exp should be ~7 days from now.
	exp, ok := claims["exp"].(float64)
	if !ok {
		t.Fatal("exp claim missing or wrong type")
	}
	expTime := time.Unix(int64(exp), 0)
	minExp := time.Now().Add(6 * 24 * time.Hour)
	maxExp := time.Now().Add(8 * 24 * time.Hour)
	if expTime.Before(minExp) || expTime.After(maxExp) {
		t.Errorf("exp = %v, want within [%v, %v]", expTime, minExp, maxExp)
	}
}

// TestIssueJWT_DifferentSecretInvalid ensures a JWT signed with one secret
// cannot be verified with a different secret.
func TestIssueJWT_DifferentSecretInvalid(t *testing.T) {
	user := fakeUser()
	tokenStr, err := issueJWT(user, "correct-secret")
	if err != nil {
		t.Fatalf("issueJWT() error: %v", err)
	}

	_, err = jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		return []byte("wrong-secret"), nil
	})
	if err == nil {
		t.Error("expected error when verifying with wrong secret, got nil")
	}
}
