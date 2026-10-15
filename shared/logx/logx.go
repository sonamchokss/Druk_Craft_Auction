// Package logx writes the agreed log format (plan section 6): one JSON line
// per event with service, eventType, eventId, auctionId and result.
package logx

import (
	"io"
	"log/slog"
	"os"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
)

// tagged is true once Init has attached "service" to the default logger,
// so Event must not add it again (that produced duplicate JSON keys).
var tagged bool

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
	initTo(service, os.Stdout)
}

// initTo is Init with a chosen writer; an empty service leaves the logger untagged.
func initTo(service string, w io.Writer) {
	l := slog.New(slog.NewJSONHandler(w, nil))
	tagged = service != ""
	if tagged {
		l = l.With("service", service)
	}
	slog.SetDefault(l)
}

// Event logs one event outcome. Extra key/value pairs (e.g. "err", err) are appended.
// service is only written if Init wasn't called; otherwise the logger already carries it.
func Event(service string, env events.Envelope, result string, extra ...any) {
	var args []any
	if !tagged {
		args = append(args, "service", service)
	}
	args = append(args,
		"eventType", env.EventType,
		"eventId", env.EventID,
		"auctionId", env.AuctionID(),
		"result", result,
	)
	args = append(args, extra...)
	if result == DeadLettered || result == Retry {
		slog.Warn("event", args...)
		return
	}
	slog.Info("event", args...)
}
