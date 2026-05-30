package db

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned when a queried row does not exist.
// Handlers should check for this instead of importing pgx directly.
var ErrNotFound = errors.New("not found")

// IsNotFound reports whether err is a not-found error from the DB layer.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, pgx.ErrNoRows)
}
