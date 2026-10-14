// Package main is the entry point of the API server.
// It only wires things together; real logic lives under internal/.
package main

import (
	"log"

	"mscs/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	// Never log DATABASE_URL / MONGO_URI: they contain passwords.
	log.Printf("config loaded: port=%s mongo_db=%s", cfg.Port, cfg.MongoDB)
}
