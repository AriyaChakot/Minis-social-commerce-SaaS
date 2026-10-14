// Package server builds the HTTP server: router, middleware and routes.
package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/config"
	"mscs/internal/database"
	"mscs/internal/handler"
)

// Deps groups the shared resources that handlers need.
// Adding a new dependency later means adding a field here, not changing New's signature.
type Deps struct {
	Postgres *pgxpool.Pool
	Mongo    *database.Mongo
}

// New returns an *http.Server that is configured but not started yet.
func New(cfg config.Config, deps Deps) *http.Server {
	r := gin.New()
	r.Use(gin.Logger())   // log every request
	r.Use(gin.Recovery()) // turn a panic into a 500 response instead of crashing the process

	health := handler.NewHealthHandler(map[string]handler.Pinger{
		"postgres": deps.Postgres,
		"mongo":    deps.Mongo,
	})
	r.GET("/healthz", health.Live)
	r.GET("/readyz", health.Ready)

	return &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
		// Timeouts protect the server from slow or stuck clients.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
