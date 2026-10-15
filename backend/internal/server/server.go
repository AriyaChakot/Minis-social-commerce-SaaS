// Package server builds the HTTP server: router, middleware and routes.
package server

import (
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/auth"
	"mscs/internal/config"
	"mscs/internal/database"
	"mscs/internal/handler"
)

// maxBodyBytes caps request bodies so a client cannot exhaust memory with a huge upload.
const maxBodyBytes = 1 << 20 // 1 MiB

// Deps groups the shared resources that handlers need.
// Adding a new dependency later means adding a field here, not changing New's signature.
type Deps struct {
	Postgres *pgxpool.Pool
	Mongo    *database.Mongo
}

// New returns an *http.Server that is configured but not started yet.
func New(cfg config.Config, deps Deps) *http.Server {
	useJSONNamesInValidationErrors()

	r := gin.New()
	r.Use(gin.Logger())   // log every request
	r.Use(gin.Recovery()) // turn a panic into a 500 response instead of crashing the process
	r.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
		c.Next()
	})

	health := handler.NewHealthHandler(map[string]handler.Pinger{
		"postgres": deps.Postgres,
		"mongo":    deps.Mongo,
	})
	r.GET("/healthz", health.Live)
	r.GET("/readyz", health.Ready)

	// Composition root: services are built here and handed to handlers (dependency injection).
	authSvc := auth.NewService(deps.Postgres)
	authH := handler.NewAuthHandler(authSvc)

	api := r.Group("/api/v1")
	{
		// Public routes.
		api.POST("/auth/register", authH.Register)
		api.POST("/auth/login", authH.Login)

		// Routes for any logged-in user.
		authed := api.Group("", handler.RequireAuth(authSvc))
		authed.POST("/auth/logout", authH.Logout)
		authed.GET("/me", authH.Me)

		// Routes inside one shop: the user must be a member of :tenantID.
		shop := authed.Group("/tenants/:tenantID", handler.RequireTenantMember(authSvc))
		shop.GET("", handler.GetTenant)
	}

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

// useJSONNamesInValidationErrors makes validation errors report "shop_slug" (the JSON name
// the client sent) instead of "ShopSlug" (the Go field name).
func useJSONNamesInValidationErrors() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
}
