// Listing service — owner: Sonam Choki.
// Skeleton only: boots, declares the shared RabbitMQ topology, serves GET /health.
package main

import (
	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/service"
)

func main() {
	app, ctx, cancel := service.New(events.SourceListing)
	defer cancel()

	// TODO(Sonam Choki):
	//  - schema: items + processed_events (db.ProcessedEventsDDL) in listings_db
	//  - POST /listings, POST /listings/{id}/start, GET /listings, GET /listings/{id}
	//  - publish AuctionStarted on start
	//  - consume rabbitmq.ListingQueue: AuctionClosed, PaymentCompleted, PaymentFailed

	app.Run(ctx)
}
