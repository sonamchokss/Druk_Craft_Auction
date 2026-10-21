// Control service — demo controls for the admin panel (plan section 6.1):
// stop/start a service container, re-publish an event unchanged (duplicate
// test, T8) and publish a malformed event (poison test, T9).
//
// It talks to Docker through its socket, which is powerful, so it is locked
// down: compose binds it to 127.0.0.1, it only answers the frontend's own
// origins (no drive-by requests from other websites), and it can only touch
// the five app containers below.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/logx"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/rabbitmq"
)

const service = "control-service"

// containers maps the names the admin panel uses to compose container names.
var containers = map[string]string{
	"listing":      "druk-listing",
	"bidding":      "druk-bidding",
	"timer":        "druk-timer",
	"payment":      "druk-payment",
	"notification": "druk-notification",
}

var allowedOrigins = map[string]bool{
	"http://localhost:3000": true,
	"http://127.0.0.1:3000": true,
}

type server struct {
	docker *http.Client
	mu     chan struct{} // serialises publishes on the confirm channel
	ch     *amqp.Channel
}

func main() {
	logx.Init(service)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	conn, err := rabbitmq.Connect(ctx, os.Getenv("RABBITMQ_URL"))
	if err != nil {
		fatal(err)
	}
	ch, err := conn.Channel()
	if err != nil {
		fatal(err)
	}
	if err := ch.Confirm(false); err != nil {
		fatal(err)
	}

	s := &server{
		docker: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
			},
		}},
		mu: make(chan struct{}, 1),
		ch: ch,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /services", s.listServices)
	mux.HandleFunc("POST /services/{name}/{action}", s.serviceAction)
	mux.HandleFunc("POST /events/resend", s.resend)
	mux.HandleFunc("POST /events/poison", s.poison)

	addr := ":" + envOr("PORT", "8099")
	srv := &http.Server{Addr: addr, Handler: guard(mux)}
	go func() {
		slog.Info("http listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal(err)
		}
	}()
	<-ctx.Done()
	shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	_ = srv.Shutdown(shutdown)
	_ = conn.Close()
}

// guard rejects requests from any page other than the frontend. Browsers
// always send Origin on cross-origin requests; curl on this machine sends none.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !allowedOrigins[origin] {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "controls are only available from the frontend on this machine"})
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GET /services -> [{"name":"payment","status":"running"}, ...]
func (s *server) listServices(w http.ResponseWriter, r *http.Request) {
	out := []map[string]string{}
	for _, name := range []string{"listing", "bidding", "timer", "payment", "notification"} {
		out = append(out, map[string]string{"name": name, "status": s.containerStatus(r.Context(), containers[name])})
	}
	writeJSON(w, 200, out)
}

// POST /services/{name}/stop|start
func (s *server) serviceAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	container, ok := containers[name]
	if !ok || (action != "stop" && action != "start") {
		writeJSON(w, 404, map[string]string{"error": "unknown service or action"})
		return
	}
	path := fmt.Sprintf("http://docker/containers/%s/%s", container, action)
	if action == "stop" {
		path += "?t=5"
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, path, nil)
	resp, err := s.docker.Do(req)
	if err != nil {
		slog.Error("docker request failed", "service", name, "action", action, "err", err)
		writeJSON(w, 502, map[string]string{"error": "Docker is not reachable"})
		return
	}
	defer resp.Body.Close()
	// 204 done, 304 already in that state; both are fine for the demo.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		slog.Error("docker refused", "service", name, "action", action, "status", resp.StatusCode, "body", string(body))
		writeJSON(w, 502, map[string]string{"error": fmt.Sprintf("Docker could not %s %s", action, name)})
		return
	}
	slog.Info("service "+action, "service", name)
	writeJSON(w, 200, map[string]string{"name": name, "status": s.containerStatus(r.Context(), container)})
}

func (s *server) containerStatus(ctx context.Context, container string) string {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+container+"/json", nil)
	resp, err := s.docker.Do(req)
	if err != nil {
		return "unknown"
	}
	defer resp.Body.Close()
	var info struct{ State struct{ Status string } }
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&info) != nil {
		return "unknown"
	}
	return info.State.Status
}

// POST /events/resend with an envelope as the body. It is published byte for
// byte, so the eventId stays the same and consumers must skip it (T8).
func (s *server) resend(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "could not read body"})
		return
	}
	env, err := events.Parse(body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	key, ok := events.RoutingKeys[env.EventType]
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "unknown event type " + env.EventType})
		return
	}
	if err := s.publish(r.Context(), key, amqp.Publishing{MessageId: env.EventID, Type: env.EventType, Body: body}); err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	logx.Event(service, env, logx.Published, "resend", true)
	writeJSON(w, 200, map[string]string{"eventId": env.EventID, "routingKey": key})
}

// POST /events/poison publishes a body that is not JSON on auction.closed.
// Every consumer of that key retries it 3 times, then dead-letters it (T9).
func (s *server) poison(w http.ResponseWriter, r *http.Request) {
	if err := s.publish(r.Context(), events.KeyAuctionClosed, amqp.Publishing{Type: "Malformed", Body: []byte("this is not a valid event")}); err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("published malformed event", "routingKey", events.KeyAuctionClosed)
	writeJSON(w, 200, map[string]string{"routingKey": events.KeyAuctionClosed})
}

func (s *server) publish(ctx context.Context, key string, msg amqp.Publishing) error {
	s.mu <- struct{}{}
	defer func() { <-s.mu }()
	msg.DeliveryMode = amqp.Persistent
	msg.ContentType = "application/json"
	msg.Timestamp = time.Now()
	confirm, err := s.ch.PublishWithDeferredConfirmWithContext(ctx, rabbitmq.Exchange, key, false, false, msg)
	if err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	ok, err := confirm.WaitContext(ctx)
	if err != nil || !ok {
		return fmt.Errorf("broker did not confirm the publish")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fatal(err error) {
	slog.Error("startup failed", "err", err)
	os.Exit(1)
}
