// Package auth contains HTTP handlers and business logic for authentication
// flows, including self-service registration and magic-link login.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/db"
	"github.com/m00nk0d3/wrappr/api/internal/mailer"
)

// registerRequest is the JSON body for POST /v1/auth/register.
type registerRequest struct {
	CompanyName string `json:"company_name" binding:"required"`
	OwnerName   string `json:"owner_name"   binding:"required"`
	Email       string `json:"email"        binding:"required,email"`
}

// RegisterHandler returns a Gin handler for POST /v1/auth/register.
//
// TODO: add per-IP / per-email rate limiting before going to production.
//
//	201 {"message":"Registration successful. Check your email for a magic link."}
//	400 {"error":"<validation message>"}
//	500 {"error":"Internal server error"}
func RegisterHandler(pool *pgxpool.Pool, m mailer.Mailer, appURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req registerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx := c.Request.Context()

		// Check whether this email is already registered.
		q := db.New(pool)
		existing, err := q.GetUserByEmail(ctx, req.Email)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			log.Printf("register: lookup user by email: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		if err == nil {
			// Email already registered — silently issue a magic link to the
			// existing account and return the same 201 so we don't leak info.
			if sendErr := issueMagicLink(ctx, pool, m, appURL, existing.CompanyID, existing.Email, existing.Name, existing.Role); sendErr != nil {
				log.Printf("register: issue magic link for existing user: %v", sendErr)
				// Still return success to avoid leaking account existence.
			}
			c.JSON(http.StatusCreated, gin.H{"message": "Registration successful. Check your email for a magic link."})
			return
		}

		// New registration — run all DB writes in a single transaction.
		tx, err := pool.Begin(ctx)
		if err != nil {
			log.Printf("register: begin tx: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck

		qtx := db.New(tx)

		// Create the company, retrying once on slug collision.
		// Only one retry is attempted; a second collision would be
		// astronomically unlikely and would surface as a 500.
		slug := slugify(req.CompanyName)
		company, err := qtx.CreateCompany(ctx, db.CreateCompanyParams{Name: req.CompanyName, Slug: slug})
		if err != nil {
			// Slug collision — append a random 4-char suffix and retry.
			slug = slug + "-" + randomSuffix(4)
			company, err = qtx.CreateCompany(ctx, db.CreateCompanyParams{Name: req.CompanyName, Slug: slug})
			if err != nil {
				log.Printf("register: create company: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
				return
			}
		}

		// Create the owner user.
		user, err := qtx.CreateUser(ctx, db.CreateUserParams{
			CompanyID: company.ID,
			Email:     req.Email,
			Name:      req.OwnerName,
			Role:      "owner",
		})
		if err != nil {
			log.Printf("register: create user: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		// Create the magic-link invitation inside the same transaction.
		token, err := generateToken()
		if err != nil {
			log.Printf("register: generate token: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		_, err = qtx.CreateInvitation(ctx, db.CreateInvitationParams{
			CompanyID: company.ID,
			Email:     user.Email,
			Role:      user.Role,
			Token:     token,
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		})
		if err != nil {
			log.Printf("register: create invitation: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		if err := tx.Commit(ctx); err != nil {
			log.Printf("register: commit tx: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		// Send the magic link email after the transaction is committed.
		magicURL := fmt.Sprintf("%s/auth/magic?token=%s", strings.TrimRight(appURL, "/"), token)
		if err := m.SendMagicLink(ctx, user.Email, user.Name, magicURL); err != nil {
			// Log but don't fail the request — the user can request a new link.
			log.Printf("register: send magic link email: %v", err)
		}

		c.JSON(http.StatusCreated, gin.H{"message": "Registration successful. Check your email for a magic link."})
	}
}

// issueMagicLink creates a new invitation and sends a magic-link email to an
// existing user without requiring a transaction (read-then-write on existing data).
func issueMagicLink(ctx context.Context, pool *pgxpool.Pool, m mailer.Mailer, appURL string, companyID pgtype.UUID, email, name, role string) error {
	token, err := generateToken()
	if err != nil {
		return fmt.Errorf("generate token: %w", err)
	}

	q := db.New(pool)
	_, err = q.CreateInvitation(ctx, db.CreateInvitationParams{
		CompanyID: companyID,
		Email:     email,
		Role:      role,
		Token:     token,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("create invitation: %w", err)
	}

	magicURL := fmt.Sprintf("%s/auth/magic?token=%s", strings.TrimRight(appURL, "/"), token)
	if err := m.SendMagicLink(ctx, email, name, magicURL); err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	return nil
}

var nonAlphanumHyphen = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a company name into a URL-safe slug.
func slugify(name string) string {
	lower := strings.ToLower(name)
	slug := nonAlphanumHyphen.ReplaceAllString(lower, "-")
	return strings.Trim(slug, "-")
}

// generateToken returns 32 random bytes encoded as a 64-char lowercase hex string.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// randomSuffix returns n random lowercase hex characters.
func randomSuffix(n int) string {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		return "0000"
	}
	return hex.EncodeToString(b)[:n]
}
