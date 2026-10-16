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
