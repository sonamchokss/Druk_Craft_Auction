// Package logx writes the agreed log format (plan section 6): one JSON line
// per event with service, eventType, eventId, auctionId and result.
package logx

import (
	"log/slog"
	"os"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
)

// Result values for Event.
const (
	Published        = "published"
	Processed        = "processed"
	DuplicateSkipped = "duplicate-skipped"
	Retry            = "retry"
	DeadLettered     = "dead-lettered"
)

// Init makes slog emit JSON tagged with the service name. Call once in main.
func Init(service string) {
	h := slog.NewJSONHandler(os.Stdout, nil)
	slog.SetDefault(slog.New(h).With("service", service))
}

// Event logs one event outcome. Extra key/value pairs (e.g. "err", err) are appended.
func Event(service string, env events.Envelope, result string, extra ...any) {
	args := append([]any{
		"service", service,
		"eventType", env.EventType,
		"eventId", env.EventID,
		"auctionId", env.AuctionID(),
		"result", result,
	}, extra...)
	if result == DeadLettered || result == Retry {
		slog.Warn("event", args...)
		return
	}
	slog.Info("event", args...)
}
