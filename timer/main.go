package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/db"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/rabbitmq"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/reliability"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/service"
	"log/slog"
	"os"
	"sync"
	"time"
)

const schema = `CREATE TABLE IF NOT EXISTS timers(
 auction_id TEXT PRIMARY KEY,item_title TEXT NOT NULL,artisan_id TEXT NOT NULL,
 ends_at TIMESTAMPTZ NOT NULL,winner_id TEXT,highest_amount BIGINT NOT NULL DEFAULT 0,
 closed BOOLEAN NOT NULL DEFAULT false);
 CREATE INDEX IF NOT EXISTS timers_due ON timers(ends_at) WHERE NOT closed;`

type Store struct{ pool *pgxpool.Pool }

// Extension adds 30 seconds to the current deadline; bids at/after expiry are late.
func extendedDeadline(end, placed time.Time) time.Time {
	if !placed.Before(end) || placed.Before(end.Add(-30*time.Second)) {
		return end
	}
	return end.Add(30 * time.Second)
}
func (s *Store) Apply(ctx context.Context, env events.Envelope) (bool, error) {
	return reliability.Apply(ctx, s.pool, env, func(tx pgx.Tx) error {
		switch env.EventType {
		case events.TypeAuctionStarted:
			d, err := events.Decode[events.AuctionStarted](env)
			if err != nil {
				return err
			}
			if d.AuctionID == "" || d.EndsAt.IsZero() || d.StartPrice < 0 {
				return errors.New("invalid AuctionStarted")
			}
			_, err = tx.Exec(ctx, `INSERT INTO timers(auction_id,item_title,artisan_id,ends_at) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, d.AuctionID, d.ItemTitle, d.ArtisanID, d.EndsAt)
			return err
		case events.TypeBidPlaced:
			d, err := events.Decode[events.BidPlaced](env)
			if err != nil {
				return err
			}
			if d.AuctionID == "" || d.BidderID == "" || d.Amount <= 0 || env.OccurredAt.IsZero() {
				return errors.New("invalid BidPlaced")
			}
			var end time.Time
			var closed bool
			var highest int64
			if err = tx.QueryRow(ctx, `SELECT ends_at,closed,highest_amount FROM timers WHERE auction_id=$1 FOR UPDATE`, d.AuctionID).Scan(&end, &closed, &highest); err != nil {
				return err
			}
			if closed || !env.OccurredAt.Before(end) || d.Amount <= highest {
				return nil
			}
			next := extendedDeadline(end, env.OccurredAt)
			if _, err = tx.Exec(ctx, `UPDATE timers SET winner_id=$2,highest_amount=$3,ends_at=$4 WHERE auction_id=$1`, d.AuctionID, d.BidderID, d.Amount, next); err != nil {
				return err
			}
			if !next.Equal(end) {
				return reliability.Enqueue(ctx, tx, events.SourceTimer, events.TypeAuctionExtended, events.AuctionExtended{AuctionID: d.AuctionID, NewEndsAt: next})
			}
			return nil
		default:
			return errors.New("unexpected timer event")
		}
	})
}
func (s *Store) CloseExpired(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx, `SELECT auction_id,item_title,artisan_id,winner_id,highest_amount FROM timers WHERE NOT closed AND ends_at<=now() ORDER BY ends_at LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	pending := []events.AuctionClosed{}
	for rows.Next() {
		var d events.AuctionClosed
		if err = rows.Scan(&d.AuctionID, &d.ItemTitle, &d.ArtisanID, &d.WinnerID, &d.FinalPrice); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, d)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, d := range pending {
		if err = reliability.Enqueue(ctx, tx, events.SourceTimer, events.TypeAuctionClosed, d); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE timers SET closed=true WHERE auction_id=$1`, d.AuctionID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func main() {
	app, ctx, cancel := service.New(events.SourceTimer)
	defer cancel()
	if _, err := app.DB.Exec(ctx, db.ProcessedEventsDDL+reliability.OutboxDDL+schema); err != nil {
		slog.Error("schema failed", "err", err)
		os.Exit(1)
	}
	store := &Store{app.DB}
	var wg sync.WaitGroup
	failed := make(chan error, 3)
	start := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				failed <- err
				cancel()
			}
		}()
	}
	start(func() error {
		return reliability.Consume(ctx, app.Conn, app.Channel, rabbitmq.TimerQueue, events.SourceTimer, store.Apply)
	})
	start(func() error { return reliability.Relay(ctx, app.DB, app.Publisher) })
	start(func() error {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-tick.C:
				if err := store.CloseExpired(ctx); err != nil {
					return err
				}
			}
		}
	})
	// Workers stop when the application context is cancelled.

	app.Run(ctx)
	wg.Wait()
	select {
	case err := <-failed:
		slog.Error("worker stopped", "err", err)
		os.Exit(1)
	default:
	}
}
