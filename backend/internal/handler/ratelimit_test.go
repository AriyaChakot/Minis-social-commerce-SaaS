package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mscs/internal/ratelimit"
)

// newLimitedRouter: one route allowing 2 requests per client, with 10.0.0.5 as the trusted
// proxy (standing in for our SvelteKit/Nuxt server).
func newLimitedRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := r.SetTrustedProxies([]string{"10.0.0.5"}); err != nil {
		t.Fatal(err)
	}
	r.RemoteIPHeaders = []string{"X-Forwarded-For"}
	r.GET("/limited", RateLimitByIP(ratelimit.New(2, time.Minute)), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return r
}

func get(r http.Handler, remote, forwardedFor string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/limited", nil)
	req.RemoteAddr = remote + ":51000"
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRateLimitByIP(t *testing.T) {
	t.Run("over the limit: 429 with Retry-After and the usual error body", func(t *testing.T) {
		r := newLimitedRouter(t)
		get(r, "198.51.100.1", "")
		get(r, "198.51.100.1", "")
		w := get(r, "198.51.100.1", "")
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("want 429, got %d", w.Code)
		}
		if got := w.Header().Get("Retry-After"); got != "60" {
			t.Fatalf("want Retry-After: 60, got %q", got)
		}
		var body struct {
			Error struct{ Code string } `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error.Code != "rate_limited" {
			t.Fatalf("want error code rate_limited, got %s", w.Body.String())
		}
	})

	t.Run("behind the trusted proxy, each customer has their own allowance", func(t *testing.T) {
		r := newLimitedRouter(t)
		for range 2 {
			get(r, "10.0.0.5", "203.0.113.7") // customer A uses up their allowance
		}
		if w := get(r, "10.0.0.5", "203.0.113.7"); w.Code != http.StatusTooManyRequests {
			t.Fatalf("customer A: want 429, got %d", w.Code)
		}
		if w := get(r, "10.0.0.5", "203.0.113.8"); w.Code != http.StatusNoContent {
			t.Fatalf("customer B (same proxy) must not be blocked, got %d", w.Code)
		}
	})

	t.Run("a client cannot dodge the limit with a fake X-Forwarded-For", func(t *testing.T) {
		r := newLimitedRouter(t)
		// Not our proxy: its X-Forwarded-For is ignored, so changing it changes nothing.
		for i, fake := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
			w := get(r, "198.51.100.9", fake)
			if want := []int{204, 204, 429}[i]; w.Code != want {
				t.Fatalf("request %d with fake XFF %s: want %d, got %d", i+1, fake, want, w.Code)
			}
		}
	})
}
