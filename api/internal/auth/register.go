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

// roleOwner is the role assigned to the first user of every new company.
const roleOwner = "owner"

// registrationResult holds the data needed to send the post-registration email.
type registrationResult struct {
	userEmail string
	userName  string
	token     string
}

// RegisterHandler returns a Gin handler for POST /v1/auth/register.
//
// TODO: add per-IP / per-email rate limiting before going to production.
// Tracked in https://github.com/m00nk0d3/wrappr/issues/41
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

		// New registration — create company, user, and invitation in one transaction.
		reg, err := createRegistration(ctx, pool, req)
		if err != nil {
			log.Printf("register: create registration: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		// Send the magic link email after the transaction is committed.
		// Failure is non-fatal — the user can request a new link later.
		magicURL := fmt.Sprintf("%s/auth/magic?token=%s", strings.TrimRight(appURL, "/"), reg.token)
		if err := m.SendMagicLink(ctx, reg.userEmail, reg.userName, magicURL); err != nil {
			log.Printf("register: send magic link email: %v", err)
		}

		c.JSON(http.StatusCreated, gin.H{"message": "Registration successful. Check your email for a magic link."})
	}
}

// createRegistration runs the full DB transaction for a new company+owner registration:
// creates the company (with one slug-collision retry), the owner user, and an
// invitation token — all atomically. The caller is responsible for sending the
// magic-link email after this function returns successfully.
func createRegistration(ctx context.Context, pool *pgxpool.Pool, req registerRequest) (*registrationResult, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	qtx := db.New(tx)

	// Create the company, retrying once on slug collision.
	// Only one retry is attempted; a second collision would be
	// astronomically unlikely and would surface as a 500.
	slug := slugify(req.CompanyName)
	company, err := qtx.CreateCompany(ctx, db.CreateCompanyParams{Name: req.CompanyName, Slug: slug})
	if err != nil {
		// Slug collision — append a random 4-char suffix and retry once.
		suffix, suffixErr := randomSuffix(4)
		if suffixErr != nil {
			return nil, fmt.Errorf("generate slug suffix: %w", suffixErr)
		}
		slug = slug + "-" + suffix
		company, err = qtx.CreateCompany(ctx, db.CreateCompanyParams{Name: req.CompanyName, Slug: slug})
		if err != nil {
			return nil, fmt.Errorf("create company: %w", err)
		}
	}

	user, err := qtx.CreateUser(ctx, db.CreateUserParams{
		CompanyID: company.ID,
		Email:     req.Email,
		Name:      req.OwnerName,
		Role:      roleOwner,
	})
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	_, err = qtx.CreateInvitation(ctx, db.CreateInvitationParams{
		CompanyID: company.ID,
		Email:     user.Email,
		Role:      user.Role,
		Token:     token,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("create invitation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &registrationResult{
		userEmail: user.Email,
		userName:  user.Name,
		token:     token,
	}, nil
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
func randomSuffix(n int) (string, error) {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(b)[:n], nil
}

