// Auction Timer service — owner: Sangay Choden.
// Skeleton only: boots, declares the shared RabbitMQ topology, serves GET /health.
package main

import (
	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/service"
)

func main() {
	app, ctx, cancel := service.New(events.SourceTimer)
	defer cancel()

	// TODO(Sangay Choden):
	//  - schema: timers + processed_events in timers_db
	//  - consume rabbitmq.TimerQueue: AuctionStarted (store endsAt), BidPlaced (track winner, anti-sniping)
	//  - publish AuctionExtended when a bid lands in the last 30 s
	//  - every second: publish AuctionClosed for expired auctions

	app.Run(ctx)
}
