package main

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/db"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/reliability"
	"os"
	"testing"
	"time"
)

// TEST_DATABASE_URL must point to a disposable Postgres database with CREATE SCHEMA permission.
func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set: Postgres integration test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	name := "timer_test_" + uuid.NewString()[:8]
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+name); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec(ctx, `DROP SCHEMA `+name+` CASCADE`); admin.Close() })
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, db.ProcessedEventsDDL+reliability.OutboxDDL+schema); err != nil {
		t.Fatal(err)
	}
	return &Store{pool}
}

func TestTimerTransactions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	end := time.Now().UTC().Add(time.Minute)
	start, _ := events.New(events.SourceListing, events.TypeAuctionStarted, events.AuctionStarted{AuctionID: "auction", ItemTitle: "Mask", ArtisanID: "artisan", EndsAt: end})
	if _, err := s.Apply(ctx, start); err != nil {
		t.Fatal(err)
	}
	bid, _ := events.New(events.SourceBidding, events.TypeBidPlaced, events.BidPlaced{AuctionID: "auction", BidderID: "buyer", Amount: 4500})
	bid.OccurredAt = end.Add(-10 * time.Second)
	if _, err := s.Apply(ctx, bid); err != nil {
		t.Fatal(err)
	}
	if dup, err := s.Apply(ctx, bid); err != nil || !dup {
		t.Fatalf("duplicate bid: %v %v", dup, err)
	}
	var actual time.Time
	var amount int64
	if err := s.pool.QueryRow(ctx, `SELECT ends_at,highest_amount FROM timers WHERE auction_id='auction'`).Scan(&actual, &amount); err != nil {
		t.Fatal(err)
	}
	// PostgreSQL timestamps retain microsecond precision.
	if actual.Sub(end.Add(30*time.Second)).Abs() > time.Microsecond || amount != 4500 {
		t.Fatalf("end=%v amount=%d", actual, amount)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE timers SET ends_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseExpired(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseExpired(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	var winner string
	var price int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM reliability_outbox WHERE envelope->>'eventType'='AuctionClosed'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT envelope->'data'->>'winnerId',(envelope->'data'->>'finalPrice')::bigint FROM reliability_outbox WHERE envelope->>'eventType'='AuctionClosed'`).Scan(&winner, &price); err != nil {
		t.Fatal(err)
	}
	if count != 1 || winner != "buyer" || price != 4500 {
		t.Fatalf("closures=%d winner=%s price=%d", count, winner, price)
	}
}
