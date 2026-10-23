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
	"mscs/internal/inbox"
	"mscs/internal/order"
	"mscs/internal/platform"
	"mscs/internal/product"
	"mscs/internal/ratelimit"
	"mscs/internal/storage"
	"mscs/internal/storefront"
	"mscs/internal/team"
	"mscs/internal/tenant"
)

// maxBodyBytes caps request bodies so a client cannot exhaust memory with a huge upload.
const maxBodyBytes = 1 << 20 // 1 MiB

// Deps groups the shared resources that handlers need.
// Adding a new dependency later means adding a field here, not changing New's signature.
type Deps struct {
	Postgres *pgxpool.Pool
	Mongo    *database.Mongo
	// Inbox is built in main, which also runs its startup tasks (indexes, secret encryption).
	Inbox *inbox.Service
	// Storage holds the product photos (S3-compatible object storage).
	Storage storage.Store
}

// New returns an *http.Server that is configured but not started yet.
func New(cfg config.Config, deps Deps) *http.Server {
	useJSONNamesInValidationErrors()

	r := gin.New()
	// Which callers may tell us the real client IP. Gin's default trusts everyone, so any client
	// could fake X-Forwarded-For; nil trusts no one, and then ClientIP is the TCP peer.
	// (config.Load has validated the list, so this cannot fail.)
	_ = r.SetTrustedProxies(cfg.TrustedProxies)
	r.RemoteIPHeaders = []string{"X-Forwarded-For"}
	r.Use(gin.Logger())   // log every request
	r.Use(gin.Recovery()) // turn a panic into a 500 response instead of crashing the process
	r.Use(func(c *gin.Context) {
		// Routing has already happened, so FullPath (the route pattern) is known here: the photo
		// upload route gets a bigger limit, every other route keeps the small one.
		limit := int64(maxBodyBytes)
		if c.Request.Method == http.MethodPost && strings.HasSuffix(c.FullPath(), "/products/:productID/images") {
			limit = handler.MaxUploadBytes
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	})

	health := handler.NewHealthHandler(map[string]handler.Pinger{
		"postgres": deps.Postgres,
		"mongo":    deps.Mongo,
	})
	r.GET("/healthz", health.Live)
	r.GET("/readyz", health.Ready)

	// Composition root: services are built here and handed to handlers (dependency injection).
	// Rate limits (token buckets, see package ratelimit): burst, then one more per interval.
	// Strict where a request is expensive or abusable: login runs Argon2 (slow on purpose) and
	// invites password guessing; a guest order reserves stock. Generous for public reads.
	var (
		authByIP        = ratelimit.New(10, 6*time.Second) // ~10 per minute
		loginByEmail    = ratelimit.New(5, time.Minute)    // 5 tries, then 1 per minute
		guestOrderByIP  = ratelimit.New(5, 2*time.Minute)  // 5 orders, then 1 per 2 minutes
		publicReadsByIP = ratelimit.New(60, time.Second/2) // ~120 per minute
	)

	authSvc := auth.NewService(deps.Postgres)
	authH := handler.NewAuthHandler(authSvc, loginByEmail)
	productSvc := product.NewService(deps.Postgres, product.WithMediaBaseURL(cfg.MediaBaseURL))
	imageH := handler.NewImageHandler(product.NewImageService(productSvc, deps.Storage), deps.Storage)
	orderSvc := order.NewService(deps.Postgres)
	productH := handler.NewProductHandler(productSvc)
	orderH := handler.NewOrderHandler(orderSvc)
	inboxH := handler.NewInboxHandler(deps.Inbox)
	teamH := handler.NewTeamHandler(team.NewService(deps.Postgres, authSvc))
	shippingH := handler.NewShippingHandler(deps.Postgres)
	platformSvc := platform.NewService(deps.Postgres)
	platformH := handler.NewPlatformHandler(authSvc, platformSvc, loginByEmail)
	storeH := handler.NewStorefrontHandler(storefront.NewService(deps.Postgres, productSvc, orderSvc, cfg.PaymentWindow))

	api := r.Group("/api/v1")
	{
		// Public routes.
		limitAuth := handler.RateLimitByIP(authByIP)
		api.POST("/auth/register", limitAuth, authH.Register)
		api.POST("/auth/login", limitAuth, authH.Login)

		// Customer-facing storefront: no login, the shop is chosen by its slug.
		store := api.Group("/public/shops/:slug")
		limitReads := handler.RateLimitByIP(publicReadsByIP)
		store.GET("", limitReads, storeH.Shop)
		store.GET("/products", limitReads, storeH.Products)
		store.POST("/orders", handler.RateLimitByIP(guestOrderByIP), storeH.PlaceOrder)
		store.GET("/orders/:token", limitReads, storeH.TrackOrder)

		// Product photos, for every browser (like images on any website); see ImageHandler.Serve.
		api.GET("/media/*key", imageH.Serve)

		// Called by LINE's servers, not by a browser: authenticated by the body's HMAC signature.
		// Not rate-limited per IP: LINE delivers for every shop from a few addresses.
		api.POST("/webhooks/line/:channelID", inboxH.LINEWebhook)

		// Routes for any logged-in user.
		authed := api.Group("", handler.RequireAuth(authSvc))
		authed.POST("/auth/logout", authH.Logout)
		authed.GET("/me", authH.Me)

		// Invitation links: opening one needs no account (the link is the permission); joining
		// needs one, existing (accept) or new (register).
		api.GET("/invitations/:token", limitAuth, teamH.Preview)
		api.POST("/invitations/:token/register", limitAuth, teamH.Register)
		authed.POST("/invitations/:token/accept", limitAuth, teamH.Accept)

		// Routes inside one shop: the user must be a member of :tenantID.
		shop := authed.Group("/tenants/:tenantID", handler.RequireTenantMember(authSvc))
		shop.GET("", handler.GetTenant)
		shop.GET("/products", productH.List)
		shop.GET("/products/:productID", productH.Get)
		shop.GET("/orders", orderH.List)
		shop.GET("/orders/:orderID", orderH.Get)
		shop.POST("/orders", orderH.Create)
		shop.POST("/orders/:orderID/status", orderH.Transition)
		shop.GET("/conversations", inboxH.Conversations)
		shop.GET("/conversations/:conversationID/messages", inboxH.Messages)
		shop.POST("/conversations/:conversationID/messages", inboxH.Reply)
		shop.POST("/conversations/:conversationID/read", inboxH.MarkRead)
		shop.GET("/channels/line", inboxH.LINEChannel)

		// Inside one shop, but only for roles that may change the catalog.
		managers := shop.Group("", handler.RequireRole(tenant.RoleOwner, tenant.RoleAdmin))
		managers.POST("/products", productH.Create)
		managers.PATCH("/products/:productID", productH.Update)
		managers.DELETE("/products/:productID", productH.Archive)
		managers.POST("/products/:productID/images", imageH.Upload)
		managers.DELETE("/products/:productID/images/:imageID", imageH.Delete)
		managers.POST("/products/:productID/images/:imageID/cover", imageH.MakeCover)
		managers.PUT("/channels/line", inboxH.ConnectLINE)
		managers.PUT("/shipping", shippingH.Update)
		managers.GET("/team", teamH.Team)
		managers.POST("/invitations", teamH.Invite)
		managers.DELETE("/invitations/:invitationID", teamH.Revoke)
		managers.DELETE("/members/:userID", teamH.RemoveMember)

		// The platform console (React SPA): its own cookie session, platform admins only.
		api.POST("/platform/login", limitAuth, platformH.Login)
		console := api.Group("/platform", handler.RequirePlatformAdmin(authSvc, platformSvc))
		console.POST("/logout", platformH.Logout)
		console.GET("/me", platformH.Me)
		console.GET("/overview", platformH.Overview)
		console.GET("/audit", platformH.Audit)
		console.GET("/tenants", platformH.Tenants)
		console.POST("/tenants/:tenantID/suspend", platformH.Suspend)
		console.POST("/tenants/:tenantID/reactivate", platformH.Reactivate)
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
