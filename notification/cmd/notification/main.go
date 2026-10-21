package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/lib/pq"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/api"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/config"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/consumer"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/store"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/ws"
)

func main() {
	cfg := config.Load()
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	for db.PingContext(ctx) != nil {
		log.Printf("service=notification database-wait")
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
	st := store.New(db)
	if err = st.Migrate(ctx); err != nil {
		log.Fatal(err)
	}
	hub := ws.NewHub()
	c := &consumer.Consumer{URL: cfg.RabbitURL, Store: st, Hub: hub, Cache: consumer.NewCache()}
	go c.Run(ctx)
	server := &http.Server{Addr: ":" + cfg.Port, Handler: (&api.API{Store: st, Hub: hub}).Handler()}
	go func() {
		log.Printf("service=notification port=%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	server.Shutdown(context.Background())
}
