package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/logx"
)

// Publisher sends envelopes to auction.events as persistent messages.
// It owns its own channel (AMQP channels are not safe for concurrent use),
// so it can be shared across HTTP handlers.
type Publisher struct {
	mu      sync.Mutex
	ch      *amqp.Channel
	service string
}

func NewPublisher(conn *amqp.Connection, service string) (*Publisher, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open publisher channel: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		return nil, fmt.Errorf("enable publisher confirms: %w", err)
	}
	return &Publisher{ch: ch, service: service}, nil
}

// Publish builds an envelope for data and sends it. Returns the envelope so
// callers can log or store the eventId.
func (p *Publisher) Publish(ctx context.Context, eventType string, data any) (events.Envelope, error) {
	env, err := events.New(p.service, eventType, data)
	if err != nil {
		return events.Envelope{}, err
	}
	return env, p.PublishEnvelope(ctx, env)
}

// PublishEnvelope sends an existing envelope unchanged, as used by the admin
// "resend last event" button so the duplicate keeps its original eventId.
func (p *Publisher) PublishEnvelope(ctx context.Context, env events.Envelope) error {
	key, ok := events.RoutingKeys[env.EventType]
	if !ok {
		return fmt.Errorf("unknown event type %q", env.EventType)
	}
	body, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	confirmation, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, Exchange, key, false, false, amqp.Publishing{
		DeliveryMode: amqp.Persistent,
		ContentType:  "application/json",
		MessageId:    env.EventID,
		Type:         env.EventType,
		Timestamp:    time.Now(),
		Body:         body,
	})
	if err != nil {
		return fmt.Errorf("publish %s: %w", env.EventType, err)
	}
	confirmed, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("confirm %s: %w", env.EventType, err)
	}
	if !confirmed {
		return fmt.Errorf("broker rejected %s", env.EventType)
	}
	logx.Event(p.service, env, logx.Published)
	return nil
}

func (p *Publisher) Close() error {
	return p.ch.Close()
}
