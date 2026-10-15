// Payment service — owner: Sangay Choden.
// Skeleton only: boots, declares the shared RabbitMQ topology, serves GET /health.
package main

import (
	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/service"
)

func main() {
	app, ctx, cancel := service.New(events.SourcePayment)
	defer cancel()

	// TODO(Sangay Choden):
	//  - schema: invoices, wallets + processed_events in payments_db
	//  - consume rabbitmq.PaymentQueue: AuctionClosed -> invoice + charge wallet
	//  - publish PaymentCompleted or PaymentFailed
	//  - GET /payments?buyerId=, GET /wallets/{buyerId}

	app.Run(ctx)
}
