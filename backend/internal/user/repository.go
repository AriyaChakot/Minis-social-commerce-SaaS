package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mscs/internal/database"
)

// Repository reads and writes users in Postgres. See tenant.Repository for the DBTX convention.
type Repository struct{}

const userColumns = `id, email, password_hash, display_name, created_at, updated_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// Create inserts a user. passwordHash must already be hashed; this layer never sees plain passwords.
func (Repository) Create(ctx context.Context, db database.DBTX, email, passwordHash, displayName string) (User, error) {
	u, err := scanUser(db.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ($1, $2, $3) RETURNING `+userColumns,
		NormalizeEmail(email), passwordHash, displayName,
	))
	if database.IsUniqueViolation(err, "users_email_key") {
		return User{}, ErrEmailTaken
	}
	if err != nil {
		return User{}, fmt.Errorf("user: create: %w", err)
	}
	return u, nil
}

// GetByID finds a user by id.
func (Repository) GetByID(ctx context.Context, db database.DBTX, id uuid.UUID) (User, error) {
	u, err := scanUser(db.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`,
		id,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("user: get by id: %w", err)
	}
	return u, nil
}

// GetByEmail finds a user by email (case-insensitive, via NormalizeEmail).
func (Repository) GetByEmail(ctx context.Context, db database.DBTX, email string) (User, error) {
	u, err := scanUser(db.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE email = $1`,
		NormalizeEmail(email),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("user: get by email: %w", err)
	}
	return u, nil
}
