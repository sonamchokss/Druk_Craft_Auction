package reliability

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/rabbitmq"
	"time"
)

const OutboxDDL = `CREATE TABLE IF NOT EXISTS reliability_outbox(seq BIGSERIAL PRIMARY KEY,envelope JSONB NOT NULL,published_at TIMESTAMPTZ);`

func Enqueue(ctx context.Context, tx pgx.Tx, source, kind string, data any) error {
	env, err := events.New(source, kind, data)
	if err != nil {
		return err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO reliability_outbox(envelope) VALUES($1)`, body)
	return err
}

// Relay preserves event IDs across restarts. Publishing is at-least-once.
func Relay(ctx context.Context, pool *pgxpool.Pool, pub *rabbitmq.Publisher) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if err := flush(ctx, pool, pub); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
func flush(ctx context.Context, pool *pgxpool.Pool, pub *rabbitmq.Publisher) error {
	for i := 0; i < 100; i++ {
		if err := flushOne(ctx, pool, pub); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
	}
	return nil
}
func flushOne(ctx context.Context, pool *pgxpool.Pool, pub *rabbitmq.Publisher) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(303006)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return pgx.ErrNoRows
	}
	var seq int64
	var body []byte
	if err = tx.QueryRow(ctx, `SELECT seq,envelope FROM reliability_outbox WHERE published_at IS NULL ORDER BY seq LIMIT 1 FOR UPDATE`).Scan(&seq, &body); err != nil {
		return err
	}
	env, err := events.Parse(body)
	if err != nil {
		return err
	}
	if err = pub.PublishEnvelope(ctx, env); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE reliability_outbox SET published_at=now() WHERE seq=$1`, seq); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
