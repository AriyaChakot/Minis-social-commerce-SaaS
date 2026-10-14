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
// *pgxpool.Pool and *database.Mongo satisfy it, and so can a fake in tests.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler serves liveness and readiness checks.
type HealthHandler struct {
	checks map[string]Pinger // name -> dependency, e.g. "postgres" -> pool
}

// NewHealthHandler receives its dependencies instead of creating them (dependency injection).
func NewHealthHandler(checks map[string]Pinger) *HealthHandler {
	return &HealthHandler{checks: checks}
}

// Live reports that the process is up and able to serve HTTP.
// It does not touch the databases: a DB outage should not get the app restarted.
func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Ready reports whether the app can do real work, i.e. every dependency responds.
func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	code := http.StatusOK
	body := gin.H{"status": "ok"}

	for name, p := range h.checks {
		if err := p.Ping(ctx); err != nil {
			log.Printf("readiness: %s ping failed: %v", name, err)
			body[name] = "down"
			code = http.StatusServiceUnavailable
			continue
		}
		body[name] = "up"
	}

	if code != http.StatusOK {
		body["status"] = "unavailable"
	}
	c.JSON(code, body)
}
