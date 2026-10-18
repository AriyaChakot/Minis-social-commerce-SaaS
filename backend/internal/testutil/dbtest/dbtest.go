// Package dbtest provides helpers for integration tests that need a real Postgres.
package dbtest

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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

// Mongo connects to MONGO_URI and returns a fresh database just for this test (dropped when the
// test ends), so tests never see each other's documents or the dev data. Without MONGO_URI the
// test is skipped.
func Mongo(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set; skipping integration test")
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("dbtest: mongo connect: %v", err)
	}
	db := client.Database("mscs_test_" + Suffix())
	t.Cleanup(func() {
		ctx := context.Background()
		_ = db.Drop(ctx)
		_ = client.Disconnect(ctx)
	})
	return db
}

// Suffix returns a short random string to keep unique columns (slug, email) unique across runs.
func Suffix() string {
	return uuid.NewString()[:8]
}
