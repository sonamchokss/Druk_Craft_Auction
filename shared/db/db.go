// Package db opens a service's own Postgres database, retrying until it is up.
package db

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ProcessedEventsDDL is the idempotency table every consumer needs (plan section 6).
const ProcessedEventsDDL = `
CREATE TABLE IF NOT EXISTS processed_events (
    event_id     UUID PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);`

// Connect opens a pool and pings it, retrying every 2 s until ctx is cancelled.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	for attempt := 1; ; attempt++ {
		pool, err := pgxpool.New(ctx, url)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				slog.Info("connected to postgres", "attempt", attempt)
				return pool, nil
			}
			pool.Close()
		}
		slog.Warn("postgres not reachable, retrying", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
