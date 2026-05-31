package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/db"
	"github.com/m00nk0d3/wrappr/api/internal/mailer"
	"github.com/m00nk0d3/wrappr/api/internal/middleware"
)

// inviteRequest is the JSON body for POST /v1/team/invite.
type inviteRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// inviteTTL is how long a technician invitation stays valid.
const inviteTTL = 7 * 24 * time.Hour

// InviteHandler returns a Gin handler for POST /v1/team/invite.
// Requires JWT authentication and the owner role (enforced by middleware).
//
//	201 {"message":"Invitation sent"}
//	400 {"error":"<validation message>"}
//	409 {"error":"A pending invitation already exists for this email"}
//	500 {"error":"Internal server error"}
func InviteHandler(pool *pgxpool.Pool, m mailer.Mailer, appURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req inviteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		rawCompanyID := middleware.GetCompanyID(c)
		companyUUID, err := parseUUID(rawCompanyID)
		if err != nil {
			log.Printf("invite: parse company_id from JWT: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		ctx := c.Request.Context()
		q := db.New(pool)

		// Reject if a pending (non-expired, non-accepted) invitation already exists
		// for this email+company combination.
		_, err = q.GetPendingInvitationByEmailAndCompany(ctx, db.GetPendingInvitationByEmailAndCompanyParams{
			CompanyID: companyUUID,
			Email:     req.Email,
		})
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "A pending invitation already exists for this email"})
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Printf("invite: check pending invitation: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		token, err := generateToken()
		if err != nil {
			log.Printf("invite: generate token: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		_, err = q.CreateInvitation(ctx, db.CreateInvitationParams{
			CompanyID: companyUUID,
			Email:     req.Email,
			Role:      "technician",
			Token:     token,
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(inviteTTL), Valid: true},
		})
		if err != nil {
			log.Printf("invite: create invitation: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"message": "Invitation sent"})

		// Send the invitation email asynchronously after responding so the
		// client is not blocked by email delivery latency.
		bgCtx := context.WithoutCancel(ctx)
		go func() {
			inviteURL := fmt.Sprintf("%s/auth/accept-invite?token=%s", strings.TrimRight(appURL, "/"), token)
			if err := m.SendInvitation(bgCtx, req.Email, inviteURL); err != nil {
				log.Printf("invite: send invitation email: %v", err)
			}
		}()
	}
}
