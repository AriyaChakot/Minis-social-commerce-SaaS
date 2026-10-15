package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mscs/internal/database"
)

// SessionTTL is how long a login stays valid.
const SessionTTL = 7 * 24 * time.Hour

// Session is what the client receives after register/login.
// Token is shown exactly once; the server keeps only its hash.
type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// newToken returns 128 bits of randomness as 26 base32 characters (crypto/rand.Text, Go 1.24+).
func newToken() string {
	return rand.Text()
}

// hashToken is SHA-256, not Argon2: the token is already long and random,
// so a fast hash is enough and keeps every authenticated request cheap.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// sessionStore is the repository for the sessions table (unexported: only this package uses it).
type sessionStore struct{}

func (sessionStore) create(ctx context.Context, db database.DBTX, userID uuid.UUID, tokenHash []byte, expiresAt time.Time) error {
	_, err := db.Exec(ctx,
		`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("session: create: %w", err)
	}
	return nil
}

// userID returns the owner of a non-expired session, or ErrUnauthenticated.
func (sessionStore) userID(ctx context.Context, db database.DBTX, tokenHash []byte) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT user_id FROM sessions WHERE token_hash = $1 AND expires_at > now()`,
		tokenHash,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrUnauthenticated
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("session: lookup: %w", err)
	}
	return id, nil
}

func (sessionStore) delete(ctx context.Context, db database.DBTX, tokenHash []byte) error {
	if _, err := db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("session: delete: %w", err)
	}
	return nil
}
