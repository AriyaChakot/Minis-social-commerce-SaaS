// Package main is the entry point of the API server.
// It only wires things together; real logic lives under internal/.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"mscs/internal/config"
	"mscs/internal/database"
	"mscs/internal/inbox"
	"mscs/internal/lineapi"
	"mscs/internal/secretbox"
	"mscs/internal/server"
	"mscs/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	// Never log DATABASE_URL / MONGO_URI: they contain passwords.
	log.Printf("config loaded: port=%s mongo_db=%s", cfg.Port, cfg.MongoDB)

	// ctx is cancelled when we receive Ctrl+C (SIGINT) or `docker stop` (SIGTERM).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pg, err := database.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pg.Close()
	log.Println("postgres connected")

	mg, err := database.NewMongo(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mg.Close(closeCtx); err != nil {
			log.Printf("mongo: close: %v", err)
		}
	}()
	log.Println("mongo connected")

	box, err := secretbox.New(cfg.DataEncryptionKey)
	if err != nil {
		log.Fatal(err)
	}
	store := inbox.NewStore(mg.DB)
	inboxSvc := inbox.NewService(pg, store, box, lineapi.New(cfg.LineAPIBaseURL))

	// MongoDB has no migrations: collections appear on first write, indexes are created here.
	// CreateMany is a no-op for indexes that already exist, so this is safe on every start.
	if err := store.EnsureIndexes(ctx); err != nil {
		log.Fatal(err)
	}
	// Secrets saved before encryption existed (migration 000012) are encrypted now; a no-op later.
	if n, err := inboxSvc.EncryptLegacySecrets(ctx); err != nil {
		log.Fatal(err)
	} else if n > 0 {
		log.Printf("inbox: encrypted %d LINE channel secret(s) stored in plaintext", n)
	}

	// Object storage for product photos; the bucket is created on first start.
	media, err := storage.NewS3(storage.S3Config(cfg.S3))
	if err != nil {
		log.Fatal(err)
	}
	if err := media.EnsureBucket(ctx); err != nil {
		log.Fatal(err)
	}
	log.Printf("object storage ready: bucket=%s", cfg.S3.Bucket)

	srv := server.New(cfg, server.Deps{Postgres: pg, Mongo: mg, Inbox: inboxSvc, Storage: media})

	// ListenAndServe blocks forever, so run it in a goroutine and keep main free to wait for the signal.
	go func() {
		log.Printf("http server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	// Give in-flight requests up to 10 seconds to finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}
	log.Println("server stopped")
}
