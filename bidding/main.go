// Bidding service — owner: Pema Losel Maurer.
// Accepts and validates bids, keeps bid history, publishes BidPlaced, and
// opens/locks auctions from AuctionStarted/AuctionClosed.
package main

import (
	"log/slog"
	"os"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/service"
)

func main() {
	app, ctx, cancel := service.New(events.SourceBidding)
	defer cancel()

	store := &Store{pool: app.DB}
	if err := store.Migrate(ctx); err != nil {
		slog.Error("startup failed", "err", err)
		os.Exit(1)
	}

	relay := NewRelay(app.DB, app.Publisher)
	go relay.Run(ctx)

	consumer := &Consumer{store: store, ch: app.Channel}
	go func() {
		if err := consumer.Run(ctx); err != nil {
			slog.Error("consumer stopped, exiting so Docker restarts us", "err", err)
			os.Exit(1)
		}
	}()

	(&api{store: store, relay: relay}).routes(app.Mux)
	app.Run(ctx)
}
