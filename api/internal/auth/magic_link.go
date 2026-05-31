package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/db"
	"github.com/m00nk0d3/wrappr/api/internal/mailer"
)

// magicLinkRequest is the JSON body for POST /v1/auth/magic-link.
type magicLinkRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// magicLinkTTL is how long a magic-link token stays valid.
const magicLinkTTL = 15 * time.Minute

// MagicLinkHandler returns a Gin handler for POST /v1/auth/magic-link.
//
// Always returns 200 immediately — before any DB lookup or email send — to
// prevent both email enumeration (same body) and timing side-channel attacks
// (same response latency regardless of whether the email is registered).
// The token generation, storage, and send happen in a background goroutine.
//
//	200 {"message":"If this email is registered, a login link has been sent."}
//	400 {"error":"<validation message>"}
func MagicLinkHandler(pool *pgxpool.Pool, m mailer.Mailer, appURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req magicLinkRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Respond 200 immediately so response latency is identical whether the
		// email exists or not, closing the timing side-channel.
		c.JSON(http.StatusOK, gin.H{"message": "If this email is registered, a login link has been sent."})

		// Use a detached context so the goroutine is not cancelled when the
		// HTTP request context ends after the handler returns.
		bgCtx := context.WithoutCancel(c.Request.Context())

		go func() {
			q := db.New(pool)

			user, err := q.GetUserByEmail(bgCtx, req.Email)
			if err != nil {
				// User not found — nothing to do.
				return
			}

			rawToken, err := generateToken()
			if err != nil {
				log.Printf("magic-link: generate token: %v", err)
				return
			}

			tokenHash := sha256Hex(rawToken)

			_, err = q.CreateAuthToken(bgCtx, db.CreateAuthTokenParams{
				UserID:    user.ID,
				TokenHash: tokenHash,
				ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(magicLinkTTL), Valid: true},
			})
			if err != nil {
				log.Printf("magic-link: store token: %v", err)
				return
			}

			magicURL := fmt.Sprintf("%s/auth/verify?token=%s", strings.TrimRight(appURL, "/"), rawToken)
			if err := m.SendMagicLink(bgCtx, user.Email, user.Name, magicURL); err != nil {
				// Non-fatal — token is stored and the user can retry.
				log.Printf("magic-link: send email: %v", err)
			}
		}()
	}
}

// sha256Hex returns the lowercase hex-encoded SHA-256 digest of s.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
