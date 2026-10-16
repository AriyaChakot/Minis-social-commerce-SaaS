package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadWorker(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")

	t.Run("defaults", func(t *testing.T) {
		cfg, err := LoadWorker()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Interval != time.Minute || cfg.PaymentWindow != 24*time.Hour {
			t.Fatalf("unexpected defaults: %+v", cfg)
		}
	})

	t.Run("custom values", func(t *testing.T) {
		t.Setenv("WORKER_INTERVAL", "30s")
		t.Setenv("ORDER_PAYMENT_WINDOW", "2h")
		cfg, err := LoadWorker()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Interval != 30*time.Second || cfg.PaymentWindow != 2*time.Hour {
			t.Fatalf("got %+v", cfg)
		}
	})

	for _, tc := range []struct{ key, value, want string }{
		{"WORKER_INTERVAL", "1 minute", "not a duration"},
		{"WORKER_INTERVAL", "1s", "too short"},
		{"ORDER_PAYMENT_WINDOW", "24", "not a duration"}, // a bare number has no unit: refuse, don't guess
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := LoadWorker(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestTrustedProxies(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("MONGO_URI", "mongodb://example")
	t.Setenv("MONGO_DB", "mscs")

	t.Setenv("TRUSTED_PROXIES", " 172.16.0.0/12, 10.0.0.5 ,")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "172.16.0.0/12" || cfg.TrustedProxies[1] != "10.0.0.5" {
		t.Fatalf("got %q", cfg.TrustedProxies)
	}

	t.Setenv("TRUSTED_PROXIES", "172.16.0.0/12,localhost")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), `"localhost"`) {
		t.Fatalf("want an error naming the bad entry, got %v", err)
	}

	t.Setenv("TRUSTED_PROXIES", "")
	if cfg, err := Load(); err != nil || cfg.TrustedProxies != nil {
		t.Fatalf("unset: want no trusted proxies, got %q (err %v)", cfg.TrustedProxies, err)
	}
}
