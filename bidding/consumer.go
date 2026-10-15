package main

import (
	"context"
	"errors"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/logx"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/rabbitmq"
)

// Consumer reads bidding.queue: AuctionStarted opens an auction locally,
// AuctionClosed locks it.
//
// Retry/DLQ follows docs/events.md §6. Swap handle/fail for the shared
// helper once feature/reliability is merged.
type Consumer struct {
	store *Store
	ch    *amqp.Channel
}

// Run blocks until ctx is cancelled. It returns an error if the broker
// connection drops; main exits and Docker's restart policy reconnects.
func (c *Consumer) Run(ctx context.Context) error {
	msgs, err := c.ch.Consume(rabbitmq.BiddingQueue, events.SourceBidding, false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case m, ok := <-msgs:
			if !ok {
				return errors.New("delivery channel closed")
			}
			c.handle(ctx, m)
		}
	}
}

func (c *Consumer) handle(ctx context.Context, m amqp.Delivery) {
	env, err := events.Parse(m.Body)
	if err != nil {
		c.fail(ctx, m, env, err)
		return
	}
	duplicate, err := c.store.ApplyEvent(ctx, env)
	if err != nil {
		c.fail(ctx, m, env, err)
		return
	}
	result := logx.Processed
	if duplicate {
		result = logx.DuplicateSkipped
	}
	logx.Event(events.SourceBidding, env, result)
	_ = m.Ack(false)
}

// fail retries a message up to MaxRetries times, then dead-letters it.
// Retries go through the default exchange straight to bidding.queue, so other
// services don't receive the event a second time.
func (c *Consumer) fail(ctx context.Context, m amqp.Delivery, env events.Envelope, cause error) {
	if env.EventID == "" { // malformed body — log what the AMQP properties say
		env.EventID, env.EventType = m.MessageId, m.Type
	}

	attempt := retryCount(m.Headers)
	if attempt >= rabbitmq.MaxRetries {
		logx.Event(events.SourceBidding, env, logx.DeadLettered, "err", cause.Error())
		_ = m.Nack(false, false) // x-dead-letter-exchange routes it to bidding.dlq
		return
	}

	headers := amqp.Table{}
	for k, v := range m.Headers {
		headers[k] = v
	}
	headers[rabbitmq.RetryHeader] = int32(attempt + 1)

	err := c.ch.PublishWithContext(ctx, "", rabbitmq.BiddingQueue, false, false, amqp.Publishing{
		Headers:      headers,
		DeliveryMode: amqp.Persistent,
		ContentType:  m.ContentType,
		MessageId:    m.MessageId,
		Type:         m.Type,
		Body:         m.Body,
	})
	if err != nil {
		_ = m.Nack(false, true) // couldn't re-publish: put the original back
		return
	}
	logx.Event(events.SourceBidding, env, logx.Retry, "attempt", attempt+1, "err", cause.Error())
	_ = m.Ack(false)
}

func retryCount(h amqp.Table) int {
	switch v := h[rabbitmq.RetryHeader].(type) {
	case int32:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	}
	return 0
}
