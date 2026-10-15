// Bidding service — owner: Pema Losel Maurer.
// Skeleton only: boots, declares the shared RabbitMQ topology, serves GET /health.
package main

import (
	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/service"
)

func main() {
	app, ctx, cancel := service.New(events.SourceBidding)
	defer cancel()

	// TODO(Pema Losel Maurer):
	//  - schema: auctions, bids + processed_events in bids_db
	//  - POST /auctions/{id}/bids, GET /auctions/{id}/bids
	//  - validate: amount > current highest and >= start price; reject on closed auctions
	//  - publish BidPlaced with previousBidderId / previousAmount
	//  - consume rabbitmq.BiddingQueue: AuctionStarted (open), AuctionClosed (lock)

	app.Run(ctx)
}
