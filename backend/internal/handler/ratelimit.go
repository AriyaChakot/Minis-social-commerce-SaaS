package handler

import (
	"math"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"mscs/internal/ratelimit"
)

// RateLimitByIP refuses a request with 429 when its client IP has used up its allowance in l.
//
// c.ClientIP() is the real client only if the router trusts the proxy in front of it (the Nuxt
// or SvelteKit server, which forwards the browser's address in X-Forwarded-For). See
// config.TrustedProxies: trusting every proxy would let anyone send a fake X-Forwarded-For
// and get a fresh allowance on every request.
func RateLimitByIP(l *ratelimit.Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !allow(c, l, ratelimit.IPKey(c.ClientIP())) {
			return
		}
		c.Next()
	}
}

// allow takes a token for key; when there is none it writes the 429 response and returns false.
func allow(c *gin.Context, l *ratelimit.Limiter, key string) bool {
	ok, wait := l.Allow(key)
	if ok {
		return true
	}
	// Retry-After (in whole seconds) tells well-behaved clients when to come back.
	c.Header("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	writeError(c, http.StatusTooManyRequests, "rate_limited", "too many requests; please try again later")
	return false
}
