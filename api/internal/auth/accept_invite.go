package auth

import (
	"context"
	"errors"
	"fmt"
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

// acceptInviteExecutor handles the transactional DB work for invitation acceptance.
// It is a seam for unit testing — production code uses poolAcceptInviteExecutor.
type acceptInviteExecutor interface {
	AcceptInvite(ctx context.Context, token, name string) (db.User, error)
}

// poolAcceptInviteExecutor is the production executor backed by a pgxpool.
// It runs AcceptInvitation + CreateUser in a single transaction and translates
// known DB sentinel errors to db.ErrNotFound / db.ErrConflict.
type poolAcceptInviteExecutor struct {
	pool *pgxpool.Pool
}

func (e *poolAcceptInviteExecutor) AcceptInvite(ctx context.Context, token, name string) (db.User, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return db.User{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := db.New(tx)

	// AcceptInvitation atomically marks the invitation as accepted.
	// Returns pgx.ErrNoRows if the token is not found, already accepted, or expired.
	invitation, err := q.AcceptInvitation(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, db.ErrNotFound
		}
		return db.User{}, fmt.Errorf("accept invitation: %w", err)
	}

	user, err := q.CreateUser(ctx, db.CreateUserParams{
		CompanyID: invitation.CompanyID,
		Email:     invitation.Email,
		Name:      name,
		Role:      invitation.Role,
	})
	if err != nil {
		if db.IsDuplicateKey(err) {
			return db.User{}, db.ErrConflict
		}
		return db.User{}, fmt.Errorf("create user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return db.User{}, fmt.Errorf("commit tx: %w", err)
	}

	return user, nil
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
	return acceptInviteHandlerWithExecutor(&poolAcceptInviteExecutor{pool: pool}, jwtSecret)
}

// acceptInviteHandlerWithExecutor is the testable core of AcceptInviteHandler.
func acceptInviteHandlerWithExecutor(exec acceptInviteExecutor, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req acceptInviteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx := c.Request.Context()

		user, err := exec.AcceptInvite(ctx, req.Token, req.Name)
		if err != nil {
			switch {
			case errors.Is(err, db.ErrNotFound):
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired invitation"})
			case errors.Is(err, db.ErrConflict):
				c.JSON(http.StatusConflict, gin.H{"error": "An account with this email already exists in this company"})
			default:
				log.Printf("accept-invite: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			}
			return
		}

		// At this point the user is committed to the database. If JWT issuance
		// fails here, the user exists but has no way to authenticate via the
		// invite flow. This is an acceptable edge case — the owner can re-invite
		// or the user can log in once a password-reset / magic-link flow is added.
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
