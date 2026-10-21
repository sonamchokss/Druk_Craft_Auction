// Listing Service - Druk Craft Auction
// Owns listings_db. Publishes AuctionStarted.
// Consumes AuctionClosed, PaymentCompleted, PaymentFailed.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	serviceName = "listing-service"
	exchange    = "auction.events" // topic, durable
	dlxName     = "auction.dlx"    // direct, durable
	queueName   = "listing.queue"
	dlqName     = "listing.dlq"
	maxRetries  = 3
	retryHeader = "x-retry-count"
)

var (
	db     *sql.DB
	broker *Broker
)

// ---------------------------------------------------------------- schema

const schema = `
CREATE TABLE IF NOT EXISTS items (
	id               SERIAL PRIMARY KEY,
	title            TEXT NOT NULL,
	description      TEXT NOT NULL DEFAULT '',
	image_url        TEXT NOT NULL DEFAULT '',
	artisan_id       TEXT NOT NULL,
	start_price      INTEGER NOT NULL CHECK (start_price > 0),
	duration_seconds INTEGER NOT NULL CHECK (duration_seconds > 0),
	status           TEXT NOT NULL DEFAULT 'Draft',
	auction_id       TEXT UNIQUE,
	ends_at          TIMESTAMPTZ,
	winner_id        TEXT,
	final_price      INTEGER,
	receipt_no       TEXT,
	created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Added after the first release; ALTER keeps existing databases working.
ALTER TABLE items ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'Handicraft';
CREATE TABLE IF NOT EXISTS processed_events (
	event_id     TEXT PRIMARY KEY,
	processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);`

// ---------------------------------------------------------------- models

type Item struct {
	ID              int        `json:"id"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	ImageURL        string     `json:"imageUrl"`
	ArtisanID       string     `json:"artisanId"`
	StartPrice      int        `json:"startPrice"`
	DurationSeconds int        `json:"durationSeconds"`
	Status          string     `json:"status"`
	AuctionID       *string    `json:"auctionId"`
	EndsAt          *time.Time `json:"endsAt"`
	WinnerID        *string    `json:"winnerId"`
	FinalPrice      *int       `json:"finalPrice"`
	ReceiptNo       *string    `json:"receiptNo"`
	CreatedAt       time.Time  `json:"createdAt"`
	Category        string     `json:"category"`
}

const itemCols = `id, title, description, image_url, artisan_id, start_price, duration_seconds,
	status, auction_id, ends_at, winner_id, final_price, receipt_no, created_at, category`

type scanner interface{ Scan(dest ...any) error }

func scanItem(s scanner) (Item, error) {
	var it Item
	err := s.Scan(&it.ID, &it.Title, &it.Description, &it.ImageURL, &it.ArtisanID,
		&it.StartPrice, &it.DurationSeconds, &it.Status, &it.AuctionID, &it.EndsAt,
		&it.WinnerID, &it.FinalPrice, &it.ReceiptNo, &it.CreatedAt, &it.Category)
	return it, err
}

// Standard event envelope (see Section 4.2 of the plan)
type Envelope struct {
	EventID    string          `json:"eventId"`
	EventType  string          `json:"eventType"`
	OccurredAt string          `json:"occurredAt"`
	Source     string          `json:"source"`
	Data       json.RawMessage `json:"data"`
}

// ---------------------------------------------------------------- helpers

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// One log format for every event (timestamp is added by the log package).
func logEvent(eventType, eventID, auctionID, result string) {
	log.Printf("service=%s eventType=%s eventId=%s auctionId=%s result=%s",
		serviceName, eventType, eventID, auctionID, result)
}

// ---------------------------------------------------------------- broker

type Broker struct {
	url string
	mu  sync.RWMutex
	pub *amqp.Channel
}

func (b *Broker) setPub(ch *amqp.Channel) {
	b.mu.Lock()
	b.pub = ch
	b.mu.Unlock()
}

func declareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.ExchangeDeclare(dlxName, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(dlqName, true, false, false, false, nil); err != nil {
		return err
	}
	// docs/events.md §5: dead-letter routing key is the *queue* name. Every
	// service declares this queue, and RabbitMQ rejects (406) mismatched args.
	if err := ch.QueueBind(dlqName, queueName, dlxName, false, nil); err != nil {
		return err
	}
	args := amqp.Table{
		"x-dead-letter-exchange":    dlxName,
		"x-dead-letter-routing-key": queueName,
	}
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, args); err != nil {
		return err
	}
	for _, key := range []string{"auction.closed", "payment.*"} {
		if err := ch.QueueBind(queueName, key, exchange, false, nil); err != nil {
			return err
		}
	}
	return nil
}

// Publish sends a persistent event and waits for the broker's confirm.
func (b *Broker) Publish(ctx context.Context, routingKey, eventType string, data any) (string, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	env := Envelope{
		EventID:    uuid.NewString(),
		EventType:  eventType,
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
		Source:     serviceName,
		Data:       payload,
	}
	body, _ := json.Marshal(env)

	b.mu.RLock()
	ch := b.pub
	b.mu.RUnlock()
	if ch == nil {
		return "", errors.New("broker not connected")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conf, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, false, false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    env.EventID,
			Type:         eventType,
			Timestamp:    time.Now(),
			Body:         body,
		})
	if err != nil {
		return "", err
	}
	ok, err := conf.WaitContext(ctx)
	if err != nil || !ok {
		return "", fmt.Errorf("publish not confirmed: %v", err)
	}
	return env.EventID, nil
}

// Run keeps (re)connecting forever, so the consumer resumes after a RabbitMQ
// or service restart and drains whatever is waiting in the queue.
func (b *Broker) Run(ctx context.Context) {
	for {
		err := b.session(ctx)
		if ctx.Err() != nil {
			return
		}
		log.Printf("service=%s broker session ended (%v); reconnecting in 3s", serviceName, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (b *Broker) session(ctx context.Context) error {
	conn, err := amqp.Dial(b.url)
	if err != nil {
		return err
	}
	defer conn.Close()

	pub, err := conn.Channel()
	if err != nil {
		return err
	}
	if err := pub.Confirm(false); err != nil {
		return err
	}
	if err := declareTopology(pub); err != nil {
		return err
	}
	con, err := conn.Channel()
	if err != nil {
		return err
	}
	if err := con.Qos(10, 0, false); err != nil { // prefetch = 10
		return err
	}
	msgs, err := con.Consume(queueName, serviceName, false, false, false, false, nil) // manual ack
	if err != nil {
		return err
	}

	b.setPub(pub)
	defer b.setPub(nil)
	log.Printf("service=%s connected to RabbitMQ, consuming %s", serviceName, queueName)

	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-closed:
			return fmt.Errorf("connection closed: %v", e)
		case d, ok := <-msgs:
			if !ok {
				return errors.New("delivery channel closed")
			}
			b.handleDelivery(ctx, pub, d)
		}
	}
}

func retryCount(h amqp.Table) int {
	switch v := h[retryHeader].(type) {
	case int32:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// handleDelivery: ack on success; retry up to 3 times on error; then DLQ.
func (b *Broker) handleDelivery(ctx context.Context, pub *amqp.Channel, d amqp.Delivery) {
	var env Envelope
	var result string
	err := json.Unmarshal(d.Body, &env)
	if err == nil && env.EventID == "" {
		err = errors.New("malformed event: missing eventId")
	}
	auctionID := "-"
	if err == nil {
		var probe struct {
			AuctionID string `json:"auctionId"`
		}
		_ = json.Unmarshal(env.Data, &probe)
		if probe.AuctionID != "" {
			auctionID = probe.AuctionID
		}
		result, err = handleEvent(env)
	}

	if err == nil {
		logEvent(env.EventType, env.EventID, auctionID, result)
		_ = d.Ack(false)
		return
	}

	retries := retryCount(d.Headers)
	if retries >= maxRetries {
		logEvent(env.EventType, env.EventID, auctionID, "dead-lettered: "+err.Error())
		_ = d.Nack(false, false) // requeue=false -> goes to listing.dlq via DLX
		return
	}

	logEvent(env.EventType, env.EventID, auctionID,
		fmt.Sprintf("retry %d/%d: %s", retries+1, maxRetries, err.Error()))
	time.Sleep(time.Second) // small back-off
	headers := amqp.Table{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	headers[retryHeader] = int32(retries + 1)
	// Default exchange + queue name = send straight back to our own queue
	perr := pub.PublishWithContext(ctx, "", queueName, false, false, amqp.Publishing{
		ContentType:  d.ContentType,
		DeliveryMode: amqp.Persistent,
		Headers:      headers,
		MessageId:    d.MessageId,
		Type:         d.Type,
		Body:         d.Body,
	})
	if perr != nil {
		_ = d.Nack(false, true) // could not re-publish: put it back, do not lose it
		return
	}
	_ = d.Ack(false)
}

// ---------------------------------------------------------------- event handlers

// handleEvent applies one event inside ONE transaction together with the
// processed_events insert, so a duplicate delivery can never be applied twice.
func handleEvent(env Envelope) (string, error) {
	switch env.EventType {
	case "AuctionClosed", "PaymentCompleted", "PaymentFailed":
	default:
		return "ignored", nil
	}

	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO processed_events(event_id) VALUES ($1) ON CONFLICT DO NOTHING`, env.EventID)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "duplicate-skipped", nil
	}

	var upd sql.Result
	switch env.EventType {
	case "AuctionClosed":
		var d struct {
			AuctionID  string  `json:"auctionId"`
			WinnerID   *string `json:"winnerId"`
			FinalPrice *int    `json:"finalPrice"`
		}
		if err := json.Unmarshal(env.Data, &d); err != nil || d.AuctionID == "" {
			return "", errors.New("malformed AuctionClosed data")
		}
		if d.WinnerID == nil || *d.WinnerID == "" {
			upd, err = tx.Exec(`UPDATE items SET status='Unsold' WHERE auction_id=$1`, d.AuctionID)
		} else {
			upd, err = tx.Exec(`UPDATE items SET status='Awaiting Payment', winner_id=$1, final_price=$2
				WHERE auction_id=$3`, d.WinnerID, d.FinalPrice, d.AuctionID)
		}
	case "PaymentCompleted":
		var d struct {
			AuctionID string `json:"auctionId"`
			ReceiptNo string `json:"receiptNo"`
		}
		if err := json.Unmarshal(env.Data, &d); err != nil || d.AuctionID == "" {
			return "", errors.New("malformed PaymentCompleted data")
		}
		upd, err = tx.Exec(`UPDATE items SET status='Sold', receipt_no=$1 WHERE auction_id=$2`, d.ReceiptNo, d.AuctionID)
	case "PaymentFailed":
		var d struct {
			AuctionID string `json:"auctionId"`
		}
		if err := json.Unmarshal(env.Data, &d); err != nil || d.AuctionID == "" {
			return "", errors.New("malformed PaymentFailed data")
		}
		upd, err = tx.Exec(`UPDATE items SET status='Payment Failed' WHERE auction_id=$1`, d.AuctionID)
	}
	if err != nil {
		return "", err
	}
	if n, _ := upd.RowsAffected(); n == 0 {
		return "", errors.New("no listing found for this auctionId")
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return "processed", nil
}

// ---------------------------------------------------------------- HTTP handlers

type createReq struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	ImageURL        string `json:"imageUrl"`
	ArtisanID       string `json:"artisanId"`
	StartPrice      int    `json:"startPrice"`
	DurationSeconds int    `json:"durationSeconds"`
	Category        string `json:"category"`
}

func createListing(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.ArtisanID = strings.TrimSpace(req.ArtisanID)
	switch {
	case req.Title == "":
		writeErr(w, 400, "title is required")
		return
	case req.ArtisanID == "":
		writeErr(w, 400, "artisanId is required")
		return
	case req.StartPrice <= 0:
		writeErr(w, 400, "startPrice must be a positive whole number (Nu.)")
		return
	case req.DurationSeconds <= 0:
		writeErr(w, 400, "durationSeconds must be greater than 0")
		return
	}
	req.Category = strings.TrimSpace(req.Category)
	if req.Category == "" {
		req.Category = "Handicraft"
	}
	row := db.QueryRowContext(r.Context(),
		`INSERT INTO items(title, description, image_url, artisan_id, start_price, duration_seconds, category)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+itemCols,
		req.Title, req.Description, req.ImageURL, req.ArtisanID, req.StartPrice, req.DurationSeconds, req.Category)
	it, err := scanItem(row)
	if err != nil {
		log.Printf("service=%s createListing error: %v", serviceName, err)
		writeErr(w, 500, "could not create listing")
		return
	}
	writeJSON(w, 201, it)
}

func startListing(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "invalid id")
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, 500, "database error")
		return
	}
	defer tx.Rollback()

	it, err := scanItem(tx.QueryRow(`SELECT `+itemCols+` FROM items WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, "listing not found")
		return
	} else if err != nil {
		writeErr(w, 500, "database error")
		return
	}
	if it.Status != "Draft" {
		writeErr(w, 409, "auction can only be started from Draft (current status: "+it.Status+")")
		return
	}

	auctionID := fmt.Sprintf("A-%d", it.ID)
	endsAt := time.Now().UTC().Add(time.Duration(it.DurationSeconds) * time.Second)
	if _, err := tx.Exec(`UPDATE items SET status='Open', auction_id=$1, ends_at=$2 WHERE id=$3`,
		auctionID, endsAt, it.ID); err != nil {
		writeErr(w, 500, "database error")
		return
	}

	// Publish BEFORE commit: if RabbitMQ is down the status change is rolled back.
	eventID, err := broker.Publish(r.Context(), "auction.started", "AuctionStarted", map[string]any{
		"auctionId":  auctionID,
		"itemId":     strconv.Itoa(it.ID), // contract: all IDs are strings
		"itemTitle":  it.Title,
		"artisanId":  it.ArtisanID,
		"startPrice": it.StartPrice,
		"endsAt":     endsAt.Format(time.RFC3339),
	})
	if err != nil {
		log.Printf("service=%s publish AuctionStarted failed: %v", serviceName, err)
		writeErr(w, 503, "event broker unavailable - auction not started, please retry")
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "database error")
		return
	}
	logEvent("AuctionStarted", eventID, auctionID, "published")

	it.Status, it.AuctionID, it.EndsAt = "Open", &auctionID, &endsAt
	writeJSON(w, 200, it)
}

func getListing(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "invalid id")
		return
	}
	it, err := scanItem(db.QueryRowContext(r.Context(), `SELECT `+itemCols+` FROM items WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, "listing not found")
		return
	} else if err != nil {
		writeErr(w, 500, "database error")
		return
	}
	writeJSON(w, 200, it)
}

func listListings(w http.ResponseWriter, r *http.Request) {
	q := `SELECT ` + itemCols + ` FROM items WHERE 1=1`
	var args []any
	if s := r.URL.Query().Get("status"); s != "" {
		args = append(args, s)
		q += fmt.Sprintf(" AND status=$%d", len(args))
	}
	if a := r.URL.Query().Get("artisanId"); a != "" {
		args = append(args, a)
		q += fmt.Sprintf(" AND artisan_id=$%d", len(args))
	}
	q += " ORDER BY id DESC"
	rows, err := db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, 500, "database error")
		return
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			writeErr(w, 500, "database error")
			return
		}
		items = append(items, it)
	}
	writeJSON(w, 200, items)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------- main

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	port := getenv("PORT", "8001")
	dbURL := getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/listings_db?sslmode=disable")
	mqURL := getenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")

	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
	}
	for i := 1; i <= 30; i++ { // wait for Postgres
		if err = db.Ping(); err == nil {
			break
		}
		log.Printf("service=%s waiting for database (%d/30): %v", serviceName, i, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("database unreachable: %v", err)
	}
	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	broker = &Broker{url: mqURL}
	go broker.Run(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /listings", createListing)
	mux.HandleFunc("GET /listings", listListings)
	mux.HandleFunc("GET /listings/{id}", getListing)
	mux.HandleFunc("POST /listings/{id}/start", startListing)

	srv := &http.Server{Addr: ":" + port, Handler: cors(mux)}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Printf("service=%s listening on :%s", serviceName, port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
