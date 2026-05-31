package auth

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/db"
)

// acceptInviteRequest is the JSON body for POST /v1/auth/accept-invite.
type acceptInviteRequest struct {
	Token string `json:"token" binding:"required"`
	Name  string `json:"name"  binding:"required"`
}

// AcceptInviteHandler returns a Gin handler for POST /v1/auth/accept-invite.
// This is a public endpoint — no JWT required.
//
//	200 {"token":"<jwt>","user":{...}}
//	400 {"error":"<validation message>"}
//	401 {"error":"Invalid or expired invitation"}
//	409 {"error":"An account with this email already exists in this company"}
//	500 {"error":"Internal server error"}
func AcceptInviteHandler(pool *pgxpool.Pool, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req acceptInviteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx := c.Request.Context()

		tx, err := pool.Begin(ctx)
		if err != nil {
			log.Printf("accept-invite: begin tx: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck

		q := db.New(tx)

		// AcceptInvitation atomically marks the invitation as accepted.
		// Returns pgx.ErrNoRows if the token is not found, already accepted, or expired.
		invitation, err := q.AcceptInvitation(ctx, req.Token)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired invitation"})
			} else {
				log.Printf("accept-invite: accept invitation: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			}
			return
		}

		user, err := q.CreateUser(ctx, db.CreateUserParams{
			CompanyID: invitation.CompanyID,
			Email:     invitation.Email,
			Name:      req.Name,
			Role:      invitation.Role,
		})
		if err != nil {
			if isUniqueViolation(err) {
				c.JSON(http.StatusConflict, gin.H{"error": "An account with this email already exists in this company"})
			} else {
				log.Printf("accept-invite: create user: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			}
			return
		}

		if err := tx.Commit(ctx); err != nil {
			log.Printf("accept-invite: commit tx: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		jwtToken, err := issueJWT(user, jwtSecret)
		if err != nil {
			log.Printf("accept-invite: issue jwt: %v", err)
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
