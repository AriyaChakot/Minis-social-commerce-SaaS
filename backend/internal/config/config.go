// Package config loads application settings from environment variables.
package config

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
	"time"
)

// Config holds every setting the app needs at startup.
type Config struct {
	Port        string
	DatabaseURL string
	MongoURI    string
	MongoDB     string

	// PaymentWindow: how long a storefront order may stay unpaid before the worker cancels it.
	// The API needs it too, to show customers their payment deadline.
	PaymentWindow time.Duration

	// TrustedProxies: addresses or CIDR ranges of the servers allowed to tell the API the real
	// client IP (X-Forwarded-For): our Nuxt and SvelteKit servers. Empty = trust nobody.
	TrustedProxies []string
}

// WorkerConfig holds the settings of the background worker (cmd/worker).
type WorkerConfig struct {
	DatabaseURL   string
	Interval      time.Duration // how often the jobs run
	PaymentWindow time.Duration
}

// Load reads settings from the environment.
// It returns an error if a required variable is missing, so the app fails fast at startup
// instead of crashing later on the first database call.
func Load() (Config, error) {
	cfg := Config{
		Port:        getEnv("APP_PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		MongoURI:    os.Getenv("MONGO_URI"),
		MongoDB:     os.Getenv("MONGO_DB"),
	}

	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.MongoURI == "" {
		missing = append(missing, "MONGO_URI")
	}
	if cfg.MongoDB == "" {
		missing = append(missing, "MONGO_DB")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("config: missing required env vars: %s", strings.Join(missing, ", "))
	}

	var err error
	if cfg.PaymentWindow, err = paymentWindow(); err != nil {
		return Config{}, err
	}
	if cfg.TrustedProxies, err = getAddrList("TRUSTED_PROXIES"); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// getAddrList parses a comma-separated list of IP addresses and CIDR ranges.
func getAddrList(key string) ([]string, error) {
	var list []string
	for _, item := range strings.Split(os.Getenv(key), ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		_, errPrefix := netip.ParsePrefix(item)
		_, errAddr := netip.ParseAddr(item)
		if errPrefix != nil && errAddr != nil {
			return nil, fmt.Errorf("config: %s: %q is not an IP address or CIDR range", key, item)
		}
		list = append(list, item)
	}
	return list, nil
}

// LoadWorker reads the worker's settings. It needs only PostgreSQL.
func LoadWorker() (WorkerConfig, error) {
	cfg := WorkerConfig{DatabaseURL: os.Getenv("DATABASE_URL")}
	if cfg.DatabaseURL == "" {
		return WorkerConfig{}, fmt.Errorf("config: missing required env vars: DATABASE_URL")
	}

	var err error
	if cfg.Interval, err = getDuration("WORKER_INTERVAL", time.Minute, 5*time.Second); err != nil {
		return WorkerConfig{}, err
	}
	if cfg.PaymentWindow, err = paymentWindow(); err != nil {
		return WorkerConfig{}, err
	}
	return cfg, nil
}

// paymentWindow is shared by the API and the worker, so they always agree on the deadline.
func paymentWindow() (time.Duration, error) {
	return getDuration("ORDER_PAYMENT_WINDOW", 24*time.Hour, time.Minute)
}

// getDuration parses a Go duration such as "90s", "15m" or "24h". A typo or a value below min
// is an error (fail fast), not a silent fallback to the default.
func getDuration(key string, fallback, min time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s=%q is not a duration (examples: 30s, 15m, 24h)", key, v)
	}
	if d < min {
		return 0, fmt.Errorf("config: %s=%s is too short (minimum %s)", key, d, min)
	}
	return d, nil
}

// getEnv returns the value of key, or fallback when it is unset or empty.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
