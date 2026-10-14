// Package dbtest provides helpers for integration tests that need a real Postgres.
package dbtest

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool connects to DATABASE_URL (which must use the restricted app role, so RLS applies)
// and closes the pool when the test ends. Without DATABASE_URL the test is skipped.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("dbtest: connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("dbtest: ping: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Suffix returns a short random string to keep unique columns (slug, email) unique across runs.
func Suffix() string {
	return uuid.NewString()[:8]
}
