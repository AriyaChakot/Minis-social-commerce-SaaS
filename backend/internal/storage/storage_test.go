package storage_test

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"mscs/internal/storage"
	"mscs/internal/testutil/dbtest"
)

// The same behaviour is expected from the real S3 store and the in-memory one used by tests.
func checkStore(t *testing.T, s storage.Store) {
	t.Helper()
	ctx := context.Background()
	key := "tests/" + dbtest.Suffix() + "/a.jpg"

	if _, _, err := s.Get(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing key: want ErrNotFound, got %v", err)
	}
	if err := s.Put(ctx, key, "image/jpeg", []byte("jpeg-bytes")); err != nil {
		t.Fatal(err)
	}
	body, ct, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	if string(data) != "jpeg-bytes" || ct != "image/jpeg" {
		t.Fatalf("got %q %q", data, ct)
	}
	if err := s.Delete(ctx, key, key+"-never-existed"); err != nil {
		t.Fatalf("delete (a missing key is not an error): %v", err)
	}
	if _, _, err := s.Get(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("after delete: want ErrNotFound, got %v", err)
	}
}

func TestMemory(t *testing.T) {
	checkStore(t, storage.NewMemory())
}

// Against the real object storage from docker compose (SeaweedFS); skipped without S3_ENDPOINT.
func TestS3(t *testing.T) {
	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_ENDPOINT not set; skipping object storage test")
	}
	s, err := storage.NewS3(storage.S3Config{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		Bucket:    "mscs-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for range 2 { // creating the bucket twice is fine (startup runs it every time)
		if err := s.EnsureBucket(ctx); err != nil {
			t.Fatal(err)
		}
	}
	checkStore(t, s)
}
