// Package service is the common bootstrap each microservice's main.go uses:
// logging, Postgres, RabbitMQ topology, a publisher, and GET /health.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/db"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/logx"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/rabbitmq"
)

// App holds the shared dependencies of a running service.
type App struct {
	Name      string
	DB        *pgxpool.Pool
	Conn      *amqp.Connection
	Channel   *amqp.Channel // for consuming; use Publisher to publish
	Publisher *rabbitmq.Publisher
	Mux       *http.ServeMux
}

// New connects everything using DATABASE_URL and RABBITMQ_URL. The context
// is cancelled on SIGINT/SIGTERM.
func New(name string) (*App, context.Context, context.CancelFunc) {
	logx.Init(name)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	pool, err := db.Connect(ctx, mustEnv("DATABASE_URL"))
	if err != nil {
		fatal("postgres", err)
	}
	conn, ch, err := rabbitmq.Setup(ctx, mustEnv("RABBITMQ_URL"))
	if err != nil {
		fatal("rabbitmq", err)
	}
	pub, err := rabbitmq.NewPublisher(conn, name)
	if err != nil {
		fatal("publisher", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return &App{Name: name, DB: pool, Conn: conn, Channel: ch, Publisher: pub, Mux: mux}, ctx, cancel
}

// Run serves HTTP on $PORT until ctx is cancelled, then shuts down cleanly.
func (a *App) Run(ctx context.Context) {
	addr := ":" + getEnv("PORT", "8080")
	srv := &http.Server{Addr: addr, Handler: cors(a.Mux)}

	go func() {
		slog.Info("http listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("http", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	_ = a.Publisher.Close()
	_ = a.Conn.Close()
	a.DB.Close()
}

// WriteJSON writes v as a JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes {"error": msg} — use for user-facing failures like "Bid too low".
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// cors allows the frontend on :3000 to call services directly.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fatal("config", errors.New(key+" is not set"))
	}
	return v
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fatal(what string, err error) {
	slog.Error("startup failed", "component", what, "err", err)
	os.Exit(1)
}
