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
	name := "payment_test_" + uuid.NewString()[:8]
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
func TestPaymentTransactions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	buyer := "buyer"
	closeEvent := func(id string, amount int64, winner *string) events.Envelope {
		env, err := events.New(events.SourceTimer, events.TypeAuctionClosed, events.AuctionClosed{AuctionID: id, WinnerID: winner, FinalPrice: amount})
		if err != nil {
			t.Fatal(err)
		}
		return env
	}
	env := closeEvent("sold", 4500, &buyer)
	if dup, err := s.Apply(ctx, env); err != nil || dup {
		t.Fatalf("first apply: %v %v", dup, err)
	}
	if dup, err := s.Apply(ctx, env); err != nil || !dup {
		t.Fatalf("duplicate: %v %v", dup, err)
	}
	env.EventID = uuid.NewString()
	if _, err := s.Apply(ctx, env); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, closeEvent("too-expensive", 6000, &buyer)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, closeEvent("unsold", 0, nil)); err != nil {
		t.Fatal(err)
	}
	bad := closeEvent("bad", -1, &buyer)
	if _, err := s.Apply(ctx, bad); err == nil {
		t.Fatal("invalid price accepted")
	}
	var balance int64
	var invoices, outbox, processed int
	if err := s.pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE buyer_id=$1`, buyer).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	s.pool.QueryRow(ctx, `SELECT count(*) FROM invoices`).Scan(&invoices)
	s.pool.QueryRow(ctx, `SELECT count(*) FROM reliability_outbox`).Scan(&outbox)
	s.pool.QueryRow(ctx, `SELECT count(*) FROM processed_events`).Scan(&processed)
	if balance != 5500 || invoices != 2 || outbox != 2 || processed != 4 {
		t.Fatalf("balance=%d invoices=%d outbox=%d processed=%d", balance, invoices, outbox, processed)
	}
	var status, kind string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM invoices WHERE auction_id='too-expensive'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT envelope->>'eventType' FROM reliability_outbox ORDER BY seq DESC LIMIT 1`).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if status != "Failed" || kind != events.TypePaymentFailed {
		t.Fatalf("status=%s event=%s", status, kind)
	}
}
