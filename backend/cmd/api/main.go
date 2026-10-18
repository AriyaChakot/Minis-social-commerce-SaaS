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
	"mscs/internal/server"
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

	// MongoDB has no migrations: collections appear on first write, indexes are created here.
	// CreateMany is a no-op for indexes that already exist, so this is safe on every start.
	if err := inbox.NewStore(mg.DB).EnsureIndexes(ctx); err != nil {
		log.Fatal(err)
	}

	srv := server.New(cfg, server.Deps{Postgres: pg, Mongo: mg})

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
