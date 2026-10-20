// Package storage keeps files (product photos) in S3-compatible object storage.
//
// Files do not belong in PostgreSQL: they are large, they would bloat every backup, and serving
// them would tie up database connections. Object storage is made for them and is cheap. The
// database keeps only each file's key (its name in the bucket).
//
// The API speaks plain S3, so the same code works with SeaweedFS (local development), AWS S3,
// Cloudflare R2 or any other S3-compatible service: only the endpoint and keys change.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ErrNotFound: no object has this key.
var ErrNotFound = errors.New("storage: object not found")

// Store is what the rest of the code needs from object storage. Tests use Memory instead of S3.
type Store interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	// Get returns the object's content and type. The caller closes it.
	Get(ctx context.Context, key string) (io.ReadCloser, string, error)
	// Delete removes objects; keys that do not exist are not an error.
	Delete(ctx context.Context, keys ...string) error
}

// S3 is a Store backed by an S3-compatible bucket.
type S3 struct {
	client *minio.Client
	bucket string
}

type S3Config struct {
	Endpoint  string // host:port, e.g. storage:8333 or s3.amazonaws.com
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

func NewS3(cfg S3Config) (*S3, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		// http://host/bucket/key, which every S3-compatible server understands (the other style,
		// bucket.host, needs DNS for each bucket).
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	return &S3{client: client, bucket: cfg.Bucket}, nil
}

// EnsureBucket creates the bucket if it does not exist yet (at startup, like the Mongo indexes).
// The bucket stays private: files are served through the API, never read from the bucket by browsers.
func (s *S3) EnsureBucket(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("storage: check bucket %s: %w", s.bucket, err)
	}
	if ok {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("storage: create bucket %s: %w", s.bucket, err)
	}
	return nil
}

func (s *S3) Put(ctx context.Context, key, contentType string, data []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("storage: get %s: %w", key, err)
	}
	// GetObject is lazy: Stat makes the request, so a missing key is found here.
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("storage: get %s: %w", key, err)
	}
	return obj, info.ContentType, nil
}

func (s *S3) Delete(ctx context.Context, keys ...string) error {
	var errs []error
	for _, key := range keys {
		if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
			errs = append(errs, fmt.Errorf("storage: delete %s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}

// Memory is an in-memory Store for tests.
type Memory struct {
	mu      sync.Mutex
	objects map[string]memoryObject
	// FailPut makes every Put fail (to test cleanup paths).
	FailPut bool
}

type memoryObject struct {
	data        []byte
	contentType string
}

func NewMemory() *Memory { return &Memory{objects: map[string]memoryObject{}} }

func (m *Memory) Put(_ context.Context, key, contentType string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailPut {
		return errors.New("storage: memory: put failed (FailPut)")
	}
	m.objects[key] = memoryObject{bytes.Clone(data), contentType}
	return nil
}

func (m *Memory) Get(_ context.Context, key string) (io.ReadCloser, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return nil, "", ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(o.data)), o.contentType, nil
}

func (m *Memory) Delete(_ context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.objects, k)
	}
	return nil
}

// Keys returns the stored keys (for tests).
func (m *Memory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.objects))
	for k := range m.objects {
		out = append(out, k)
	}
	return out
}
