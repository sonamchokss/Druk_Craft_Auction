// Notification service — owner: Sonam Wangmo.
// Skeleton only: boots, declares the shared RabbitMQ topology, serves GET /health.
package main

import (
	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/service"
)

func main() {
	app, ctx, cancel := service.New(events.SourceNotification)
	defer cancel()

	// TODO(Sonam Wangmo):
	//  - schema: notifications + processed_events in notifications_db
	//  - consume rabbitmq.NotificationQueue (all 6 events) -> notifications
	//  - GET /notifications?userId=
	//  - WebSocket endpoint pushing notifications + raw event stream

	app.Run(ctx)
}
