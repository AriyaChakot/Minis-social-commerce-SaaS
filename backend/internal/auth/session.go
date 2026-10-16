package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mscs/internal/database"
	"mscs/internal/securetoken"
)

// SessionTTL is how long a login stays valid.
const SessionTTL = 7 * 24 * time.Hour

// Session is what the client receives after register/login.
// Token is shown exactly once; the server keeps only its hash.
type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// newToken and hashToken: see package securetoken (shared with order tracking links).
// SHA-256 rather than Argon2 keeps every authenticated request cheap.
func newToken() string              { return securetoken.New() }
func hashToken(token string) []byte { return securetoken.Hash(token) }

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

// deleteExpired removes up to limit sessions that expired before cutoff and reports how many.
// Deleting in small batches keeps each statement short, so it never holds locks or bloats the
// write-ahead log the way one huge DELETE of millions of rows would.
func (sessionStore) deleteExpired(ctx context.Context, db database.DBTX, cutoff time.Time, limit int) (int64, error) {
	tag, err := db.Exec(ctx,
		`DELETE FROM sessions
		  WHERE id IN (SELECT id FROM sessions WHERE expires_at < $1 LIMIT $2)`,
		cutoff, limit,
	)
	if err != nil {
		return 0, fmt.Errorf("session: delete expired: %w", err)
	}
	return tag.RowsAffected(), nil
}
