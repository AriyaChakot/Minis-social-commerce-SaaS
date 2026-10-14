// Package config loads application settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds every setting the app needs at startup.
type Config struct {
	Port        string
	DatabaseURL string
	MongoURI    string
	MongoDB     string
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

	return cfg, nil
}

// getEnv returns the value of key, or fallback when it is unset or empty.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
