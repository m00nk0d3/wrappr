package db

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is the canonical not-found sentinel for the DB layer.
// Generated sqlc code returns pgx.ErrNoRows directly; repository wrappers
// should map pgx.ErrNoRows → ErrNotFound so callers never need to import pgx.
// Use IsNotFound() to check for either form.
var ErrNotFound = errors.New("not found")

// ErrConflict is the canonical unique-constraint sentinel for the DB layer.
// Use IsDuplicateKey() to detect this condition from a raw pgx error.
var ErrConflict = errors.New("conflict")

// IsNotFound reports whether err is a not-found error from the DB layer.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, pgx.ErrNoRows)
}

// IsDuplicateKey reports whether err is a unique constraint violation (Postgres error 23505).
// Repository wrappers should map this to ErrConflict so callers never need to import pgconn.
func IsDuplicateKey(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
