package auth

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/db"
)

// jwtExpiry is how long an issued JWT is valid.
const jwtExpiry = 7 * 24 * time.Hour

// verifyRequest is the JSON body for POST /v1/auth/verify.
type verifyRequest struct {
	Token string `json:"token" binding:"required"`
}

// verifyResponse is the JSON payload returned on a successful token exchange.
type verifyResponse struct {
	Token string      `json:"token"`
	User  userProfile `json:"user"`
}

type userProfile struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	CompanyID string `json:"company_id"`
}

// VerifyHandler returns a Gin handler for POST /v1/auth/verify.
//
//	200 {"token":"<jwt>","user":{...}}
//	400 {"error":"<validation message>"}
//	401 {"error":"Invalid or expired token"}
//	500 {"error":"Internal server error"}
func VerifyHandler(pool *pgxpool.Pool, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req verifyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx := c.Request.Context()
		tokenHash := sha256Hex(req.Token)

		// Run UseAuthToken and GetUserByID inside a single transaction so that
		// if GetUserByID fails, the token is not permanently consumed and the
		// user can retry with the same magic link.
		tx, err := pool.Begin(ctx)
		if err != nil {
			log.Printf("verify: begin tx: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck // intentional best-effort rollback

		q := db.New(tx)

		// UseAuthToken atomically validates and marks the token as used.
		// Returns pgx.ErrNoRows if not found, expired, or already used.
		authToken, err := q.UseAuthToken(ctx, tokenHash)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			return
		}

		user, err := q.GetUserByID(ctx, authToken.UserID)
		if err != nil {
			log.Printf("verify: get user by id: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		if err := tx.Commit(ctx); err != nil {
			log.Printf("verify: commit tx: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		jwtToken, err := issueJWT(user, jwtSecret)
		if err != nil {
			log.Printf("verify: issue jwt: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		c.JSON(http.StatusOK, verifyResponse{
			Token: jwtToken,
			User: userProfile{
				ID:        user.ID.String(),
				Email:     user.Email,
				Name:      user.Name,
				Role:      user.Role,
				CompanyID: user.CompanyID.String(),
			},
		})
	}
}

// issueJWT signs and returns a JWT for the given user using HS256.
// Claims: sub (user UUID), company_id (UUID), role, iat, exp.
func issueJWT(user db.User, secret string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":        user.ID.String(),
		"company_id": user.CompanyID.String(),
		"role":       user.Role,
		"iat":        now.Unix(),
		"exp":        now.Add(jwtExpiry).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(secret))
}
