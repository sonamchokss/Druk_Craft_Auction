package consumer

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/model"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/store"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/ws"
)

type Consumer struct {
	URL   string
	Store *store.Store
	Hub   *ws.Hub
	Cache *Cache
	mu    sync.Mutex
}

func (c *Consumer) Run(ctx context.Context) {
	for {
		if err := c.consume(ctx); err != nil {
			log.Printf("service=notification consumer-error=%v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *Consumer) consume(ctx context.Context) error {
	conn, err := amqp091.Dial(c.URL)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err = ch.ExchangeDeclare("auction.events", "topic", true, false, false, false, nil); err != nil {
		return err
	}
	// Must match docs/events.md §5 exactly: RabbitMQ rejects (406) any
	// re-declaration whose type or arguments differ from the other services'.
	if err = ch.ExchangeDeclare("auction.dlx", "direct", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err = ch.QueueDeclare("notification.dlq", true, false, false, false, nil); err != nil {
		return err
	}
	args := amqp091.Table{
		"x-dead-letter-exchange":    "auction.dlx",
		"x-dead-letter-routing-key": "notification.queue",
	}
	if _, err = ch.QueueDeclare("notification.queue", true, false, false, false, args); err != nil {
		return err
	}
	if err = ch.QueueBind("notification.queue", "#", "auction.events", false, nil); err != nil {
		return err
	}
	// Bound by queue name so only notification.queue's rejects land here.
	if err = ch.QueueBind("notification.dlq", "notification.queue", "auction.dlx", false, nil); err != nil {
		return err
	}
	if err = ch.Qos(10, 0, false); err != nil {
		return err
	}
	messages, err := ch.Consume("notification.queue", "notification-service", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case message, ok := <-messages:
			if !ok {
				return context.Canceled
			}
			c.handle(ctx, ch, message)
		}
	}
}

func (c *Consumer) handle(ctx context.Context, ch *amqp091.Channel, message amqp091.Delivery) {
	var envelope model.Envelope
	err := json.Unmarshal(message.Body, &envelope)
	eventID, eventType, auctionID := envelope.EventID, envelope.EventType, ""
	var raw model.EventData
	_ = json.Unmarshal(envelope.Data, &raw)
	auctionID = raw.AuctionID
	logResult := func(result string) {
		log.Printf("timestamp=%s service=notification eventType=%s eventId=%s auctionId=%s result=%s", time.Now().UTC().Format(time.RFC3339), eventType, eventID, auctionID, result)
	}
	if err == nil {
		c.Hub.SendEvent(envelope)
	}
	if err == nil && eventID != "" {
		c.mu.Lock()
		bidders, _ := c.Store.Bidders(ctx, auctionID)
		notes, newBidders, mapErr := Map(c.Cache, envelope, bidders)
		err = mapErr // a mapping failure must be retried, not acked as processed
		if mapErr == nil {
			err = c.Store.Process(ctx, eventID, notes, newBidders, auctionID)
			if err == nil {
				for _, note := range notes {
					c.Hub.SendUser(note.UserID, note)
				}
			}
		}
		c.mu.Unlock()
		if err == store.ErrDuplicate {
			_ = message.Ack(false)
			logResult("duplicate-skipped")
			return
		}
	}
	if err == nil {
		_ = message.Ack(false)
		logResult("processed")
		return
	}
	retries := 0
	if value, ok := message.Headers["x-retry-count"].(int32); ok {
		retries = int(value)
	}
	if value, ok := message.Headers["x-retry-count"].(int64); ok {
		retries = int(value)
	}
	if retries < 3 {
		headers := amqp091.Table{"x-retry-count": int32(retries + 1)}
		// Default exchange straight to our own queue: re-publishing to auction.events
		// would deliver the event to every other service again.
		pubErr := ch.PublishWithContext(ctx, "", "notification.queue", false, false, amqp091.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp091.Persistent,
			MessageId:    message.MessageId,
			Type:         message.Type,
			Body:         message.Body,
			Headers:      headers,
		})
		if pubErr != nil {
			_ = message.Nack(false, true) // keep the original rather than lose it
			return
		}
		_ = message.Ack(false)
		logResult("retry")
		return
	}
	_ = message.Nack(false, false)
	logResult("dead-lettered")
}
