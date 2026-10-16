// Package main is the background worker: a second program built from the same code base as the
// API. It runs periodic jobs, and only while it is the elected leader, so it can be deployed with
// several replicas for availability without the jobs running twice.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"mscs/internal/auth"
	"mscs/internal/config"
	"mscs/internal/database"
	"mscs/internal/leader"
	"mscs/internal/order"
	"mscs/internal/worker"
)

func main() {
	cfg, err := config.LoadWorker()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("config loaded: interval=%s payment_window=%s", cfg.Interval, cfg.PaymentWindow)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pg, err := database.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pg.Close()
	log.Println("postgres connected")

	jobs := worker.Jobs(order.NewService(pg), auth.NewService(pg), cfg.PaymentWindow, time.Now)

	elector := leader.New(pg.Config().ConnConfig, leader.Key("mscs-worker"))
	elector.Logf = log.Printf
	log.Println("worker started; waiting to become the leader")
	elector.Run(ctx, func(ctx context.Context) {
		worker.Loop(ctx, cfg.Interval, jobs)
	})
	log.Println("worker stopped")
}
