// Package reliability provides transactional deduplication and bounded retries.
package reliability

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/logx"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/rabbitmq"
)

func Apply(ctx context.Context, pool *pgxpool.Pool, env events.Envelope, handle func(pgx.Tx) error) (bool, error) {
	if _, err := uuid.Parse(env.EventID); err != nil {
		return false, fmt.Errorf("invalid eventId: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	tag, err := tx.Exec(ctx, `INSERT INTO processed_events(event_id) VALUES($1) ON CONFLICT DO NOTHING`, env.EventID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return true, nil
	}
	if err = handle(tx); err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}
func RetryCount(h amqp.Table) int64 {
	var n int64
	switch v := h[rabbitmq.RetryHeader].(type) {
	case int:
		n = int64(v)
	case int64:
		n = v
	case int32:
		n = int64(v)
	case int16:
		n = int64(v)
	case int8:
		n = int64(v)
	}
	if n < 0 {
		return 0
	}
	return n
}

// Consume uses manual ack and a separate, confirmed publisher for queue-local retries.
func Consume(ctx context.Context, conn *amqp.Connection, ch *amqp.Channel, queue, source string, handle func(context.Context, events.Envelope) (bool, error)) error {
	retry, err := conn.Channel()
	if err != nil {
		return err
	}
	defer retry.Close()
	if err = retry.Confirm(false); err != nil {
		return err
	}
	msgs, err := ch.Consume(queue, source, false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case m, ok := <-msgs:
			if !ok {
				return errors.New("consumer delivery channel closed")
			}
			env, processingErr := events.Parse(m.Body)
			duplicate := false
			if processingErr == nil {
				duplicate, processingErr = handle(ctx, env)
			}
			if processingErr == nil {
				result := logx.Processed
				if duplicate {
					result = logx.DuplicateSkipped
				}
				logx.Event(source, env, result)
				if err = m.Ack(false); err != nil {
					return err
				}
				continue
			}
			n := RetryCount(m.Headers)
			if n >= rabbitmq.MaxRetries {
				if err = m.Nack(false, false); err != nil {
					return err
				}
				logx.Event(source, env, logx.DeadLettered, "err", processingErr)
				continue
			}
			headers := amqp.Table{}
			for k, v := range m.Headers {
				headers[k] = v
			}
			headers[rabbitmq.RetryHeader] = n + 1
			confirmation, err := retry.PublishWithDeferredConfirmWithContext(ctx, "", queue, false, false, amqp.Publishing{Headers: headers, ContentType: m.ContentType, ContentEncoding: m.ContentEncoding, DeliveryMode: amqp.Persistent, MessageId: m.MessageId, CorrelationId: m.CorrelationId, Type: m.Type, Timestamp: m.Timestamp, Body: m.Body})
			if err != nil {
				return err
			}
			ack, err := confirmation.WaitContext(ctx)
			if err != nil {
				return err
			}
			if !ack {
				return errors.New("retry publish rejected")
			}
			if err = m.Ack(false); err != nil {
				return err
			}
			logx.Event(source, env, logx.Retry, "attempt", n+1, "err", processingErr)
		}
	}
}
