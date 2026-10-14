// Package handler contains HTTP handlers: they read the request and write the response.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Health reports that the process is up and able to serve HTTP.
// Docker, load balancers and uptime monitors call this endpoint.
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
