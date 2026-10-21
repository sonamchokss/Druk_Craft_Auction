package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/rabbitmq"
)

// Relay publishes outbox rows in order. It runs right after each bid (Kick)
// and every 2 s as a fallback, so events queued while RabbitMQ was
// unreachable go out once it's back.
//
// Delivery is at-least-once: a crash between publish and marking the row
// sends the same eventId again, which consumers skip via processed_events.
type Relay struct {
	pool *pgxpool.Pool
	pub  *rabbitmq.Publisher
	kick chan struct{}
}

type outboxRow struct {
	Seq      int64
	Envelope []byte
}

func NewRelay(pool *pgxpool.Pool, pub *rabbitmq.Publisher) *Relay {
	return &Relay{pool: pool, pub: pub, kick: make(chan struct{}, 1)}
}

// Kick asks the relay to flush now. Never blocks.
func (r *Relay) Kick() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

func (r *Relay) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		r.flush(ctx)
		select {
		case <-ctx.Done():
			return
		case <-r.kick:
		case <-ticker.C:
		}
	}
}

func (r *Relay) flush(ctx context.Context) {
	rows, _ := r.pool.Query(ctx, `
		SELECT seq, envelope FROM outbox
		WHERE published_at IS NULL ORDER BY seq LIMIT 100`)
	pending, err := pgx.CollectRows(rows, pgx.RowToStructByPos[outboxRow])
	if err != nil {
		slog.Error("outbox read failed", "err", err)
		return
	}

	for _, row := range pending {
		var env events.Envelope
		if err := json.Unmarshal(row.Envelope, &env); err != nil {
			slog.Error("outbox row unreadable, skipping", "seq", row.Seq, "err", err)
			continue
		}
		if err := r.pub.PublishEnvelope(ctx, env); err != nil {
			// Stop here to keep order; the next tick retries from this row.
			slog.Error("outbox publish failed, will retry", "eventId", env.EventID, "err", err)
			return
		}
		if _, err := r.pool.Exec(ctx, `UPDATE outbox SET published_at = now() WHERE seq = $1`, row.Seq); err != nil {
			slog.Error("outbox mark failed", "seq", row.Seq, "err", err)
			return
		}
	}
}
