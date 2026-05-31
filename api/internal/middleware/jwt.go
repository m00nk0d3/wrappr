package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	contextKeyUserID    = "userID"
	contextKeyCompanyID = "companyID"
	contextKeyRole      = "role"
)

// JWT returns a Gin middleware that validates a Bearer JWT from the
// Authorization header and injects userID, companyID, and role into the
// Gin context. Returns 401 if the token is missing, malformed, or expired.
func JWT(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header missing or malformed"})
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")

		tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err != nil || !tok.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			return
		}

		claims, ok := tok.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			return
		}

		userID, _ := claims["sub"].(string)
		companyID, _ := claims["company_id"].(string)
		role, _ := claims["role"].(string)

		c.Set(contextKeyUserID, userID)
		c.Set(contextKeyCompanyID, companyID)
		c.Set(contextKeyRole, role)

		c.Next()
	}
}

// GetUserID returns the authenticated user's ID from the Gin context.
// Must be called on routes protected by the JWT middleware.
func GetUserID(c *gin.Context) string {
	v, _ := c.Get(contextKeyUserID)
	s, _ := v.(string)
	return s
}

// GetCompanyID returns the authenticated user's company ID from the Gin
// context. Must be called on routes protected by the JWT middleware.
func GetCompanyID(c *gin.Context) string {
	v, _ := c.Get(contextKeyCompanyID)
	s, _ := v.(string)
	return s
}

// GetRole returns the authenticated user's role from the Gin context.
// Must be called on routes protected by the JWT middleware.
func GetRole(c *gin.Context) string {
	v, _ := c.Get(contextKeyRole)
	s, _ := v.(string)
	return s
}

// RequireOwner returns a Gin middleware that aborts with 403 if the
// authenticated user's role is not "owner". Must be placed after JWT.
func RequireOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		if GetRole(c) != "owner" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Owner access required"})
			return
		}
		c.Next()
	}
}
