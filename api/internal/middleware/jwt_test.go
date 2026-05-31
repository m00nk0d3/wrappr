package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-jwt-secret"

// makeToken builds a signed JWT with the given claims and expiry.
func makeToken(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("makeToken: sign: %v", err)
	}
	return s
}

// validClaims returns standard valid claims for a technician.
func validClaims(role string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"sub":        "user-uuid-1234",
		"company_id": "company-uuid-5678",
		"role":       role,
		"iat":        now.Unix(),
		"exp":        now.Add(7 * 24 * time.Hour).Unix(),
	}
}

// newJWTRouter builds a minimal Gin router with the JWT middleware and a
// protected /protected endpoint that echoes the injected context values.
func newJWTRouter(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	protected := r.Group("/protected")
	protected.Use(JWT(secret))
	protected.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"userID":    GetUserID(c),
			"companyID": GetCompanyID(c),
			"role":      GetRole(c),
		})
	})
	return r
}

// newOwnerRouter adds a RequireOwner-gated route on top of JWT.
func newOwnerRouter(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	owner := r.Group("/owner")
	owner.Use(JWT(secret))
	owner.Use(RequireOwner())
	owner.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func doGet(router *gin.Engine, path, authHeader string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	router.ServeHTTP(w, req)
	return w
}

// --- JWT middleware tests ---

func TestJWT_MissingHeader(t *testing.T) {
	w := doGet(newJWTRouter(testSecret), "/protected", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestJWT_NoBearerPrefix(t *testing.T) {
	w := doGet(newJWTRouter(testSecret), "/protected", "Token abc123")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestJWT_MalformedToken(t *testing.T) {
	w := doGet(newJWTRouter(testSecret), "/protected", "Bearer not.a.jwt")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestJWT_ExpiredToken(t *testing.T) {
	claims := jwt.MapClaims{
		"sub":        "user-uuid",
		"company_id": "company-uuid",
		"role":       "technician",
		"iat":        time.Now().Add(-2 * time.Hour).Unix(),
		"exp":        time.Now().Add(-1 * time.Hour).Unix(), // already expired
	}
	token := makeToken(t, testSecret, claims)
	w := doGet(newJWTRouter(testSecret), "/protected", "Bearer "+token)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestJWT_WrongSecret(t *testing.T) {
	token := makeToken(t, "wrong-secret", validClaims("technician"))
	w := doGet(newJWTRouter(testSecret), "/protected", "Bearer "+token)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestJWT_ValidToken_InjectsContext(t *testing.T) {
	claims := validClaims("technician")
	token := makeToken(t, testSecret, claims)
	w := doGet(newJWTRouter(testSecret), "/protected", "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "user-uuid-1234") {
		t.Errorf("userID not injected; body = %s", body)
	}
	if !strings.Contains(body, "company-uuid-5678") {
		t.Errorf("companyID not injected; body = %s", body)
	}
	if !strings.Contains(body, "technician") {
		t.Errorf("role not injected; body = %s", body)
	}
}

func TestJWT_MissingSub(t *testing.T) {
	claims := jwt.MapClaims{
		"company_id": "company-uuid-5678",
		"role":       "technician",
		"iat":        time.Now().Unix(),
		"exp":        time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	token := makeToken(t, testSecret, claims)
	w := doGet(newJWTRouter(testSecret), "/protected", "Bearer "+token)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestJWT_MissingCompanyID(t *testing.T) {
	claims := jwt.MapClaims{
		"sub":  "user-uuid-1234",
		"role": "technician",
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	token := makeToken(t, testSecret, claims)
	w := doGet(newJWTRouter(testSecret), "/protected", "Bearer "+token)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// --- RequireOwner tests ---

func TestRequireOwner_TechnicianForbidden(t *testing.T) {
	token := makeToken(t, testSecret, validClaims("technician"))
	w := doGet(newOwnerRouter(testSecret), "/owner", "Bearer "+token)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestRequireOwner_OwnerAllowed(t *testing.T) {
	token := makeToken(t, testSecret, validClaims("owner"))
	w := doGet(newOwnerRouter(testSecret), "/owner", "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
}

