// Package rabbitmq holds the shared RabbitMQ setup: connecting with retry,
// declaring the exchange/queue topology from docs/events.md, and publishing.
package rabbitmq

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
)

const (
	Exchange    = "auction.events"
	DLX         = "auction.dlx"
	RetryHeader = "x-retry-count"
	MaxRetries  = 3
	Prefetch    = 10
)

// Queue names.
const (
	ListingQueue      = "listing.queue"
	BiddingQueue      = "bidding.queue"
	TimerQueue        = "timer.queue"
	PaymentQueue      = "payment.queue"
	NotificationQueue = "notification.queue"
)

// Bindings is the routing table from docs/events.md section 5.
var Bindings = map[string][]string{
	ListingQueue:      {events.KeyAuctionClosed, "payment.*"},
	BiddingQueue:      {events.KeyAuctionStarted, events.KeyAuctionClosed},
	TimerQueue:        {events.KeyAuctionStarted, events.KeyBidPlaced},
	PaymentQueue:      {events.KeyAuctionClosed},
	NotificationQueue: {"#"},
}

// DLQName returns the dead-letter queue for a queue, e.g. payment.queue -> payment.dlq.
func DLQName(queue string) string {
	return queue[:len(queue)-len(".queue")] + ".dlq"
}

// Connect dials RabbitMQ, retrying every 2 s until it succeeds or ctx is cancelled.
// Services start before RabbitMQ is ready, so this loop is expected on boot.
func Connect(ctx context.Context, url string) (*amqp.Connection, error) {
	for attempt := 1; ; attempt++ {
		conn, err := amqp.Dial(url)
		if err == nil {
			slog.Info("connected to rabbitmq", "attempt", attempt)
			return conn, nil
		}
		slog.Warn("rabbitmq not reachable, retrying", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// DeclareTopology declares the main exchange, the DLX, every queue, its DLQ
// and all bindings. Idempotent — every service calls it on startup, so every
// queue exists (and buffers messages) even while its owner is down.
func DeclareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(Exchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange %s: %w", Exchange, err)
	}
	if err := ch.ExchangeDeclare(DLX, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange %s: %w", DLX, err)
	}

	for queue, keys := range Bindings {
		dlq := DLQName(queue)
		if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare %s: %w", dlq, err)
		}
		// The DLQ is bound by queue name so each queue dead-letters only into its own DLQ.
		if err := ch.QueueBind(dlq, queue, DLX, false, nil); err != nil {
			return fmt.Errorf("bind %s: %w", dlq, err)
		}

		args := amqp.Table{
			"x-dead-letter-exchange":    DLX,
			"x-dead-letter-routing-key": queue,
		}
		if _, err := ch.QueueDeclare(queue, true, false, false, false, args); err != nil {
			return fmt.Errorf("declare %s: %w", queue, err)
		}
		for _, key := range keys {
			if err := ch.QueueBind(queue, key, Exchange, false, nil); err != nil {
				return fmt.Errorf("bind %s to %s: %w", queue, key, err)
			}
		}
	}
	return nil
}

// Setup connects, opens a channel, declares the topology and sets prefetch.
// Most services only need this one call at startup.
func Setup(ctx context.Context, url string) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := Connect(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("open channel: %w", err)
	}
	if err := DeclareTopology(ch); err != nil {
		conn.Close()
		return nil, nil, err
	}
	if err := ch.Qos(Prefetch, 0, false); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("set prefetch: %w", err)
	}
	return conn, ch, nil
}
