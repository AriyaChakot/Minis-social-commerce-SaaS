// Package user owns everything about user accounts.
package user

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// User is a person who can log in.
type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"` // "-" = never serialized to JSON, even by accident
	DisplayName  string    `json:"display_name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

var (
	ErrNotFound   = errors.New("user: not found")
	ErrEmailTaken = errors.New("user: email already registered")
)

// NormalizeEmail trims spaces and lowercases, matching the users_email_lowercase constraint.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
