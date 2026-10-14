// Package server builds the HTTP server: router, middleware and routes.
package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"mscs/internal/config"
	"mscs/internal/handler"
)

// New returns an *http.Server that is configured but not started yet.
func New(cfg config.Config) *http.Server {
	r := gin.New()
	r.Use(gin.Logger())   // log every request
	r.Use(gin.Recovery()) // turn a panic into a 500 response instead of crashing the process

	r.GET("/healthz", handler.Health)

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
