// Package handler contains HTTP handlers: they read the request and write the response.
package handler

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Pinger is anything that can check its own connection.
// *pgxpool.Pool satisfies it, and so can a fake in tests.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler serves liveness and readiness checks.
type HealthHandler struct {
	postgres Pinger
}

// NewHealthHandler receives its dependencies instead of creating them (dependency injection).
func NewHealthHandler(postgres Pinger) *HealthHandler {
	return &HealthHandler{postgres: postgres}
}

// Live reports that the process is up and able to serve HTTP.
// It does not touch the database: a DB outage should not get the app restarted.
func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Ready reports whether the app can do real work, i.e. its dependencies respond.
func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if err := h.postgres.Ping(ctx); err != nil {
		log.Printf("readiness: postgres ping failed: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "postgres": "down"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "postgres": "up"})
}
