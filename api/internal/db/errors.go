package db

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrNotFound is the canonical not-found sentinel for the DB layer.
// Generated sqlc code returns pgx.ErrNoRows directly; repository wrappers
// should map pgx.ErrNoRows → ErrNotFound so callers never need to import pgx.
// Use IsNotFound() to check for either form.
var ErrNotFound = errors.New("not found")

// IsNotFound reports whether err is a not-found error from the DB layer.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, pgx.ErrNoRows)
}
